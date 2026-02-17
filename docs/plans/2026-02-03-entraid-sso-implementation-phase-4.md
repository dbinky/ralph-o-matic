# Phase 4: Auth Middleware & Server Routes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire up auth middleware into the Chi router and add `/auth/*` routes (login, callback, logout, config). The middleware is a no-op when auth mode is `none` and enforces JWT/session validation when mode is `entra`.

**Architecture:** `internal/auth/middleware.go` exports `Middleware()` (Chi middleware) and `RequireRole()` (per-route wrapper). The middleware distinguishes browser requests (session cookie) from API requests (`Authorization: Bearer` header). Server startup in `internal/api/server.go` loads auth config, optionally initializes `EntraProvider`, and injects middleware + auth routes.

**Tech Stack:** `go-chi/chi/v5`, `coreos/go-oidc/v3`, `golang.org/x/oauth2`, Go stdlib

---

### Task 1: Create auth middleware (mode=none passthrough)

**Files:**
- Create: `internal/auth/middleware.go`
- Test: `internal/auth/middleware_test.go`

**Step 1: Write the failing test**

Create `internal/auth/middleware_test.go`:

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMiddleware_ModeNone_PassesThrough(t *testing.T) {
	mw := Middleware(nil, nil) // nil provider + nil store = mode none

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no user in context
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
	mw := Middleware(nil, nil)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No Authorization header, no cookies
	req := httptest.NewRequest("POST", "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestMiddleware_ModeNone ./internal/auth/`
Expected: FAIL — `Middleware` not defined

**Step 3: Write minimal implementation**

Create `internal/auth/middleware.go`:

```go
package auth

import (
	"net/http"
	"strings"
)

// Middleware returns Chi middleware that enforces authentication.
// When provider is nil (auth mode "none"), all requests pass through.
func Middleware(provider *EntraProvider, store *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Auth mode "none": pass through
			if provider == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Check for Bearer token (API requests)
			if token := extractBearerToken(r); token != "" {
				user, err := provider.ValidateToken(r.Context(), token)
				if err != nil {
					http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
					return
				}
				ctx := ContextWithUser(r.Context(), user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Check for session cookie (browser requests)
			if sessionID := GetSessionID(r); sessionID != "" {
				session := store.Get(sessionID)
				if session != nil {
					ctx := ContextWithUser(r.Context(), &session.User)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// No valid auth: decide response based on request type
			if isBrowserRequest(r) {
				http.Redirect(w, r, "/auth/login", http.StatusFound)
				return
			}

			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		})
	}
}

// RequireRole wraps a handler to enforce that the authenticated user has the specified role.
// Admins always pass. Returns 403 if the role check fails.
func RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())

		// If no user in context (auth mode none), allow through
		if user == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Admins can do anything
		if user.IsAdmin() {
			next.ServeHTTP(w, r)
			return
		}

		if !user.HasRole(role) {
			http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func isBrowserRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestMiddleware_ModeNone ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "feat(auth): add auth middleware with mode=none passthrough"
```

---

### Task 2: Test middleware with Bearer token validation

**Files:**
- Modify: `internal/auth/middleware_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/middleware_test.go`:

```go
import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/go-jose/go-jose.v2"
)

func TestMiddleware_BearerToken_Valid(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "test-client", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	mw := Middleware(provider, store)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-1",
		"preferred_username": "ryan@contoso.com",
		"name":               "Ryan",
		"roles":              []string{"Admin"},
	})

	var capturedUser *User
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "user-oid-1", capturedUser.ID)
	assert.Equal(t, "Ryan", capturedUser.Name)
}

func TestMiddleware_BearerToken_Expired(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "test-client", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	mw := Middleware(provider, store)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss": srv.URL,
		"aud": "test-client",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"oid": "user-oid-1",
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for expired token")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_BearerToken_NoHeader_APIRequest(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "test-client", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	mw := Middleware(provider, store)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for unauthenticated API request")
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_BrowserRequest_NoSession_Redirects(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "test-client", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	mw := Middleware(provider, store)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "test-client", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	session := &Session{
		User: User{ID: "session-user", Name: "Session User", Roles: []string{"User"}},
	}
	sessionID := store.Create(session)

	mw := Middleware(provider, store)

	var capturedUser *User
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "ralph_session", Value: sessionID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "session-user", capturedUser.ID)
}

func TestMiddleware_BearerTakesPrecedenceOverCookie(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "test-client", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	session := &Session{
		User: User{ID: "cookie-user", Name: "Cookie User"},
	}
	sessionID := store.Create(session)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "bearer-user",
		"preferred_username": "bearer@contoso.com",
		"name":               "Bearer User",
		"roles":              []string{"Admin"},
	})

	mw := Middleware(provider, store)

	var capturedUser *User
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.AddCookie(&http.Cookie{Name: "ralph_session", Value: sessionID})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "bearer-user", capturedUser.ID)
}
```

**Step 2: Run test to verify it passes**

Run: `go test -v -race -run "TestMiddleware_" ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/middleware_test.go
git commit -m "test(auth): add middleware tests for Bearer, session, and redirect"
```

---

### Task 3: Test RequireRole wrapper

**Files:**
- Modify: `internal/auth/middleware_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/middleware_test.go`:

```go
func TestRequireRole_NoUser_PassesThrough(t *testing.T) {
	handler := RequireRole("User", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_AdminAlwaysPasses(t *testing.T) {
	user := &User{ID: "admin", Roles: []string{"Admin"}}
	ctx := ContextWithUser(context.Background(), user)

	handler := RequireRole("User", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/jobs", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_UserHasRole(t *testing.T) {
	user := &User{ID: "user1", Roles: []string{"User"}}
	ctx := ContextWithUser(context.Background(), user)

	handler := RequireRole("User", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/jobs", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_UserLacksRole(t *testing.T) {
	user := &User{ID: "user1", Roles: []string{"User"}}
	ctx := ContextWithUser(context.Background(), user)

	handler := RequireRole("Admin", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	req := httptest.NewRequest("PATCH", "/api/config", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireRole_NoRoles(t *testing.T) {
	user := &User{ID: "user1", Roles: []string{}}
	ctx := ContextWithUser(context.Background(), user)

	handler := RequireRole("User", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	req := httptest.NewRequest("GET", "/api/jobs", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

**Step 2: Run test to verify it passes**

Run: `go test -v -run TestRequireRole ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/middleware_test.go
git commit -m "test(auth): add RequireRole wrapper tests"
```

---

### Task 4: Add auth routes handler (login, callback, logout, config)

**Files:**
- Create: `internal/auth/routes.go`
- Test: `internal/auth/routes_test.go`

**Step 1: Write the failing test**

Create `internal/auth/routes_test.go`:

```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthConfigEndpoint_ModeNone(t *testing.T) {
	handler := NewAuthRoutes(nil, nil, false)

	r := chi.NewRouter()
	r.Mount("/auth", handler)

	req := httptest.NewRequest("GET", "/auth/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp AuthConfigResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "none", resp.Mode)
	assert.Empty(t, resp.ClientID)
	assert.Empty(t, resp.TenantID)
}

func TestAuthConfigEndpoint_ModeEntra(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "my-tenant", ClientID: "my-client", ClientSecret: "my-secret",
	}, srv.URL)
	require.NoError(t, err)

	store := NewSessionStore(30 * time.Minute)
	handler := NewAuthRoutes(provider, store, false)

	r := chi.NewRouter()
	r.Mount("/auth", handler)

	req := httptest.NewRequest("GET", "/auth/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp AuthConfigResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "entra", resp.Mode)
	assert.Equal(t, "my-client", resp.ClientID)
	assert.Equal(t, "my-tenant", resp.TenantID)
}

func TestAuthConfigEndpoint_CacheControl(t *testing.T) {
	handler := NewAuthRoutes(nil, nil, false)

	r := chi.NewRouter()
	r.Mount("/auth", handler)

	req := httptest.NewRequest("GET", "/auth/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Contains(t, w.Header().Get("Cache-Control"), "max-age")
}

func TestLogoutEndpoint(t *testing.T) {
	store := NewSessionStore(30 * time.Minute)
	session := &Session{User: User{ID: "user-1"}}
	sessionID := store.Create(session)

	key := testRSAKey(t)
	srv := testOIDCServer(t, key)
	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID: "t", ClientID: "c", ClientSecret: "s",
	}, srv.URL)
	require.NoError(t, err)

	handler := NewAuthRoutes(provider, store, false)

	r := chi.NewRouter()
	r.Mount("/auth", handler)

	req := httptest.NewRequest("POST", "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "ralph_session", Value: sessionID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Session should be deleted
	assert.Nil(t, store.Get(sessionID))

	// Response should clear cookie
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "ralph_session" {
			found = true
			assert.True(t, c.MaxAge < 0)
		}
	}
	assert.True(t, found, "session cookie should be cleared")
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestAuthConfigEndpoint|TestLogoutEndpoint" ./internal/auth/`
Expected: FAIL — `NewAuthRoutes`, `AuthConfigResponse` not defined

**Step 3: Write minimal implementation**

Create `internal/auth/routes.go`:

```go
package auth

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AuthConfigResponse is returned by GET /auth/config
type AuthConfigResponse struct {
	Mode     string `json:"mode"`
	ClientID string `json:"client_id,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// NewAuthRoutes returns a Chi router with auth-related routes.
// These routes are exempt from auth middleware.
func NewAuthRoutes(provider *EntraProvider, store *SessionStore, secure bool) chi.Router {
	r := chi.NewRouter()

	r.Get("/config", handleAuthConfig(provider))
	r.Get("/login", handleLogin(provider, secure))
	r.Get("/callback", handleCallback(provider, store, secure))
	r.Post("/logout", handleLogout(store))

	return r
}

func handleAuthConfig(provider *EntraProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")

		resp := AuthConfigResponse{Mode: "none"}
		if provider != nil {
			resp.Mode = "entra"
			resp.ClientID = provider.ClientID()
			resp.TenantID = provider.TenantID()
		}

		json.NewEncoder(w).Encode(resp)
	}
}

func handleLogin(provider *EntraProvider, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			http.Error(w, "authentication not configured", http.StatusNotFound)
			return
		}

		// Generate state and PKCE verifier, store in cookie, redirect to EntraID
		// Full implementation will use oauth2.Config.AuthCodeURL with PKCE
		state := generateSessionID()[:16]

		oauthCfg := provider.OAuth2Config(schemeHost(r, secure) + "/auth/callback")

		// Store state in a short-lived cookie for CSRF protection
		http.SetCookie(w, &http.Cookie{
			Name:     "ralph_auth_state",
			Value:    state,
			Path:     "/auth",
			MaxAge:   600, // 10 minutes
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})

		url := oauthCfg.AuthCodeURL(state)
		http.Redirect(w, r, url, http.StatusFound)
	}
}

