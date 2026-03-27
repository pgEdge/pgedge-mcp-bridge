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
)

// mockProcessManager implements process.Manager for testing
type mockProcessManager struct {
	mu           sync.RWMutex
	running      bool
	stdinWriter  *io.PipeWriter
	stdinReader  *io.PipeReader
	stdoutWriter *io.PipeWriter
	stdoutReader *io.PipeReader
	stderrWriter *io.PipeWriter
	stderrReader *io.PipeReader
	events       chan process.Event
	startErr     error
	stopErr      error
	state        process.ProcessState
	pid          int
	restartCount int
	closed       bool
	startCalled  bool
	stopCalled   bool
}

func newMockProcessManager() *mockProcessManager {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	return &mockProcessManager{
		stdinWriter:  stdinWriter,
		stdinReader:  stdinReader,
		stdoutWriter: stdoutWriter,
		stdoutReader: stdoutReader,
		stderrWriter: stderrWriter,
		stderrReader: stderrReader,
		events:       make(chan process.Event, 16),
		state:        process.StateStopped,
	}
}

func (m *mockProcessManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = true
	if m.startErr != nil {
		return m.startErr
	}
	m.running = true
	m.state = process.StateRunning
	m.pid = 12345
	return nil
}

func (m *mockProcessManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
	if m.stopErr != nil {
		return m.stopErr
	}
	m.running = false
	m.state = process.StateStopped
	return nil
}

func (m *mockProcessManager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}

func (m *mockProcessManager) Stdin() io.WriteCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stdinWriter
}

func (m *mockProcessManager) Stdout() io.ReadCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stdoutReader
}

func (m *mockProcessManager) Stderr() io.ReadCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stderrReader
}

func (m *mockProcessManager) Wait() error {
	return nil
}

func (m *mockProcessManager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *mockProcessManager) PID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pid
}

func (m *mockProcessManager) State() process.ProcessState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *mockProcessManager) RestartCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restartCount
}

func (m *mockProcessManager) Events() <-chan process.Event {
	return m.events
}

func (m *mockProcessManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.stdinWriter.Close()
	m.stdoutWriter.Close()
	m.stderrWriter.Close()
	close(m.events)
	return nil
}

func (m *mockProcessManager) SetRunning(running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = running
	if running {
		m.state = process.StateRunning
	} else {
		m.state = process.StateStopped
	}
}

