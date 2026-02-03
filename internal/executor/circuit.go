package executor

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitHalfOpen                     // Monitoring — approaching threshold
	CircuitOpen                         // Halted — job should fail
)

// CircuitBreaker tracks consecutive failures per job to detect stuck loops.
// It uses a three-state model: Closed → HalfOpen → Open.
type CircuitBreaker struct {
	state                 CircuitState
	consecutiveNoProgress int
	consecutiveSameError  int
	lastError             string
	noProgressThreshold   int // 0 = disabled
	sameErrorThreshold    int // 0 = disabled
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
// A threshold of 0 disables that check.
func NewCircuitBreaker(noProgressThreshold, sameErrorThreshold int) *CircuitBreaker {
	return &CircuitBreaker{
		state:               CircuitClosed,
		noProgressThreshold: noProgressThreshold,
		sameErrorThreshold:  sameErrorThreshold,
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	return cb.state
}

// RecordIteration updates the circuit breaker after an iteration.
// hasProgress indicates whether meaningful work was detected (git diff or metadata).
// errMsg is the error message from this iteration (empty if no error).
// Returns the new state.
func (cb *CircuitBreaker) RecordIteration(hasProgress bool, errMsg string) CircuitState {
	// Open is terminal
	if cb.state == CircuitOpen {
		return CircuitOpen
	}

	// Early return: HalfOpen with progress transitions immediately to Closed
	// This explicit transition ensures we don't re-enter HalfOpen state
	if cb.state == CircuitHalfOpen && hasProgress {
		cb.consecutiveNoProgress = 0
		if errMsg == "" {
			cb.consecutiveSameError = 0
			cb.lastError = ""
		}
		cb.state = CircuitClosed
		return CircuitClosed
	}

	if hasProgress {
		cb.consecutiveNoProgress = 0
		// Check same-error before resetting
		if errMsg == "" {
			cb.consecutiveSameError = 0
			cb.lastError = ""
		}
	} else {
		cb.consecutiveNoProgress++
	}

	// Track same-error (only for non-empty errors)
	if errMsg != "" {
		if errMsg == cb.lastError {
			cb.consecutiveSameError++
		} else {
			cb.consecutiveSameError = 1
			cb.lastError = errMsg
		}
	}

	// Check same-error threshold → Open
	if cb.sameErrorThreshold > 0 && cb.consecutiveSameError >= cb.sameErrorThreshold {
		cb.state = CircuitOpen
		return CircuitOpen
	}

	// Check no-progress threshold → Open
	if cb.noProgressThreshold > 0 && cb.consecutiveNoProgress >= cb.noProgressThreshold {
		cb.state = CircuitOpen
		return CircuitOpen
	}

	// HalfOpen at threshold - 1 (monitoring)
	if cb.noProgressThreshold > 1 && cb.consecutiveNoProgress >= cb.noProgressThreshold-1 {
		cb.state = CircuitHalfOpen
		return CircuitHalfOpen
	}

	return cb.state
}

// Reset returns the circuit breaker to Closed state with zeroed counters.
func (cb *CircuitBreaker) Reset() {
	cb.state = CircuitClosed
	cb.consecutiveNoProgress = 0
	cb.consecutiveSameError = 0
	cb.lastError = ""
}
