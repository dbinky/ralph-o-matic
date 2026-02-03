package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLoopConfig_Anthropic(t *testing.T) {
	cfg := DefaultLoopConfig(BackendAnthropic)

	assert.Equal(t, 100, cfg.MaxCallsPerHour)
	assert.Equal(t, 15, cfg.TimeoutMinutes)
	assert.Equal(t, 5, cfg.PauseBetweenSecs)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 3, cfg.CircuitBreakerNoProgress)
	assert.Equal(t, 5, cfg.CircuitBreakerSameError)
	assert.Equal(t, 24, cfg.SessionExpiryHours)
}

func TestDefaultLoopConfig_Ollama(t *testing.T) {
	cfg := DefaultLoopConfig(BackendOllama)

	assert.Equal(t, 0, cfg.MaxCallsPerHour, "ollama should have no rate limit")
	assert.Equal(t, 30, cfg.TimeoutMinutes)
	assert.Equal(t, 1, cfg.PauseBetweenSecs)
	assert.Equal(t, 3, cfg.MaxRetries)
}

func TestDefaultLoopConfig_Empty_DefaultsToOllama(t *testing.T) {
	cfg := DefaultLoopConfig("")

	assert.Equal(t, 0, cfg.MaxCallsPerHour, "empty backend should default to ollama behavior")
	assert.Equal(t, 30, cfg.TimeoutMinutes)
}
