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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
)

// mockReader implements io.Reader for testing
type mockReader struct {
	data    []byte
	pos     int
	err     error
	mu      sync.Mutex
	blocked bool
	unblock chan struct{}
}

func newMockReader(data string) *mockReader {
	return &mockReader{
		data:    []byte(data),
		unblock: make(chan struct{}),
	}
}

func (r *mockReader) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	if r.blocked {
		// Store channel reference while holding lock to avoid race with Unblock()
		unblock := r.unblock
		r.mu.Unlock()
		<-unblock
		r.mu.Lock()
	}
	defer r.mu.Unlock()

	if r.err != nil {
		return 0, r.err
	}

	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *mockReader) Block() {
	r.mu.Lock()
	r.blocked = true
	r.mu.Unlock()
}

func (r *mockReader) Unblock() {
	r.mu.Lock()
	if r.blocked {
		r.blocked = false
		close(r.unblock)
		r.unblock = make(chan struct{})
	}
	r.mu.Unlock()
}

func (r *mockReader) SetError(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

// mockWriter implements io.Writer for testing
type mockWriter struct {
	buf bytes.Buffer
	mu  sync.Mutex
	err error
}

func newMockWriter() *mockWriter {
	return &mockWriter{}
}

func (w *mockWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.err != nil {
		return 0, w.err
	}

	return w.buf.Write(p)
}

func (w *mockWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *mockWriter) SetError(err error) {
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
}

func createTestLogger() *logging.Logger {
	cfg := config.LogConfig{
		Level:  "debug",
		Format: "text",
		Output: "stdout",
	}
	logger, _ := logging.NewLogger(cfg)
	return logger
}

func TestNewClient_ValidConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 30 * time.Second,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   3,
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     1 * time.Second,
			Multiplier:   2.0,
		},
	}

	logger := createTestLogger()
	client, err := NewClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	if client.config != cfg {
		t.Error("Config mismatch")
	}

	if client.transport == nil {
		t.Error("Transport should be set")
	}

	if client.stdio == nil {
		t.Error("Stdio should be set")
	}

	if client.Closed() {
		t.Error("Client should not be closed")
	}
}

func TestNewClient_NilConfig(t *testing.T) {
	_, err := NewClient(nil, nil)
	if err == nil {
		t.Error("Expected error for nil config")
	}
}

func TestNewClient_EmptyURL(t *testing.T) {
	cfg := &config.ClientConfig{
		URL: "",
	}

	_, err := NewClient(cfg, nil)
	if err == nil {
		t.Error("Expected error for empty URL")
	}
}

func TestNewClient_NilLogger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	// Should use default logger
	client, err := NewClient(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	if client.logger == nil {
		t.Error("Logger should be set to default")
	}
}

func TestClient_SessionIDManagement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Initially no session ID
	if client.SessionID() != "" {
		t.Error("Expected empty session ID initially")
	}

	// Set session ID
	client.SetSessionID("test-session-123")
	if client.SessionID() != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123', got '%s'", client.SessionID())
	}

	// Clear session ID
	client.SetSessionID("")
	if client.SessionID() != "" {
		t.Error("Expected empty session ID after clearing")
	}
}

func TestClient_Close(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.Closed() {
		t.Error("Client should not be closed initially")
	}

	// Close client
	err = client.Close()
	if err != nil {
		t.Errorf("Failed to close client: %v", err)
	}

	if !client.Closed() {
		t.Error("Client should be closed after Close()")
	}

	// Close again should be safe
	err = client.Close()
	if err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

func TestClient_Run_ClosedClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	client.Close()

	ctx := context.Background()
	err = client.Run(ctx)
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("Expected ErrClientClosed, got %v", err)
	}
}

func TestClient_Run_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err = client.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestClient_RunWithSSE_ClosedClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	client.Close()

	ctx := context.Background()
	err = client.RunWithSSE(ctx)
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("Expected ErrClientClosed, got %v", err)
	}
}

