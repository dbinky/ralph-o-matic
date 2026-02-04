# Notification System Design

## Overview

Add notification capabilities to the ralph-o-matic server so users are alerted when jobs reach terminal states (completed, failed, cancelled). Supports two channels: email (SMTP) and Microsoft Teams (Incoming Webhook). Configured globally by admins via `ServerConfig`.

## Constraints & Decisions

- **Global config, not per-user:** Admin configures SMTP server and Teams webhook once. All notifications go to the same recipients/channel.
- **Fire-and-forget:** Notification failure never blocks or fails a job. Errors are logged.
- **No new dependencies:** Uses Go stdlib (`net/smtp`, `net/http`) only.
- **No schema migration needed:** Existing `config` key-value table stores new keys naturally.
- **Strict TDD:** All code written test-first with comprehensive scenario coverage.

## Architecture

### Notifier Interface

```
internal/notify/
  notify.go          -- Notifier interface, Dispatcher, Event types
  smtp.go            -- SMTPNotifier
  smtp_test.go       -- SMTP tests
  teams.go           -- TeamsNotifier
  teams_test.go      -- Teams tests
  dispatcher_test.go -- Dispatcher tests
```

```go
type Event string

const (
    EventCompleted Event = "completed"
    EventFailed    Event = "failed"
    EventCancelled Event = "cancelled"
)

type Notifier interface {
    Notify(ctx context.Context, job *models.Job, event Event) error
    Name() string
}

type Dispatcher struct {
    notifiers []Notifier
    logger    *slog.Logger
}
```

The `Dispatcher` iterates all registered notifiers, calling each one. If a notifier fails, it logs the error and continues to the next. If a notifier panics, it recovers and continues.

### SMTP Notifier

**Config fields:**

| Key | Type | Example |
|-----|------|---------|
| `notify.smtp.enabled` | bool | `true` |
| `notify.smtp.host` | string | `smtp.office365.com` |
| `notify.smtp.port` | int | `587` |
| `notify.smtp.username` | string | `ralph@company.com` |
| `notify.smtp.password` | string | `app-password` |
| `notify.smtp.from` | string | `ralph@company.com` |
| `notify.smtp.recipients` | string (comma-separated) | `team@company.com,lead@company.com` |

**Behavior:**

- Sends plain-text email with job summary
- Subject: `[ralph-o-matic] Job #42 completed - repo/branch` or `Job #42 failed - repo/branch`
- Body includes: job ID, repo URL, branch, owner name, iteration count, duration, PR URL (if completed), error message (if failed)
- Uses STARTTLS when available
- 10-second connection timeout
- All configured recipients receive all notifications

### Teams Webhook Notifier

**Config fields:**

| Key | Type | Example |
|-----|------|---------|
| `notify.teams.enabled` | bool | `true` |
| `notify.teams.webhook_url` | string | `https://outlook.office.com/webhook/...` |

**Behavior:**

- HTTP POST with Adaptive Card JSON payload
- Card color: green (completed), red (failed), yellow (cancelled)
- Card title: `Job #42 Completed` / `Job #42 Failed` / `Job #42 Cancelled`
- Card body: repository, branch, owner, iterations, duration
- Action button: "View PR" linking to PR URL (only on completed with PRURL)
- Truncates error messages over 500 chars (Teams card size limits)
- 10-second HTTP timeout

### Integration Point

Notifications fire in `internal/worker/worker.go` after the job's status has been updated in the database:

```go
// After queue.Complete(job):
w.dispatcher.Notify(ctx, job, notify.EventCompleted)

// After queue.Fail(job, msg):
w.dispatcher.Notify(ctx, job, notify.EventFailed)

// After queue.Cancel(job):
w.dispatcher.Notify(ctx, job, notify.EventCancelled)
```

The dispatcher reads config from the DB on each `Notify()` call. This is simple and always uses current config. Notifications are infrequent (one per terminal state transition), so the overhead is negligible.

### Server Startup Wiring

In `cmd/server/main.go`:

1. Load `ServerConfig` from DB
2. Create `Dispatcher` with `configRepo` reference (reads config per-call)
3. Pass `Dispatcher` to `Worker`

### CLI Commands

Config commands for notification settings:

```
ralph config set notify.smtp.enabled true
ralph config set notify.smtp.host smtp.office365.com
ralph config set notify.smtp.port 587
ralph config set notify.smtp.username ralph@company.com
ralph config set notify.smtp.password app-password
ralph config set notify.smtp.from ralph@company.com
ralph config set notify.smtp.recipients "team@company.com,lead@company.com"

ralph config set notify.teams.enabled true
ralph config set notify.teams.webhook_url "https://outlook.office.com/webhook/..."
```

Test commands to verify configuration:

```
ralph config test-notify smtp
ralph config test-notify teams
```

These send a test notification using current config and print success/failure.

