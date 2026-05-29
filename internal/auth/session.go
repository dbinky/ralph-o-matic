package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Session holds the data for an authenticated user session.
type Session struct {
	User         User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// sessionEntry wraps a session with its expiration time in the store.
type sessionEntry struct {
	session   *Session
	expiresAt time.Time
}

// SessionStore is a thread-safe in-memory session store.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry
	ttl      time.Duration
}

// NewSessionStore creates a new session store with the given TTL for sessions.
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]sessionEntry),
		ttl:      ttl,
	}
}

// Create stores a session and returns a random hex session ID (32 bytes, 64 hex chars).
func (s *SessionStore) Create(session *Session) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	id := hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[id] = sessionEntry{
		session:   session,
		expiresAt: time.Now().Add(s.ttl),
	}
	return id, nil
}

// Get returns the session for the given ID, or nil if not found, empty, or expired.
func (s *SessionStore) Get(id string) *Session {
	if id == "" {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.sessions[id]
	if !ok {
		return nil
	}
	if time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.session
}

// Update replaces the session data for the given ID.
// No-op if the ID does not exist.
func (s *SessionStore) Update(id string, session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.sessions[id]
	if !ok {
		return
	}
	entry.session = session
	s.sessions[id] = entry
}

// Delete removes a session by ID. No-op if the ID does not exist.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, id)
}

// Cleanup removes expired sessions and returns the number removed.
func (s *SessionStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0
	for id, entry := range s.sessions {
		if now.After(entry.expiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}

// Len returns the number of sessions in the store.
func (s *SessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.sessions)
}

// cookieName is the name of the session cookie.
const cookieName = "ralph_session"

// SetSessionCookie sets the session cookie on the response.
// If secure is true, the Secure flag is set (use for HTTPS).
func SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	// G124: HttpOnly and SameSite=Lax are always set. Secure is intentionally
	// conditional on `secure` (HTTPS deployments) so local HTTP development can
	// still authenticate; production runs with secure=true.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is deployment-conditional; see comment above
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie by setting MaxAge=-1.
// It mirrors the security flags from SetSessionCookie to ensure the
// browser matches and removes the correct cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	// G124: mirrors SetSessionCookie's flags so the browser removes the matching
	// cookie. Secure is deployment-conditional for the same local-dev reason.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is deployment-conditional; see comment above
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// GetSessionID reads the session ID from the request cookie.
// Returns an empty string if the cookie is absent.
func GetSessionID(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
