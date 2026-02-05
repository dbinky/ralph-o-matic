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

func TestRecoverOrphaned_MultipleRunningJobs(t *testing.T) {
	q, _ := newTestQueue(t)

	// Create 3 jobs and move them all to running
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, q.Enqueue(job))
		_, err := q.Dequeue()
		require.NoError(t, err)
	}

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// All should be queued now
	running := q.GetRunning()
	assert.Len(t, running, 0)
	assert.Equal(t, 3, q.Size())
}

func TestRecoverOrphaned_NoOrphanedJobs(t *testing.T) {
	q, _ := newTestQueue(t)

	// Enqueue a job but don't dequeue it (stays queued)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRecoverOrphaned_OnlyRunningJobsTouched(t *testing.T) {
	q, _ := newTestQueue(t)

	// Queued job
	queued := models.NewJob("git@github.com:user/repo.git", "main", "queued", 10)
	require.NoError(t, q.Enqueue(queued))

	// Running job (will be recovered)
	running := models.NewJob("git@github.com:user/repo.git", "main", "running", 10)
	require.NoError(t, q.Enqueue(running))
	dequeuedRunning, err := q.Dequeue()
	require.NoError(t, err)

	// Paused job (should NOT be recovered)
	paused := models.NewJob("git@github.com:user/repo.git", "main", "paused", 10)
	require.NoError(t, q.Enqueue(paused))
	dequeuedPaused, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Pause(dequeuedPaused))

	// Completed job
	completed := models.NewJob("git@github.com:user/repo.git", "main", "completed", 10)
	require.NoError(t, q.Enqueue(completed))
	dequeuedCompleted, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Complete(dequeuedCompleted))

	// Failed job
	failed := models.NewJob("git@github.com:user/repo.git", "main", "failed", 10)
	require.NoError(t, q.Enqueue(failed))
	dequeuedFailed, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Fail(dequeuedFailed, "some error"))

	// Cancelled job
	cancelled := models.NewJob("git@github.com:user/repo.git", "main", "cancelled", 10)
	require.NoError(t, q.Enqueue(cancelled))
	require.NoError(t, q.Cancel(cancelled))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Only the running job should be recovered")

	// Verify the running job was recovered
	recoveredJob, err := q.Get(dequeuedRunning.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recoveredJob.Status)

	// Verify paused job is still paused
	pausedJob, err := q.Get(dequeuedPaused.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusPaused, pausedJob.Status)

	// Verify completed job is still completed
	completedJob, err := q.Get(dequeuedCompleted.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCompleted, completedJob.Status)
}

func TestRecoverOrphaned_PreservesFields(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "feature-branch", "do stuff", 10)
	job.Priority = models.PriorityHigh
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	// Set some fields that should survive recovery
	dequeued.Iteration = 7
	dequeued.ResultBranch = "ralph/feature-branch-result"
	dequeued.RetryCount = 2
	require.NoError(t, q.Update(dequeued))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Nil(t, recovered.StartedAt)
	assert.Equal(t, 7, recovered.Iteration)
	assert.Equal(t, "ralph/feature-branch-result", recovered.ResultBranch)
	assert.Equal(t, models.PriorityHigh, recovered.Priority)
	assert.Equal(t, "feature-branch", recovered.Branch)
	assert.Equal(t, "do stuff", recovered.Prompt)
	assert.Equal(t, 2, recovered.RetryCount)
	assert.Equal(t, 10, recovered.MaxIterations)
}

func TestRecoverOrphaned_RecoveredJobPickedUpByDequeue(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	// Recover
	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Dequeue should pick it back up
	redequeued, err := q.Dequeue()
	require.NoError(t, err)
	require.NotNil(t, redequeued)
	assert.Equal(t, dequeued.ID, redequeued.ID)
	assert.Equal(t, models.StatusRunning, redequeued.Status)
}

func TestRecoverOrphaned_IdempotentSecondCall(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	_, err := q.Dequeue()
	require.NoError(t, err)

	// First recovery
	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Second recovery — nothing to do
	count, err = q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRecoverOrphaned_ZeroIterationJob(t *testing.T) {
	q, database := newTestQueue(t)
	logRepo := db.NewLogRepo(database)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)
	// Iteration is 0 — crashed before any work done

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Equal(t, 0, recovered.Iteration)

	logs, err := logRepo.GetForJob(dequeued.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Message, "iteration 0/10")
}

func TestRecoverOrphaned_MaxIterationJob(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 5)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	dequeued.Iteration = 5 // At max
	require.NoError(t, q.Update(dequeued))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Re-queued even at max — worker handles the max-iteration check
	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Equal(t, 5, recovered.Iteration)
}
