# Anthropic API as First-Class Backend — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Anthropic API as an alternative backend to Ollama, selectable per-server (default) and per-job (override), so users can run jobs against cloud models.

**Architecture:** New `Backend` type controls whether `ClaudeExecutor.BuildEnv()` configures env vars for Ollama (existing behavior) or Anthropic API (real API key, no base URL override). Backend is resolved per-job: `job.Backend > server.DefaultBackend > "ollama"`. No new dependencies — just env var switching.

**Tech Stack:** Go, SQLite (existing), testify for assertions

---

### Task 1: Backend Type and AnthropicConfig in Models

**Files:**
- Modify: `internal/models/config.go:1-10` (add Backend type after imports)
- Modify: `internal/models/config.go:29-33` (add AnthropicConfig after OllamaConfig)
- Modify: `internal/models/config.go:44-64` (add fields to ServerConfig)
- Modify: `internal/models/config.go:67-79` (update DefaultServerConfig)
- Test: `internal/models/config_test.go`

**Step 1: Write failing tests for Backend type and AnthropicConfig**

Add to `internal/models/config_test.go`:

```go
func TestBackend_Valid(t *testing.T) {
	assert.True(t, models.BackendOllama.Valid())
	assert.True(t, models.BackendAnthropic.Valid())
	assert.False(t, models.Backend("gpt").Valid())
	assert.True(t, models.Backend("").Valid()) // empty = use default
}

func TestAnthropicConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		ac := models.AnthropicConfig{
			LargeModel: "claude-opus-4-5-20251101",
			SmallModel: "claude-haiku-4-5-20251001",
		}
		assert.NoError(t, ac.Validate())
	})

	t.Run("empty large model fails", func(t *testing.T) {
		ac := models.AnthropicConfig{LargeModel: "", SmallModel: "claude-haiku-4-5-20251001"}
		assert.Error(t, ac.Validate())
	})

	t.Run("empty small model fails", func(t *testing.T) {
		ac := models.AnthropicConfig{LargeModel: "claude-opus-4-5-20251101", SmallModel: ""}
		assert.Error(t, ac.Validate())
	})
}

func TestDefaultServerConfig_AnthropicDefaults(t *testing.T) {
	cfg := models.DefaultServerConfig()
	assert.Equal(t, models.BackendOllama, cfg.DefaultBackend)
	assert.Equal(t, "claude-opus-4-5-20251101", cfg.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-4-5-20251001", cfg.Anthropic.SmallModel)
	assert.Empty(t, cfg.Anthropic.APIKey)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestBackend_Valid|TestAnthropicConfig_Validate|TestDefaultServerConfig_AnthropicDefaults' ./internal/models/`
Expected: FAIL — `Backend` type not defined

**Step 3: Implement Backend type, AnthropicConfig, and update ServerConfig**

In `internal/models/config.go`, add after the imports:

```go
// Backend identifies which AI provider to use
type Backend string

const (
	BackendOllama    Backend = "ollama"
	BackendAnthropic Backend = "anthropic"
)

// Valid returns true for known backends and empty (which means "use default")
func (b Backend) Valid() bool {
	switch b {
	case "", BackendOllama, BackendAnthropic:
		return true
	default:
		return false
	}
}
```

Add after `OllamaConfig`:

```go
// AnthropicConfig holds settings for the Anthropic API backend
type AnthropicConfig struct {
	APIKey     string `json:"api_key,omitempty"`
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
}

// Validate checks that model names are set
func (ac *AnthropicConfig) Validate() error {
	if ac.LargeModel == "" {
		return fmt.Errorf("large_model is required")
	}
	if ac.SmallModel == "" {
		return fmt.Errorf("small_model is required")
	}
	return nil
}
```

Add two fields to `ServerConfig`:

```go
DefaultBackend Backend          `json:"default_backend"`
Anthropic      AnthropicConfig  `json:"anthropic"`
```

Update `DefaultServerConfig()` to include:

```go
DefaultBackend: BackendOllama,
Anthropic: AnthropicConfig{
	LargeModel: "claude-opus-4-5-20251101",
	SmallModel: "claude-haiku-4-5-20251001",
},
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run 'TestBackend_Valid|TestAnthropicConfig_Validate|TestDefaultServerConfig_AnthropicDefaults' ./internal/models/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat: add Backend type and AnthropicConfig to models"
```

