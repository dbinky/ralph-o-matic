# Feature Pipeline — Phase 1: Server Changes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add post-completion hook, notify CLI command, and SendMessage to ralph-o-matic server so skills can send Teams notifications and trigger automated PR review after ralph jobs complete.

**Architecture:** New `PostCompletionCommand` config field stores a shell command. After a job reaches terminal status, the worker spawns it as a subprocess with job metadata as env vars. A new `SendMessage` method on the notification dispatcher and a `ralph-o-matic notify` CLI command let skills send arbitrary Teams/SMTP messages.

**Tech Stack:** Go 1.24, SQLite, Cobra CLI, Chi router, testify

**Spec:** `docs/superpowers/specs/2026-04-03-feature-pipeline-design.md`

---

### Task 1: Add PostCompletionCommand to Config Model

**Files:**
- Modify: `internal/models/config.go:150-178` (ServerConfig struct, Merge, DefaultServerConfig)
- Modify: `internal/models/config.go:352-453` (MergeJSON)
- Modify: `internal/models/config_response.go:43-101` (ServerConfigResponse, NewServerConfigResponse)
- Test: `internal/models/config_test.go`

- [ ] **Step 1: Write test for PostCompletionCommand merge**

Add to `internal/models/config_test.go`:

```go
func TestServerConfig_Merge_PostCompletionCommand(t *testing.T) {
	base := DefaultServerConfig()
	assert.Equal(t, "", base.PostCompletionCommand)

	// Merge with non-empty value
	updates := &ServerConfig{PostCompletionCommand: "echo hello"}
	result := base.Merge(updates)
	assert.Equal(t, "echo hello", result.PostCompletionCommand)

	// Merge with empty string should NOT clear (standard Merge behavior)
	updates2 := &ServerConfig{PostCompletionCommand: ""}
	result2 := result.Merge(updates2)
	assert.Equal(t, "echo hello", result2.PostCompletionCommand)
}

func TestServerConfig_MergeJSON_PostCompletionCommand_Clear(t *testing.T) {
	base := DefaultServerConfig()
	base.PostCompletionCommand = "echo hello"

	// Explicitly setting to empty string via JSON should clear it
	raw := json.RawMessage(`{"post_completion_command": ""}`)
	result, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, "", result.PostCompletionCommand)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestServerConfig_Merge_PostCompletionCommand ./internal/models/`
Expected: compilation error — `PostCompletionCommand` not defined on ServerConfig

- [ ] **Step 3: Add PostCompletionCommand field to ServerConfig**

In `internal/models/config.go`, add to the `ServerConfig` struct after the Notifications section:

```go
	// Notifications
	Notify NotifyConfig `json:"notify"`

	// Hooks
	PostCompletionCommand string `json:"post_completion_command"`
}
```

- [ ] **Step 4: Add to Merge method**

In `internal/models/config.go`, in the `Merge` method, after the Teams notification merge block (after line ~344):

```go
	// Post-completion hook
	if updates.PostCompletionCommand != "" {
		result.PostCompletionCommand = updates.PostCompletionCommand
	}
```

- [ ] **Step 5: Add to MergeJSON method**

In `internal/models/config.go`, in the `MergeJSON` method, after the notify block (after line ~450):

```go
	// Post-completion hook: allow clearing via explicit empty string
	if _, ok := rawMap["post_completion_command"]; ok {
		result.PostCompletionCommand = updates.PostCompletionCommand
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -v -run TestServerConfig_Merge_PostCompletionCommand ./internal/models/`
Expected: PASS

Run: `go test -v -run TestServerConfig_MergeJSON_PostCompletionCommand_Clear ./internal/models/`
Expected: PASS

- [ ] **Step 7: Add to ServerConfigResponse**

In `internal/models/config_response.go`, add to the `ServerConfigResponse` struct:

```go
	Notify               NotifyConfigResponse    `json:"notify"`
	PostCompletionCommand string                  `json:"post_completion_command"`
}
```

In `NewServerConfigResponse`, add before the closing brace:

