package queue

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_StartStop(t *testing.T) {
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	defer database.Close()

	q := New(database)

	var processCount int32
	handler := func(ctx context.Context, job *models.Job) error {
		atomic.AddInt32(&processCount, 1)
		return nil
	}

	s := NewScheduler(q, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Start(ctx)

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Should be running
	assert.True(t, s.IsRunning())

	// Stop it
	cancel()
	time.Sleep(50 * time.Millisecond)

	assert.False(t, s.IsRunning())
}

func TestScheduler_ProcessJob(t *testing.T) {
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	defer database.Close()

	q := New(database)

	// Add a job
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	done := make(chan *models.Job, 1)
	handler := func(ctx context.Context, j *models.Job) error {
		done <- j
		return nil
	}

	s := NewScheduler(q, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go s.Start(ctx)

	select {
	case processedJob := <-done:
		assert.NotNil(t, processedJob)
		assert.Equal(t, job.ID, processedJob.ID)
	case <-ctx.Done():
		t.Fatal("timed out waiting for job to be processed")
	}
}

func TestScheduler_HandlerError(t *testing.T) {
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	defer database.Close()

	q := New(database)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	handler := func(ctx context.Context, j *models.Job) error {
		return fmt.Errorf("handler error")
	}

	s := NewScheduler(q, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go s.Start(ctx)

	time.Sleep(200 * time.Millisecond)

	// Job should be marked as failed
	updated, err := q.Get(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusFailed, updated.Status)
}

func TestScheduler_JobSignal(t *testing.T) {
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	defer database.Close()

	q := New(database)

	var processed int32
	handler := func(ctx context.Context, j *models.Job) error {
		atomic.AddInt32(&processed, 1)
		return nil
	}

	s := NewScheduler(q, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Add job and signal
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	s.Signal()

	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, int32(1), atomic.LoadInt32(&processed))
}