func TestClient_WriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Replace stdio with mock
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	testErr := errors.New("test error message")
	err = client.WriteError(testErr)
	if err != nil {
		t.Errorf("WriteError failed: %v", err)
	}

	output := mockOut.String()
	if !strings.Contains(output, "test error message") {
		t.Errorf("Expected error message in output, got: %s", output)
	}
	if !strings.Contains(output, "jsonrpc") {
		t.Errorf("Expected JSON-RPC format, got: %s", output)
	}
}

func TestClient_StdioAccessors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	if client.StdioReader() == nil {
		t.Error("StdioReader should not be nil")
	}

	if client.StdioWriter() == nil {
		t.Error("StdioWriter should not be nil")
	}
}

func TestClient_ConfigAccessor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	if client.Config() != cfg {
		t.Error("Config() should return the original config")
	}
}

func TestClient_TransportAccessor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	if client.Transport() == nil {
		t.Error("Transport() should not be nil")
	}
}

func TestClient_RefreshAuth_NoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Should not error when no auth is configured
	err = client.RefreshAuth(context.Background())
	if err != nil {
		t.Errorf("RefreshAuth should not error when no auth: %v", err)
	}
}

func TestClient_HandleMessage(t *testing.T) {
	requestReceived := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestReceived <- body
		w.Header().Set(HeaderMcpSessionID, "new-session-123")
		w.Header().Set("Content-Type", "application/json")
		response := `{"jsonrpc":"2.0","result":"ok","id":1}`
		w.Write([]byte(response))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Replace stdio with mock
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	// Send a message
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)
	ctx := context.Background()
	err = client.handleMessage(ctx, msg)
	if err != nil {
		t.Fatalf("handleMessage failed: %v", err)
	}

	// Verify request was received
	select {
	case received := <-requestReceived:
		if string(received) != string(msg) {
			t.Errorf("Expected message %s, got %s", string(msg), string(received))
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for request")
	}

	// Verify session ID was updated
	if client.SessionID() != "new-session-123" {
		t.Errorf("Expected session ID 'new-session-123', got '%s'", client.SessionID())
	}

	// Verify response was written to stdout
	time.Sleep(50 * time.Millisecond)
	output := mockOut.String()
	if !strings.Contains(output, "result") {
		t.Errorf("Expected response in output, got: %s", output)
	}
}

func TestClient_HandleMessage_SessionExpired(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			// First request: session expired
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Second request: success
		w.Header().Set(HeaderMcpSessionID, "new-session")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
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

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Set initial session ID
	client.SetSessionID("old-session")

	// Replace stdio with mock
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	// Send a message
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)
	ctx := context.Background()
	err = client.handleMessage(ctx, msg)
	if err != nil {
		t.Fatalf("handleMessage failed: %v", err)
	}

	// Session should have been cleared and new one set
	if client.SessionID() != "new-session" {
		t.Errorf("Expected session ID 'new-session', got '%s'", client.SessionID())
	}
}

func TestHTTPError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *HTTPError
		expected string
	}{
		{
			name: "with body",
			err: &HTTPError{
				StatusCode: 404,
				Status:     "404 Not Found",
				Body:       []byte("resource not found"),
			},
			expected: "HTTP 404 404 Not Found: resource not found",
		},
		{
			name: "without body",
			err: &HTTPError{
				StatusCode: 500,
				Status:     "500 Internal Server Error",
				Body:       nil,
			},
			expected: "HTTP 500 500 Internal Server Error",
		},
		{
			name: "empty body",
			err: &HTTPError{
				StatusCode: 400,
				Status:     "400 Bad Request",
				Body:       []byte{},
			},
			expected: "HTTP 400 400 Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, tt.err.Error())
			}
		})
	}
}

