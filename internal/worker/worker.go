package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ryan/ralph-o-matic/internal/executor"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/notify"
)

// DefaultBackend is the default backend when job.Backend is empty.
// This should match the server's default.
const DefaultBackend = models.BackendOllama

// JobHandler executes a single iteration of the ralph loop.
type JobHandler interface {
	Handle(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error)
	Finalize(ctx context.Context, job *models.Job, success bool) error
}

// JobQueue manages job scheduling and state transitions.
type JobQueue interface {
	Dequeue() (*models.Job, error)
	Get(id int64) (*models.Job, error)
	Update(job *models.Job) error
	Complete(job *models.Job) error
	Fail(job *models.Job, errMsg string) error
}

// JobNotifier sends notifications about job events.
// Satisfied by *notify.Dispatcher.
type JobNotifier interface {
	Notify(ctx context.Context, job *models.Job, event notify.Event)
}

// Worker polls the queue and executes jobs.
type Worker struct {
	queue    JobQueue
	handler  JobHandler
	interval time.Duration

	// Notifications (nil = disabled)
	notifier JobNotifier

	// Circuit breaker thresholds (0 = disabled)
	circuitBreakerNoProgress int
	circuitBreakerSameError  int

	// Retry settings
	maxRetries     int
	retryBaseDelay time.Duration

	// Rate limiting (recreated per job based on backend)
	rateLimiter *RateLimiter
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

// SetNotifier sets the notification dispatcher. Nil disables notifications.
func (w *Worker) SetNotifier(n JobNotifier) {
	w.notifier = n
}

// notify sends a notification if a notifier is configured. Never panics.
func (w *Worker) sendNotification(ctx context.Context, job *models.Job, event notify.Event) {
	if w.notifier == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Worker: notification panic recovered: %v", r)
		}
	}()
	w.notifier.Notify(ctx, job, event)
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

	// Use backend-specific loop config for circuit breaker thresholds
	backend := job.Backend
	if backend == "" {
		backend = DefaultBackend
	}
	loopConfig := models.DefaultLoopConfig(backend)
	cb := executor.NewCircuitBreaker(loopConfig.CircuitBreakerNoProgress, loopConfig.CircuitBreakerSameError)
	w.rateLimiter = NewRateLimiter(loopConfig.MaxCallsPerHour)

	completedBySignal := false
	for {
		if ctx.Err() != nil {
			log.Printf("Worker: context cancelled, stopping job #%d", job.ID)
			// Clean up session to prevent memory leak (use background context since ctx is cancelled)
			_ = w.handler.Finalize(context.Background(), job, false)
			return
		}

		job.IncrementIteration()
		log.Printf("Worker: job #%d starting iteration %d/%d", job.ID, job.Iteration, job.MaxIterations)

		if err := w.queue.Update(job); err != nil {
			log.Printf("Worker: failed to update job #%d iteration: %v", job.ID, err)
		}

		if err := w.waitForRateLimit(ctx, job); err != nil {
			_ = w.handler.Finalize(context.Background(), job, false)
			return
		}

		result, err := w.executeWithRetry(ctx, job)
		if err != nil {
			log.Printf("Worker: job #%d failed at iteration %d: %v", job.ID, job.Iteration, err)
			// Clean up session to prevent memory leak
			_ = w.handler.Finalize(ctx, job, false)
			if fErr := w.queue.Fail(job, err.Error()); fErr != nil {
				log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
			}
			w.sendNotification(ctx, job, notify.EventFailed)
			return
		}

		if result != nil && result.Completed {
			log.Printf("Worker: job #%d signaled completion at iteration %d", job.ID, job.Iteration)
			completedBySignal = true
			break
		}

		// Feed circuit breaker
		hasProgress := detectProgress(result)
		errMsg := extractErrorSummary(result)
		cbState := cb.RecordIteration(hasProgress, errMsg)

		if cbState == executor.CircuitOpen {
			log.Printf("Worker: job #%d circuit breaker opened after %d iterations", job.ID, job.Iteration)
			// Clean up session to prevent memory leak
			_ = w.handler.Finalize(ctx, job, false)
			if fErr := w.queue.Fail(job, fmt.Sprintf("circuit breaker: no progress after %d iterations", job.Iteration)); fErr != nil {
				log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
			}
			w.sendNotification(ctx, job, notify.EventFailed)
			return
		}

		// Check if job was paused or cancelled externally between iterations.
		// The current iteration's work is already committed (Handle does per-iteration commits).
		if w.checkExternalStop(ctx, job) {
			return
		}

		if job.HasReachedMaxIterations() {
			log.Printf("Worker: job #%d reached max iterations (%d)", job.ID, job.MaxIterations)
			break
		}
	}

	// Finalize: commit and create PR
	if err := w.handler.Finalize(ctx, job, completedBySignal); err != nil {
		log.Printf("Worker: job #%d finalize failed: %v", job.ID, err)
		if fErr := w.queue.Fail(job, fmt.Sprintf("finalize failed: %v", err)); fErr != nil {
			log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
		}
		w.sendNotification(ctx, job, notify.EventFailed)
		return
	}

	if completedBySignal {
		if err := w.queue.Complete(job); err != nil {
			log.Printf("Worker: failed to mark job #%d as complete: %v", job.ID, err)
		} else {
			log.Printf("Worker: job #%d completed after %d iterations", job.ID, job.Iteration)
		}
		w.sendNotification(ctx, job, notify.EventCompleted)
	} else {
		log.Printf("Worker: job #%d reached max iterations (%d) without completion signal", job.ID, job.MaxIterations)
		if fErr := w.queue.Fail(job, fmt.Sprintf("max iterations reached (%d) without completion signal", job.MaxIterations)); fErr != nil {
			log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
		}
		w.sendNotification(ctx, job, notify.EventFailed)
	}
}

// checkExternalStop re-reads the job from the database to detect pause or cancel
// requests issued via the API while the worker was executing iterations.
// Returns true if the worker should stop processing this job.
func (w *Worker) checkExternalStop(ctx context.Context, job *models.Job) bool {
	fresh, err := w.queue.Get(job.ID)
	if err != nil {
		// If we can't read the job, continue processing — transient DB error
		// shouldn't stop a running job.
		log.Printf("Worker: job #%d failed to check external status: %v", job.ID, err)
		return false
	}

	switch fresh.Status {
	case models.StatusCancelled:
		log.Printf("Worker: job #%d was cancelled externally, stopping", job.ID)
		w.sendNotification(ctx, job, notify.EventCancelled)
		return true
	case models.StatusPaused:
		log.Printf("Worker: job #%d was paused externally at iteration %d, stopping", job.ID, job.Iteration)
		return true
	default:
		// Sync mutable fields that may have been updated via the API.
		if fresh.MaxIterations > 0 {
			job.MaxIterations = fresh.MaxIterations
		}
		return false
	}
}

// waitForRateLimit blocks until the rate limiter allows a call.
func (w *Worker) waitForRateLimit(ctx context.Context, job *models.Job) error {
	if w.rateLimiter == nil || w.rateLimiter.maxPerHour <= 0 {
		return nil
	}
	log.Printf("Worker: job #%d rate limit check (%d calls/hour)", job.ID, w.rateLimiter.maxPerHour)
	if err := w.rateLimiter.Wait(ctx); err != nil {
		log.Printf("Worker: job #%d rate limit wait cancelled: %v", job.ID, err)
		return err
	}
	return nil
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
