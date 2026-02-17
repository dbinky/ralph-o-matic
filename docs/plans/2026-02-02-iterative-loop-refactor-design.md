# Iterative Loop Refactor: Internalizing Ralph Loop Logic

**Date:** 2026-02-02
**Status:** Draft
**Branch:** TBD (from `dev`)

## Summary

Replace the external dependency on frankbria/ralph-claude-code with native loop
intelligence built into the ralph-o-matic server. The server already runs
`claude --print` in a loop via the worker — this refactor adds the missing
sophistication: completion detection, circuit breaker, session continuity,
retry/backoff, rate limiting, and per-iteration commits.

Additionally, remove the frankbria/ralph-claude-code install step from both
install scripts (bash and PowerShell) since the server will handle all loop
logic internally.

## Design Decisions

1. **JSON output parsing** — Use `claude --print --output-format json` for
   structured responses. More reliable than text parsing or prompt-based signals.
2. **Progress detection** — Git diff + JSON output metadata (belt and suspenders).
3. **Per-backend config** — Anthropic gets rate limits and shorter timeouts.
   Ollama gets unlimited calls and longer timeouts.
4. **Circuit breaker is in-memory per-job** — Not persisted to DB. Resets when
   a job restarts. Lives in `internal/executor/`.
5. **Rate limiter lives in worker** — Checked before each iteration, per-backend.

## Architecture

### Package Layout

```
internal/executor/
  claude.go          — Enhanced: JSON output, --resume, timeout
  response.go        — NEW: Parse Claude JSON output → ResponseMetadata
  circuit.go         — NEW: Per-job circuit breaker state machine
  ralph.go           — Enhanced: per-iteration commits
  *_test.go          — Tests for all of the above

internal/worker/
  worker.go          — Enhanced: completion check, circuit breaker, retry
  ratelimit.go       — NEW: Per-backend rate limiter
  *_test.go          — Tests for all of the above

internal/models/
  config.go          — Enhanced: LoopConfig with per-backend defaults

scripts/
  install.sh         — Remove frankbria/ralph-claude-code install
  install.ps1        — Remove frankbria/ralph-claude-code install
```

### New Types

```go
// internal/executor/response.go

// ResponseMetadata holds parsed data from Claude's JSON output
type ResponseMetadata struct {
    SessionID     string
    FilesModified int
    HasErrors     bool
    IsTestOnly    bool
    ExitSignal    bool
    Completed     bool
    WorkSummary   string
    ErrorMessages []string
}

// ParseResponse parses Claude CLI JSON output into ResponseMetadata.
// Handles both single-object and array formats.
func ParseResponse(jsonOutput []byte) (*ResponseMetadata, error)
```

```go
// internal/executor/circuit.go

type CircuitState int
const (
    CircuitClosed   CircuitState = iota  // Normal operation
    CircuitHalfOpen                       // Monitoring — 2 no-progress loops
    CircuitOpen                           // Halted — manual reset or job fails
)

type CircuitBreaker struct {
    state                    CircuitState
    consecutiveNoProgress    int
    consecutiveSameError     int
    lastError                string
    noProgressThreshold      int  // default 3
    sameErrorThreshold       int  // default 5
}

// RecordIteration updates circuit breaker state after an iteration.
// hasProgress comes from git diff check + ResponseMetadata.
// Returns the new state.
func (cb *CircuitBreaker) RecordIteration(hasProgress bool, errMsg string) CircuitState

// Reset returns the circuit breaker to Closed state.
func (cb *CircuitBreaker) Reset()
```

```go
// internal/worker/ratelimit.go

type RateLimiter struct {
    maxPerHour int        // 0 = unlimited
    count      int
    resetAt    time.Time
    mu         sync.Mutex
}

// NewRateLimiter creates a rate limiter. maxPerHour=0 means no limit.
func NewRateLimiter(maxPerHour int) *RateLimiter

// Wait blocks until a call is allowed or ctx is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error
```