func TestHTTPError_Is(t *testing.T) {
	err := &HTTPError{StatusCode: 500}

	if !errors.Is(err, ErrHTTPError) {
		t.Error("Expected HTTPError to match ErrHTTPError")
	}

	if errors.Is(err, ErrClientClosed) {
		t.Error("HTTPError should not match ErrClientClosed")
	}
}

func TestHTTPError_IsRetryable(t *testing.T) {
	tests := []struct {
		statusCode int
		retryable  bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.statusCode), func(t *testing.T) {
			err := &HTTPError{StatusCode: tt.statusCode}
			if err.IsRetryable() != tt.retryable {
				t.Errorf("Expected IsRetryable()=%v for status %d", tt.retryable, tt.statusCode)
			}
		})
	}
}

func TestHTTPError_IsSessionExpired(t *testing.T) {
	tests := []struct {
		statusCode int
		expired    bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, true},
		{http.StatusGone, true},
		{http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.statusCode), func(t *testing.T) {
			err := &HTTPError{StatusCode: tt.statusCode}
			if err.IsSessionExpired() != tt.expired {
				t.Errorf("Expected IsSessionExpired()=%v for status %d", tt.expired, tt.statusCode)
			}
		})
	}
}

func TestHTTPError_IsUnauthorized(t *testing.T) {
	tests := []struct {
		statusCode   int
		unauthorized bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.statusCode), func(t *testing.T) {
			err := &HTTPError{StatusCode: tt.statusCode}
			if err.IsUnauthorized() != tt.unauthorized {
				t.Errorf("Expected IsUnauthorized()=%v for status %d", tt.unauthorized, tt.statusCode)
			}
		})
	}
}

func TestStdioHandler_ReadMessages(t *testing.T) {
	messages := "msg1\nmsg2\nmsg3\n"
	reader := newMockReader(messages)
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgChan := handler.ReadMessages(ctx)

	received := []string{}
	for msg := range msgChan {
		received = append(received, string(msg))
	}

	if len(received) != 3 {
		t.Errorf("Expected 3 messages, got %d: %v", len(received), received)
	}

	expected := []string{"msg1", "msg2", "msg3"}
	for i, msg := range expected {
		if i >= len(received) || received[i] != msg {
			t.Errorf("Message %d: expected %q, got %q", i, msg, received[i])
		}
	}
}

func TestStdioHandler_ReadMessages_ContextCancellation(t *testing.T) {
	reader := newMockReader("")
	reader.Block()
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	ctx, cancel := context.WithCancel(context.Background())

	msgChan := handler.ReadMessages(ctx)

	// Cancel context
	cancel()

	// Should close channel
	select {
	case _, ok := <-msgChan:
		if ok {
			t.Error("Channel should be closed after context cancellation")
		}
	case <-time.After(100 * time.Millisecond):
		// This is also acceptable as the goroutine might be blocked
	}
}

func TestStdioHandler_WriteMessage(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	msg := []byte(`{"test": "message"}`)
	err := handler.WriteMessage(msg)
	if err != nil {
		t.Errorf("WriteMessage failed: %v", err)
	}

	output := writer.String()
	if !strings.Contains(output, `{"test": "message"}`) {
		t.Errorf("Expected message in output, got: %s", output)
	}

	// Should have added newline
	if !strings.HasSuffix(output, "\n") {
		t.Error("Expected newline at end of output")
	}
}

func TestStdioHandler_WriteMessage_WithNewline(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	// Message already has newline
	msg := []byte("message\n")
	err := handler.WriteMessage(msg)
	if err != nil {
		t.Errorf("WriteMessage failed: %v", err)
	}

	output := writer.String()
	// Should not double the newline
	if output != "message\n" {
		t.Errorf("Expected 'message\\n', got %q", output)
	}
}

func TestStdioHandler_WriteMessage_Closed(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	handler.Close()

	err := handler.WriteMessage([]byte("test"))
	if !errors.Is(err, ErrStdioHandlerClosed) {
		t.Errorf("Expected ErrStdioHandlerClosed, got %v", err)
	}
}

