# Real-Time Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add live dashboard updates via enhanced SSE — real-time card transitions, progress indicators, and an expandable terminal panel on the running job.

**Architecture:** Enhance existing SSE broadcaster to emit structured event types (`job_status`, `job_progress`, `job_log`). Dashboard subscribes to a single global SSE stream. Vanilla JS handles DOM updates. No new dependencies.

**Tech Stack:** Go (existing), SSE (existing broadcaster), Vanilla JS (inline in templates)

**Design doc:** `docs/plans/2026-02-18-realtime-dashboard-design.md`

---

### Task 1: Queue publishes `job_status` events on state transitions

**Files:**
- Modify: `internal/queue/queue.go:34-38` (replace `publish()` method)
- Test: `internal/queue/queue_test.go` (new test functions)

**Step 1: Write failing tests for job_status event publishing**

Add tests to `internal/queue/queue_test.go`. These tests subscribe to the `global` broadcaster topic, trigger a queue operation, and assert the published JSON payload.

```go
func TestQueue_Enqueue_PublishesJobStatusEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	_, ch := b.Subscribe("global")

	job := models.NewJob("https://github.com/user/repo.git", "main", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "queued", evt["status"])
		assert.Equal(t, float64(job.ID), evt["jobID"])
		assert.Equal(t, "https://github.com/user/repo.git", evt["repo"])
		assert.Equal(t, "main", evt["branch"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_Dequeue_PublishesRunningEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	// Drain the enqueue event
	_, ch := b.Subscribe("global")

	dequeuedJob, err := q.Dequeue()
	require.NoError(t, err)
	require.NotNil(t, dequeuedJob)

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "running", evt["status"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_Complete_PublishesCompletedEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeuedJob, err := q.Dequeue()
	require.NoError(t, err)

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Complete(dequeuedJob))

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "completed", evt["status"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_Fail_PublishesFailedEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeuedJob, err := q.Dequeue()
	require.NoError(t, err)

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Fail(dequeuedJob, "something broke"))

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "failed", evt["status"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_Cancel_PublishesCancelledEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Cancel(job))

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "cancelled", evt["status"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_Pause_PublishesPausedEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeuedJob, err := q.Dequeue()
	require.NoError(t, err)

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Pause(dequeuedJob))

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "paused", evt["status"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_Resume_PublishesQueuedEvent(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	q := New(db)
	q.SetBroadcaster(b)

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeuedJob, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Pause(dequeuedJob))

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Resume(dequeuedJob))

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_status", evt["type"])
		assert.Equal(t, "queued", evt["status"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestQueue_NoBroadcaster_NoPublish(t *testing.T) {
	db := newTestDB(t)
	q := New(db) // No broadcaster set

	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job)) // Should not panic
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestQueue_.*Publish -short ./internal/queue/`
Expected: FAIL — `publish()` currently sends `{}` not structured events

**Step 3: Implement `publishJobStatus()` in queue.go**

Replace the existing `publish()` method with a `publishJobStatus(job)` method that marshals a structured event:

```go
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
```

Then replace every `q.publish()` call with `q.publishJobStatus(job)` in: `Enqueue`, `Dequeue`, `Pause`, `Resume`, `Complete`, `Fail`, `Cancel`, `Reorder`, `RecoverOrphaned`.

Add `"encoding/json"` to imports.

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestQueue_.*Publish -short ./internal/queue/`
Expected: PASS

**Step 5: Run full queue test suite to check for regressions**

Run: `go test -v -short ./internal/queue/`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_test.go
git commit -m "feat: publish structured job_status events on queue transitions"
```

---

### Task 2: LogRepo publishes to both `job:{id}` and `global` topics

**Files:**
- Modify: `internal/db/logs.go:46-53` (enhance broadcast in `Append`)
- Test: `internal/db/logs_test.go` (new test functions)

**Step 1: Write failing tests for dual-topic publishing**

