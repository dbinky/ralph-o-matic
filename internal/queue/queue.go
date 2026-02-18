package queue

import (
	"encoding/json"
	"fmt"
	"log/slog"
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

func (q *Queue) publishJobStatus(job *models.Job) {
	if q.broadcaster == nil {
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"type":      "job_status",
		"jobID":     job.ID,
		"status":    job.Status,
		"repo":      job.RepoURL,
		"branch":    job.Branch,
		"user":      job.OwnerName,
		"priority":  job.Priority,
		"iteration": job.Iteration,
		"createdAt": job.CreatedAt,
	})
	if err != nil {
		return
	}
	q.broadcaster.Publish("global", payload)
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
	q.publishJobStatus(job)
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

	q.publishJobStatus(job)
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
	q.publishJobStatus(job)
	return nil
}

// Resume re-queues a paused job so the worker picks it up naturally.
// The job transitions paused→queued (not paused→running) because the worker
// polls via Dequeue which only selects queued jobs. The iteration counter is
// preserved so execution resumes where it left off.
func (q *Queue) Resume(job *models.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if job.Status != models.StatusPaused {
		return fmt.Errorf("cannot resume job: job is not paused (status: %s)", job.Status)
	}

	if err := job.TransitionTo(models.StatusQueued); err != nil {
		return fmt.Errorf("cannot resume job: %w", err)
	}

	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publishJobStatus(job)
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
	q.publishJobStatus(job)
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
	q.publishJobStatus(job)
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
	q.publishJobStatus(job)
	return nil
}

// Reorder changes the order of queued jobs
func (q *Queue) Reorder(jobIDs []int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.jobRepo.UpdatePositions(jobIDs); err != nil {
		return err
	}
	if q.broadcaster != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"type": "queue_reorder",
		})
		q.broadcaster.Publish("global", payload)
	}
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

// RecoverOrphaned finds jobs stuck in "running" status (orphaned after server
// crash) and re-queues them. Returns the count of recovered jobs.
// This bypasses the state machine intentionally — running→queued is not a
// normal transition and should only happen during startup recovery.
func (q *Queue) RecoverOrphaned() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	logRepo := db.NewLogRepo(q.db)

	// Find all orphaned running jobs
	jobs, _, err := q.jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusRunning},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to list running jobs: %w", err)
	}

	if len(jobs) == 0 {
		return 0, nil
	}

	recovered := 0
	for _, job := range jobs {
		// Bypass state machine: set status directly
		job.Status = models.StatusQueued
		job.StartedAt = nil

		if err := q.jobRepo.Update(job); err != nil {
			return recovered, fmt.Errorf("failed to re-queue job %d: %w", job.ID, err)
		}

		// Best-effort log entry
		msg := fmt.Sprintf("[RECOVERY] Server restarted while job was running (iteration %d/%d). Job re-queued and will resume automatically.", job.Iteration, job.MaxIterations)
		if err := logRepo.Append(job.ID, job.Iteration, msg); err != nil {
			slog.Warn("failed to append recovery log entry", "job_id", job.ID, "error", err)
		}

		q.publishJobStatus(job)
		recovered++
	}

	return recovered, nil
}
