package executor

import (
	"strings"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestClaudeExecutor_BuildEnv(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOllama, map[string]string{"CUSTOM": "value"})

	// Should contain Ollama config from ServerConfig
	assert.Contains(t, env, "ANTHROPIC_BASE_URL=http://localhost:11434")
	assert.Contains(t, env, "ANTHROPIC_AUTH_TOKEN=ollama")
	assert.Contains(t, env, "ANTHROPIC_API_KEY=")
	assert.Contains(t, env, "ANTHROPIC_MODEL=devstral")
	assert.Contains(t, env, "ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3:8b")
	assert.Contains(t, env, "CUSTOM=value")
}

func TestClaudeExecutor_BuildEnv_RemoteOllama(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Ollama.Host = "http://192.168.1.50:11434"
	cfg.Ollama.IsRemote = true
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOllama, nil)

	assert.Contains(t, env, "ANTHROPIC_BASE_URL=http://192.168.1.50:11434")
}

func TestClaudeExecutor_ParseOutput_Iteration(t *testing.T) {
	output := `[iteration 5] Running tests...
[iteration 5] Tests failed: 3 errors
[iteration 5] Fixing auth.go`

	iterations := ParseIterations(output)
	assert.Equal(t, 5, iterations)
}

func TestClaudeExecutor_ParseOutput_Promise(t *testing.T) {
	output := `All tests passing!
<promise>COMPLETE</promise>`

	assert.True(t, ContainsPromise(output, "COMPLETE"))
	assert.False(t, ContainsPromise(output, "DONE"))
}

func TestClaudeExecutor_ParseOutput_NoPromise(t *testing.T) {
	output := "Still working on tests..."

	assert.False(t, ContainsPromise(output, "COMPLETE"))
}

func TestClaudeExecutor_BuildEnv_CustomModels(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.LargeModel.Name = "my-custom:70b"
	cfg.SmallModel.Name = "my-helper:1.5b"
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOllama, nil)

	assert.Contains(t, env, "ANTHROPIC_MODEL=my-custom:70b")
	assert.Contains(t, env, "ANTHROPIC_DEFAULT_HAIKU_MODEL=my-helper:1.5b")
}

func TestClaudeExecutor_BuildEnv_NilExtra(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	// Should not panic with nil extra map
	env := exec.BuildEnv(models.BackendOllama, nil)
	assert.NotEmpty(t, env)
	assert.Contains(t, env, "ANTHROPIC_AUTH_TOKEN=ollama")
}

func TestClaudeExecutor_BuildEnv_DevicePlacement(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.LargeModel.Device = "gpu"
	cfg.SmallModel.Device = "cpu"
	exec := NewClaudeExecutor(cfg)

	// Device placement doesn't affect env vars, just verify no panic
	env := exec.BuildEnv(models.BackendOllama, nil)
	assert.Contains(t, env, "ANTHROPIC_MODEL=devstral")
}

func TestClaudeExecutor_BuildEnv_EmptyHost(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Ollama.Host = ""
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOllama, nil)
	assert.Contains(t, env, "ANTHROPIC_BASE_URL=")
}

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

func TestClaudeExecutor_BuildEnv_DeniedEnvVarsFiltered(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	// These should be filtered out as defense-in-depth
	extra := map[string]string{
		"LD_PRELOAD":            "/tmp/evil.so",
		"DYLD_INSERT_LIBRARIES": "/tmp/evil.dylib",
		"PATH":                  "/tmp/evil",
		"HOME":                  "/tmp/evil",
		"SHELL":                 "/tmp/evil",
		"ANTHROPIC_API_KEY":     "stolen-key",
		"CLAUDE_CONFIG":         "/tmp/evil",
		"SAFE_VAR":              "allowed",
	}

	env := exec.BuildEnv(models.BackendOllama, extra)
	envMap := envToMap(env)

	// Denied vars should not appear with the injected values
	assert.NotEqual(t, "/tmp/evil.so", envMap["LD_PRELOAD"])
	assert.NotEqual(t, "/tmp/evil.dylib", envMap["DYLD_INSERT_LIBRARIES"])
	assert.NotEqual(t, "/tmp/evil", envMap["PATH"])
	assert.NotEqual(t, "/tmp/evil", envMap["HOME"])
	assert.NotEqual(t, "/tmp/evil", envMap["SHELL"])
	assert.NotEqual(t, "stolen-key", envMap["ANTHROPIC_API_KEY"])
	assert.NotEqual(t, "/tmp/evil", envMap["CLAUDE_CONFIG"])

	// Safe vars should be present
	assert.Equal(t, "allowed", envMap["SAFE_VAR"])
}