func handleCallback(provider *EntraProvider, store *SessionStore, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			http.Error(w, "authentication not configured", http.StatusNotFound)
			return
		}

		// Verify state
		stateCookie, err := r.Cookie("ralph_auth_state")
		if err != nil || stateCookie.Value == "" {
			http.Error(w, "missing auth state", http.StatusBadRequest)
			return
		}

		if r.URL.Query().Get("state") != stateCookie.Value {
			http.Error(w, "invalid state parameter", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}

		// Exchange code for tokens
		oauthCfg := provider.OAuth2Config(schemeHost(r, secure) + "/auth/callback")
		oauth2Token, err := oauthCfg.Exchange(r.Context(), code)
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusInternalServerError)
			return
		}

		// Extract and validate ID token
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "no id_token in response", http.StatusInternalServerError)
			return
		}

		user, err := provider.ValidateToken(r.Context(), rawIDToken)
		if err != nil {
			http.Error(w, "token validation failed", http.StatusUnauthorized)
			return
		}

		// Create session
		session := &Session{
			User:         *user,
			AccessToken:  oauth2Token.AccessToken,
			RefreshToken: oauth2Token.RefreshToken,
		}
		sessionID := store.Create(session)

		// Clear the auth state cookie
		http.SetCookie(w, &http.Cookie{
			Name:   "ralph_auth_state",
			Value:  "",
			Path:   "/auth",
			MaxAge: -1,
		})

		// Set session cookie and redirect to dashboard
		SetSessionCookie(w, sessionID, secure)
		redirectTo := r.URL.Query().Get("redirect")
		if redirectTo == "" {
			redirectTo = "/"
		}
		http.Redirect(w, r, redirectTo, http.StatusFound)
	}
}

