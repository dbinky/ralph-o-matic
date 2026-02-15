package worker

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/executor"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandler implements JobHandler for testing
type mockHandler struct {
	mu              sync.Mutex
	results         []*executor.ExecutionResult // one per call, cycles if exhausted
	errors          []error
	calls           int
	handleFn        func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error)
	finalizeSuccess *bool // records the success arg passed to Finalize
}

func (m *mockHandler) Handle(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.calls
	m.calls++

	if m.handleFn != nil {
		return m.handleFn(ctx, job)
	}

	var result *executor.ExecutionResult
	if idx < len(m.results) {
		result = m.results[idx]
	} else if len(m.results) > 0 {
		result = m.results[len(m.results)-1]
	} else {
		result = &executor.ExecutionResult{}
	}

	var err error
	if idx < len(m.errors) {
		err = m.errors[idx]
	}

	return result, err
}

func (m *mockHandler) Finalize(ctx context.Context, job *models.Job, success bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finalizeSuccess = &success
	return nil
}

func (m *mockHandler) getFinalizeSuccess() *bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finalizeSuccess
}

func (m *mockHandler) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockQueue implements JobQueue for testing
type mockQueue struct {
	mu        sync.Mutex
	jobs      []*models.Job
	completed []*models.Job
	failed    []*models.Job

	// externalStatus overrides the status returned by Get (simulates external pause/cancel).
	// When set, Get returns a copy of the job with this status.
	externalStatus models.JobStatus
	// externalAfter is the iteration count after which externalStatus takes effect.
	externalAfter int
}

func (m *mockQueue) Dequeue() (*models.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.jobs) == 0 {
		return nil, nil
	}
	job := m.jobs[0]
	m.jobs = m.jobs[1:]
	return job, nil
}

func (m *mockQueue) Get(id int64) (*models.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// If external status is configured and we've passed the threshold, return it
	if m.externalStatus != "" && m.externalAfter >= 0 {
		return &models.Job{ID: id, Status: m.externalStatus}, nil
	}
	// Default: return running (normal state during worker execution)
	return &models.Job{ID: id, Status: models.StatusRunning}, nil
}

func (m *mockQueue) Update(job *models.Job) error {
	return nil
}

func (m *mockQueue) Complete(job *models.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completed = append(m.completed, job)
	return nil
}

func (m *mockQueue) Fail(job *models.Job, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Error = errMsg
	m.failed = append(m.failed, job)
	return nil
}

func newTestJob(maxIterations int) *models.Job {
	job := models.NewJob("git@github.com:user/repo.git", "main", "test prompt", maxIterations)
	job.ID = 1
	job.Status = models.StatusRunning
	now := time.Now()
	job.StartedAt = &now
	return job
}

// --- Step 1 Tests: Early Termination ---

func TestWorker_EarlyTermination_CompletedBeforeMax(t *testing.T) {
	// Job has max 10 iterations, but Handle signals completion on iteration 3
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false},
			{Completed: false},
			{Completed: true}, // 3rd iteration signals done
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(10)}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run one poll cycle
	w.poll(ctx)

	assert.Equal(t, 3, handler.callCount(), "should have run exactly 3 iterations")
	require.Len(t, q.completed, 1, "job should be completed")
}

func TestWorker_RunsToMaxIterations_WhenNeverCompleted(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: &executor.ResponseMetadata{FilesModified: 1}},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 5, handler.callCount(), "should have run all 5 iterations")
	require.Len(t, q.failed, 1, "job should be failed when max iterations reached without completion signal")
	assert.Contains(t, q.failed[0].Error, "max iterations reached")
	require.NotNil(t, handler.getFinalizeSuccess())
	assert.False(t, *handler.getFinalizeSuccess(), "Finalize should be called with success=false")
}

func TestWorker_EarlyTermination_FirstIteration(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true}, // done on first iteration
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(10)}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 1, handler.callCount(), "should have run exactly 1 iteration")
	require.Len(t, q.completed, 1, "job should be completed")
}

func TestWorker_EarlyTermination_LastIteration(t *testing.T) {
	// Completed=true on the last iteration (same as max) — should still finalize as success
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
			{Completed: false, Metadata: progress},
			{Completed: false, Metadata: progress},
			{Completed: false, Metadata: progress},
			{Completed: true, Metadata: progress}, // 5th = last iteration
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 5, handler.callCount(), "should have run all 5 iterations")
	require.Len(t, q.completed, 1, "job should be completed")
}