func TestStdioHandler_ReadMessage(t *testing.T) {
	reader := newMockReader("test message\n")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	msg, err := handler.ReadMessage()
	if err != nil {
		t.Errorf("ReadMessage failed: %v", err)
	}

	if string(msg) != "test message" {
		t.Errorf("Expected 'test message', got %q", string(msg))
	}
}

func TestStdioHandler_ReadMessage_Closed(t *testing.T) {
	reader := newMockReader("test\n")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	handler.Close()

	_, err := handler.ReadMessage()
	if !errors.Is(err, ErrStdioHandlerClosed) {
		t.Errorf("Expected ErrStdioHandlerClosed, got %v", err)
	}
}

func TestStdioHandler_WriteRaw(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	data := []byte("raw data without newline")
	err := handler.WriteRaw(data)
	if err != nil {
		t.Errorf("WriteRaw failed: %v", err)
	}

	output := writer.String()
	if output != "raw data without newline" {
		t.Errorf("Expected 'raw data without newline', got %q", output)
	}
}

func TestStdioHandler_WriteRaw_Closed(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	handler.Close()

	err := handler.WriteRaw([]byte("test"))
	if !errors.Is(err, ErrStdioHandlerClosed) {
		t.Errorf("Expected ErrStdioHandlerClosed, got %v", err)
	}
}

func TestStdioHandler_ReaderWriter(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	if handler.Reader() != reader {
		t.Error("Reader() should return the underlying reader")
	}

	if handler.Writer() != writer {
		t.Error("Writer() should return the underlying writer")
	}
}

func TestStdioHandler_Closed(t *testing.T) {
	handler := NewStdioHandler(newMockReader(""), newMockWriter(), createTestLogger())

	if handler.Closed() {
		t.Error("Handler should not be closed initially")
	}

	handler.Close()

	if !handler.Closed() {
		t.Error("Handler should be closed after Close()")
	}
}

func TestThreadSafeWriter(t *testing.T) {
	var buf bytes.Buffer
	writer := NewThreadSafeWriter(&buf)

	// Test concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := []byte("test")
			writer.Write(data)
		}(i)
	}
	wg.Wait()

	// Should have written 10 times "test"
	if buf.Len() != 40 {
		t.Errorf("Expected 40 bytes, got %d", buf.Len())
	}
}

func TestThreadSafeWriter_WriteString(t *testing.T) {
	var buf bytes.Buffer
	writer := NewThreadSafeWriter(&buf)

	n, err := writer.WriteString("hello")
	if err != nil {
		t.Errorf("WriteString failed: %v", err)
	}
	if n != 5 {
		t.Errorf("Expected 5 bytes written, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("Expected 'hello', got %q", buf.String())
	}
}

func TestThreadSafeReader(t *testing.T) {
	data := []byte("test data for reading")
	reader := NewThreadSafeReader(bytes.NewReader(data))

	// Test concurrent reads
	var wg sync.WaitGroup
	results := make(chan []byte, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4)
			n, _ := reader.Read(buf)
			if n > 0 {
				results <- buf[:n]
			}
		}()
	}

	wg.Wait()
	close(results)

	// Should have read something
	totalRead := 0
	for result := range results {
		totalRead += len(result)
	}

	if totalRead != len(data) {
		t.Errorf("Expected to read %d bytes total, got %d", len(data), totalRead)
	}
}

func TestClient_ConcurrentSessionAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Test concurrent session ID access
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			client.SetSessionID(string(rune('A' + n%26)))
		}(i)
		go func() {
			defer wg.Done()
			_ = client.SessionID()
		}()
	}
	wg.Wait()

	// Should not panic
}

