# Phase 6: Executor (Ralph Loop)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the ralph loop executor that shells out to Claude Code with Ollama configuration and monitors for completion.

**Architecture:** The executor spawns Claude Code as a subprocess with appropriate environment variables. It monitors output for iteration progress and the `<promise>` completion tag. Supports pause/resume via process signals.

**Tech Stack:** Go 1.22+, os/exec for process management, bufio for output parsing

**Dependencies:** Phase 5 must be complete (git integration)

---

## Task 1: Implement Claude Code Executor

**Files:**
- Create: `internal/executor/claude.go`
- Create: `internal/executor/claude_test.go`

**Step 1: Write the failing tests**

Create `internal/executor/claude_test.go`:

```go
package executor

import (
	"context"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestClaudeExecutor_BuildEnv(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(map[string]string{"CUSTOM": "value"})

	// Should contain Ollama config
	assert.Contains(t, env, "ANTHROPIC_BASE_URL=http://localhost:11434")
	assert.Contains(t, env, "ANTHROPIC_AUTH_TOKEN=ollama")
	assert.Contains(t, env, "ANTHROPIC_API_KEY=")
	assert.Contains(t, env, "ANTHROPIC_MODEL=qwen3-coder:70b")
	assert.Contains(t, env, "ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen2.5-coder:7b")
	assert.Contains(t, env, "CUSTOM=value")
}

func TestClaudeExecutor_BuildCommand(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	cmd := exec.BuildCommand("Write tests for auth module")

	assert.Equal(t, "claude", cmd[0])
	assert.Contains(t, cmd, "--print")
	// Prompt should be passed via stdin, not command line
}

func TestClaudeExecutor_ParseOutput_Iteration(t *testing.T) {
	output := `[iteration 5] Running tests...
[iteration 5] Tests failed: 3 errors
[iteration 5] Fixing auth.go`

	iterations := ParseIterations(output)
	assert.Equal(t, 5, iterations)
}

func TestClaudeExecutor_ParseOutput_Promise(t *testing.T) {
	output := `All tests passing!
<promise>COMPLETE</promise>`

	assert.True(t, ContainsPromise(output, "COMPLETE"))
	assert.False(t, ContainsPromise(output, "DONE"))
}

func TestClaudeExecutor_ParseOutput_NoPromise(t *testing.T) {
	output := "Still working on tests..."

	assert.False(t, ContainsPromise(output, "COMPLETE"))
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/executor/... -v
```

Expected: FAIL - ClaudeExecutor not defined

**Step 3: Write minimal implementation**

Create `internal/executor/claude.go`:

```go
package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ClaudeExecutor manages Claude Code subprocess execution
type ClaudeExecutor struct {
	config *models.ServerConfig
}

// NewClaudeExecutor creates a new executor
func NewClaudeExecutor(config *models.ServerConfig) *ClaudeExecutor {
	return &ClaudeExecutor{config: config}
}

// BuildEnv creates the environment variables for Claude Code with Ollama
func (e *ClaudeExecutor) BuildEnv(extra map[string]string) []string {
	env := os.Environ()

	// Ollama configuration
	ollamaEnv := map[string]string{
		"ANTHROPIC_BASE_URL":           "http://localhost:11434",
		"ANTHROPIC_AUTH_TOKEN":         "ollama",
		"ANTHROPIC_API_KEY":            "",
		"ANTHROPIC_MODEL":              e.config.LargeModel,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.SmallModel,
	}

	for k, v := range ollamaEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range extra {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// BuildCommand creates the claude command arguments
func (e *ClaudeExecutor) BuildCommand(prompt string) []string {
	return []string{
		"claude",
		"--print", // Non-interactive mode
	}
}

// ExecutionResult contains the results of running Claude Code
type ExecutionResult struct {
	Output     string
	Iterations int
	Completed  bool
	Error      error
}

// OutputCallback is called for each line of output
type OutputCallback func(line string)

// Execute runs Claude Code with the given prompt
func (e *ClaudeExecutor) Execute(ctx context.Context, workDir, prompt string, env map[string]string, onOutput OutputCallback) (*ExecutionResult, error) {
	cmd := exec.CommandContext(ctx, "claude", "--print")
	cmd.Dir = workDir
	cmd.Env = e.BuildEnv(env)

	// Pass prompt via stdin
	cmd.Stdin = strings.NewReader(prompt)

	// Capture output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Read output in goroutines
	var outputBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		e.readOutput(stdout, &outputBuf, onOutput)
	}()

	go func() {
		defer wg.Done()
		e.readOutput(stderr, &outputBuf, onOutput)
	}()

	wg.Wait()

	err = cmd.Wait()

	output := outputBuf.String()
	result := &ExecutionResult{
		Output:     output,
		Iterations: ParseIterations(output),
		Completed:  ContainsPromise(output, "COMPLETE") || ContainsPromise(output, "DONE"),
		Error:      err,
	}

	return result, nil
}

func (e *ClaudeExecutor) readOutput(r io.Reader, buf *bytes.Buffer, callback OutputCallback) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line + "\n")
		if callback != nil {
			callback(line)
		}
	}
}

// ParseIterations extracts the current iteration number from output
func ParseIterations(output string) int {
	// Look for patterns like "[iteration 5]" or "Iteration: 5"
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\[iteration\s+(\d+)\]`),
		regexp.MustCompile(`Iteration:\s*(\d+)`),
		regexp.MustCompile(`iter\s+(\d+)`),
	}

	maxIter := 0
	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(output, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				if iter, err := strconv.Atoi(match[1]); err == nil && iter > maxIter {
					maxIter = iter
				}
			}
		}
	}

	return maxIter
}

