package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_StartsInClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	assert.Equal(t, CircuitClosed, cb.State())
}

func TestCircuitBreaker_ProgressKeepsClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	state := cb.RecordIteration(true, "")
	assert.Equal(t, CircuitClosed, state)
}

func TestCircuitBreaker_ProgressResetsCounters(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	// 2 no-progress then progress should reset
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "")
	state := cb.RecordIteration(true, "")
	assert.Equal(t, CircuitClosed, state)

	// After reset, need 3 more no-progress to open
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "")
	assert.Equal(t, CircuitHalfOpen, cb.State())
}

func TestCircuitBreaker_TwoNoProgress_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	cb.RecordIteration(false, "")
	state := cb.RecordIteration(false, "")
	assert.Equal(t, CircuitHalfOpen, state)
}

func TestCircuitBreaker_HalfOpen_ProgressRecovery(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "") // → HalfOpen
	assert.Equal(t, CircuitHalfOpen, cb.State())

	state := cb.RecordIteration(true, "") // recovery
	assert.Equal(t, CircuitClosed, state)
}

func TestCircuitBreaker_ThreeNoProgress_Open(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "")
	state := cb.RecordIteration(false, "")
	assert.Equal(t, CircuitOpen, state)
}

func TestCircuitBreaker_FiveSameError_Open(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	for i := 0; i < 4; i++ {
		cb.RecordIteration(true, "same error") // progress but same error
	}
	assert.NotEqual(t, CircuitOpen, cb.State())

	state := cb.RecordIteration(true, "same error")
	assert.Equal(t, CircuitOpen, state)
}

func TestCircuitBreaker_DifferentErrors_NoOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	for i := 0; i < 10; i++ {
		cb.RecordIteration(true, "error "+string(rune('A'+i)))
	}
	assert.NotEqual(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_ErrorThenSuccessThenError_Resets(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	cb.RecordIteration(true, "error A")
	cb.RecordIteration(true, "error A")
	cb.RecordIteration(true, "error A")
	// Progress with no error resets same-error counter
	cb.RecordIteration(true, "")
	cb.RecordIteration(true, "error A")
	cb.RecordIteration(true, "error A")
	assert.NotEqual(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_OpenIsTerminal(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "") // → Open
	assert.Equal(t, CircuitOpen, cb.State())

	// Further calls don't change state
	state := cb.RecordIteration(true, "")
	assert.Equal(t, CircuitOpen, state)
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(3, 5)
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "")
	cb.RecordIteration(false, "") // → Open

	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.State())

	// Should work normally again
	state := cb.RecordIteration(true, "")
	assert.Equal(t, CircuitClosed, state)
}

func TestCircuitBreaker_CustomThresholds(t *testing.T) {
	// Very low thresholds
	cb := NewCircuitBreaker(1, 2)

	state := cb.RecordIteration(false, "")
	assert.Equal(t, CircuitOpen, state, "threshold 1 should open immediately")
}

func TestCircuitBreaker_ZeroThreshold_NeverOpens(t *testing.T) {
	cb := NewCircuitBreaker(0, 0)
	for i := 0; i < 100; i++ {
		cb.RecordIteration(false, "same error")
	}
	assert.NotEqual(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_EmptyError_NoSameErrorIncrement(t *testing.T) {
	cb := NewCircuitBreaker(3, 2)
	// Empty errors should not increment same-error counter
	cb.RecordIteration(true, "")
	cb.RecordIteration(true, "")
	cb.RecordIteration(true, "")
	assert.NotEqual(t, CircuitOpen, cb.State())
}
