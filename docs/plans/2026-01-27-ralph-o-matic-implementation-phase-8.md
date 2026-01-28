# Phase 8: CLI

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the cross-platform CLI with all commands for job submission, queue management, and configuration.

**Architecture:** Standard Go CLI using cobra for command parsing. HTTP client for server communication. YAML config file support.

**Tech Stack:** Go 1.22+, cobra CLI framework, YAML config

**Dependencies:** Phase 7 must be complete (dashboard)

---

## Task 1: Add CLI Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add cobra and yaml**

Run:
```bash
go get github.com/spf13/cobra
go get gopkg.in/yaml.v3
```

**Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add cobra CLI framework and yaml support"
```

---

## Task 2: Implement CLI Config

**Files:**
- Create: `internal/cli/config.go`
- Create: `internal/cli/config_test.go`

**Step 1: Write the tests**

Create `internal/cli/config_test.go`:

```go
package cli

import (
	"os"
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
```

**Step 2: Write implementation**

Create `internal/cli/config.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config holds CLI configuration
type Config struct {
	Server               string `yaml:"server"`
	DefaultPriority      string `yaml:"default_priority"`
	DefaultMaxIterations int    `yaml:"default_max_iterations"`
}

// DefaultConfig returns a config with defaults
func DefaultConfig() *Config {
	return &Config{
		Server:               "http://localhost:9090",
		DefaultPriority:      "normal",
		DefaultMaxIterations: 50,
	}
}

// ConfigPath returns the default config file path
func ConfigPath() string {
	var configDir string

	switch runtime.GOOS {
	case "windows":
		configDir = os.Getenv("APPDATA")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	default:
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}

	return filepath.Join(configDir, "ralph-o-matic", "config.yaml")
}

// LoadConfig loads config from file, returning defaults if not found
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// SaveConfig saves config to file
func SaveConfig(path string, cfg *Config) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Merge returns a new config with non-zero values from other applied
func (c *Config) Merge(other *Config) *Config {
	result := *c

	if other.Server != "" {
		result.Server = other.Server
	}
	if other.DefaultPriority != "" {
		result.DefaultPriority = other.DefaultPriority
	}
	if other.DefaultMaxIterations > 0 {
		result.DefaultMaxIterations = other.DefaultMaxIterations
	}

	return &result
}
```

**Step 3: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go
git commit -m "feat(cli): add config file support"
```

---

## Task 3: Implement HTTP Client

**Files:**
- Create: `internal/cli/client.go`
- Create: `internal/cli/client_test.go`

**Step 1: Write the tests**

Create `internal/cli/client_test.go`:

```go
package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/jobs", r.URL.Path)

		resp := map[string]interface{}{
			"jobs":  []*models.Job{},
			"total": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	jobs, total, err := client.GetJobs(nil)

	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Len(t, jobs, 0)
}

func TestClient_CreateJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/jobs", r.URL.Path)

		job := &models.Job{ID: 1}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(job)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	job, err := client.CreateJob(&CreateJobRequest{
		RepoURL:       "git@github.com:user/repo.git",
		Branch:        "main",
		Prompt:        "test",
		MaxIterations: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), job.ID)
}

func TestClient_PauseJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/jobs/1/pause", r.URL.Path)

		job := &models.Job{ID: 1, Status: models.StatusPaused}
		json.NewEncoder(w).Encode(job)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	job, err := client.PauseJob(1)

	require.NoError(t, err)
	assert.Equal(t, models.StatusPaused, job.Status)
}
```

**Step 2: Write implementation**

Create `internal/cli/client.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// Client communicates with the ralph-o-matic server
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new API client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// CreateJobRequest is the request for creating a job
type CreateJobRequest struct {
	RepoURL       string            `json:"repo_url"`
	Branch        string            `json:"branch"`
	Prompt        string            `json:"prompt"`
	MaxIterations int               `json:"max_iterations"`
	Priority      string            `json:"priority,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
}

// GetJobs retrieves jobs from the server
func (c *Client) GetJobs(statuses []string) ([]*models.Job, int, error) {
	path := "/api/jobs"
	if len(statuses) > 0 {
		path += "?status=" + url.QueryEscape(strings.Join(statuses, ","))
	}

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}

	if err := c.get(path, &resp); err != nil {
		return nil, 0, err
	}

	return resp.Jobs, resp.Total, nil
}

