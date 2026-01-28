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
