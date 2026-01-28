# Flexible Model Selection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hardcoded Ollama model configuration with smart model selection based on hardware detection, support local and remote Ollama, and allow per-model device placement (GPU vs CPU).

**Architecture:** New types `ModelPlacement` and `OllamaConfig` replace string model fields in `ServerConfig`. A model catalog (`models.yaml`) embedded in the binary defines available models. A selection algorithm scores all valid (large, small) pairings across detected hardware (RAM, GPU VRAM) and recommends optimal placement. An Ollama REST client manages model lifecycle. The installer is updated to present the ideal config and allow customization.

**Tech Stack:** Go 1.22+, testify, `//go:embed` for models.yaml, `gopkg.in/yaml.v3` for catalog parsing, `net/http` for Ollama API client.

---

## Task 1: Add YAML Dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add gopkg.in/yaml.v3**

Run:
```bash
go get gopkg.in/yaml.v3
```

Expected: `go.mod` updated with yaml.v3 dependency

**Step 2: Tidy modules**

Run:
```bash
go mod tidy
```

Expected: `go.sum` updated

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add yaml.v3 for model catalog parsing"
```

---

## Task 2: Update ServerConfig with ModelPlacement and OllamaConfig

**Files:**
- Modify: `internal/models/config.go`
- Modify: `internal/models/config_test.go`

**Step 1: Write the failing tests**

Replace the entire contents of `internal/models/config_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPlacement_Validate(t *testing.T) {
	t.Run("valid placement passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "qwen3-coder:70b", Device: "cpu", MemoryGB: 42}
		assert.NoError(t, mp.Validate())
	})

	t.Run("empty name fails", func(t *testing.T) {
		mp := ModelPlacement{Name: "", Device: "cpu", MemoryGB: 42}
		assert.Error(t, mp.Validate())
	})

	t.Run("invalid device fails", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "tpu", MemoryGB: 5}
		assert.Error(t, mp.Validate())
	})

	t.Run("gpu device passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "gpu", MemoryGB: 5}
		assert.NoError(t, mp.Validate())
	})

	t.Run("auto device passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "auto", MemoryGB: 5}
		assert.NoError(t, mp.Validate())
	})
}

func TestOllamaConfig_Validate(t *testing.T) {
	t.Run("valid config passes", func(t *testing.T) {
		oc := OllamaConfig{Host: "http://localhost:11434", IsRemote: false}
		assert.NoError(t, oc.Validate())
	})

	t.Run("empty host fails", func(t *testing.T) {
		oc := OllamaConfig{Host: "", IsRemote: false}
		assert.Error(t, oc.Validate())
	})

	t.Run("remote config passes", func(t *testing.T) {
		oc := OllamaConfig{Host: "http://192.168.1.50:11434", IsRemote: true}
		assert.NoError(t, oc.Validate())
	})
}

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	assert.Equal(t, "http://localhost:11434", cfg.Ollama.Host)
	assert.False(t, cfg.Ollama.IsRemote)
	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel.Name)
	assert.Equal(t, "cpu", cfg.LargeModel.Device)
	assert.Equal(t, 42.0, cfg.LargeModel.MemoryGB)
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel.Name)
	assert.Equal(t, "gpu", cfg.SmallModel.Device)
	assert.Equal(t, 5.0, cfg.SmallModel.MemoryGB)
	assert.Equal(t, 50, cfg.DefaultMaxIterations)
	assert.Equal(t, 1, cfg.ConcurrentJobs)
	assert.Equal(t, 30, cfg.JobRetentionDays)
	assert.Equal(t, 3, cfg.MaxClaudeRetries)
	assert.Equal(t, 3, cfg.MaxGitRetries)
	assert.Equal(t, 1000, cfg.GitRetryBackoffMs)
}

func TestServerConfig_Validate(t *testing.T) {
	validConfig := func() *ServerConfig {
		return DefaultServerConfig()
	}

	t.Run("valid config passes", func(t *testing.T) {
		cfg := validConfig()
		assert.NoError(t, cfg.Validate())
	})

	t.Run("empty large_model name fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.LargeModel.Name = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("empty small_model name fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.SmallModel.Name = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("invalid large_model device fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.LargeModel.Device = "tpu"
		assert.Error(t, cfg.Validate())
	})

	t.Run("empty ollama host fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.Ollama.Host = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero default_max_iterations fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.DefaultMaxIterations = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero concurrent_jobs fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.ConcurrentJobs = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("negative job_retention_days fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.JobRetentionDays = -1
		assert.Error(t, cfg.Validate())
	})
}

func TestServerConfig_Merge(t *testing.T) {
	t.Run("merge updates model name without clobbering device", func(t *testing.T) {
		base := DefaultServerConfig()
		base.LargeModel.Name = "original-model"
		base.ConcurrentJobs = 2

		updates := &ServerConfig{
			LargeModel: ModelPlacement{Name: "new-model"},
		}

		merged := base.Merge(updates)

		assert.Equal(t, "new-model", merged.LargeModel.Name)
		assert.Equal(t, "cpu", merged.LargeModel.Device) // Preserved from base
		assert.Equal(t, base.SmallModel, merged.SmallModel)
		assert.Equal(t, 2, merged.ConcurrentJobs) // Unchanged
	})

	t.Run("merge updates ollama host without clobbering IsRemote", func(t *testing.T) {
		base := DefaultServerConfig()
		base.Ollama.IsRemote = true

		updates := &ServerConfig{
			Ollama: OllamaConfig{Host: "http://192.168.1.50:11434"},
		}

		merged := base.Merge(updates)

		assert.Equal(t, "http://192.168.1.50:11434", merged.Ollama.Host)
		assert.True(t, merged.Ollama.IsRemote) // Preserved from base
	})

	t.Run("zero-value ModelPlacement changes nothing", func(t *testing.T) {
		base := DefaultServerConfig()
		updates := &ServerConfig{}

		merged := base.Merge(updates)

		assert.Equal(t, base.LargeModel, merged.LargeModel)
		assert.Equal(t, base.SmallModel, merged.SmallModel)
		assert.Equal(t, base.Ollama, merged.Ollama)
	})
}

func TestServerConfig_JSON(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.LargeModel.Name = "test-model"
	cfg.Ollama.Host = "http://remote:11434"
	cfg.Ollama.IsRemote = true

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ServerConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, cfg.LargeModel.Name, decoded.LargeModel.Name)
	assert.Equal(t, cfg.LargeModel.Device, decoded.LargeModel.Device)
	assert.Equal(t, cfg.LargeModel.MemoryGB, decoded.LargeModel.MemoryGB)
	assert.Equal(t, cfg.SmallModel.Name, decoded.SmallModel.Name)
	assert.Equal(t, cfg.Ollama.Host, decoded.Ollama.Host)
	assert.Equal(t, cfg.Ollama.IsRemote, decoded.Ollama.IsRemote)
	assert.Equal(t, cfg.DefaultMaxIterations, decoded.DefaultMaxIterations)
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/models/... -v -run "TestModelPlacement|TestOllamaConfig|TestDefaultServer|TestServerConfig"
```

Expected: FAIL - types not defined

**Step 3: Write the implementation**

Replace the entire contents of `internal/models/config.go`:

```go
package models

