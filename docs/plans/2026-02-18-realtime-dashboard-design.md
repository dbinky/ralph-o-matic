# Real-Time Dashboard with Live Terminal Output

**Date:** 2026-02-18
**Status:** Approved
**Approach:** Enhanced SSE + Vanilla JS (no gRPC, no frameworks)

## Overview

Upgrade the dashboard to show real-time job status changes, progress indicators, and a live terminal window for the running job. Builds on the existing SSE broadcaster infrastructure with no new dependencies.

## Requirements

- Live card updates: job cards move between status sections without full page reload
- Progress indicators: iteration count and elapsed time update live on the running job
- Expandable panel on the running job card showing job details and a ~30-line terminal window
- Terminal shows raw text output from Claude's `--print` subprocess
- All jobs visible to all users (small team tool, transparency over isolation)
- 300-line ring buffer in the browser to cap memory usage

## SSE Event Protocol

Three event types on the global stream (`GET /api/events`):

| Event Type | Payload | Emitted When |
|---|---|---|
| `job_status` | `{type, jobID, status, repo, branch, user, priority, iteration, createdAt}` | Job transitions state |
| `job_progress` | `{type, jobID, iteration, elapsedSec}` | Every ~5s while a job is running |
| `job_log` | `{type, jobID, iteration, message}` | Each line of Claude output |

Emit points:
- `job_status`: from `queue.UpdateStatus()`
- `job_progress`: new ticker in worker (5s interval)
- `job_log`: from `LogRepo.Append()` to both `job:{id}` and `global` topics

## Dashboard UI

### Live Card Management

On initial page load, server renders as today. JavaScript takes over for live updates:

- `job_status` events: move/create/remove cards in the correct status section
- `job_progress` events: update iteration badge and elapsed time on running card
- Empty-state messages shown when sections are empty

### Expandable Panel (Running Job Only)

```
┌─────────────────────────────────────────────────┐
│  ▶ Job: fix-auth-bug          Running  iter 3   │  ← card
├─────────────────────────────────────────────────┤
│  Repo: ryan/myapp        Branch: dev-auth       │
│  User: ryan              Priority: normal       │
│  Started: 2 min ago      Iteration: 3           │
├─────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────┐│
│  │ Reading file internal/auth/handler.go...    ││
│  │ The authentication middleware needs to...   ││
│  │ Editing internal/auth/handler.go...         ││
│  │ Writing test for new auth flow...           ││
│  │                                    ▼ auto   ││
│  └─────────────────────────────────────────────┘│
└─────────────────────────────────────────────────┘
```

- ~30 lines visible, monospace font, dark background
- Auto-scrolls to bottom on new lines
- On first expand: backfill via `GET /api/jobs/{id}/logs?limit=300`
- Subsequent expand/collapse uses in-memory buffer
- 300-line ring buffer — prune oldest lines beyond limit
- Collapsed state: buffer log events but don't render

## Backend Changes

| Package | Change |
|---|---|
| `internal/db/logs.go` | Publish `job_log` to `global` topic in addition to `job:{id}` |
| `internal/queue` | Publish `job_status` event on status transitions in `UpdateStatus()` |
| `internal/worker` | New `ProgressReporter` — goroutine with 5s ticker emitting `job_progress` |
| `internal/api/server.go` | Remove admin guard from `GET /api/events`; add `GET /api/dashboard-state` |
| `internal/broadcast` | No changes needed |
| `internal/executor` | No changes needed |

### New Endpoint

`GET /api/dashboard-state` — returns JSON with current job statuses for SSE reconnect reconciliation:
```json
{"jobs": [{"id": "...", "status": "...", "repo": "...", "branch": "...", "user": "...", "priority": "...", "iteration": 0, "createdAt": "..."}]}
```

## Frontend JavaScript

Single `EventSource('/api/events')` connection. Event dispatcher routes by `data.type`:

- `handleStatusChange(data)`: find/create card by `data-job-id`, move to correct section, attach/remove expandable panel
- `handleProgress(data)`: update iteration and elapsed time on running card
- `handleLog(data)`: append to 300-line ring buffer, render if panel expanded