// ContainsPromise checks if output contains a promise tag with the given text
func ContainsPromise(output, promiseText string) bool {
	pattern := fmt.Sprintf(`<promise>%s</promise>`, regexp.QuoteMeta(promiseText))
	matched, _ := regexp.MatchString(pattern, output)
	return matched
}

// IsClaudeInstalled checks if claude CLI is available
func IsClaudeInstalled() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/executor/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/executor/claude.go internal/executor/claude_test.go
git commit -m "feat(executor): add Claude Code executor with Ollama config"
```

---

## Task 2: Implement Ralph Loop Handler

**Files:**
- Create: `internal/executor/ralph.go`
- Create: `internal/executor/ralph_test.go`

**Step 1: Write the failing tests**

Create `internal/executor/ralph_test.go`:

```go
package executor

import (
	"context"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })
	return database
}

func TestRalphHandler_ShouldContinue(t *testing.T) {
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)

	// Not at max
	job.Iteration = 5
	assert.True(t, shouldContinue(job))

	// At max
	job.Iteration = 10
	assert.False(t, shouldContinue(job))
}

func TestRalphHandler_UpdateIteration(t *testing.T) {
	database := newTestDB(t)
	jobRepo := db.NewJobRepo(database)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	handler := NewRalphHandler(database, models.DefaultServerConfig(), "/tmp")

	handler.updateIteration(job, 5)
	assert.Equal(t, 5, job.Iteration)

	// Verify persisted
	fetched, _ := jobRepo.Get(job.ID)
	assert.Equal(t, 5, fetched.Iteration)
}
```

**Step 2: Write implementation**

Create `internal/executor/ralph.go`:

```go
package executor

import (
	"context"
	"fmt"
	"log"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// RalphHandler implements the ralph loop execution
type RalphHandler struct {
	db          *db.DB
	config      *models.ServerConfig
	repoManager *git.RepoManager
	executor    *ClaudeExecutor
	jobRepo     *db.JobRepo
	logRepo     *db.LogRepo
}

// NewRalphHandler creates a new ralph handler
func NewRalphHandler(database *db.DB, config *models.ServerConfig, workspaceDir string) *RalphHandler {
	return &RalphHandler{
		db:          database,
		config:      config,
		repoManager: git.NewRepoManager(workspaceDir),
		executor:    NewClaudeExecutor(config),
		jobRepo:     db.NewJobRepo(database),
		logRepo:     db.NewLogRepo(database),
	}
}

// Handle executes the ralph loop for a job
func (h *RalphHandler) Handle(ctx context.Context, job *models.Job) error {
	log.Printf("Starting ralph loop for job %d: %s", job.ID, job.Branch)

	// Setup workspace
	workDir, err := h.repoManager.Setup(ctx, job.ID, job.RepoURL, job.Branch)
	if err != nil {
		return fmt.Errorf("failed to setup workspace: %w", err)
	}

	// Determine working directory
	if job.WorkingDir != "" {
		workDir = workDir + "/" + job.WorkingDir
	}

	// Execute claude with the prompt
	result, err := h.executor.Execute(ctx, workDir, job.Prompt, job.Env, func(line string) {
		h.logRepo.Append(job.ID, job.Iteration, line)
	})

	if err != nil {
		return fmt.Errorf("claude execution failed: %w", err)
	}

	// Update iteration from output
	if result.Iterations > job.Iteration {
		h.updateIteration(job, result.Iterations)
	}

	// Check completion
	if result.Completed {
		log.Printf("Job %d completed successfully after %d iterations", job.ID, job.Iteration)
		return h.finalize(ctx, job, true)
	}

	// Check max iterations
	if job.HasReachedMaxIterations() {
		log.Printf("Job %d reached max iterations (%d)", job.ID, job.MaxIterations)
		return h.finalize(ctx, job, false)
	}

	// Continue running (scheduler will handle re-execution)
	return nil
}

func (h *RalphHandler) updateIteration(job *models.Job, iteration int) {
	job.Iteration = iteration
	if err := h.jobRepo.Update(job); err != nil {
		log.Printf("Failed to update job iteration: %v", err)
	}
}

func (h *RalphHandler) finalize(ctx context.Context, job *models.Job, success bool) error {
	workDir := h.repoManager.WorkspacePath(job.ID)
	if job.WorkingDir != "" {
		workDir = workDir + "/" + job.WorkingDir
	}

	// Commit any remaining changes
	hash, err := h.repoManager.Commit(ctx, workDir, fmt.Sprintf("Ralph iteration %d", job.Iteration))
	if err != nil {
		log.Printf("Warning: failed to commit final changes: %v", err)
	}
	if hash != "" {
		log.Printf("Final commit: %s", hash)
	}

	// Push and create PR
	prURL, err := h.repoManager.PushAndCreatePR(ctx, workDir, job.Branch, job.Iteration, success, "")
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}

	job.PRURL = prURL
	if err := h.jobRepo.Update(job); err != nil {
		log.Printf("Failed to update job with PR URL: %v", err)
	}

	log.Printf("Job %d PR created: %s", job.ID, prURL)
	return nil
}

func shouldContinue(job *models.Job) bool {
	return job.Iteration < job.MaxIterations
}
```

**Step 3: Run tests**

Run:
```bash
go test ./internal/executor/... -v
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/executor/ralph.go internal/executor/ralph_test.go
git commit -m "feat(executor): add ralph loop handler"
```

---

## Phase 6 Completion Checklist

- [ ] Claude Code executor with Ollama environment
- [ ] Output parsing for iterations and promises
- [ ] Ralph loop handler with workspace setup
- [ ] Commit and PR creation on completion
- [ ] Logging integration
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 7 - Web Dashboard