// GetJob retrieves a single job
func (c *Client) GetJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.get(fmt.Sprintf("/api/jobs/%d", id), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// CreateJob creates a new job
func (c *Client) CreateJob(req *CreateJobRequest) (*models.Job, error) {
	var job models.Job
	if err := c.post("/api/jobs", req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// CancelJob cancels a job
func (c *Client) CancelJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.delete(fmt.Sprintf("/api/jobs/%d", id), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// PauseJob pauses a running job
func (c *Client) PauseJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.post(fmt.Sprintf("/api/jobs/%d/pause", id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ResumeJob resumes a paused job
func (c *Client) ResumeJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.post(fmt.Sprintf("/api/jobs/%d/resume", id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ReorderJobs reorders the queue
func (c *Client) ReorderJobs(jobIDs []int64) error {
	req := map[string][]int64{"job_ids": jobIDs}
	return c.put("/api/jobs/order", req, nil)
}

// GetConfig retrieves server config
func (c *Client) GetConfig() (*models.ServerConfig, error) {
	var cfg models.ServerConfig
	if err := c.get("/api/config", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateConfig updates server config
func (c *Client) UpdateConfig(updates map[string]interface{}) (*models.ServerConfig, error) {
	var cfg models.ServerConfig
	if err := c.patch("/api/config", updates, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetLogs retrieves logs for a job
func (c *Client) GetLogs(jobID int64) ([]map[string]interface{}, error) {
	var resp struct {
		Logs []map[string]interface{} `json:"logs"`
	}
	if err := c.get(fmt.Sprintf("/api/jobs/%d/logs", jobID), &resp); err != nil {
		return nil, err
	}
	return resp.Logs, nil
}

// Ping checks if server is reachable
func (c *Client) Ping() error {
	return c.get("/health", nil)
}

func (c *Client) get(path string, result interface{}) error {
	return c.request("GET", path, nil, result)
}

func (c *Client) post(path string, body, result interface{}) error {
	return c.request("POST", path, body, result)
}

func (c *Client) put(path string, body, result interface{}) error {
	return c.request("PUT", path, body, result)
}

func (c *Client) patch(path string, body, result interface{}) error {
	return c.request("PATCH", path, body, result)
}

func (c *Client) delete(path string, result interface{}) error {
	return c.request("DELETE", path, nil, result)
}

func (c *Client) request(method, path string, body, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("server error: %s", errResp.Error)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
```

**Step 3: Commit**

```bash
git add internal/cli/client.go internal/cli/client_test.go
git commit -m "feat(cli): add HTTP client for server communication"
```

---

## Task 4: Implement CLI Commands

**Files:**
- Create: `cmd/cli/main.go`
- Create: `cmd/cli/commands.go`

**Step 1: Create main.go**

Create `cmd/cli/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/ryan/ralph-o-matic/internal/cli"
)

var (
	cfg    *cli.Config
	client *cli.Client
)

func main() {
	var err error
	cfg, err = cli.LoadConfig(cli.ConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = cli.DefaultConfig()
	}

	client = cli.NewClient(cfg.Server)

	rootCmd := &cobra.Command{
		Use:   "ralph-o-matic",
		Short: "Ralph-o-matic CLI - submit and manage ralph loop jobs",
	}

	rootCmd.AddCommand(
		submitCmd(),
		statusCmd(),
		logsCmd(),
		cancelCmd(),
		pauseCmd(),
		resumeCmd(),
		moveCmd(),
		configCmd(),
		serverConfigCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

**Step 2: Create commands.go**

Create `cmd/cli/commands.go`:

```go
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ryan/ralph-o-matic/internal/cli"
)

func submitCmd() *cobra.Command {
	var prompt, priority, workingDir string
	var maxIterations int
	var openEnded bool

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a new job to the queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get repo info from git
			repoURL, branch, err := getGitInfo()
			if err != nil {
				return fmt.Errorf("failed to get git info: %w", err)
			}

			// Resolve prompt
			if prompt == "" {
				prompt, err = readPromptFile(workingDir)
				if err != nil {
					return fmt.Errorf("no prompt provided and RALPH.md not found")
				}
			}

			if priority == "" {
				priority = cfg.DefaultPriority
			}
			if maxIterations == 0 {
				maxIterations = cfg.DefaultMaxIterations
			}

			req := &cli.CreateJobRequest{
				RepoURL:       repoURL,
				Branch:        branch,
				Prompt:        prompt,
				MaxIterations: maxIterations,
				Priority:      priority,
				WorkingDir:    workingDir,
			}

			fmt.Println("Submitting job...")
			fmt.Printf("  Repository:    %s\n", repoURL)
			fmt.Printf("  Branch:        %s\n", branch)
			fmt.Printf("  Max iterations: %d\n", maxIterations)
			fmt.Printf("  Priority:      %s\n", priority)

			job, err := client.CreateJob(req)
			if err != nil {
				return err
			}

			fmt.Printf("\n✓ Job #%d queued (position: %d)\n", job.ID, job.Position)
			fmt.Printf("\nDashboard: %s/jobs/%d\n", cfg.Server, job.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text (overrides RALPH.md)")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority: high, normal, low")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Max iterations")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory")
	cmd.Flags().BoolVar(&openEnded, "open-ended", false, "Use open-ended prompt")

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [job-id]",
		Short: "Show queue status or specific job details",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Show specific job
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid job ID")
				}

				job, err := client.GetJob(id)
				if err != nil {
					return err
				}

				printJobDetail(job)
				return nil
			}

			// Show queue overview
			jobs, _, err := client.GetJobs(nil)
			if err != nil {
				return err
			}

			printQueueOverview(jobs)
			return nil
		},
	}
}

func logsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <job-id>",
		Short: "View job logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			logs, err := client.GetLogs(id)
			if err != nil {
				return err
			}

			for _, log := range logs {
				fmt.Printf("[iter %v] %v\n", log["iteration"], log["message"])
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs in real-time")
	return cmd
}

func cancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			job, err := client.CancelJob(id)
			if err != nil {
				return err
			}

			fmt.Printf("✓ Job #%d cancelled\n", job.ID)
			return nil
		},
	}
}

func pauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <job-id>",
		Short: "Pause a running job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			job, err := client.PauseJob(id)
			if err != nil {
				return err
			}

			fmt.Printf("✓ Job #%d paused at iteration %d\n", job.ID, job.Iteration)
			return nil
		},
	}
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <job-id>",
		Short: "Resume a paused job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			job, err := client.ResumeJob(id)
			if err != nil {
				return err
			}

			fmt.Printf("✓ Job #%d resumed\n", job.ID)
			return nil
		},
	}
}

func moveCmd() *cobra.Command {
	var position int
	var after int64
	var first bool

	cmd := &cobra.Command{
		Use:   "move <job-id>",
		Short: "Move job in queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			// Get current queue
			jobs, _, err := client.GetJobs([]string{"queued"})
			if err != nil {
				return err
			}

			// Build new order
			var newOrder []int64
			for _, j := range jobs {
				if j.ID != id {
					newOrder = append(newOrder, j.ID)
				}
			}

			// Insert at new position
			if first {
				newOrder = append([]int64{id}, newOrder...)
			} else if position > 0 {
				pos := position - 1
				if pos > len(newOrder) {
					pos = len(newOrder)
				}
				newOrder = append(newOrder[:pos], append([]int64{id}, newOrder[pos:]...)...)
			} else {
				newOrder = append(newOrder, id)
			}

			if err := client.ReorderJobs(newOrder); err != nil {
				return err
			}

			fmt.Printf("✓ Job #%d moved\n", id)
			return nil
		},
	}

	cmd.Flags().IntVar(&position, "position", 0, "Move to specific position")
	cmd.Flags().Int64Var(&after, "after", 0, "Move after another job")
	cmd.Flags().BoolVar(&first, "first", false, "Move to front of queue")
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [set <key> <value>]",
		Short: "Show or set CLI configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Printf("server: %s\n", cfg.Server)
				fmt.Printf("default_priority: %s\n", cfg.DefaultPriority)
				fmt.Printf("default_max_iterations: %d\n", cfg.DefaultMaxIterations)
				return nil
			}

			if len(args) >= 3 && args[0] == "set" {
				key := args[1]
				value := args[2]

				switch key {
				case "server":
					cfg.Server = value
				case "default_priority":
					cfg.DefaultPriority = value
				case "default_max_iterations":
					v, _ := strconv.Atoi(value)
					cfg.DefaultMaxIterations = v
				default:
					return fmt.Errorf("unknown config key: %s", key)
				}

				return cli.SaveConfig(cli.ConfigPath(), cfg)
			}

			return nil
		},
	}
	return cmd
}

func serverConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server-config [set <key> <value>]",
		Short: "Show or set server configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cfg, err := client.GetConfig()
				if err != nil {
					return err
				}

				fmt.Printf("large_model: %s\n", cfg.LargeModel)
				fmt.Printf("small_model: %s\n", cfg.SmallModel)
				fmt.Printf("default_max_iterations: %d\n", cfg.DefaultMaxIterations)
				fmt.Printf("concurrent_jobs: %d\n", cfg.ConcurrentJobs)
				return nil
			}

			if len(args) >= 3 && args[0] == "set" {
				updates := map[string]interface{}{
					args[1]: args[2],
				}

				_, err := client.UpdateConfig(updates)
				return err
			}

			return nil
		},
	}
	return cmd
}

// Helper functions

func getGitInfo() (string, string, error) {
	// TODO: Implement using git commands
	return "", "", fmt.Errorf("not implemented")
}

func readPromptFile(workingDir string) (string, error) {
	paths := []string{
		"RALPH.md",
	}
	if workingDir != "" {
		paths = append([]string{workingDir + "/RALPH.md"}, paths...)
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
	}

	return "", fmt.Errorf("RALPH.md not found")
}

func printQueueOverview(jobs []*models.Job) {
	fmt.Println("Ralph-o-matic Queue")
	fmt.Println(strings.Repeat("═", 40))

	// Group by status
	var running, paused, queued []*models.Job
	for _, j := range jobs {
		switch j.Status {
		case "running":
			running = append(running, j)
		case "paused":
			paused = append(paused, j)
		case "queued":
			queued = append(queued, j)
		}
	}

	if len(running) > 0 {
		fmt.Println("\n▶ RUNNING")
		for _, j := range running {
			fmt.Printf("  #%d %s    iter %d/%d\n", j.ID, j.Branch, j.Iteration, j.MaxIterations)
		}
	}

	if len(paused) > 0 {
		fmt.Println("\n⏸ PAUSED")
		for _, j := range paused {
			fmt.Printf("  #%d %s    iter %d/%d\n", j.ID, j.Branch, j.Iteration, j.MaxIterations)
		}
	}

	if len(queued) > 0 {
		fmt.Printf("\n⏳ QUEUED (%d)\n", len(queued))
		for _, j := range queued {
			fmt.Printf("  #%d %s    %s\n", j.ID, j.Branch, j.Priority)
		}
	}

	fmt.Printf("\nDashboard: %s\n", cfg.Server)
}

func printJobDetail(job *models.Job) {
	fmt.Printf("Job #%d\n", job.ID)
	fmt.Printf("  Branch:     %s\n", job.Branch)
	fmt.Printf("  Status:     %s\n", job.Status)
	fmt.Printf("  Iteration:  %d/%d\n", job.Iteration, job.MaxIterations)
	fmt.Printf("  Priority:   %s\n", job.Priority)
	if job.PRURL != "" {
		fmt.Printf("  PR:         %s\n", job.PRURL)
	}
}
```

**Step 3: Commit**

```bash
git add cmd/cli/main.go cmd/cli/commands.go
git commit -m "feat(cli): add CLI with all commands"
```

---

## Phase 8 Completion Checklist

- [ ] CLI config file support
- [ ] HTTP client for API communication
- [ ] Submit command
- [ ] Status command (overview and detail)
- [ ] Logs command
- [ ] Cancel/pause/resume commands
- [ ] Move command for reordering
- [ ] Config commands (local and server)
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 9 - Install Scripts