import "fmt"

// ModelPlacement describes a model and where it should run
type ModelPlacement struct {
	Name     string  `json:"name"`      // e.g. "qwen3-coder:70b"
	Device   string  `json:"device"`    // "gpu", "cpu", or "auto"
	MemoryGB float64 `json:"memory_gb"` // expected memory footprint
}

// Validate checks if the model placement has valid values
func (m ModelPlacement) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("model name is required")
	}
	switch m.Device {
	case "gpu", "cpu", "auto", "":
		// valid
	default:
		return fmt.Errorf("invalid device %q: must be gpu, cpu, or auto", m.Device)
	}
	return nil
}

// OllamaConfig describes how to connect to Ollama
type OllamaConfig struct {
	Host     string `json:"host"`      // e.g. "http://localhost:11434"
	IsRemote bool   `json:"is_remote"` // true = skip local install/management
}

// Validate checks if the Ollama config has valid values
func (o OllamaConfig) Validate() error {
	if o.Host == "" {
		return fmt.Errorf("ollama host is required")
	}
	return nil
}

// ServerConfig holds server-wide configuration
type ServerConfig struct {
	// Ollama connection
	Ollama OllamaConfig `json:"ollama"`

	// Models
	LargeModel ModelPlacement `json:"large_model"`
	SmallModel ModelPlacement `json:"small_model"`

	// Execution
	DefaultMaxIterations int `json:"default_max_iterations"`
	ConcurrentJobs       int `json:"concurrent_jobs"`

	// Storage
	WorkspaceDir     string `json:"workspace_dir"`
	JobRetentionDays int    `json:"job_retention_days"`

	// Retry behavior
	MaxClaudeRetries  int `json:"max_claude_retries"`
	MaxGitRetries     int `json:"max_git_retries"`
	GitRetryBackoffMs int `json:"git_retry_backoff_ms"`
}

// DefaultServerConfig returns a ServerConfig with sensible defaults
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Ollama: OllamaConfig{
			Host:     "http://localhost:11434",
			IsRemote: false,
		},
		LargeModel: ModelPlacement{
			Name:     "qwen3-coder:70b",
			Device:   "cpu",
			MemoryGB: 42,
		},
		SmallModel: ModelPlacement{
			Name:     "qwen2.5-coder:7b",
			Device:   "gpu",
			MemoryGB: 5,
		},
		DefaultMaxIterations: 50,
		ConcurrentJobs:       1,
		JobRetentionDays:     30,
		MaxClaudeRetries:     3,
		MaxGitRetries:        3,
		GitRetryBackoffMs:    1000,
	}
}

// Validate checks if the config has valid values
func (c *ServerConfig) Validate() error {
	if err := c.Ollama.Validate(); err != nil {
		return fmt.Errorf("ollama config: %w", err)
	}
	if err := c.LargeModel.Validate(); err != nil {
		return fmt.Errorf("large_model: %w", err)
	}
	if err := c.SmallModel.Validate(); err != nil {
		return fmt.Errorf("small_model: %w", err)
	}
	if c.DefaultMaxIterations <= 0 {
		return fmt.Errorf("default_max_iterations must be positive")
	}
	if c.ConcurrentJobs <= 0 {
		return fmt.Errorf("concurrent_jobs must be positive")
	}
	if c.JobRetentionDays < 0 {
		return fmt.Errorf("job_retention_days cannot be negative")
	}
	return nil
}

// Merge returns a new config with non-zero values from updates applied
func (c *ServerConfig) Merge(updates *ServerConfig) *ServerConfig {
	result := *c // Copy

	// Ollama
	if updates.Ollama.Host != "" {
		result.Ollama.Host = updates.Ollama.Host
	}
	// Note: IsRemote is preserved from base unless Ollama.Host changes
	// To update IsRemote, the caller sets it explicitly after merge

	// Models - only update if Name is non-empty (partial update)
	if updates.LargeModel.Name != "" {
		result.LargeModel.Name = updates.LargeModel.Name
		if updates.LargeModel.Device != "" {
			result.LargeModel.Device = updates.LargeModel.Device
		}
		if updates.LargeModel.MemoryGB > 0 {
			result.LargeModel.MemoryGB = updates.LargeModel.MemoryGB
		}
	}
	if updates.SmallModel.Name != "" {
		result.SmallModel.Name = updates.SmallModel.Name
		if updates.SmallModel.Device != "" {
			result.SmallModel.Device = updates.SmallModel.Device
		}
		if updates.SmallModel.MemoryGB > 0 {
			result.SmallModel.MemoryGB = updates.SmallModel.MemoryGB
		}
	}

	if updates.DefaultMaxIterations > 0 {
		result.DefaultMaxIterations = updates.DefaultMaxIterations
	}
	if updates.ConcurrentJobs > 0 {
		result.ConcurrentJobs = updates.ConcurrentJobs
	}
	if updates.WorkspaceDir != "" {
		result.WorkspaceDir = updates.WorkspaceDir
	}
	if updates.JobRetentionDays > 0 {
		result.JobRetentionDays = updates.JobRetentionDays
	}
	if updates.MaxClaudeRetries > 0 {
		result.MaxClaudeRetries = updates.MaxClaudeRetries
	}
	if updates.MaxGitRetries > 0 {
		result.MaxGitRetries = updates.MaxGitRetries
	}
	if updates.GitRetryBackoffMs > 0 {
		result.GitRetryBackoffMs = updates.GitRetryBackoffMs
	}

	return &result
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/models/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat(models): replace string model fields with ModelPlacement and OllamaConfig"
```

---

## Task 3: Fix All Compilation Errors from ServerConfig Change

This is a breaking change. Every file referencing `config.LargeModel` as a string must update to `config.LargeModel.Name`. This task fixes all downstream code.

**Files:**
- Modify: `internal/executor/claude.go:38-39`
- Modify: `internal/executor/claude_test.go`
- Modify: `internal/api/config.go:40-44`
- Modify: `internal/api/config_test.go`
- Modify: `internal/db/config.go:50-51,110-113`
- Modify: `internal/db/config_test.go`
- Modify: `cmd/cli/commands.go:312-313`

**Step 1: Fix executor/claude.go**

In `internal/executor/claude.go`, change `BuildEnv` (lines 34-39) from:

```go
	ollamaEnv := map[string]string{
		"ANTHROPIC_BASE_URL":            "http://localhost:11434",
		"ANTHROPIC_AUTH_TOKEN":          "ollama",
		"ANTHROPIC_API_KEY":             "",
		"ANTHROPIC_MODEL":               e.config.LargeModel,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.SmallModel,
	}
```

To:

```go
	ollamaEnv := map[string]string{
		"ANTHROPIC_BASE_URL":            e.config.Ollama.Host,
		"ANTHROPIC_AUTH_TOKEN":          "ollama",
		"ANTHROPIC_API_KEY":             "",
		"ANTHROPIC_MODEL":               e.config.LargeModel.Name,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.SmallModel.Name,
	}
```

**Step 2: Fix executor/claude_test.go**

In `internal/executor/claude_test.go`, change `TestClaudeExecutor_BuildEnv` from:

```go
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
```

To:

```go
func TestClaudeExecutor_BuildEnv(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(map[string]string{"CUSTOM": "value"})

	// Should contain Ollama config from ServerConfig
	assert.Contains(t, env, "ANTHROPIC_BASE_URL=http://localhost:11434")
	assert.Contains(t, env, "ANTHROPIC_AUTH_TOKEN=ollama")
	assert.Contains(t, env, "ANTHROPIC_API_KEY=")
	assert.Contains(t, env, "ANTHROPIC_MODEL=qwen3-coder:70b")
	assert.Contains(t, env, "ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen2.5-coder:7b")
	assert.Contains(t, env, "CUSTOM=value")
}

func TestClaudeExecutor_BuildEnv_RemoteOllama(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Ollama.Host = "http://192.168.1.50:11434"
	cfg.Ollama.IsRemote = true
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(nil)

	assert.Contains(t, env, "ANTHROPIC_BASE_URL=http://192.168.1.50:11434")
}
```

**Step 3: Fix db/config.go**

Replace the entire contents of `internal/db/config.go`:

```go
package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ConfigRepo handles config persistence
type ConfigRepo struct {
	db *DB
}

// NewConfigRepo creates a new config repository
func NewConfigRepo(db *DB) *ConfigRepo {
	return &ConfigRepo{db: db}
}

// Get retrieves the current config (or defaults if not set)
func (r *ConfigRepo) Get() (*models.ServerConfig, error) {
	cfg := models.DefaultServerConfig()

	rows, err := r.db.conn.Query("SELECT key, value FROM config")
	if err != nil {
		return nil, fmt.Errorf("failed to query config: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan config row: %w", err)
		}

		if err := applyConfigValue(cfg, key, value); err != nil {
			// Log but don't fail on unknown keys
			continue
		}
	}

	return cfg, nil
}

