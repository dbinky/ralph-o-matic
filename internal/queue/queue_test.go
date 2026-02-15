package queue

import (
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestQueue(t *testing.T) (*Queue, *db.DB) {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	q := New(database)
	return q, database
}

func TestQueue_Enqueue(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := q.Enqueue(job)
	require.NoError(t, err)

	assert.Greater(t, job.ID, int64(0))
	assert.Equal(t, models.StatusQueued, job.Status)
}

func TestQueue_Enqueue_InvalidJob(t *testing.T) {
	q, _ := newTestQueue(t)

	job := &models.Job{} // Missing required fields
	err := q.Enqueue(job)
	assert.Error(t, err)
}

func TestQueue_Dequeue(t *testing.T) {
	q, _ := newTestQueue(t)

	// Enqueue a job
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := q.Enqueue(job)
	require.NoError(t, err)

	// Dequeue it
	dequeued, err := q.Dequeue()
	require.NoError(t, err)
	require.NotNil(t, dequeued)

	assert.Equal(t, job.ID, dequeued.ID)
	assert.Equal(t, models.StatusRunning, dequeued.Status)
	assert.NotNil(t, dequeued.StartedAt)
}

func TestQueue_Dequeue_Empty(t *testing.T) {
	q, _ := newTestQueue(t)

	job, err := q.Dequeue()
	require.NoError(t, err)
	assert.Nil(t, job)
}

func TestQueue_Dequeue_Priority(t *testing.T) {
	q, _ := newTestQueue(t)

	// Enqueue low priority first
	lowJob := models.NewJob("git@github.com:user/repo.git", "main", "low", 10)
	lowJob.Priority = models.PriorityLow
	require.NoError(t, q.Enqueue(lowJob))

	// Enqueue high priority second
	highJob := models.NewJob("git@github.com:user/repo.git", "main", "high", 10)
	highJob.Priority = models.PriorityHigh
	require.NoError(t, q.Enqueue(highJob))

	// Should dequeue high priority first
	dequeued, err := q.Dequeue()
	require.NoError(t, err)
	assert.Equal(t, highJob.ID, dequeued.ID)
}

func TestQueue_Pause(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, _ := q.Dequeue()
	dequeued.Iteration = 5

	err := q.Pause(dequeued)
	require.NoError(t, err)

	assert.Equal(t, models.StatusPaused, dequeued.Status)
	assert.NotNil(t, dequeued.PausedAt)
	assert.Equal(t, 5, dequeued.Iteration) // Preserved
}

func TestQueue_Pause_NotRunning(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	err := q.Pause(job) // Still queued, not running
	assert.Error(t, err)
}

func TestQueue_Resume(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, _ := q.Dequeue()
	dequeued.Iteration = 5
	require.NoError(t, q.Pause(dequeued))

	err := q.Resume(dequeued)
	require.NoError(t, err)

	assert.Equal(t, models.StatusQueued, dequeued.Status)
	assert.Equal(t, 5, dequeued.Iteration) // Still preserved
}

func TestQueue_Resume_NotPaused(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	err := q.Resume(job) // Still queued
	assert.Error(t, err)
}

func TestQueue_Complete(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, _ := q.Dequeue()
	dequeued.PRURL = "https://github.com/user/repo/pull/123"

	err := q.Complete(dequeued)
	require.NoError(t, err)

	assert.Equal(t, models.StatusCompleted, dequeued.Status)
	assert.NotNil(t, dequeued.CompletedAt)
}

func TestQueue_Fail(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, _ := q.Dequeue()

	err := q.Fail(dequeued, "max iterations reached")
	require.NoError(t, err)

	assert.Equal(t, models.StatusFailed, dequeued.Status)
	assert.Equal(t, "max iterations reached", dequeued.Error)
}

func TestQueue_Cancel(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	err := q.Cancel(job)
	require.NoError(t, err)

	assert.Equal(t, models.StatusCancelled, job.Status)
}

func TestQueue_Cancel_Running(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, _ := q.Dequeue()

	err := q.Cancel(dequeued)
	require.NoError(t, err)

	assert.Equal(t, models.StatusCancelled, dequeued.Status)
}

func TestQueue_Reorder(t *testing.T) {
	q, _ := newTestQueue(t)

	// Create 3 jobs
	var jobs []*models.Job
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, q.Enqueue(job))
		jobs = append(jobs, job)
	}

	// Reorder: [3, 1, 2]
	newOrder := []int64{jobs[2].ID, jobs[0].ID, jobs[1].ID}
	err := q.Reorder(newOrder)
	require.NoError(t, err)

	// Dequeue should return job 3 first
	dequeued, _ := q.Dequeue()
	assert.Equal(t, jobs[2].ID, dequeued.ID)
}

