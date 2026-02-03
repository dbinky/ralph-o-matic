# Phase 7: Dashboard Auth Display & Rate Limiting Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update the dashboard templates to show the authenticated user's name and job ownership info when auth is enabled. Add IP-based rate limiting to `GET /auth/config` (10 requests/minute per IP).

**Architecture:** Dashboard templates get conditional display of user name and a logout link in the header. Job list shows owner names when auth is enabled. Rate limiting is a simple in-memory token bucket per IP, implemented as Chi middleware applied only to the `/auth/config` route.

**Tech Stack:** Go stdlib (`sync`, `time`, `net/http`), Go templates, existing dashboard package

---

### Task 1: Add rate limiter for /auth/config

**Files:**
- Create: `internal/auth/ratelimit.go`
- Test: `internal/auth/ratelimit_test.go`

**Step 1: Write the failing test**

Create `internal/auth/ratelimit_test.go`:

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/auth/config", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should succeed", i+1)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(5, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use up the limit
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/auth/config", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// 6th request should be rate limited
	req := httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "10.0.0.1:5678"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestRateLimiter_DifferentIPsIndependent(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust limit for IP A
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/auth/config", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// IP A is now rate limited
	req := httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "1.1.1.1:5678"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// IP B should still work
	req = httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "2.2.2.2:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond) // short window for testing

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use up limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/auth/config", nil)
		req.RemoteAddr = "3.3.3.3:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Should be rate limited
	req := httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "3.3.3.3:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Wait for window reset
	time.Sleep(60 * time.Millisecond)

	// Should work again
	req = httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "3.3.3.3:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimiter_XForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with X-Forwarded-For
	req := httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "proxy:1234"
	req.Header.Set("X-Forwarded-For", "real-client-ip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Second request from same real IP
	req = httptest.NewRequest("GET", "/auth/config", nil)
	req.RemoteAddr = "proxy:5678"
	req.Header.Set("X-Forwarded-For", "real-client-ip")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(100, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/auth/config", nil)
			req.RemoteAddr = "5.5.5.5:1234"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 50; i++ {
		<-done
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestRateLimiter ./internal/auth/`
Expected: FAIL — `NewRateLimiter` not defined

**Step 3: Write minimal implementation**

Create `internal/auth/ratelimit.go`:

```go
package auth

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type ipBucket struct {
	count     int
	windowEnd time.Time
}

// RateLimiter provides IP-based rate limiting
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	limit   int
	window  time.Duration
}

// NewRateLimiter creates a rate limiter allowing `limit` requests per `window` per IP
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*ipBucket),
		limit:   limit,
		window:  window,
	}
}

// Middleware returns HTTP middleware that enforces the rate limit
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		rl.mu.Lock()
		bucket, ok := rl.buckets[ip]
		now := time.Now()

		if !ok || now.After(bucket.windowEnd) {
			bucket = &ipBucket{
				count:     0,
				windowEnd: now.Add(rl.window),
			}
			rl.buckets[ip] = bucket
		}

		bucket.count++
		allowed := bucket.count <= rl.limit
		remaining := bucket.windowEnd.Sub(now)
		rl.mu.Unlock()

		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())+1))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (client IP)
		if i := len(xff); i > 0 {
			for j := 0; j < len(xff); j++ {
				if xff[j] == ',' {
					return xff[:j]
				}
			}
			return xff
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -race -run TestRateLimiter ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/ratelimit.go internal/auth/ratelimit_test.go
git commit -m "feat(auth): add IP-based rate limiter for /auth/config"
```

---

### Task 2: Apply rate limiter to /auth/config route

**Files:**
- Modify: `internal/auth/routes.go`

**Step 1: Update NewAuthRoutes to wrap /config with rate limiter**

```go
func NewAuthRoutes(provider *EntraProvider, store *SessionStore, secure bool) chi.Router {
	r := chi.NewRouter()

	configLimiter := NewRateLimiter(10, 1*time.Minute)
	r.With(configLimiter.Middleware).Get("/config", handleAuthConfig(provider))

	r.Get("/login", handleLogin(provider, secure))
	r.Get("/callback", handleCallback(provider, store, secure))
	r.Post("/logout", handleLogout(store))

	return r
}
```

Add `time` to imports.

**Step 2: Run existing tests**

Run: `go test -v -run TestAuthConfigEndpoint ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/routes.go
git commit -m "feat(auth): apply rate limiter to /auth/config endpoint"
```

---

### Task 3: Update dashboard templates for auth display

**Files:**
- Modify: `web/templates/layout.html`
- Modify: `internal/dashboard/dashboard.go`

**Step 1: Add user info to template data**

Modify `internal/dashboard/dashboard.go` to pass user info to templates. Add a helper that extracts the user from the request context:

```go
import "github.com/ryan/ralph-o-matic/internal/auth"

// templateData adds common template data including auth user
func (d *Dashboard) templateData(r *http.Request) map[string]interface{} {
	data := map[string]interface{}{
		"AuthUser": auth.UserFromContext(r.Context()),
	}
	return data
}
```

Update template rendering to merge this data.

**Step 2: Add conditional user display in layout.html**

Add to the header/nav section of `web/templates/layout.html`:

```html
{{if .AuthUser}}
<div class="auth-user">
    <span>{{.AuthUser.Name}}</span>
    <form method="POST" action="/auth/logout" style="display:inline">
        <button type="submit">Logout</button>
    </form>
</div>
{{end}}
```

**Step 3: Add owner column to job list in dashboard.html**

If auth is enabled (indicated by `AuthUser` being present), show the owner name column in the jobs table.

**Step 4: Verify templates render**

Run: `go test -v ./internal/dashboard/`
Expected: PASS

**Step 5: Commit**

```bash
git add web/templates/layout.html internal/dashboard/dashboard.go
git commit -m "feat(dashboard): show authenticated user and job owner names"
```

---

### Task 4: Run full test suite

**Step 1: Run all tests**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 2: Run linter**

Run: `make lint`
Expected: No lint errors

---

## Dependencies

- **Depends on:** Phase 2 (User context helpers), Phase 4 (Auth middleware sets user in context), Phase 5 (Job ownership fields)
- **Blocks:** Nothing

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 191-200, "Auth Config Discovery" + rate limiting)
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 634-669, "Testing 11f: Rate Limiting & Job Ownership")
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 309-310, "Dashboard Changes")
- Existing dashboard: `internal/dashboard/dashboard.go`
- Existing templates: `web/templates/layout.html`, `web/templates/dashboard.html`