---

### Task 2: ServerConfig Validate and Merge for New Fields

**Files:**
- Modify: `internal/models/config.go:82-101` (Validate — add anthropic validation)
- Modify: `internal/models/config.go:105-161` (Merge — add anthropic/backend merge)
- Modify: `internal/models/config.go:166-208` (MergeJSON — add anthropic handling)
- Test: `internal/models/config_test.go`

**Step 1: Write failing tests**

Add to `internal/models/config_test.go`:

```go
func TestServerConfig_Validate_Anthropic(t *testing.T) {
	t.Run("default config validates", func(t *testing.T) {
		cfg := models.DefaultServerConfig()
		assert.NoError(t, cfg.Validate())
	})

	t.Run("anthropic backend with empty large model fails", func(t *testing.T) {
		cfg := models.DefaultServerConfig()
		cfg.DefaultBackend = models.BackendAnthropic
		cfg.Anthropic.LargeModel = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("ollama backend skips anthropic validation", func(t *testing.T) {
		cfg := models.DefaultServerConfig()
		cfg.DefaultBackend = models.BackendOllama
		cfg.Anthropic.LargeModel = "" // doesn't matter for ollama
		assert.NoError(t, cfg.Validate())
	})

	t.Run("invalid backend fails", func(t *testing.T) {
		cfg := models.DefaultServerConfig()
		cfg.DefaultBackend = "gpt"
		assert.Error(t, cfg.Validate())
	})
}

func TestServerConfig_Merge_Backend(t *testing.T) {
	t.Run("merge updates default_backend", func(t *testing.T) {
		base := models.DefaultServerConfig()
		updates := &models.ServerConfig{DefaultBackend: models.BackendAnthropic}
		merged := base.Merge(updates)
		assert.Equal(t, models.BackendAnthropic, merged.DefaultBackend)
	})

	t.Run("empty backend preserves base", func(t *testing.T) {
		base := models.DefaultServerConfig()
		base.DefaultBackend = models.BackendAnthropic
		updates := &models.ServerConfig{}
		merged := base.Merge(updates)
		assert.Equal(t, models.BackendAnthropic, merged.DefaultBackend)
	})

	t.Run("merge updates anthropic config", func(t *testing.T) {
		base := models.DefaultServerConfig()
		updates := &models.ServerConfig{
			Anthropic: models.AnthropicConfig{
				LargeModel: "claude-sonnet-4-20250514",
			},
		}
		merged := base.Merge(updates)
		assert.Equal(t, "claude-sonnet-4-20250514", merged.Anthropic.LargeModel)
		assert.Equal(t, "claude-haiku-4-5-20251001", merged.Anthropic.SmallModel) // preserved
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestServerConfig_Validate_Anthropic|TestServerConfig_Merge_Backend' ./internal/models/`
Expected: FAIL

**Step 3: Implement validation and merge logic**

In `Validate()`, add before the return:

```go
if !c.DefaultBackend.Valid() {
	return fmt.Errorf("invalid default_backend: %q", c.DefaultBackend)
}
if c.DefaultBackend == BackendAnthropic {
	if err := c.Anthropic.Validate(); err != nil {
		return fmt.Errorf("anthropic: %w", err)
	}
}
```

In `Merge()`, add before the return:

```go
// DefaultBackend
if updates.DefaultBackend != "" {
	result.DefaultBackend = updates.DefaultBackend
}

// Anthropic: merge individual fields
if updates.Anthropic.APIKey != "" {
	result.Anthropic.APIKey = updates.Anthropic.APIKey
}
if updates.Anthropic.LargeModel != "" {
	result.Anthropic.LargeModel = updates.Anthropic.LargeModel
}
if updates.Anthropic.SmallModel != "" {
	result.Anthropic.SmallModel = updates.Anthropic.SmallModel
}
```

In `MergeJSON()`, add a case for `default_backend` and `anthropic`:

```go
if _, ok := rawMap["default_backend"]; ok {
	result.DefaultBackend = updates.DefaultBackend
}

if anthropicRaw, ok := rawMap["anthropic"]; ok {
	var anthropicMap map[string]json.RawMessage
	if err := json.Unmarshal(anthropicRaw, &anthropicMap); err == nil {
		if _, ok := anthropicMap["api_key"]; ok {
			result.Anthropic.APIKey = updates.Anthropic.APIKey
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run 'TestServerConfig_Validate_Anthropic|TestServerConfig_Merge_Backend' ./internal/models/`
Expected: PASS

**Step 5: Run all models tests to check for regressions**

Run: `go test -v ./internal/models/`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat: add Validate/Merge support for backend and anthropic config"
```

---

### Task 3: ConfigRepo Persistence for New Fields

**Files:**
- Modify: `internal/db/config.go:49-95` (Save — add anthropic + default_backend)
- Modify: `internal/db/config.go:124-190` (applyConfigValue — add cases)
- Test: `internal/db/config_test.go`

**Step 1: Write failing tests**

Add to `internal/db/config_test.go`:

```go
func TestConfigRepo_SaveAnthropic(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.DefaultBackend = models.BackendAnthropic
	cfg.Anthropic.APIKey = "sk-test-key"
	cfg.Anthropic.LargeModel = "claude-sonnet-4-20250514"
	cfg.Anthropic.SmallModel = "claude-haiku-4-5-20251001"

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, models.BackendAnthropic, fetched.DefaultBackend)
	assert.Equal(t, "sk-test-key", fetched.Anthropic.APIKey)
	assert.Equal(t, "claude-sonnet-4-20250514", fetched.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-4-5-20251001", fetched.Anthropic.SmallModel)
}

func TestConfigRepo_DefaultBackend_Defaults(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, models.BackendOllama, cfg.DefaultBackend)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestConfigRepo_SaveAnthropic|TestConfigRepo_DefaultBackend_Defaults' ./internal/db/`
Expected: FAIL — new fields not persisted

**Step 3: Implement persistence**

In `Save()`, add to the `values` map:

```go
"default_backend": string(cfg.DefaultBackend),
```

Add anthropic serialization:

```go
anthropicJSON, err := json.Marshal(cfg.Anthropic)
if err != nil {
	return fmt.Errorf("failed to marshal anthropic: %w", err)
}
```

And add to values map:

```go
"anthropic": string(anthropicJSON),
```

In `applyConfigValue()`, add cases:

```go
case "default_backend":
	cfg.DefaultBackend = models.Backend(value)
case "anthropic":
	var ac models.AnthropicConfig
	if err := json.Unmarshal([]byte(value), &ac); err != nil {
		return err
	}
	cfg.Anthropic = ac
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run 'TestConfigRepo_SaveAnthropic|TestConfigRepo_DefaultBackend_Defaults' ./internal/db/`
Expected: PASS

**Step 5: Run all db tests for regressions**

