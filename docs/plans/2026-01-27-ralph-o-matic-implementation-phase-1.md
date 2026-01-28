# Phase 1: Project Setup & Core Models

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Initialize the Go project with proper structure, dependencies, and core data models with full test coverage.

**Architecture:** Standard Go project layout with cmd/ for binaries and internal/ for private packages. Models are pure structs with validation methods. All code is test-driven.

**Tech Stack:** Go 1.22+, testify for assertions, no external dependencies for models.

---

## Task 1: Initialize Go Module

**Files:**
- Create: `go.mod`
- Create: `go.sum`

**Step 1: Create the Go module**

Run:
```bash
cd /Users/ryan/Repos/RalphFactory
go mod init github.com/ryan/ralph-o-matic
```

Expected: `go.mod` created with module name

**Step 2: Add testify dependency**

Run:
```bash
go get github.com/stretchr/testify
```

Expected: `go.sum` created, testify added to `go.mod`

**Step 3: Verify module**

Run:
```bash
cat go.mod
```

Expected output contains:
```
module github.com/ryan/ralph-o-matic

go 1.22
```

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: initialize Go module with testify dependency"
```

---

## Task 2: Create Directory Structure

**Files:**
- Create: `cmd/server/.gitkeep`
- Create: `cmd/cli/.gitkeep`
- Create: `internal/models/.gitkeep`
- Create: `internal/db/.gitkeep`
- Create: `internal/queue/.gitkeep`
- Create: `internal/api/.gitkeep`
- Create: `internal/executor/.gitkeep`
- Create: `internal/git/.gitkeep`
- Create: `internal/dashboard/.gitkeep`
- Create: `internal/platform/.gitkeep`
- Create: `web/templates/.gitkeep`
- Create: `web/static/css/.gitkeep`
- Create: `web/static/js/.gitkeep`
- Create: `scripts/.gitkeep`

**Step 1: Create all directories**

Run:
```bash
mkdir -p cmd/server cmd/cli internal/{models,db,queue,api,executor,git,dashboard,platform} web/{templates,static/{css,js}} scripts
touch cmd/server/.gitkeep cmd/cli/.gitkeep internal/models/.gitkeep internal/db/.gitkeep internal/queue/.gitkeep internal/api/.gitkeep internal/executor/.gitkeep internal/git/.gitkeep internal/dashboard/.gitkeep internal/platform/.gitkeep web/templates/.gitkeep web/static/css/.gitkeep web/static/js/.gitkeep scripts/.gitkeep
```

**Step 2: Verify structure**

Run:
```bash
find . -type d | grep -v ".git" | sort
```

Expected output includes all directories

**Step 3: Commit**

```bash
git add .
git commit -m "feat: create project directory structure"
```

---

## Task 3: Define Priority Type with Tests

**Files:**
- Create: `internal/models/priority.go`
- Create: `internal/models/priority_test.go`

**Step 1: Write the failing tests**

Create `internal/models/priority_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriority_Valid(t *testing.T) {
	tests := []struct {
		name     string
		priority Priority
		want     bool
	}{
		{"high is valid", PriorityHigh, true},
		{"normal is valid", PriorityNormal, true},
		{"low is valid", PriorityLow, true},
		{"empty is invalid", Priority(""), false},
		{"unknown is invalid", Priority("urgent"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.priority.Valid())
		})
	}
}

func TestPriority_Weight(t *testing.T) {
	assert.Greater(t, PriorityHigh.Weight(), PriorityNormal.Weight())
	assert.Greater(t, PriorityNormal.Weight(), PriorityLow.Weight())
}