// Helper to create a test logger
func newTestLogger(t *testing.T) *logging.Logger {
	t.Helper()
	logger, err := logging.NewLogger(config.LogConfig{
		Level:  "debug",
		Format: "text",
		Output: "stderr",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return logger
}

// Helper to create a basic server config
func newTestServerConfig() *config.ServerConfig {
	return &config.ServerConfig{
		Listen:       "127.0.0.1:0",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		MCPServer: config.MCPServerConfig{
			Command:                 "echo",
			Args:                    []string{"test"},
			GracefulShutdownTimeout: 5 * time.Second,
		},
		Session: config.SessionConfig{
			Enabled:         true,
			Timeout:         30 * time.Minute,
			MaxSessions:     100,
			CleanupInterval: 5 * time.Minute,
		},
	}
}

func TestNewServer_ValidConfig(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if server == nil {
		t.Fatal("NewServer() returned nil server")
	}

	// Verify components are initialized
	if server.sessionManager == nil {
		t.Error("session manager not initialized")
	}
	if server.processManager == nil {
		t.Error("process manager not initialized")
	}
	if server.mcpHandler == nil {
		t.Error("MCP handler not initialized")
	}
	if server.httpServer == nil {
		t.Error("HTTP server not initialized")
	}
	if server.cfg != cfg {
		t.Error("config not stored correctly")
	}
	if server.logger != logger {
		t.Error("logger not stored correctly")
	}
}

func TestNewServer_NilConfig(t *testing.T) {
	logger := newTestLogger(t)

	server, err := NewServer(nil, logger)
	if err == nil {
		t.Error("NewServer() expected error for nil config")
	}
	if server != nil {
		t.Error("NewServer() expected nil server for nil config")
	}
	if err.Error() != "server config is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewServer_NilLogger(t *testing.T) {
	cfg := newTestServerConfig()

	server, err := NewServer(cfg, nil)
	if err == nil {
		t.Error("NewServer() expected error for nil logger")
	}
	if server != nil {
		t.Error("NewServer() expected nil server for nil logger")
	}
	if err.Error() != "logger is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewServer_WithTLSConfig(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.TLS = &config.TLSConfig{
		Enabled:    true,
		CertFile:   "/nonexistent/cert.pem",
		KeyFile:    "/nonexistent/key.pem",
		MinVersion: "TLS1.2",
	}
	logger := newTestLogger(t)

	// This should fail because the cert files don't exist
	server, err := NewServer(cfg, logger)
	if err == nil {
		t.Error("NewServer() expected error for invalid TLS config")
	}
	if server != nil {
		t.Error("NewServer() expected nil server for invalid TLS config")
	}
}

func TestNewServer_WithAuthConfig(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Auth = &config.AuthConfig{
		Type: "bearer",
		Bearer: &config.BearerAuthConfig{
			ValidTokens: []string{"test-token-123"},
		},
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if server.auth == nil {
		t.Error("authenticator not initialized")
	}
	if server.authMiddleware == nil {
		t.Error("auth middleware not initialized")
	}
}

func TestNewServer_WithCORSConfig(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.CORS = &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if server.corsHandler == nil {
		t.Error("CORS handler not initialized")
	}
	if !server.corsHandler.IsEnabled() {
		t.Error("CORS should be enabled")
	}
}

func TestNewServer_WithNoAuth(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Auth = &config.AuthConfig{
		Type: "none",
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Auth middleware should not be set for "none" type
	if server.authMiddleware != nil {
		t.Error("auth middleware should not be set for 'none' auth type")
	}
}

func TestServer_HandleHealth(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("handler returned wrong content type: got %v want %v", contentType, "application/json")
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("unexpected status: got %v want healthy", response["status"])
	}
}

func TestServer_HandleReady_NotReady(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace with mock process manager that's not running
	mockPM := newMockProcessManager()
	mockPM.SetRunning(false)
	server.processManager = mockPM

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()

	server.handleReady(rr, req)

	if status := rr.Code; status != http.StatusServiceUnavailable {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusServiceUnavailable)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if response["status"] != "not_ready" {
		t.Errorf("unexpected status: got %v want not_ready", response["status"])
	}
	if response["reason"] != "mcp_subprocess_not_running" {
		t.Errorf("unexpected reason: got %v want mcp_subprocess_not_running", response["reason"])
	}
}

func TestServer_HandleReady_Ready(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace with mock process manager that's running
	mockPM := newMockProcessManager()
	mockPM.SetRunning(true)
	server.processManager = mockPM
	server.running.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()

	server.handleReady(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if response["status"] != "ready" {
		t.Errorf("unexpected status: got %v want ready", response["status"])
	}
}

func TestServer_HandleReady_ServerNotRunning(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Process is running but server is not
	mockPM := newMockProcessManager()
	mockPM.SetRunning(true)
	server.processManager = mockPM
	server.running.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()

	server.handleReady(rr, req)

	if status := rr.Code; status != http.StatusServiceUnavailable {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusServiceUnavailable)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if response["reason"] != "server_not_running" {
		t.Errorf("unexpected reason: got %v want server_not_running", response["reason"])
	}
}

func TestServer_BuildRouter(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	router := server.buildRouter()
	if router == nil {
		t.Fatal("buildRouter() returned nil")
	}

	// Test that routes are registered by making requests
	testCases := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{
			name:           "GET health endpoint",
			method:         http.MethodGet,
			path:           "/health",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "GET ready endpoint (not ready)",
			method:         http.MethodGet,
			path:           "/ready",
			expectedStatus: http.StatusServiceUnavailable, // Process not running
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Errorf("got status %d, want %d", rr.Code, tc.expectedStatus)
			}
		})
	}
}

func TestServer_RunningState(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Initial state should be not running
	if server.Running() {
		t.Error("server should not be running initially")
	}

	// Set running state
	server.running.Store(true)
	if !server.Running() {
		t.Error("server should be running after setting state")
	}

	// Reset state
	server.running.Store(false)
	if server.Running() {
		t.Error("server should not be running after resetting state")
	}
}

func TestServer_Addr(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Addr should be empty when not running
	addr := server.Addr()
	if addr != "" {
		t.Errorf("Addr() should be empty when not running, got %q", addr)
	}
}

func TestServer_Done(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	done := server.Done()
	if done == nil {
		t.Error("Done() should return a non-nil channel")
	}

	// Channel should not be closed initially
	select {
	case <-done:
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}
}

func TestServer_SessionManager(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	sm := server.SessionManager()
	if sm == nil {
		t.Error("SessionManager() should return non-nil session manager")
	}
}

func TestServer_ProcessManager(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	pm := server.ProcessManager()
	if pm == nil {
		t.Error("ProcessManager() should return non-nil process manager")
	}
}

