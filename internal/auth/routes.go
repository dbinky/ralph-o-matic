package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// AuthConfigResponse is the JSON response for GET /auth/config.
type AuthConfigResponse struct {
	Mode     string `json:"mode"`
	ClientID string `json:"client_id,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// stateCookieName is the name of the CSRF state cookie used during OAuth2 flow.
const stateCookieName = "ralph_auth_state"

// NewAuthRoutes returns a chi.Router with authentication routes:
//
//	GET  /config   — returns auth configuration (mode, client/tenant IDs)
//	GET  /login    — initiates OAuth2 login flow
//	GET  /callback — handles OAuth2 callback
//	POST /logout   — destroys session and clears cookie
func NewAuthRoutes(provider *EntraProvider, store *SessionStore, secure bool) chi.Router {
	r := chi.NewRouter()

	configLimiter := NewRateLimiter(10, 1*time.Minute)
	authFlowLimiter := NewRateLimiter(20, 1*time.Minute)
	r.With(configLimiter.Middleware).Get("/config", handleAuthConfig(provider))
	r.With(authFlowLimiter.Middleware).Get("/login", handleLogin(provider, secure))
	r.With(authFlowLimiter.Middleware).Get("/callback", handleCallback(provider, store, secure))
	r.Post("/logout", handleLogout(store, secure))

	return r
}

// handleAuthConfig returns the auth configuration.
func handleAuthConfig(provider *EntraProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := AuthConfigResponse{Mode: "none"}

		if provider != nil {
			resp.Mode = "entra"
			resp.ClientID = provider.ClientID()
			resp.TenantID = provider.TenantID()
		}

		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// handleLogin initiates the OAuth2 authorization code flow.
func handleLogin(provider *EntraProvider, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			http.NotFound(w, r)
			return
		}

		// Generate random state for CSRF protection
		stateBytes := make([]byte, 16)
		if _, err := rand.Read(stateBytes); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to generate state")
			return
		}
		state := hex.EncodeToString(stateBytes)

		// Store state in cookie
		http.SetCookie(w, &http.Cookie{
			Name:     stateCookieName,
			Value:    state,
			Path:     "/auth",
			MaxAge:   600, // 10 minutes
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})

		redirectURL := schemeHost(r, secure) + "/auth/callback"
		oauth2Cfg := provider.OAuth2Config(redirectURL)
		authURL := oauth2Cfg.AuthCodeURL(state)

		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// handleCallback handles the OAuth2 callback after the user authenticates with EntraID.
func handleCallback(provider *EntraProvider, store *SessionStore, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provider == nil {
			http.NotFound(w, r)
			return
		}

		// Validate state parameter matches cookie
		stateCookie, err := r.Cookie(stateCookieName)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "missing state cookie")
			return
		}

		stateParam := r.URL.Query().Get("state")
		if stateParam == "" || stateParam != stateCookie.Value {
			writeJSONError(w, http.StatusBadRequest, "state mismatch")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			writeJSONError(w, http.StatusBadRequest, "missing authorization code")
			return
		}

		// Exchange code for token
		redirectURL := schemeHost(r, secure) + "/auth/callback"
		oauth2Cfg := provider.OAuth2Config(redirectURL)
		oauth2Token, err := oauth2Cfg.Exchange(r.Context(), code)
		if err != nil {
			log.Printf("auth: token exchange failed: %v", err)
			writeJSONError(w, http.StatusUnauthorized, "token exchange failed")
			return
		}

		// Extract and validate the id_token
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "missing id_token in response")
			return
		}

		user, err := provider.ValidateToken(r.Context(), rawIDToken)
		if err != nil {
			log.Printf("auth: id_token validation failed: %v", err)
			writeJSONError(w, http.StatusUnauthorized, "invalid id_token")
			return
		}

		// Create session
		session := &Session{
			User:         *user,
			AccessToken:  oauth2Token.AccessToken,
			RefreshToken: oauth2Token.RefreshToken,
			ExpiresAt:    oauth2Token.Expiry,
		}
		sessionID, err := store.Create(session)
		if err != nil {
			log.Printf("auth: failed to create session: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to create session")
			return
		}

		// Set session cookie
		SetSessionCookie(w, sessionID, secure)

		// Clear state cookie
		http.SetCookie(w, &http.Cookie{
			Name:     stateCookieName,
			Value:    "",
			Path:     "/auth",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})

		// Redirect to the original URL or "/"
		redirect := r.URL.Query().Get("redirect")
		if !isValidRedirect(redirect) {
			redirect = "/"
		}
		http.Redirect(w, r, redirect, http.StatusFound)
	}
}

// handleLogout destroys the session and clears cookies.
func handleLogout(store *SessionStore, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store != nil {
			if sessionID := GetSessionID(r); sessionID != "" {
				store.Delete(sessionID)
			}
		}

		ClearSessionCookie(w, secure)
		w.WriteHeader(http.StatusOK)
	}
}

// isValidRedirect checks that a redirect URL is a safe relative path.
// It must start with "/" and must not start with "//" or "/\" to prevent
// open redirect attacks (browsers treat both as absolute URLs).
func isValidRedirect(redirect string) bool {
	if redirect == "" {
		return false
	}
	if !strings.HasPrefix(redirect, "/") {
		return false
	}
	if len(redirect) > 1 && (redirect[1] == '/' || redirect[1] == '\\') {
		return false
	}
	return true
}

// schemeHost returns the scheme and host for constructing redirect URLs.
// When secure is true, "https://" is used; otherwise "http://".
func schemeHost(r *http.Request, secure bool) string {
	scheme := "http://"
	if secure {
		scheme = "https://"
	}
	return scheme + r.Host
}