func TestPriority_JSON(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		data, err := json.Marshal(PriorityHigh)
		require.NoError(t, err)
		assert.Equal(t, `"high"`, string(data))
	})

	t.Run("unmarshal valid", func(t *testing.T) {
		var p Priority
		err := json.Unmarshal([]byte(`"normal"`), &p)
		require.NoError(t, err)
		assert.Equal(t, PriorityNormal, p)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var p Priority
		err := json.Unmarshal([]byte(`"urgent"`), &p)
		assert.Error(t, err)
	})
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input   string
		want    Priority
		wantErr bool
	}{
		{"high", PriorityHigh, false},
		{"normal", PriorityNormal, false},
		{"low", PriorityLow, false},
		{"HIGH", PriorityHigh, false},
		{"Normal", PriorityNormal, false},
		{"", Priority(""), true},
		{"urgent", Priority(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParsePriority(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/models/... -v
```

Expected: FAIL - package not found or types not defined

**Step 3: Write minimal implementation**

Create `internal/models/priority.go`:

```go
package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Priority represents job priority level
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// Valid returns true if the priority is a known value
func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	default:
		return false
	}
}

// Weight returns a numeric weight for sorting (higher = more important)
func (p Priority) Weight() int {
	switch p {
	case PriorityHigh:
		return 3
	case PriorityNormal:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

// UnmarshalJSON implements json.Unmarshaler with validation
func (p *Priority) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParsePriority(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// ParsePriority parses a string into a Priority (case-insensitive)
func ParsePriority(s string) (Priority, error) {
	switch strings.ToLower(s) {
	case "high":
		return PriorityHigh, nil
	case "normal":
		return PriorityNormal, nil
	case "low":
		return PriorityLow, nil
	default:
		return "", fmt.Errorf("invalid priority: %q", s)
	}
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/models/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/priority.go internal/models/priority_test.go
git commit -m "feat(models): add Priority type with validation and JSON support"
```

---

## Task 4: Define JobStatus Type with Tests

**Files:**
- Create: `internal/models/status.go`
- Create: `internal/models/status_test.go`

**Step 1: Write the failing tests**

Create `internal/models/status_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStatus_Valid(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"queued", StatusQueued, true},
		{"running", StatusRunning, true},
		{"paused", StatusPaused, true},
		{"completed", StatusCompleted, true},
		{"failed", StatusFailed, true},
		{"cancelled", StatusCancelled, true},
		{"empty", JobStatus(""), false},
		{"unknown", JobStatus("pending"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}

func TestJobStatus_IsTerminal(t *testing.T) {
	terminals := []JobStatus{StatusCompleted, StatusFailed, StatusCancelled}
	nonTerminals := []JobStatus{StatusQueued, StatusRunning, StatusPaused}

	for _, s := range terminals {
		assert.True(t, s.IsTerminal(), "%s should be terminal", s)
	}
	for _, s := range nonTerminals {
		assert.False(t, s.IsTerminal(), "%s should not be terminal", s)
	}
}

func TestJobStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from    JobStatus
		to      JobStatus
		allowed bool
	}{
		// From queued
		{StatusQueued, StatusRunning, true},
		{StatusQueued, StatusCancelled, true},
		{StatusQueued, StatusPaused, false},
		{StatusQueued, StatusCompleted, false},
		// From running
		{StatusRunning, StatusPaused, true},
		{StatusRunning, StatusCompleted, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusCancelled, true},
		{StatusRunning, StatusQueued, false},
		// From paused
		{StatusPaused, StatusRunning, true},
		{StatusPaused, StatusCancelled, true},
		{StatusPaused, StatusQueued, false},
		{StatusPaused, StatusCompleted, false},
		// From terminal states
		{StatusCompleted, StatusRunning, false},
		{StatusFailed, StatusRunning, false},
		{StatusCancelled, StatusRunning, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			assert.Equal(t, tt.allowed, tt.from.CanTransitionTo(tt.to))
		})
	}
}

func TestJobStatus_JSON(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		data, err := json.Marshal(StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, `"running"`, string(data))
	})

	t.Run("unmarshal valid", func(t *testing.T) {
		var s JobStatus
		err := json.Unmarshal([]byte(`"paused"`), &s)
		require.NoError(t, err)
		assert.Equal(t, StatusPaused, s)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var s JobStatus
		err := json.Unmarshal([]byte(`"pending"`), &s)
		assert.Error(t, err)
	})
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/models/... -v
```

Expected: FAIL - types not defined

**Step 3: Write minimal implementation**

Create `internal/models/status.go`:

```go
package models

import (
	"encoding/json"
	"fmt"
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusPaused    JobStatus = "paused"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// Valid returns true if the status is a known value
func (s JobStatus) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the status is a final state
func (s JobStatus) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if transitioning from s to target is valid
func (s JobStatus) CanTransitionTo(target JobStatus) bool {
	switch s {
	case StatusQueued:
		return target == StatusRunning || target == StatusCancelled
	case StatusRunning:
		return target == StatusPaused || target == StatusCompleted ||
			target == StatusFailed || target == StatusCancelled
	case StatusPaused:
		return target == StatusRunning || target == StatusCancelled
	case StatusCompleted, StatusFailed, StatusCancelled:
		return false // Terminal states cannot transition
	default:
		return false
	}
}

// UnmarshalJSON implements json.Unmarshaler with validation
func (s *JobStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	status := JobStatus(str)
	if !status.Valid() {
		return fmt.Errorf("invalid job status: %q", str)
	}
	*s = status
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/models/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/status.go internal/models/status_test.go
git commit -m "feat(models): add JobStatus type with state machine validation"
```

---

## Task 5: Define Job Model with Tests

**Files:**
- Create: `internal/models/job.go`
- Create: `internal/models/job_test.go`

**Step 1: Write the failing tests**

Create `internal/models/job_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJob(t *testing.T) {
	job := NewJob(
		"git@github.com:user/repo.git",
		"feature/test",
		"Run all tests",
		50,
	)

	assert.Equal(t, int64(0), job.ID) // Not set until persisted
	assert.Equal(t, StatusQueued, job.Status)
	assert.Equal(t, PriorityNormal, job.Priority)
	assert.Equal(t, "git@github.com:user/repo.git", job.RepoURL)
	assert.Equal(t, "feature/test", job.Branch)
	assert.Equal(t, "ralph/feature/test-result", job.ResultBranch)
	assert.Equal(t, "Run all tests", job.Prompt)
	assert.Equal(t, 50, job.MaxIterations)
	assert.Equal(t, 0, job.Iteration)
	assert.False(t, job.CreatedAt.IsZero())
}

func TestJob_GenerateResultBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/auth", "ralph/feature/auth-result"},
		{"main", "ralph/main-result"},
		{"fix/bug-123", "ralph/fix/bug-123-result"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			assert.Equal(t, tt.want, GenerateResultBranch(tt.branch))
		})
	}
}

func TestJob_Validate(t *testing.T) {
	validJob := func() *Job {
		return NewJob(
			"git@github.com:user/repo.git",
			"feature/test",
			"Run tests",
			50,
		)
	}

	t.Run("valid job passes", func(t *testing.T) {
		job := validJob()
		assert.NoError(t, job.Validate())
	})

	t.Run("empty repo_url fails", func(t *testing.T) {
		job := validJob()
		job.RepoURL = ""
		assert.Error(t, job.Validate())
	})

	t.Run("empty branch fails", func(t *testing.T) {
		job := validJob()
		job.Branch = ""
		assert.Error(t, job.Validate())
	})

	t.Run("empty prompt fails", func(t *testing.T) {
		job := validJob()
		job.Prompt = ""
		assert.Error(t, job.Validate())
	})

	t.Run("zero max_iterations fails", func(t *testing.T) {
		job := validJob()
		job.MaxIterations = 0
		assert.Error(t, job.Validate())
	})

	t.Run("negative max_iterations fails", func(t *testing.T) {
		job := validJob()
		job.MaxIterations = -1
		assert.Error(t, job.Validate())
	})

	t.Run("invalid priority fails", func(t *testing.T) {
		job := validJob()
		job.Priority = "urgent"
		assert.Error(t, job.Validate())
	})
}

func TestJob_TransitionTo(t *testing.T) {
	t.Run("valid transition updates status and timestamp", func(t *testing.T) {
		job := NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := job.TransitionTo(StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, StatusRunning, job.Status)
		assert.NotNil(t, job.StartedAt)
	})

	t.Run("invalid transition returns error", func(t *testing.T) {
		job := NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := job.TransitionTo(StatusCompleted)
		assert.Error(t, err)
		assert.Equal(t, StatusQueued, job.Status) // Unchanged
	})

	t.Run("pause sets paused_at", func(t *testing.T) {
		job := NewJob("git@github.com:user/repo.git", "main", "test", 10)
		_ = job.TransitionTo(StatusRunning)
		err := job.TransitionTo(StatusPaused)
		require.NoError(t, err)
		assert.NotNil(t, job.PausedAt)
	})

	t.Run("complete sets completed_at", func(t *testing.T) {
		job := NewJob("git@github.com:user/repo.git", "main", "test", 10)
		_ = job.TransitionTo(StatusRunning)
		err := job.TransitionTo(StatusCompleted)
		require.NoError(t, err)
		assert.NotNil(t, job.CompletedAt)
	})
}

func TestJob_IncrementIteration(t *testing.T) {
	job := NewJob("git@github.com:user/repo.git", "main", "test", 3)
	_ = job.TransitionTo(StatusRunning)

	assert.Equal(t, 0, job.Iteration)

	job.IncrementIteration()
	assert.Equal(t, 1, job.Iteration)

	job.IncrementIteration()
	assert.Equal(t, 2, job.Iteration)

	job.IncrementIteration()
	assert.Equal(t, 3, job.Iteration)
}

func TestJob_HasReachedMaxIterations(t *testing.T) {
	job := NewJob("git@github.com:user/repo.git", "main", "test", 2)
	_ = job.TransitionTo(StatusRunning)

	assert.False(t, job.HasReachedMaxIterations())
	job.IncrementIteration()
	assert.False(t, job.HasReachedMaxIterations())
	job.IncrementIteration()
	assert.True(t, job.HasReachedMaxIterations())
}

func TestJob_Progress(t *testing.T) {
	job := NewJob("git@github.com:user/repo.git", "main", "test", 100)
	assert.Equal(t, 0.0, job.Progress())

	job.Iteration = 50
	assert.Equal(t, 0.5, job.Progress())

	job.Iteration = 100
	assert.Equal(t, 1.0, job.Progress())
}

func TestJob_JSON(t *testing.T) {
	job := NewJob("git@github.com:user/repo.git", "feature/test", "Run tests", 50)
	job.ID = 42
	job.WorkingDir = "packages/auth"
	job.Env = map[string]string{"NODE_ENV": "test"}

	data, err := json.Marshal(job)
	require.NoError(t, err)

	var decoded Job
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, job.ID, decoded.ID)
	assert.Equal(t, job.RepoURL, decoded.RepoURL)
	assert.Equal(t, job.Branch, decoded.Branch)
	assert.Equal(t, job.Status, decoded.Status)
	assert.Equal(t, job.Priority, decoded.Priority)
	assert.Equal(t, job.WorkingDir, decoded.WorkingDir)
	assert.Equal(t, job.Env, decoded.Env)
}

func TestJob_Duration(t *testing.T) {
	job := NewJob("git@github.com:user/repo.git", "main", "test", 10)

	t.Run("not started returns zero", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), job.Duration())
	})

	t.Run("running returns elapsed time", func(t *testing.T) {
		startTime := time.Now().Add(-5 * time.Minute)
		job.StartedAt = &startTime
		job.Status = StatusRunning
		duration := job.Duration()
		assert.True(t, duration >= 5*time.Minute)
	})

	t.Run("completed returns total time", func(t *testing.T) {
		startTime := time.Now().Add(-10 * time.Minute)
		endTime := time.Now().Add(-5 * time.Minute)
		job.StartedAt = &startTime
		job.CompletedAt = &endTime
		job.Status = StatusCompleted
		duration := job.Duration()
		assert.Equal(t, 5*time.Minute, duration)
	})
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/models/... -v
```

Expected: FAIL - Job type not defined

**Step 3: Write minimal implementation**

Create `internal/models/job.go`:

```go
package models