All JS inline in `dashboard.html` — no build step, no external dependencies.

## Error Handling

- **SSE reconnect**: `EventSource` auto-reconnects; on reconnect, fetch `/api/dashboard-state` to reconcile DOM
- **Race conditions**: buffer orphaned `job_log` events by jobID, attach when card appears; status moves are idempotent
- **Multiple running jobs**: each gets a card, only first gets expandable terminal (revisit if concurrent execution added)
- **No running jobs**: empty state in Running section, no log/progress events fire
- **Long-running jobs**: 300-line ring buffer caps memory regardless of duration

## Testing Strategy

Strict TDD: every change is test-first. Tests cover happy path, success, failure, error, and edge case scenarios.

### Backend: Queue Status Event Publishing (`internal/queue`)

| Scenario | Type | Description |
|---|---|---|
| Happy: each transition publishes | Happy path | Enqueue, Dequeue, Pause, Resume, Complete, Fail, Cancel each publish a `job_status` event to `global` topic |
| Payload contains full metadata | Success | Published JSON includes jobID, status, repo, branch, user, priority, iteration, createdAt |
| No broadcaster configured | Failure | All queue operations succeed without panic when broadcaster is nil |
| RecoverOrphaned publishes | Edge case | Bulk recovery of orphaned jobs emits status events |

### Backend: Dual-Topic Log Publishing (`internal/db/logs.go`)

| Scenario | Type | Description |
|---|---|---|
| Append publishes to both topics | Happy path | `Append()` publishes `job_log` to both `job:{id}` and `global` |
| Global payload includes jobID | Success | Global topic event includes the jobID field (job-specific topic omits it since it's implicit) |
| No broadcaster | Failure | Append still writes to DB when broadcaster is nil |
| DB write fails | Error | Broadcast is skipped when DB insert fails (no partial publish) |

### Backend: ProgressReporter (`internal/worker`)

| Scenario | Type | Description |
|---|---|---|
| Emits progress on tick | Happy path | Running job emits `job_progress` with jobID, iteration, elapsedSec |
| Stops on context cancel | Success | Reporter stops emitting when context is cancelled |
| No running job | Failure | Reporter is a no-op when no job is active |
| Overlapping ticks skipped | Edge case | TryLock prevents concurrent ticks from double-publishing |
| Elapsed time accuracy | Edge case | `elapsedSec` reflects wall clock since job started |

### Backend: API Route Changes (`internal/api`)

| Scenario | Type | Description |
|---|---|---|
| Global SSE accessible to all | Happy path | Non-admin users can subscribe to `/api/events` (admin guard removed) |
| Dashboard state returns jobs | Success | `GET /api/dashboard-state` returns JSON with all active jobs and metadata |
| Dashboard state empty queue | Failure | Returns `{"jobs":[]}` when no jobs exist |
| No ownership filtering | Edge case | Authenticated non-admin sees all jobs in dashboard-state |
| SSE admin tests updated | Edge case | Existing tests that assert admin-only behavior are updated to reflect open access |

### Frontend: SSE Event Handling (manual + integration tests)

| Scenario | Type | Description |
|---|---|---|
| Status event moves card | Happy path | `job_status` event causes card to move to correct section |
| Progress event updates display | Happy path | `job_progress` updates iteration badge and elapsed time |
| Log event appends to terminal | Happy path | `job_log` appends line to terminal div |
| Ring buffer caps at 300 | Success | 301st line causes oldest line to be pruned |
| Expand triggers backfill | Success | First expand fetches `/api/jobs/{id}/logs?limit=300` |
| SSE reconnect reconciles | Error | After disconnect/reconnect, dashboard fetches `/api/dashboard-state` and reconciles DOM |
| Orphaned log buffering | Edge case | `job_log` events for unknown jobID are buffered, attached when `job_status` creates the card |
| Collapsed panel buffers only | Edge case | Log events while collapsed are buffered but not rendered; expand renders them instantly |
| No running jobs shows empty state | Edge case | Running section displays empty state message when last running job completes |
