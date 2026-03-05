# Gaps Identified

Tracking spec-vs-implementation divergences and quality issues discovered during review.
Items are removed from this list when fixed.

## Open Issues

## Previously Fixed

- [x] **FIXED** Installer model config never applied to server — `configure_ralph()` wrote server config fields to YAML, but server reads config from database only. Added `apply_model_config()` to push hardware-detected model selections to server API after startup (same pattern as `apply_notification_config()`). Also fixed server/full mode YAML to write CLI-compatible fields.

- [x] **FIXED** Worker marked max-iterations-reached jobs as "completed" instead of "failed" — spec says "Max iterations reached → Mark failed, create PR with failure notes." The `poll()` method in `internal/worker/worker.go` unconditionally set `success := true` after the iteration loop, regardless of whether the loop exited via Claude's completion signal or by hitting the max iterations cap. This meant jobs timing out appeared as successes in the dashboard, PR titles showed "✓" instead of "✗ FAILED", and notifications reported completion instead of failure. Fixed by tracking `completedBySignal` and routing to `queue.Fail()` with `EventFailed` notification when max iterations reached. Also fixed test `TestWorker_RunsToMaxIterations_WhenNeverCompleted` and `TestWorker_CircuitBreaker_ProgressPreventsOpen` which codified the bug.

- [x] **FIXED** Worker ignored pause/cancel during execution; Resume left jobs stuck — The worker's `poll()` loop operated on a stale in-memory job copy without re-reading DB state between iterations. When a user paused or cancelled a running job via the API, the worker kept iterating and eventually overwrote the paused/cancelled state with completed/failed. Additionally, `Resume()` transitioned paused→running, but `Dequeue()` only pulls queued jobs, so resumed jobs became permanently stuck in "running" with no worker processing them. Fixed by: (1) adding `checkExternalStop()` to the worker loop that re-reads job status from DB after each iteration and stops if paused/cancelled, (2) changing `Resume()` to transition paused→queued so the worker naturally picks up resumed jobs via `Dequeue()`, (3) adding `StatusQueued` as a valid transition target from `StatusPaused` in the state machine. Cancel sends `EventCancelled` notification; pause does not (user-initiated). Five new tests cover the behavior.

- [x] **FIXED** List jobs API lacked input validation — `handleListJobs` accepted arbitrary strings in the `status` query parameter without validating against known `JobStatus` values (garbage like `?status=pending` silently passed to SQL). The `limit` and `offset` parameters swallowed parse errors (`?limit=abc` silently became 0, meaning "no limit" — returning all jobs unbounded). No default pagination limit was applied, so as jobs accumulated the endpoint would return increasingly large responses. `handleReorderJobs` was also missing `MaxBytesReader` unlike the other POST/PATCH handlers. Fixed by: (1) validating each status value against `JobStatus.Valid()` with 400 error, (2) returning 400 for non-numeric or negative limit/offset, (3) applying a default limit of 100, (4) adding `MaxBytesReader` to `handleReorderJobs`. Three new test functions cover the validation.

## Won't Fix

These items may be divergences from the original spec, but have changed in subsequent spec revisions within the `docs/plans` directory.  Only the user may move items into the "Won't Fix" category - do not do it yourself.  Once an item is in this section, you can safely ignore it.

- [ ] Spec says server binds "LAN IP only (not 0.0.0.0)"; server actually binds to `:9090` (all interfaces) by default

- [ ] Spec says `ConcurrentJobs` is a server config field (default: 1); implementation has no such field in `ServerConfig` (it was intentionally removed per design doc `2026-02-04-remove-concurrent-jobs-design.md`, so this is expected)