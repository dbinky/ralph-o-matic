package executor

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
)

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

func TestClaudeExecutor_BuildCommand(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	cmd := exec.BuildCommand("Write tests for auth module")

	assert.Equal(t, "claude", cmd[0])
	assert.Contains(t, cmd, "--print")
	// Prompt should be passed via stdin, not command line
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
