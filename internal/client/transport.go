/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/auth"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// HTTP headers used for MCP communication.
const (
	// HeaderMcpSessionID is the header for MCP session identification.
	HeaderMcpSessionID = "Mcp-Session-Id"

	// HeaderContentType is the standard Content-Type header.
	HeaderContentType = "Content-Type"

	// HeaderAccept is the standard Accept header.
	HeaderAccept = "Accept"

	// ContentTypeJSON is the MIME type for JSON content.
	ContentTypeJSON = "application/json"

	// ContentTypeSSE is the MIME type for Server-Sent Events.
	ContentTypeSSE = "text/event-stream"
)

// Transport errors.
var (
	// ErrTransportClosed indicates the transport has been closed.
	ErrTransportClosed = errors.New("transport is closed")

	// ErrHTTPError indicates an HTTP error response was received.
	ErrHTTPError = errors.New("http error")

	// ErrRetryExhausted indicates all retry attempts have failed.
	ErrRetryExhausted = errors.New("retry attempts exhausted")

	// ErrRequestCancelled indicates the request was cancelled.
	ErrRequestCancelled = errors.New("request cancelled")
)

// Transport handles HTTP communication with the remote MCP server.
// It manages connection pooling, authentication, and retry logic.
type Transport struct {
	// HTTP client
	httpClient *http.Client

	// Configuration
	serverURL string
	timeout   time.Duration
	retry     config.RetryConfig

	// Authentication
	auth auth.Authenticator

	// State management
	closed   bool
	closedMu sync.RWMutex
}

// NewTransport creates a new HTTP transport for communicating with the MCP server.
func NewTransport(cfg *config.ClientConfig, authenticator auth.Authenticator, tlsConfig interface{}) (*Transport, error) {
	if cfg == nil {
		return nil, errors.New("client config is required")
	}

	if cfg.URL == "" {
		return nil, errors.New("server URL is required")
	}

	// Create HTTP transport with connection pooling
	httpTransport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConns,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		DisableCompression:  false,
		ForceAttemptHTTP2:   true,
	}

	// Configure TLS if provided
	if tlsConfig != nil {
		if tc, ok := tlsConfig.(*tls.Config); ok {
			httpTransport.TLSClientConfig = tc
		}
	}

	// Set default values
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns == 0 {
		maxIdleConns = 100
	}

	idleConnTimeout := cfg.IdleConnTimeout
	if idleConnTimeout == 0 {
		idleConnTimeout = 90 * time.Second
	}

	httpTransport.MaxIdleConns = maxIdleConns
	httpTransport.MaxIdleConnsPerHost = maxIdleConns
	httpTransport.IdleConnTimeout = idleConnTimeout

	// Create HTTP client
	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   timeout,
	}

	return &Transport{
		httpClient: httpClient,
		serverURL:  cfg.URL,
		timeout:    timeout,
		retry:      cfg.Retry,
		auth:       authenticator,
	}, nil
}

// Send sends a message to the server and returns the response.
// It handles authentication, session management, and error handling.
// Returns the response body, the session ID from the response, and any error.
func (t *Transport) Send(ctx context.Context, msg []byte, sessionID string) ([]byte, string, error) {
	t.closedMu.RLock()
	if t.closed {
		t.closedMu.RUnlock()
		return nil, "", ErrTransportClosed
	}
	t.closedMu.RUnlock()

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.serverURL, bytes.NewReader(msg))
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	req.Header.Set(HeaderContentType, ContentTypeJSON)
	req.Header.Set(HeaderAccept, ContentTypeJSON)

	// Add session ID if present
	if sessionID != "" {
		req.Header.Set(HeaderMcpSessionID, sessionID)
	}

	// Apply authentication
	if t.auth != nil {
		if err := t.auth.Authenticate(ctx, req); err != nil {
			return nil, "", fmt.Errorf("authenticating request: %w", err)
		}
	}

	// Send request with retry logic
	resp, err := t.doWithRetry(ctx, req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// Extract session ID from response
	respSessionID := resp.Header.Get(HeaderMcpSessionID)

	// Handle HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, respSessionID, newHTTPError(resp.StatusCode, resp.Status, body)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, respSessionID, fmt.Errorf("reading response body: %w", err)
	}

	return body, respSessionID, nil
}

