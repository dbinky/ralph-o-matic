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

	// Process any pending jobs immediately on startup
	s.processNext(ctx)

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
