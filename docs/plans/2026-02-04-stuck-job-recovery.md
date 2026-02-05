# Stuck Job Recovery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Recover orphaned jobs (stuck in `running` status) after a server crash or restart by re-queuing them at startup.

**Architecture:** New `RecoverOrphaned()` method on `queue.Queue` that directly sets orphaned running jobs back to `queued` status (bypassing the state machine, since `running→queued` is intentionally not a valid transition). Called once at server startup, before the worker polling loop begins. Uses `LogRepo.Append()` best-effort to record recovery events.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), testify, slog

**Design doc:** `docs/plans/2026-02-04-stuck-job-recovery-design.md`

---

### Task 1: Recovery Tests — Happy Path

**Files:**
- Create: `internal/queue/recovery_test.go`

**Step 1: Write the test file with the happy-path test**

```go
package queue

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverOrphaned_RunningJobRequeued(t *testing.T) {
	q, database := newTestQueue(t)
	logRepo := db.NewLogRepo(database)

	// Create a job and move it to running via Dequeue
	job := models.NewJob("git@github.com:user/repo.git", "main", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	// Simulate progress
	dequeued.Iteration = 3
	require.NoError(t, q.Update(dequeued))

	// Now recover — simulating server restart
	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify job is back to queued
	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Nil(t, recovered.StartedAt, "StartedAt should be cleared")
	assert.Equal(t, 3, recovered.Iteration, "Iteration count should be preserved")

	// Verify log entry was written
	logs, err := logRepo.GetForJob(dequeued.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Message, "[RECOVERY]")
	assert.Contains(t, logs[0].Message, "iteration 3/10")
}
```

**Step 2: Run the test to verify it fails**

Run: `go test -v -run TestRecoverOrphaned_RunningJobRequeued ./internal/queue/`
Expected: Compilation error — `q.RecoverOrphaned undefined`

**Step 3: Commit the failing test**

```bash
git add internal/queue/recovery_test.go
git commit -m "test: add failing test for stuck job recovery happy path"
```

---

### Task 2: Minimal RecoverOrphaned Implementation

**Files:**
- Modify: `internal/queue/queue.go`

**Step 1: Add the `RecoverOrphaned` method to `queue.Queue`**

Add these imports to the existing import block in `internal/queue/queue.go`:

```go
"log/slog"
```

Then add the method at the end of the file:

```go
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

		recovered++
	}

	if recovered > 0 {
		q.publish()
	}

	return recovered, nil
}
```

**Step 2: Run the test to verify it passes**

Run: `go test -v -run TestRecoverOrphaned_RunningJobRequeued ./internal/queue/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/queue/queue.go
git commit -m "feat: add RecoverOrphaned method for stuck job recovery"
```

---

### Task 3: Recovery Tests — Multiple Jobs and Edge Cases

**Files:**
- Modify: `internal/queue/recovery_test.go`

**Step 1: Add test for multiple running jobs recovered**

Append to `internal/queue/recovery_test.go`:

```go
func TestRecoverOrphaned_MultipleRunningJobs(t *testing.T) {
	q, _ := newTestQueue(t)

	// Create 3 jobs and move them all to running
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, q.Enqueue(job))
		_, err := q.Dequeue()
		require.NoError(t, err)
	}

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// All should be queued now
	running := q.GetRunning()
	assert.Len(t, running, 0)
	assert.Equal(t, 3, q.Size())
}
```

**Step 2: Add test for no orphaned jobs**

```go
func TestRecoverOrphaned_NoOrphanedJobs(t *testing.T) {
	q, _ := newTestQueue(t)

	// Enqueue a job but don't dequeue it (stays queued)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
```

**Step 3: Add test that other statuses are not touched**