// Save persists the entire config
func (r *ConfigRepo) Save(cfg *models.ServerConfig) error {
	// Serialize structured fields as JSON
	largeModelJSON, err := json.Marshal(cfg.LargeModel)
	if err != nil {
		return fmt.Errorf("failed to marshal large_model: %w", err)
	}
	smallModelJSON, err := json.Marshal(cfg.SmallModel)
	if err != nil {
		return fmt.Errorf("failed to marshal small_model: %w", err)
	}
	ollamaJSON, err := json.Marshal(cfg.Ollama)
	if err != nil {
		return fmt.Errorf("failed to marshal ollama: %w", err)
	}

	values := map[string]string{
		"large_model":            string(largeModelJSON),
		"small_model":            string(smallModelJSON),
		"ollama":                 string(ollamaJSON),
		"default_max_iterations": strconv.Itoa(cfg.DefaultMaxIterations),
		"concurrent_jobs":        strconv.Itoa(cfg.ConcurrentJobs),
		"workspace_dir":          cfg.WorkspaceDir,
		"job_retention_days":     strconv.Itoa(cfg.JobRetentionDays),
		"max_claude_retries":     strconv.Itoa(cfg.MaxClaudeRetries),
		"max_git_retries":        strconv.Itoa(cfg.MaxGitRetries),
		"git_retry_backoff_ms":   strconv.Itoa(cfg.GitRetryBackoffMs),
	}

	tx, err := r.db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for key, value := range values {
		_, err := tx.Exec(`
			INSERT INTO config (key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
		`, key, value, value)
		if err != nil {
			return fmt.Errorf("failed to save config key %s: %w", key, err)
		}
	}

	return tx.Commit()
}

// Update sets a single config value
func (r *ConfigRepo) Update(key, value string) error {
	_, err := r.db.conn.Exec(`
		INSERT INTO config (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
	`, key, value, value)
	if err != nil {
		return fmt.Errorf("failed to update config key %s: %w", key, err)
	}
	return nil
}

// GetKey retrieves a single config value
func (r *ConfigRepo) GetKey(key string) (string, error) {
	var value string
	err := r.db.conn.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get config key %s: %w", key, err)
	}
	return value, nil
}

// applyConfigValue sets a config field from a string value
func applyConfigValue(cfg *models.ServerConfig, key, value string) error {
	switch key {
	case "large_model":
		var mp models.ModelPlacement
		if err := json.Unmarshal([]byte(value), &mp); err != nil {
			// Backwards compatibility: treat as plain model name
			cfg.LargeModel.Name = value
			return nil
		}
		cfg.LargeModel = mp
	case "small_model":
		var mp models.ModelPlacement
		if err := json.Unmarshal([]byte(value), &mp); err != nil {
			// Backwards compatibility: treat as plain model name
			cfg.SmallModel.Name = value
			return nil
		}
		cfg.SmallModel = mp
	case "ollama":
		var oc models.OllamaConfig
		if err := json.Unmarshal([]byte(value), &oc); err != nil {
			return err
		}
		cfg.Ollama = oc
	case "default_max_iterations":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.DefaultMaxIterations = v
	case "concurrent_jobs":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.ConcurrentJobs = v
	case "workspace_dir":
		cfg.WorkspaceDir = value
	case "job_retention_days":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.JobRetentionDays = v
	case "max_claude_retries":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.MaxClaudeRetries = v
	case "max_git_retries":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.MaxGitRetries = v
	case "git_retry_backoff_ms":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.GitRetryBackoffMs = v
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}
```

**Step 4: Fix db/config_test.go**

Replace the entire contents of `internal/db/config_test.go`:

```go
package db

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigRepo_GetDefault(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg, err := repo.Get()
	require.NoError(t, err)

	// Should return defaults when no config exists
	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel.Name)
	assert.Equal(t, "cpu", cfg.LargeModel.Device)
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel.Name)
	assert.Equal(t, "gpu", cfg.SmallModel.Device)
	assert.Equal(t, "http://localhost:11434", cfg.Ollama.Host)
	assert.False(t, cfg.Ollama.IsRemote)
}

func TestConfigRepo_Save(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel.Name = "custom-model:latest"
	cfg.LargeModel.Device = "gpu"
	cfg.LargeModel.MemoryGB = 20
	cfg.ConcurrentJobs = 5

	err := repo.Save(cfg)
	require.NoError(t, err)

	// Fetch and verify
	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "custom-model:latest", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
	assert.Equal(t, 20.0, fetched.LargeModel.MemoryGB)
	assert.Equal(t, 5, fetched.ConcurrentJobs)
}

func TestConfigRepo_SaveOllama(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.Ollama.Host = "http://192.168.1.50:11434"
	cfg.Ollama.IsRemote = true

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "http://192.168.1.50:11434", fetched.Ollama.Host)
	assert.True(t, fetched.Ollama.IsRemote)
}

func TestConfigRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Save initial config
	cfg := models.DefaultServerConfig()
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Update specific field using JSON
	err = repo.Update("large_model", `{"name":"updated-model","device":"gpu","memory_gb":20}`)
	require.NoError(t, err)

	// Verify
	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "updated-model", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
}

func TestConfigRepo_GetKey(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel.Name = "test-model"
	err := repo.Save(cfg)
	require.NoError(t, err)

	value, err := repo.GetKey("large_model")
	require.NoError(t, err)
	assert.Contains(t, value, "test-model")
}

func TestConfigRepo_GetKey_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	_, err := repo.GetKey("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConfigRepo_BackwardsCompat(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Simulate old-format data: plain string model name
	err := repo.Update("large_model", "old-plain-model-name")
	require.NoError(t, err)

	cfg, err := repo.Get()
	require.NoError(t, err)
	// Should parse as plain name, keeping default device
	assert.Equal(t, "old-plain-model-name", cfg.LargeModel.Name)
}
```

**Step 5: Fix api/config.go**

Replace the entire contents of `internal/api/config.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	cfg, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	// Get current config
	current, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse updates as a ServerConfig (partial)
	var updates models.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates via merge
	merged := current.Merge(&updates)

	// Validate
	if err := merged.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Save
	if err := configRepo.Save(merged); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, merged)
}
```

**Step 6: Fix api/config_test.go**

Replace the entire contents of `internal/api/config_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_GetConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Should return defaults
	assert.Equal(t, "qwen3-coder:70b", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device)
	assert.Equal(t, "http://localhost:11434", resp.Ollama.Host)
}

func TestAPI_UpdateConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := models.ServerConfig{
		LargeModel:     models.ModelPlacement{Name: "custom-model:latest"},
		ConcurrentJobs: 3,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "custom-model:latest", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device) // Preserved from default
	assert.Equal(t, 3, resp.ConcurrentJobs)
}
```

**Step 7: Fix cmd/cli/commands.go**

In `cmd/cli/commands.go`, find the `server-config` display block (around line 312) and change:

```go
				fmt.Printf("large_model: %s\n", serverCfg.LargeModel)
				fmt.Printf("small_model: %s\n", serverCfg.SmallModel)
```

To:

```go
				fmt.Printf("ollama_host: %s (remote: %v)\n", serverCfg.Ollama.Host, serverCfg.Ollama.IsRemote)
				fmt.Printf("large_model: %s (device: %s, %.1fGB)\n", serverCfg.LargeModel.Name, serverCfg.LargeModel.Device, serverCfg.LargeModel.MemoryGB)
				fmt.Printf("small_model: %s (device: %s, %.1fGB)\n", serverCfg.SmallModel.Name, serverCfg.SmallModel.Device, serverCfg.SmallModel.MemoryGB)
```

**Step 8: Run all tests to verify everything compiles and passes**

Run:
```bash
go test ./... -short -v 2>&1 | tail -30
```

Expected: ALL PASS

**Step 9: Commit**

```bash
git add internal/executor/claude.go internal/executor/claude_test.go internal/api/config.go internal/api/config_test.go internal/db/config.go internal/db/config_test.go cmd/cli/commands.go
git commit -m "refactor: update all code for ModelPlacement and OllamaConfig types"
```

---

## Task 4: Create Model Catalog (models.yaml)

**Files:**
- Create: `models.yaml`
- Create: `internal/platform/catalog.go`
- Create: `internal/platform/catalog_test.go`

**Step 1: Create models.yaml**

Create `models.yaml` at the repo root:

```yaml
# models.yaml - Recommended coding models for ralph-o-matic
# quality: relative ranking (higher = better coding performance)
# role: "large" (primary model), "small" (helper), or "both"
models:
  - name: "qwen3-coder:70b"
    memory_gb: 42
    role: large
    quality: 10
    description: "Best coding performance, needs ~42GB"

  - name: "qwen2.5-coder:32b"
    memory_gb: 20
    role: large
    quality: 8
    description: "Strong coding, much smaller footprint"

  - name: "qwen2.5-coder:14b"
    memory_gb: 10
    role: large
    quality: 6
    description: "Good coding capability"

  - name: "qwen2.5-coder:7b"
    memory_gb: 5
    role: both
    quality: 4
    description: "Decent coding, fast inference"

  - name: "qwen2.5-coder:1.5b"
    memory_gb: 1.5
    role: small
    quality: 2
    description: "Lightweight helper only"
```

**Step 2: Write the failing tests**

Create `internal/platform/catalog_test.go`:

```go
package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEmbeddedCatalog(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(catalog.Models), 5)

	// Verify first model
	assert.Equal(t, "qwen3-coder:70b", catalog.Models[0].Name)
	assert.Equal(t, 42.0, catalog.Models[0].MemoryGB)
	assert.Equal(t, "large", catalog.Models[0].Role)
	assert.Equal(t, 10, catalog.Models[0].Quality)
}

func TestLoadCatalogFromBytes(t *testing.T) {
	yaml := []byte(`
models:
  - name: "test-model:7b"
    memory_gb: 5
    role: both
    quality: 4
    description: "Test model"
`)
	catalog, err := LoadCatalogFromBytes(yaml)
	require.NoError(t, err)
	assert.Len(t, catalog.Models, 1)
	assert.Equal(t, "test-model:7b", catalog.Models[0].Name)
}

func TestLoadCatalogFromBytes_Invalid(t *testing.T) {
	t.Run("malformed yaml", func(t *testing.T) {
		_, err := LoadCatalogFromBytes([]byte("not: [valid: yaml"))
		assert.Error(t, err)
	})

	t.Run("empty models list", func(t *testing.T) {
		_, err := LoadCatalogFromBytes([]byte("models: []"))
		assert.Error(t, err)
	})

	t.Run("missing name", func(t *testing.T) {
		yaml := []byte(`
models:
  - memory_gb: 5
    role: both
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})

	t.Run("missing memory_gb", func(t *testing.T) {
		yaml := []byte(`
models:
  - name: "test:7b"
    role: both
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})

	t.Run("missing role", func(t *testing.T) {
		yaml := []byte(`
models:
  - name: "test:7b"
    memory_gb: 5
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})

	t.Run("duplicate model names", func(t *testing.T) {
		yaml := []byte(`
models:
  - name: "test:7b"
    memory_gb: 5
    role: both
    quality: 4
  - name: "test:7b"
    memory_gb: 5
    role: both
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})
}

func TestCatalog_LargeModels(t *testing.T) {
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "a", Role: "large", Quality: 10, MemoryGB: 42},
			{Name: "b", Role: "small", Quality: 2, MemoryGB: 1.5},
			{Name: "c", Role: "both", Quality: 4, MemoryGB: 5},
		},
	}

	large := catalog.LargeModels()
	assert.Len(t, large, 2) // "a" (large) and "c" (both)
	assert.Equal(t, "a", large[0].Name)
	assert.Equal(t, "c", large[1].Name)
}

