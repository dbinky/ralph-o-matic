package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
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

	// SSE broadcaster for progress events (nil = disabled)
	broadcaster *broadcast.Broadcaster

	// Circuit breaker thresholds (0 = disabled)
	circuitBreakerNoProgress int
	circuitBreakerSameError  int

	// Retry settings
	maxRetries     int
	retryBaseDelay time.Duration

	// Rate limiting (recreated per job based on backend)
	rateLimiter *RateLimiter

	// Watchdog interval for checking external pause/cancel during iteration execution.
	watchdogInterval time.Duration

	// Progress reporting interval (0 = use default 5s)
	progressInterval time.Duration
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
		watchdogInterval:         5 * time.Second,
	}
}

// SetNotifier sets the notification dispatcher. Nil disables notifications.
func (w *Worker) SetNotifier(n JobNotifier) {
	w.notifier = n
}

// SetBroadcaster sets the SSE broadcaster for progress events.
func (w *Worker) SetBroadcaster(b *broadcast.Broadcaster) {
	w.broadcaster = b
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

	// Create a per-job context so the watchdog can interrupt the subprocess
	// when pause/cancel is detected mid-iteration.
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()

	go w.watchExternalStop(jobCtx, job.ID, jobCancel)

	// Start progress reporter (emits job_progress events while job runs)
	pr := NewProgressReporter(w.broadcaster)
	if w.progressInterval > 0 {
		pr.interval = w.progressInterval
	}
	go pr.Start(jobCtx, job)

	completedBySignal := false
	for {
		if jobCtx.Err() != nil {
			if ctx.Err() != nil {
				log.Printf("Worker: context cancelled, stopping job #%d", job.ID)
				_ = w.handler.Finalize(context.Background(), job, false)
			} else {
				w.handleExternalInterrupt(job)
			}
			return
		}

		job.IncrementIteration()
		pr.SetIteration(job.Iteration)
		log.Printf("Worker: job #%d starting iteration %d/%d", job.ID, job.Iteration, job.MaxIterations)

		if err := w.queue.Update(job); err != nil {
			log.Printf("Worker: failed to update job #%d iteration: %v", job.ID, err)
		}

		if err := w.waitForRateLimit(jobCtx, job); err != nil {
			if ctx.Err() != nil {
				_ = w.handler.Finalize(context.Background(), job, false)
			} else {
				w.handleExternalInterrupt(job)
			}
			return
		}

		result, err := w.executeWithRetry(jobCtx, job)
		if err != nil {
			// Check if this was a watchdog interruption (job context cancelled
			// but server context still active).
			if jobCtx.Err() != nil && ctx.Err() == nil {
				w.handleExternalInterrupt(job)
				return
			}
			log.Printf("Worker: job #%d failed at iteration %d: %v", job.ID, job.Iteration, err)
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
		hasProgress := detectProgress(result, job.EffectiveExitPromise())
		errMsg := extractErrorSummary(result)
		cbState := cb.RecordIteration(hasProgress, errMsg)

		if cbState == executor.CircuitOpen {
			log.Printf("Worker: job #%d circuit breaker opened after %d iterations", job.ID, job.Iteration)
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

// watchExternalStop polls the database during iteration execution to detect
// pause or cancel requests. When detected, it cancels jobCtx which kills the
// running subprocess (via exec.CommandContext) instead of waiting for the
// iteration to finish. This is complementary to checkExternalStop, which
// runs between iterations.
func (w *Worker) watchExternalStop(jobCtx context.Context, jobID int64, cancel context.CancelFunc) {
	ticker := time.NewTicker(w.watchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-jobCtx.Done():
			return
		case <-ticker.C:
			fresh, err := w.queue.Get(jobID)
			if err != nil {
				continue
			}
			switch fresh.Status {
			case models.StatusCancelled, models.StatusPaused:
				log.Printf("Worker: job #%d detected %s externally, interrupting subprocess", jobID, fresh.Status)
				cancel()
				return
			}
		}
	}
}

// handleExternalInterrupt handles the case where the watchdog cancelled the
// job context mid-iteration. It re-reads the job status to determine whether
// it was paused or cancelled, and acts accordingly (no Finalize, no failure mark).
func (w *Worker) handleExternalInterrupt(job *models.Job) {
	fresh, err := w.queue.Get(job.ID)
	if err != nil {
		log.Printf("Worker: job #%d interrupted but failed to read status: %v", job.ID, err)
		return
	}

	switch fresh.Status {
	case models.StatusPaused:
		log.Printf("Worker: job #%d paused externally at iteration %d, subprocess interrupted", job.ID, job.Iteration)
	case models.StatusCancelled:
		log.Printf("Worker: job #%d cancelled externally at iteration %d, subprocess interrupted", job.ID, job.Iteration)
		w.sendNotification(context.Background(), job, notify.EventCancelled)
	default:
		log.Printf("Worker: job #%d interrupted with unexpected status %s", job.ID, fresh.Status)
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
// Progress is detected via RALPH_STATUS file counts, git commit fallback,
// or any non-exit promise tag (explicit progress signals from the prompt,
// e.g. CLOSER, REVIEW COMPLETE).
func detectProgress(result *executor.ExecutionResult, exitPromise string) bool {
	if result == nil {
		return false
	}
	if result.Metadata != nil && result.Metadata.FilesModified > 0 {
		return true
	}
	// Check for non-exit promise tags in the parsed result text to avoid
	// false positives from intermediate reasoning in stream-json output.
	checkText := result.Output
	if result.Metadata != nil && result.Metadata.ResultText != "" {
		checkText = result.Metadata.ResultText
	}
	if executor.HasNonExitPromise(checkText, exitPromise) {
		return true
	}
	return false
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