func TestServer_WithInvalidAuthConfig(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Auth = &config.AuthConfig{
		Type: "invalid_type",
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err == nil {
		t.Error("NewServer() expected error for invalid auth type")
	}
	if server != nil {
		t.Error("NewServer() expected nil server for invalid auth type")
	}
}

func TestServer_ConfigTimeouts(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.ReadTimeout = 45 * time.Second
	cfg.WriteTimeout = 50 * time.Second
	cfg.IdleTimeout = 120 * time.Second

	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if server.httpServer.ReadTimeout != 45*time.Second {
		t.Errorf("ReadTimeout = %v, want %v", server.httpServer.ReadTimeout, 45*time.Second)
	}
	if server.httpServer.WriteTimeout != 50*time.Second {
		t.Errorf("WriteTimeout = %v, want %v", server.httpServer.WriteTimeout, 50*time.Second)
	}
	if server.httpServer.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want %v", server.httpServer.IdleTimeout, 120*time.Second)
	}
}

func TestServer_CORSDisabled(t *testing.T) {
	cfg := newTestServerConfig()
	// No CORS config or disabled
	cfg.CORS = &config.CORSConfig{
		Enabled: false,
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if server.corsHandler == nil {
		t.Error("CORS handler should still be created even if disabled")
	}
	if server.corsHandler.IsEnabled() {
		t.Error("CORS should not be enabled")
	}
}

// TestServer_StartAndStop tests the server lifecycle
func TestServer_StartAndStop(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"    // Use port 0 to get a random available port
	cfg.MCPServer.Command = "cat" // Simple command that reads stdin
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	if !server.Running() {
		t.Error("server should be running after Start")
	}

	// Verify address is set
	addr := server.Addr()
	if addr == "" {
		t.Error("server address should be set while running")
	}

	// Stop the server
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err = server.Stop(stopCtx)
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify server stopped
	if server.Running() {
		t.Error("server should not be running after Stop")
	}

	// Verify done channel is closed
	select {
	case <-server.Done():
		// Good
	default:
		t.Error("Done channel should be closed after Stop")
	}
}

// TestServer_StartAlreadyRunning tests starting an already running server
func TestServer_StartAlreadyRunning(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start first instance
	go func() {
		server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Try to start again - should fail
	err = server.Start(ctx)
	if err == nil || err.Error() != "server is already running" {
		t.Errorf("expected 'server is already running' error, got: %v", err)
	}

	// Clean up
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.Stop(stopCtx)
}

// TestServer_StopIdempotent tests that Stop can be called multiple times
func TestServer_StopIdempotent(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop multiple times - should not panic
	stopCtx := context.Background()
	err1 := server.Stop(stopCtx)
	err2 := server.Stop(stopCtx)
	err3 := server.Stop(stopCtx)

	// Only first stop should do anything, subsequent calls are no-ops
	if err1 != nil {
		t.Errorf("first Stop() error = %v", err1)
	}
	if err2 != nil {
		t.Errorf("second Stop() should not error, got = %v", err2)
	}
	if err3 != nil {
		t.Errorf("third Stop() should not error, got = %v", err3)
	}
}

// TestServer_StopWithNilContext tests Stop with nil context
func TestServer_StopWithNilContext(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop with background context
	err = server.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

// TestServer_ProcessManagerStartError tests handling of process manager start failure
func TestServer_ProcessManagerStartError(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock that fails to start
	mockPM := newMockProcessManager()
	mockPM.startErr = errors.New("failed to start subprocess")
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()
	err = server.Start(ctx)

	if err == nil {
		t.Error("expected error when process manager fails to start")
	}
	if !strings.Contains(err.Error(), "starting MCP subprocess") {
		t.Errorf("expected error about subprocess, got: %v", err)
	}
}

// TestServer_ProcessManagerStopError tests handling of process manager stop failure
func TestServer_ProcessManagerStopError(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock that fails to stop
	mockPM := newMockProcessManager()
	mockPM.stopErr = errors.New("failed to stop subprocess")
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop should return error but still complete
	stopCtx := context.Background()
	err = server.Stop(stopCtx)

	if err == nil {
		t.Error("expected error when process manager fails to stop")
	}
	if !strings.Contains(err.Error(), "stopping MCP subprocess") {
		t.Errorf("expected error about subprocess stop, got: %v", err)
	}

	// Server should still be stopped
	if server.Running() {
		t.Error("server should be stopped even if process manager failed")
	}
}

// TestServer_MonitorProcessEvents tests process event monitoring
func TestServer_MonitorProcessEvents(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Send various events to the process manager
	mockPM.events <- process.Event{Type: process.EventStarted, PID: 1234}
	mockPM.events <- process.Event{Type: process.EventStopped, ExitCode: 0}
	mockPM.events <- process.Event{Type: process.EventFailed, ExitCode: 1, Error: errors.New("test error")}
	mockPM.events <- process.Event{Type: process.EventRestarting}

	// Give time for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Server should still be running (no max restarts reached)
	if !server.Running() {
		t.Error("server should still be running")
	}

	// Clean up
	stopCtx := context.Background()
	server.Stop(stopCtx)
}

// TestServer_MonitorProcessEvents_MaxRestartsReached tests max restarts handling
func TestServer_MonitorProcessEvents_MaxRestartsReached(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Send max restarts reached event
	mockPM.events <- process.Event{Type: process.EventMaxRestartsReached}

	// Wait for server to shut down
	select {
	case <-server.Done():
		// Good - server shut down
	case <-time.After(5 * time.Second):
		t.Error("server should shut down after max restarts reached")
	}
}

// TestServer_AddrWhenNotRunning tests Addr() when server is not running
func TestServer_AddrWhenNotRunning(t *testing.T) {
	cfg := newTestServerConfig()
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Without starting, address should be empty
	if server.Addr() != "" {
		t.Errorf("Addr() should be empty when not running, got %q", server.Addr())
	}
}

// TestServer_DoneChannelBehavior tests Done channel behavior
func TestServer_DoneChannelBehavior(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	// Done channel should not be closed initially
	select {
	case <-server.Done():
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}

	ctx := context.Background()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Done channel should still not be closed while running
	select {
	case <-server.Done():
		t.Error("Done channel should not be closed while running")
	default:
		// Expected
	}

	// Stop server
	stopCtx := context.Background()
	server.Stop(stopCtx)

	// Done channel should now be closed
	select {
	case <-server.Done():
		// Expected
	default:
		t.Error("Done channel should be closed after Stop")
	}
}

// TestServer_RestartDoneChannel tests done channel reset on restart
func TestServer_RestartDoneChannel(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	// First server instance
	server1, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM1 := newMockProcessManager()
	server1.processManager = mockPM1
	server1.mcpHandler = NewMCPHandler(mockPM1, server1.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start and stop first server
	go func() {
		server1.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)
	server1.Stop(context.Background())

	// Wait for done
	<-server1.Done()

	// Verify first server is fully stopped
	if server1.Running() {
		t.Error("first server should be stopped")
	}

	// Create a new server instance for restart scenario
	server2, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() for restart error = %v", err)
	}

	// Replace process manager with mock
	mockPM2 := newMockProcessManager()
	server2.processManager = mockPM2
	server2.mcpHandler = NewMCPHandler(mockPM2, server2.sessionManager, logger, 0, 0)

	// Start second server
	go func() {
		server2.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Server should be running
	if !server2.Running() {
		t.Error("second server should be running")
	}

	// Done channel should not be closed while running
	select {
	case <-server2.Done():
		t.Error("Done channel should not be closed while running")
	default:
		// Expected
	}

	// Clean up
	server2.Stop(context.Background())
}

// TestServer_BuildRouterWithAuth tests router building with authentication
func TestServer_BuildRouterWithAuth(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Auth = &config.AuthConfig{
		Type: "bearer",
		Bearer: &config.BearerAuthConfig{
			ValidTokens: []string{"test-token"},
		},
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Verify auth middleware is set
	if server.authMiddleware == nil {
		t.Error("auth middleware should be set")
	}

	router := server.buildRouter()
	if router == nil {
		t.Fatal("buildRouter() returned nil")
	}

	// Health endpoint should work without auth (skipped)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("health endpoint should work, got status %d", rr.Code)
	}
}

// TestServer_BuildRouterWithCORS tests router building with CORS
func TestServer_BuildRouterWithCORS(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.CORS = &config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST"},
	}
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Verify CORS handler is set and enabled
	if server.corsHandler == nil {
		t.Error("CORS handler should be set")
	}
	if !server.corsHandler.IsEnabled() {
		t.Error("CORS should be enabled")
	}
}

// TestServer_HTTPEndpointsIntegration tests HTTP endpoint integration
func TestServer_HTTPEndpointsIntegration(t *testing.T) {
	cfg := newTestServerConfig()
	cfg.Listen = "127.0.0.1:0"
	logger := newTestLogger(t)

	server, err := NewServer(cfg, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Replace process manager with mock
	mockPM := newMockProcessManager()
	mockPM.SetRunning(true)
	server.processManager = mockPM
	server.mcpHandler = NewMCPHandler(mockPM, server.sessionManager, logger, 0, 0)

	ctx := context.Background()

	// Start server
	go func() {
		server.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Get server address
	addr := server.Addr()
	if addr == "" {
		t.Fatal("server address not set")
	}

	// Test health endpoint via HTTP
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Test ready endpoint via HTTP
	server.running.Store(true)
	resp, err = http.Get("http://" + addr + "/ready")
	if err != nil {
		t.Fatalf("ready request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("ready endpoint status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Clean up
	server.Stop(context.Background())
}
