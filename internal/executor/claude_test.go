package executor

import (
	"os"
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
	assert.Contains(t, env, "ANTHROPIC_MODEL=devstral")
	assert.Contains(t, env, "ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3:8b")
	assert.Contains(t, env, "CUSTOM=value")
	// Always force standard context so Max-plan accounts don't auto-upgrade to
	// the 1M window, which fails headless jobs with a usage-credits error.
	assert.Contains(t, env, "CLAUDE_CODE_DISABLE_1M_CONTEXT=1")
}

func TestClaudeExecutor_BuildEnv_Disables1MContextForAnthropic(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendAnthropic, nil)

	assert.Contains(t, env, "CLAUDE_CODE_DISABLE_1M_CONTEXT=1")
}

func TestClaudeExecutor_BuildEnv_1MContextEnabledWhenConfigured(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Disable1MContext = false
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendAnthropic, nil)

	assert.NotContains(t, env, "CLAUDE_CODE_DISABLE_1M_CONTEXT=1")
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

func TestContainsPromise_FINIT(t *testing.T) {
	output := "All tests passing!\n<promise>FINIT</promise>\n"
	assert.True(t, ContainsPromise(output, "FINIT"))
	assert.False(t, ContainsPromise(output, "COMPLETE"))
}

func TestClaudeExecutor_ParseOutput_NoPromise(t *testing.T) {
	output := "Still working on tests..."

	assert.False(t, ContainsPromise(output, "COMPLETE"))
}

func TestHasNonExitPromise(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		exitPromise string
		want        bool
	}{
		{
			name:        "CLOSER is progress when exit is FINIT",
			output:      "Fixed.\n<promise>CLOSER</promise>\n",
			exitPromise: "FINIT",
			want:        true,
		},
		{
			name:        "REVIEW COMPLETE is progress when exit is FINIT",
			output:      "Done reviewing.\n<promise>REVIEW COMPLETE</promise>\n",
			exitPromise: "FINIT",
			want:        true,
		},
		{
			name:        "exit promise only — no progress",
			output:      "Done!\n<promise>FINIT</promise>\n",
			exitPromise: "FINIT",
			want:        false,
		},
		{
			name:        "no promise tags at all",
			output:      "Still working on things...",
			exitPromise: "FINIT",
			want:        false,
		},
		{
			name:        "mixed: exit + non-exit",
			output:      "<promise>CLOSER</promise>\n<promise>FINIT</promise>\n",
			exitPromise: "FINIT",
			want:        true,
		},
		{
			name:        "COMPLETE is progress when exit is DONE",
			output:      "<promise>COMPLETE</promise>",
			exitPromise: "DONE",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasNonExitPromise(tt.output, tt.exitPromise)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainsPromise_FalsePositive_StreamJSON(t *testing.T) {
	// Simulates stream-json output where the model discusses FINIT in reasoning
	// but only outputs CLOSER as the actual promise. The intermediate assistant
	// event contains the exit promise text in the model's reasoning.
	streamJSON := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Checking the focus areas... not all reviews are complete, so I'll output <promise>CLOSER</promise> instead of <promise>FINIT</promise>."}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/focus-areas.md"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"<promise>CLOSER</promise>"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"<promise>CLOSER</promise>","session_id":"abc123"}`,
	}, "\n")

	// BUG: ContainsPromise searches the ENTIRE stream-json buffer, including
	// intermediate assistant events where the model quotes the exit promise.
	// This should be false because the model's actual output is CLOSER, not FINIT.
	assert.True(t, ContainsPromise(streamJSON, "FINIT"),
		"CURRENT BEHAVIOR: ContainsPromise matches FINIT in intermediate reasoning text")

	// The model's actual result only contains CLOSER
	assert.True(t, ContainsPromise(streamJSON, "CLOSER"))

	// What we WANT: only check the result event's text, not intermediate events
	meta, err := ParseResponse([]byte(streamJSON))
	assert.NoError(t, err)
	assert.False(t, ContainsPromise(meta.ResultText, "FINIT"),
		"DESIRED BEHAVIOR: checking only result text should NOT match FINIT")
	assert.True(t, ContainsPromise(meta.ResultText, "CLOSER"),
		"DESIRED BEHAVIOR: checking only result text SHOULD match CLOSER")
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
	cfg.Anthropic.LargeModel = "claude-opus-4-5-20251101"
	cfg.Anthropic.SmallModel = "claude-haiku-4-5-20251001"
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendAnthropic, nil)

	envMap := envToMap(env)
	assert.Equal(t, "claude-opus-4-5-20251101", envMap["ANTHROPIC_MODEL"])
	assert.Equal(t, "claude-haiku-4-5-20251001", envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	// No API key — Claude Code handles its own auth
	_, hasAPIKey := envMap["ANTHROPIC_API_KEY"]
	assert.False(t, hasAPIKey)
	_, hasBaseURL := envMap["ANTHROPIC_BASE_URL"]
	assert.False(t, hasBaseURL)
}

