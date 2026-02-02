package worker

import (
	"context"
	"sync"
	"time"
)

// RateLimiter limits calls per hour. A maxPerHour of 0 means unlimited.
type RateLimiter struct {
	maxPerHour int
	count      int
	resetAt    time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a rate limiter. maxPerHour=0 means no limit.
func NewRateLimiter(maxPerHour int) *RateLimiter {
	return &RateLimiter{
		maxPerHour: maxPerHour,
		resetAt:    time.Now().Add(time.Hour),
	}
}

// Wait blocks until a call is allowed or ctx is cancelled.
// Returns nil immediately if rate limiting is disabled (maxPerHour=0).
func (rl *RateLimiter) Wait(ctx context.Context) error {
	if rl.maxPerHour <= 0 {
		return nil
	}

	for {
		rl.mu.Lock()
		now := time.Now()

		// Reset counter if hour has passed
		if now.After(rl.resetAt) {
			rl.count = 0
			rl.resetAt = now.Add(time.Hour)
		}

		if rl.count < rl.maxPerHour {
			rl.count++
			rl.mu.Unlock()
			return nil
		}

		waitUntil := rl.resetAt
		rl.mu.Unlock()

		// Wait for reset or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(waitUntil)):
			// Loop back to check again
		}
	}
}

// resetForTesting resets the counter and window. Test use only.
func (rl *RateLimiter) resetForTesting() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.count = 0
	rl.resetAt = time.Now().Add(time.Hour)
}