func TestCatalog_SmallModels(t *testing.T) {
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "a", Role: "large", Quality: 10, MemoryGB: 42},
			{Name: "b", Role: "small", Quality: 2, MemoryGB: 1.5},
			{Name: "c", Role: "both", Quality: 4, MemoryGB: 5},
		},
	}

	small := catalog.SmallModels()
	assert.Len(t, small, 2) // "b" (small) and "c" (both)
	assert.Equal(t, "b", small[0].Name)
	assert.Equal(t, "c", small[1].Name)
}
```

**Step 3: Run tests to verify they fail**

Run:
```bash
go test ./internal/platform/... -v
```

Expected: FAIL - types not defined

**Step 4: Write the implementation**

Create `internal/platform/catalog.go`:

```go
package platform

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed catalog_embed.yaml
var embeddedCatalog []byte

// CatalogModel describes a model available in Ollama
type CatalogModel struct {
	Name        string  `yaml:"name"`
	MemoryGB    float64 `yaml:"memory_gb"`
	Role        string  `yaml:"role"` // "large", "small", or "both"
	Quality     int     `yaml:"quality"`
	Description string  `yaml:"description"`
}

// Catalog holds the list of recommended models
type Catalog struct {
	Models []CatalogModel `yaml:"models"`
}

// LoadEmbeddedCatalog loads the built-in model catalog
func LoadEmbeddedCatalog() (*Catalog, error) {
	return LoadCatalogFromBytes(embeddedCatalog)
}

// LoadCatalogFromBytes parses a YAML byte slice into a Catalog
func LoadCatalogFromBytes(data []byte) (*Catalog, error) {
	var catalog Catalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("failed to parse model catalog: %w", err)
	}

	if err := catalog.validate(); err != nil {
		return nil, err
	}

	return &catalog, nil
}

func (c *Catalog) validate() error {
	if len(c.Models) == 0 {
		return fmt.Errorf("model catalog is empty")
	}

	seen := make(map[string]bool)
	for i, m := range c.Models {
		if m.Name == "" {
			return fmt.Errorf("model at index %d: name is required", i)
		}
		if m.MemoryGB <= 0 {
			return fmt.Errorf("model %q: memory_gb must be positive", m.Name)
		}
		if m.Role == "" {
			return fmt.Errorf("model %q: role is required", m.Name)
		}
		if m.Role != "large" && m.Role != "small" && m.Role != "both" {
			return fmt.Errorf("model %q: role must be large, small, or both", m.Name)
		}
		if seen[m.Name] {
			return fmt.Errorf("duplicate model name: %q", m.Name)
		}
		seen[m.Name] = true
	}

	return nil
}

// LargeModels returns models that can serve as the large (primary) model
func (c *Catalog) LargeModels() []CatalogModel {
	var result []CatalogModel
	for _, m := range c.Models {
		if m.Role == "large" || m.Role == "both" {
			result = append(result, m)
		}
	}
	return result
}

// SmallModels returns models that can serve as the small (helper) model
func (c *Catalog) SmallModels() []CatalogModel {
	var result []CatalogModel
	for _, m := range c.Models {
		if m.Role == "small" || m.Role == "both" {
			result = append(result, m)
		}
	}
	return result
}
```

**Step 5: Copy models.yaml for embedding**

The `//go:embed` directive requires the file to be in the same package directory or a subdirectory. Copy `models.yaml` to `internal/platform/catalog_embed.yaml`:

Run:
```bash
cp models.yaml internal/platform/catalog_embed.yaml
```

**Step 6: Remove the .gitkeep file**

Run:
```bash
rm internal/platform/.gitkeep
```

**Step 7: Run tests to verify they pass**

Run:
```bash
go test ./internal/platform/... -v
```

Expected: PASS

**Step 8: Commit**

```bash
git add models.yaml internal/platform/catalog.go internal/platform/catalog_test.go internal/platform/catalog_embed.yaml
git rm internal/platform/.gitkeep
git commit -m "feat(platform): add model catalog with embedded YAML and validation"
```

---

## Task 5: Hardware Detection

**Files:**
- Create: `internal/platform/hardware.go`
- Create: `internal/platform/hardware_test.go`

**Step 1: Write the failing tests**

Create `internal/platform/hardware_test.go`:

```go
package platform

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectHardware(t *testing.T) {
	hw, err := DetectHardware()
	require.NoError(t, err)

	// Should always detect some RAM
	assert.Greater(t, hw.SystemRAMGB, 0.0)
}

func TestDetectHardware_HasPlatformInfo(t *testing.T) {
	hw, err := DetectHardware()
	require.NoError(t, err)

	assert.NotEmpty(t, hw.OS)
	assert.NotEmpty(t, hw.Arch)
}

func TestDetectHardware_AppleSilicon(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Apple Silicon test only runs on darwin/arm64")
	}

	hw, err := DetectHardware()
	require.NoError(t, err)

	// Apple Silicon should report unified memory as GPU
	require.Len(t, hw.GPUs, 1)
	assert.Equal(t, "apple", hw.GPUs[0].Type)
	assert.Greater(t, hw.GPUs[0].VRAMGB, 0.0)
	// Unified memory: GPU VRAM should equal system RAM
	assert.Equal(t, hw.SystemRAMGB, hw.GPUs[0].VRAMGB)
}

func TestHardwareInfo_TotalGPUMemory(t *testing.T) {
	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs: []GPUInfo{
			{Type: "nvidia", Name: "RTX 4090", VRAMGB: 24},
			{Type: "nvidia", Name: "RTX 3090", VRAMGB: 24},
		},
	}

	assert.Equal(t, 48.0, hw.TotalGPUMemoryGB())
}

func TestHardwareInfo_TotalGPUMemory_NoGPU(t *testing.T) {
	hw := &HardwareInfo{
		SystemRAMGB: 32,
		GPUs:        nil,
	}

	assert.Equal(t, 0.0, hw.TotalGPUMemoryGB())
}

func TestHardwareInfo_HasGPU(t *testing.T) {
	t.Run("with GPU", func(t *testing.T) {
		hw := &HardwareInfo{GPUs: []GPUInfo{{Type: "nvidia", VRAMGB: 8}}}
		assert.True(t, hw.HasGPU())
	})

	t.Run("without GPU", func(t *testing.T) {
		hw := &HardwareInfo{GPUs: nil}
		assert.False(t, hw.HasGPU())
	})
}

func TestHardwareInfo_BestGPU(t *testing.T) {
	hw := &HardwareInfo{
		GPUs: []GPUInfo{
			{Type: "nvidia", Name: "RTX 3070", VRAMGB: 8},
			{Type: "nvidia", Name: "RTX 4090", VRAMGB: 24},
		},
	}

	best := hw.BestGPU()
	require.NotNil(t, best)
	assert.Equal(t, "RTX 4090", best.Name)
	assert.Equal(t, 24.0, best.VRAMGB)
}

func TestHardwareInfo_BestGPU_NoGPU(t *testing.T) {
	hw := &HardwareInfo{GPUs: nil}
	assert.Nil(t, hw.BestGPU())
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/platform/... -v -run "TestDetect|TestHardware"
```

