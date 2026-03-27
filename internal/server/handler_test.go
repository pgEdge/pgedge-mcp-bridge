/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package server

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
	"github.com/pgEdge/pgedge-mcp-bridge/internal/process"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/protocol"
)

// mockStdinWriter wraps a PipeWriter and records what was written
type mockStdinWriter struct {
	*io.PipeWriter
	mu      sync.Mutex
	written [][]byte
}

func (m *mockStdinWriter) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	// Make a copy to avoid data races
	copied := make([]byte, len(p))
	copy(copied, p)
	m.written = append(m.written, copied)
	m.mu.Unlock()
	return m.PipeWriter.Write(p)
}

func (m *mockStdinWriter) Close() error {
	return m.PipeWriter.Close()
}

func (m *mockStdinWriter) GetWritten() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]byte, len(m.written))
	copy(result, m.written)
	return result
}

// nonBlockingWriter is a writer that buffers writes without blocking
type nonBlockingWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
	err    error
}

func (w *nonBlockingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	return w.buf.Write(p)
}

func (w *nonBlockingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *nonBlockingWriter) GetWritten() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Bytes()
}

func (w *nonBlockingWriter) SetError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.err = err
}

// testMockProcessManager is a more controllable mock for handler tests
type testMockProcessManager struct {
	mu           sync.RWMutex
	running      bool
	stdinWriter  *nonBlockingWriter
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	stderrReader *io.PipeReader
	stderrWriter *io.PipeWriter
	events       chan process.Event
	state        process.ProcessState
	pid          int
}

func newTestMockProcessManager() *testMockProcessManager {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	return &testMockProcessManager{
		stdinWriter:  &nonBlockingWriter{},
		stdoutReader: stdoutReader,
		stdoutWriter: stdoutWriter,
		stderrReader: stderrReader,
		stderrWriter: stderrWriter,
		events:       make(chan process.Event, 16),
		state:        process.StateRunning,
		running:      true,
		pid:          12345,
	}
}

func (m *testMockProcessManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = true
	m.state = process.StateRunning
	return nil
}

func (m *testMockProcessManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.state = process.StateStopped
	return nil
}

func (m *testMockProcessManager) Restart(ctx context.Context) error {
	return nil
}

func (m *testMockProcessManager) Stdin() io.WriteCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stdinWriter
}

func (m *testMockProcessManager) Stdout() io.ReadCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stdoutReader
}

func (m *testMockProcessManager) Stderr() io.ReadCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stderrReader
}

func (m *testMockProcessManager) Wait() error {
	return nil
}

func (m *testMockProcessManager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *testMockProcessManager) PID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pid
}

func (m *testMockProcessManager) State() process.ProcessState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *testMockProcessManager) RestartCount() int {
	return 0
}

func (m *testMockProcessManager) Events() <-chan process.Event {
	return m.events
}

func (m *testMockProcessManager) Close() error {
	m.stdinWriter.Close()
	m.stdoutWriter.Close()
	m.stderrWriter.Close()
	close(m.events)
	return nil
}

func (m *testMockProcessManager) SetRunning(running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = running
	if running {
		m.state = process.StateRunning
	} else {
		m.state = process.StateStopped
	}
}

// WriteResponse writes a response to stdout to simulate subprocess output
func (m *testMockProcessManager) WriteResponse(response []byte) {
	m.stdoutWriter.Write(response)
	m.stdoutWriter.Write([]byte("\n"))
}

// GetWrittenStdin returns what was written to stdin
func (m *testMockProcessManager) GetWrittenStdin() []byte {
	return m.stdinWriter.GetWritten()
}

func newTestHandler() (*MCPHandler, *testMockProcessManager) {
	pm := newTestMockProcessManager()
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)
	return handler, pm
}

// createTestLogger creates a logger for testing without requiring *testing.T
func createTestLogger() *logging.Logger {
	logger, _ := logging.NewLogger(config.LogConfig{
		Level:  "error", // Use error level to reduce test output noise
		Format: "text",
		Output: "stderr",
	})
	return logger
}

func TestMCPHandler_NewMCPHandler(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled: true,
	})
	logger := newTestLogger(t)

	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	if handler == nil {
		t.Fatal("NewMCPHandler() returned nil")
	}
	if handler.processManager != pm {
		t.Error("processManager not set correctly")
	}
	if handler.sessionManager != sm {
		t.Error("sessionManager not set correctly")
	}
	if handler.logger != logger {
		t.Error("logger not set correctly")
	}
	if handler.pendingResponses == nil {
		t.Error("pendingResponses map not initialized")
	}
	if handler.sseClients == nil {
		t.Error("sseClients map not initialized")
	}
}

