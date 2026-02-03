package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Middleware returns an HTTP middleware that authenticates requests.
//
// When provider is nil (auth mode none), requests pass through without
// setting any user context. When provider is set, the middleware checks
// for a Bearer token first, then falls back to session cookies.
func Middleware(provider *EntraProvider, store *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Auth mode none: pass through without setting user
			if provider == nil {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Check for Bearer token (takes precedence over cookie)
			if token := extractBearerToken(r); token != "" {
				user, err := provider.ValidateToken(r.Context(), token)
				if err != nil {
					writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
					return
				}
				next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), user)))
				return
			}

			// 2. Check for session cookie
			if sessionID := GetSessionID(r); sessionID != "" {
				if session := store.Get(sessionID); session != nil {
					user := &session.User
					next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), user)))
					return
				}
			}

			// 3. No valid authentication found
			if isBrowserRequest(r) {
				http.Redirect(w, r, "/auth/login", http.StatusFound)
				return
			}

			writeJSONError(w, http.StatusUnauthorized, "authentication required")
		})
	}
}

// RequireRole wraps an http.HandlerFunc and enforces that the authenticated
// user has the specified role. Admin users always pass through. If no user
// is in context (auth mode none), the request passes through.
func RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			// Auth mode none: pass through
			next.ServeHTTP(w, r)
			return
		}

		if user.IsAdmin() || user.HasRole(role) {
			next.ServeHTTP(w, r)
			return
		}

		writeJSONError(w, http.StatusForbidden, "insufficient permissions")
	}
}

// extractBearerToken parses the "Bearer <token>" value from the Authorization header.
// Returns an empty string if the header is absent or malformed.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimPrefix(auth, prefix)
}

// isBrowserRequest checks whether the Accept header indicates a browser request
// (contains "text/html").
func isBrowserRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