```go
func TestLogRepo_Append_PublishesToGlobalTopic(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	repo := NewLogRepo(db)
	repo.SetBroadcaster(b)

	_, globalCh := b.Subscribe("global")

	require.NoError(t, repo.Append(42, 1, "hello world"))

	select {
	case msg := <-globalCh:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_log", evt["type"])
		assert.Equal(t, float64(42), evt["jobID"])
		assert.Equal(t, float64(1), evt["iteration"])
		assert.Equal(t, "hello world", evt["message"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for global event")
	}
}

func TestLogRepo_Append_PublishesToJobTopic(t *testing.T) {
	db := newTestDB(t)
	b := broadcast.New()
	repo := NewLogRepo(db)
	repo.SetBroadcaster(b)

	_, jobCh := b.Subscribe("job:42")

	require.NoError(t, repo.Append(42, 1, "hello world"))

	select {
	case msg := <-jobCh:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "log", evt["type"]) // Job topic keeps existing "log" type for backward compat
		assert.Equal(t, "hello world", evt["message"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job event")
	}
}

func TestLogRepo_Append_NoBroadcaster_StillWritesDB(t *testing.T) {
	db := newTestDB(t)
	repo := NewLogRepo(db) // No broadcaster

	require.NoError(t, repo.Append(1, 1, "test"))

	logs, err := repo.GetForJob(1)
	require.NoError(t, err)
	assert.Len(t, logs, 1)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestLogRepo_Append_Publishes -short ./internal/db/`
Expected: FAIL — currently only publishes to `job:{id}`, not `global`

**Step 3: Update `Append()` to publish to both topics**

In `internal/db/logs.go`, update the broadcast block in `Append()`:

```go
if r.broadcaster != nil {
	// Publish to job-specific topic (backward compatible — keeps "log" type)
	jobPayload, _ := json.Marshal(map[string]interface{}{
		"type":      "log",
		"iteration": iteration,
		"message":   message,
	})
	r.broadcaster.Publish(fmt.Sprintf("job:%d", jobID), jobPayload)

	// Publish to global topic (includes jobID for dashboard routing)
	globalPayload, _ := json.Marshal(map[string]interface{}{
		"type":      "job_log",
		"jobID":     jobID,
		"iteration": iteration,
		"message":   message,
	})
	r.broadcaster.Publish("global", globalPayload)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestLogRepo_Append -short ./internal/db/`
Expected: PASS

**Step 5: Run full db test suite**

Run: `go test -v -short ./internal/db/`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/db/logs.go internal/db/logs_test.go
git commit -m "feat: publish job_log events to both job-specific and global SSE topics"
```

---

### Task 3: ProgressReporter worker component

**Files:**
- Create: `internal/worker/progress.go`
- Create: `internal/worker/progress_test.go`

**Step 1: Write failing tests for ProgressReporter**

```go
func TestProgressReporter_EmitsProgress(t *testing.T) {
	b := broadcast.New()
	_, ch := b.Subscribe("global")

	now := time.Now()
	job := &models.Job{ID: 42, Iteration: 3, StartedAt: &now}

	pr := NewProgressReporter(b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go pr.Start(ctx, job)

	// Use a short interval for testing
	pr.interval = 50 * time.Millisecond

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_progress", evt["type"])
		assert.Equal(t, float64(42), evt["jobID"])
		assert.Equal(t, float64(3), evt["iteration"])
		assert.True(t, evt["elapsedSec"].(float64) >= 0)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for progress event")
	}
}

func TestProgressReporter_StopsOnCancel(t *testing.T) {
	b := broadcast.New()
	now := time.Now()
	job := &models.Job{ID: 1, Iteration: 1, StartedAt: &now}

	pr := NewProgressReporter(b)
	pr.interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pr.Start(ctx, job)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Good — returned promptly
	case <-time.After(time.Second):
		t.Fatal("ProgressReporter did not stop after context cancel")
	}
}

