package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ClaudeExecutor manages Claude Code subprocess execution
type ClaudeExecutor struct {
	config *models.ServerConfig
}

// NewClaudeExecutor creates a new executor
func NewClaudeExecutor(config *models.ServerConfig) *ClaudeExecutor {
	return &ClaudeExecutor{config: config}
}

// BuildEnv creates the environment variables for Claude Code based on backend
func (e *ClaudeExecutor) BuildEnv(backend models.Backend, extra map[string]string) []string {
	// Filter ANTHROPIC_ vars from inherited environment to avoid duplicates
	// and prevent key leakage between backends
	raw := os.Environ()
	env := make([]string, 0, len(raw))
	for _, e := range raw {
		if !strings.HasPrefix(e, "ANTHROPIC_") {
			env = append(env, e)
		}
	}

	var backendEnv map[string]string

	switch backend {
	case models.BackendAnthropic:
		backendEnv = map[string]string{
			"ANTHROPIC_API_KEY":             e.resolveAnthropicKey(),
			"ANTHROPIC_MODEL":               e.config.Anthropic.LargeModel,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.Anthropic.SmallModel,
		}
	default: // ollama
		backendEnv = map[string]string{
			"ANTHROPIC_BASE_URL":            e.config.Ollama.Host,
			"ANTHROPIC_AUTH_TOKEN":          "ollama",
			"ANTHROPIC_API_KEY":             "",
			"ANTHROPIC_MODEL":               e.config.LargeModel.Name,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.SmallModel.Name,
		}
	}

	for k, v := range backendEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range extra {
		// Defense-in-depth: skip dangerous env var prefixes
		if isDeniedEnvVar(k) {
			continue
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// envVarDenylist contains environment variable names and prefixes that should
// not be set by job env for security reasons.
var envVarDenylist = []string{
	"LD_",       // Linux dynamic linker
	"DYLD_",     // macOS dynamic linker
	"PATH",      // executable search path
	"HOME",      // home directory
	"SHELL",     // shell executable
	"ANTHROPIC_", // Anthropic API config
	"CLAUDE_",   // Claude CLI config
}

// isDeniedEnvVar checks if an env var name is on the denylist.
func isDeniedEnvVar(key string) bool {
	upperKey := strings.ToUpper(key)
	for _, denied := range envVarDenylist {
		if strings.HasPrefix(upperKey, denied) || upperKey == denied {
			return true
		}
	}
	return false
}

// resolveAnthropicKey returns the API key, preferring env var over config
func (e *ClaudeExecutor) resolveAnthropicKey() string {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return key
	}
	return e.config.Anthropic.APIKey
}

// ExecutionResult contains the results of running Claude Code
type ExecutionResult struct {
	Output     string
	RawJSON    []byte             // raw JSON from claude --output-format json
	Iterations int
	Completed  bool
	SessionID  string             // extracted from JSON output
	Metadata   *ResponseMetadata  // parsed response analysis
	Error      error
}

// OutputCallback is called for each line of output
type OutputCallback func(line string)

// Execute runs Claude Code with the given prompt.
// For the ollama backend, --dangerously-skip-permissions is enabled by default.
// For the anthropic backend, it is OFF by default due to the elevated risk of
// unattended code execution with a frontier model.
// If session is non-nil and valid, --resume is passed for continuity.
func (e *ClaudeExecutor) Execute(ctx context.Context, workDir, prompt string, backend models.Backend, env map[string]string, session *Session, onOutput OutputCallback) (*ExecutionResult, error) {
	skipPerms := backend != models.BackendAnthropic
	args := buildClaudeArgs(skipPerms, session)
	cmd := exec.CommandContext(ctx, "claude", args...) //nolint:gosec // claude is a trusted CLI tool
	cmd.Dir = workDir
	cmd.Env = e.BuildEnv(backend, env)

	// Pass prompt via stdin
	cmd.Stdin = strings.NewReader(prompt)

	// Capture output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Read output in goroutines
	var outputBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		e.readOutput(stdout, &outputBuf, onOutput)
	}()

	go func() {
		defer wg.Done()
		e.readOutput(stderr, &stderrBuf, func(line string) {
			if onOutput != nil {
				onOutput("[stderr] " + line)
			}
		})
	}()

	wg.Wait()

	err = cmd.Wait()

	output := outputBuf.String()
	if err != nil {
		errDetail := stderrBuf.String()
		if errDetail != "" {
			return nil, fmt.Errorf("claude exited with error: %w\nstderr: %s", err, errDetail)
		}
		return nil, fmt.Errorf("claude exited with error: %w", err)
	}

	result := &ExecutionResult{
		Output:     output,
		RawJSON:    outputBuf.Bytes(),
		Iterations: ParseIterations(output),
		Completed:  ContainsPromise(output, "COMPLETE") || ContainsPromise(output, "DONE"),
	}

	// Parse JSON response for metadata
	if meta, parseErr := ParseResponse(outputBuf.Bytes()); parseErr == nil {
		result.Metadata = meta
		result.SessionID = meta.SessionID
		if meta.Completed || meta.ExitSignal {
			result.Completed = true
		}
	}

	return result, nil
}

func (e *ClaudeExecutor) readOutput(r io.Reader, buf *bytes.Buffer, callback OutputCallback) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line + "\n")
		if callback != nil {
			callback(line)
		}
	}
}

// ParseIterations extracts the current iteration number from output
func ParseIterations(output string) int {
	// Look for patterns like "[iteration 5]" or "Iteration: 5"
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\[iteration\s+(\d+)\]`),
		regexp.MustCompile(`Iteration:\s*(\d+)`),
		regexp.MustCompile(`iter\s+(\d+)`),
	}

	maxIter := 0
	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(output, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				if iter, err := strconv.Atoi(match[1]); err == nil && iter > maxIter {
					maxIter = iter
				}
			}
		}
	}

	return maxIter
}

// ContainsPromise checks if output contains a promise tag with the given text
func ContainsPromise(output, promiseText string) bool {
	pattern := fmt.Sprintf(`<promise>%s</promise>`, regexp.QuoteMeta(promiseText))
	matched, _ := regexp.MatchString(pattern, output)
	return matched
}

// IsClaudeInstalled checks if claude CLI is available
func IsClaudeInstalled() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}
