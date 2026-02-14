# Workspace and Job Retention Cleanup Design

## Context

Every job clones a repo into the workspace directory (`workspaces/job-{id}/`) and it's never cleaned up. The `job_retention_days` config exists (default 30) but no cleanup routine uses it. Disk fills up over time.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Cleanup location | Separate goroutine in `internal/worker` | Different concern from job processing, own schedule, but same package since it's another background server task |
| Cleanup interval | Fixed at 1 hour | Simple, no config bloat, can add configurability later if needed |
| Workspace cleanup timing | Immediately on terminal state | Workspace only useful during execution; results are pushed to branch/PR |
| DB retention timing | After `job_retention_days` | Job records are cheap, useful for history |
| Git safety check | `git status` + `git log @{u}..` | Verify no uncommitted/unpushed work before deleting completed job workspaces |
| Safety check failure | Log warning, skip workspace | Conservative; operator investigates rather than automated recovery |
| Workspace discovery | DB-driven (query terminal jobs) | Simpler than filesystem scanning; avoids directory name parsing |

## Architecture

### Cleaner Struct

New `Cleaner` struct in `internal/worker/cleaner.go`:

```go
type GitChecker interface {
    HasUncommittedChanges(workDir string) (bool, error)
    HasUnpushedCommits(workDir string) (bool, error)
}

type Cleaner struct {
    jobRepo    *db.JobRepo
    configRepo *db.ConfigRepo
    repoMgr    *git.RepoManager
    gitChecker GitChecker
    interval   time.Duration
}
```

Started from `cmd/server/main.go` alongside the worker, shares the same `context.Context`.

### Workspace Cleanup Flow

Runs every tick. For each job in terminal state (`completed`, `failed`, `cancelled`):

1. Check if workspace directory exists — skip if not
2. If job is `completed`:
   a. Run `git status --porcelain` in the workspace
   b. Run `git log @{u}.. --oneline` to check for unpushed commits
   c. If either returns output — log warning, skip this workspace
   d. If git commands error (e.g., corrupted repo) — log error, skip this workspace
3. Call `RepoManager.Cleanup(jobID)` — removes the directory
4. Log that workspace was cleaned up

For `failed`/`cancelled` jobs, skip the git check — partial work isn't expected to be preserved.

### Job Retention Flow

Runs every tick, after workspace cleanup. Only if `job_retention_days > 0`:

1. Calculate cutoff = `now - job_retention_days`
2. Query `JobRepo.ListExpired(cutoff)` — returns terminal jobs older than cutoff
3. For each expired job:
   a. Defensive workspace check — delete if still present
   b. Delete job record (logs cascade-delete via foreign key)
   c. Log the purge

### New DB Method

```go
// ListExpired returns jobs in terminal states (completed/failed/cancelled)
// with completed_at (or updated_at as fallback) before the cutoff time.
func (r *JobRepo) ListExpired(cutoff time.Time) ([]*models.Job, error)
```

## Testing Plan

### Cleaner Lifecycle

- Starts and runs on configured interval
- Stops cleanly on context cancellation
- Skips tick if previous cleanup still running (no double-run)

### Workspace Cleanup — Happy Path

- Deletes workspace for `completed` job with clean git status
- Deletes workspace for `failed` job unconditionally
- Deletes workspace for `cancelled` job unconditionally

### Workspace Cleanup — Skip Scenarios

- Skips `queued` jobs entirely
- Skips `running` jobs entirely
- Skips when workspace directory doesn't exist (already cleaned)
- Skips `completed` job with uncommitted changes (logs warning)
- Skips `completed` job with unpushed commits (logs warning)

### Workspace Cleanup — Error Scenarios

- Git status check returns error (corrupted repo) — logs error, skips workspace
- `os.RemoveAll` fails (permissions) — logs error, continues to next job
- DB query for terminal jobs fails — logs error, skips entire workspace cleanup cycle

### Job Retention — Happy Path

- Purges completed job older than retention period
- Purges failed job older than retention period
- Purges cancelled job older than retention period
- Cascade-deletes associated logs

### Job Retention — Skip/Boundary Scenarios

- Does not purge when `job_retention_days` is 0 (keep forever)
- Does not purge job exactly at the cutoff boundary
- Does not purge `queued` or `running` jobs regardless of age
- Does not purge terminal job younger than retention period

### Job Retention — Error Scenarios

- `ListExpired` query fails — logs error, skips retention cycle
- Individual job delete fails — logs error, continues to next job
- Defensive workspace delete during retention (workspace still exists) — deletes it

### Job Retention — Edge Cases

- Job has no `completed_at` timestamp (old data) — uses `created_at` as fallback
- Retention runs with empty database — no-op, no errors
- Large batch of expired jobs — processes all without OOM or timeout

### Integration Between Phases

- Workspace cleanup runs before retention on same tick
- Retention still checks for workspace if cleanup skipped it earlier
- Cleanup + retention together: workspace deleted first, then DB record purged on same tick

## Implementation Steps

### Step 1: Add `ListExpired` to JobRepo

Add method to `internal/db/jobs.go` that queries for terminal jobs older than the cutoff. Uses `completed_at` with `updated_at` fallback. Tests in `internal/db/jobs_test.go`.

### Step 2: Add `GitChecker` interface and production implementation

Add `GitChecker` interface and `RealGitChecker` to `internal/worker/cleaner.go`. Shells out to `git status --porcelain` and `git log @{u}.. --oneline`.

### Step 3: Implement `Cleaner` struct

Add `Cleaner` to `internal/worker/cleaner.go` with:
- `New` constructor
- `Run(ctx)` method with hourly ticker
- `cleanup(ctx)` method orchestrating both phases
- `cleanWorkspaces(ctx)` for workspace cleanup
- `purgeExpiredJobs(ctx)` for retention cleanup

### Step 4: Comprehensive tests

All tests from the test plan above in `internal/worker/cleaner_test.go` and `internal/db/jobs_test.go`. Strict TDD — tests written before implementation.

### Step 5: Wire into server startup

In `cmd/server/main.go`, create and start the `Cleaner` alongside the worker. Pass same context for graceful shutdown.

### Step 6: Update dashboard/API (if needed)

If the dashboard or API shows workspace paths or assumes jobs exist forever, update accordingly. Likely minimal — the dashboard already handles missing jobs gracefully.