## Config Model Changes

Add to `internal/models/config.go`:

```go
type NotifyConfig struct {
    SMTP  SMTPConfig
    Teams TeamsConfig
}

type SMTPConfig struct {
    Enabled    bool
    Host       string
    Port       int
    Username   string
    Password   string
    From       string
    Recipients []string
}

type TeamsConfig struct {
    Enabled    bool
    WebhookURL string
}
```

Add `Notify NotifyConfig` field to `ServerConfig`.

Add corresponding `applyConfigValue` cases in `internal/db/config.go` for all `notify.*` keys.

## Files Changed/Created

**New files:**
- `internal/notify/notify.go` -- interface, dispatcher, event types
- `internal/notify/smtp.go` -- SMTP notifier implementation
- `internal/notify/teams.go` -- Teams webhook notifier implementation
- `internal/notify/smtp_test.go` -- SMTP tests
- `internal/notify/teams_test.go` -- Teams tests
- `internal/notify/dispatcher_test.go` -- dispatcher tests

**Modified files:**
- `internal/models/config.go` -- add NotifyConfig, SMTPConfig, TeamsConfig
- `internal/db/config.go` -- add applyConfigValue cases for notify.* keys
- `internal/worker/worker.go` -- add dispatcher field, call Notify() on terminal states
- `internal/worker/worker_test.go` -- notification integration tests
- `cmd/server/main.go` -- wire up dispatcher
- `cmd/cli/commands.go` -- add test-notify command

## Test Plan (Strict TDD)

All implementations written test-first. Tests organized by scenario category.

### SMTP Notifier Tests (`smtp_test.go`)

**Happy path:**
- Completed job sends email with correct subject, body, recipients, from address
- Failed job sends email with error message in body and "failed" in subject
- Cancelled job sends email with "cancelled" in subject

**Success scenarios:**
- Email includes PR URL when job has one
- Email includes job owner name when present
- Email includes iteration count and duration
- Multiple recipients all receive the email

**Failure scenarios:**
- SMTP server unreachable -- returns error, no panic
- SMTP auth failure (wrong credentials) -- returns error with context
- SMTP server rejects recipient -- returns error
- SMTP connection timeout (exceeds 10s) -- returns timeout error

**Edge cases:**
- Job with empty OwnerName -- email sends, gracefully omits owner
- Job with empty PRURL (failed before PR creation) -- body omits PR section
- Job with very long error message -- not truncated (email has no length limit)
- Empty recipient list in config -- returns error at send time
- Special characters in job prompt/repo URL -- properly escaped in email body

### Teams Webhook Notifier Tests (`teams_test.go`)

**Happy path:**
- Completed job sends green Adaptive Card with PR link action button
- Failed job sends red card with error details
- Cancelled job sends yellow card

**Success scenarios:**
- Card includes owner name, repo, branch, iteration count, duration
- Card includes "View PR" action button when PRURL is set
- HTTP 200 response treated as success

**Failure scenarios:**
- Webhook URL unreachable -- returns error
- Webhook returns non-2xx status (400, 403, 429, 500) -- returns error with status code
- HTTP connection timeout -- returns timeout error
- Malformed webhook URL -- returns error

**Edge cases:**
- Job with no OwnerName -- card renders without owner field
- Job with no PRURL -- card omits action button
- Very long error message -- truncated to reasonable card length
- Webhook URL with trailing slash vs without -- both work
- Response body on error -- included in error message for debugging

### Dispatcher Tests (`dispatcher_test.go`)

**Happy path:**
- Single notifier called with correct job and event
- Multiple notifiers all called with correct arguments

**Failure scenarios:**
- First notifier fails, second still called (fan-out continues)
- All notifiers fail -- all errors logged, no panic
- Notifier panics -- recovered, other notifiers still fire

**Edge cases:**
- Zero notifiers configured -- Notify() is a no-op, no error
- Nil job passed -- handled gracefully
- Context cancelled mid-notification -- notifiers receive cancelled context

### Worker Integration Tests (`worker_test.go`)

**Happy path:**
- Job completes successfully -- dispatcher called with EventCompleted
- Job fails -- dispatcher called with EventFailed
- Job cancelled -- dispatcher called with EventCancelled

**Failure scenarios:**
- Dispatcher.Notify returns error -- job status still correct in DB
- Dispatcher panics -- worker recovers, job status still correct

**Edge cases:**
- Dispatcher is nil (not configured) -- no panic
- Rapid job completion -- no race conditions
- Job transitions paused->running->completed -- only one notification on terminal state

### Test-Notify CLI Tests

**Happy path:**
- `test-notify smtp` sends test email, prints success
- `test-notify teams` sends test Teams message, prints success

**Failure scenarios:**
- SMTP not configured -- prints clear error
- Teams not configured -- prints clear error
- Notification fails -- prints notifier error
