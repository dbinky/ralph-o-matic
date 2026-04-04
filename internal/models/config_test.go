package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPlacement_Validate(t *testing.T) {
	t.Run("valid placement", func(t *testing.T) {
		mp := ModelPlacement{Name: "devstral", Device: "gpu", MemoryGB: 15}
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
	assert.Equal(t, "devstral", cfg.LargeModel.Name)
	assert.Equal(t, "cpu", cfg.LargeModel.Device)
	assert.Equal(t, 15.0, cfg.LargeModel.MemoryGB)

	// SmallModel defaults
	assert.Equal(t, "qwen3:8b", cfg.SmallModel.Name)
	assert.Equal(t, "gpu", cfg.SmallModel.Device)
	assert.Equal(t, 5.2, cfg.SmallModel.MemoryGB)

	// Existing fields
	assert.Equal(t, 50, cfg.DefaultMaxIterations)
	assert.Equal(t, 30, cfg.JobRetentionDays)
	assert.Equal(t, 3, cfg.MaxClaudeRetries)
	assert.Equal(t, 3, cfg.MaxGitRetries)
	assert.Equal(t, 1000, cfg.GitRetryBackoffMs)
}

func TestBackend_Valid(t *testing.T) {
	assert.True(t, BackendOllama.Valid())
	assert.True(t, BackendAnthropic.Valid())
	assert.False(t, Backend("gpt").Valid())
	assert.True(t, Backend("").Valid())
}

func TestAnthropicConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		ac := AnthropicConfig{
			LargeModel: "claude-opus-4-5-20251101",
			SmallModel: "claude-haiku-4-5-20251001",
		}
		assert.NoError(t, ac.Validate())
	})

	t.Run("empty large model fails", func(t *testing.T) {
		ac := AnthropicConfig{LargeModel: "", SmallModel: "claude-haiku-4-5-20251001"}
		assert.Error(t, ac.Validate())
	})

	t.Run("empty small model fails", func(t *testing.T) {
		ac := AnthropicConfig{LargeModel: "claude-opus-4-5-20251101", SmallModel: ""}
		assert.Error(t, ac.Validate())
	})
}

func TestDefaultServerConfig_AnthropicDefaults(t *testing.T) {
	cfg := DefaultServerConfig()
	assert.Equal(t, BackendOllama, cfg.DefaultBackend)
	assert.Equal(t, "claude-sonnet-4-6-20260218", cfg.Anthropic.LargeModel)
	assert.Equal(t, "claude-sonnet-4-6-20260218", cfg.Anthropic.SmallModel)
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
		assert.Equal(t, 15.0, merged.LargeModel.MemoryGB)
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

func TestServerConfig_Validate_Anthropic(t *testing.T) {
	t.Run("default config validates", func(t *testing.T) {
		cfg := DefaultServerConfig()
		assert.NoError(t, cfg.Validate())
	})

	t.Run("anthropic backend with empty large model fails", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.DefaultBackend = BackendAnthropic
		cfg.Anthropic.LargeModel = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("ollama backend skips anthropic validation", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.DefaultBackend = BackendOllama
		cfg.Anthropic.LargeModel = ""
		assert.NoError(t, cfg.Validate())
	})

	t.Run("invalid backend fails", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.DefaultBackend = "gpt"
		assert.Error(t, cfg.Validate())
	})
}

func TestServerConfig_Merge_Backend(t *testing.T) {
	t.Run("merge updates default_backend", func(t *testing.T) {
		base := DefaultServerConfig()
		updates := &ServerConfig{DefaultBackend: BackendAnthropic}
		merged := base.Merge(updates)
		assert.Equal(t, BackendAnthropic, merged.DefaultBackend)
	})

	t.Run("empty backend preserves base", func(t *testing.T) {
		base := DefaultServerConfig()
		base.DefaultBackend = BackendAnthropic
		updates := &ServerConfig{}
		merged := base.Merge(updates)
		assert.Equal(t, BackendAnthropic, merged.DefaultBackend)
	})

	t.Run("merge updates anthropic config", func(t *testing.T) {
		base := DefaultServerConfig()
		updates := &ServerConfig{
			Anthropic: AnthropicConfig{
				LargeModel: "claude-sonnet-4-20250514",
			},
		}
		merged := base.Merge(updates)
		assert.Equal(t, "claude-sonnet-4-20250514", merged.Anthropic.LargeModel)
		assert.Equal(t, "claude-sonnet-4-6-20260218", merged.Anthropic.SmallModel)
	})
}

func TestDefaultServerConfig_NoConcurrentJobs(t *testing.T) {
	cfg := DefaultServerConfig()

	// ConcurrentJobs field should not exist — verify via JSON serialization
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasConcurrentJobs := raw["concurrent_jobs"]
	assert.False(t, hasConcurrentJobs, "concurrent_jobs should not be in serialized config")
}

