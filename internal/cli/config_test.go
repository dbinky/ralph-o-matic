package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Default(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "http://localhost:9090", cfg.Server)
	assert.Equal(t, "normal", cfg.DefaultPriority)
	assert.Equal(t, 50, cfg.DefaultMaxIterations)
}

func TestConfig_Load_NotExists(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	// Should return defaults
	assert.Equal(t, DefaultConfig().Server, cfg.Server)
}

func TestConfig_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &Config{
		Server:               "http://192.168.1.50:9090",
		DefaultPriority:      "high",
		DefaultMaxIterations: 100,
	}

	err := SaveConfig(configPath, cfg)
	require.NoError(t, err)

	loaded, err := LoadConfig(configPath)
	require.NoError(t, err)

	assert.Equal(t, cfg.Server, loaded.Server)
	assert.Equal(t, cfg.DefaultPriority, loaded.DefaultPriority)
	assert.Equal(t, cfg.DefaultMaxIterations, loaded.DefaultMaxIterations)
}

func TestConfig_Merge(t *testing.T) {
	base := DefaultConfig()
	base.Server = "http://old-server:9090"

	overrides := &Config{
		Server: "http://new-server:9090",
	}

	merged := base.Merge(overrides)

	assert.Equal(t, "http://new-server:9090", merged.Server)
	assert.Equal(t, base.DefaultPriority, merged.DefaultPriority)
}