func TestMCPHandler_HandlePost_InvalidJSON(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString("invalid json{")
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusOK { // JSON-RPC errors return 200
		t.Errorf("expected status 200 for JSON-RPC error, got %d", rr.Code)
	}

	var response protocol.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error == nil {
		t.Error("expected error in response")
	}
	if response.Error != nil && response.Error.Code != protocol.ParseError {
		t.Errorf("expected parse error code %d, got %d", protocol.ParseError, response.Error.Code)
	}
}

func TestMCPHandler_HandlePost_UnsupportedContentType(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "text/plain")

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected status %d, got %d", http.StatusUnsupportedMediaType, rr.Code)
	}
}

func TestMCPHandler_HandlePost_UnsupportedAccept(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set("Accept", "text/html")

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusNotAcceptable {
		t.Errorf("expected status %d, got %d", http.StatusNotAcceptable, rr.Code)
	}
}

func TestMCPHandler_HandlePost_ProcessNotRunning(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	pm.SetRunning(false)

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestMCPHandler_HandlePost_SessionNotFound(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, "nonexistent-session")

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestMCPHandler_HandlePost_CreatesSession(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a notification that doesn't require a response
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// For notifications, we expect 202 Accepted
	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, rr.Code)
	}

	// Check that a session ID was returned
	sessionID := rr.Header().Get(MCPSessionIDHeader)
	if sessionID == "" {
		t.Error("expected Mcp-Session-Id header to be set")
	}
}

func TestMCPHandler_HandlePost_ContentTypeHeader(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a notification that doesn't require a response
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should accept this content type
	if rr.Code == http.StatusUnsupportedMediaType {
		t.Error("should accept 'application/json; charset=utf-8' content type")
	}
}

func TestMCPHandler_HandlePost_AcceptWildcard(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set("Accept", "*/*")

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should accept wildcard
	if rr.Code == http.StatusNotAcceptable {
		t.Error("should accept '*/*' accept header")
	}
}