func TestClaudeExecutor_BuildEnv_OpenRouter(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.OpenRouter.APIKey = "sk-or-v1-test-key"
	cfg.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOpenRouter, nil)

	envMap := envToMap(env)
	assert.Equal(t, "https://openrouter.ai/api/v1", envMap["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "sk-or-v1-test-key", envMap["ANTHROPIC_AUTH_TOKEN"])
	assert.Equal(t, "moonshotai/kimi-k2.5", envMap["ANTHROPIC_MODEL"])
	assert.Equal(t, "mistralai/devstral-2-2512", envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
}

func TestClaudeExecutor_BuildEnv_Ollama_Unchanged(t *testing.T) {
	cfg := models.DefaultServerConfig()
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOllama, map[string]string{"CUSTOM": "value"})

	envMap := envToMap(env)
	assert.Equal(t, "http://localhost:11434", envMap["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "ollama", envMap["ANTHROPIC_AUTH_TOKEN"])
	assert.Equal(t, "devstral", envMap["ANTHROPIC_MODEL"])
	assert.Equal(t, "qwen3:8b", envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	assert.Equal(t, "value", envMap["CUSTOM"])
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

func TestResolveClaudeBinary_FindsAbsolutePath(t *testing.T) {
	path := resolveClaudeBinary()
	// In a dev environment, claude should be found
	// The resolved path should be absolute (not bare "claude")
	if path != "claude" {
		assert.True(t, strings.HasPrefix(path, "/"), "resolved path should be absolute: %s", path)
	}
}

func TestAugmentPath_AddsExistingDirs(t *testing.T) {
	env := []string{"HOME=/tmp", "PATH=/usr/bin:/bin"}
	result := augmentPath(env)

	// Should still contain original PATH entries
	var pathVal string
	for _, e := range result {
		if strings.HasPrefix(e, "PATH=") {
			pathVal = strings.TrimPrefix(e, "PATH=")
			break
		}
	}
	assert.Contains(t, pathVal, "/usr/bin")
	assert.Contains(t, pathVal, "/bin")
	// /usr/local/bin exists on macOS/Linux, should be added
	if _, err := os.Stat("/usr/local/bin"); err == nil {
		assert.Contains(t, pathVal, "/usr/local/bin")
	}
}

func TestAugmentPath_NoDuplicates(t *testing.T) {
	// PATH already contains /usr/local/bin — should not duplicate
	env := []string{"PATH=/usr/bin:/usr/local/bin:/bin"}
	result := augmentPath(env)

	var pathVal string
	for _, e := range result {
		if strings.HasPrefix(e, "PATH=") {
			pathVal = strings.TrimPrefix(e, "PATH=")
			break
		}
	}
	// Count occurrences of /usr/local/bin
	count := strings.Count(pathVal, "/usr/local/bin")
	assert.Equal(t, 1, count, "should not duplicate existing dirs")
}

func TestAugmentPath_NoPATH(t *testing.T) {
	// If there's no PATH entry, augmentPath should not crash
	env := []string{"HOME=/tmp", "CUSTOM=value"}
	result := augmentPath(env)
	assert.Equal(t, env, result)
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