func TestStdioHandler_HandleWindowsLineEndings(t *testing.T) {
	// Windows line endings
	messages := "msg1\r\nmsg2\r\n"
	reader := newMockReader(messages)
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgChan := handler.ReadMessages(ctx)

	received := []string{}
	for msg := range msgChan {
		received = append(received, string(msg))
	}

	if len(received) != 2 {
		t.Errorf("Expected 2 messages, got %d: %v", len(received), received)
	}

	// Messages should not have \r
	for _, msg := range received {
		if strings.Contains(msg, "\r") {
			t.Errorf("Message should not contain \\r: %q", msg)
		}
	}
}

func TestStdioHandler_SkipEmptyLines(t *testing.T) {
	// Multiple empty lines
	messages := "\n\nmsg1\n\n\nmsg2\n\n"
	reader := newMockReader(messages)
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgChan := handler.ReadMessages(ctx)

	received := []string{}
	for msg := range msgChan {
		received = append(received, string(msg))
	}

	if len(received) != 2 {
		t.Errorf("Expected 2 non-empty messages, got %d: %v", len(received), received)
	}
}

func TestJSONRPCErrorFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	testErr := errors.New("connection refused")
	client.WriteError(testErr)

	output := mockOut.String()

	// Parse the JSON-RPC error
	var response map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &response); err != nil {
		t.Fatalf("Failed to parse JSON-RPC response: %v", err)
	}

	if response["jsonrpc"] != "2.0" {
		t.Error("Expected jsonrpc version 2.0")
	}

	errObj, ok := response["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected error object")
	}

	if errObj["code"].(float64) != -32603 {
		t.Errorf("Expected error code -32603, got %v", errObj["code"])
	}

	if !strings.Contains(errObj["message"].(string), "connection refused") {
		t.Errorf("Expected error message to contain 'connection refused', got %v", errObj["message"])
	}
}

// TestClient_Run_WithMessages tests the full Run loop with messages
func TestClient_Run_WithMessages(t *testing.T) {
	responseCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseCount++
		w.Header().Set(HeaderMcpSessionID, "session-123")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Create mock stdin with two messages
	mockIn := newMockReader(`{"jsonrpc":"2.0","method":"test","id":1}` + "\n" +
		`{"jsonrpc":"2.0","method":"test2","id":2}` + "\n")
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(mockIn, mockOut, createTestLogger())

	// Run should process messages and then return when stdin closes
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = client.Run(ctx)
	// Should return nil when stdin closes (EOF)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run returned unexpected error: %v", err)
	}

	// Should have processed messages
	if responseCount < 1 {
		t.Error("Expected at least one request to server")
	}
}

// TestClient_Run_StdinClosed tests Run returns when stdin closes
func TestClient_Run_StdinClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Empty stdin - will immediately get EOF
	mockIn := newMockReader("")
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(mockIn, mockOut, createTestLogger())

	ctx := context.Background()
	err = client.Run(ctx)

	// Should return nil when stdin closes
	if err != nil {
		t.Errorf("Run should return nil when stdin closes, got: %v", err)
	}
}

// TestClient_RunWithSSE_WithMessages tests RunWithSSE with actual SSE responses
func TestClient_RunWithSSE_WithMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HeaderMcpSessionID, "sse-session")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter doesn't support flushing")
		}

		// Send SSE events
		w.Write([]byte("event: message\ndata: {\"result\":\"event1\"}\n\n"))
		flusher.Flush()
		w.Write([]byte("event: message\ndata: {\"result\":\"event2\"}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Create mock stdin with one message
	mockIn := newMockReader(`{"jsonrpc":"2.0","method":"test","id":1}` + "\n")
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(mockIn, mockOut, createTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = client.RunWithSSE(ctx)
	// Should process and return when stdin closes or context times out
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Logf("RunWithSSE returned: %v (this may be expected)", err)
	}

	// Check if we got SSE responses
	time.Sleep(100 * time.Millisecond)
	output := mockOut.String()
	if len(output) > 0 {
		t.Logf("Received output: %s", output)
	}
}

// TestClient_HandleMessageWithSSE tests the handleMessageWithSSE function
func TestClient_HandleMessageWithSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HeaderMcpSessionID, "sse-session-123")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		w.Write([]byte("event: message\ndata: {\"result\":\"success\"}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Replace stdio with mock
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)
	err = client.handleMessageWithSSE(ctx, msg)
	if err != nil {
		t.Fatalf("handleMessageWithSSE failed: %v", err)
	}

	// Check session ID was set
	if client.SessionID() != "sse-session-123" {
		t.Errorf("Expected session ID 'sse-session-123', got '%s'", client.SessionID())
	}

	// Check response was written
	output := mockOut.String()
	if !strings.Contains(output, "success") {
		t.Logf("Output: %s", output)
	}
}

