package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPlacement_Validate(t *testing.T) {
	t.Run("valid placement", func(t *testing.T) {
		mp := ModelPlacement{Name: "qwen3-coder:70b", Device: "gpu", MemoryGB: 42}
		assert.NoError(t, mp.Validate())
	})

	t.Run("empty name fails", func(t *testing.T) {
		mp := ModelPlacement{Name: "", Device: "gpu", MemoryGB: 5}
		assert.Error(t, mp.Validate())
	})

	t.Run("invalid device fails", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "tpu", MemoryGB: 5}
		assert.Error(t, mp.Validate())
	})

	t.Run("gpu passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "gpu"}
		assert.NoError(t, mp.Validate())
	})

	t.Run("cpu passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "cpu"}
		assert.NoError(t, mp.Validate())
	})

	t.Run("auto passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: "auto"}
		assert.NoError(t, mp.Validate())
	})

	t.Run("empty device passes", func(t *testing.T) {
		mp := ModelPlacement{Name: "model", Device: ""}
		assert.NoError(t, mp.Validate())
	})
}

func TestOllamaConfig_Validate(t *testing.T) {
	t.Run("valid passes", func(t *testing.T) {
		oc := OllamaConfig{Host: "http://localhost:11434", IsRemote: false}
		assert.NoError(t, oc.Validate())
	})

	t.Run("empty host fails", func(t *testing.T) {
		oc := OllamaConfig{Host: "", IsRemote: false}
		assert.Error(t, oc.Validate())
	})

	t.Run("remote passes", func(t *testing.T) {
		oc := OllamaConfig{Host: "http://remote:11434", IsRemote: true}
		assert.NoError(t, oc.Validate())
	})
}

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	// Ollama defaults
	assert.Equal(t, "http://localhost:11434", cfg.Ollama.Host)
	assert.False(t, cfg.Ollama.IsRemote)

	// LargeModel defaults
	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel.Name)
	assert.Equal(t, "cpu", cfg.LargeModel.Device)
	assert.Equal(t, 42.0, cfg.LargeModel.MemoryGB)

	// SmallModel defaults
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel.Name)
	assert.Equal(t, "gpu", cfg.SmallModel.Device)
	assert.Equal(t, 5.0, cfg.SmallModel.MemoryGB)

	// Existing fields
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
		assert.NoError(t, validConfig().Validate())
	})

	t.Run("empty model name fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.LargeModel.Name = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("invalid device fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.SmallModel.Device = "tpu"
		assert.Error(t, cfg.Validate())
	})

	t.Run("empty ollama host fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.Ollama.Host = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero iterations fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.DefaultMaxIterations = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("zero jobs fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.ConcurrentJobs = 0
		assert.Error(t, cfg.Validate())
	})

	t.Run("negative retention fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.JobRetentionDays = -1
		assert.Error(t, cfg.Validate())
	})
}

func TestServerConfig_Merge(t *testing.T) {
	t.Run("merge updates model name without clobbering device", func(t *testing.T) {
		base := DefaultServerConfig()
		updates := &ServerConfig{
			LargeModel: ModelPlacement{Name: "new-model"},
		}
		merged := base.Merge(updates)
		assert.Equal(t, "new-model", merged.LargeModel.Name)
		assert.Equal(t, "cpu", merged.LargeModel.Device)
		assert.Equal(t, 42.0, merged.LargeModel.MemoryGB)
	})

	t.Run("merge updates ollama host without clobbering IsRemote", func(t *testing.T) {
		base := DefaultServerConfig()
		base.Ollama.IsRemote = true
		updates := &ServerConfig{
			Ollama: OllamaConfig{Host: "http://other:11434"},
		}
		merged := base.Merge(updates)
		assert.Equal(t, "http://other:11434", merged.Ollama.Host)
		assert.True(t, merged.Ollama.IsRemote)
	})

	t.Run("zero-value changes nothing", func(t *testing.T) {
		base := DefaultServerConfig()
		updates := &ServerConfig{}
		merged := base.Merge(updates)
		assert.Equal(t, base.LargeModel, merged.LargeModel)
		assert.Equal(t, base.SmallModel, merged.SmallModel)
		assert.Equal(t, base.Ollama, merged.Ollama)
		assert.Equal(t, base.DefaultMaxIterations, merged.DefaultMaxIterations)
	})
}

func TestServerConfig_JSON(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.LargeModel.Name = "test-model"
	cfg.Ollama.IsRemote = true

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ServerConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "test-model", decoded.LargeModel.Name)
	assert.Equal(t, "cpu", decoded.LargeModel.Device)
	assert.Equal(t, 42.0, decoded.LargeModel.MemoryGB)
	assert.Equal(t, cfg.SmallModel, decoded.SmallModel)
	assert.Equal(t, "http://localhost:11434", decoded.Ollama.Host)
	assert.True(t, decoded.Ollama.IsRemote)
	assert.Equal(t, cfg.DefaultMaxIterations, decoded.DefaultMaxIterations)
}