func TestWorker_HandleError_FailsJob(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{{}},
		errors:  []error{fmt.Errorf("claude crashed")},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}

	w := New(q, handler, 50*time.Millisecond)
	w.maxRetries = 0 // no retries for this test
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 1, handler.callCount())
	require.Len(t, q.failed, 1, "job should be failed")
	assert.Contains(t, q.failed[0].Error, "claude crashed")
}

// --- Circuit Breaker Integration Tests ---

func TestWorker_CircuitBreaker_OpensOnNoProgress(t *testing.T) {
	// No progress for 3 iterations → circuit opens → job fails
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: &executor.ResponseMetadata{FilesModified: 0}},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(20)}}

	w := New(q, handler, 50*time.Millisecond)
	w.circuitBreakerNoProgress = 3
	w.circuitBreakerSameError = 5
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 3, handler.callCount(), "should stop after circuit opens")
	require.Len(t, q.failed, 1, "job should be failed")
	assert.Contains(t, q.failed[0].Error, "circuit breaker")
}

func TestWorker_CircuitBreaker_ProgressPreventsOpen(t *testing.T) {
	// Alternating progress/no-progress should not open circuit
	handler := &mockHandler{
		handleFn: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			progress := job.Iteration%2 == 1 // odd iterations have progress
			return &executor.ExecutionResult{
				Completed: false,
				Metadata:  &executor.ResponseMetadata{FilesModified: boolToInt(progress)},
			}, nil
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(6)}}

	w := New(q, handler, 50*time.Millisecond)
	w.circuitBreakerNoProgress = 3
	w.circuitBreakerSameError = 5
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 6, handler.callCount(), "should run all 6 iterations")
	// Circuit breaker stayed closed (good), but job still hit max iterations without
	// a completion signal, so it should be marked as failed — not completed.
	require.Len(t, q.failed, 1, "job should fail when max iterations reached without completion signal")
	assert.Contains(t, q.failed[0].Error, "max iterations reached")
}

// --- Retry Tests ---

func TestWorker_Retry_TransientErrorThenSuccess(t *testing.T) {
	callNum := 0
	handler := &mockHandler{
		handleFn: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			callNum++
			if callNum == 1 {
				return nil, fmt.Errorf("transient error")
			}
			return &executor.ExecutionResult{Completed: true}, nil
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}

	w := New(q, handler, 50*time.Millisecond)
	w.maxRetries = 3
	w.retryBaseDelay = time.Millisecond // fast for tests
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	// Should have retried: 1 fail + 1 success = 2 calls for iteration 1
	assert.Equal(t, 2, callNum)
	require.Len(t, q.completed, 1, "job should complete after retry")
}

func TestWorker_Retry_AllAttemptsFail(t *testing.T) {
	handler := &mockHandler{
		handleFn: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			return nil, fmt.Errorf("persistent error")
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}

	w := New(q, handler, 50*time.Millisecond)
	w.maxRetries = 2
	w.retryBaseDelay = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.failed, 1, "job should fail after max retries")
	assert.Contains(t, q.failed[0].Error, "persistent error")
}

func TestWorker_Retry_ZeroRetries_FailImmediately(t *testing.T) {
	callNum := 0
	handler := &mockHandler{
		handleFn: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			callNum++
			return nil, fmt.Errorf("error")
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}

	w := New(q, handler, 50*time.Millisecond)
	w.maxRetries = 0
	w.retryBaseDelay = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 1, callNum, "should not retry with maxRetries=0")
	require.Len(t, q.failed, 1)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Notification Integration Tests ---

// mockNotifier records notification calls for testing.
type mockNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

type notifyCall struct {
	JobID int64
	Event notify.Event
}

func (m *mockNotifier) Notify(_ context.Context, job *models.Job, event notify.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, notifyCall{JobID: job.ID, Event: event})
}

func (m *mockNotifier) getCalls() []notifyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]notifyCall, len(m.calls))
	copy(result, m.calls)
	return result
}

// panicNotifier panics on every call, for testing recovery.
type panicNotifier struct{}

func (p *panicNotifier) Notify(_ context.Context, _ *models.Job, _ notify.Event) {
	panic("notifier panic!")
}

func TestWorker_Notification_CompletedJob(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(10)}}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.completed, 1)
	calls := mn.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, notify.EventCompleted, calls[0].Event)
	assert.Equal(t, int64(1), calls[0].JobID)
}

func TestWorker_Notification_FailedJob(t *testing.T) {
	handler := &mockHandler{
		handleFn: func(_ context.Context, _ *models.Job) (*executor.ExecutionResult, error) {
			return nil, fmt.Errorf("crash")
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(5)}}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	w.maxRetries = 0
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.failed, 1)
	calls := mn.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, notify.EventFailed, calls[0].Event)
}

