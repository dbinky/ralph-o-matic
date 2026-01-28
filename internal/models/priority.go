package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Priority represents job priority level
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// Valid returns true if the priority is a known value
func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	default:
		return false
	}
}

// Weight returns a numeric weight for sorting (higher = more important)
func (p Priority) Weight() int {
	switch p {
	case PriorityHigh:
		return 3
	case PriorityNormal:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

// UnmarshalJSON implements json.Unmarshaler with validation
func (p *Priority) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParsePriority(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// ParsePriority parses a string into a Priority (case-insensitive)
func ParsePriority(s string) (Priority, error) {
	switch strings.ToLower(s) {
	case "high":
		return PriorityHigh, nil
	case "normal":
		return PriorityNormal, nil
	case "low":
		return PriorityLow, nil
	default:
		return "", fmt.Errorf("invalid priority: %q", s)
	}
}
