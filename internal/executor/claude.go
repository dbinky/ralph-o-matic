package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ClaudeExecutor manages Claude Code subprocess execution
type ClaudeExecutor struct {
	config     *models.ServerConfig
	claudePath string // absolute path to claude binary
}

// NewClaudeExecutor creates a new executor with automatic binary resolution
func NewClaudeExecutor(config *models.ServerConfig) *ClaudeExecutor {
	return &ClaudeExecutor{
		config:     config,
		claudePath: resolveClaudeBinary(),
	}
}

// resolveClaudeBinary finds the claude CLI binary by checking PATH first,
// then common installation directories. This handles launchd/systemd services
// that run with a minimal PATH (e.g. /usr/bin:/bin:/usr/sbin:/sbin).
func resolveClaudeBinary() string {
	// Try PATH first (works when run interactively or with full PATH)
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}

	// Search common installation directories
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Fall back to bare name — will produce a clear error at exec time
	return "claude"
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
			"ANTHROPIC_MODEL":               e.config.Anthropic.LargeModel,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.Anthropic.SmallModel,
		}
	default: // ollama
		backendEnv = map[string]string{
			"ANTHROPIC_BASE_URL":            e.config.Ollama.Host,
			"ANTHROPIC_AUTH_TOKEN":          "ollama",
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

	// Augment PATH so child processes can find git, gh, etc.
	// even when the server runs under launchd/systemd with a minimal PATH.
	env = augmentPath(env)

	return env
}

// augmentPath ensures common binary directories are in PATH for child processes.
func augmentPath(env []string) []string {
	home, _ := os.UserHomeDir()
	extraDirs := []string{
		filepath.Join(home, ".local", "bin"),
		"/usr/local/bin",
		"/opt/homebrew/bin",
	}

	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			current := strings.TrimPrefix(e, "PATH=")
			for _, dir := range extraDirs {
				if !strings.Contains(current, dir) {
					if _, err := os.Stat(dir); err == nil {
						current += ":" + dir
					}
				}
			}
			env[i] = "PATH=" + current
			return env
		}
	}

	return env
}

// envVarDenylist contains environment variable names and prefixes that should
// not be set by job env for security reasons.
var envVarDenylist = []string{
	"LD_",        // Linux dynamic linker
	"DYLD_",      // macOS dynamic linker
	"PATH",       // executable search path
	"HOME",       // home directory
	"SHELL",      // shell executable
	"ANTHROPIC_", // Anthropic API config
	"CLAUDE_",    // Claude CLI config
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

// ExecutionResult contains the results of running Claude Code
type ExecutionResult struct {
	Output     string
	RawJSON    []byte // raw JSON from claude --output-format json
	Iterations int
	Completed  bool
	SessionID  string            // extracted from JSON output
	Metadata   *ResponseMetadata // parsed response analysis
	Error      error
}

// OutputCallback is called for each line of output
type OutputCallback func(line string)

// Execute runs Claude Code with the given prompt.
// Permissions are always skipped because --print mode is automated
// with no human to approve interactive prompts.
// If session is non-nil and valid, --resume is passed for continuity.
func (e *ClaudeExecutor) Execute(ctx context.Context, workDir, prompt string, backend models.Backend, env map[string]string, session *Session, exitPromise string, onOutput OutputCallback) (*ExecutionResult, error) {
	args := buildClaudeArgs(true, session)
	cmd := exec.CommandContext(ctx, e.claudePath, args...) //nolint:gosec // claude is a trusted CLI tool
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
		Completed:  ContainsPromise(output, exitPromise),
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
	// stream-json events can be large (tool results with file contents);
	// increase from default 64KB to 1MB per line.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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

// promiseTagPattern matches <promise>X</promise> tags in output.
var promiseTagPattern = regexp.MustCompile(`<promise>(.*?)</promise>`)

// HasNonExitPromise returns true if output contains any <promise>X</promise>
// tag where X is NOT the exit promise. These tags are progress signals
// from the ralph loop script (e.g. CLOSER, REVIEW COMPLETE).
func HasNonExitPromise(output, exitPromise string) bool {
	matches := promiseTagPattern.FindAllStringSubmatch(output, -1)
	for _, match := range matches {
		if len(match) >= 2 && match[1] != exitPromise {
			return true
		}
	}
	return false
}

// IsClaudeInstalled checks if claude CLI is available
func IsClaudeInstalled() bool {
	p := resolveClaudeBinary()
	return p != "claude" // resolved to an absolute path
}
