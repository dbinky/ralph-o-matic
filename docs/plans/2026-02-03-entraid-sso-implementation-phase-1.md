# Phase 1: Auth Config & Settings Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add auth configuration loading from environment variables and `settings.json`, with validation and sensible defaults.

**Architecture:** New `internal/auth` package with `AuthMode` type, `Config` struct, and `LoadConfig()` function. Config resolution follows env vars > settings.json > defaults. `settings.json` uses platform-conventional paths (`/etc/ralph-o-matic/settings.json` on Linux/macOS, `%ProgramData%\ralph-o-matic\settings.json` on Windows), overridable via `RALPH_CONFIG_FILE`.

**Tech Stack:** Go stdlib (`encoding/json`, `os`, `runtime`, `io`). No external dependencies.

---

### Task 1: Create auth package with AuthMode type and Config struct

**Files:**
- Create: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

**Step 1: Write the failing test**

Create `internal/auth/auth_test.go`:

```go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthMode_Valid(t *testing.T) {
	tests := []struct {
		mode  AuthMode
		valid bool
	}{
		{AuthModeNone, true},
		{AuthModeEntra, true},
		{AuthMode(""), true},       // empty defaults to none
		{AuthMode("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.mode.Valid())
		})
	}
}

func TestAuthMode_Normalize(t *testing.T) {
	tests := []struct {
		input    AuthMode
		expected AuthMode
	}{
		{AuthModeNone, AuthModeNone},
		{AuthModeEntra, AuthModeEntra},
		{AuthMode(""), AuthModeNone},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.Normalize())
		})
	}
}

func TestConfig_Validate_ModeNone(t *testing.T) {
	cfg := &Config{Mode: AuthModeNone}
	assert.NoError(t, cfg.Validate())
}

func TestConfig_Validate_ModeEntra_Complete(t *testing.T) {
	cfg := &Config{
		Mode: AuthModeEntra,
		Entra: EntraConfig{
			TenantID:     "tenant-id",
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		},
	}
	assert.NoError(t, cfg.Validate())
}

func TestConfig_Validate_ModeEntra_MissingTenantID(t *testing.T) {
	cfg := &Config{
		Mode: AuthModeEntra,
		Entra: EntraConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id")
}

func TestConfig_Validate_ModeEntra_MissingClientID(t *testing.T) {
	cfg := &Config{
		Mode: AuthModeEntra,
		Entra: EntraConfig{
			TenantID:     "tenant-id",
			ClientSecret: "client-secret",
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client_id")
}

func TestConfig_Validate_ModeEntra_MissingClientSecret(t *testing.T) {
	cfg := &Config{
		Mode: AuthModeEntra,
		Entra: EntraConfig{
			TenantID: "tenant-id",
			ClientID: "client-id",
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client_secret")
}

func TestConfig_Validate_ModeEntra_AllMissing(t *testing.T) {
	cfg := &Config{Mode: AuthModeEntra}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id")
	assert.Contains(t, err.Error(), "client_id")
	assert.Contains(t, err.Error(), "client_secret")
}

func TestConfig_Validate_UnknownMode(t *testing.T) {
	cfg := &Config{Mode: AuthMode("ldap")}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth mode")
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestAuthMode ./internal/auth/`
Expected: FAIL — package does not exist yet

**Step 3: Write minimal implementation**

Create `internal/auth/auth.go`:

```go
package auth

import (
	"fmt"
	"strings"
)

// AuthMode represents the authentication mode
type AuthMode string

const (
	// AuthModeNone disables authentication (default)
	AuthModeNone AuthMode = "none"
	// AuthModeEntra enables EntraID (Azure AD) SSO authentication
	AuthModeEntra AuthMode = "entra"
)

// Valid returns true if the auth mode is recognized
func (m AuthMode) Valid() bool {
	switch m.Normalize() {
	case AuthModeNone, AuthModeEntra:
		return true
	default:
		return false
	}
}

// Normalize returns the canonical form of the auth mode.
// Empty string normalizes to AuthModeNone.
func (m AuthMode) Normalize() AuthMode {
	if m == "" {
		return AuthModeNone
	}
	return m
}

// EntraConfig holds EntraID-specific configuration
type EntraConfig struct {
	TenantID     string `json:"tenant_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Config holds the complete auth configuration
