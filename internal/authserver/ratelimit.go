/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package authserver

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// window tracks request counts for a single IP within a time window.
type window struct {
	count  int
	expiry time.Time
}

// RateLimiter provides IP-based fixed-window rate limiting.
type RateLimiter struct {
	maxRequests    int
	windowDuration time.Duration

	mu      sync.Mutex
	windows map[string]*window

	done chan struct{}
}

// NewRateLimiter creates a new rate limiter with the given limits.
func NewRateLimiter(maxRequests int, windowDuration time.Duration) *RateLimiter {
	rl := &RateLimiter{
		maxRequests:    maxRequests,
		windowDuration: windowDuration,
		windows:        make(map[string]*window),
		done:           make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks whether a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	w, ok := rl.windows[ip]
	if !ok || now.After(w.expiry) {
		// New window
		rl.windows[ip] = &window{
			count:  1,
			expiry: now.Add(rl.windowDuration),
		}
		return true
	}

	w.count++
	return w.count <= rl.maxRequests
}

// RateLimitMiddleware returns an HTTP middleware that enforces rate limits.
func (rl *RateLimiter) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		if !rl.Allow(ip) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close stops the cleanup goroutine.
func (rl *RateLimiter) Close() {
	close(rl.done)
}

// cleanupLoop periodically removes expired windows.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.cleanup()
		}
	}
}

// cleanup removes expired windows.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, w := range rl.windows {
		if now.After(w.expiry) {
			delete(rl.windows, ip)
		}
	}
}

// extractIP extracts the client IP from the request.
func extractIP(r *http.Request) string {
	// Use RemoteAddr, stripping port
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
