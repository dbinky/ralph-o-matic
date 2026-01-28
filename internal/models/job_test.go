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
		now := time.Now()
		startTime := now.Add(-10 * time.Minute)
		endTime := now.Add(-5 * time.Minute)
		job.StartedAt = &startTime
		job.CompletedAt = &endTime
		job.Status = StatusCompleted
		duration := job.Duration()
		assert.Equal(t, 5*time.Minute, duration)
	})
}