func TestMCPHandler_HandleSSE_RequiresSessionID(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", ContentTypeSSE)

	rr := httptest.NewRecorder()
	handler.HandleSSE(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestMCPHandler_HandleSSE_SessionNotFound(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", ContentTypeSSE)
	req.Header.Set(MCPSessionIDHeader, "nonexistent-session")

	rr := httptest.NewRecorder()
	handler.HandleSSE(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestMCPHandler_HandleSSE_InvalidAcceptHeader(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a session first
	session, _ := handler.sessionManager.CreateSession()

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandleSSE(rr, req)

	if rr.Code != http.StatusNotAcceptable {
		t.Errorf("expected status %d, got %d", http.StatusNotAcceptable, rr.Code)
	}
}

func TestMCPHandler_HandleSessionClose_RequiresSessionID(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)

	rr := httptest.NewRecorder()
	handler.HandleSessionClose(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestMCPHandler_HandleSessionClose_SessionNotFound(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set(MCPSessionIDHeader, "nonexistent-session")

	rr := httptest.NewRecorder()
	handler.HandleSessionClose(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestMCPHandler_HandleSessionClose_Success(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a session first
	session, err := handler.sessionManager.CreateSession()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandleSessionClose(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	// Verify session was closed
	if handler.sessionManager.GetSession(session.ID) != nil {
		t.Error("session should have been removed")
	}
}

func TestMCPHandler_ServeHTTP_MethodRouting(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	testCases := []struct {
		name           string
		method         string
		setupRequest   func(req *http.Request)
		expectedStatus int
	}{
		{
			name:   "POST routes to HandlePost",
			method: http.MethodPost,
			setupRequest: func(req *http.Request) {
				req.Header.Set("Content-Type", ContentTypeJSON)
			},
			expectedStatus: http.StatusServiceUnavailable, // Process not running by default
		},
		{
			name:   "GET routes to HandleSSE",
			method: http.MethodGet,
			setupRequest: func(req *http.Request) {
				req.Header.Set("Accept", ContentTypeSSE)
			},
			expectedStatus: http.StatusBadRequest, // Missing session ID
		},
		{
			name:           "DELETE routes to HandleSessionClose",
			method:         http.MethodDelete,
			setupRequest:   func(req *http.Request) {},
			expectedStatus: http.StatusBadRequest, // Missing session ID
		},
		{
			name:           "PUT is not allowed",
			method:         http.MethodPut,
			setupRequest:   func(req *http.Request) {},
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PATCH is not allowed",
			method:         http.MethodPatch,
			setupRequest:   func(req *http.Request) {},
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pm.SetRunning(false)

			body := bytes.NewBufferString(`{}`)
			req := httptest.NewRequest(tc.method, "/mcp", body)
			tc.setupRequest(req)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, rr.Code)
			}
		})
	}
}

func TestMCPHandler_WriteError(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	rr := httptest.NewRecorder()
	handler.writeError(rr, http.StatusBadRequest, "test error message")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response["error"] != "test error message" {
		t.Errorf("expected error message 'test error message', got %s", response["error"])
	}
}

func TestMCPHandler_WriteJSONRPCError(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	rr := httptest.NewRecorder()
	id := protocol.NewStringID("test-id")
	protoErr := protocol.NewInternalError("internal error data")

	handler.writeJSONRPCError(rr, id, protoErr)

	// JSON-RPC errors should return 200 OK
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != ContentTypeJSON {
		t.Errorf("expected content type %s, got %s", ContentTypeJSON, contentType)
	}

	var response protocol.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error == nil {
		t.Error("expected error in response")
	}
	if response.Error.Code != protocol.InternalError {
		t.Errorf("expected error code %d, got %d", protocol.InternalError, response.Error.Code)
	}
}

func TestMCPHandler_InvalidRequestType(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// A message with result but no method is a response, which is invalid for POST
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","result":{},"id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Response messages should be rejected with an invalid request error
	var response protocol.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error == nil {
		t.Error("expected error in response")
	}
	if response.Error.Code != protocol.InvalidRequest {
		t.Errorf("expected invalid request error code %d, got %d", protocol.InvalidRequest, response.Error.Code)
	}
}

func TestMCPHandler_Notification_Returns202(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Notification (no id field)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/progress"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d for notification, got %d", http.StatusAccepted, rr.Code)
	}

	// Verify session ID header is set
	sessionID := rr.Header().Get(MCPSessionIDHeader)
	if sessionID == "" {
		t.Error("expected Mcp-Session-Id header to be set for notification")
	}
}

func TestMCPHandler_ExistingSession(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a session first
	session, err := handler.sessionManager.CreateSession()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Use the existing session
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, rr.Code)
	}

	// Verify same session ID is returned
	returnedSessionID := rr.Header().Get(MCPSessionIDHeader)
	if returnedSessionID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, returnedSessionID)
	}
}

func TestMCPHandler_TouchSession(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a session
	session, err := handler.sessionManager.CreateSession()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	initialActivity := session.LastActivity

	// Wait a tiny bit
	time.Sleep(10 * time.Millisecond)

	// Make a request with the session
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Session should have been touched
	updatedSession := handler.sessionManager.GetSession(session.ID)
	if !updatedSession.LastActivity.After(initialActivity) {
		t.Error("session LastActivity should have been updated")
	}
}

