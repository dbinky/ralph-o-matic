package models

// LoopConfig holds per-backend loop execution settings.
type LoopConfig struct {
	MaxCallsPerHour         int `json:"max_calls_per_hour"`
	TimeoutMinutes          int `json:"timeout_minutes"`
	MaxRetries              int `json:"max_retries"`
	PauseBetweenSecs        int `json:"pause_between_secs"`
	CircuitBreakerNoProgress int `json:"cb_no_progress_threshold"`
	CircuitBreakerSameError  int `json:"cb_same_error_threshold"`
	SessionExpiryHours      int `json:"session_expiry_hours"`
}

// DefaultLoopConfig returns sensible defaults for the given backend.
func DefaultLoopConfig(backend Backend) LoopConfig {
	switch backend {
	case BackendAnthropic:
		return LoopConfig{
			MaxCallsPerHour:         100,
			TimeoutMinutes:          15,
			MaxRetries:              3,
			PauseBetweenSecs:        5,
			CircuitBreakerNoProgress: 3,
			CircuitBreakerSameError:  5,
			SessionExpiryHours:      24,
		}
	default: // Ollama and empty
		return LoopConfig{
			MaxCallsPerHour:         0, // unlimited
			TimeoutMinutes:          30,
			MaxRetries:              3,
			PauseBetweenSecs:        1,
			CircuitBreakerNoProgress: 3,
			CircuitBreakerSameError:  5,
			SessionExpiryHours:      24,
		}
	}
}