Run: `go test -v ./internal/db/`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/db/config.go internal/db/config_test.go
git commit -m "feat: persist backend and anthropic config in ConfigRepo"
```

---

### Task 4: Job.Backend Field

**Files:**
- Modify: `internal/models/job.go:9-39` (add Backend field to Job struct)
- Modify: `internal/models/job.go:63-80` (add backend validation to Validate)
- Modify: `internal/db/migrations/002_add_backend.sql` (new migration)
- Test: `internal/models/job_test.go` (or existing test file)

**Step 1: Write failing test**

Find or create test file. Add:

```go
func TestJob_Validate_Backend(t *testing.T) {
	t.Run("empty backend is valid (uses server default)", func(t *testing.T) {
		job := models.NewJob("https://github.com/foo/bar", "main", "fix bugs", 10)
		assert.NoError(t, job.Validate())
	})

	t.Run("ollama backend is valid", func(t *testing.T) {
		job := models.NewJob("https://github.com/foo/bar", "main", "fix bugs", 10)
		job.Backend = models.BackendOllama
		assert.NoError(t, job.Validate())
	})

	t.Run("anthropic backend is valid", func(t *testing.T) {
		job := models.NewJob("https://github.com/foo/bar", "main", "fix bugs", 10)
		job.Backend = models.BackendAnthropic
		assert.NoError(t, job.Validate())
	})

	t.Run("unknown backend fails", func(t *testing.T) {
		job := models.NewJob("https://github.com/foo/bar", "main", "fix bugs", 10)
		job.Backend = "gpt"
		assert.Error(t, job.Validate())
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestJob_Validate_Backend ./internal/models/`
Expected: FAIL — Backend field doesn't exist

**Step 3: Implement**

Add to `Job` struct:

```go
Backend Backend `json:"backend,omitempty"`
```

Add to `Validate()` before the return:

```go
if !j.Backend.Valid() {
	return fmt.Errorf("invalid backend: %q", j.Backend)
}
```

Create `internal/db/migrations/002_add_backend.sql`:

```sql
-- Add backend column to jobs table
ALTER TABLE jobs ADD COLUMN backend TEXT NOT NULL DEFAULT '';
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestJob_Validate_Backend ./internal/models/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/job.go internal/models/job_test.go internal/db/migrations/002_add_backend.sql
git commit -m "feat: add Backend field to Job model with migration"
```

---

### Task 5: Backend-Aware BuildEnv in ClaudeExecutor

**Files:**
- Modify: `internal/executor/claude.go:30-51` (BuildEnv signature + backend switch)
- Modify: `internal/executor/claude.go:74-78` (Execute call site)
- Test: `internal/executor/claude_test.go`

**Step 1: Write failing tests**

Add to `internal/executor/claude_test.go`:

```go
func TestClaudeExecutor_BuildEnv_Anthropic(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Anthropic.APIKey = "sk-test-key-123"
	cfg.Anthropic.LargeModel = "claude-opus-4-5-20251101"
	cfg.Anthropic.SmallModel = "claude-haiku-4-5-20251001"
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendAnthropic, nil)

	envMap := envToMap(env)
	assert.Equal(t, "sk-test-key-123", envMap["ANTHROPIC_API_KEY"])
	assert.Equal(t, "claude-opus-4-5-20251101", envMap["ANTHROPIC_MODEL"])
	assert.Equal(t, "claude-haiku-4-5-20251001", envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	// Must NOT set ANTHROPIC_BASE_URL for anthropic backend
	_, hasBaseURL := envMap["ANTHROPIC_BASE_URL"]
	assert.False(t, hasBaseURL)
}

func TestClaudeExecutor_BuildEnv_Ollama_Unchanged(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOllama, map[string]string{"CUSTOM": "value"})

	envMap := envToMap(env)
	assert.Equal(t, "http://localhost:11434", envMap["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "ollama", envMap["ANTHROPIC_AUTH_TOKEN"])
	assert.Equal(t, "", envMap["ANTHROPIC_API_KEY"])
	assert.Equal(t, "devstral", envMap["ANTHROPIC_MODEL"])
	assert.Equal(t, "qwen3:8b", envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	assert.Equal(t, "value", envMap["CUSTOM"])
}

func TestClaudeExecutor_BuildEnv_AnthropicKeyFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-env-key")
	cfg := models.DefaultServerConfig()
	cfg.Anthropic.APIKey = "sk-config-key"
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendAnthropic, nil)

	envMap := envToMap(env)
	// Env var takes precedence over config
	assert.Equal(t, "sk-env-key", envMap["ANTHROPIC_API_KEY"])
}

// envToMap converts env slice to map (last value wins)
func envToMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
```

Also add `"strings"` to the test file imports.

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestClaudeExecutor_BuildEnv_Anthropic|TestClaudeExecutor_BuildEnv_Ollama_Unchanged|TestClaudeExecutor_BuildEnv_AnthropicKeyFromEnv' ./internal/executor/`
Expected: FAIL — BuildEnv signature mismatch

**Step 3: Implement backend-aware BuildEnv**

Change `BuildEnv` signature and body in `internal/executor/claude.go`:

```go
// BuildEnv creates the environment variables for Claude Code
func (e *ClaudeExecutor) BuildEnv(backend models.Backend, extra map[string]string) []string {
	env := os.Environ()

	var backendEnv map[string]string

	switch backend {
	case models.BackendAnthropic:
		backendEnv = map[string]string{
			"ANTHROPIC_API_KEY":             e.resolveAnthropicKey(),
			"ANTHROPIC_MODEL":               e.config.Anthropic.LargeModel,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.Anthropic.SmallModel,
		}
	default: // ollama
		backendEnv = map[string]string{
			"ANTHROPIC_BASE_URL":            e.config.Ollama.Host,
			"ANTHROPIC_AUTH_TOKEN":          "ollama",
			"ANTHROPIC_API_KEY":             "",
			"ANTHROPIC_MODEL":               e.config.LargeModel.Name,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.SmallModel.Name,
		}
	}

	for k, v := range backendEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range extra {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// resolveAnthropicKey returns the API key, preferring env var over config
func (e *ClaudeExecutor) resolveAnthropicKey() string {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return key
	}
	return e.config.Anthropic.APIKey
}
```

Update `Execute()` to accept backend parameter. Change signature to:

```go
func (e *ClaudeExecutor) Execute(ctx context.Context, workDir, prompt string, backend models.Backend, env map[string]string, onOutput OutputCallback) (*ExecutionResult, error) {
```

And update the `BuildEnv` call inside:

```go
cmd.Env = e.BuildEnv(backend, env)
```

**Step 4: Fix existing tests that call BuildEnv with old signature**

Update existing tests to pass `models.BackendOllama` as first arg. For example:

```go
env := exec.BuildEnv(models.BackendOllama, map[string]string{"CUSTOM": "value"})
```

**Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/executor/`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/executor/claude.go internal/executor/claude_test.go
git commit -m "feat: make BuildEnv backend-aware with Anthropic support"
```

---

### Task 6: Backend Resolution in RalphHandler

**Files:**
- Modify: `internal/executor/ralph.go:37-85` (Handle — resolve backend, pass to Execute)
- Test: `internal/executor/ralph_test.go` (new or existing)

**Step 1: Write failing test for effectiveBackend**

Add to a test file in `internal/executor/`:

```go
func TestEffectiveBackend(t *testing.T) {
	t.Run("job backend takes precedence", func(t *testing.T) {
		result := effectiveBackend(models.BackendAnthropic, models.BackendOllama)
		assert.Equal(t, models.BackendAnthropic, result)
	})

	t.Run("falls back to server default", func(t *testing.T) {
		result := effectiveBackend("", models.BackendAnthropic)
		assert.Equal(t, models.BackendAnthropic, result)
	})

	t.Run("falls back to ollama when both empty", func(t *testing.T) {
		result := effectiveBackend("", "")
		assert.Equal(t, models.BackendOllama, result)
	})
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestEffectiveBackend ./internal/executor/`
Expected: FAIL — function not defined

**Step 3: Implement effectiveBackend and wire it into Handle**

Add to `internal/executor/ralph.go`:

```go
// effectiveBackend resolves which backend to use: job > server > ollama
func effectiveBackend(jobBackend, serverDefault models.Backend) models.Backend {
	if jobBackend != "" {
		return jobBackend
	}
	if serverDefault != "" {
		return serverDefault
	}
	return models.BackendOllama
}
```

In `Handle()`, before the `Execute` call, resolve the backend:

```go
backend := effectiveBackend(job.Backend, h.config.DefaultBackend)
```

Update the `Execute` call to pass `backend`:

```go
result, err := h.executor.Execute(ctx, workDir, job.Prompt, backend, job.Env, func(line string) {
```

**Step 4: Run tests**

Run: `go test -v ./internal/executor/`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/executor/ralph.go internal/executor/ralph_test.go
git commit -m "feat: resolve effective backend in RalphHandler"
```

---

### Task 7: API Validation for Backend Field

**Files:**
- Modify: `internal/api/jobs.go:16-24` (add Backend to CreateJobRequest)
- Modify: `internal/api/jobs.go:39-65` (handleCreateJob — set backend, validate)
- Test: `internal/api/jobs_test.go` (or create if needed)

**Step 1: Write failing tests**

Find or create `internal/api/jobs_test.go`. Add:

```go
func TestAPI_CreateJob_WithBackend(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10,"backend":"anthropic"}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var job models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &job))
	assert.Equal(t, models.BackendAnthropic, job.Backend)
}