func handleLogout(store *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := GetSessionID(r)
		if sessionID != "" {
			store.Delete(sessionID)
		}
		ClearSessionCookie(w)
		w.WriteHeader(http.StatusOK)
	}
}

// schemeHost returns the scheme + host for building redirect URIs
func schemeHost(r *http.Request, secure bool) string {
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestAuthConfigEndpoint|TestLogoutEndpoint" ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/routes.go internal/auth/routes_test.go
git commit -m "feat(auth): add auth routes (config, login, callback, logout)"
```

---

### Task 5: Integrate auth into server startup

**Files:**
- Modify: `internal/api/server.go`
- Modify: `cmd/server/main.go`

**Step 1: Write the failing test**

Add to `internal/api/server_test.go`:

```go
func TestServer_Health_WithNilAuth(t *testing.T) {
	// Verify health endpoint works when auth is nil (mode=none)
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
```

**Step 2: Run test to verify it passes** (existing behavior should work)

Run: `go test -v -run TestServer_Health ./internal/api/`
Expected: PASS

**Step 3: Modify server to accept auth config**

Modify `internal/api/server.go` — add `AuthConfig` to `Server` struct and modify `NewServer`:

```go
import "github.com/ryan/ralph-o-matic/internal/auth"

type Server struct {
	db         *db.DB
	queue      *queue.Queue
	dashboard  *dashboard.Dashboard
	addr       string
	router     chi.Router
	server     *http.Server
	authProvider *auth.EntraProvider    // nil when auth mode is "none"
	sessions     *auth.SessionStore     // nil when auth mode is "none"
	secure       bool                   // true when behind HTTPS
}

// ServerOptions configures the API server
type ServerOptions struct {
	AuthProvider *auth.EntraProvider
	Sessions     *auth.SessionStore
	Secure       bool
}

func NewServer(database *db.DB, q *queue.Queue, addr string, opts *ServerOptions) *Server {
	// ... existing template loading ...

	s := &Server{
		db:        database,
		queue:     q,
		dashboard: dashboard.New(database, q, templatesFS),
		addr:      addr,
	}

	if opts != nil {
		s.authProvider = opts.AuthProvider
		s.sessions = opts.Sessions
		s.secure = opts.Secure
	}

	s.setupRoutes()
	return s
}
```

Update `setupRoutes()` to mount auth routes and middleware:

```go
func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(corsMiddleware)

	// Health check (exempt from auth)
	r.Get("/health", s.handleHealth)

	// Auth routes (exempt from auth)
	r.Mount("/auth", auth.NewAuthRoutes(s.authProvider, s.sessions, s.secure))

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.authProvider, s.sessions))

		// Dashboard
		r.Get("/", s.dashboard.HandleIndex)
		r.Get("/config", s.dashboard.HandleConfig)
		r.Get("/jobs/{jobID}", func(w http.ResponseWriter, r *http.Request) {
			// ... existing handler ...
		})

		// API routes
		r.Route("/api", func(r chi.Router) {
			// ... existing API routes ...
		})
	})

	s.router = r
}
```

Update `cmd/server/main.go` to load auth config:

```go
import "github.com/ryan/ralph-o-matic/internal/auth"