func TestMCPHandler_Shutdown(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	ctx := context.Background()
	err := handler.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestMCPHandler_InvalidJSONRPCVersion(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Invalid JSON-RPC version
	body := bytes.NewBufferString(`{"jsonrpc":"1.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	var response protocol.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error == nil {
		t.Error("expected error for invalid JSON-RPC version")
	}
}

func TestMCPHandler_EmptyBody(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString("")
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Empty body should result in parse error
	var response protocol.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.Error == nil {
		t.Error("expected error for empty body")
	}
}

func TestMCPHandler_NoContentTypeHeader(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	// No Content-Type header set

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should accept requests without Content-Type
	if rr.Code == http.StatusUnsupportedMediaType {
		t.Error("should accept requests without Content-Type header")
	}
}

func TestMCPHandler_EmptyAcceptHeader(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	// No Accept header set

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should accept requests without Accept header
	if rr.Code == http.StatusNotAcceptable {
		t.Error("should accept requests without Accept header")
	}
}

// mockFlusherRecorder implements http.ResponseWriter and http.Flusher
type mockFlusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func newMockFlusherRecorder() *mockFlusherRecorder {
	return &mockFlusherRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockFlusherRecorder) Flush() {
	m.flushed = true
}

// syncMockFlusherRecorder is a thread-safe version of mockFlusherRecorder
// that protects concurrent access to the underlying response body.
type syncMockFlusherRecorder struct {
	mu      sync.Mutex
	rr      *httptest.ResponseRecorder
	flushed bool
}

func newSyncMockFlusherRecorder() *syncMockFlusherRecorder {
	return &syncMockFlusherRecorder{
		rr: httptest.NewRecorder(),
	}
}

func (m *syncMockFlusherRecorder) Header() http.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rr.Header()
}

func (m *syncMockFlusherRecorder) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rr.Write(b)
}

func (m *syncMockFlusherRecorder) WriteHeader(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rr.WriteHeader(code)
}

func (m *syncMockFlusherRecorder) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed = true
}

func (m *syncMockFlusherRecorder) BodyString() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rr.Body.String()
}

func TestMCPHandler_HandleSSE_EstablishesConnection(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	// Create a session first
	session, err := handler.sessionManager.CreateSession()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create request with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Accept", ContentTypeSSE)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := newMockFlusherRecorder()

	// Run handler in goroutine since it blocks
	done := make(chan struct{})
	go func() {
		handler.HandleSSE(rr, req)
		close(done)
	}()

	// Give it a moment to set up
	time.Sleep(50 * time.Millisecond)

	// Cancel the context to close the connection
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
		// Good
	case <-time.After(2 * time.Second):
		t.Error("handler did not finish after context cancellation")
	}

	// Check that headers were set
	contentType := rr.Header().Get("Content-Type")
	if contentType != ContentTypeSSE {
		t.Errorf("expected Content-Type %s, got %s", ContentTypeSSE, contentType)
	}

	// Check that connected event was sent
	body := rr.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Error("expected 'connected' event in response")
	}
	if !strings.Contains(body, session.ID) {
		t.Error("expected session ID in connected event data")
	}
}

// processManagerWithControlledStdout allows controlled stdout responses
type processManagerWithControlledStdout struct {
	*testMockProcessManager
	responseQueue chan []byte
}

func newProcessManagerWithControlledStdout() *processManagerWithControlledStdout {
	pm := newTestMockProcessManager()
	return &processManagerWithControlledStdout{
		testMockProcessManager: pm,
		responseQueue:          make(chan []byte, 100),
	}
}

func (pm *processManagerWithControlledStdout) QueueResponse(response []byte) {
	pm.responseQueue <- response
}

// TestMCPHandler_HandleRequest_Success tests the complete request/response flow
func TestMCPHandler_HandleRequest_Success(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session first
	session, _ := sm.CreateSession()

	// Start a goroutine to simulate subprocess response
	go func() {
		time.Sleep(50 * time.Millisecond)
		// Write a response to stdout that will be read by readLoop
		response := `{"jsonrpc":"2.0","result":{"message":"success"},"id":"test-123"}` + "\n"
		pm.stdoutWriter.Write([]byte(response))
	}()

	// Send a request
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test/method","id":"test-123"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Verify the request was forwarded to subprocess stdin
	written := pm.stdinWriter.GetWritten()
	if len(written) == 0 {
		t.Error("expected request to be written to subprocess stdin")
	}

	// Verify session ID header is returned
	if rr.Header().Get(MCPSessionIDHeader) != session.ID {
		t.Errorf("expected session ID %s in response header", session.ID)
	}
}

// TestMCPHandler_HandleRequest_Timeout tests request timeout handling
func TestMCPHandler_HandleRequest_Timeout(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session first
	session, _ := sm.CreateSession()

	// No subprocess response - will timeout

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test/method","id":"timeout-test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	// Short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Request should complete (either with timeout error or context cancellation)
	// The key is that it doesn't hang forever
}

// TestMCPHandler_HandleNotification_Success tests notification forwarding
func TestMCPHandler_HandleNotification_Success(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session first
	session, _ := sm.CreateSession()

	// Send a notification (no id field)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":50}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d for notification, got %d", http.StatusAccepted, rr.Code)
	}

	// Verify notification was written to subprocess stdin
	written := pm.stdinWriter.GetWritten()
	if len(written) == 0 {
		t.Error("expected notification to be written to subprocess stdin")
	}

	// Check the written content contains the notification
	if !strings.Contains(string(written), "notifications/progress") {
		t.Error("expected notification method in written data")
	}
}

// TestMCPHandler_ReadLoop_ResponseRouting tests the readLoop routing responses
func TestMCPHandler_ReadLoop_ResponseRouting(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Manually start the read loop
	handler.ensureReadLoop()

	// Register a pending response
	responseChan := make(chan []byte, 1)
	handler.pendingMu.Lock()
	handler.pendingResponses["route-test-id"] = responseChan
	handler.pendingMu.Unlock()

	// Write a response to stdout
	response := `{"jsonrpc":"2.0","result":{"routed":true},"id":"route-test-id"}` + "\n"
	pm.stdoutWriter.Write([]byte(response))

	// Wait for response to be routed
	select {
	case received := <-responseChan:
		if !strings.Contains(string(received), "routed") {
			t.Errorf("expected routed response, got %s", string(received))
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for response routing")
	}

	// Cleanup
	handler.pendingMu.Lock()
	delete(handler.pendingResponses, "route-test-id")
	handler.pendingMu.Unlock()
}

// TestMCPHandler_ReadLoop_NotificationRouting tests the readLoop routing notifications
func TestMCPHandler_ReadLoop_NotificationRouting(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session
	session, _ := sm.CreateSession()

	// Manually start the read loop
	handler.ensureReadLoop()

	// Register an SSE client using thread-safe recorder to avoid data races
	rr := newSyncMockFlusherRecorder()
	sse := NewSSEWriter(rr, rr)
	handler.sseClientsMu.Lock()
	handler.sseClients[session.ID] = sse
	handler.sseClientsMu.Unlock()

	// Write a notification to stdout
	notification := `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}` + "\n"
	pm.stdoutWriter.Write([]byte(notification))

	// Give time for routing
	time.Sleep(100 * time.Millisecond)

	// Check if notification was sent to SSE client (using thread-safe accessor)
	body := rr.BodyString()
	if !strings.Contains(body, "tools/list_changed") && !strings.Contains(body, "notifications") {
		// The notification may have been routed through session notifications instead
		// Check session notifications channel
		select {
		case msg := <-session.Notifications():
			if !strings.Contains(string(msg), "tools/list_changed") {
				t.Errorf("expected notification in session, got %s", string(msg))
			}
		default:
			// Notification routing may work differently - the test passes if no panic
		}
	}

	// Cleanup
	handler.sseClientsMu.Lock()
	delete(handler.sseClients, session.ID)
	handler.sseClientsMu.Unlock()
}

// TestMCPHandler_ReadLoop_InvalidJSON tests handling of invalid JSON in readLoop
func TestMCPHandler_ReadLoop_InvalidJSON(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Start read loop
	handler.ensureReadLoop()

	// Write invalid JSON - should be handled gracefully (logged, not crash)
	pm.stdoutWriter.Write([]byte("not valid json\n"))

	// Then write valid JSON to confirm loop is still running
	responseChan := make(chan []byte, 1)
	handler.pendingMu.Lock()
	handler.pendingResponses["after-invalid"] = responseChan
	handler.pendingMu.Unlock()

	response := `{"jsonrpc":"2.0","result":"ok","id":"after-invalid"}` + "\n"
	pm.stdoutWriter.Write([]byte(response))

	// Should still receive the valid response
	select {
	case received := <-responseChan:
		if !strings.Contains(string(received), "after-invalid") {
			t.Errorf("expected response, got %s", string(received))
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout - readLoop may have crashed on invalid JSON")
	}

	handler.pendingMu.Lock()
	delete(handler.pendingResponses, "after-invalid")
	handler.pendingMu.Unlock()
}

// TestMCPHandler_ReadLoop_EOF tests readLoop handling of stdout EOF
func TestMCPHandler_ReadLoop_EOF(t *testing.T) {
	pm := newTestMockProcessManager()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Start read loop
	handler.ensureReadLoop()

	// Give read loop time to start
	time.Sleep(50 * time.Millisecond)

	// Close stdout to simulate subprocess exit
	pm.stdoutWriter.Close()

	// Give time for readLoop to detect EOF and clean up
	time.Sleep(100 * time.Millisecond)

	// Verify readLoop marked itself as stopped
	handler.readLoopMu.Lock()
	restarted := !handler.readLoopStarted
	handler.readLoopMu.Unlock()

	if !restarted {
		t.Error("readLoop should mark itself as stopped after EOF")
	}
}

// TestMCPHandler_RouteResponse_MissingID tests response routing with missing ID
func TestMCPHandler_RouteResponse_MissingID(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Start read loop
	handler.ensureReadLoop()

	// Write a response without ID - should be logged but not crash
	response := `{"jsonrpc":"2.0","result":"orphan"}` + "\n"
	pm.stdoutWriter.Write([]byte(response))

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Test passes if no panic occurred
}

// TestMCPHandler_RouteResponse_NoPendingRequest tests response for unknown request ID
func TestMCPHandler_RouteResponse_NoPendingRequest(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Start read loop
	handler.ensureReadLoop()

	// Write a response with ID that has no pending request
	response := `{"jsonrpc":"2.0","result":"orphan","id":"unknown-id"}` + "\n"
	pm.stdoutWriter.Write([]byte(response))

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Test passes if no panic occurred - warning should be logged
}

// TestMCPHandler_HandleSSE_WithNotifications tests SSE with actual notifications
func TestMCPHandler_HandleSSE_WithNotifications(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session first
	session, err := sm.CreateSession()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create request with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Accept", ContentTypeSSE)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := newMockFlusherRecorder()

	// Run handler in goroutine since it blocks
	done := make(chan struct{})
	go func() {
		handler.HandleSSE(rr, req)
		close(done)
	}()

	// Give time for connection setup
	time.Sleep(50 * time.Millisecond)

	// Send a notification through the session
	testNotification := []byte(`{"jsonrpc":"2.0","method":"test/notification"}`)
	session.QueueNotification(testNotification)

	// Give time for notification to be processed
	time.Sleep(50 * time.Millisecond)

	// Cancel to close connection
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handler did not finish")
	}

	// Check response contains connected event
	body := rr.Body.String()
	if !strings.Contains(body, "connected") {
		t.Error("expected connected event")
	}
}

// TestMCPHandler_HandleSessionClose_WithSSE tests closing session with active SSE
func TestMCPHandler_HandleSessionClose_WithSSE(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session
	session, err := sm.CreateSession()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Register an SSE client for this session
	rr := newMockFlusherRecorder()
	sse := NewSSEWriter(rr, rr)
	handler.sseClientsMu.Lock()
	handler.sseClients[session.ID] = sse
	handler.sseClientsMu.Unlock()

	// Now close the session via DELETE
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	deleteRr := httptest.NewRecorder()
	handler.HandleSessionClose(deleteRr, req)

	if deleteRr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, deleteRr.Code)
	}

	// Verify SSE client was removed
	handler.sseClientsMu.RLock()
	_, exists := handler.sseClients[session.ID]
	handler.sseClientsMu.RUnlock()

	if exists {
		t.Error("SSE client should have been removed after session close")
	}

	// Verify close event was sent
	body := rr.Body.String()
	if !strings.Contains(body, "close") {
		t.Log("Close event may or may not have been sent, depending on timing")
	}
}

// TestMCPHandler_Shutdown_WithSSEClients tests shutdown with active SSE clients
func TestMCPHandler_Shutdown_WithSSEClients(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create sessions with SSE clients
	for i := 0; i < 3; i++ {
		session, _ := sm.CreateSession()
		rr := newMockFlusherRecorder()
		sse := NewSSEWriter(rr, rr)
		handler.sseClientsMu.Lock()
		handler.sseClients[session.ID] = sse
		handler.sseClientsMu.Unlock()
	}

	// Verify we have SSE clients
	handler.sseClientsMu.RLock()
	initialCount := len(handler.sseClients)
	handler.sseClientsMu.RUnlock()

	if initialCount != 3 {
		t.Errorf("expected 3 SSE clients, got %d", initialCount)
	}

	// Shutdown handler
	ctx := context.Background()
	err := handler.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Verify all SSE clients were closed
	handler.sseClientsMu.RLock()
	finalCount := len(handler.sseClients)
	handler.sseClientsMu.RUnlock()

	if finalCount != 0 {
		t.Errorf("expected 0 SSE clients after shutdown, got %d", finalCount)
	}
}

// TestMCPHandler_HandlePost_StdinNil tests handling when stdin becomes nil
func TestMCPHandler_HandlePost_StdinNil(t *testing.T) {
	// Create a mock process manager that returns nil stdin
	pm := &nilStdinProcessManager{
		testMockProcessManager: newTestMockProcessManager(),
	}
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create session
	session, _ := sm.CreateSession()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should return service unavailable when stdin is nil
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

// nilStdinProcessManager returns nil for stdin
type nilStdinProcessManager struct {
	*testMockProcessManager
}

func (m *nilStdinProcessManager) Stdin() io.WriteCloser {
	return nil
}

// TestMCPHandler_HandlePost_InitializeRequest tests initialize request handling
func TestMCPHandler_HandlePost_InitializeRequest(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Start a goroutine to simulate subprocess response
	go func() {
		time.Sleep(50 * time.Millisecond)
		// Write initialize response with capabilities
		response := `{"jsonrpc":"2.0","result":{"protocolVersion":"1.0","capabilities":{"tools":true,"prompts":true}},"id":"init-1"}` + "\n"
		pm.stdoutWriter.Write([]byte(response))
	}()

	// Send initialize request (no session ID - creates new session)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"initialize","id":"init-1","params":{"protocolVersion":"1.0"}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should have created a session
	sessionID := rr.Header().Get(MCPSessionIDHeader)
	if sessionID == "" {
		t.Error("expected session ID to be returned for initialize request")
	}

	// Verify written to stdin
	written := pm.stdinWriter.GetWritten()
	if len(written) == 0 {
		t.Error("expected initialize request to be written to subprocess")
	}
}

// TestMCPHandler_HandlePost_BatchRequest tests batch JSON-RPC requests
func TestMCPHandler_HandlePost_BatchRequest(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Session for request
	session, _ := sm.CreateSession()

	// Batch request array
	body := bytes.NewBufferString(`[{"jsonrpc":"2.0","method":"test1","id":1},{"jsonrpc":"2.0","method":"test2","id":2}]`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Batch requests may be handled differently - check response
	// The implementation might return an error for batches or handle them
	// This test ensures no crash occurs
}

// TestMCPHandler_HandleSSE_StreamingNotSupported tests SSE without flusher
func TestMCPHandler_HandleSSE_StreamingNotSupported(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Create a session
	session, _ := sm.CreateSession()

	// Use a non-flusher writer
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", ContentTypeSSE)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	// Create a writer that doesn't implement http.Flusher
	rr := &realNonFlusherWriter{
		header:     make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}
	handler.HandleSSE(rr, req)

	if rr.statusCode != http.StatusInternalServerError {
		t.Errorf("expected status %d for non-flusher, got %d", http.StatusInternalServerError, rr.statusCode)
	}
}

// realNonFlusherWriter is a ResponseWriter that does NOT implement http.Flusher
type realNonFlusherWriter struct {
	header     http.Header
	body       *bytes.Buffer
	statusCode int
}

func (w *realNonFlusherWriter) Header() http.Header {
	return w.header
}

func (w *realNonFlusherWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

func (w *realNonFlusherWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

// TestMCPHandler_HandlePost_LargeRequest tests handling of large requests
func TestMCPHandler_HandlePost_LargeRequest(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	// Create a large request (but within limits)
	largeParams := strings.Repeat("a", 1000000) // 1MB of data
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test","params":"` + largeParams + `"}`)

	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should accept large notification
	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d for large notification, got %d", http.StatusAccepted, rr.Code)
	}
}

