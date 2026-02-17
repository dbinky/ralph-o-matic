package auth

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SessionStore.Create and Get ---

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := NewSessionStore(time.Hour)
	session := &Session{
		User: User{
			ID:    "oid-abc",
			Name:  "Alice",
			Email: "alice@example.com",
			Roles: []string{"User"},
		},
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-456",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	id, err := store.Create(session)
	require.NoError(t, err)
	assert.Len(t, id, 64) // 32 bytes = 64 hex chars

	got := store.Get(id)
	require.NotNil(t, got)
	assert.Equal(t, "oid-abc", got.User.ID)
	assert.Equal(t, "Alice", got.User.Name)
	assert.Equal(t, "alice@example.com", got.User.Email)
	assert.Equal(t, []string{"User"}, got.User.Roles)
	assert.Equal(t, "access-token-123", got.AccessToken)
	assert.Equal(t, "refresh-token-456", got.RefreshToken)
}

func TestSessionStore_CreateReturnsUniqueIDs(t *testing.T) {
	store := NewSessionStore(time.Hour)
	session := &Session{User: User{ID: "oid-1"}}

	ids := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id, err := store.Create(session)
		require.NoError(t, err)
		_, exists := ids[id]
		assert.False(t, exists, "duplicate session ID generated")
		ids[id] = struct{}{}
	}
}

// --- SessionStore.Get not found / empty ---

func TestSessionStore_GetNotFound(t *testing.T) {
	store := NewSessionStore(time.Hour)
	got := store.Get("nonexistent-id")
	assert.Nil(t, got)
}

func TestSessionStore_GetEmptyID(t *testing.T) {
	store := NewSessionStore(time.Hour)
	got := store.Get("")
	assert.Nil(t, got)
}

// --- SessionStore.Delete ---

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore(time.Hour)
	session := &Session{User: User{ID: "oid-del"}}
	id, err := store.Create(session)
	require.NoError(t, err)

	// Confirm it exists
	require.NotNil(t, store.Get(id))

	store.Delete(id)
	assert.Nil(t, store.Get(id))
}

func TestSessionStore_DeleteNonexistent(t *testing.T) {
	store := NewSessionStore(time.Hour)
	// Should not panic
	store.Delete("nonexistent-id")
}

// --- Expired sessions ---

func TestSessionStore_ExpiredSessionReturnsNil(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)
	session := &Session{User: User{ID: "oid-exp"}}
	id, err := store.Create(session)
	require.NoError(t, err)

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	got := store.Get(id)
	assert.Nil(t, got)
}

// --- Multiple users, same user multiple sessions ---

func TestSessionStore_MultipleUsersIsolated(t *testing.T) {
	store := NewSessionStore(time.Hour)

	s1 := &Session{User: User{ID: "oid-1", Name: "Alice"}}
	s2 := &Session{User: User{ID: "oid-2", Name: "Bob"}}

	id1, err := store.Create(s1)
	require.NoError(t, err)
	id2, err := store.Create(s2)
	require.NoError(t, err)

	got1 := store.Get(id1)
	got2 := store.Get(id2)

	require.NotNil(t, got1)
	require.NotNil(t, got2)
	assert.Equal(t, "Alice", got1.User.Name)
	assert.Equal(t, "Bob", got2.User.Name)
}

func TestSessionStore_SameUserMultipleSessions(t *testing.T) {
	store := NewSessionStore(time.Hour)

	s1 := &Session{User: User{ID: "oid-1"}, AccessToken: "token-a"}
	s2 := &Session{User: User{ID: "oid-1"}, AccessToken: "token-b"}

	id1, err := store.Create(s1)
	require.NoError(t, err)
	id2, err := store.Create(s2)
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)

	got1 := store.Get(id1)
	got2 := store.Get(id2)

	require.NotNil(t, got1)
	require.NotNil(t, got2)
	assert.Equal(t, "token-a", got1.AccessToken)
	assert.Equal(t, "token-b", got2.AccessToken)
}

// --- Update ---

func TestSessionStore_Update(t *testing.T) {
	store := NewSessionStore(time.Hour)
	session := &Session{
		User:        User{ID: "oid-upd", Name: "Alice"},
		AccessToken: "old-token",
	}
	id, err := store.Create(session)
	require.NoError(t, err)

	updated := &Session{
		User:        User{ID: "oid-upd", Name: "Alice Updated"},
		AccessToken: "new-token",
	}
	store.Update(id, updated)

	got := store.Get(id)
	require.NotNil(t, got)
	assert.Equal(t, "Alice Updated", got.User.Name)
	assert.Equal(t, "new-token", got.AccessToken)
}

