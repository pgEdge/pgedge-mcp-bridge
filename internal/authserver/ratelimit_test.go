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
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowUnderLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	defer rl.Close()

	for i := 0; i < 5; i++ {
		if !rl.Allow("192.168.1.1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_BlockOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	defer rl.Close()

	for i := 0; i < 3; i++ {
		rl.Allow("192.168.1.1")
	}

	if rl.Allow("192.168.1.1") {
		t.Error("4th request should be blocked")
	}
}

func TestRateLimiter_SeparateIPs(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	defer rl.Close()

	// Fill up IP1
	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.1")

	if rl.Allow("10.0.0.1") {
		t.Error("IP1 should be blocked")
	}

	// IP2 should still be allowed
	if !rl.Allow("10.0.0.2") {
		t.Error("IP2 should be allowed")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)
	defer rl.Close()

	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.1")

	if rl.Allow("10.0.0.1") {
		t.Error("should be blocked before window expires")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("10.0.0.1") {
		t.Error("should be allowed after window expires")
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)
	defer rl.Close()

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow("10.0.0.1")
		}()
	}

	wg.Wait()
	close(allowed)

	allowedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		}
	}

	if allowedCount != 100 {
		t.Errorf("expected exactly 100 allowed requests, got %d", allowedCount)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	defer rl.Close()

	handler := rl.RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/oauth/token", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("POST", "/oauth/token", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}