type Config struct {
	Mode  AuthMode    `json:"mode"`
	Entra EntraConfig `json:"entra"`
}

// Validate checks the auth configuration for completeness
func (c *Config) Validate() error {
	c.Mode = c.Mode.Normalize()

	if !c.Mode.Valid() {
		return fmt.Errorf("unknown auth mode: %q", c.Mode)
	}

	if c.Mode == AuthModeEntra {
		var missing []string
		if c.Entra.TenantID == "" {
			missing = append(missing, "tenant_id")
		}
		if c.Entra.ClientID == "" {
			missing = append(missing, "client_id")
		}
		if c.Entra.ClientSecret == "" {
			missing = append(missing, "client_secret")
		}
		if len(missing) > 0 {
			return fmt.Errorf("auth mode %q requires: %s", c.Mode, strings.Join(missing, ", "))
		}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestAuthMode -run TestConfig_Validate ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat(auth): add AuthMode type, Config struct, and validation"
```

---

### Task 2: Implement settings.json file loading

**Files:**
- Modify: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/auth_test.go`:

```go
func TestDefaultSettingsPath(t *testing.T) {
	path := DefaultSettingsPath()
	assert.NotEmpty(t, path)
	assert.Contains(t, path, "ralph-o-matic")
	assert.Contains(t, path, "settings.json")
}

func TestLoadSettingsFile_CompleteConfig(t *testing.T) {
	content := `{
		"auth": {
			"mode": "entra",
			"entra": {
				"tenant_id": "tid",
				"client_id": "cid",
				"client_secret": "csecret"
			}
		}
	}`
	cfg, err := loadSettingsFile(strings.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, AuthModeEntra, cfg.Mode)
	assert.Equal(t, "tid", cfg.Entra.TenantID)
	assert.Equal(t, "cid", cfg.Entra.ClientID)
	assert.Equal(t, "csecret", cfg.Entra.ClientSecret)
}

func TestLoadSettingsFile_EmptyJSON(t *testing.T) {
	cfg, err := loadSettingsFile(strings.NewReader("{}"))
	require.NoError(t, err)
	assert.Equal(t, AuthModeNone, cfg.Mode.Normalize())
}

func TestLoadSettingsFile_NoAuthBlock(t *testing.T) {
	cfg, err := loadSettingsFile(strings.NewReader(`{"other": "stuff"}`))
	require.NoError(t, err)
	assert.Equal(t, AuthModeNone, cfg.Mode.Normalize())
}

func TestLoadSettingsFile_MalformedJSON(t *testing.T) {
	_, err := loadSettingsFile(strings.NewReader("{invalid"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestLoadSettingsFile_AuthBlockPartial(t *testing.T) {
	content := `{
		"auth": {
			"mode": "entra",
			"entra": {
				"tenant_id": "tid"
			}
		}
	}`
	cfg, err := loadSettingsFile(strings.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, AuthModeEntra, cfg.Mode)
	assert.Equal(t, "tid", cfg.Entra.TenantID)
	assert.Equal(t, "", cfg.Entra.ClientID)
}

func TestLoadSettingsFile_EmptyMode(t *testing.T) {
	content := `{"auth": {"mode": ""}}`
	cfg, err := loadSettingsFile(strings.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, AuthModeNone, cfg.Mode.Normalize())
}
```

Add `strings` to the test file imports.

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestLoadSettingsFile ./internal/auth/`
Expected: FAIL — `loadSettingsFile` not defined

**Step 3: Write minimal implementation**

Add to `internal/auth/auth.go`:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

// settingsFile represents the top-level settings.json structure
type settingsFile struct {
	Auth *Config `json:"auth"`
}

// DefaultSettingsPath returns the platform-conventional path for settings.json
func DefaultSettingsPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(`C:\ProgramData`, "ralph-o-matic", "settings.json")
	default:
		return filepath.Join("/etc", "ralph-o-matic", "settings.json")
	}
}

// loadSettingsFile parses auth config from a settings.json reader.
// Returns a zero-value Config (mode "none") if the auth block is missing.
func loadSettingsFile(r io.Reader) (*Config, error) {
	var sf settingsFile
	if err := json.NewDecoder(r).Decode(&sf); err != nil {
		return nil, fmt.Errorf("failed to parse settings file: %w", err)
	}

	if sf.Auth == nil {
		return &Config{}, nil
	}

	return sf.Auth, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestLoadSettingsFile|TestDefaultSettingsPath" ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat(auth): add settings.json file loading"
```

---

### Task 3: Implement env var config loading with precedence

**Files:**
- Modify: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/auth_test.go`:

```go
func TestLoadConfig_DefaultsWhenNothingSet(t *testing.T) {
	cfg, err := LoadConfig(
		func(key string) string { return "" },  // no env vars
		"",  // no settings file path override
	)
	require.NoError(t, err)
	assert.Equal(t, AuthModeNone, cfg.Mode)
}

func TestLoadConfig_EnvVarsFullySet(t *testing.T) {
	envVars := map[string]string{
		"RALPH_AUTH_MODE":          "entra",
		"RALPH_ENTRA_TENANT_ID":   "env-tenant",
		"RALPH_ENTRA_CLIENT_ID":   "env-client",
		"RALPH_ENTRA_CLIENT_SECRET": "env-secret",
	}
	cfg, err := LoadConfig(
		func(key string) string { return envVars[key] },
		"",
	)
	require.NoError(t, err)
	assert.Equal(t, AuthModeEntra, cfg.Mode)
	assert.Equal(t, "env-tenant", cfg.Entra.TenantID)
	assert.Equal(t, "env-client", cfg.Entra.ClientID)
	assert.Equal(t, "env-secret", cfg.Entra.ClientSecret)
}

func TestLoadConfig_EnvVarsOverrideFile(t *testing.T) {
	// Write a settings file
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	content := `{
		"auth": {
			"mode": "entra",
			"entra": {
				"tenant_id": "file-tenant",
				"client_id": "file-client",
				"client_secret": "file-secret"
			}
		}
	}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(content), 0600))

	envVars := map[string]string{
		"RALPH_AUTH_MODE":          "entra",
		"RALPH_ENTRA_TENANT_ID":   "env-tenant",
		"RALPH_CONFIG_FILE":       settingsPath,
	}
	cfg, err := LoadConfig(
		func(key string) string { return envVars[key] },
		"",
	)
	require.NoError(t, err)
	// Env vars take precedence
	assert.Equal(t, "env-tenant", cfg.Entra.TenantID)
	// File fills in the rest
	assert.Equal(t, "file-client", cfg.Entra.ClientID)
	assert.Equal(t, "file-secret", cfg.Entra.ClientSecret)
}

func TestLoadConfig_FileOnly(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	content := `{
		"auth": {
			"mode": "entra",
			"entra": {
				"tenant_id": "file-tenant",
				"client_id": "file-client",
				"client_secret": "file-secret"
			}
		}
	}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(content), 0600))

	cfg, err := LoadConfig(
		func(key string) string {
			if key == "RALPH_CONFIG_FILE" {
				return settingsPath
			}
			return ""
		},
		"",
	)
	require.NoError(t, err)
	assert.Equal(t, AuthModeEntra, cfg.Mode)
	assert.Equal(t, "file-tenant", cfg.Entra.TenantID)
}

