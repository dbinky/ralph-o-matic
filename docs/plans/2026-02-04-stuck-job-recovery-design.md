# Stuck Job Recovery After Server Restart

## Problem

When the server crashes or restarts while a job is in `running` status, that job is orphaned forever. The worker's `Dequeue()` only picks up `StatusQueued` jobs, so orphaned running jobs are invisible. No startup recovery exists. In an org trial with multiple users, server restarts will happen.

## Decision Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| What happens to orphaned jobs | Re-queue with log entry | Preserves progress, avoids manual resubmission burden |
| Runtime detection (heartbeat) | Not now, startup only | Worker is single goroutine; if stuck, system is visibly broken. Heartbeat adds column, goroutine, tuning. Add later if needed. |
| Iteration count | Preserved | Executor already handles re-entry. Reset would misrepresent progress and break circuit breaker/max-iteration logic. |
| Architecture | Method on Queue | Queue owns all status transitions. Keeps responsibility consolidated and testable. |
| State transition | Direct field set, bypass `TransitionTo()` | `Running -> Queued` is not a normal transition. Adding it to the state machine would mask bugs. |
| Log append failure | Best-effort | Recovery itself must not fail because a log entry couldn't be written. Log via slog and continue. |

## Design

### Recovery Method

New method on `queue.Queue`:

```go
func (q *Queue) RecoverOrphaned() (int, error)
```

Behavior:
1. Query DB for all jobs with `status = 'running'`
2. For each orphaned job:
   - Set `job.Status = models.StatusQueued` directly (bypass state machine)
   - Clear `StartedAt` (fresh start timer on next dequeue)
   - Preserve iteration count, position, priority, branch, all other fields
   - Append log entry via `LogRepo.Append()` (best-effort)
3. Return count of recovered jobs

Log entry format:
```
[RECOVERY] Server restarted while job was running (iteration 3/10). Job re-queued and will resume automatically.
```

### Startup Integration

In `cmd/server/main.go`, after queue creation, before worker start:

```go
q := queue.New(database)
recovered, err := q.RecoverOrphaned()
if err != nil {
    slog.Error("failed to recover orphaned jobs", "error", err)
    os.Exit(1)
}
if recovered > 0 {
    slog.Info("recovered orphaned jobs", "count", recovered)
}
```

DB recovery errors are fatal. If we can't fix orphaned jobs, the database is likely broken.

### Files Changed

- `internal/queue/queue.go` — Add `RecoverOrphaned()` method
- `internal/queue/recovery_test.go` — Comprehensive tests
- `cmd/server/main.go` — Call `RecoverOrphaned()` at startup

### No Migration Needed

No new columns, tables, or schema changes. Uses existing `status` field and `LogRepo.Append()`.

## Test Plan (Strict TDD)

All tests in `internal/queue/recovery_test.go` using `newTestDB(t)`.

### Happy path
- Running job gets re-queued: status becomes `queued`, log entry appended, iteration count preserved
- Multiple running jobs (3) all recovered in a single call

### Success scenarios
- `StartedAt` cleared for fresh start timer on next dequeue
- Log entry contains iteration count and recovery message
- Position, priority, branch, and all other fields untouched
- Return value matches count of recovered jobs
- Recovered job is picked up by `Dequeue()` (round-trip test)

### Failure/error scenarios
- DB error on initial query for running jobs: returns error, no jobs modified
- DB error on job update mid-recovery: returns error with partial context
- Log append failure: best-effort, logged via slog, recovery continues

### Edge cases
- No orphaned jobs: returns `(0, nil)`, no DB writes, no log entries
- Jobs in every other status (queued, paused, completed, failed, cancelled): none touched
- Paused jobs specifically NOT recovered (deliberate user action)
- Job with zero iterations (crashed immediately after dequeue)
- Job at max iterations: re-queued, worker handles max-iteration check on next pickup
- Called twice in a row: second call returns 0 (idempotent)