```go
// internal/models/config.go (additions)

type LoopConfig struct {
    MaxCallsPerHour             int `json:"max_calls_per_hour"`    // 0 = unlimited
    TimeoutMinutes              int `json:"timeout_minutes"`
    MaxRetries                  int `json:"max_retries"`           // per-iteration
    PauseBetweenSecs            int `json:"pause_between_secs"`
    CircuitBreakerNoProgress    int `json:"cb_no_progress_threshold"`
    CircuitBreakerSameError     int `json:"cb_same_error_threshold"`
    SessionExpiryHours          int `json:"session_expiry_hours"`
}

func DefaultLoopConfig(backend Backend) LoopConfig
```

### Enhanced ExecutionResult

```go
// internal/executor/claude.go (modified)

type ExecutionResult struct {
    Output     string
    RawJSON    []byte             // raw JSON from claude
    Iterations int
    Completed  bool
    SessionID  string             // extracted from JSON
    Metadata   *ResponseMetadata  // parsed response analysis
    Error      error
}
```

### Iteration Flow (Worker Loop)

```
for each iteration:
    1. RATE LIMIT CHECK
       - Anthropic: check calls/hour, block if exceeded
       - Ollama: skip (unlimited)

    2. BUILD COMMAND
       - --output-format json
       - If sessionId exists and not expired: --resume <sessionId>
       - --dangerously-skip-permissions for Ollama (not Anthropic)
       - Timeout via context.WithTimeout (15min Anthropic, 30min Ollama)

    3. EXECUTE CLAUDE
       - Run subprocess, stream output to DB logs
       - On timeout: → RETRY
       - On other error: → RETRY

    4. PARSE RESPONSE
       - Parse JSON → ResponseMetadata
       - Extract sessionId for next iteration

    5. CHECK COMPLETION
       - If metadata.ExitSignal or metadata.Completed: → FINALIZE (success)

    6. PER-ITERATION COMMIT
       - git add -A && git commit in work dir
       - No-op if nothing changed

    7. CIRCUIT BREAKER
       - Check git diff + metadata for progress
       - Feed to circuit breaker
       - OPEN → FINALIZE (failure: "no progress after N iterations")

    8. MAX ITERATIONS CHECK
       - If reached: → FINALIZE (success, reached max)

    9. INTER-ITERATION PAUSE
       - Anthropic: 5s
       - Ollama: 1s

    RETRY:
       - If retries < max: exponential backoff (5s, 15s, 45s)
       - If retries >= max: fail job
```

### Backend Behavior Matrix

| Behavior                          | Ollama         | Anthropic      |
|-----------------------------------|----------------|----------------|
| Rate limiting                     | None           | 100 calls/hour |
| `--dangerously-skip-permissions`  | Yes (default)  | No (default)   |
| Default timeout                   | 30 min         | 15 min         |
| Inter-iteration pause             | 1s             | 5s             |
| Session continuity (`--resume`)   | Yes            | Yes            |
| Circuit breaker thresholds        | Same           | Same           |
| Retry backoff base                | 5s             | 5s             |
| Max retries per iteration         | 3              | 3              |

### Install Script Changes

**`scripts/install.sh`** — Remove the `install_plugins` function body that
clones and installs frankbria/ralph-claude-code (lines 671-685). Keep the
brainstorm-to-ralph skill installation. Rename function to `install_skill`
or similar to reflect it only installs the skill now.

**`scripts/install.ps1`** — Same change: remove the ralph-claude-code clone
block from `Install-Plugins` (lines 388-400). Keep the brainstorm-to-ralph
skill installation.

## Implementation Steps

Each step is independently testable and shippable. Strict TDD throughout —
write tests first, then implement.

### Step 1: Wire Up Early Termination

**What:** Check `result.Completed` in the worker loop. Currently computed but
never acted on.

**Files:** `internal/worker/worker.go`

**Tests (write first):**
- Happy path: job completes when `result.Completed` is true before max iterations
- Happy path: job runs to max iterations when `result.Completed` is always false
- Edge case: `result.Completed` true on first iteration → finalize immediately
- Edge case: `result.Completed` true on last iteration (same as max)

