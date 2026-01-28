# Phase 3: Queue & Scheduler

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the priority queue logic, job state machine enforcement, worker scheduling, and pause/resume functionality.

**Architecture:** In-memory queue backed by database for persistence. Single worker goroutine processes jobs sequentially (configurable for concurrent jobs later). Thread-safe operations with mutex protection.

**Tech Stack:** Go 1.22+, channels for coordination, context for cancellation

**Dependencies:** Phase 2 must be complete (database layer)

---

## Task 1: Implement Queue Interface and Types

**Files:**
- Create: `internal/queue/queue.go`
- Create: `internal/queue/queue_test.go`

**Step 1: Write the failing tests**

Create `internal/queue/queue_test.go`:

```go
package queue

import (
	"testing"

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

	assert.Equal(t, models.StatusRunning, dequeued.Status)
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/queue/... -v
```

Expected: FAIL - Queue not defined

**Step 3: Write minimal implementation**

Create `internal/queue/queue.go`:

```go
package queue

import (
	"fmt"
	"sync"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// Queue manages job scheduling and state transitions
type Queue struct {
	db      *db.DB
	jobRepo *db.JobRepo
	mu      sync.RWMutex
}

// New creates a new queue backed by the database
func New(database *db.DB) *Queue {
	return &Queue{
		db:      database,
		jobRepo: db.NewJobRepo(database),
	}
}

// Enqueue adds a new job to the queue
func (q *Queue) Enqueue(job *models.Job) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("invalid job: %w", err)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	job.Status = models.StatusQueued
	return q.jobRepo.Create(job)
}

// Dequeue returns the next job to process (highest priority, lowest position)
func (q *Queue) Dequeue() (*models.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	jobs, err := q.jobRepo.ListQueued()
	if err != nil {
		return nil, fmt.Errorf("failed to list queued jobs: %w", err)
	}

	if len(jobs) == 0 {
		return nil, nil
	}

	job := jobs[0]
	if err := job.TransitionTo(models.StatusRunning); err != nil {
		return nil, fmt.Errorf("failed to transition job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return nil, fmt.Errorf("failed to update job: %w", err)
	}

	return job, nil
}

// Pause pauses a running job, preserving its iteration count
func (q *Queue) Pause(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusPaused); err != nil {
		return fmt.Errorf("cannot pause job: %w", err)
	}

	return q.jobRepo.Update(job)
}

// Resume resumes a paused job
func (q *Queue) Resume(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusRunning); err != nil {
		return fmt.Errorf("cannot resume job: %w", err)
	}

	return q.jobRepo.Update(job)
}

// Complete marks a job as successfully completed
func (q *Queue) Complete(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusCompleted); err != nil {
		return fmt.Errorf("cannot complete job: %w", err)
	}

	return q.jobRepo.Update(job)
}

// Fail marks a job as failed with an error message
func (q *Queue) Fail(job *models.Job, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job.Error = errMsg
	if err := job.TransitionTo(models.StatusFailed); err != nil {
		return fmt.Errorf("cannot fail job: %w", err)
	}

	return q.jobRepo.Update(job)
}

// Cancel cancels a job (can be called from any non-terminal state)
func (q *Queue) Cancel(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusCancelled); err != nil {
		return fmt.Errorf("cannot cancel job: %w", err)
	}

	return q.jobRepo.Update(job)
}

// Reorder changes the order of queued jobs
func (q *Queue) Reorder(jobIDs []int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.jobRepo.UpdatePositions(jobIDs)
}

// Size returns the number of queued jobs
func (q *Queue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs, err := q.jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusQueued},
	})
	if err != nil {
		return 0
	}

	return len(jobs)
}

// GetRunning returns all currently running jobs
func (q *Queue) GetRunning() []*models.Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs, _ := q.jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusRunning},
	})

	return jobs
}

// GetPaused returns all paused jobs
func (q *Queue) GetPaused() []*models.Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs, _ := q.jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusPaused},
	})

	return jobs
}

// Get retrieves a job by ID
func (q *Queue) Get(id int64) (*models.Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.jobRepo.Get(id)
}

// Update saves job changes to the database
func (q *Queue) Update(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.jobRepo.Update(job)
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/queue/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_test.go
git commit -m "feat(queue): add priority queue with state machine enforcement"
```

