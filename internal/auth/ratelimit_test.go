package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// okHandler is a simple handler that returns 200 OK for testing.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Minute)
	handler := rl.Middleware(okHandler)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/config", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "request %d should be allowed", i+1)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(5, 1*time.Minute)
	handler := rl.Middleware(okHandler)

	// First 5 requests should succeed
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/config", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "request %d should be allowed", i+1)
	}

	// 6th request should be rate limited
	req := httptest.NewRequest("GET", "/config", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Verify Retry-After header is present and is a positive integer
	retryAfter := w.Header().Get("Retry-After")
	require.NotEmpty(t, retryAfter, "Retry-After header should be set")
	seconds, err := strconv.Atoi(retryAfter)
	require.NoError(t, err, "Retry-After should be an integer")
	assert.Greater(t, seconds, 0, "Retry-After should be positive")
}

func TestRateLimiter_DifferentIPsIndependent(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute)
	handler := rl.Middleware(okHandler)

	// Exhaust limit for IP A
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/config", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// IP A should now be blocked
	req := httptest.NewRequest("GET", "/config", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// IP B should still work
	req = httptest.NewRequest("GET", "/config", nil)
	req.RemoteAddr = "10.0.0.2:54321"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)
	handler := rl.Middleware(okHandler)

	// Exhaust limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/config", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Should be blocked now
	req := httptest.NewRequest("GET", "/config", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	req = httptest.NewRequest("GET", "/config", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimiter_XForwardedForIgnored(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute)
	handler := rl.Middleware(okHandler)

	// X-Forwarded-For should be ignored; rate limiting uses RemoteAddr only.
	// All requests come from the same RemoteAddr, so XFF should not matter.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/config", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Third request should be blocked based on RemoteAddr, not XFF
	req := httptest.NewRequest("GET", "/config", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.99") // different XFF should not help
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(50, 1*time.Minute)
	handler := rl.Middleware(okHandler)

	var wg sync.WaitGroup
	results := make([]int, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/config", nil)
			req.RemoteAddr = "192.168.1.1:12345"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			results[idx] = w.Code
		}(i)
	}

	wg.Wait()

	// All 50 should be allowed (limit is 50)
	okCount := 0
	for _, code := range results {
		if code == http.StatusOK {
			okCount++
		}
	}
	assert.Equal(t, 50, okCount, "all 50 concurrent requests should succeed")
}