```go
		PostCompletionCommand: cfg.PostCompletionCommand,
```

- [ ] **Step 8: Add to serverConfigCmd display**

In `cmd/cli/commands.go`, in the `serverConfigCmd` RunE function, after the notifications display block (after the `fmt.Printf("notify.teams.enabled: %v\n", serverCfg.Notify.Teams.Enabled)` line):

```go
				fmt.Println()
				fmt.Println("# Hooks")
				if serverCfg.PostCompletionCommand != "" {
					fmt.Printf("post_completion_command: %s\n", serverCfg.PostCompletionCommand)
				} else {
					fmt.Printf("post_completion_command: (none)\n")
				}
```

- [ ] **Step 9: Run full model tests**

Run: `go test -v -short ./internal/models/`
Expected: all PASS

- [ ] **Step 10: Commit**

```bash
git add internal/models/config.go internal/models/config_response.go internal/models/config_test.go cmd/cli/commands.go
git commit -m "feat: add PostCompletionCommand config field for post-job hooks"
```

---

### Task 2: Add SendMessage to Notification System

**Files:**
- Modify: `internal/notify/teams.go` (add SendMessage method)
- Modify: `internal/notify/notify.go` (add SendMessage to Dispatcher)
- Test: `internal/notify/teams_test.go`
- Test: `internal/notify/notify_test.go`

- [ ] **Step 1: Write test for TeamsNotifier.SendMessage**

Create or add to `internal/notify/teams_test.go`:

```go
package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamsNotifier_SendMessage(t *testing.T) {
	var received map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		err := json.NewDecoder(r.Body).Decode(&received)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewTeamsNotifier(models.TeamsConfig{
		Enabled:    true,
		WebhookURL: server.URL,
	})

	err := notifier.SendMessage(context.Background(), "Pipeline started for user-auth")
	require.NoError(t, err)

	assert.Equal(t, "MessageCard", received["@type"])
	assert.Equal(t, "Ralph-o-matic", received["summary"])
	sections := received["sections"].([]interface{})
	require.Len(t, sections, 1)
	section := sections[0].(map[string]interface{})
	assert.Equal(t, "Pipeline started for user-auth", section["text"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestTeamsNotifier_SendMessage ./internal/notify/`
Expected: compilation error — `SendMessage` not defined

- [ ] **Step 3: Implement TeamsNotifier.SendMessage**

Add to `internal/notify/teams.go`:

```go
// SendMessage sends a plain text message to Teams via webhook.
func (t *TeamsNotifier) SendMessage(ctx context.Context, message string) error {
	if t.config.WebhookURL == "" {
		return fmt.Errorf("teams: no webhook URL configured")
	}

	card := map[string]interface{}{
		"@type":    "MessageCard",
		"@context": "http://schema.org/extensions",
		"themeColor": "0078D7",
		"summary":    "Ralph-o-matic",
		"sections": []interface{}{
			map[string]interface{}{
				"activityTitle": "Ralph-o-matic",
				"text":          message,
				"markdown":      true,
			},
		},
	}

	payload, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("teams: failed to marshal card: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("teams: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("teams: webhook returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestTeamsNotifier_SendMessage ./internal/notify/`
Expected: PASS

- [ ] **Step 5: Write test for Dispatcher.SendMessage**

Add to `internal/notify/notify_test.go` (create if needed):

```go
package notify

import (
	"context"
	"log/slog"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
)

type mockConfigProvider struct {
	cfg *models.ServerConfig
}

func (m *mockConfigProvider) Get() (*models.ServerConfig, error) {
	return m.cfg, nil
}

type mockMessageSender struct {
	messages []string
	name     string
}

func (m *mockMessageSender) Notify(ctx context.Context, job *models.Job, event Event) error {
	return nil
}

func (m *mockMessageSender) Name() string { return m.name }

func (m *mockMessageSender) SendMessage(ctx context.Context, message string) error {
	m.messages = append(m.messages, message)
	return nil
}

func TestDispatcher_SendMessage_NoNotifiers(t *testing.T) {
	cp := &mockConfigProvider{cfg: models.DefaultServerConfig()}
	d := NewDispatcher(cp, slog.Default())

	// Should not error — just no-op
	d.SendMessage(context.Background(), "hello")
}

func TestDispatcher_SendMessage_TeamsEnabled(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Notify.Teams.Enabled = true
	cfg.Notify.Teams.WebhookURL = "http://example.com/webhook"

	cp := &mockConfigProvider{cfg: cfg}
	d := NewDispatcher(cp, slog.Default())

	// This will fail to reach the URL but should not panic
	d.SendMessage(context.Background(), "test message")
}
```