import (
	"fmt"
	"time"
)

// Job represents a ralph loop job in the queue
type Job struct {
	ID       int64     `json:"id"`
	Status   JobStatus `json:"status"`
	Priority Priority  `json:"priority"`
	Position int       `json:"position"`

	// Repository info
	RepoURL      string `json:"repo_url"`
	Branch       string `json:"branch"`
	ResultBranch string `json:"result_branch"`
	WorkingDir   string `json:"working_dir,omitempty"`

	// Execution config
	Prompt        string            `json:"prompt"`
	MaxIterations int               `json:"max_iterations"`
	Env           map[string]string `json:"env,omitempty"`

	// Progress tracking
	Iteration  int `json:"iteration"`
	RetryCount int `json:"retry_count"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	PausedAt    *time.Time `json:"paused_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// Results
	PRURL string `json:"pr_url,omitempty"`
	Error string `json:"error,omitempty"`
}

// NewJob creates a new job with default values
func NewJob(repoURL, branch, prompt string, maxIterations int) *Job {
	return &Job{
		Status:        StatusQueued,
		Priority:      PriorityNormal,
		RepoURL:       repoURL,
		Branch:        branch,
		ResultBranch:  GenerateResultBranch(branch),
		Prompt:        prompt,
		MaxIterations: maxIterations,
		Iteration:     0,
		RetryCount:    0,
		CreatedAt:     time.Now(),
	}
}