func TestAPI_CreateJob_InvalidBackend(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10,"backend":"gpt"}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_CreateJob_EmptyBackend_UsesDefault(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var job models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &job))
	assert.Equal(t, models.Backend(""), job.Backend) // empty = use server default
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestAPI_CreateJob_WithBackend|TestAPI_CreateJob_InvalidBackend|TestAPI_CreateJob_EmptyBackend' ./internal/api/`
Expected: FAIL

**Step 3: Implement**

Add `Backend` field to `CreateJobRequest`:

```go
Backend string `json:"backend,omitempty"`
```

In `handleCreateJob`, after setting priority and before `Enqueue`:

```go
if req.Backend != "" {
	backend := models.Backend(req.Backend)
	if !backend.Valid() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid backend: %q", req.Backend))
		return
	}
	job.Backend = backend
}
```

Add `"fmt"` to the imports if not already present.

**Step 4: Run tests**

Run: `go test -v ./internal/api/`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/api/jobs.go internal/api/jobs_test.go
git commit -m "feat: validate backend field in job creation API"
```

---

### Task 8: API Config Tests for Anthropic Fields

**Files:**
- Test: `internal/api/config_test.go`

**Step 1: Write tests for anthropic config round-trip via API**

Add to `internal/api/config_test.go`:

```go
func TestAPI_ConfigRoundTrip_Anthropic(t *testing.T) {
	srv, _ := newTestServer(t)

	body := []byte(`{"default_backend":"anthropic","anthropic":{"api_key":"sk-test","large_model":"claude-sonnet-4-20250514","small_model":"claude-haiku-4-5-20251001"}}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET and verify
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.BackendAnthropic, resp.DefaultBackend)
	assert.Equal(t, "sk-test", resp.Anthropic.APIKey)
	assert.Equal(t, "claude-sonnet-4-20250514", resp.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-4-5-20251001", resp.Anthropic.SmallModel)
}

func TestAPI_GetConfig_IncludesAnthropicDefaults(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.BackendOllama, resp.DefaultBackend)
	assert.Equal(t, "claude-opus-4-5-20251101", resp.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-4-5-20251001", resp.Anthropic.SmallModel)
}
```

**Step 2: Run tests**

Run: `go test -v -run 'TestAPI_ConfigRoundTrip_Anthropic|TestAPI_GetConfig_IncludesAnthropicDefaults' ./internal/api/`
Expected: PASS (if Tasks 1-3 are done correctly, these should pass with no new production code)

**Step 3: Commit**

```bash
git add internal/api/config_test.go
git commit -m "test: add API config tests for anthropic backend fields"
```

---

### Task 9: CLI --backend Flag

**Files:**
- Modify: `cmd/cli/commands.go` (submitCmd — add --backend flag)
- Modify: `internal/cli/client.go` (if CreateJobRequest is defined there too, add Backend field)

**Step 1: Check where CreateJobRequest is used in CLI**

The CLI likely sends a request struct to the API. Look at `cmd/cli/commands.go` `submitCmd()` and the client wrapper.

**Step 2: Add --backend flag to submit command**

In the submit command definition, add:

```go
cmd.Flags().StringVar(&backend, "backend", "", "Backend to use: ollama or anthropic (default: server setting)")
```

In the submit handler, pass it to the request:

```go
if backend != "" {
	reqBody.Backend = backend
}
```

Update the CLI's request struct (wherever it's defined) to include:

```go
Backend string `json:"backend,omitempty"`
```

**Step 3: Run full test suite**

Run: `make test`
Expected: All PASS

**Step 4: Commit**

```bash
git add cmd/cli/commands.go internal/cli/client.go
git commit -m "feat: add --backend flag to CLI submit command"
```

---

### Task 10: Full Integration Smoke Test

**Files:**
- Run full test suite and lint

**Step 1: Run all tests**

Run: `make test`
Expected: All PASS

**Step 2: Run linter**

Run: `make lint`
Expected: No errors

**Step 3: Build**

Run: `make build`
Expected: Builds successfully

**Step 4: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: cleanup after anthropic backend integration"
```
