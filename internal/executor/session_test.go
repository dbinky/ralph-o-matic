package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSession_New(t *testing.T) {
	s := NewSession("abc-123", 24*time.Hour)

	assert.Equal(t, "abc-123", s.ID)
	assert.False(t, s.IsExpired())
}

func TestSession_Expired(t *testing.T) {
	s := &Session{
		ID:        "old-session",
		CreatedAt: time.Now().Add(-25 * time.Hour),
		ExpiresIn: 24 * time.Hour,
	}

	assert.True(t, s.IsExpired())
}

func TestSession_NotExpiredAtBoundary(t *testing.T) {
	s := &Session{
		ID:        "boundary-session",
		CreatedAt: time.Now().Add(-23 * time.Hour),
		ExpiresIn: 24 * time.Hour,
	}

	assert.False(t, s.IsExpired())
}

func TestSession_EmptyID(t *testing.T) {
	s := NewSession("", 24*time.Hour)

	assert.Empty(t, s.ID)
	// Empty session should be treated as absent — IsValid returns false
	assert.False(t, s.IsValid())
}

func TestSession_IsValid(t *testing.T) {
	s := NewSession("valid-id", 24*time.Hour)
	assert.True(t, s.IsValid())

	expired := &Session{
		ID:        "expired-id",
		CreatedAt: time.Now().Add(-25 * time.Hour),
		ExpiresIn: 24 * time.Hour,
	}
	assert.False(t, expired.IsValid())
}

func TestBuildArgs_WithSession(t *testing.T) {
	args := buildClaudeArgs(true, NewSession("sess-123", 24*time.Hour))

	assert.Contains(t, args, "--resume")
	// Find the value after --resume
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) {
			assert.Equal(t, "sess-123", args[i+1])
		}
	}
}

func TestBuildArgs_WithoutSession(t *testing.T) {
	args := buildClaudeArgs(true, nil)
	assert.NotContains(t, args, "--resume")
}

func TestBuildArgs_WithExpiredSession(t *testing.T) {
	expired := &Session{
		ID:        "old-session",
		CreatedAt: time.Now().Add(-25 * time.Hour),
		ExpiresIn: 24 * time.Hour,
	}
	args := buildClaudeArgs(true, expired)
	assert.NotContains(t, args, "--resume")
}

func TestBuildArgs_IncludesJSONFormat(t *testing.T) {
	args := buildClaudeArgs(false, nil)
	assert.Contains(t, args, "--output-format")
	assert.Contains(t, args, "json")
}

func TestBuildArgs_SkipPermissions(t *testing.T) {
	args := buildClaudeArgs(true, nil)
	assert.Contains(t, args, "--dangerously-skip-permissions")
}

func TestBuildArgs_NoSkipPermissionsWhenFalse(t *testing.T) {
	args := buildClaudeArgs(false, nil)
	assert.NotContains(t, args, "--dangerously-skip-permissions")
}
