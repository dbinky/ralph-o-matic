package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AuthMode represents the authentication mode.
type AuthMode string

const (
	// AuthModeNone disables authentication (default).
	AuthModeNone AuthMode = "none"
	// AuthModeEntra enables EntraID (Azure AD) SSO authentication.
	AuthModeEntra AuthMode = "entra"
	// AuthModeAPIKey enables static API key authentication via Bearer token.
	// Use this when Entra SSO is not available but you need to protect endpoints
	// from unauthenticated access (e.g. when using the Anthropic backend).
	AuthModeAPIKey AuthMode = "apikey"
)

// Normalize returns the canonical form of the auth mode.
// Empty string normalizes to AuthModeNone.
func (m AuthMode) Normalize() AuthMode {
	if m == "" {
		return AuthModeNone
	}
	return m
}

// Valid returns true if the auth mode is recognized.
// Empty string is valid (normalizes to none).
func (m AuthMode) Valid() bool {
	switch m.Normalize() {
	case AuthModeNone, AuthModeEntra, AuthModeAPIKey:
		return true
	default:
		return false
	}
}

// EntraConfig holds EntraID-specific configuration.
type EntraConfig struct {
	TenantID     string `json:"tenant_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Config holds the complete auth configuration.
type Config struct {
	Mode   AuthMode    `json:"mode"`
	Entra  EntraConfig `json:"entra"`
	APIKey string      `json:"api_key"`
}

// Validate checks the auth configuration for completeness.
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

	if c.Mode == AuthModeAPIKey && c.APIKey == "" {
		return fmt.Errorf("auth mode %q requires a non-empty api_key", c.Mode)
	}

	return nil
}

// settingsFile represents the top-level settings.json structure.
type settingsFile struct {
	Auth *Config `json:"auth"`
}

// DefaultSettingsPath returns the platform-conventional path for settings.json.
func DefaultSettingsPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(`C:\ProgramData`, "ralph-o-matic", "settings.json")
	}
	return filepath.Join("/etc", "ralph-o-matic", "settings.json")
}

// loadSettingsFile parses auth config from a settings.json reader.
// Returns a zero-value Config (mode "") if the auth block is missing.
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

// LoadConfig resolves auth configuration from environment variables
// and settings.json, with env vars taking precedence.
//
// getenv is the env var lookup function (typically os.Getenv).
// defaultSettingsPath is the fallback file path when RALPH_CONFIG_FILE is not set.
// If defaultSettingsPath is empty, DefaultSettingsPath() is used.
func LoadConfig(getenv func(string) string, defaultSettingsPath string) (*Config, error) {
	cfg := &Config{Mode: AuthModeNone}

	// 1. Resolve settings file path: RALPH_CONFIG_FILE > defaultSettingsPath > DefaultSettingsPath()
	settingsPath := getenv("RALPH_CONFIG_FILE")
	if settingsPath == "" {
		settingsPath = defaultSettingsPath
	}
	if settingsPath == "" {
		settingsPath = DefaultSettingsPath()
	}

	// 2. Try to load from settings file
	if settingsPath != "" {
		f, err := os.Open(settingsPath)
		if err == nil {
			defer f.Close()
			fileCfg, parseErr := loadSettingsFile(f)
			if parseErr != nil {
				return nil, fmt.Errorf("error reading %s: %w", settingsPath, parseErr)
			}
			cfg = fileCfg
		}
		// File not found is not an error -- fall through to defaults
	}

	// 3. Env vars override file values (non-empty only)
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
	if v := getenv("RALPH_API_KEY"); v != "" {
		cfg.APIKey = v
	}

	// 4. Normalize mode
	cfg.Mode = cfg.Mode.Normalize()

	return cfg, nil
}