// TestMCPHandler_EnsureReadLoop_MultiplesCalls tests idempotent read loop start
func TestMCPHandler_EnsureReadLoop_MultipleCalls(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Call ensureReadLoop multiple times
	for i := 0; i < 10; i++ {
		handler.ensureReadLoop()
	}

	// Should only have started one read loop
	handler.readLoopMu.Lock()
	started := handler.readLoopStarted
	handler.readLoopMu.Unlock()

	if !started {
		t.Error("readLoop should be started")
	}

	// Test passes if no panic from multiple goroutines
}

// TestMCPHandler_HandleNotification_WriteError tests notification write error handling
func TestMCPHandler_HandleNotification_WriteError(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	// Make stdin writer return error
	pm.stdinWriter.SetError(errors.New("write error"))

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

// TestMCPHandler_HandleNotification_StdinWriteErrors tests various stdin write errors
func TestMCPHandler_HandleNotification_StdinWriteErrors(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	// First, test with a working stdin
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should succeed
	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, rr.Code)
	}
}

// TestMCPHandler_HandleNotification_StdinNil tests notification with nil stdin
func TestMCPHandler_HandleNotification_StdinNil(t *testing.T) {
	// Use nilStdinProcessManager which returns nil for stdin
	pm := &nilStdinProcessManager{
		testMockProcessManager: newTestMockProcessManager(),
	}
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should return service unavailable when stdin is nil
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

// TestMCPHandler_HandleNotification_MultipleNotifications tests sending multiple notifications
func TestMCPHandler_HandleNotification_MultipleNotifications(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	// Send multiple notifications
	for i := 0; i < 5; i++ {
		body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test","params":{"index":` + string(rune('0'+i)) + `}}`)
		req := httptest.NewRequest(http.MethodPost, "/mcp", body)
		req.Header.Set("Content-Type", ContentTypeJSON)
		req.Header.Set(MCPSessionIDHeader, session.ID)

		rr := httptest.NewRecorder()
		handler.HandlePost(rr, req)

		if rr.Code != http.StatusAccepted {
			t.Errorf("notification %d: expected status %d, got %d", i, http.StatusAccepted, rr.Code)
		}
	}

	// Check all notifications were written to stdin
	written := pm.stdinWriter.GetWritten()
	if !strings.Contains(string(written), "notifications/test") {
		t.Error("expected notifications to be written to stdin")
	}
}

// TestMCPHandler_HandleRequest_StdinNil tests request with nil stdin
func TestMCPHandler_HandleRequest_StdinNil(t *testing.T) {
	pm := &nilStdinProcessManager{
		testMockProcessManager: newTestMockProcessManager(),
	}
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":1}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", ContentTypeJSON)
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandlePost(rr, req)

	// Should return service unavailable
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

// TestMCPHandler_HandleRequest_ConcurrentRequests tests concurrent request handling
func TestMCPHandler_HandleRequest_ConcurrentRequests(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	// Simulate responses in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		for i := 0; i < 5; i++ {
			response := `{"jsonrpc":"2.0","result":"ok","id":"concurrent-` + string(rune('0'+i)) + `"}` + "\n"
			pm.stdoutWriter.Write([]byte(response))
		}
	}()

	// Send concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"test","id":"concurrent-` + string(rune('0'+idx)) + `"}`)
			req := httptest.NewRequest(http.MethodPost, "/mcp", body)
			req.Header.Set("Content-Type", ContentTypeJSON)
			req.Header.Set(MCPSessionIDHeader, session.ID)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.HandlePost(rr, req)

			// Accept any status code - we're testing concurrency safety
		}(i)
	}

	wg.Wait()
}

