package worker

import (
	"context"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPostCompletionHook_SetsEnvVars(t *testing.T) {
	started := time.Now()
	job := &models.Job{
		ID:           42,
		RepoURL:      "https://github.com/test/repo",
		Branch:       "dev-feature",
		ResultBranch: "ralph/dev-feature-result",
		PRURL:        "https://github.com/test/repo/pull/7",
		WorkingDir:   "/tmp/test-repo",
		Status:       models.StatusCompleted,
		StartedAt:    &started,
	}

	output, err := RunPostCompletionHook(
		context.Background(),
		"env | grep RALPH_",
		job,
	)
	require.NoError(t, err)

	assert.Contains(t, output, "42")
	assert.Contains(t, output, "https://github.com/test/repo")
	assert.Contains(t, output, "dev-feature")
	assert.Contains(t, output, "ralph/dev-feature-result")
	assert.Contains(t, output, "https://github.com/test/repo/pull/7")
	assert.Contains(t, output, "/tmp/test-repo")
	assert.Contains(t, output, "completed")
}

func TestRunPostCompletionHook_EmptyCommand(t *testing.T) {
	job := &models.Job{ID: 1, Status: models.StatusCompleted}
	output, err := RunPostCompletionHook(context.Background(), "", job)
	assert.NoError(t, err)
	assert.Equal(t, "", output)
}

func TestRunPostCompletionHook_CommandFailure(t *testing.T) {
	job := &models.Job{ID: 1, Status: models.StatusFailed}
	_, err := RunPostCompletionHook(context.Background(), "exit 1", job)
	assert.Error(t, err)
}

func TestRunPostCompletionHook_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	job := &models.Job{ID: 1, Status: models.StatusCompleted}
	_, err := RunPostCompletionHook(ctx, "sleep 10", job)
	assert.Error(t, err)
}
