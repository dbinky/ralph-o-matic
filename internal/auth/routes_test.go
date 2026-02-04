package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AuthConfig endpoint tests ---

func TestAuthRoutes_Config_ModeNone(t *testing.T) {
	router := NewAuthRoutes(nil, nil, false)

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp AuthConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "none", resp.Mode)
	assert.Empty(t, resp.ClientID)
	assert.Empty(t, resp.TenantID)
}

func TestAuthRoutes_Config_ModeEntra(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	router := NewAuthRoutes(provider, store, false)

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp AuthConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "entra", resp.Mode)
	assert.Equal(t, "test-client", resp.ClientID)
	assert.Equal(t, "test-tenant", resp.TenantID)
}

func TestAuthRoutes_Config_HasCacheControl(t *testing.T) {
	router := NewAuthRoutes(nil, nil, false)

	req := httptest.NewRequest("GET", "/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
}

// --- Logout endpoint test ---

func TestAuthRoutes_Logout_DeletesSessionAndClearsCookie(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	// Create a session
	session := &Session{
		User: User{
			ID:    "logout-user",
			Name:  "Logout User",
			Email: "logout@example.com",
			Roles: []string{"User"},
		},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	sessionID, err := store.Create(session)
	require.NoError(t, err)
	require.Equal(t, 1, store.Len())

	router := NewAuthRoutes(provider, store, false)

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Session should be deleted from store
	assert.Nil(t, store.Get(sessionID))
	assert.Equal(t, 0, store.Len())

	// Session cookie should be cleared (MaxAge=-1)
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == cookieName {
			found = true
			assert.Equal(t, -1, c.MaxAge)
		}
	}
	assert.True(t, found, "session cookie should be set with MaxAge=-1")
}

// --- Login endpoint tests ---

func TestAuthRoutes_Login_ModeNone_Returns404(t *testing.T) {
	router := NewAuthRoutes(nil, nil, false)

	req := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAuthRoutes_Login_ModeEntra_Redirects(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	router := NewAuthRoutes(provider, store, false)

	req := httptest.NewRequest("GET", "/login", nil)
	req.Host = "localhost:9090"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)

	location := w.Header().Get("Location")
	assert.NotEmpty(t, location)
	// Should redirect to the OIDC provider's authorize endpoint
	assert.Contains(t, location, "/authorize")
	assert.Contains(t, location, "client_id=test-client")
	assert.Contains(t, location, "redirect_uri=")

	// Should have set state cookie
	cookies := w.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "ralph_auth_state" {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie, "state cookie should be set")
	assert.True(t, stateCookie.HttpOnly)
	assert.Equal(t, "/auth", stateCookie.Path)
	assert.Equal(t, 600, stateCookie.MaxAge)
}

// --- Callback endpoint tests ---

func TestAuthRoutes_Callback_ModeNone_Returns404(t *testing.T) {
	router := NewAuthRoutes(nil, nil, false)

	req := httptest.NewRequest("GET", "/callback?code=abc&state=xyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAuthRoutes_Callback_MissingState_Returns400(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	router := NewAuthRoutes(provider, store, false)

	req := httptest.NewRequest("GET", "/callback?code=abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthRoutes_Callback_StateMismatch_Returns400(t *testing.T) {
	provider, _, _ := newTestProvider(t)
	store := NewSessionStore(30 * time.Minute)

	router := NewAuthRoutes(provider, store, false)

	req := httptest.NewRequest("GET", "/callback?code=abc&state=wrongstate", nil)
	req.AddCookie(&http.Cookie{Name: "ralph_auth_state", Value: "correctstate"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