func TestSessionStore_UpdateNonexistent(t *testing.T) {
	store := NewSessionStore(time.Hour)
	// Should not panic
	store.Update("nonexistent", &Session{User: User{ID: "oid"}})
	assert.Nil(t, store.Get("nonexistent"))
}

// --- Cleanup ---

func TestSessionStore_Cleanup(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)

	s1 := &Session{User: User{ID: "oid-1"}}
	s2 := &Session{User: User{ID: "oid-2"}}
	_, err := store.Create(s1)
	require.NoError(t, err)
	_, err = store.Create(s2)
	require.NoError(t, err)

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	// Add one more that's still valid (new store TTL won't help, so we create a new store)
	store2 := NewSessionStore(time.Hour)
	s3 := &Session{User: User{ID: "oid-3"}}
	_, err = store2.Create(s3)
	require.NoError(t, err)

	removed := store.Cleanup()
	assert.Equal(t, 2, removed)
	assert.Equal(t, 0, store.Len())
}

func TestSessionStore_CleanupKeepsValid(t *testing.T) {
	store := NewSessionStore(time.Hour)
	s1 := &Session{User: User{ID: "oid-1"}}
	_, err := store.Create(s1)
	require.NoError(t, err)

	removed := store.Cleanup()
	assert.Equal(t, 0, removed)
	assert.Equal(t, 1, store.Len())
}

// --- Len ---

func TestSessionStore_Len(t *testing.T) {
	store := NewSessionStore(time.Hour)
	assert.Equal(t, 0, store.Len())

	_, err := store.Create(&Session{User: User{ID: "oid-1"}})
	require.NoError(t, err)
	assert.Equal(t, 1, store.Len())

	_, err = store.Create(&Session{User: User{ID: "oid-2"}})
	require.NoError(t, err)
	assert.Equal(t, 2, store.Len())
}

// --- Concurrent access (race detector) ---

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := NewSessionStore(time.Hour)
	var wg sync.WaitGroup

	const goroutines = 100

	ids := make([]string, goroutines)
	// Phase 1: concurrent creates
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := &Session{User: User{ID: "oid", Name: "User"}}
			id, err := store.Create(s)
			assert.NoError(t, err)
			ids[idx] = id
		}(i)
	}
	wg.Wait()

	assert.Equal(t, goroutines, store.Len())

	// Phase 2: concurrent reads
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got := store.Get(ids[idx])
			assert.NotNil(t, got)
		}(i)
	}
	wg.Wait()

	// Phase 3: concurrent deletes
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			store.Delete(ids[idx])
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 0, store.Len())
}

func TestSessionStore_ConcurrentMixed(t *testing.T) {
	store := NewSessionStore(time.Hour)
	var wg sync.WaitGroup

	// Mix of creates, gets, deletes, updates, cleanup running concurrently
	const goroutines = 100
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &Session{User: User{ID: "oid"}}
			id, err := store.Create(s)
			assert.NoError(t, err)
			store.Get(id)
			store.Update(id, &Session{User: User{ID: "oid-updated"}})
			store.Get(id)
			store.Cleanup()
			store.Delete(id)
		}()
	}
	wg.Wait()
}

// --- Cookie helpers ---

func TestSetSessionCookie_Secure(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "session-id-123", true)

	resp := w.Result()
	defer resp.Body.Close()

	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, cookieName, c.Name)
	assert.Equal(t, "session-id-123", c.Value)
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
}

func TestSetSessionCookie_NotSecure(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "session-id-456", false)

	resp := w.Result()
	defer resp.Body.Close()

	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, cookieName, c.Name)
	assert.Equal(t, "session-id-456", c.Value)
	assert.True(t, c.HttpOnly)
	assert.False(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w, false)

	resp := w.Result()
	defer resp.Body.Close()

	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, cookieName, c.Name)
	assert.Equal(t, "", c.Value)
	assert.Equal(t, -1, c.MaxAge)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
}

func TestClearSessionCookie_Secure(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w, true)

	resp := w.Result()
	defer resp.Body.Close()

	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.True(t, c.Secure)
	assert.True(t, c.HttpOnly)
}

func TestGetSessionID_WithCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: "my-session-id",
	})

	id := GetSessionID(req)
	assert.Equal(t, "my-session-id", id)
}

func TestGetSessionID_NoCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	id := GetSessionID(req)
	assert.Empty(t, id)
}