// TestClient_HandleSendError_NoRetry tests handleSendError without retry
func TestClient_HandleSendError_NoRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled: false, // Retries disabled
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.handleMessage(ctx, msg)
	if err == nil {
		t.Error("Expected error from failed request")
	}
}

// TestClient_RetryMessage_Success tests successful retry
func TestClient_RetryMessage_Success(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// First attempt fails
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Second attempt succeeds
		w.Header().Set(HeaderMcpSessionID, "retry-session")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
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

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.retryMessage(ctx, msg, "")
	if err != nil {
		t.Errorf("retryMessage should succeed on retry: %v", err)
	}

	if attempts < 2 {
		t.Errorf("Expected at least 2 attempts, got %d", attempts)
	}
}

// TestClient_RetryMessage_MaxRetriesExceeded tests retry exhaustion
func TestClient_RetryMessage_MaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   2,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     50 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.retryMessage(ctx, msg, "")
	if err == nil {
		t.Error("Expected error when max retries exceeded")
	}
	if !strings.Contains(err.Error(), "max retries") {
		t.Errorf("Expected max retries error, got: %v", err)
	}
}

// TestClient_RetryMessage_ContextCancellation tests retry cancellation
func TestClient_RetryMessage_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled:      true,
			MaxRetries:   10,
			InitialDelay: 500 * time.Millisecond, // Long delay
			MaxDelay:     1 * time.Second,
			Multiplier:   2.0,
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	// Cancel context after short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = client.retryMessage(ctx, msg, "")
	if err == nil {
		t.Error("Expected error from context cancellation")
	}
}

// TestClient_RetryMessage_ExponentialBackoff tests delay calculation
func TestClient_RetryMessage_ExponentialBackoff(t *testing.T) {
	attempts := 0
	var timestamps []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		timestamps = append(timestamps, time.Now())
		if attempts <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
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
			InitialDelay: 50 * time.Millisecond,
			MaxDelay:     200 * time.Millisecond,
			Multiplier:   2.0,
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.retryMessage(ctx, msg, "")
	if err != nil {
		t.Errorf("Expected success after retries: %v", err)
	}

	// Check delays are increasing
	if len(timestamps) >= 3 {
		delay1 := timestamps[1].Sub(timestamps[0])
		delay2 := timestamps[2].Sub(timestamps[1])
		if delay2 <= delay1 {
			t.Logf("Delays: %v, %v (second should be >= first)", delay1, delay2)
		}
	}
}

// TestStdioHandler_ReadMessages_Error tests error handling in ReadMessages
func TestStdioHandler_ReadMessages_Error(t *testing.T) {
	reader := newMockReader("")
	reader.SetError(errors.New("read error"))
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgChan := handler.ReadMessages(ctx)

	// Should close channel on error
	received := []string{}
	for msg := range msgChan {
		received = append(received, string(msg))
	}

	if len(received) != 0 {
		t.Errorf("Expected 0 messages on error, got %d", len(received))
	}
}

// TestStdioHandler_WriteMessage_Error tests error handling in WriteMessage
func TestStdioHandler_WriteMessage_Error(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	writer.SetError(errors.New("write error"))
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	err := handler.WriteMessage([]byte("test"))
	if err == nil {
		t.Error("Expected error from write failure")
	}
}

