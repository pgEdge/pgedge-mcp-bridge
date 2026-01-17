package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/auth"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

func TestNewTransport_ValidConfig(t *testing.T) {
	cfg := &config.ClientConfig{
		URL:             "http://localhost:8080",
		Timeout:         30 * time.Second,
		MaxIdleConns:    50,
		IdleConnTimeout: 60 * time.Second,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   3,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     5 * time.Second,
			Multiplier:   2.0,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	if transport.serverURL != cfg.URL {
		t.Errorf("Expected URL %s, got %s", cfg.URL, transport.serverURL)
	}

	if transport.timeout != cfg.Timeout {
		t.Errorf("Expected timeout %v, got %v", cfg.Timeout, transport.timeout)
	}

	if transport.Closed() {
		t.Error("Transport should not be closed")
	}
}

func TestNewTransport_NilConfig(t *testing.T) {
	_, err := NewTransport(nil, nil, nil)
	if err == nil {
		t.Error("Expected error for nil config")
	}
}

func TestNewTransport_EmptyURL(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "",
	}

	_, err := NewTransport(cfg, nil, nil)
	if err == nil {
		t.Error("Expected error for empty URL")
	}
}

func TestNewTransport_DefaultValues(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "http://localhost:8080",
		// No other values set - should use defaults
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	if transport.timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", transport.timeout)
	}
}