---

## Task 2: Implement Scheduler/Worker

**Files:**
- Create: `internal/queue/scheduler.go`
- Create: `internal/queue/scheduler_test.go`

**Step 1: Write the failing tests**

Create `internal/queue/scheduler_test.go`:

```go
package queue

import (
	"context"
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

	var processedJob *models.Job
	handler := func(ctx context.Context, j *models.Job) error {
		processedJob = j
		return nil
	}

	s := NewScheduler(q, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go s.Start(ctx)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	assert.NotNil(t, processedJob)
	assert.Equal(t, job.ID, processedJob.ID)
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/queue/... -v
```

Expected: FAIL - Scheduler not defined

**Step 3: Write minimal implementation**

Create `internal/queue/scheduler.go`:

```go
package queue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// JobHandler is called for each job to be processed
type JobHandler func(ctx context.Context, job *models.Job) error

// Scheduler manages job execution
type Scheduler struct {
	queue   *Queue
	handler JobHandler
	signal  chan struct{}
	running bool
	mu      sync.RWMutex
}

// NewScheduler creates a new scheduler
func NewScheduler(queue *Queue, handler JobHandler) *Scheduler {
	return &Scheduler{
		queue:   queue,
		handler: handler,
		signal:  make(chan struct{}, 1),
	}
}

// Start begins processing jobs until context is cancelled
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.signal:
			s.processNext(ctx)
		case <-ticker.C:
			s.processNext(ctx)
		}
	}
}

// Signal notifies the scheduler that new work is available
func (s *Scheduler) Signal() {
	select {
	case s.signal <- struct{}{}:
	default:
		// Channel full, signal already pending
	}
}

// IsRunning returns true if the scheduler is running
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Scheduler) processNext(ctx context.Context) {
	job, err := s.queue.Dequeue()
	if err != nil {
		log.Printf("Error dequeuing job: %v", err)
		return
	}

	if job == nil {
		return // No jobs available
	}

	log.Printf("Processing job %d: %s", job.ID, job.Branch)

	// Create a context for this job that can be cancelled
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Run the handler
	if err := s.handler(jobCtx, job); err != nil {
		log.Printf("Job %d failed: %v", job.ID, err)
		if err := s.queue.Fail(job, err.Error()); err != nil {
			log.Printf("Failed to mark job as failed: %v", err)
		}
		return
	}

	// Mark as completed if handler succeeded
	if job.Status == models.StatusRunning {
		if err := s.queue.Complete(job); err != nil {
			log.Printf("Failed to mark job as completed: %v", err)
		}
	}

	// Signal that we might have more work
	s.Signal()
}

// PauseJob pauses a specific running job
func (s *Scheduler) PauseJob(jobID int64) error {
	job, err := s.queue.Get(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	return s.queue.Pause(job)
}

// ResumeJob resumes a specific paused job
func (s *Scheduler) ResumeJob(jobID int64) error {
	job, err := s.queue.Get(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if err := s.queue.Resume(job); err != nil {
		return err
	}

	s.Signal()
	return nil
}

// CancelJob cancels a specific job
func (s *Scheduler) CancelJob(jobID int64) error {
	job, err := s.queue.Get(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	return s.queue.Cancel(job)
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/queue/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/queue/scheduler.go internal/queue/scheduler_test.go
git commit -m "feat(queue): add scheduler for job execution coordination"
```

---

## Phase 3 Completion Checklist

- [ ] Queue with enqueue/dequeue
- [ ] Priority-based dequeuing
- [ ] State transitions (pause/resume/complete/fail/cancel)
- [ ] Queue reordering
- [ ] Scheduler with job processing loop
- [ ] Signal-based wakeup
- [ ] Error handling and job failure marking
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 4 - REST API