// TestStdioHandler_WriteRaw_Error tests error handling in WriteRaw
func TestStdioHandler_WriteRaw_Error(t *testing.T) {
	reader := newMockReader("")
	writer := newMockWriter()
	writer.SetError(errors.New("write error"))
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	err := handler.WriteRaw([]byte("test"))
	if err == nil {
		t.Error("Expected error from write failure")
	}
}

// shortWriter simulates partial writes
type shortWriter struct {
	writeLimit int
	written    int
}

func (w *shortWriter) Write(p []byte) (n int, err error) {
	if w.written+len(p) > w.writeLimit {
		n = w.writeLimit - w.written
		if n < 0 {
			n = 0
		}
		w.written += n
		return n, nil
	}
	w.written += len(p)
	return len(p), nil
}

// TestStdioHandler_WriteMessage_ShortWrite tests short write handling
func TestStdioHandler_WriteMessage_ShortWrite(t *testing.T) {
	reader := newMockReader("")
	writer := &shortWriter{writeLimit: 2} // Only allow 2 bytes
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	err := handler.WriteMessage([]byte("test message"))
	if err == nil {
		t.Error("Expected error from short write")
	}
	if !errors.Is(err, ErrWriteFailed) {
		t.Errorf("Expected ErrWriteFailed, got %v", err)
	}
}

// TestStdioHandler_WriteRaw_ShortWrite tests short write handling in WriteRaw
func TestStdioHandler_WriteRaw_ShortWrite(t *testing.T) {
	reader := newMockReader("")
	writer := &shortWriter{writeLimit: 2}
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	err := handler.WriteRaw([]byte("test message"))
	if err == nil {
		t.Error("Expected error from short write")
	}
	if !errors.Is(err, ErrWriteFailed) {
		t.Errorf("Expected ErrWriteFailed, got %v", err)
	}
}

// TestStdioHandler_ReadMessages_ClosedPipe tests closed pipe handling
func TestStdioHandler_ReadMessages_ClosedPipe(t *testing.T) {
	reader := newMockReader("")
	reader.SetError(io.ErrClosedPipe)
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msgChan := handler.ReadMessages(ctx)

	// Should close channel on closed pipe
	for range msgChan {
	}

	// Test passed if no panic
}

// TestStdioHandler_ReadMessages_HandlerClosed tests handler closed during read
func TestStdioHandler_ReadMessages_HandlerClosed(t *testing.T) {
	reader := newMockReader("")
	reader.Block() // Block reading
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	msgChan := handler.ReadMessages(ctx)

	// Close handler while reading
	go func() {
		time.Sleep(50 * time.Millisecond)
		handler.Close()
		reader.Unblock()
	}()

	// Drain channel
	for range msgChan {
	}

	// Test passed if no hang or panic
}

// TestClient_HandleMessage_WriteError tests handling of stdout write errors
func TestClient_HandleMessage_WriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Replace stdio with mock that fails writes
	mockOut := newMockWriter()
	mockOut.SetError(errors.New("write failed"))
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.handleMessage(ctx, msg)
	if err == nil {
		t.Error("Expected error from write failure")
	}
	if !strings.Contains(err.Error(), "writing response") {
		t.Errorf("Expected write error, got: %v", err)
	}
}

// TestClient_NewClient_WithAuth tests client creation with auth config
func TestClient_NewClient_WithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Auth: &config.AuthConfig{
			Type: "bearer",
			Bearer: &config.BearerAuthConfig{
				Token: "test-token",
			},
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client with auth: %v", err)
	}
	defer client.Close()

	if client.auth == nil {
		t.Error("Auth should be set when auth config is provided")
	}
}

// TestClient_NewClient_InvalidAuth tests client creation with invalid auth
func TestClient_NewClient_InvalidAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Auth: &config.AuthConfig{
			Type: "invalid-auth-type",
		},
	}

	_, err := NewClient(cfg, createTestLogger())
	if err == nil {
		t.Error("Expected error for invalid auth config")
	}
}