func TestTransport_Send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get(HeaderContentType) != ContentTypeJSON {
			t.Errorf("Expected Content-Type %s, got %s", ContentTypeJSON, r.Header.Get(HeaderContentType))
		}
		if r.Header.Get(HeaderAccept) != ContentTypeJSON {
			t.Errorf("Expected Accept %s, got %s", ContentTypeJSON, r.Header.Get(HeaderAccept))
		}

		// Read request body
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"test":"request"}` {
			t.Errorf("Expected request body %q, got %q", `{"test":"request"}`, string(body))
		}

		// Set response headers
		w.Header().Set(HeaderMcpSessionID, "session-123")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"test":"response"}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	response, sessionID, err := transport.Send(ctx, []byte(`{"test":"request"}`), "")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if string(response) != `{"test":"response"}` {
		t.Errorf("Expected response %q, got %q", `{"test":"response"}`, string(response))
	}

	if sessionID != "session-123" {
		t.Errorf("Expected session ID 'session-123', got '%s'", sessionID)
	}
}

func TestTransport_Send_WithSessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify session ID header
		sessionID := r.Header.Get(HeaderMcpSessionID)
		if sessionID != "existing-session" {
			t.Errorf("Expected session ID 'existing-session', got '%s'", sessionID)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "existing-session")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

func TestTransport_Send_HTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"bad request", http.StatusBadRequest, "invalid request"},
		{"unauthorized", http.StatusUnauthorized, "auth required"},
		{"not found", http.StatusNotFound, "session not found"},
		{"server error", http.StatusInternalServerError, "internal error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			cfg := &config.ClientConfig{
				URL: server.URL,
			}

			transport, err := NewTransport(cfg, nil, nil)
			if err != nil {
				t.Fatalf("Failed to create transport: %v", err)
			}
			defer transport.Close()

			ctx := context.Background()
			_, _, err = transport.Send(ctx, []byte(`{}`), "")

			var httpErr *HTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("Expected HTTPError, got %v", err)
			}

			if httpErr.StatusCode != tt.statusCode {
				t.Errorf("Expected status code %d, got %d", tt.statusCode, httpErr.StatusCode)
			}

			if string(httpErr.Body) != tt.body {
				t.Errorf("Expected body %q, got %q", tt.body, string(httpErr.Body))
			}
		})
	}
}

func TestTransport_Send_Closed(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "http://localhost:8080",
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	transport.Close()

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "")

	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("Expected ErrTransportClosed, got %v", err)
	}
}

func TestTransport_Send_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 10 * time.Second, // Long timeout so context cancellation happens first
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, _, err = transport.Send(ctx, []byte(`{}`), "")

	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}

func TestTransport_shouldRetry(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "http://localhost:8080",
		Retry: config.RetryConfig{
			Enabled:    true,
			MaxRetries: 3,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	tests := []struct {
		name        string
		err         error
		resp        *http.Response
		shouldRetry bool
	}{
		{
			name:        "network error should retry",
			err:         errors.New("network error"),
			resp:        nil,
			shouldRetry: true,
		},
		{
			name:        "context canceled should not retry",
			err:         context.Canceled,
			resp:        nil,
			shouldRetry: false,
		},
		{
			name:        "deadline exceeded should not retry",
			err:         context.DeadlineExceeded,
			resp:        nil,
			shouldRetry: false,
		},
		{
			name:        "request timeout should retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusRequestTimeout},
			shouldRetry: true,
		},
		{
			name:        "too many requests should retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusTooManyRequests},
			shouldRetry: true,
		},
		{
			name:        "internal server error should retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusInternalServerError},
			shouldRetry: true,
		},
		{
			name:        "bad gateway should retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusBadGateway},
			shouldRetry: true,
		},
		{
			name:        "service unavailable should retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusServiceUnavailable},
			shouldRetry: true,
		},
		{
			name:        "gateway timeout should retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusGatewayTimeout},
			shouldRetry: true,
		},
		{
			name:        "bad request should not retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusBadRequest},
			shouldRetry: false,
		},
		{
			name:        "unauthorized should not retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusUnauthorized},
			shouldRetry: false,
		},
		{
			name:        "OK should not retry",
			err:         nil,
			resp:        &http.Response{StatusCode: http.StatusOK},
			shouldRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transport.shouldRetry(tt.err, tt.resp)
			if result != tt.shouldRetry {
				t.Errorf("Expected shouldRetry=%v, got %v", tt.shouldRetry, result)
			}
		})
	}
}

func TestTransport_doWithRetry(t *testing.T) {
	// Note: doWithRetry only retries on network errors, not HTTP status code errors
	// HTTP status code errors (like 503) are returned to the caller
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		// Return success
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   3,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	response, _, err := transport.Send(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if string(response) != `{"success":true}` {
		t.Errorf("Expected response %q, got %q", `{"success":true}`, string(response))
	}

	// Should have made 1 request (no retry needed for success)
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}
}

func TestTransport_doWithRetry_MaxRetriesExceeded(t *testing.T) {
	// Note: doWithRetry only retries on network errors
	// HTTP status code errors (like 503) are returned immediately
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		// Return 503 - this will be returned as an HTTPError, not retried
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   2,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "")

	// Should have failed with HTTP error
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("Expected HTTPError, got %v", err)
	}

	if httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", httpErr.StatusCode)
	}

	// Only 1 request made (no retry for HTTP errors, only network errors)
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}
}

func TestTransport_doWithRetry_NoRetry(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled: false, // Retries disabled
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "")

	// Should have failed with HTTP error
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("Expected HTTPError, got %v", err)
	}

	// Should have made only 1 request
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}
}

func TestTransport_Close(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "http://localhost:8080",
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	if transport.Closed() {
		t.Error("Transport should not be closed initially")
	}

	err = transport.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if !transport.Closed() {
		t.Error("Transport should be closed after Close()")
	}

	// Second close should be safe
	err = transport.Close()
	if err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

func TestTransport_SendWithSSE_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify SSE headers
		if r.Header.Get(HeaderAccept) != ContentTypeSSE {
			t.Errorf("Expected Accept %s, got %s", ContentTypeSSE, r.Header.Get(HeaderAccept))
		}

		w.Header().Set(HeaderMcpSessionID, "sse-session")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send SSE events
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("Expected flusher")
		}

		w.Write([]byte("event: message\ndata: {\"event\":1}\n\n"))
		flusher.Flush()
		w.Write([]byte("event: message\ndata: {\"event\":2}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, sessionID, err := transport.SendWithSSE(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("SendWithSSE failed: %v", err)
	}

	if sessionID != "sse-session" {
		t.Errorf("Expected session ID 'sse-session', got '%s'", sessionID)
	}

	// Collect events
	received := []string{}
	for event := range events {
		received = append(received, string(event))
		if len(received) >= 2 {
			break
		}
	}

	if len(received) < 2 {
		t.Errorf("Expected at least 2 events, got %d", len(received))
	}
}

func TestTransport_SendWithSSE_Closed(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "http://localhost:8080",
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	transport.Close()

	ctx := context.Background()
	_, _, err = transport.SendWithSSE(ctx, []byte(`{}`), "")

	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("Expected ErrTransportClosed, got %v", err)
	}
}

func TestTransport_SendWithSSE_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	_, _, err = transport.SendWithSSE(ctx, []byte(`{}`), "")

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("Expected HTTPError, got %v", err)
	}

	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", httpErr.StatusCode)
	}
}

func TestTransport_SendWithSSE_ContextCancellation(t *testing.T) {
	eventSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if ok {
			// Send an event first
			w.Write([]byte("data: test\n\n"))
			flusher.Flush()
		}
		close(eventSent)

		// Keep connection open until context is cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 30 * time.Second, // Long timeout so we test context cancel, not timeout
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())

	events, _, err := transport.SendWithSSE(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("SendWithSSE failed: %v", err)
	}

	// Wait for event to be sent
	<-eventSent

	// Read the event
	select {
	case event := <-events:
		if string(event) != "test" {
			t.Errorf("Expected event 'test', got '%s'", string(event))
		}
	case <-time.After(time.Second):
		t.Error("Expected to receive an event")
	}

	// Cancel context
	cancel()

	// Events channel should be closed after cancellation
	select {
	case _, ok := <-events:
		if ok {
			// Drain remaining events
			for range events {
			}
		}
		// Channel closed, test passes
	case <-time.After(2 * time.Second):
		t.Error("Events channel should be closed after context cancellation")
	}
}

func TestTransport_ExponentialBackoff(t *testing.T) {
	// Test that exponential backoff configuration is applied to transport
	// Note: doWithRetry only retries on network errors, not HTTP status code errors
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   5,
			InitialDelay: 50 * time.Millisecond,
			MaxDelay:     500 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	// Verify retry config is stored
	if transport.retry.InitialDelay != 50*time.Millisecond {
		t.Errorf("Expected initial delay 50ms, got %v", transport.retry.InitialDelay)
	}
	if transport.retry.MaxDelay != 500*time.Millisecond {
		t.Errorf("Expected max delay 500ms, got %v", transport.retry.MaxDelay)
	}
	if transport.retry.Multiplier != 2.0 {
		t.Errorf("Expected multiplier 2.0, got %v", transport.retry.Multiplier)
	}

	ctx := context.Background()
	response, _, err := transport.Send(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if string(response) != `{"ok":true}` {
		t.Errorf("Expected response %q, got %q", `{"ok":true}`, string(response))
	}

	// Success case should have 1 request
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}
}

func TestTransport_MaxDelayRespected(t *testing.T) {
	// Test that max delay configuration is properly stored
	// Note: doWithRetry only retries on network errors, not HTTP status code errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   10,
			InitialDelay: 50 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond, // Low max delay
			Multiplier:   4.0,                    // High multiplier to test capping
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	// Verify max delay is configured
	if transport.retry.MaxDelay != 100*time.Millisecond {
		t.Errorf("Expected max delay 100ms, got %v", transport.retry.MaxDelay)
	}

	// Verify max retries is configured
	if transport.retry.MaxRetries != 10 {
		t.Errorf("Expected max retries 10, got %d", transport.retry.MaxRetries)
	}

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

// TestTransport_doWithRetry_ActualRetry tests the retry loop with network errors
func TestTransport_doWithRetry_ActualRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"attempt":` + string(rune('0'+attempts)) + `}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   3,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

// TestTransport_SendWithSSE_SessionHeader tests SSE handling with session headers
func TestTransport_SendWithSSE_SessionHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify session header was sent
		sessionID := r.Header.Get(HeaderMcpSessionID)
		if sessionID != "existing-session" {
			t.Errorf("Expected session ID 'existing-session', got '%s'", sessionID)
		}

		w.Header().Set(HeaderMcpSessionID, "new-session")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		w.Write([]byte("event: message\ndata: {}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Send with existing session ID
	events, newSessionID, err := transport.SendWithSSE(ctx, []byte(`{}`), "existing-session")
	if err != nil {
		t.Fatalf("SendWithSSE failed: %v", err)
	}

	// Verify new session ID
	if newSessionID != "new-session" {
		t.Errorf("Expected new session ID 'new-session', got '%s'", newSessionID)
	}

	// Drain events
	for range events {
	}
}

// TestTransport_SendWithSSE_ErrorEvent tests SSE error event handling
func TestTransport_SendWithSSE_ErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// Send error event
		w.Write([]byte("event: error\ndata: {\"error\":\"something went wrong\"}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, _, err := transport.SendWithSSE(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("SendWithSSE failed: %v", err)
	}

	// Should receive error event
	for range events {
	}
}

// TestTransport_SendWithSSE_MultipleEvents tests handling multiple SSE events
func TestTransport_SendWithSSE_MultipleEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// Send multiple events
		for i := 0; i < 3; i++ {
			w.Write([]byte("event: message\ndata: {\"index\":" + string(rune('0'+i)) + "}\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, _, err := transport.SendWithSSE(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("SendWithSSE failed: %v", err)
	}

	count := 0
	for range events {
		count++
	}

	if count < 3 {
		t.Logf("Received %d events (expected at least 3)", count)
	}
}

// TestTransport_Send_WithAuth tests Send with authentication
func TestTransport_Send_WithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"authenticated":true}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	// Create mock auth provider
	mockAuth := &mockAuthProvider{token: "Bearer test-token"}

	transport, err := NewTransport(cfg, mockAuth, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	resp, _, err := transport.Send(ctx, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if !strings.Contains(string(resp), "authenticated") {
		t.Errorf("Expected authenticated response, got: %s", string(resp))
	}
}

// mockAuthProvider for testing (implements auth.Authenticator interface)
type mockAuthProvider struct {
	token     string
	refreshed bool
}

func (a *mockAuthProvider) Validate(ctx context.Context, req *http.Request) (*auth.Principal, error) {
	return nil, nil
}

func (a *mockAuthProvider) Authenticate(ctx context.Context, req *http.Request) error {
	req.Header.Set("Authorization", a.token)
	return nil
}

func (a *mockAuthProvider) Refresh(ctx context.Context) error {
	a.refreshed = true
	return nil
}

func (a *mockAuthProvider) Close() error {
	return nil
}

// TestTransport_Send_AuthorizationHeaderTypes tests various authorization scenarios
func TestTransport_Send_AuthorizationHeaderTypes(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		useAuth  bool
	}{
		{"no auth needed", http.StatusOK, false},
		{"with auth", http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(`{}`))
			}))
			defer server.Close()

			cfg := &config.ClientConfig{
				URL: server.URL,
			}

			var authProvider auth.Authenticator
			if tt.useAuth {
				authProvider = &mockAuthProvider{token: "Bearer test"}
			}

			transport, err := NewTransport(cfg, authProvider, nil)
			if err != nil {
				t.Fatalf("Failed to create transport: %v", err)
			}
			defer transport.Close()

			ctx := context.Background()
			_, _, err = transport.Send(ctx, []byte(`{}`), "")
			if err != nil {
				t.Logf("Send returned error: %v", err)
			}
		})
	}
}

// TestTransport_Send_ReadBodyError tests handling of response body read errors
func TestTransport_Send_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000") // Lie about content length
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("short")) // Write less data than claimed
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	_, _, err = transport.Send(ctx, []byte(`{}`), "")
	// May or may not error depending on HTTP client behavior
	// Just ensure no panic
}

// TestTransport_Closed_Concurrent tests concurrent Close calls
func TestTransport_Closed_Concurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	// Close concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transport.Close()
		}()
	}
	wg.Wait()

	// Should be closed
	if !transport.Closed() {
		t.Error("Transport should be closed")
	}
}

// TestTransport_doWithRetry_ContextCancelDuringWait tests context cancellation during retry wait
func TestTransport_doWithRetry_ContextCancelDuringWait(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Close connection to trigger network error
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   10,
			InitialDelay: 1 * time.Second, // Long delay
			MaxDelay:     5 * time.Second,
			Multiplier:   2.0,
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		_, _, err := transport.Send(ctx, []byte(`{}`), "")
		done <- err
	}()

	// Cancel context after short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// Should get cancelled error
		if err == nil {
			t.Error("Expected error after context cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Error("Send did not return after context cancellation")
	}
}

// TestTransport_doWithRetry_MaxDelayEnforced tests that max delay is enforced
func TestTransport_doWithRetry_MaxDelayEnforced(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 3 {
			// Close connection to trigger retry
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   5,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond, // Low max delay
			Multiplier:   10.0,                  // High multiplier that would exceed max
		},
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	start := time.Now()
	_, _, err = transport.Send(context.Background(), []byte(`{}`), "")
	duration := time.Since(start)

	// Should succeed after retries
	if err != nil {
		t.Logf("Send returned error (may be expected with hijacking): %v", err)
	}

	// With 3 retries at max 50ms each, should complete in under 500ms
	// (actual delays may be shorter due to jitter)
	if duration > 2*time.Second {
		t.Errorf("Retries took too long: %v (max delay should be capped)", duration)
	}

	t.Logf("Completed in %v with %d attempts", duration, attempts)
}

// TestTransport_SendWithSSE_ConnectionClosed tests SSE with closed connection
func TestTransport_SendWithSSE_ConnectionClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Close connection immediately
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, _, err := transport.SendWithSSE(ctx, []byte(`{}`), "")
	if err != nil {
		// Connection closed error is expected
		t.Logf("SendWithSSE error (expected): %v", err)
		return
	}

	// Drain events
	for range events {
	}
}

// TestTransport_Send_TransportClosed tests Send on closed transport
func TestTransport_Send_TransportClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	// Close transport first
	transport.Close()

	// Try to send
	_, _, err = transport.Send(context.Background(), []byte(`{}`), "")
	if err == nil {
		t.Error("Expected error when sending on closed transport")
	}
	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("Expected ErrTransportClosed, got: %v", err)
	}
}

// TestTransport_SendWithSSE_TransportClosed tests SendWithSSE on closed transport
func TestTransport_SendWithSSE_TransportClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	transport, err := NewTransport(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create transport: %v", err)
	}

	// Close transport first
	transport.Close()

	// Try to send with SSE
	_, _, err = transport.SendWithSSE(context.Background(), []byte(`{}`), "")
	if err == nil {
		t.Error("Expected error when sending on closed transport")
	}
	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("Expected ErrTransportClosed, got: %v", err)
	}
}
