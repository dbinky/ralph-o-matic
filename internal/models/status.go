package models

import (
	"encoding/json"
	"fmt"
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusPaused    JobStatus = "paused"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// Valid returns true if the status is a known value
func (s JobStatus) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusPaused, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the status is a final state
func (s JobStatus) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if transitioning from s to target is valid
func (s JobStatus) CanTransitionTo(target JobStatus) bool {
	switch s {
	case StatusQueued:
		return target == StatusRunning || target == StatusCancelled
	case StatusRunning:
		return target == StatusPaused || target == StatusCompleted ||
			target == StatusFailed || target == StatusCancelled
	case StatusPaused:
		return target == StatusRunning || target == StatusCancelled
	case StatusCompleted, StatusFailed, StatusCancelled:
		return false // Terminal states cannot transition
	default:
		return false
	}
}

// UnmarshalJSON implements json.Unmarshaler with validation
func (s *JobStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	status := JobStatus(str)
	if !status.Valid() {
		return fmt.Errorf("invalid job status: %q", str)
	}
	*s = status
	return nil
}