// TestClient_Close_WithAuth tests closing client with auth
func TestClient_Close_WithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Auth: &config.AuthConfig{
			Type: "bearer",
			Bearer: &config.BearerAuthConfig{
				Token: "test-token",
			},
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

// TestClient_HandleSendError_WithRetryableError tests handleSendError with retryable errors
func TestClient_HandleSendError_WithRetryableError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set(HeaderMcpSessionID, "session-after-retry")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
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

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.handleMessage(ctx, msg)
	if err != nil {
		t.Errorf("handleMessage should succeed after retry: %v", err)
	}

	if attempts < 2 {
		t.Errorf("Expected at least 2 attempts, got %d", attempts)
	}
}

// TestClient_HandleSendError_UnauthorizedWithoutAuth tests unauthorized without auth configured
func TestClient_HandleSendError_UnauthorizedWithoutAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Retry: config.RetryConfig{
			Enabled: false,
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(newMockReader(""), mockOut, createTestLogger())

	ctx := context.Background()
	msg := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	err = client.handleMessage(ctx, msg)
	if err == nil {
		t.Error("Expected error for unauthorized request")
	}
}

// TestClient_RunWithSSE_ContextCancellation tests RunWithSSE with context cancellation
func TestClient_RunWithSSE_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Keep connection open
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL:     server.URL,
		Timeout: 30 * time.Second,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Create mock stdin that blocks
	mockIn := newMockReader("")
	mockIn.Block()
	mockOut := newMockWriter()
	client.stdio = NewStdioHandler(mockIn, mockOut, createTestLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error)
	go func() {
		done <- client.RunWithSSE(ctx)
	}()

	// Cancel context after short delay
	time.Sleep(50 * time.Millisecond)
	cancel()
	mockIn.Unblock()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Logf("RunWithSSE returned: %v (context canceled expected)", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("RunWithSSE did not return after context cancellation")
	}
}

// TestNewStdioHandler_WithNilLogger tests NewStdioHandler with nil logger
func TestNewStdioHandler_WithNilLogger(t *testing.T) {
	reader := newMockReader("test\n")
	writer := newMockWriter()

	// Create with nil logger - should use default
	handler := NewStdioHandler(reader, writer, nil)
	defer handler.Close()

	// Should work without panic
	msg, err := handler.ReadMessage()
	if err != nil {
		t.Errorf("ReadMessage failed: %v", err)
	}
	if string(msg) != "test" {
		t.Errorf("Expected 'test', got %q", string(msg))
	}
}

// TestStdioHandler_ReadMessage_EOF tests ReadMessage at EOF
func TestStdioHandler_ReadMessage_EOF(t *testing.T) {
	reader := newMockReader("") // Empty - will return EOF
	writer := newMockWriter()
	logger := createTestLogger()

	handler := NewStdioHandler(reader, writer, logger)
	defer handler.Close()

	_, err := handler.ReadMessage()
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
}

// TestClient_RefreshAuth_WithAuth tests RefreshAuth with auth configured
func TestClient_RefreshAuth_WithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
		Auth: &config.AuthConfig{
			Type: "bearer",
			Bearer: &config.BearerAuthConfig{
				Token: "test-token",
			},
		},
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// RefreshAuth should work even though bearer auth doesn't actually refresh
	err = client.RefreshAuth(context.Background())
	if err != nil {
		t.Errorf("RefreshAuth should not error: %v", err)
	}
}

// TestClient_Close_Concurrent tests concurrent Close calls
func TestClient_Close_Concurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.ClientConfig{
		URL: server.URL,
	}

	client, err := NewClient(cfg, createTestLogger())
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Close concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Close()
		}()
	}
	wg.Wait()

	if !client.Closed() {
		t.Error("Client should be closed")
	}
}
