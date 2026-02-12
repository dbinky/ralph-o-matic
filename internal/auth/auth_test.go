package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AuthMode.Valid() ---

func TestAuthMode_Valid(t *testing.T) {
	tests := []struct {
		mode  AuthMode
		valid bool
	}{
		{AuthModeNone, true},
		{AuthModeEntra, true},
		{AuthMode(""), true},         // empty defaults to none
		{AuthMode("unknown"), false}, // unrecognized
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.mode.Valid())
		})
	}
}

// --- AuthMode.Normalize() ---

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

// --- Config.Validate() ---

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

// --- DefaultSettingsPath() ---

func TestDefaultSettingsPath(t *testing.T) {
	path := DefaultSettingsPath()
	assert.NotEmpty(t, path)
	assert.Contains(t, path, "ralph-o-matic")
	assert.Contains(t, path, "settings.json")
}

// --- loadSettingsFile() ---

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

// --- LoadConfig() ---

func TestLoadConfig_DefaultsWhenNothingSet(t *testing.T) {
	cfg, err := LoadConfig(
		func(string) string { return "" }, // no env vars
		"",                                // no settings file path override
	)
	require.NoError(t, err)
	assert.Equal(t, AuthModeNone, cfg.Mode)
}

func TestLoadConfig_EnvVarsFullySet(t *testing.T) {
	envVars := map[string]string{
		"RALPH_AUTH_MODE":           "entra",
		"RALPH_ENTRA_TENANT_ID":     "env-tenant",
		"RALPH_ENTRA_CLIENT_ID":     "env-client",
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
		"RALPH_AUTH_MODE":       "entra",
		"RALPH_ENTRA_TENANT_ID": "env-tenant",
		"RALPH_CONFIG_FILE":     settingsPath,
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