- [ ] **Step 6: Define MessageSender interface and add SendMessage to Dispatcher**

In `internal/notify/notify.go`, add the interface and method:

```go
// MessageSender can send a plain text message (not tied to a job event).
// Notifiers that implement this interface support the `notify` CLI command.
type MessageSender interface {
	SendMessage(ctx context.Context, message string) error
}

// SendMessage sends a plain text message to all enabled notifiers that
// support the MessageSender interface. Errors are logged, never returned.
func (d *Dispatcher) SendMessage(ctx context.Context, message string) {
	if message == "" {
		return
	}

	cfg, err := d.configProvider.Get()
	if err != nil {
		d.logger.Error("notify: failed to load config for SendMessage", "error", err)
		return
	}

	notifiers := d.buildNotifiers(cfg)
	for _, n := range notifiers {
		if sender, ok := n.(MessageSender); ok {
			d.callMessageSender(ctx, sender, n.Name(), message)
		}
	}
}

func (d *Dispatcher) callMessageSender(ctx context.Context, sender MessageSender, name, message string) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("notify: message sender panicked",
				"notifier", name,
				"panic", fmt.Sprintf("%v", r),
			)
		}
	}()

	if err := sender.SendMessage(ctx, message); err != nil {
		d.logger.Error("notify: SendMessage failed",
			"notifier", name,
			"error", err,
		)
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test -v -run TestDispatcher_SendMessage ./internal/notify/`
Expected: PASS

Run: `go test -v -short ./internal/notify/`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/notify/teams.go internal/notify/notify.go internal/notify/teams_test.go internal/notify/notify_test.go
git commit -m "feat: add SendMessage to notification system for arbitrary messages"
```

---

### Task 3: Implement Post-Completion Hook

**Files:**
- Create: `internal/worker/hook.go`
- Create: `internal/worker/hook_test.go`

- [ ] **Step 1: Write test for RunPostCompletionHook env vars**

Create `internal/worker/hook_test.go`:

```go
package worker

