# Rate Limiter Wiring Design

## Context

The `RateLimiter` type exists in `internal/worker/ratelimit.go` with full test coverage, and `LoopConfig.MaxCallsPerHour` is configured per backend (100 for Anthropic, 0/unlimited for Ollama). But the rate limiter is never instantiated or called in the worker loop. The Anthropic `MaxCallsPerHour` setting has no effect.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Rate limiter scope | Per-worker | API rate limits are account-level; total calls should be capped across jobs |
| Per-job recreation | Yes, based on backend | Ollama gets unlimited, Anthropic gets 100/hr; jobs are sequential so cheap to recreate |
| Blocking behavior | Block with log message | Prevents operator confusion when job stalls for up to an hour |
| Placement | Before `executeWithRetry` | Rate limit the API call, not the overhead around it |

## Changes

### Worker struct

Add `rateLimiter *RateLimiter` field. Initialize to unlimited (`NewRateLimiter(0)`) in `New()`.

### poll() method

1. After resolving `loopConfig` (where circuit breaker is already created), create a new `RateLimiter`:
   ```go
   w.rateLimiter = NewRateLimiter(loopConfig.MaxCallsPerHour)
   ```

2. Before `executeWithRetry`, add rate limit check:
   ```go
   if err := w.waitForRateLimit(ctx, job); err != nil {
       return
   }
   ```

3. New helper method `waitForRateLimit`:
   ```go
   func (w *Worker) waitForRateLimit(ctx context.Context, job *models.Job) error {
       if w.rateLimiter.maxPerHour > 0 {
           log.Printf("Worker: job #%d checking rate limit (%d calls/hour)", job.ID, w.rateLimiter.maxPerHour)
       }
       return w.rateLimiter.Wait(ctx)
   }
   ```

## Testing

- Anthropic job creates rate limiter with MaxCallsPerHour from config
- Ollama job creates unlimited rate limiter (passes through)
- Context cancellation during rate limit wait stops the job
- Rate limiter resets when a new job with different backend starts
