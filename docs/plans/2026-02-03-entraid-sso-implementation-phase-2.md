# Phase 2: Session Management Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an in-memory session store with cookie management for browser-based auth, plus request context helpers for extracting user identity and roles.

**Architecture:** `SessionStore` is a thread-safe in-memory map keyed by random session ID. Sessions hold user identity (name, email, OID), roles, access/refresh tokens, and expiry. Context helpers use `context.WithValue` to thread user info through request handlers. Cookie properties enforce security best practices (`HttpOnly`, `Secure` when HTTPS, `SameSite=Lax`).

**Tech Stack:** Go stdlib (`sync`, `crypto/rand`, `context`, `net/http`, `time`). No external dependencies.

---

### Task 1: Create context helpers for user identity

**Files:**
- Create: `internal/auth/context.go`
- Test: `internal/auth/context_test.go`

**Step 1: Write the failing test**

Create `internal/auth/context_test.go`:

```go
package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserFromContext_Present(t *testing.T) {
	user := &User{
		ID:    "oid-123",
		Name:  "Ryan",
		Email: "ryan@contoso.com",
		Roles: []string{"Admin"},
	}

	ctx := ContextWithUser(context.Background(), user)
	got := UserFromContext(ctx)

	assert.NotNil(t, got)
	assert.Equal(t, "oid-123", got.ID)
	assert.Equal(t, "Ryan", got.Name)
	assert.Equal(t, "ryan@contoso.com", got.Email)
	assert.Equal(t, []string{"Admin"}, got.Roles)
}

func TestUserFromContext_Absent(t *testing.T) {
	got := UserFromContext(context.Background())
	assert.Nil(t, got)
}

func TestUser_HasRole(t *testing.T) {
	tests := []struct {
		name     string
		roles    []string
		role     string
		expected bool
	}{
		{"admin has Admin", []string{"Admin"}, "Admin", true},
		{"user has User", []string{"User"}, "User", true},
		{"admin does not have User", []string{"Admin"}, "User", false},
		{"both roles has Admin", []string{"User", "Admin"}, "Admin", true},
		{"empty roles", []string{}, "User", false},
		{"nil roles", nil, "User", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{Roles: tt.roles}
			assert.Equal(t, tt.expected, user.HasRole(tt.role))
		})
	}
}

func TestUser_IsAdmin(t *testing.T) {
	assert.True(t, (&User{Roles: []string{"Admin"}}).IsAdmin())
	assert.True(t, (&User{Roles: []string{"User", "Admin"}}).IsAdmin())
	assert.False(t, (&User{Roles: []string{"User"}}).IsAdmin())
	assert.False(t, (&User{Roles: []string{}}).IsAdmin())
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestUserFromContext|TestUser_Has|TestUser_IsAdmin" ./internal/auth/`
Expected: FAIL — `User`, `ContextWithUser`, `UserFromContext` not defined

**Step 3: Write minimal implementation**

Create `internal/auth/context.go`:

```go
package auth

import "context"

type contextKey int

const userContextKey contextKey = iota

// User represents an authenticated user extracted from a token or session
type User struct {
	ID    string   // EntraID OID (stable GUID)
	Name  string   // Display name
	Email string   // Email / preferred_username
	Roles []string // App roles from token claims
}

// HasRole returns true if the user has the specified role
func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the user has the Admin role
func (u *User) IsAdmin() bool {
	return u.HasRole("Admin")
}

// ContextWithUser returns a new context with the user attached
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext extracts the authenticated user from the context.
// Returns nil if no user is present (unauthenticated request).
func UserFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(userContextKey).(*User)
	return user
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestUserFromContext|TestUser_Has|TestUser_IsAdmin" ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/context.go internal/auth/context_test.go
git commit -m "feat(auth): add User type and request context helpers"
```

---

### Task 2: Create in-memory session store

**Files:**
- Create: `internal/auth/session.go`
- Test: `internal/auth/session_test.go`

**Step 1: Write the failing test**

Create `internal/auth/session_test.go`:

```go
package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)

	session := &Session{
		User: User{
			ID:    "oid-123",
			Name:  "Ryan",
			Email: "ryan@contoso.com",
			Roles: []string{"Admin"},
		},
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
	}

	id := store.Create(session)
	assert.NotEmpty(t, id)

	got := store.Get(id)
	require.NotNil(t, got)
	assert.Equal(t, "oid-123", got.User.ID)
	assert.Equal(t, "Ryan", got.User.Name)
	assert.Equal(t, "access-token", got.AccessToken)
	assert.Equal(t, "refresh-token", got.RefreshToken)
}

func TestSessionStore_Get_NotFound(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)
	got := store.Get("nonexistent-id")
	assert.Nil(t, got)
}

func TestSessionStore_Get_EmptyID(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)
	got := store.Get("")
	assert.Nil(t, got)
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)

	session := &Session{User: User{ID: "oid-123"}}
	id := store.Create(session)

	store.Delete(id)

	got := store.Get(id)
	assert.Nil(t, got)
}

func TestSessionStore_Delete_Nonexistent(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)
	// Should not panic
	store.Delete("nonexistent-id")
}

func TestSessionStore_Expired(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)

	session := &Session{User: User{ID: "oid-123"}}
	id := store.Create(session)

	time.Sleep(5 * time.Millisecond)

	got := store.Get(id)
	assert.Nil(t, got)
}

func TestSessionStore_MultipleUsers(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)

	s1 := &Session{User: User{ID: "user-1"}}
	s2 := &Session{User: User{ID: "user-2"}}

	id1 := store.Create(s1)
	id2 := store.Create(s2)

	assert.NotEqual(t, id1, id2)

	got1 := store.Get(id1)
	got2 := store.Get(id2)
	require.NotNil(t, got1)
	require.NotNil(t, got2)
	assert.Equal(t, "user-1", got1.User.ID)
	assert.Equal(t, "user-2", got2.User.ID)
}

func TestSessionStore_SameUserMultipleSessions(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)

	s1 := &Session{User: User{ID: "user-1"}, AccessToken: "token-a"}
	s2 := &Session{User: User{ID: "user-1"}, AccessToken: "token-b"}

	id1 := store.Create(s1)
	id2 := store.Create(s2)

	assert.NotEqual(t, id1, id2)

	got1 := store.Get(id1)
	got2 := store.Get(id2)
	require.NotNil(t, got1)
	require.NotNil(t, got2)
	assert.Equal(t, "token-a", got1.AccessToken)
	assert.Equal(t, "token-b", got2.AccessToken)
}

func TestSessionStore_Update(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)

	session := &Session{User: User{ID: "oid-123"}, AccessToken: "old-token"}
	id := store.Create(session)

	session.AccessToken = "new-token"
	store.Update(id, session)

	got := store.Get(id)
	require.NotNil(t, got)
	assert.Equal(t, "new-token", got.AccessToken)
}

func TestSessionStore_Cleanup(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)

	// Create sessions
	for i := 0; i < 10; i++ {
		store.Create(&Session{User: User{ID: "user"}})
	}

	time.Sleep(5 * time.Millisecond)

	// Cleanup should remove expired sessions
	removed := store.Cleanup()
	assert.Equal(t, 10, removed)

	// Store should be empty
	assert.Equal(t, 0, store.Len())
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)
	done := make(chan struct{})

	// Concurrent create/get/delete
	for i := 0; i < 100; i++ {
		go func() {
			session := &Session{User: User{ID: "user"}}
			id := store.Create(session)
			store.Get(id)
			store.Delete(id)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestSessionStore ./internal/auth/`
Expected: FAIL — `SessionStore` not defined

**Step 3: Write minimal implementation**

Create `internal/auth/session.go`:

```go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session represents a server-side session for browser auth
type Session struct {
	User         User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type sessionEntry struct {
	session   *Session
	expiresAt time.Time
}

// SessionStore is a thread-safe in-memory session store
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
	ttl      time.Duration
}

// NewSessionStore creates a new session store with the given session TTL
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*sessionEntry),
		ttl:      ttl,
	}
}

// Create stores a new session and returns its ID
func (s *SessionStore) Create(session *Session) string {
	id := generateSessionID()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[id] = &sessionEntry{
		session:   session,
		expiresAt: time.Now().Add(s.ttl),
	}

	return id
}

// Get retrieves a session by ID. Returns nil if not found or expired.
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

// Update replaces the session data for the given ID
func (s *SessionStore) Update(id string, session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.sessions[id]
	if !ok {
		return
	}

	entry.session = session
}

// Delete removes a session by ID
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, id)
}

// Cleanup removes expired sessions and returns the count removed
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

// Len returns the number of active sessions
func (s *SessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate session ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -race -run TestSessionStore ./internal/auth/`
Expected: PASS with no race conditions

**Step 5: Commit**

```bash
git add internal/auth/session.go internal/auth/session_test.go
git commit -m "feat(auth): add in-memory session store with TTL and cleanup"
```

---

### Task 3: Add cookie helpers

**Files:**
- Modify: `internal/auth/session.go`
- Test: `internal/auth/session_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/session_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
)

func TestSetSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "session-id-123", false)

	resp := w.Result()
	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, "ralph_session", c.Name)
	assert.Equal(t, "session-id-123", c.Value)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
	assert.False(t, c.Secure) // HTTP mode
}

func TestSetSessionCookie_Secure(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "session-id-123", true)

	resp := w.Result()
	cookies := resp.Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure)
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w)

	resp := w.Result()
	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, "ralph_session", c.Name)
	assert.Equal(t, "", c.Value)
	assert.True(t, c.MaxAge < 0) // expired
}

func TestGetSessionID(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "ralph_session", Value: "abc123"})

	id := GetSessionID(req)
	assert.Equal(t, "abc123", id)
}

func TestGetSessionID_NoCookie(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	id := GetSessionID(req)
	assert.Equal(t, "", id)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestSetSessionCookie|TestClearSessionCookie|TestGetSessionID" ./internal/auth/`
Expected: FAIL — functions not defined

**Step 3: Write minimal implementation**

Add to `internal/auth/session.go`:

```go
import "net/http"

const cookieName = "ralph_session"

// SetSessionCookie sets the session cookie on the response.
// secure should be true when the server is behind HTTPS.
func SetSessionCookie(w http.ResponseWriter, sessionID string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// GetSessionID extracts the session ID from the request cookie.
// Returns empty string if no session cookie is present.
func GetSessionID(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestSetSessionCookie|TestClearSessionCookie|TestGetSessionID" ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/session.go internal/auth/session_test.go
git commit -m "feat(auth): add cookie helpers for session management"
```

---

### Task 4: Run full test suite

**Step 1: Run all auth tests**

Run: `go test -v -race ./internal/auth/`
Expected: All PASS

**Step 2: Run existing tests for regressions**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 3: Run linter**

Run: `make lint`
Expected: No lint errors

---

## Dependencies

- **Depends on:** Phase 1 (Config types in `internal/auth/auth.go`)
- **Blocks:** Phase 4 (Middleware needs sessions + context)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 156-200, "Session & Token Management")
- Test pattern: `internal/db/db_test.go:96-104` (for `newTestDB` helper pattern)