import (
	"context"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPostCompletionHook_SetsEnvVars(t *testing.T) {
	started := time.Now()
	job := &models.Job{
		ID:           42,
		RepoURL:      "https://github.com/test/repo",
		Branch:       "dev-feature",
		ResultBranch: "ralph/dev-feature-result",
		PRURL:        "https://github.com/test/repo/pull/7",
		WorkingDir:   "/tmp/test-repo",
		Status:       models.StatusCompleted,
		StartedAt:    &started,
	}

	// Use printenv to capture env vars
	output, err := RunPostCompletionHook(
		context.Background(),
		"printenv RALPH_JOB_ID RALPH_REPO_URL RALPH_BRANCH RALPH_RESULT_BRANCH RALPH_PR_URL RALPH_WORKING_DIR RALPH_EXIT_STATUS",
		job,
	)
	require.NoError(t, err)

	assert.Contains(t, output, "42")
	assert.Contains(t, output, "https://github.com/test/repo")
	assert.Contains(t, output, "dev-feature")
	assert.Contains(t, output, "ralph/dev-feature-result")
	assert.Contains(t, output, "https://github.com/test/repo/pull/7")
	assert.Contains(t, output, "/tmp/test-repo")
	assert.Contains(t, output, "completed")
}

func TestRunPostCompletionHook_EmptyCommand(t *testing.T) {
	job := &models.Job{ID: 1, Status: models.StatusCompleted}
	output, err := RunPostCompletionHook(context.Background(), "", job)
	assert.NoError(t, err)
	assert.Equal(t, "", output)
}

func TestRunPostCompletionHook_CommandFailure(t *testing.T) {
	job := &models.Job{ID: 1, Status: models.StatusFailed}
	_, err := RunPostCompletionHook(context.Background(), "exit 1", job)
	assert.Error(t, err)
}

func TestRunPostCompletionHook_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	job := &models.Job{ID: 1, Status: models.StatusCompleted}
	_, err := RunPostCompletionHook(ctx, "sleep 10", job)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestRunPostCompletionHook ./internal/worker/`
Expected: compilation error — `RunPostCompletionHook` not defined

- [ ] **Step 3: Implement RunPostCompletionHook**

Create `internal/worker/hook.go`:

```go
package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// RunPostCompletionHook executes the configured post-completion command
// with job metadata as environment variables. Returns combined stdout/stderr
// and any error. Returns ("", nil) if command is empty.
func RunPostCompletionHook(ctx context.Context, command string, job *models.Job) (string, error) {
	if command == "" {
		return "", nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("RALPH_JOB_ID=%d", job.ID),
		fmt.Sprintf("RALPH_REPO_URL=%s", job.RepoURL),
		fmt.Sprintf("RALPH_BRANCH=%s", job.Branch),
		fmt.Sprintf("RALPH_RESULT_BRANCH=%s", job.ResultBranch),
		fmt.Sprintf("RALPH_PR_URL=%s", job.PRURL),
		fmt.Sprintf("RALPH_WORKING_DIR=%s", job.WorkingDir),
		fmt.Sprintf("RALPH_EXIT_STATUS=%s", string(job.Status)),
	)

	output, err := cmd.CombinedOutput()
	return string(output), err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v -run TestRunPostCompletionHook ./internal/worker/`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/worker/hook.go internal/worker/hook_test.go
git commit -m "feat: add post-completion hook execution with job env vars"
```

---

### Task 4: Integrate Hook into Worker

**Files:**
- Modify: `internal/worker/worker.go:40-68` (Worker struct, setters)
- Modify: `internal/worker/worker.go:254-268` (hook trigger after completion/failure)
- Modify: `cmd/server/main.go:119-141` (wire up config provider)
- Test: `internal/worker/worker_test.go`

- [ ] **Step 1: Write test for worker calling hook on completion**

Add to `internal/worker/worker_test.go`:

```go
// mockConfigProvider implements notify.ConfigProvider for testing
type mockConfigProvider struct {
	cfg *models.ServerConfig
}

func (m *mockConfigProvider) Get() (*models.ServerConfig, error) {
	return m.cfg, nil
}

func TestWorker_PostCompletionHook_CalledOnComplete(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{{Completed: true}},
	}
	job := &models.Job{
		ID:            1,
		Branch:        "test",
		MaxIterations: 10,
		Status:        models.StatusRunning,
	}
	q := &mockQueue{jobs: []*models.Job{job}}

	cfg := models.DefaultServerConfig()
	cfg.PostCompletionCommand = "echo hook-ran"
	cp := &mockConfigProvider{cfg: cfg}

	w := New(q, handler, time.Second)
	w.SetConfigProvider(cp)

	// Disable watchdog to avoid test flake
	w.watchdogInterval = 24 * time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w.poll(ctx)

	// Job should be completed
	require.Len(t, q.completed, 1)
	assert.Equal(t, int64(1), q.completed[0].ID)
}

func TestWorker_PostCompletionHook_NotCalledWhenNoConfig(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{{Completed: true}},
	}
	job := &models.Job{
		ID:            1,
		Branch:        "test",
		MaxIterations: 10,
		Status:        models.StatusRunning,
	}
	q := &mockQueue{jobs: []*models.Job{job}}

	w := New(q, handler, time.Second)
	// No config provider set — hook should be skipped

	w.watchdogInterval = 24 * time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w.poll(ctx)

	// Should still complete without error
	require.Len(t, q.completed, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestWorker_PostCompletionHook ./internal/worker/`
Expected: compilation error — `SetConfigProvider` not defined

- [ ] **Step 3: Add configProvider to Worker and setter**

In `internal/worker/worker.go`, add to the Worker struct:

```go
	// Progress reporting interval (0 = use default 5s)
	progressInterval time.Duration

	// Config provider for reading post-completion hook command (notify.ConfigProvider)
	configProvider notify.ConfigProvider
```

Add the setter after the existing `SetBroadcaster` method. The `notify` package already defines `ConfigProvider` with the same `Get() (*models.ServerConfig, error)` signature, so reuse it:

```go
// SetConfigProvider sets the config provider for reading hook commands.
// Satisfies notify.ConfigProvider — same interface the Dispatcher uses.
func (w *Worker) SetConfigProvider(cp notify.ConfigProvider) {
	w.configProvider = cp
}
```

- [ ] **Step 4: Add hook trigger after completion and failure**

In `internal/worker/worker.go`, in the `poll` method, replace the completion block (lines 254-267) with:

```go
	if completedBySignal {
		if err := w.queue.Complete(job); err != nil {
			log.Printf("Worker: failed to mark job #%d as complete: %v", job.ID, err)
		} else {
			log.Printf("Worker: job #%d completed after %d iterations", job.ID, job.Iteration)
		}
		w.sendNotification(ctx, job, notify.EventCompleted)
	} else {
		log.Printf("Worker: job #%d reached max iterations (%d) without completion signal", job.ID, job.MaxIterations)
		if fErr := w.queue.Fail(job, fmt.Sprintf("max iterations reached (%d) without completion signal", job.MaxIterations)); fErr != nil {
			log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
		}
		w.sendNotification(ctx, job, notify.EventFailed)
	}

	// Run post-completion hook asynchronously
	w.runPostCompletionHook(job)
```

Add the helper method:

```go
// runPostCompletionHook checks config for a post-completion command and
// runs it in a background goroutine. Never blocks the worker.
func (w *Worker) runPostCompletionHook(job *models.Job) {
	if w.configProvider == nil {
		return
	}

	cfg, err := w.configProvider.Get()
	if err != nil {
		log.Printf("Worker: failed to load config for post-completion hook: %v", err)
		return
	}

	if cfg.PostCompletionCommand == "" {
		return
	}

	command := cfg.PostCompletionCommand
	go func() {
		log.Printf("Worker: running post-completion hook for job #%d", job.ID)
		output, err := RunPostCompletionHook(context.Background(), command, job)
		if err != nil {
			log.Printf("Worker: post-completion hook failed for job #%d: %v\nOutput: %s", job.ID, err, output)
			// Send failure notification
			if w.notifier != nil {
				w.sendNotification(context.Background(), job, notify.EventFailed)
			}
			return
		}
		if output != "" {
			log.Printf("Worker: post-completion hook output for job #%d:\n%s", job.ID, output)
		}
		log.Printf("Worker: post-completion hook completed for job #%d", job.ID)
	}()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v -run TestWorker_PostCompletionHook ./internal/worker/`
Expected: PASS

- [ ] **Step 6: Wire up in server main.go**

In `cmd/server/main.go`, after `w.SetNotifier(dispatcher)` (line 141), add:

```go
	w.SetConfigProvider(configRepo)
```

- [ ] **Step 7: Run all worker tests**

Run: `go test -v -short ./internal/worker/`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/worker/worker.go internal/worker/worker_test.go cmd/server/main.go
git commit -m "feat: integrate post-completion hook into worker lifecycle"
```

---

### Task 5: Add API Notify Endpoint

**Files:**
- Modify: `internal/api/config.go` (add handleSendNotify)
- Modify: `internal/api/server.go` (register route)
- Modify: `internal/cli/client.go` (add SendNotify method)

- [ ] **Step 1: Add handleSendNotify to API**

In `internal/api/config.go`, add after the `handleTestNotify` function:

```go
func (s *Server) handleSendNotify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	configRepo := db.NewConfigRepo(s.db)
	cfg, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load config: "+err.Error())
		return
	}

	// Build and send to all enabled notifiers
	var sent []string
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if cfg.Notify.Teams.Enabled {
		n := notify.NewTeamsNotifier(cfg.Notify.Teams)
		if err := n.SendMessage(ctx, req.Message); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("teams: %v", err),
			})
			return
		}
		sent = append(sent, "teams")
	}

	if cfg.Notify.SMTP.Enabled {
		n := notify.NewSMTPNotifier(cfg.Notify.SMTP)
		if sender, ok := interface{}(n).(notify.MessageSender); ok {
			if err := sender.SendMessage(ctx, req.Message); err != nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": false,
					"error":   fmt.Sprintf("smtp: %v", err),
				})
				return
			}
			sent = append(sent, "smtp")
		}
	}

	if len(sent) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "no notification channels enabled",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  fmt.Sprintf("Sent to: %s", strings.Join(sent, ", ")),
		"channels": sent,
	})
}
```

Add `"strings"` to the imports in `internal/api/config.go` (not currently imported — add after `"net/http"`).

- [ ] **Step 2: Register route in server.go**

In `internal/api/server.go`, in the `/api/config` route group, add after the test-notify line:

```go
					r.Post("/test-notify", auth.RequireRole("Admin", s.handleTestNotify))
					r.Post("/notify", s.handleSendNotify)
