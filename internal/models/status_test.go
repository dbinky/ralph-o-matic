package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStatus_Valid(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"queued", StatusQueued, true},
		{"running", StatusRunning, true},
		{"paused", StatusPaused, true},
		{"completed", StatusCompleted, true},
		{"failed", StatusFailed, true},
		{"cancelled", StatusCancelled, true},
		{"empty", JobStatus(""), false},
		{"unknown", JobStatus("pending"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}

func TestJobStatus_IsTerminal(t *testing.T) {
	terminals := []JobStatus{StatusCompleted, StatusFailed, StatusCancelled}
	nonTerminals := []JobStatus{StatusQueued, StatusRunning, StatusPaused}

	for _, s := range terminals {
		assert.True(t, s.IsTerminal(), "%s should be terminal", s)
	}
	for _, s := range nonTerminals {
		assert.False(t, s.IsTerminal(), "%s should not be terminal", s)
	}
}

func TestJobStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from    JobStatus
		to      JobStatus
		allowed bool
	}{
		// From queued
		{StatusQueued, StatusRunning, true},
		{StatusQueued, StatusCancelled, true},
		{StatusQueued, StatusPaused, false},
		{StatusQueued, StatusCompleted, false},
		// From running
		{StatusRunning, StatusPaused, true},
		{StatusRunning, StatusCompleted, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusCancelled, true},
		{StatusRunning, StatusQueued, false},
		// From paused
		{StatusPaused, StatusRunning, true},
		{StatusPaused, StatusCancelled, true},
		{StatusPaused, StatusQueued, false},
		{StatusPaused, StatusCompleted, false},
		// From terminal states
		{StatusCompleted, StatusRunning, false},
		{StatusFailed, StatusRunning, false},
		{StatusCancelled, StatusRunning, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			assert.Equal(t, tt.allowed, tt.from.CanTransitionTo(tt.to))
		})
	}
}

func TestJobStatus_JSON(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		data, err := json.Marshal(StatusRunning)
		require.NoError(t, err)
		assert.Equal(t, `"running"`, string(data))
	})

	t.Run("unmarshal valid", func(t *testing.T) {
		var s JobStatus
		err := json.Unmarshal([]byte(`"paused"`), &s)
		require.NoError(t, err)
		assert.Equal(t, StatusPaused, s)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var s JobStatus
		err := json.Unmarshal([]byte(`"pending"`), &s)
		assert.Error(t, err)
	})
}