func TestQueue_Size(t *testing.T) {
	q, _ := newTestQueue(t)

	assert.Equal(t, 0, q.Size())

	for i := 0; i < 5; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, q.Enqueue(job))
	}

	assert.Equal(t, 5, q.Size())

	q.Dequeue()
	assert.Equal(t, 4, q.Size())
}

func TestQueue_GetRunning(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	// No running jobs yet
	running := q.GetRunning()
	assert.Len(t, running, 0)

	// Dequeue starts the job
	q.Dequeue()

	running = q.GetRunning()
	assert.Len(t, running, 1)
}

func TestQueue_GetPaused(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, _ := q.Dequeue()
	q.Pause(dequeued)

	paused := q.GetPaused()
	assert.Len(t, paused, 1)
}

func TestQueue_Enqueue_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	_, ch := b.Subscribe("global")

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestQueue_Dequeue_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	_, ch := b.Subscribe("global")
	_, err := q.Dequeue()
	require.NoError(t, err)

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestQueue_StateChanges_PublishBroadcast(t *testing.T) {
	tests := []struct {
		name   string
		action func(q *Queue, job *models.Job) error
		setup  func(q *Queue, job *models.Job)
	}{
		{
			name:   "Pause",
			setup:  func(q *Queue, job *models.Job) { dequeued, _ := q.Dequeue(); *job = *dequeued },
			action: func(q *Queue, job *models.Job) error { return q.Pause(job) },
		},
		{
			name: "Resume",
			setup: func(q *Queue, job *models.Job) {
				dequeued, _ := q.Dequeue()
				q.Pause(dequeued)
				*job = *dequeued
			},
			action: func(q *Queue, job *models.Job) error { return q.Resume(job) },
		},
		{
			name:   "Complete",
			setup:  func(q *Queue, job *models.Job) { dequeued, _ := q.Dequeue(); *job = *dequeued },
			action: func(q *Queue, job *models.Job) error { return q.Complete(job) },
		},
		{
			name:   "Fail",
			setup:  func(q *Queue, job *models.Job) { dequeued, _ := q.Dequeue(); *job = *dequeued },
			action: func(q *Queue, job *models.Job) error { return q.Fail(job, "error") },
		},
		{
			name:   "Cancel",
			action: func(q *Queue, job *models.Job) error { return q.Cancel(job) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := newTestQueue(t)
			b := broadcast.New()
			q.SetBroadcaster(b)

			job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
			require.NoError(t, q.Enqueue(job))

			if tt.setup != nil {
				tt.setup(q, job)
			}

			_, ch := b.Subscribe("global")
			require.NoError(t, tt.action(q, job))

			select {
			case msg := <-ch:
				assert.Equal(t, []byte("{}"), msg)
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for broadcast")
			}
		})
	}
}

func TestQueue_Reorder_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	job1 := models.NewJob("git@github.com:user/repo.git", "main", "test1", 10)
	job2 := models.NewJob("git@github.com:user/repo.git", "main", "test2", 10)
	require.NoError(t, q.Enqueue(job1))
	require.NoError(t, q.Enqueue(job2))

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Reorder([]int64{job2.ID, job1.ID}))

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestQueue_NilBroadcaster(t *testing.T) {
	q, _ := newTestQueue(t)
	// No broadcaster set — should work without panic

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Complete(dequeued))
}
