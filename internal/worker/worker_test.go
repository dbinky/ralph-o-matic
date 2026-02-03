package worker

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/executor"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandler implements JobHandler for testing
type mockHandler struct {
	mu       sync.Mutex
	results  []*executor.ExecutionResult // one per call, cycles if exhausted
	errors   []error
	calls    int
	handleFn func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error)
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
	return nil
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
	require.Len(t, q.completed, 1, "job should be completed")
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
	require.Len(t, q.completed, 1, "job should complete normally")
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