Expected: FAIL - types not defined

**Step 3: Write the implementation**

Create `internal/platform/hardware.go`:

```go
package platform

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// GPUInfo describes a detected GPU
type GPUInfo struct {
	Type   string  // "nvidia", "amd", "apple"
	Name   string  // e.g. "RTX 4090"
	VRAMGB float64 // video memory in GB
}

// HardwareInfo describes the detected system hardware
type HardwareInfo struct {
	OS          string    // "darwin", "linux", "windows"
	Arch        string    // "amd64", "arm64"
	SystemRAMGB float64   // total system RAM in GB
	GPUs        []GPUInfo // detected GPUs
}

// DetectHardware probes the system for RAM, GPU, and platform info
func DetectHardware() (*HardwareInfo, error) {
	hw := &HardwareInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	ram, err := detectRAM()
	if err != nil {
		return nil, fmt.Errorf("failed to detect RAM: %w", err)
	}
	hw.SystemRAMGB = ram

	hw.GPUs = detectGPUs(hw.OS, hw.Arch, hw.SystemRAMGB)

	return hw, nil
}

// TotalGPUMemoryGB returns the sum of all GPU VRAM
func (h *HardwareInfo) TotalGPUMemoryGB() float64 {
	var total float64
	for _, gpu := range h.GPUs {
		total += gpu.VRAMGB
	}
	return total
}

// HasGPU returns true if any GPU was detected
func (h *HardwareInfo) HasGPU() bool {
	return len(h.GPUs) > 0
}

// BestGPU returns the GPU with the most VRAM, or nil if no GPUs
func (h *HardwareInfo) BestGPU() *GPUInfo {
	if len(h.GPUs) == 0 {
		return nil
	}
	best := &h.GPUs[0]
	for i := 1; i < len(h.GPUs); i++ {
		if h.GPUs[i].VRAMGB > best.VRAMGB {
			best = &h.GPUs[i]
		}
	}
	return best
}

func detectRAM() (float64, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0, err
		}
		bytes, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0, err
		}
		return float64(bytes) / (1024 * 1024 * 1024), nil

	case "linux":
		out, err := exec.Command("grep", "MemTotal", "/proc/meminfo").Output()
		if err != nil {
			return 0, err
		}
		// Format: "MemTotal:       32768000 kB"
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected meminfo format")
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return float64(kb) / (1024 * 1024), nil

	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func detectGPUs(os, arch string, systemRAMGB float64) []GPUInfo {
	var gpus []GPUInfo

	// Apple Silicon: unified memory
	if os == "darwin" && arch == "arm64" {
		gpus = append(gpus, GPUInfo{
			Type:   "apple",
			Name:   "Apple Silicon",
			VRAMGB: systemRAMGB, // unified memory
		})
		return gpus
	}

	// NVIDIA
	if nvidiaGPUs := detectNVIDIA(); len(nvidiaGPUs) > 0 {
		gpus = append(gpus, nvidiaGPUs...)
	}

	// AMD
	if amdGPUs := detectAMD(); len(amdGPUs) > 0 {
		gpus = append(gpus, amdGPUs...)
	}

	return gpus
}

func detectNVIDIA() []GPUInfo {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil // nvidia-smi not available
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		vramMB, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			continue
		}
		gpus = append(gpus, GPUInfo{
			Type:   "nvidia",
			Name:   name,
			VRAMGB: vramMB / 1024,
		})
	}
	return gpus
}

func detectAMD() []GPUInfo {
	out, err := exec.Command("rocm-smi", "--showmeminfo", "vram").Output()
	if err != nil {
		return nil // rocm-smi not available
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Total Memory") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		vramMB, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		gpus = append(gpus, GPUInfo{
			Type:   "amd",
			Name:   "AMD GPU",
			VRAMGB: vramMB / 1024,
		})
	}
	return gpus
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/platform/... -v -run "TestDetect|TestHardware"
```

Expected: PASS (GPU-specific tests may skip on machines without GPUs)

**Step 5: Commit**

```bash
git add internal/platform/hardware.go internal/platform/hardware_test.go
git commit -m "feat(platform): add hardware detection for RAM and GPU"
```

---

## Task 6: Model Selection Algorithm

**Files:**
- Create: `internal/platform/selector.go`
- Create: `internal/platform/selector_test.go`

**Step 1: Write the failing tests**

Create `internal/platform/selector_test.go`:

```go
package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCatalog() *Catalog {
	return &Catalog{
		Models: []CatalogModel{
			{Name: "big:70b", MemoryGB: 42, Role: "large", Quality: 10},
			{Name: "med:32b", MemoryGB: 20, Role: "large", Quality: 8},
			{Name: "small:14b", MemoryGB: 10, Role: "large", Quality: 6},
			{Name: "tiny:7b", MemoryGB: 5, Role: "both", Quality: 4},
			{Name: "micro:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
}

func TestSelectModels_SplitConfig(t *testing.T) {
	// 8GB GPU + 48GB RAM -> 70b on CPU + 7b on GPU
	hw := &HardwareInfo{
		SystemRAMGB: 48,
		GPUs:        []GPUInfo{{Type: "nvidia", Name: "RTX 3070", VRAMGB: 8}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	best := results[0]
	assert.Equal(t, "big:70b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "tiny:7b", best.Small.Name)
	assert.Equal(t, "gpu", best.Small.Device)
	assert.Equal(t, 14, best.Score)
}

func TestSelectModels_BothOnGPU(t *testing.T) {
	// 48GB GPU -> both on GPU
	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs:        []GPUInfo{{Type: "nvidia", Name: "A6000", VRAMGB: 48}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "big:70b", best.Large.Name)
	assert.Equal(t, "gpu", best.Large.Device)
	assert.Equal(t, "micro:1.5b", best.Small.Name)
	assert.Equal(t, "gpu", best.Small.Device)
}

func TestSelectModels_CPUOnly(t *testing.T) {
	// 64GB RAM, no GPU -> both on CPU
	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs:        nil,
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "big:70b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "micro:1.5b", best.Small.Name)
	assert.Equal(t, "cpu", best.Small.Device)
}

func TestSelectModels_SmallMachine(t *testing.T) {
	// 16GB RAM, no GPU -> 14b + 1.5b on CPU
	hw := &HardwareInfo{
		SystemRAMGB: 16,
		GPUs:        nil,
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "small:14b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
}

func TestSelectModels_TinyMachine(t *testing.T) {
	// 8GB RAM, no GPU -> 7b as large + 1.5b as small
	hw := &HardwareInfo{
		SystemRAMGB: 8,
		GPUs:        nil,
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "tiny:7b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "micro:1.5b", best.Small.Name)
	assert.Equal(t, "cpu", best.Small.Device)
}

func TestSelectModels_InsufficientMemory(t *testing.T) {
	// 2GB RAM, no GPU -> nothing fits
	hw := &HardwareInfo{
		SystemRAMGB: 2,
		GPUs:        nil,
	}

	_, err := SelectModels(testCatalog(), hw)
	assert.Error(t, err)
}

func TestSelectModels_ReturnsAlternatives(t *testing.T) {
	hw := &HardwareInfo{
		SystemRAMGB: 48,
		GPUs:        []GPUInfo{{Type: "nvidia", VRAMGB: 8}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	// Should have multiple alternatives
	assert.GreaterOrEqual(t, len(results), 2)

	// Results should be sorted by score descending
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score)
	}
}

func TestSelectModels_SameModelBothRoles(t *testing.T) {
	// Tiny machine: 7b is "both" so it could be used as large
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "tiny:7b", MemoryGB: 5, Role: "both", Quality: 4},
			{Name: "micro:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
	hw := &HardwareInfo{SystemRAMGB: 8, GPUs: nil}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "tiny:7b", best.Large.Name)
	assert.Equal(t, "micro:1.5b", best.Small.Name)
}

func TestSelectModels_IdenticalScoresPreferLargerModel(t *testing.T) {
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "a:14b", MemoryGB: 10, Role: "large", Quality: 6},
			{Name: "b:14b-v2", MemoryGB: 10, Role: "large", Quality: 6},
			{Name: "helper:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
	hw := &HardwareInfo{SystemRAMGB: 16, GPUs: nil}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)
	// Should return results stably
	assert.NotEmpty(t, results)
}

func TestSelectModels_TightFit(t *testing.T) {
	// Exactly enough memory for the pair
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "model:7b", MemoryGB: 5, Role: "both", Quality: 4},
			{Name: "model:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
	hw := &HardwareInfo{SystemRAMGB: 6.5, GPUs: nil}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "model:7b", best.Large.Name)
	assert.Equal(t, "model:1.5b", best.Small.Name)
	assert.True(t, best.TightFit)
}

func TestSelectModels_UnifiedMemoryAppleSilicon(t *testing.T) {
	// 24GB Apple Silicon (unified) -> treat as GPU
	hw := &HardwareInfo{
		SystemRAMGB: 24,
		GPUs:        []GPUInfo{{Type: "apple", Name: "Apple Silicon", VRAMGB: 24}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	// 14b (10GB) + 7b (5GB) = 15GB fits in 24GB unified
	// or 14b + 1.5b = 11.5GB for score 8
	// 7b + 1.5b = 6.5GB for score 6
	// Best: small:14b (quality 6) + micro:1.5b (quality 2) = 8
	// or small:14b + tiny:7b = 10 but 10+5=15 < 24, both on gpu
	assert.Equal(t, "gpu", best.Large.Device)
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/platform/... -v -run "TestSelectModels"
```

Expected: FAIL - SelectModels not defined

**Step 3: Write the implementation**

Create `internal/platform/selector.go`:

```go
package platform

import (
	"fmt"
	"sort"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ModelConfig represents a recommended model configuration
type ModelConfig struct {
	Large    models.ModelPlacement
	Small    models.ModelPlacement
	Score    int  // large.quality + small.quality
	TightFit bool // true if total memory is within 10% of available
}

// SelectModels finds the best (large, small) model pairings for the given hardware
func SelectModels(catalog *Catalog, hw *HardwareInfo) ([]ModelConfig, error) {
	largeModels := catalog.LargeModels()
	smallModels := catalog.SmallModels()

	var configs []ModelConfig

	for _, large := range largeModels {
		for _, small := range smallModels {
			// Skip same model for both roles
			if large.Name == small.Name {
				continue
			}

			placement := findBestPlacement(large, small, hw)
			if placement == nil {
				continue
			}

			totalMemory := large.MemoryGB + small.MemoryGB
			availableMemory := hw.SystemRAMGB
			if hw.HasGPU() {
				availableMemory += hw.BestGPU().VRAMGB
				// For Apple Silicon, don't double-count
				if len(hw.GPUs) == 1 && hw.GPUs[0].Type == "apple" {
					availableMemory = hw.SystemRAMGB
				}
			}
			tightFit := totalMemory > (availableMemory * 0.9)

			configs = append(configs, ModelConfig{
				Large:    placement.large,
				Small:    placement.small,
				Score:    large.Quality + small.Quality,
				TightFit: tightFit,
			})
		}
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no valid model configuration fits in available memory (%.1fGB RAM", hw.SystemRAMGB)
	}

	// Sort by score descending, then by large model memory descending (prefer bigger)
	sort.SliceStable(configs, func(i, j int) bool {
		if configs[i].Score != configs[j].Score {
			return configs[i].Score > configs[j].Score
		}
		return configs[i].Large.MemoryGB > configs[j].Large.MemoryGB
	})

	// Return top configs (max 5)
	if len(configs) > 5 {
		configs = configs[:5]
	}

	return configs, nil
}

type placementResult struct {
	large models.ModelPlacement
	small models.ModelPlacement
}

func findBestPlacement(large, small CatalogModel, hw *HardwareInfo) *placementResult {
	gpuMem := 0.0
	if hw.HasGPU() {
		gpuMem = hw.BestGPU().VRAMGB
	}
	cpuMem := hw.SystemRAMGB

	// For Apple Silicon, GPU memory IS system memory (unified)
	isUnified := len(hw.GPUs) == 1 && hw.GPUs[0].Type == "apple"

	if isUnified {
		// Unified memory: both share the same pool, but both get "gpu" device
		if large.MemoryGB+small.MemoryGB <= cpuMem {
			return &placementResult{
				large: models.ModelPlacement{Name: large.Name, Device: "gpu", MemoryGB: large.MemoryGB},
				small: models.ModelPlacement{Name: small.Name, Device: "gpu", MemoryGB: small.MemoryGB},
			}
		}
		return nil
	}

	// Strategy 1: Both on GPU
	if gpuMem > 0 && large.MemoryGB+small.MemoryGB <= gpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "gpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "gpu", MemoryGB: small.MemoryGB},
		}
	}

	// Strategy 2: Large on CPU, small on GPU (split)
	if gpuMem > 0 && small.MemoryGB <= gpuMem && large.MemoryGB <= cpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "cpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "gpu", MemoryGB: small.MemoryGB},
		}
	}

	// Strategy 3: Large on GPU, small on CPU (split, less common)
	if gpuMem > 0 && large.MemoryGB <= gpuMem && small.MemoryGB <= cpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "gpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "cpu", MemoryGB: small.MemoryGB},
		}
	}

	// Strategy 4: Both on CPU
	if large.MemoryGB+small.MemoryGB <= cpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "cpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "cpu", MemoryGB: small.MemoryGB},
		}
	}

	return nil // Doesn't fit
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/platform/... -v -run "TestSelectModels"
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/platform/selector.go internal/platform/selector_test.go
git commit -m "feat(platform): add model selection algorithm with split-device support"
```

---

## Task 7: Ollama REST Client

