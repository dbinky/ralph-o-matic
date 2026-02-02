package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_UnlimitedAlwaysReturns(t *testing.T) {
	rl := NewRateLimiter(0)
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		err := rl.Wait(ctx)
		require.NoError(t, err)
	}
}

func TestRateLimiter_WithinLimit(t *testing.T) {
	rl := NewRateLimiter(10)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		err := rl.Wait(ctx)
		require.NoError(t, err)
	}
}

func TestRateLimiter_ExceedsLimit_Blocks(t *testing.T) {
	rl := NewRateLimiter(2)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// First 2 should succeed immediately
	require.NoError(t, rl.Wait(ctx))
	require.NoError(t, rl.Wait(ctx))

	// 3rd should block and eventually fail with context deadline
	err := rl.Wait(ctx)
	assert.Error(t, err, "should block when limit exceeded")
}

func TestRateLimiter_CancelledContext(t *testing.T) {
	rl := NewRateLimiter(1)
	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, rl.Wait(ctx)) // use up the limit

	cancel() // cancel before waiting

	err := rl.Wait(ctx)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestRateLimiter_HourRollover(t *testing.T) {
	rl := NewRateLimiter(2)
	ctx := context.Background()

	require.NoError(t, rl.Wait(ctx))
	require.NoError(t, rl.Wait(ctx))

	// Simulate hour rollover by resetting
	rl.resetForTesting()

	// Should work again
	require.NoError(t, rl.Wait(ctx))
	require.NoError(t, rl.Wait(ctx))
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(100)
	ctx := context.Background()
	done := make(chan error, 100)

	for i := 0; i < 100; i++ {
		go func() {
			done <- rl.Wait(ctx)
		}()
	}

	for i := 0; i < 100; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}