### Step 2: JSON Output Parsing

**What:** Add `response.go` to parse Claude CLI JSON output. Switch
`ClaudeExecutor` to use `--output-format json`.

**Files:** `internal/executor/response.go`, `internal/executor/claude.go`

**Tests (write first):**
- **ParseResponse — happy path:**
  - Single JSON object with result, sessionId, metadata → correct ResponseMetadata
  - JSON array format (init + assistant + result messages) → correct ResponseMetadata
  - Response with RALPH_STATUS block in result text → parsed fields
- **ParseResponse — completion detection:**
  - ExitSignal=true in output → metadata.ExitSignal=true
  - STATUS: COMPLETE → metadata.Completed=true
  - "all tasks complete" in result text → metadata.Completed=true
  - No completion signals → both false
- **ParseResponse — error detection:**
  - Response with error messages → metadata.HasErrors=true, ErrorMessages populated
  - Response with no errors → metadata.HasErrors=false
- **ParseResponse — edge cases:**
  - Empty JSON → error
  - Malformed JSON → error with descriptive message
  - Valid JSON but missing expected fields → zero-value metadata, no error
  - Extremely large output → handles without OOM (truncate if needed)
  - Output with mixed text + JSON (stderr prefix) → extracts JSON portion
- **ParseResponse — test-only detection:**
  - Response mentioning only test runs → IsTestOnly=true
  - Response with implementation + tests → IsTestOnly=false
- **ClaudeExecutor — integration:**
  - Verify `--output-format json` is passed in args
  - Verify RawJSON is populated on ExecutionResult
  - Verify Metadata is populated from parsed response

### Step 3: Session Continuity

**What:** Store `sessionId` from JSON output, pass `--resume <id>` on
subsequent iterations. Expire sessions after configurable period.

**Files:** `internal/executor/claude.go`, `internal/executor/ralph.go`

**Tests (write first):**
- Happy path: sessionId extracted from first iteration, passed to second
- Happy path: session expires after configured hours → no --resume flag
- Happy path: no sessionId in output → no --resume on next iteration
- Failure: sessionId from previous job doesn't leak to new job
- Edge case: sessionId is empty string → treated as absent
- Edge case: session created at boundary of expiry window

### Step 4: Per-Iteration Commits

**What:** After each successful iteration, commit changes in the work
directory. Prevents losing work on crash.

**Files:** `internal/executor/ralph.go`, `internal/git/repo_manager.go`
(if needed)

**Tests (write first):**
- Happy path: files changed → commit created with iteration message
- Happy path: no files changed → no commit, no error
- Failure: git commit fails (e.g., lock file) → logged as warning, iteration continues
- Edge case: very large number of files → git add -A handles it
- Edge case: .gitignore excludes all changed files → no commit, no error

### Step 5: Circuit Breaker

**What:** Three-state circuit breaker per job. Tracks consecutive no-progress
and same-error iterations. Opens (fails job) when thresholds exceeded.

**Files:** `internal/executor/circuit.go`, `internal/worker/worker.go`

**Tests (write first):**
- **State machine:**
  - Starts in Closed state
  - Progress detected → stays Closed, counters reset
  - 2 consecutive no-progress → transitions to HalfOpen
  - HalfOpen + progress → transitions back to Closed
  - HalfOpen + 1 more no-progress (total 3) → transitions to Open
  - Closed + 3 consecutive no-progress → transitions to Open
  - 5 consecutive same-error → transitions to Open
  - Reset() → returns to Closed with zeroed counters
- **Progress detection:**
  - Git diff shows changes → hasProgress=true
  - No git diff but metadata.FilesModified > 0 → hasProgress=true
  - Neither → hasProgress=false
- **Same-error detection:**
  - Same error message 5 times → sameError counter reaches threshold
  - Different error messages → counter resets
  - Error then success then error → counter resets on success