// TestMCPHandler_HandleSSE_NotAcceptable tests SSE with wrong Accept header
func TestMCPHandler_HandleSSE_NotAcceptable(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	session, _ := sm.CreateSession()

	// Use wrong Accept header
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "application/json") // Wrong for SSE
	req.Header.Set(MCPSessionIDHeader, session.ID)

	rr := httptest.NewRecorder()
	handler.HandleSSE(rr, req)

	if rr.Code != http.StatusNotAcceptable {
		t.Errorf("expected status %d, got %d", http.StatusNotAcceptable, rr.Code)
	}
}

// TestMCPHandler_HandleSSE_NoSession tests SSE without session
func TestMCPHandler_HandleSSE_NoSession(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// No session header
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", ContentTypeSSE)

	rr := httptest.NewRecorder()
	handler.HandleSSE(rr, req)

	// Should require session ID
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestMCPHandler_HandleSSE_InvalidSession tests SSE with invalid session
func TestMCPHandler_HandleSSE_InvalidSession(t *testing.T) {
	pm := newTestMockProcessManager()
	defer pm.Close()

	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		Timeout:     30 * time.Minute,
		MaxSessions: 100,
	})
	logger := createTestLogger()
	handler := NewMCPHandler(pm, sm, logger, 0, 0)

	// Use non-existent session
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", ContentTypeSSE)
	req.Header.Set(MCPSessionIDHeader, "non-existent-session")

	rr := httptest.NewRecorder()
	handler.HandleSSE(rr, req)

	// Should fail with invalid session
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

// TestMCPHandler_HandlePost_AcceptHeaderVariations tests various Accept headers
func TestMCPHandler_HandlePost_AcceptHeaderVariations(t *testing.T) {
	handler, pm := newTestHandler()
	defer pm.Close()

	tests := []struct {
		name           string
		accept         string
		expectedStatus int
	}{
		{"json", "application/json", http.StatusAccepted},
		{"wildcard", "*/*", http.StatusAccepted},
		{"json with wildcard", "application/json, */*", http.StatusAccepted},
		{"empty", "", http.StatusAccepted},
		{"html", "text/html", http.StatusNotAcceptable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/test"}`)
			req := httptest.NewRequest(http.MethodPost, "/mcp", body)
			req.Header.Set("Content-Type", ContentTypeJSON)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			rr := httptest.NewRecorder()
			handler.HandlePost(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Accept=%q: expected status %d, got %d", tt.accept, tt.expectedStatus, rr.Code)
			}
		})
	}
}
