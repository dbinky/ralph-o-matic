package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel)
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel)
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

	t.Run("empty large_model fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.LargeModel = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("empty small_model fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.SmallModel = ""
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
	base := DefaultServerConfig()
	base.LargeModel = "original-model"
	base.ConcurrentJobs = 2

	updates := &ServerConfig{
		LargeModel: "new-model",
	}

	merged := base.Merge(updates)

	assert.Equal(t, "new-model", merged.LargeModel)
	assert.Equal(t, base.SmallModel, merged.SmallModel)
	assert.Equal(t, 2, merged.ConcurrentJobs) // Unchanged
}

func TestServerConfig_JSON(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.LargeModel = "test-model"

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ServerConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, cfg.LargeModel, decoded.LargeModel)
	assert.Equal(t, cfg.SmallModel, decoded.SmallModel)
	assert.Equal(t, cfg.DefaultMaxIterations, decoded.DefaultMaxIterations)
}
