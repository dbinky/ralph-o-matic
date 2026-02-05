package queue

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverOrphaned_RunningJobRequeued(t *testing.T) {
	q, database := newTestQueue(t)
	logRepo := db.NewLogRepo(database)

	// Create a job and move it to running via Dequeue
	job := models.NewJob("git@github.com:user/repo.git", "main", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	// Simulate progress
	dequeued.Iteration = 3
	require.NoError(t, q.Update(dequeued))

	// Now recover — simulating server restart
	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify job is back to queued
	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Nil(t, recovered.StartedAt, "StartedAt should be cleared")
	assert.Equal(t, 3, recovered.Iteration, "Iteration count should be preserved")

	// Verify log entry was written
	logs, err := logRepo.GetForJob(dequeued.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Message, "[RECOVERY]")
	assert.Contains(t, logs[0].Message, "iteration 3/10")
}