// GenerateResultBranch creates the result branch name from the source branch
func GenerateResultBranch(branch string) string {
	return "ralph/" + branch + "-result"
}

// Validate checks if the job has all required fields with valid values
func (j *Job) Validate() error {
	if j.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	if j.Branch == "" {
		return fmt.Errorf("branch is required")
	}
	if j.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if j.MaxIterations <= 0 {
		return fmt.Errorf("max_iterations must be positive")
	}
	if !j.Priority.Valid() {
		return fmt.Errorf("invalid priority: %q", j.Priority)
	}
	return nil
}

// TransitionTo attempts to change the job status
func (j *Job) TransitionTo(target JobStatus) error {
	if !j.Status.CanTransitionTo(target) {
		return fmt.Errorf("cannot transition from %s to %s", j.Status, target)
	}

	now := time.Now()

	switch target {
	case StatusRunning:
		if j.StartedAt == nil {
			j.StartedAt = &now
		}
	case StatusPaused:
		j.PausedAt = &now
	case StatusCompleted, StatusFailed, StatusCancelled:
		j.CompletedAt = &now
	}

	j.Status = target
	return nil
}

// IncrementIteration increases the iteration counter
func (j *Job) IncrementIteration() {
	j.Iteration++
}

// HasReachedMaxIterations returns true if iteration >= max_iterations
func (j *Job) HasReachedMaxIterations() bool {
	return j.Iteration >= j.MaxIterations
}

