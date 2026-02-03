package executor

import "time"

// Session tracks a Claude CLI session for continuity across iterations.
type Session struct {
	ID        string
	CreatedAt time.Time
	ExpiresIn time.Duration
}

// NewSession creates a session with the given ID and expiry duration.
func NewSession(id string, expiresIn time.Duration) *Session {
	return &Session{
		ID:        id,
		CreatedAt: time.Now(),
		ExpiresIn: expiresIn,
	}
}

// IsExpired returns true if the session has exceeded its expiry duration.
func (s *Session) IsExpired() bool {
	return time.Since(s.CreatedAt) > s.ExpiresIn
}

// IsValid returns true if the session has a non-empty ID and is not expired.
func (s *Session) IsValid() bool {
	return s.ID != "" && !s.IsExpired()
}

// buildClaudeArgs constructs the CLI args for a claude invocation.
// skipPermissions controls --dangerously-skip-permissions (true for Ollama).
// session is optional; if valid, --resume is added.
func buildClaudeArgs(skipPermissions bool, session *Session) []string {
	args := []string{"--print", "--output-format", "json"}
	if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if session != nil && session.IsValid() {
		args = append(args, "--resume", session.ID)
	}
	return args
}
