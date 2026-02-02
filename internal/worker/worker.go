package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ryan/ralph-o-matic/internal/executor"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// JobHandler executes a single iteration of the ralph loop.
type JobHandler interface {
	Handle(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error)
	Finalize(ctx context.Context, job *models.Job, success bool) error
}

// JobQueue manages job scheduling and state transitions.
type JobQueue interface {
	Dequeue() (*models.Job, error)
	Update(job *models.Job) error
	Complete(job *models.Job) error
	Fail(job *models.Job, errMsg string) error
}

// Worker polls the queue and executes jobs.
type Worker struct {
	queue    JobQueue
	handler  JobHandler
	interval time.Duration

	// Circuit breaker thresholds (0 = disabled)
	circuitBreakerNoProgress int
	circuitBreakerSameError  int

	// Retry settings
	maxRetries     int
	retryBaseDelay time.Duration
}

// New creates a worker that polls the queue at the given interval.
func New(q JobQueue, handler JobHandler, interval time.Duration) *Worker {
	return &Worker{
		queue:                    q,
		handler:                  handler,
		interval:                 interval,
		circuitBreakerNoProgress: 3,
		circuitBreakerSameError:  5,
		maxRetries:               3,
		retryBaseDelay:           5 * time.Second,
	}
}

// Run polls the queue until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	log.Printf("Worker started, polling every %s", w.interval)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Worker stopping")
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Worker) poll(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker: recovered from panic: %v", r)
		}
	}()

	job, err := w.queue.Dequeue()
	if err != nil {
		log.Printf("Worker: dequeue error: %v", err)
		return
	}
	if job == nil {
		return
	}

	log.Printf("Worker: picked up job #%d (%s), max iterations: %d", job.ID, job.Branch, job.MaxIterations)

	cb := executor.NewCircuitBreaker(w.circuitBreakerNoProgress, w.circuitBreakerSameError)

	for {
		if ctx.Err() != nil {
			log.Printf("Worker: context cancelled, stopping job #%d", job.ID)
			return
		}

		job.IncrementIteration()
		log.Printf("Worker: job #%d starting iteration %d/%d", job.ID, job.Iteration, job.MaxIterations)

		if err := w.queue.Update(job); err != nil {
			log.Printf("Worker: failed to update job #%d iteration: %v", job.ID, err)
		}

		result, err := w.executeWithRetry(ctx, job)
		if err != nil {
			log.Printf("Worker: job #%d failed at iteration %d: %v", job.ID, job.Iteration, err)
			if fErr := w.queue.Fail(job, err.Error()); fErr != nil {
				log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
			}
			return
		}

		if result != nil && result.Completed {
			log.Printf("Worker: job #%d signaled completion at iteration %d", job.ID, job.Iteration)
			break
		}

		// Feed circuit breaker
		hasProgress := detectProgress(result)
		errMsg := extractErrorSummary(result)
		cbState := cb.RecordIteration(hasProgress, errMsg)

		if cbState == executor.CircuitOpen {
			log.Printf("Worker: job #%d circuit breaker opened after %d iterations", job.ID, job.Iteration)
			if fErr := w.queue.Fail(job, fmt.Sprintf("circuit breaker: no progress after %d iterations", job.Iteration)); fErr != nil {
				log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
			}
			return
		}

		if job.HasReachedMaxIterations() {
			log.Printf("Worker: job #%d reached max iterations (%d)", job.ID, job.MaxIterations)
			break
		}
	}

	// Finalize: commit and create PR
	success := true
	if err := w.handler.Finalize(ctx, job, success); err != nil {
		log.Printf("Worker: job #%d finalize failed: %v", job.ID, err)
		if fErr := w.queue.Fail(job, fmt.Sprintf("finalize failed: %v", err)); fErr != nil {
			log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
		}
		return
	}

	if err := w.queue.Complete(job); err != nil {
		log.Printf("Worker: failed to mark job #%d as complete: %v", job.ID, err)
	} else {
		log.Printf("Worker: job #%d completed after %d iterations", job.ID, job.Iteration)
	}
}

// executeWithRetry runs Handle with exponential backoff retry on errors.
func (w *Worker) executeWithRetry(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
	var lastErr error
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		if attempt > 0 {
			delay := w.retryBaseDelay * time.Duration(1<<(attempt-1)) // exponential: base, 2*base, 4*base
			log.Printf("Worker: job #%d retrying iteration %d (attempt %d/%d, waiting %s)",
				job.ID, job.Iteration, attempt+1, w.maxRetries+1, delay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := w.handler.Handle(ctx, job)
		if err == nil {
			return result, nil
		}
		lastErr = err
		log.Printf("Worker: job #%d iteration %d attempt %d failed: %v", job.ID, job.Iteration, attempt+1, err)
	}
	return nil, lastErr
}

// detectProgress checks whether an iteration made meaningful progress.
func detectProgress(result *executor.ExecutionResult) bool {
	if result == nil || result.Metadata == nil {
		return false
	}
	return result.Metadata.FilesModified > 0
}

// extractErrorSummary returns a summary error string from the result.
func extractErrorSummary(result *executor.ExecutionResult) string {
	if result == nil || result.Metadata == nil {
		return ""
	}
	if len(result.Metadata.ErrorMessages) > 0 {
		return result.Metadata.ErrorMessages[0]
	}
	return ""
}
