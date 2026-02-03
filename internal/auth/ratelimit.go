package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter provides IP-based rate limiting using a fixed-window algorithm.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	limit   int
	window  time.Duration
}

// ipBucket tracks the request count and window expiration for a single IP.
type ipBucket struct {
	count     int
	windowEnd time.Time
}

// NewRateLimiter creates a rate limiter that allows limit requests per window per IP.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*ipBucket),
		limit:   limit,
		window:  window,
	}
}

// Middleware returns an HTTP middleware that enforces the rate limit.
// Requests exceeding the limit receive a 429 Too Many Requests response
// with a Retry-After header indicating when the client can retry.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()

		rl.mu.Lock()
		bucket, ok := rl.buckets[ip]
		if !ok || now.After(bucket.windowEnd) {
			// No bucket or window expired: start a new window.
			bucket = &ipBucket{
				count:     0,
				windowEnd: now.Add(rl.window),
			}
			rl.buckets[ip] = bucket
		}

		bucket.count++
		if bucket.count > rl.limit {
			retryAfter := int(time.Until(bucket.windowEnd).Seconds()) + 1
			rl.mu.Unlock()

			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP address from the request.
// It checks X-Forwarded-For first (taking the first IP before any comma),
// then falls back to the remote address from r.RemoteAddr.
func clientIP(r *http.Request) string {
	// Check X-Forwarded-For header (proxy scenario)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (the original client)
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Fall back to RemoteAddr, stripping the port
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr might not have a port (unlikely in practice)
		return r.RemoteAddr
	}
	return host
}