**Files:**
- Create: `internal/platform/ollama.go`
- Create: `internal/platform/ollama_test.go`

**Step 1: Write the failing tests**

Create `internal/platform/ollama_test.go`:

```go
package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaClient_Ping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	err := client.Ping(context.Background())
	assert.NoError(t, err)
}

func TestOllamaClient_Ping_Unreachable(t *testing.T) {
	client := NewOllamaClient("http://127.0.0.1:1") // unreachable port
	err := client.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1")
}

func TestOllamaClient_ListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		resp := map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen3-coder:70b", "size": 42000000000},
				{"name": "qwen2.5-coder:7b", "size": 5000000000},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.Equal(t, "qwen3-coder:70b", models[0].Name)
	assert.InDelta(t, 39.1, models[0].SizeGB, 1.0)
}

func TestOllamaClient_ListModels_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestOllamaClient_PullModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/pull", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "test-model:7b", body["name"])

		// Simulate completion
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success"})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	err := client.PullModel(context.Background(), "test-model:7b")
	assert.NoError(t, err)
}

func TestOllamaClient_PullModel_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	err := client.PullModel(context.Background(), "nonexistent:latest")
	assert.Error(t, err)
}

func TestOllamaClient_HasModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen3-coder:70b", "size": 42000000000},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)

	has, err := client.HasModel(context.Background(), "qwen3-coder:70b")
	require.NoError(t, err)
	assert.True(t, has)

	has, err = client.HasModel(context.Background(), "missing:latest")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestOllamaClient_NormalizesURL(t *testing.T) {
	t.Run("trailing slash removed", func(t *testing.T) {
		client := NewOllamaClient("http://localhost:11434/")
		assert.Equal(t, "http://localhost:11434", client.host)
	})

	t.Run("scheme auto-prepended", func(t *testing.T) {
		client := NewOllamaClient("localhost:11434")
		assert.Equal(t, "http://localhost:11434", client.host)
	})
}

func TestOllamaClient_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	_, err := client.ListModels(context.Background())
	assert.Error(t, err)
}

func TestOllamaClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response - will be cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.Ping(ctx)
	assert.Error(t, err)
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/platform/... -v -run "TestOllamaClient"
```

Expected: FAIL - OllamaClient not defined

**Step 3: Write the implementation**

Create `internal/platform/ollama.go`:

```go
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OllamaModel represents a model available on the Ollama server
type OllamaModel struct {
	Name   string  // model name/tag
	SizeGB float64 // size on disk in GB
}

// OllamaClient communicates with the Ollama REST API
type OllamaClient struct {
	host       string
	httpClient *http.Client
}

// NewOllamaClient creates a new client for the given Ollama host
func NewOllamaClient(host string) *OllamaClient {
	// Normalize URL
	host = strings.TrimSuffix(host, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	return &OllamaClient{
		host:       host,
		httpClient: &http.Client{},
	}
}

// Ping checks if Ollama is reachable
func (c *OllamaClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Ollama at %s: %w", c.host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	return nil
}

// ListModels returns all models available on the Ollama server
func (c *OllamaClient) ListModels(ctx context.Context) ([]OllamaModel, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models from %s: %w", c.host, err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	var models []OllamaModel
	for _, m := range result.Models {
		models = append(models, OllamaModel{
			Name:   m.Name,
			SizeGB: float64(m.Size) / (1024 * 1024 * 1024),
		})
	}

	return models, nil
}

// HasModel checks if a specific model is available on the server
func (c *OllamaClient) HasModel(ctx context.Context, name string) (bool, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return false, err
	}

	for _, m := range models {
		if m.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// PullModel downloads a model from the Ollama registry
func (c *OllamaClient) PullModel(ctx context.Context, name string) error {
	body := fmt.Sprintf(`{"name":%q}`, name)
	req, err := http.NewRequestWithContext(ctx, "POST", c.host+"/api/pull", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to pull model %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("failed to pull model %s: %s", name, errResp.Error)
		}
		return fmt.Errorf("failed to pull model %s: HTTP %d", name, resp.StatusCode)
	}

	return nil
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/platform/... -v -run "TestOllamaClient"
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/platform/ollama.go internal/platform/ollama_test.go
git commit -m "feat(platform): add Ollama REST client with ping, list, pull, and has-model"
```

---

## Task 8: Update Install Scripts

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Update the install script**

This is a large change to `scripts/install.sh`. The key changes are:

1. Replace hardcoded `LARGE_MODEL` / `SMALL_MODEL` variables with model selection flow
2. Add `select_models()` function that:
   - Detects hardware (reuses existing `detect_platform` and `detect_gpu`)
   - Asks user: GPU inference / CPU inference / Split / Remote Ollama
   - Reads `models.yaml` (bundled alongside the script or downloaded)
   - Computes best configuration and presents it
   - Lets user accept or customize
3. Add `setup_remote_ollama()` function that:
   - Asks for remote URL
   - Pings the remote Ollama
   - Lists available models
   - Suggests missing recommended models
4. Update `configure_ralph()` to write the new config format with `ollama`, `large_model`, and `small_model` as structured objects
5. Update `pull_models()` to use the selected model names

The install script changes are bash, not Go, so they don't have Go unit tests. They are verified by the existing `scripts/tests/detect_test.sh` bats tests.

Replace `scripts/install.sh` with the updated version that includes the model selection flow. The key new functions are:

- `select_models()` - Interactive model selection with hardware detection
- `setup_remote_ollama()` - Remote Ollama configuration
- `show_model_recommendation()` - Display recommended config
- `customize_models()` - Let user pick custom models

The existing functions (`detect_platform`, `detect_gpu`, `check_ram_requirement`, etc.) remain mostly unchanged. The main flow in `main()` adds `select_models` after `detect_gpu` and before `pull_models`.

**Note:** The full script replacement is large (~800 lines). The implementing agent should read the current script, understand the structure, and add the model selection flow while preserving all existing functionality.

**Step 2: Verify the script is syntactically valid**

Run:
```bash
bash -n scripts/install.sh
```

Expected: No errors

**Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add interactive model selection with hardware detection"
```

---

## Task 9: Run Full Test Suite and Verify

**Step 1: Run all tests**

Run:
```bash
go test ./... -short -v 2>&1 | tail -30
```

Expected: ALL PASS

**Step 2: Run with race detector**

Run:
```bash
go test ./... -short -race
```

Expected: No race conditions

**Step 3: Verify build**

Run:
```bash
go build ./...
```

Expected: No errors

---

## Completion Checklist

- [ ] YAML dependency added
- [ ] ServerConfig updated with ModelPlacement and OllamaConfig
- [ ] All downstream code fixed for new config shape
- [ ] Model catalog (models.yaml) with embedded loading
- [ ] Hardware detection (RAM, GPU VRAM, platform)
- [ ] Selection algorithm scoring all valid pairings with split support
- [ ] Ollama REST client (ping, list, pull, has-model)
- [ ] Install script updated with interactive model selection
- [ ] All tests passing
- [ ] All code committed