func TestServerConfig_Validate_NoConcurrentJobsCheck(t *testing.T) {
	cfg := DefaultServerConfig()
	// Default config should validate without any concurrent_jobs logic
	assert.NoError(t, cfg.Validate())

	// Verify there's no ConcurrentJobs field by checking JSON round-trip
	data, _ := json.Marshal(cfg)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	_, exists := raw["concurrent_jobs"]
	assert.False(t, exists, "concurrent_jobs field should not exist on ServerConfig")
}

func TestServerConfig_Merge_NoConcurrentJobs(t *testing.T) {
	base := DefaultServerConfig()
	updates := &ServerConfig{DefaultMaxIterations: 100}
	merged := base.Merge(updates)

	// Verify merged config has no concurrent_jobs in JSON
	data, _ := json.Marshal(merged)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	_, exists := raw["concurrent_jobs"]
	assert.False(t, exists, "concurrent_jobs should not appear in merged config")
	assert.Equal(t, 100, merged.DefaultMaxIterations)
}

func TestBackend_Valid_OpenRouter(t *testing.T) {
	assert.True(t, BackendOpenRouter.Valid())
}

func TestOpenRouterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  OpenRouterConfig
		wantErr bool
	}{
		{
			name: "valid config with https",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "https://openrouter.ai/api/v1",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: false,
		},
		{
			name: "http localhost allowed",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "http://localhost:8080/v1",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: false,
		},
		{
			name: "http 127.0.0.1 allowed",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "http://127.0.0.1:8080/v1",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: false,
		},
		{
			name: "empty base_url rejected",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "http non-localhost rejected",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "http://evil.example.com/steal",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "non-url rejected",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "not-a-url",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "ftp scheme rejected",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "ftp://files.example.com/data",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "missing API key",
			config: OpenRouterConfig{
				BaseURL:    "https://openrouter.ai/api/v1",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "missing large model",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "https://openrouter.ai/api/v1",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "missing small model",
			config: OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "https://openrouter.ai/api/v1",
				LargeModel: "moonshotai/kimi-k2.5",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServerConfig_Validate_OpenRouter(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.DefaultBackend = BackendOpenRouter
	cfg.OpenRouter.APIKey = "sk-or-v1-test"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"

	assert.NoError(t, cfg.Validate())
}

func TestServerConfig_Validate_OpenRouter_MissingKey(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.DefaultBackend = BackendOpenRouter
	// API key left empty

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "openrouter")
}

func TestServerConfig_Merge_OpenRouter(t *testing.T) {
	base := DefaultServerConfig()
	updates := &ServerConfig{
		OpenRouter: OpenRouterConfig{
			APIKey:     "sk-or-v1-new",
			LargeModel: "x-ai/grok-4-1-fast",
		},
	}

	merged := base.Merge(updates)
	assert.Equal(t, "sk-or-v1-new", merged.OpenRouter.APIKey)
	assert.Equal(t, "x-ai/grok-4-1-fast", merged.OpenRouter.LargeModel)
	// SmallModel should keep default
	assert.Equal(t, "mistralai/devstral-2-2512", merged.OpenRouter.SmallModel)
}

func TestServerConfig_MergeJSON_OpenRouter(t *testing.T) {
	base := DefaultServerConfig()
	raw := json.RawMessage(`{"openrouter":{"api_key":"sk-or-v1-json","large_model":"test/model"}}`)

	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, "sk-or-v1-json", merged.OpenRouter.APIKey)
	assert.Equal(t, "test/model", merged.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", merged.OpenRouter.SmallModel)
}

func TestDefaultServerConfig_OpenRouter_Defaults(t *testing.T) {
	cfg := DefaultServerConfig()
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.OpenRouter.BaseURL)
	assert.Equal(t, "moonshotai/kimi-k2.5", cfg.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", cfg.OpenRouter.SmallModel)
	assert.Equal(t, "", cfg.OpenRouter.APIKey)
}

func TestNewServerConfigResponse_OpenRouter_RedactsKey(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.OpenRouter.APIKey = "sk-or-v1-secret-key"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"
	cfg.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"

	resp := NewServerConfigResponse(cfg)

	assert.Equal(t, "moonshotai/kimi-k2.5", resp.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", resp.OpenRouter.SmallModel)
	assert.Equal(t, "https://openrouter.ai/api/v1", resp.OpenRouter.BaseURL)
	assert.True(t, resp.OpenRouter.APIKeySet)
}

func TestNewServerConfigResponse_OpenRouter_NoKey(t *testing.T) {
	cfg := DefaultServerConfig()

	resp := NewServerConfigResponse(cfg)

	assert.False(t, resp.OpenRouter.APIKeySet)
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
	assert.Equal(t, 15.0, decoded.LargeModel.MemoryGB)
	assert.Equal(t, cfg.SmallModel, decoded.SmallModel)
	assert.Equal(t, "http://localhost:11434", decoded.Ollama.Host)
	assert.True(t, decoded.Ollama.IsRemote)
	assert.Equal(t, cfg.DefaultMaxIterations, decoded.DefaultMaxIterations)
}

// --- MergeJSON additional coverage ---

func TestServerConfig_MergeJSON_InvalidJSON(t *testing.T) {
	base := DefaultServerConfig()
	_, err := base.MergeJSON(json.RawMessage(`{invalid`))
	assert.Error(t, err)
}

func TestServerConfig_MergeJSON_OllamaIsRemoteExplicitFalse(t *testing.T) {
	base := DefaultServerConfig()
	base.Ollama.IsRemote = true
	raw := json.RawMessage(`{"ollama":{"is_remote":false}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.False(t, merged.Ollama.IsRemote)
}

func TestServerConfig_MergeJSON_LargeModelMemoryGBExplicitZero(t *testing.T) {
	base := DefaultServerConfig()
	base.LargeModel.MemoryGB = 8.0
	raw := json.RawMessage(`{"large_model":{"memory_gb":0}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, 0.0, merged.LargeModel.MemoryGB)
}

func TestServerConfig_MergeJSON_SmallModelMemoryGBExplicitZero(t *testing.T) {
	base := DefaultServerConfig()
	base.SmallModel.MemoryGB = 4.0
	raw := json.RawMessage(`{"small_model":{"memory_gb":0}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, 0.0, merged.SmallModel.MemoryGB)
}

func TestServerConfig_MergeJSON_DefaultBackendExplicit(t *testing.T) {
	base := DefaultServerConfig()
	raw := json.RawMessage(`{"default_backend":"anthropic"}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, BackendAnthropic, merged.DefaultBackend)
}

func TestServerConfig_MergeJSON_AnthropicModels(t *testing.T) {
	base := DefaultServerConfig()
	raw := json.RawMessage(`{"anthropic":{"large_model":"claude-opus-4-6","small_model":"claude-haiku-4-5"}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-6", merged.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-4-5", merged.Anthropic.SmallModel)
}

func TestServerConfig_MergeJSON_NotifySMTPEnabledExplicitFalse(t *testing.T) {
	base := DefaultServerConfig()
	base.Notify.SMTP.Enabled = true
	base.Notify.SMTP.Port = 587
	raw := json.RawMessage(`{"notify":{"smtp":{"enabled":false,"port":0}}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.False(t, merged.Notify.SMTP.Enabled)
	assert.Equal(t, 0, merged.Notify.SMTP.Port)
}

func TestServerConfig_MergeJSON_NotifyTeamsEnabledExplicitFalse(t *testing.T) {
	base := DefaultServerConfig()
	base.Notify.Teams.Enabled = true
	raw := json.RawMessage(`{"notify":{"teams":{"enabled":false}}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.False(t, merged.Notify.Teams.Enabled)
}

func TestServerConfig_Merge_PostCompletionCommand(t *testing.T) {
	base := DefaultServerConfig()
	assert.Equal(t, "", base.PostCompletionCommand)

	updates := &ServerConfig{PostCompletionCommand: "echo hello"}
	result := base.Merge(updates)
	assert.Equal(t, "echo hello", result.PostCompletionCommand)

	updates2 := &ServerConfig{PostCompletionCommand: ""}
	result2 := result.Merge(updates2)
	assert.Equal(t, "echo hello", result2.PostCompletionCommand)
}

func TestServerConfig_MergeJSON_PostCompletionCommand_Clear(t *testing.T) {
	base := DefaultServerConfig()
	base.PostCompletionCommand = "echo hello"

	raw := json.RawMessage(`{"post_completion_command": ""}`)
	result, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, "", result.PostCompletionCommand)
}

func TestServerConfig_MergeJSON_OpenRouterAllFields(t *testing.T) {
	base := DefaultServerConfig()
	raw := json.RawMessage(`{"openrouter":{"api_key":"","base_url":"https://custom.ai","large_model":"x/big","small_model":"x/small"}}`)
	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, "", merged.OpenRouter.APIKey)
	assert.Equal(t, "https://custom.ai", merged.OpenRouter.BaseURL)
	assert.Equal(t, "x/big", merged.OpenRouter.LargeModel)
	assert.Equal(t, "x/small", merged.OpenRouter.SmallModel)
}