```

Note: `/api/notify` does NOT require admin role — skills running as the user should be able to send notifications.

- [ ] **Step 3: Add SendNotify to CLI client**

In `internal/cli/client.go`, add:

```go
// SendNotifyResponse is the response from the notify endpoint.
type SendNotifyResponse struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Error    string   `json:"error,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// SendNotify sends a message to all configured notification channels.
func (c *Client) SendNotify(message string) (*SendNotifyResponse, error) {
	req := map[string]string{"message": message}
	var resp SendNotifyResponse
	if err := c.post("/api/config/notify", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
```

- [ ] **Step 4: Run build to verify compilation**

Run: `go build ./...`
Expected: builds without errors

- [ ] **Step 5: Commit**

```bash
git add internal/api/config.go internal/api/server.go internal/cli/client.go
git commit -m "feat: add API notify endpoint for sending arbitrary messages"
```

---

### Task 6: Add CLI Notify Command

**Files:**
- Modify: `cmd/cli/commands.go` (add notifyCmd)
- Modify: `cmd/cli/main.go` (register command)

- [ ] **Step 1: Add notifyCmd function**

In `cmd/cli/commands.go`, add after the `testNotifyCmd` function:

```go
func notifyCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Send a message to all configured notification channels",
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				// Allow message as positional arg for convenience
				if len(args) > 0 {
					message = strings.Join(args, " ")
				} else {
					return fmt.Errorf("message is required: use --message or pass as argument")
				}
			}

			resp, err := client.SendNotify(message)
			if err != nil {
				return err
			}

			if resp.Success {
				fmt.Println(resp.Message)
			} else {
				return fmt.Errorf("%s", resp.Error)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "Message to send")
	return cmd
}
```

- [ ] **Step 2: Register in main.go**

In `cmd/cli/main.go`, add `notifyCmd()` to the `rootCmd.AddCommand` call:

```go
	rootCmd.AddCommand(
		submitCmd(),
		statusCmd(),
		logsCmd(),
		cancelCmd(),
		pauseCmd(),
		resumeCmd(),
		updateCmd(),
		moveCmd(),
		configCmd(),
		serverConfigCmd(),
		testNotifyCmd(),
		notifyCmd(),
	)
```

- [ ] **Step 3: Build and verify**

Run: `go build -o build/ralph-o-matic ./cmd/cli/`
Expected: builds successfully

Run: `./build/ralph-o-matic notify --help`
Expected: shows usage for notify command

- [ ] **Step 4: Run full test suite**

Run: `make test`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/cli/commands.go cmd/cli/main.go
git commit -m "feat: add 'notify' CLI command for sending arbitrary messages"
```
