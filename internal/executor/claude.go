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

// BuildEnv creates the environment variables for Claude Code with Ollama
func (e *ClaudeExecutor) BuildEnv(extra map[string]string) []string {
	env := os.Environ()

	// Ollama configuration
	ollamaEnv := map[string]string{
		"ANTHROPIC_BASE_URL":            e.config.Ollama.Host,
		"ANTHROPIC_AUTH_TOKEN":          "ollama",
		"ANTHROPIC_API_KEY":             "",
		"ANTHROPIC_MODEL":               e.config.LargeModel.Name,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.SmallModel.Name,
	}

	for k, v := range ollamaEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range extra {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// BuildCommand creates the claude command arguments
func (e *ClaudeExecutor) BuildCommand(prompt string) []string {
	return []string{
		"claude",
		"--dangerously-skip-permissions",
	}
}

// ExecutionResult contains the results of running Claude Code
type ExecutionResult struct {
	Output     string
	Iterations int
	Completed  bool
	Error      error
}

// OutputCallback is called for each line of output
type OutputCallback func(line string)

// Execute runs Claude Code with the given prompt
func (e *ClaudeExecutor) Execute(ctx context.Context, workDir, prompt string, env map[string]string, onOutput OutputCallback) (*ExecutionResult, error) {
	cmd := exec.CommandContext(ctx, "claude", "--dangerously-skip-permissions")
	cmd.Dir = workDir
	cmd.Env = e.BuildEnv(env)

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
		Iterations: ParseIterations(output),
		Completed:  ContainsPromise(output, "COMPLETE") || ContainsPromise(output, "DONE"),
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