// In run():
authCfg, err := auth.LoadConfig(os.Getenv, "")
if err != nil {
	return fmt.Errorf("failed to load auth config: %w", err)
}

if err := authCfg.Validate(); err != nil {
	return fmt.Errorf("invalid auth config: %w", err)
}

var serverOpts *api.ServerOptions
if authCfg.Mode == auth.AuthModeEntra {
	provider, err := auth.NewEntraProvider(context.Background(), authCfg.Entra, "")
	if err != nil {
		return fmt.Errorf("failed to initialize EntraID provider: %w", err)
	}
	serverOpts = &api.ServerOptions{
		AuthProvider: provider,
		Sessions:     auth.NewSessionStore(30 * time.Minute),
	}
	log.Printf("Auth mode: entra (tenant: %s)", authCfg.Entra.TenantID)
} else {
	log.Println("WARNING: running without authentication — all endpoints are open")
}

srv := api.NewServer(database, q, addr, serverOpts)
```

Update `newTestServer` helper in `internal/api/server_test.go`:

```go
func newTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	q := queue.New(database)
	srv := NewServer(database, q, ":9090", nil) // nil = no auth
	return srv, database
}
```

**Step 4: Run tests to verify**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/api/server.go cmd/server/main.go internal/api/server_test.go
git commit -m "feat(auth): integrate auth middleware and routes into server"
```

---

### Task 6: Run full test suite

**Step 1: Run all tests**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 2: Run linter**

Run: `make lint`
Expected: No lint errors

**Step 3: Build**

Run: `make build`
Expected: Clean build

---

## Dependencies

- **Depends on:** Phase 1 (Config), Phase 2 (Sessions, Context), Phase 3 (EntraProvider)
- **Blocks:** Phase 5 (Job ownership needs middleware context), Phase 6 (CLI needs `/auth/config`), Phase 7 (Rate limiting on `/auth/config`)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 109-151, "Auth Middleware & Request Flow")
- Existing server: `internal/api/server.go` (current middleware stack and route setup)
- Existing test: `internal/api/server_test.go` (test helper pattern)
- Main entry: `cmd/server/main.go` (startup flow)

## Notes for Implementer

- The `NewServer` function signature changes to accept `*ServerOptions`. Update all call sites (tests, main).
- Health check and `/auth/*` routes are mounted OUTSIDE the auth middleware group.
- Dashboard and API routes are mounted INSIDE the auth middleware group.
- When auth is `none`, the middleware is a no-op passthrough.
- The `handleCallback` does a real OAuth2 token exchange, so it can only be fully tested with a mock token endpoint (Phase 9 integration tests will cover this end-to-end).