- **Edge cases:**
  - Open state is terminal (no transitions out except Reset)
  - Custom thresholds respected (not just defaults)
  - Zero threshold → effectively disabled (never opens)
  - Nil error message → doesn't increment same-error counter
- **Worker integration:**
  - Circuit opens → job marked as failed with descriptive reason
  - Circuit stays closed → job continues iterating

### Step 6: Rate Limiting, Retry, and Timeout

**What:** Per-backend rate limiter, exponential backoff retry on transient
errors, per-invocation timeout.

**Files:** `internal/worker/ratelimit.go`, `internal/worker/worker.go`,
`internal/executor/claude.go`, `internal/models/config.go`

**Tests — Rate Limiter (write first):**
- Happy path: calls within limit → Wait returns immediately
- Happy path: unlimited (maxPerHour=0) → Wait always returns immediately
- Blocking: calls exceed limit → Wait blocks until next hour window
- Cancellation: ctx cancelled while waiting → returns ctx.Err()
- Edge case: exactly at limit → next call blocks
- Edge case: hour rolls over → counter resets, call proceeds
- Concurrency: multiple goroutines calling Wait → no races (use -race)

**Tests — Retry with Backoff (write first):**
- Happy path: first attempt succeeds → no retry
- Happy path: first attempt fails, second succeeds → 1 retry, ~5s delay
- Happy path: all attempts fail → job fails after max retries
- Failure: context cancelled during backoff → returns immediately
- Edge case: backoff durations are correct (5s, 15s, 45s for base=5s, factor=3)
- Edge case: max retries = 0 → no retries, fail immediately on error

**Tests — Timeout (write first):**
- Happy path: execution completes within timeout → normal result
- Failure: execution exceeds timeout → context.DeadlineExceeded error
- Edge case: timeout = 0 → no timeout applied (or very large default)
- Integration: timeout error treated as transient → triggers retry

**Tests — LoopConfig (write first):**
- DefaultLoopConfig(BackendAnthropic) → rate limit 100, timeout 15, pause 5
- DefaultLoopConfig(BackendOllama) → rate limit 0, timeout 30, pause 1
- DefaultLoopConfig("") → same as Ollama (backward compatible)
- Custom config overrides defaults

### Step 7: Remove Frankbria Install from Scripts

**What:** Remove the ralph-claude-code clone+install from both install scripts.
Keep brainstorm-to-ralph skill installation.

**Files:** `scripts/install.sh`, `scripts/install.ps1`

**Tests:**
- BATS tests: `install_plugins` no longer references frankbria/ralph-claude-code
- BATS tests: brainstorm-to-ralph skill installation still works
- PowerShell: manual verification (or Pester tests if available)

**Bash changes (install.sh):**
- Remove lines 671-685 (git clone frankbria/ralph-claude-code + install.sh)
- Rename `install_plugins` → `install_skill` (singular, accurate)
- Update `main()` to call `install_skill`

**PowerShell changes (install.ps1):**
- Remove lines 388-400 (git clone frankbria/ralph-claude-code + bash install.sh)
- Rename `Install-Plugins` → `Install-Skill`
- Update `Main` to call `Install-Skill`

## Testing Strategy

**Every step uses strict TDD:**
1. Write failing test(s) covering happy path, failure, and edge cases
2. Implement minimum code to pass
3. Refactor if needed
4. Verify all existing tests still pass (`make test`)

**Test fixtures:** Create `internal/executor/testdata/` with sample Claude JSON
outputs (success, error, array format, completion signals, etc.)

**Race detection:** All tests run with `-race` flag (already in Makefile).

**Integration tests:** Steps 2-6 involve subprocess execution. Unit tests should
mock the subprocess. Integration tests (gated behind `-tags=integration`) can
run against a real Claude CLI if available.

## Non-Goals

- Tmux/monitor UI (already have web dashboard)
- Tool permission allowlists (separate concern, future work)
- Task import from beads/GitHub/PRDs (orthogonal)
- Live streaming display (already stream to DB)
- Persisting circuit breaker state to DB (YAGNI — in-memory per-job is sufficient)
