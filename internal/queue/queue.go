package queue

import (
	"fmt"
	"sync"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// Queue manages job scheduling and state transitions
type Queue struct {
	db          *db.DB
	jobRepo     *db.JobRepo
	mu          sync.RWMutex
	broadcaster *broadcast.Broadcaster
}

// New creates a new queue backed by the database
func New(database *db.DB) *Queue {
	return &Queue{
		db:      database,
		jobRepo: db.NewJobRepo(database),
	}
}

// SetBroadcaster sets the broadcaster for publishing state change events.
func (q *Queue) SetBroadcaster(b *broadcast.Broadcaster) {
	q.broadcaster = b
}

func (q *Queue) publish() {
	if q.broadcaster != nil {
		q.broadcaster.Publish("global", []byte("{}"))
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
	if err := q.jobRepo.Create(job); err != nil {
		return err
	}
	q.publish()
	return nil
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

	q.publish()
	return job, nil
}

// Pause pauses a running job, preserving its iteration count
func (q *Queue) Pause(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusPaused); err != nil {
		return fmt.Errorf("cannot pause job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publish()
	return nil
}

// Resume resumes a paused job
func (q *Queue) Resume(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if job.Status != models.StatusPaused {
		return fmt.Errorf("cannot resume job: job is not paused (status: %s)", job.Status)
	}

	if err := job.TransitionTo(models.StatusRunning); err != nil {
		return fmt.Errorf("cannot resume job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publish()
	return nil
}

// Complete marks a job as successfully completed
func (q *Queue) Complete(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusCompleted); err != nil {
		return fmt.Errorf("cannot complete job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publish()
	return nil
}

// Fail marks a job as failed with an error message
func (q *Queue) Fail(job *models.Job, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job.Error = errMsg
	if err := job.TransitionTo(models.StatusFailed); err != nil {
		return fmt.Errorf("cannot fail job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publish()
	return nil
}

// Cancel cancels a job (can be called from any non-terminal state)
func (q *Queue) Cancel(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := job.TransitionTo(models.StatusCancelled); err != nil {
		return fmt.Errorf("cannot cancel job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publish()
	return nil
}

// Reorder changes the order of queued jobs
func (q *Queue) Reorder(jobIDs []int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.jobRepo.UpdatePositions(jobIDs); err != nil {
		return err
	}
	q.publish()
	return nil
}

// Size returns the number of queued jobs
func (q *Queue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs, _, err := q.jobRepo.List(db.ListOptions{
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

	jobs, _, _ := q.jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusRunning},
	})

	return jobs
}

// GetPaused returns all paused jobs
func (q *Queue) GetPaused() []*models.Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs, _, _ := q.jobRepo.List(db.ListOptions{
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