func TestWorker_Notification_CircuitBreakerFailed(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: &executor.ResponseMetadata{FilesModified: 0}},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(20)}}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	w.circuitBreakerNoProgress = 3
	w.circuitBreakerSameError = 5
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.failed, 1)
	calls := mn.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, notify.EventFailed, calls[0].Event)
}

func TestWorker_Notification_MaxIterationsReached(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: &executor.ResponseMetadata{FilesModified: 1}},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(3)}}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.failed, 1, "job should be failed")
	assert.Contains(t, q.failed[0].Error, "max iterations reached")
	calls := mn.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, notify.EventFailed, calls[0].Event)
}

func TestWorker_Notification_NilNotifier_NoPanic(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(10)}}

	w := New(q, handler, 50*time.Millisecond)
	// Do NOT set notifier — it should be nil
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assert.NotPanics(t, func() {
		w.poll(ctx)
	})
	require.Len(t, q.completed, 1)
}

func TestWorker_Notification_PanicRecovery(t *testing.T) {
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(10)}}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(&panicNotifier{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Should not panic — worker recovers from notifier panic
	assert.NotPanics(t, func() {
		w.poll(ctx)
	})
	// Job should still be marked as completed regardless of notifier panic
	require.Len(t, q.completed, 1)
}

func TestWorker_Notification_OnlyOnTerminalState(t *testing.T) {
	// Job runs 3 iterations, completes — should get exactly one notification
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
			{Completed: false, Metadata: progress},
			{Completed: true, Metadata: progress},
		},
	}
	q := &mockQueue{jobs: []*models.Job{newTestJob(10)}}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.completed, 1)
	calls := mn.getCalls()
	require.Len(t, calls, 1, "should get exactly one notification on terminal state")
	assert.Equal(t, notify.EventCompleted, calls[0].Event)
}

// --- External Pause/Cancel Tests ---

func TestWorker_ExternalPause_StopsIterating(t *testing.T) {
	// Job has 10 max iterations, but is paused externally after iteration 2
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
		},
	}
	q := &mockQueue{
		jobs:           []*models.Job{newTestJob(10)},
		externalStatus: models.StatusPaused,
	}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	// Should have run only 1 iteration (check happens after first iteration)
	assert.Equal(t, 1, handler.callCount(), "should stop after detecting pause")
	// Job should NOT be completed or failed — status was already set by the API
	assert.Empty(t, q.completed, "paused job should not be marked completed")
	assert.Empty(t, q.failed, "paused job should not be marked failed")
}

func TestWorker_ExternalCancel_StopsIterating(t *testing.T) {
	// Job has 10 max iterations, but is cancelled externally after iteration 2
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
		},
	}
	q := &mockQueue{
		jobs:           []*models.Job{newTestJob(10)},
		externalStatus: models.StatusCancelled,
	}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Equal(t, 1, handler.callCount(), "should stop after detecting cancel")
	assert.Empty(t, q.completed, "cancelled job should not be marked completed")
	assert.Empty(t, q.failed, "cancelled job should not be marked failed")
}

func TestWorker_ExternalCancel_SendsNotification(t *testing.T) {
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
		},
	}
	q := &mockQueue{
		jobs:           []*models.Job{newTestJob(10)},
		externalStatus: models.StatusCancelled,
	}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	calls := mn.getCalls()
	require.Len(t, calls, 1, "should send exactly one notification")
	assert.Equal(t, notify.EventCancelled, calls[0].Event)
}

func TestWorker_ExternalPause_NoNotification(t *testing.T) {
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
		},
	}
	q := &mockQueue{
		jobs:           []*models.Job{newTestJob(10)},
		externalStatus: models.StatusPaused,
	}
	mn := &mockNotifier{}

	w := New(q, handler, 50*time.Millisecond)
	w.SetNotifier(mn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	calls := mn.getCalls()
	assert.Empty(t, calls, "pause should not trigger notification")
}

func TestWorker_ExternalStop_NoFinalize(t *testing.T) {
	// When paused/cancelled externally, Finalize should NOT be called
	// (no PR creation for interrupted jobs)
	progress := &executor.ResponseMetadata{FilesModified: 1}
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: false, Metadata: progress},
		},
	}
	q := &mockQueue{
		jobs:           []*models.Job{newTestJob(10)},
		externalStatus: models.StatusPaused,
	}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	assert.Nil(t, handler.getFinalizeSuccess(), "Finalize should not be called for paused jobs")
}