```go
func TestRecoverOrphaned_OnlyRunningJobsTouched(t *testing.T) {
	q, _ := newTestQueue(t)

	// Queued job
	queued := models.NewJob("git@github.com:user/repo.git", "main", "queued", 10)
	require.NoError(t, q.Enqueue(queued))

	// Running job (will be recovered)
	running := models.NewJob("git@github.com:user/repo.git", "main", "running", 10)
	require.NoError(t, q.Enqueue(running))
	dequeuedRunning, err := q.Dequeue()
	require.NoError(t, err)

	// Paused job (should NOT be recovered)
	paused := models.NewJob("git@github.com:user/repo.git", "main", "paused", 10)
	require.NoError(t, q.Enqueue(paused))
	dequeuedPaused, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Pause(dequeuedPaused))

	// Completed job
	completed := models.NewJob("git@github.com:user/repo.git", "main", "completed", 10)
	require.NoError(t, q.Enqueue(completed))
	dequeuedCompleted, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Complete(dequeuedCompleted))

	// Failed job
	failed := models.NewJob("git@github.com:user/repo.git", "main", "failed", 10)
	require.NoError(t, q.Enqueue(failed))
	dequeuedFailed, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Fail(dequeuedFailed, "some error"))

	// Cancelled job
	cancelled := models.NewJob("git@github.com:user/repo.git", "main", "cancelled", 10)
	require.NoError(t, q.Enqueue(cancelled))
	require.NoError(t, q.Cancel(cancelled))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Only the running job should be recovered")

	// Verify the running job was recovered
	recoveredJob, err := q.Get(dequeuedRunning.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recoveredJob.Status)

	// Verify paused job is still paused
	pausedJob, err := q.Get(dequeuedPaused.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusPaused, pausedJob.Status)

	// Verify completed job is still completed
	completedJob, err := q.Get(dequeuedCompleted.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusCompleted, completedJob.Status)
}
```

**Step 4: Run all recovery tests**

Run: `go test -v -run TestRecoverOrphaned ./internal/queue/`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/queue/recovery_test.go
git commit -m "test: add edge case tests for stuck job recovery"
```

---

### Task 4: Recovery Tests — Field Preservation and Round-Trip

**Files:**
- Modify: `internal/queue/recovery_test.go`

**Step 1: Add test for field preservation**

Append to `internal/queue/recovery_test.go`:

```go
func TestRecoverOrphaned_PreservesFields(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "feature-branch", "do stuff", 10)
	job.Priority = models.PriorityHigh
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	// Set some fields that should survive recovery
	dequeued.Iteration = 7
	dequeued.ResultBranch = "ralph/feature-branch-result"
	dequeued.RetryCount = 2
	require.NoError(t, q.Update(dequeued))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Nil(t, recovered.StartedAt)
	assert.Equal(t, 7, recovered.Iteration)
	assert.Equal(t, "ralph/feature-branch-result", recovered.ResultBranch)
	assert.Equal(t, models.PriorityHigh, recovered.Priority)
	assert.Equal(t, "feature-branch", recovered.Branch)
	assert.Equal(t, "do stuff", recovered.Prompt)
	assert.Equal(t, 2, recovered.RetryCount)
	assert.Equal(t, 10, recovered.MaxIterations)
}
```

**Step 2: Add round-trip test (recovered job can be dequeued)**

```go
func TestRecoverOrphaned_RecoveredJobPickedUpByDequeue(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	// Recover
	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Dequeue should pick it back up
	redequeued, err := q.Dequeue()
	require.NoError(t, err)
	require.NotNil(t, redequeued)
	assert.Equal(t, dequeued.ID, redequeued.ID)
	assert.Equal(t, models.StatusRunning, redequeued.Status)
}
```

**Step 3: Add idempotency test**

```go
func TestRecoverOrphaned_IdempotentSecondCall(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	_, err := q.Dequeue()
	require.NoError(t, err)

	// First recovery
	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Second recovery — nothing to do
	count, err = q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
```

**Step 4: Add zero-iteration test (crashed immediately after dequeue)**

```go
func TestRecoverOrphaned_ZeroIterationJob(t *testing.T) {
	q, database := newTestQueue(t)
	logRepo := db.NewLogRepo(database)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)
	// Iteration is 0 — crashed before any work done

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Equal(t, 0, recovered.Iteration)

	logs, err := logRepo.GetForJob(dequeued.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Message, "iteration 0/10")
}
```

**Step 5: Add max-iteration test**

```go
func TestRecoverOrphaned_MaxIterationJob(t *testing.T) {
	q, _ := newTestQueue(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 5)
	require.NoError(t, q.Enqueue(job))
	dequeued, err := q.Dequeue()
	require.NoError(t, err)

	dequeued.Iteration = 5 // At max
	require.NoError(t, q.Update(dequeued))

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Re-queued even at max — worker handles the max-iteration check
	recovered, err := q.Get(dequeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.StatusQueued, recovered.Status)
	assert.Equal(t, 5, recovered.Iteration)
}
```

**Step 6: Run all recovery tests**

Run: `go test -v -run TestRecoverOrphaned ./internal/queue/`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/queue/recovery_test.go
git commit -m "test: add field preservation, round-trip, and edge case recovery tests"
```