// Progress returns completion percentage as 0.0-1.0
func (j *Job) Progress() float64 {
	if j.MaxIterations == 0 {
		return 0
	}
	return float64(j.Iteration) / float64(j.MaxIterations)
}

// Duration returns how long the job has been running
func (j *Job) Duration() time.Duration {
	if j.StartedAt == nil {
		return 0
	}

	end := time.Now()
	if j.CompletedAt != nil {
		end = *j.CompletedAt
	}

	return end.Sub(*j.StartedAt)
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/models/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/job.go internal/models/job_test.go
git commit -m "feat(models): add Job model with validation and state transitions"
```

---

## Task 6: Define ServerConfig Model with Tests

**Files:**
- Create: `internal/models/config.go`
- Create: `internal/models/config_test.go`

**Step 1: Write the failing tests**

Create `internal/models/config_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/models/... -v
```

Expected: FAIL - ServerConfig not defined

**Step 3: Write minimal implementation**

Create `internal/models/config.go`:

```go
package models

import "fmt"

// ServerConfig holds server-wide configuration
type ServerConfig struct {
	// Models
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`

	// Execution
	DefaultMaxIterations int `json:"default_max_iterations"`
	ConcurrentJobs       int `json:"concurrent_jobs"`

	// Storage
	WorkspaceDir     string `json:"workspace_dir"`
	JobRetentionDays int    `json:"job_retention_days"`

	// Retry behavior
	MaxClaudeRetries  int `json:"max_claude_retries"`
	MaxGitRetries     int `json:"max_git_retries"`
	GitRetryBackoffMs int `json:"git_retry_backoff_ms"`
}

// DefaultServerConfig returns a ServerConfig with sensible defaults
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		LargeModel:           "qwen3-coder:70b",
		SmallModel:           "qwen2.5-coder:7b",
		DefaultMaxIterations: 50,
		ConcurrentJobs:       1,
		JobRetentionDays:     30,
		MaxClaudeRetries:     3,
		MaxGitRetries:        3,
		GitRetryBackoffMs:    1000,
	}
}

// Validate checks if the config has valid values
func (c *ServerConfig) Validate() error {
	if c.LargeModel == "" {
		return fmt.Errorf("large_model is required")
	}
	if c.SmallModel == "" {
		return fmt.Errorf("small_model is required")
	}
	if c.DefaultMaxIterations <= 0 {
		return fmt.Errorf("default_max_iterations must be positive")
	}
	if c.ConcurrentJobs <= 0 {
		return fmt.Errorf("concurrent_jobs must be positive")
	}
	if c.JobRetentionDays < 0 {
		return fmt.Errorf("job_retention_days cannot be negative")
	}
	return nil
}

// Merge returns a new config with non-zero values from updates applied
func (c *ServerConfig) Merge(updates *ServerConfig) *ServerConfig {
	result := *c // Copy

	if updates.LargeModel != "" {
		result.LargeModel = updates.LargeModel
	}
	if updates.SmallModel != "" {
		result.SmallModel = updates.SmallModel
	}
	if updates.DefaultMaxIterations > 0 {
		result.DefaultMaxIterations = updates.DefaultMaxIterations
	}
	if updates.ConcurrentJobs > 0 {
		result.ConcurrentJobs = updates.ConcurrentJobs
	}
	if updates.WorkspaceDir != "" {
		result.WorkspaceDir = updates.WorkspaceDir
	}
	if updates.JobRetentionDays > 0 {
		result.JobRetentionDays = updates.JobRetentionDays
	}
	if updates.MaxClaudeRetries > 0 {
		result.MaxClaudeRetries = updates.MaxClaudeRetries
	}
	if updates.MaxGitRetries > 0 {
		result.MaxGitRetries = updates.MaxGitRetries
	}
	if updates.GitRetryBackoffMs > 0 {
		result.GitRetryBackoffMs = updates.GitRetryBackoffMs
	}

	return &result
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/models/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat(models): add ServerConfig with defaults, validation, and merge"
```

---

## Task 7: Create Makefile

**Files:**
- Create: `Makefile`

**Step 1: Create the Makefile**

Create `Makefile`:

```makefile
.PHONY: all build test test-unit test-integration test-coverage clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Binary names
SERVER_BINARY=ralph-o-matic-server
CLI_BINARY=ralph-o-matic

# Directories
CMD_SERVER=./cmd/server
CMD_CLI=./cmd/cli
BUILD_DIR=./build

# Build flags
LDFLAGS=-ldflags "-s -w"

all: test build

## Build targets

build: build-server build-cli

build-server:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) $(CMD_SERVER)

build-cli:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY) $(CMD_CLI)

## Cross-compilation targets

build-all: build-server-all build-cli-all

build-server-all:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-darwin-arm64 $(CMD_SERVER)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-darwin-amd64 $(CMD_SERVER)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-linux-amd64 $(CMD_SERVER)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-linux-arm64 $(CMD_SERVER)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-windows-amd64.exe $(CMD_SERVER)

build-cli-all:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-darwin-arm64 $(CMD_CLI)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-darwin-amd64 $(CMD_CLI)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-linux-amd64 $(CMD_CLI)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-linux-arm64 $(CMD_CLI)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-windows-amd64.exe $(CMD_CLI)

## Test targets

test: test-unit

test-unit:
	$(GOTEST) -v -short -race ./...

test-integration:
	$(GOTEST) -v -race -tags=integration ./...

test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## Utility targets

clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

deps:
	$(GOMOD) download
	$(GOMOD) tidy

lint:
	golangci-lint run ./...

fmt:
	$(GOCMD) fmt ./...

vet:
	$(GOCMD) vet ./...
```

**Step 2: Verify Makefile works**

Run:
```bash
make test-unit
```

Expected: All tests pass

**Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add Makefile with build, test, and cross-compilation targets"
```

---

## Task 8: Add .gitignore

**Files:**
- Create: `.gitignore`

**Step 1: Create .gitignore**

Create `.gitignore`:

```gitignore
# Binaries
/build/
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test files
*.test
coverage.out
coverage.html

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Go
vendor/

# Local config (may contain secrets)
.env
*.local.yaml
*.local.json

# Database
*.db
*.sqlite
*.sqlite3

# Logs
*.log
logs/

# Temp
tmp/
temp/
```

**Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore for Go project"
```

---

## Task 9: Run Full Test Suite and Verify

**Step 1: Run all tests**

Run:
```bash
make test-unit
```

Expected: All tests pass

**Step 2: Check test coverage**

Run:
```bash
make test-coverage
```

Expected: Coverage report generated

**Step 3: Verify module compiles**

Run:
```bash
go build ./...
```

Expected: No errors

---

## Phase 1 Completion Checklist

- [ ] Go module initialized with testify
- [ ] Directory structure created
- [ ] Priority type with tests
- [ ] JobStatus type with state machine tests
- [ ] Job model with validation and transition tests
- [ ] ServerConfig with defaults and merge tests
- [ ] Makefile with build/test targets
- [ ] .gitignore configured
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 2 - Database Layer