// SendWithSSE sends a message and returns a channel for receiving SSE events.
// This is used for streaming responses from the server.
// Returns an event channel, the session ID, and any error.
func (t *Transport) SendWithSSE(ctx context.Context, msg []byte, sessionID string) (<-chan []byte, string, error) {
	t.closedMu.RLock()
	if t.closed {
		t.closedMu.RUnlock()
		return nil, "", ErrTransportClosed
	}
	t.closedMu.RUnlock()

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.serverURL, bytes.NewReader(msg))
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	// Set headers for SSE
	req.Header.Set(HeaderContentType, ContentTypeJSON)
	req.Header.Set(HeaderAccept, ContentTypeSSE)

	// Add session ID if present
	if sessionID != "" {
		req.Header.Set(HeaderMcpSessionID, sessionID)
	}

	// Apply authentication
	if t.auth != nil {
		if err := t.auth.Authenticate(ctx, req); err != nil {
			return nil, "", fmt.Errorf("authenticating request: %w", err)
		}
	}

	// Send request (no retry for SSE as it's a streaming response)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, "", ErrRequestCancelled
		}
		return nil, "", fmt.Errorf("sending SSE request: %w", err)
	}

	// Extract session ID from response
	respSessionID := resp.Header.Get(HeaderMcpSessionID)

	// Handle HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, respSessionID, newHTTPError(resp.StatusCode, resp.Status, body)
	}

	// Create SSE reader and event channel
	events := make(chan []byte, 10)
	reader := NewSSEReader(resp.Body)

	// Start goroutine to read SSE events
	go func() {
		defer close(events)
		defer resp.Body.Close()
		defer reader.Close()

		for {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Read next message event
			data, err := reader.ReadMessage()
			if err != nil {
				if err == io.EOF || errors.Is(err, context.Canceled) {
					return
				}
				// Log error and continue or return based on severity
				return
			}

			if len(data) > 0 {
				select {
				case events <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, respSessionID, nil
}

// doWithRetry executes the request with retry logic.
func (t *Transport) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastErr error
	maxAttempts := 1

	if t.retry.Enabled {
		maxAttempts = t.retry.MaxRetries + 1
	}

	delay := t.retry.InitialDelay

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Clone request body for retry (if body exists)
		var bodyClone io.ReadCloser
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("reading request body: %w", err)
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			bodyClone = io.NopCloser(bytes.NewReader(body))
		}

		resp, err := t.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Check if we should retry
		if !t.shouldRetry(err, resp) || attempt == maxAttempts {
			break
		}

		// Restore body for next attempt
		if bodyClone != nil {
			req.Body = bodyClone
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * t.retry.Multiplier)
		if delay > t.retry.MaxDelay {
			delay = t.retry.MaxDelay
		}
	}

	if lastErr != nil {
		if errors.Is(lastErr, context.Canceled) {
			return nil, ErrRequestCancelled
		}
		return nil, fmt.Errorf("request failed after %d attempts: %w", maxAttempts, lastErr)
	}

	return nil, ErrRetryExhausted
}

// shouldRetry determines if a request should be retried based on the error or response.
func (t *Transport) shouldRetry(err error, resp *http.Response) bool {
	// Always retry on network errors
	if err != nil {
		// Don't retry on context cancellation
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}

	// Retry on certain HTTP status codes
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusRequestTimeout,
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return true
		}
	}

	return false
}

// Close shuts down the transport and releases resources.
func (t *Transport) Close() error {
	t.closedMu.Lock()
	defer t.closedMu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	// Close idle connections
	if transport, ok := t.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	return nil
}

// Closed returns true if the transport has been closed.
func (t *Transport) Closed() bool {
	t.closedMu.RLock()
	defer t.closedMu.RUnlock()
	return t.closed
}

// HTTPError represents an HTTP error response from the server.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       []byte
}

// newHTTPError creates a new HTTPError.
func newHTTPError(statusCode int, status string, body []byte) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		Status:     status,
		Body:       body,
	}
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if len(e.Body) > 0 {
		return fmt.Sprintf("HTTP %d %s: %s", e.StatusCode, e.Status, string(e.Body))
	}
	return fmt.Sprintf("HTTP %d %s", e.StatusCode, e.Status)
}

// Is implements errors.Is support.
func (e *HTTPError) Is(target error) bool {
	return target == ErrHTTPError
}

// IsRetryable returns true if this error indicates a retryable condition.
func (e *HTTPError) IsRetryable() bool {
	switch e.StatusCode {
	case http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// IsSessionExpired returns true if this error indicates the session has expired.
func (e *HTTPError) IsSessionExpired() bool {
	// Session expiration is typically indicated by:
	// - 404 Not Found (session not found)
	// - 410 Gone (session explicitly expired)
	return e.StatusCode == http.StatusNotFound || e.StatusCode == http.StatusGone
}

// IsUnauthorized returns true if this error indicates authentication failure.
func (e *HTTPError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}