---

### Task 5: Recovery Tests — Broadcast Integration

**Files:**
- Modify: `internal/queue/recovery_test.go`

**Step 1: Add broadcast test**

Append to `internal/queue/recovery_test.go`:

```go
func TestRecoverOrphaned_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))
	_, err := q.Dequeue()
	require.NoError(t, err)

	_, ch := b.Subscribe("global")

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast after recovery")
	}
}

func TestRecoverOrphaned_NoBroadcastWhenNothingRecovered(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	_, ch := b.Subscribe("global")

	count, err := q.RecoverOrphaned()
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	select {
	case <-ch:
		t.Fatal("should not broadcast when nothing was recovered")
	default:
		// Good — no broadcast
	}
}
```

Note: you'll need to add `"time"` and `"github.com/ryan/ralph-o-matic/internal/broadcast"` to the import block if not already present.

**Step 2: Run all recovery tests**

Run: `go test -v -run TestRecoverOrphaned ./internal/queue/`
Expected: All PASS

**Step 3: Commit**

```bash
git add internal/queue/recovery_test.go
git commit -m "test: add broadcast integration tests for recovery"
```

---

### Task 6: Startup Integration

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: Add recovery call to `run()` function**

In `cmd/server/main.go`, find the line `q := queue.New(database)` (line 60). Insert the recovery call after queue creation and broadcaster setup, but before auth config loading. Specifically, add these lines after `q.SetBroadcaster(b)` (after line 63):

```go
	// Recover jobs orphaned by a previous server crash/restart
	recovered, err := q.RecoverOrphaned()
	if err != nil {
		return fmt.Errorf("failed to recover orphaned jobs: %w", err)
	}
	if recovered > 0 {
		slog.Info("recovered orphaned jobs", "count", recovered)
	}
```

**Step 2: Build to verify compilation**

Run: `make build`
Expected: Clean build, no errors

**Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: recover orphaned jobs on server startup"
```

---

### Task 7: Full Test Suite Verification

**Files:** None (verification only)

**Step 1: Run all queue tests**

Run: `go test -v ./internal/queue/`
Expected: All PASS

**Step 2: Run the full test suite**

Run: `make test`
Expected: All PASS

**Step 3: Run lint**

Run: `make lint`
Expected: Clean

**Step 4: Run build**

Run: `make build`
Expected: Clean build

---

### Task 8: Final Commit and Cleanup

**Files:** None (git only)

**Step 1: Verify all changes are committed**

Run: `git status`
Expected: Clean working tree

**Step 2: Review the full diff**

Run: `git log --oneline origin/dev-readiness-gap..HEAD`
Expected: All commits from this implementation listed

**Step 3: Push**

Run: `git push`