// --- Rate Limiter Integration Tests ---

func TestWorker_RateLimiter_OllamaJobUnlimited(t *testing.T) {
	// Ollama backend should create an unlimited rate limiter (maxPerHour=0)
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true},
		},
	}
	job := newTestJob(10)
	job.Backend = models.BackendOllama
	q := &mockQueue{jobs: []*models.Job{job}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.completed, 1)
	assert.NotNil(t, w.rateLimiter, "rate limiter should be set")
	assert.Equal(t, 0, w.rateLimiter.maxPerHour, "Ollama should have unlimited rate limiter")
}

func TestWorker_RateLimiter_AnthropicJobLimited(t *testing.T) {
	// Anthropic backend should create a rate limiter with MaxCallsPerHour from config
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true},
		},
	}
	job := newTestJob(10)
	job.Backend = models.BackendAnthropic
	q := &mockQueue{jobs: []*models.Job{job}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	w.poll(ctx)

	require.Len(t, q.completed, 1)
	assert.NotNil(t, w.rateLimiter)
	expected := models.DefaultLoopConfig(models.BackendAnthropic).MaxCallsPerHour
	assert.Equal(t, expected, w.rateLimiter.maxPerHour, "Anthropic should have configured rate limit")
}

func TestWorker_RateLimiter_BlocksWhenExceeded(t *testing.T) {
	// After handler exhausts the rate limiter on the 2nd call,
	// the 3rd iteration's waitForRateLimit blocks and context timeout stops the job.
	progress := &executor.ResponseMetadata{FilesModified: 1}
	callCount := 0
	var w *Worker

	handler := &mockHandler{
		handleFn: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			callCount++
			if callCount >= 2 {
				// Exhaust rate limiter so next waitForRateLimit blocks
				w.rateLimiter.mu.Lock()
				w.rateLimiter.count = w.rateLimiter.maxPerHour
				w.rateLimiter.mu.Unlock()
			}
			return &executor.ExecutionResult{Completed: false, Metadata: progress}, nil
		},
	}
	job := newTestJob(10)
	job.Backend = models.BackendAnthropic
	q := &mockQueue{jobs: []*models.Job{job}}

	w = New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	w.poll(ctx)

	// Should have run exactly 2 iterations (rate limit blocks 3rd, context times out)
	assert.Equal(t, 2, callCount, "should have run exactly 2 iterations before rate limit blocked")
}

func TestWorker_RateLimiter_ContextCancelDuringWait(t *testing.T) {
	// If context is cancelled while waiting for rate limit, job should stop.
	// Handler exhausts rate limiter on first call; second iteration blocks; cancel fires.
	progress := &executor.ResponseMetadata{FilesModified: 1}
	var w *Worker

	handler := &mockHandler{
		handleFn: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			// Exhaust rate limiter so next waitForRateLimit blocks
			w.rateLimiter.mu.Lock()
			w.rateLimiter.count = w.rateLimiter.maxPerHour
			w.rateLimiter.mu.Unlock()
			return &executor.ExecutionResult{Completed: false, Metadata: progress}, nil
		},
	}
	job := newTestJob(10)
	job.Backend = models.BackendAnthropic
	q := &mockQueue{jobs: []*models.Job{job}}

	w = New(q, handler, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay (long enough for 1 iteration)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	w.poll(ctx)

	// Should have run 1 iteration, then been blocked by rate limit, then context cancelled
	assert.Equal(t, 1, handler.callCount(), "should have run 1 iteration before rate limit + cancel")
}

func TestWorker_RateLimiter_RecreatedPerJob(t *testing.T) {
	// First job: Anthropic (limited), second job: Ollama (unlimited)
	handler := &mockHandler{
		results: []*executor.ExecutionResult{
			{Completed: true},
		},
	}

	job1 := newTestJob(10)
	job1.Backend = models.BackendAnthropic
	job2 := newTestJob(10)
	job2.ID = 2
	job2.Backend = models.BackendOllama

	q := &mockQueue{jobs: []*models.Job{job1, job2}}

	w := New(q, handler, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// First poll — Anthropic job
	w.poll(ctx)
	assert.Equal(t, models.DefaultLoopConfig(models.BackendAnthropic).MaxCallsPerHour, w.rateLimiter.maxPerHour)

	// Second poll — Ollama job
	w.poll(ctx)
	assert.Equal(t, 0, w.rateLimiter.maxPerHour, "rate limiter should be recreated for Ollama job")
}
