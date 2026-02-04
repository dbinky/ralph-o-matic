package cli

import (
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config holds CLI configuration
type Config struct {
	Server               string `yaml:"server"`
	DefaultPriority      string `yaml:"default_priority"`
	DefaultMaxIterations int    `yaml:"default_max_iterations"`
}

// DefaultConfig returns a config with defaults
func DefaultConfig() *Config {
	return &Config{
		Server:               "http://localhost:9090",
		DefaultPriority:      "normal",
		DefaultMaxIterations: 50,
	}
}

// configDir returns the platform-conventional config directory for ralph-o-matic.
func configDir() string {
	var dir string

	switch runtime.GOOS {
	case "windows":
		dir = os.Getenv("APPDATA")
		if dir == "" {
			dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	default:
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}

	return filepath.Join(dir, "ralph-o-matic")
}

// ConfigPath returns the default config file path
func ConfigPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

// LoadConfig loads config from file, returning defaults if not found
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// SaveConfig saves config to file
func SaveConfig(path string, cfg *Config) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// Merge returns a new config with non-zero values from other applied
func (c *Config) Merge(other *Config) *Config {
	result := *c

	if other.Server != "" {
		result.Server = other.Server
	}
	if other.DefaultPriority != "" {
		result.DefaultPriority = other.DefaultPriority
	}
	if other.DefaultMaxIterations > 0 {
		result.DefaultMaxIterations = other.DefaultMaxIterations
	}

	return &result
}
