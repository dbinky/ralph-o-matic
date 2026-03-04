package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Middleware tests ---

func TestMiddleware_ModeNone_PassesThrough(t *testing.T) {
	handler := Middleware(nil, nil, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No user should be set in context
		user := UserFromContext(r.Context())
		assert.Nil(t, user)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_ModeNone_NoHeaderRequired(t *testing.T) {
	handler := Middleware(nil, nil, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	// Request with no Authorization header at all
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestMiddleware_BearerToken_Valid(t *testing.T) {
	provider, key, srv := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	claims := map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-mw",
		"preferred_username": "mw@example.com",
		"name":               "MW User",
		"roles":              []string{"User"},
	}
	token := signTestJWT(t, key, claims)

	var capturedUser *User
	handler := Middleware(provider, store, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "user-oid-mw", capturedUser.ID)
	assert.Equal(t, "mw@example.com", capturedUser.Email)
	assert.Equal(t, "MW User", capturedUser.Name)
}

func TestMiddleware_BearerToken_Expired(t *testing.T) {
	provider, key, srv := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	claims := map[string]interface{}{
		"iss": srv.URL,
		"aud": "test-client",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"oid": "user-oid-expired",
	}
	token := signTestJWT(t, key, claims)

	handler := Middleware(provider, store, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for expired token")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "invalid or expired token", resp["error"])
}

func TestMiddleware_NoAuth_APIRequest_Returns401(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	handler := Middleware(provider, store, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for unauthenticated API request")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "authentication required", resp["error"])
}

func TestMiddleware_BrowserRequest_NoSession_RedirectsToLogin(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	handler := Middleware(provider, store, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for unauthenticated browser request")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/auth/login", w.Header().Get("Location"))
}

func TestMiddleware_SessionCookie_Valid(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	session := &Session{
		User: User{
			ID:    "session-user-1",
			Name:  "Session User",
			Email: "session@example.com",
			Roles: []string{"User"},
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	sessionID, err := store.Create(session)
	require.NoError(t, err)

	var capturedUser *User
	handler := Middleware(provider, store, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "session-user-1", capturedUser.ID)
	assert.Equal(t, "Session User", capturedUser.Name)
}

func TestMiddleware_BearerTakesPrecedenceOverCookie(t *testing.T) {
	provider, key, srv := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	// Create a session with one user
	session := &Session{
		User: User{
			ID:    "cookie-user",
			Name:  "Cookie User",
			Email: "cookie@example.com",
			Roles: []string{"User"},
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	sessionID, err := store.Create(session)
	require.NoError(t, err)

	// Create a token with a different user
	claims := map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "bearer-user",
		"preferred_username": "bearer@example.com",
		"name":               "Bearer User",
		"roles":              []string{"Admin"},
	}
	token := signTestJWT(t, key, claims)

	var capturedUser *User
	handler := Middleware(provider, store, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedUser)
	// Bearer user should win
	assert.Equal(t, "bearer-user", capturedUser.ID)
	assert.Equal(t, "Bearer User", capturedUser.Name)
}

// --- RequireRole tests ---

func TestRequireRole_NoUser_PassesThrough(t *testing.T) {
	called := false
	handler := RequireRole("Admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// No user in context (auth mode none scenario)
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_AdminAlwaysPasses(t *testing.T) {
	called := false
	handler := RequireRole("SpecialRole", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	user := &User{ID: "admin-1", Roles: []string{"Admin"}}
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req = req.WithContext(ContextWithUser(context.Background(), user))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_UserHasRole_Passes(t *testing.T) {
	called := false
	handler := RequireRole("Editor", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	user := &User{ID: "editor-1", Roles: []string{"Editor", "Viewer"}}
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req = req.WithContext(ContextWithUser(context.Background(), user))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_UserLacksRole_Returns403(t *testing.T) {
	handler := RequireRole("Admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when user lacks role")
	}))

	user := &User{ID: "viewer-1", Roles: []string{"Viewer"}}
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req = req.WithContext(ContextWithUser(context.Background(), user))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "insufficient permissions", resp["error"])
}

func TestRequireRole_NoRoles_Returns403(t *testing.T) {
	handler := RequireRole("Admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when user has no roles")
	}))

	user := &User{ID: "norole-1", Roles: []string{}}
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req = req.WithContext(ContextWithUser(context.Background(), user))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "insufficient permissions", resp["error"])
}

// --- APIKey middleware tests ---

func TestMiddleware_APIKey_CorrectKey_Passes(t *testing.T) {
	const key = "secret-api-key"
	called := false
	handler := Middleware(nil, nil, key)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// No user context set in apikey mode
		assert.Nil(t, UserFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_APIKey_WrongKey_Returns401(t *testing.T) {
	const key = "correct-key"
	handler := Middleware(nil, nil, key)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for wrong API key")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "API key required", resp["error"])
}

func TestMiddleware_APIKey_NoHeader_Returns401(t *testing.T) {
	const key = "correct-key"
	handler := Middleware(nil, nil, key)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with no auth header")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_APIKey_MalformedHeader_Returns401(t *testing.T) {
	const key = "correct-key"
	handler := Middleware(nil, nil, key)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with malformed header")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Token "+key) // wrong scheme
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