func TestLoadConfig_ConfigFileEnvOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom-settings.json")
	content := `{"auth": {"mode": "entra", "entra": {"tenant_id": "custom", "client_id": "cid", "client_secret": "cs"}}}`
	require.NoError(t, os.WriteFile(customPath, []byte(content), 0600))

	cfg, err := LoadConfig(
		func(key string) string {
			if key == "RALPH_CONFIG_FILE" {
				return customPath
			}
			return ""
		},
		filepath.Join(dir, "nonexistent.json"), // default path doesn't exist
	)
	require.NoError(t, err)
	assert.Equal(t, "custom", cfg.Entra.TenantID)
}

func TestLoadConfig_EmptyEnvVarsTreatedAsUnset(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	content := `{"auth": {"mode": "entra", "entra": {"tenant_id": "file-t", "client_id": "file-c", "client_secret": "file-s"}}}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(content), 0600))

	cfg, err := LoadConfig(
		func(key string) string {
			switch key {
			case "RALPH_AUTH_MODE":
				return "" // empty, should fall through
			case "RALPH_CONFIG_FILE":
				return settingsPath
			}
			return ""
		},
		"",
	)
	require.NoError(t, err)
	// Falls through to file
	assert.Equal(t, AuthModeEntra, cfg.Mode)
	assert.Equal(t, "file-t", cfg.Entra.TenantID)
}

func TestLoadConfig_NonexistentConfigFileFallsToDefaults(t *testing.T) {
	cfg, err := LoadConfig(
		func(key string) string {
			if key == "RALPH_CONFIG_FILE" {
				return "/nonexistent/path/settings.json"
			}
			return ""
		},
		"",
	)
	require.NoError(t, err)
	assert.Equal(t, AuthModeNone, cfg.Mode)
}

func TestLoadConfig_MalformedFileFails(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte("{invalid"), 0600))

	_, err := LoadConfig(
		func(key string) string {
			if key == "RALPH_CONFIG_FILE" {
				return settingsPath
			}
			return ""
		},
		"",
	)
	assert.Error(t, err)
}
```

Add `os` and `path/filepath` to the test file imports (alongside the existing `strings`).

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestLoadConfig ./internal/auth/`
Expected: FAIL — `LoadConfig` not defined

**Step 3: Write minimal implementation**

Add to `internal/auth/auth.go`:

```go
import "os"

// LoadConfig resolves auth configuration from environment variables
// and settings.json, with env vars taking precedence.
//
// getenv is the env var lookup function (typically os.Getenv).
// defaultSettingsPath is the fallback file path when RALPH_CONFIG_FILE is not set.
func LoadConfig(getenv func(string) string, defaultSettingsPath string) (*Config, error) {
	cfg := &Config{Mode: AuthModeNone}

	// 1. Try to load from settings.json file
	settingsPath := getenv("RALPH_CONFIG_FILE")
	if settingsPath == "" {
		settingsPath = defaultSettingsPath
	}
	if settingsPath == "" {
		settingsPath = DefaultSettingsPath()
	}

	if settingsPath != "" {
		f, err := os.Open(settingsPath)
		if err == nil {
			defer f.Close()
			fileCfg, err := loadSettingsFile(f)
			if err != nil {
				return nil, fmt.Errorf("error reading %s: %w", settingsPath, err)
			}
			cfg = fileCfg
		}
		// File not found is not an error — fall through to defaults
	}

	// 2. Env vars override file values (non-empty only)
	if mode := getenv("RALPH_AUTH_MODE"); mode != "" {
		cfg.Mode = AuthMode(mode)
	}
	if v := getenv("RALPH_ENTRA_TENANT_ID"); v != "" {
		cfg.Entra.TenantID = v
	}
	if v := getenv("RALPH_ENTRA_CLIENT_ID"); v != "" {
		cfg.Entra.ClientID = v
	}
	if v := getenv("RALPH_ENTRA_CLIENT_SECRET"); v != "" {
		cfg.Entra.ClientSecret = v
	}

	// 3. Normalize mode
	cfg.Mode = cfg.Mode.Normalize()

	return cfg, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestLoadConfig ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat(auth): add LoadConfig with env var + file precedence"
```

---

### Task 4: Run full test suite

**Step 1: Run all auth tests**

Run: `go test -v -race ./internal/auth/`
Expected: All PASS with no race conditions

**Step 2: Run existing test suite to verify no regressions**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 3: Run linter**

Run: `make lint`
Expected: No lint errors

**Step 4: Commit (if any fixes needed)**

```bash
git add -A
git commit -m "fix(auth): address lint/test issues"
```

---

## Dependencies

- **Depends on:** Nothing (first phase)
- **Blocks:** Phase 2, Phase 3, Phase 4

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 23-67, "Auth Mode & Configuration")
- Existing config pattern: `internal/cli/config.go` (for platform path conventions)
- Test pattern: `internal/db/db_test.go` (for test helper style)