func TestProgressReporter_NilBroadcaster(t *testing.T) {
	pr := NewProgressReporter(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should not panic
	pr.Start(ctx, &models.Job{ID: 1})
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestProgressReporter -short ./internal/worker/`
Expected: FAIL — `NewProgressReporter` does not exist

**Step 3: Implement ProgressReporter**

Create `internal/worker/progress.go`:

```go
package worker

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/models"
)

const defaultProgressInterval = 5 * time.Second

// ProgressReporter emits job_progress events to the global SSE topic
// on a periodic interval while a job is running.
type ProgressReporter struct {
	broadcaster *broadcast.Broadcaster
	interval    time.Duration
}

// NewProgressReporter creates a new ProgressReporter.
func NewProgressReporter(b *broadcast.Broadcaster) *ProgressReporter {
	return &ProgressReporter{
		broadcaster: b,
		interval:    defaultProgressInterval,
	}
}

// Start emits progress events until ctx is cancelled.
// Blocks until done — call in a goroutine.
func (p *ProgressReporter) Start(ctx context.Context, job *models.Job) {
	if p.broadcaster == nil {
		<-ctx.Done()
		return
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.emit(job)
		}
	}
}

func (p *ProgressReporter) emit(job *models.Job) {
	elapsed := 0.0
	if job.StartedAt != nil {
		elapsed = math.Round(time.Since(*job.StartedAt).Seconds())
	}

	payload, err := json.Marshal(map[string]interface{}{
		"type":       "job_progress",
		"jobID":      job.ID,
		"iteration":  job.Iteration,
		"elapsedSec": elapsed,
	})
	if err != nil {
		return
	}
	p.broadcaster.Publish("global", payload)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestProgressReporter -short ./internal/worker/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worker/progress.go internal/worker/progress_test.go
git commit -m "feat: add ProgressReporter for periodic job_progress SSE events"
```

---

### Task 4: Wire ProgressReporter into worker loop

**Files:**
- Modify: `internal/worker/worker.go` (add ProgressReporter field, start/stop in poll)
- Modify: `cmd/server/main.go` (pass broadcaster to worker)

**Step 1: Write failing test for worker starting progress reporter**

In `internal/worker/worker_test.go`, add a test that verifies the worker starts the progress reporter when processing a job. This is an integration-style test using the existing worker test infrastructure.

```go
func TestWorker_StartsProgressReporter(t *testing.T) {
	b := broadcast.New()
	_, ch := b.Subscribe("global")

	q := &mockQueue{
		dequeueFunc: func() (*models.Job, error) {
			now := time.Now()
			return &models.Job{
				ID: 1, Status: models.StatusRunning,
				Iteration: 1, MaxIterations: 2,
				StartedAt: &now,
				RepoURL: "https://github.com/user/repo.git",
				Branch: "main", Prompt: "test",
			}, nil
		},
	}

	handler := &mockHandler{
		handleFunc: func(ctx context.Context, job *models.Job) (*executor.ExecutionResult, error) {
			time.Sleep(200 * time.Millisecond) // Give progress reporter time to tick
			return &executor.ExecutionResult{Completed: true}, nil
		},
	}

	w := New(q, handler, time.Second)
	w.SetBroadcaster(b)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go w.Run(ctx)

	// Should receive at least one job_progress event
	var gotProgress bool
	timeout := time.After(time.Second)
	for !gotProgress {
		select {
		case msg := <-ch:
			var evt map[string]interface{}
			if json.Unmarshal(msg, &evt) == nil && evt["type"] == "job_progress" {
				gotProgress = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for job_progress event")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestWorker_StartsProgressReporter -short ./internal/worker/`
Expected: FAIL — `SetBroadcaster` doesn't exist on Worker

**Step 3: Add broadcaster to Worker and start ProgressReporter in poll()**

In `internal/worker/worker.go`:

1. Add field: `broadcaster *broadcast.Broadcaster`
2. Add method: `SetBroadcaster(b *broadcast.Broadcaster)`
3. In `poll()`, after dequeue succeeds, start the progress reporter:

```go
// In poll(), after creating jobCtx:
pr := NewProgressReporter(w.broadcaster)
pr.interval = 100 * time.Millisecond // Use short interval for tests; override in test
go pr.Start(jobCtx, job)
// jobCancel() already defers, which will stop the reporter
```

4. In `cmd/server/main.go`, after creating the worker, add:

```go
w.SetBroadcaster(b)
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestWorker_StartsProgressReporter -short ./internal/worker/`
Expected: PASS

**Step 5: Run full worker and server test suites**

Run: `go test -v -short ./internal/worker/ && go test -v -short ./cmd/server/`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/worker/worker.go cmd/server/main.go
git commit -m "feat: wire ProgressReporter into worker loop"
```

---

### Task 5: Remove admin guard from global SSE and add dashboard-state endpoint

**Files:**
- Modify: `internal/api/server.go:94` (remove `auth.RequireRole("Admin", ...)` wrapper)
- Modify: `internal/api/server.go` (add `handleDashboardState` route)
- Create: `internal/api/dashboard_state.go` (new handler)
- Modify: `internal/api/sse_test.go` (update admin-only tests)
- Create: `internal/api/dashboard_state_test.go`

**Step 1: Update existing SSE tests — admin guard removal**

In `internal/api/sse_test.go`:

- `TestSSE_GlobalEvents_NonAdminGetsForbidden` — change expected status from 403 to 200
- Rename to `TestSSE_GlobalEvents_NonAdminCanAccess` to match new behavior

```go
func TestSSE_GlobalEvents_NonAdminCanAccess(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	req := httptest.NewRequest("GET", "/api/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, w.Code)
}
```

**Step 2: Write failing tests for dashboard-state endpoint**

In `internal/api/dashboard_state_test.go`:

```go
func TestDashboardState_ReturnsActiveJobs(t *testing.T) {
	srv, database, _ := newTestServerWithBroadcaster(t)

	// Create jobs in different states
	jobRepo := db.NewJobRepo(database)
	running := models.NewJob("https://github.com/user/repo.git", "feat-a", "test", 10)
	running.Status = models.StatusRunning
	now := time.Now()
	running.StartedAt = &now
	running.Iteration = 3
	require.NoError(t, jobRepo.Create(running))

	queued := models.NewJob("https://github.com/user/repo.git", "feat-b", "test", 5)
	require.NoError(t, jobRepo.Create(queued))

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dashboard-state")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	jobs := result["jobs"].([]interface{})
	assert.Len(t, jobs, 2)
}

func TestDashboardState_EmptyQueue(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dashboard-state")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	jobs := result["jobs"].([]interface{})
	assert.Len(t, jobs, 0)
}
```

**Step 3: Run tests to verify they fail**

Run: `go test -v -run "TestSSE_GlobalEvents_NonAdmin|TestDashboardState" -short ./internal/api/`
Expected: FAIL

**Step 4: Remove admin guard and implement dashboard-state handler**

In `internal/api/server.go`, change line 94 from:
```go
r.Get("/api/events", auth.RequireRole("Admin", s.handleSSEGlobal))
```
to:
```go
r.Get("/api/events", s.handleSSEGlobal)
```

Add the dashboard-state route inside the timeout group:
```go
r.Get("/api/dashboard-state", s.handleDashboardState)
```

Create `internal/api/dashboard_state.go`:

```go
package api

import (
	"net/http"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// DashboardJob is a lightweight job summary for SSE reconnect reconciliation.
type DashboardJob struct {
	ID        int64            `json:"id"`
	Status    models.JobStatus `json:"status"`
	Repo      string           `json:"repo"`
	Branch    string           `json:"branch"`
	User      string           `json:"user"`
	Priority  models.Priority  `json:"priority"`
	Iteration int              `json:"iteration"`
	CreatedAt string           `json:"createdAt"`
}

func (s *Server) handleDashboardState(w http.ResponseWriter, r *http.Request) {
	jobRepo := db.NewJobRepo(s.db)

	// Fetch all non-terminal jobs (running, paused, queued)
	activeStatuses := []models.JobStatus{
		models.StatusRunning,
		models.StatusPaused,
		models.StatusQueued,
	}

	jobs, _, err := jobRepo.List(db.ListOptions{
		Statuses: activeStatuses,
		Limit:    100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Also include recent terminal jobs (last 10)
	terminal, _, err := jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{
			models.StatusCompleted,
			models.StatusFailed,
			models.StatusCancelled,
		},
		Limit: 10,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	allJobs := append(jobs, terminal...)

	result := make([]DashboardJob, 0, len(allJobs))
	for _, j := range allJobs {
		result = append(result, DashboardJob{
			ID:        j.ID,
			Status:    j.Status,
			Repo:      j.RepoURL,
			Branch:    j.Branch,
			User:      j.OwnerName,
			Priority:  j.Priority,
			Iteration: j.Iteration,
			CreatedAt: j.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"jobs": result})
}
```

**Step 5: Run tests to verify they pass**

Run: `go test -v -run "TestSSE_GlobalEvents|TestDashboardState" -short ./internal/api/`
Expected: PASS

**Step 6: Run full API test suite**

Run: `go test -v -short ./internal/api/`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/api/server.go internal/api/dashboard_state.go internal/api/dashboard_state_test.go internal/api/sse_test.go
git commit -m "feat: open global SSE to all users, add /api/dashboard-state endpoint"
```

---

### Task 6: Dashboard HTML — expandable panel structure and CSS

**Files:**
- Modify: `web/templates/dashboard.html` (add expandable panel HTML to running job cards)
- Modify: `web/templates/layout.html` (add CSS for expandable panel and terminal)

**Step 1: Add CSS to layout.html**

Add these styles to `web/templates/layout.html` before `</style>`:

```css
/* Expandable panel */
.job-expand-toggle {
    background: none;
    border: none;
    color: #888;
    cursor: pointer;
    font-size: 0.875rem;
    padding: 4px 8px;
    transition: color 0.2s;
}
.job-expand-toggle:hover {
    color: #fff;
}
.job-detail-panel {
    display: none;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid #333;
}
.job-detail-panel.expanded {
    display: block;
}
.job-details-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    margin-bottom: 10px;
    font-size: 0.875rem;
}
.job-details-grid dt {
    color: #888;
}
.job-details-grid dd {
    color: #eee;
    margin: 0;
}

/* Terminal window */
.job-terminal {
    background: #0d1117;
    border: 1px solid #333;
    border-radius: 6px;
    padding: 10px;
    font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
    font-size: 0.8rem;
    line-height: 1.4;
    color: #c9d1d9;
    height: 420px; /* ~30 lines */
    overflow-y: auto;
    white-space: pre-wrap;
    word-break: break-all;
}
.job-terminal .terminal-line {
    display: block;
}
.job-terminal-empty {
    color: #555;
    font-style: italic;
    text-align: center;
    padding: 20px;
}
```

**Step 2: Update running job card template in dashboard.html**

Replace the running jobs section with:

```html
{{range .Running}}
<div class="job-card running" data-job-id="{{.ID}}">
    <div class="job-header">
        <div>
            <button class="job-expand-toggle" onclick="toggleJobPanel({{.ID}})" data-job-id="{{.ID}}">&#9654;</button>
            <span class="job-id">#{{.ID}}</span>
            <span class="job-branch">{{.Branch}}</span>
        </div>
        <span class="job-iter" data-job-id="{{.ID}}">iter {{.Iteration}}/{{.MaxIterations}}</span>
    </div>
    <div class="job-progress">
        <div class="job-progress-bar" data-job-id="{{.ID}}" style="width: {{printf "%.0f" (multiply .Progress 100)}}%"></div>
    </div>
    <div class="job-meta">
        <span>{{.Prompt | truncate 50}}</span>
        <span class="job-elapsed" data-job-id="{{.ID}}">Running {{.Duration | duration}}</span>
        {{if .OwnerName}}<span>by {{.OwnerName}}</span>{{end}}
    </div>
    <div class="job-actions">
        <button class="btn btn-secondary" onclick="pauseJob({{.ID}})">Pause</button>
        <button class="btn btn-danger" onclick="cancelJob({{.ID}})">Cancel</button>
    </div>
    <div class="job-detail-panel" id="panel-{{.ID}}">
        <dl class="job-details-grid">
            <dt>Repository</dt><dd>{{.RepoURL}}</dd>
            <dt>Branch</dt><dd>{{.Branch}}</dd>
            <dt>User</dt><dd>{{if .OwnerName}}{{.OwnerName}}{{else}}—{{end}}</dd>
            <dt>Priority</dt><dd>{{.Priority}}</dd>
            <dt>Iterations</dt><dd>{{.Iteration}}/{{.MaxIterations}}</dd>
            <dt>Started</dt><dd>{{.StartedAt | timeago}}</dd>
        </dl>
        <div class="job-terminal" id="terminal-{{.ID}}">
            <span class="job-terminal-empty">Connecting to live output...</span>
        </div>
    </div>
</div>
{{end}}
```

**Step 3: Verify templates parse correctly**

Run: `go build ./cmd/server/`
Expected: BUILD SUCCESS (templates are embedded at compile time)

**Step 4: Commit**

```bash
git add web/templates/dashboard.html web/templates/layout.html
git commit -m "feat: add expandable panel HTML structure and terminal CSS to dashboard"
```

---

### Task 7: Dashboard JavaScript — SSE event handling and DOM updates

**Files:**
- Modify: `web/templates/dashboard.html` (replace `{{define "scripts"}}` block)

**Step 1: Replace the scripts block in dashboard.html**

Replace the entire `{{define "scripts"}}...{{end}}` block with the new JavaScript. This is the core frontend logic.

```html
{{define "scripts"}}
<script src="https://cdn.jsdelivr.net/npm/sortablejs@1.15.0/Sortable.min.js"></script>
<script>
(function() {
    'use strict';

    // --- State ---
    const logBuffers = {};  // jobID -> string[]
    const MAX_LOG_LINES = 300;
    const expandedPanels = new Set();
    let backfilledJobs = new Set();

    // --- Sortable (existing) ---
    const queuedEl = document.getElementById('queued-jobs');
    if (queuedEl) {
        new Sortable(queuedEl, {
            animation: 150,
            handle: '.drag-handle',
            ghostClass: 'sortable-ghost',
            onEnd: function() {
                const ids = Array.from(queuedEl.querySelectorAll('.job-card'))
                    .map(c => parseInt(c.dataset.jobId));
                reorderJobs(ids);
            }
        });
    }

    // --- Job actions ---
    window.pauseJob = async function(id) {
        await fetch('/api/jobs/' + id + '/pause', { method: 'POST' });
    };
    window.resumeJob = async function(id) {
        await fetch('/api/jobs/' + id + '/resume', { method: 'POST' });
    };
    window.cancelJob = async function(id) {
        if (confirm('Cancel this job?')) {
            await fetch('/api/jobs/' + id, { method: 'DELETE' });
        }
    };
    async function reorderJobs(jobIds) {
        await fetch('/api/jobs/order', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ job_ids: jobIds })
        });
    }

    // --- Expand/collapse ---
    window.toggleJobPanel = function(jobId) {
        const panel = document.getElementById('panel-' + jobId);
        const toggle = document.querySelector('.job-expand-toggle[data-job-id="' + jobId + '"]');
        if (!panel) return;

        if (expandedPanels.has(jobId)) {
            panel.classList.remove('expanded');
            expandedPanels.delete(jobId);
            if (toggle) toggle.innerHTML = '&#9654;'; // right arrow
        } else {
            panel.classList.add('expanded');
            expandedPanels.add(jobId);
            if (toggle) toggle.innerHTML = '&#9660;'; // down arrow

            // Backfill logs on first expand
            if (!backfilledJobs.has(jobId)) {
                backfilledJobs.add(jobId);
                backfillLogs(jobId);
            } else {
                renderTerminal(jobId);
            }
        }
    };

    // --- Log backfill ---
    async function backfillLogs(jobId) {
        try {
            const resp = await fetch('/api/jobs/' + jobId + '/logs');
            const data = await resp.json();
            if (data.logs && data.logs.length > 0) {
                if (!logBuffers[jobId]) logBuffers[jobId] = [];
                const existing = logBuffers[jobId];
                // Prepend historical logs before any live-buffered ones
                const historical = data.logs.map(l => l.message);
                logBuffers[jobId] = historical.concat(existing).slice(-MAX_LOG_LINES);
            }
        } catch (e) {
            console.error('Failed to backfill logs:', e);
        }
        renderTerminal(jobId);
    }

    // --- Terminal rendering ---
    function renderTerminal(jobId) {
        const terminal = document.getElementById('terminal-' + jobId);
        if (!terminal) return;

        const lines = logBuffers[jobId] || [];
        if (lines.length === 0) {
            terminal.innerHTML = '<span class="job-terminal-empty">No output yet...</span>';
            return;
        }

        terminal.innerHTML = lines.map(function(line) {
            return '<span class="terminal-line">' + escapeHtml(line) + '</span>';
        }).join('');

        terminal.scrollTop = terminal.scrollHeight;
    }

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    // --- Ring buffer append ---
    function appendLog(jobId, message) {
        if (!logBuffers[jobId]) logBuffers[jobId] = [];
        logBuffers[jobId].push(message);
        if (logBuffers[jobId].length > MAX_LOG_LINES) {
            logBuffers[jobId].shift();
        }

        // Only render if panel is expanded
        if (expandedPanels.has(jobId)) {
            const terminal = document.getElementById('terminal-' + jobId);
            if (terminal) {
                // Append single line instead of re-rendering all
                const emptyMsg = terminal.querySelector('.job-terminal-empty');
                if (emptyMsg) emptyMsg.remove();

                const span = document.createElement('span');
                span.className = 'terminal-line';
                span.textContent = message;
                terminal.appendChild(span);

                // Prune DOM if over limit
                while (terminal.children.length > MAX_LOG_LINES) {
                    terminal.removeChild(terminal.firstChild);
                }

                terminal.scrollTop = terminal.scrollHeight;
            }
        }
    }

    // --- SSE event handling ---
    function handleStatusChange(data) {
        // Reload the page for status changes — simplest correct approach
        // for moving cards between sections, creating/removing panels, etc.
        // The SSE reconnect will re-establish the connection automatically.
        location.reload();
    }

    function handleProgress(data) {
        const jobId = data.jobID;
        const iterEl = document.querySelector('.job-iter[data-job-id="' + jobId + '"]');
        if (iterEl) {
            // We don't have maxIterations in the progress event, so just update iteration
            iterEl.textContent = 'iter ' + data.iteration + iterEl.textContent.replace(/^iter \d+/, '');
        }

        const elapsedEl = document.querySelector('.job-elapsed[data-job-id="' + jobId + '"]');
        if (elapsedEl) {
            const secs = Math.round(data.elapsedSec);
            const mins = Math.floor(secs / 60);
            const hours = Math.floor(mins / 60);
            if (hours > 0) {
                elapsedEl.textContent = 'Running ' + hours + 'h' + (mins % 60) + 'm';
            } else if (mins > 0) {
                elapsedEl.textContent = 'Running ' + mins + 'm';
            } else {
                elapsedEl.textContent = 'Running < 1m';
            }
        }

        // Update progress bar
        const barEl = document.querySelector('.job-progress-bar[data-job-id="' + jobId + '"]');
        if (barEl && data.iteration) {
            const card = barEl.closest('.job-card');
            const iterText = card ? card.querySelector('.job-iter') : null;
            if (iterText) {
                const match = iterText.textContent.match(/iter (\d+)\/(\d+)/);
                if (match) {
                    const pct = (parseInt(match[1]) / parseInt(match[2])) * 100;
                    barEl.style.width = pct + '%';
                }
            }
        }

        // Update iteration in detail panel if expanded
        const panelIterDd = document.querySelector('#panel-' + jobId + ' dd:nth-of-type(5)');
        if (panelIterDd) {
            const match = panelIterDd.textContent.match(/\d+\/(\d+)/);
            if (match) {
                panelIterDd.textContent = data.iteration + '/' + match[1];
            }
        }
    }

    function handleLog(data) {
        appendLog(data.jobID, data.message);
    }

    // --- SSE connection ---
    let evtSource;

    function connectSSE() {
        evtSource = new EventSource('/api/events');

        evtSource.onmessage = function(event) {
            try {
                const data = JSON.parse(event.data);
                switch (data.type) {
                    case 'job_status':
                        handleStatusChange(data);
                        break;
                    case 'job_progress':
                        handleProgress(data);
                        break;
                    case 'job_log':
                        handleLog(data);
                        break;
                }
            } catch (e) {
                // Ignore malformed events (including legacy "{}" events)
            }
        };

        evtSource.onerror = function() {
            // EventSource auto-reconnects. On reconnect, reconcile state.
            if (evtSource.readyState === EventSource.CONNECTING) {
                reconcileDashboard();
            }
        };
    }

    async function reconcileDashboard() {
        try {
            const resp = await fetch('/api/dashboard-state');
            if (resp.ok) {
                // Simple reconciliation: reload if state might have changed
                // A full DOM-diff is overkill — SSE reconnects are rare
                location.reload();
            }
        } catch (e) {
            // Network still down, EventSource will retry
        }
    }

    connectSSE();
})();
</script>
{{end}}
```

**Step 2: Verify templates parse and server builds**

Run: `go build ./cmd/server/`
Expected: BUILD SUCCESS

**Step 3: Commit**

```bash
git add web/templates/dashboard.html
git commit -m "feat: add SSE event handling, terminal rendering, and live progress updates"
```

---

### Task 8: Integration test — full SSE event flow

**Files:**
- Create: `internal/api/dashboard_integration_test.go`

**Step 1: Write integration test for full event flow**

This test verifies the end-to-end flow: queue operation -> broadcaster -> SSE client receives structured event.

```go
func TestIntegration_SSE_ReceivesStructuredEvents(t *testing.T) {
	srv, database, b := newTestServerWithBroadcaster(t)
	q := queue.New(database)
	q.SetBroadcaster(b)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Connect SSE client
	resp, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Create and enqueue a job (triggers job_status event)
	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	// Read the SSE event
	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan())
	line := strings.TrimPrefix(scanner.Text(), "data: ")

	var evt map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt))
	assert.Equal(t, "job_status", evt["type"])
	assert.Equal(t, "queued", evt["status"])
	assert.Equal(t, "main", evt["branch"])
}

func TestIntegration_SSE_LogEventsOnGlobal(t *testing.T) {
	srv, database, b := newTestServerWithBroadcaster(t)

	logRepo := db.NewLogRepo(database)
	logRepo.SetBroadcaster(b)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Append a log (triggers job_log on global topic)
	require.NoError(t, logRepo.Append(42, 1, "test output line"))

	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan())
	line := strings.TrimPrefix(scanner.Text(), "data: ")

	var evt map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt))
	assert.Equal(t, "job_log", evt["type"])
	assert.Equal(t, float64(42), evt["jobID"])
	assert.Equal(t, "test output line", evt["message"])
}
```

**Step 2: Run integration tests**

Run: `go test -v -run TestIntegration_SSE -short ./internal/api/`
Expected: PASS (depends on Tasks 1-2 and 5 being complete)

**Step 3: Commit**

```bash
git add internal/api/dashboard_integration_test.go
git commit -m "test: add integration tests for structured SSE event flow"
```

---

### Task 9: Full regression test and cleanup

**Files:**
- No new files — final verification pass

**Step 1: Run full test suite**

Run: `go test -v -short -race ./...`
Expected: ALL PASS with no races

**Step 2: Run linter**

Run: `make lint`
Expected: PASS

**Step 3: Build all platforms**

Run: `make build`
Expected: BUILD SUCCESS

**Step 4: Manual smoke test (if server accessible)**

Start the server locally and verify:
1. Dashboard loads at `/`
2. SSE connects (check browser DevTools Network tab for `/api/events`)
3. Submit a test job and observe card appearing without page reload
4. Expand running job panel — terminal shows live output
5. Progress indicators update while job runs

**Step 5: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: final cleanup for real-time dashboard feature"
```
