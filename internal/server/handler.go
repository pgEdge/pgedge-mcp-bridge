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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/process"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/protocol"
)

const (
	// MCPSessionIDHeader is the HTTP header used to track MCP sessions.
	MCPSessionIDHeader = "Mcp-Session-Id"

	// ContentTypeJSON is the MIME type for JSON content.
	ContentTypeJSON = "application/json"

	// ContentTypeSSE is the MIME type for Server-Sent Events.
	ContentTypeSSE = "text/event-stream"

	// defaultReadTimeout is the default timeout for reading from the subprocess.
	defaultReadTimeout = 30 * time.Second

	// defaultSSEKeepaliveInterval is the default interval for SSE keepalive pings.
	defaultSSEKeepaliveInterval = 30 * time.Second
)

// MCPHandler handles MCP HTTP requests by forwarding them to the MCP subprocess.
// It supports both synchronous request/response and Server-Sent Events for
// server-initiated notifications.
type MCPHandler struct {
	// processManager manages the MCP subprocess.
	processManager process.Manager

	// sessionManager manages MCP sessions.
	sessionManager *SessionManager

	// logger is used for structured logging.
	logger *logging.Logger

	// readTimeout is the timeout for reading a response from the subprocess.
	readTimeout time.Duration

	// sseKeepaliveInterval is the interval for SSE keepalive pings.
	sseKeepaliveInterval time.Duration

	// mu protects concurrent access to the subprocess I/O.
	mu sync.Mutex

	// pendingResponses tracks pending responses by request ID.
	pendingResponses map[string]chan []byte
	pendingMu        sync.Mutex

	// sseClients tracks active SSE connections by session ID.
	sseClients   map[string]*SSEWriter
	sseClientsMu sync.RWMutex

	// readLoopStarted indicates if the read loop has been started.
	readLoopStarted bool
	readLoopMu      sync.Mutex
}

// NewMCPHandler creates a new MCP handler with the given process manager,
// session manager, logger, and configurable timeouts.
// If readTimeout or sseKeepaliveInterval are zero, defaults are used.
func NewMCPHandler(pm process.Manager, sm *SessionManager, logger *logging.Logger, readTimeout, sseKeepaliveInterval time.Duration) *MCPHandler {
	if readTimeout == 0 {
		readTimeout = defaultReadTimeout
	}
	if sseKeepaliveInterval == 0 {
		sseKeepaliveInterval = defaultSSEKeepaliveInterval
	}
	return &MCPHandler{
		processManager:       pm,
		sessionManager:       sm,
		logger:               logger,
		readTimeout:          readTimeout,
		sseKeepaliveInterval: sseKeepaliveInterval,
		pendingResponses:     make(map[string]chan []byte),
		sseClients:           make(map[string]*SSEWriter),
	}
}

// ServeHTTP implements the http.Handler interface.
func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.HandlePost(w, r)
	case http.MethodGet:
		h.HandleSSE(w, r)
	case http.MethodDelete:
		h.HandleSessionClose(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// HandlePost handles POST requests to the /mcp endpoint.
// It receives JSON-RPC messages, forwards them to the subprocess stdin,
// and returns the response from stdout.
func (h *MCPHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	// Check Content-Type
	contentType := r.Header.Get("Content-Type")
	if contentType != "" && contentType != ContentTypeJSON && contentType != "application/json; charset=utf-8" {
		h.writeError(w, http.StatusUnsupportedMediaType, "unsupported content type")
		return
	}

	// Check Accept header
	accept := r.Header.Get("Accept")
	wantsSSE := false
	switch {
	case accept == "" || accept == ContentTypeJSON || accept == "*/*" || accept == "application/json, */*":
		// JSON response (default)
	case accept == ContentTypeSSE || strings.Contains(accept, ContentTypeSSE):
		wantsSSE = true
	default:
		h.writeError(w, http.StatusNotAcceptable, "unsupported accept type")
		return
	}

	// Check if process is running
	if !h.processManager.Running() {
		h.writeError(w, http.StatusServiceUnavailable, "MCP server not available")
		return
	}

	// Get or create session
	sessionID := r.Header.Get(MCPSessionIDHeader)
	var session *Session
	var err error

	if sessionID != "" {
		session = h.sessionManager.GetSession(sessionID)
		if session == nil {
			h.writeError(w, http.StatusNotFound, "session not found")
			return
		}
		h.sessionManager.TouchSession(sessionID)
	} else {
		// Create new session for initialization request
		session, err = h.sessionManager.CreateSession()
		if err != nil {
			h.logger.Error("failed to create session", "error", err)
			h.writeError(w, http.StatusServiceUnavailable, "failed to create session")
			return
		}
	}

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		h.logger.Error("failed to read request body", "error", err)
		h.writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Parse the message to determine type and extract ID
	msg, err := protocol.ParseMessage(body)
	if err != nil {
		h.writeJSONRPCError(w, protocol.RequestID{}, protocol.NewParseError(err.Error()))
		return
	}

	// Start read loop if not already running
	h.ensureReadLoop()

	// Handle based on message type
	switch msg.Type() {
	case protocol.MessageTypeRequest:
		h.handleRequest(w, r, session, body, msg, wantsSSE)

	case protocol.MessageTypeNotification:
		h.handleNotification(w, r, session, body)

	default:
		h.writeJSONRPCError(w, protocol.RequestID{}, protocol.NewInvalidRequestError("invalid message type"))
	}
}

// handleRequest handles a JSON-RPC request message.
func (h *MCPHandler) handleRequest(w http.ResponseWriter, r *http.Request, session *Session, body []byte, msg *protocol.Message, wantsSSE bool) {
	req := msg.ToRequest()
	if req == nil {
		h.writeJSONRPCError(w, protocol.RequestID{}, protocol.NewInvalidRequestError("invalid request"))
		return
	}

	// Create response channel
	responseChan := make(chan []byte, 1)
	requestID := req.ID.String()

	h.pendingMu.Lock()
	h.pendingResponses[requestID] = responseChan
	h.pendingMu.Unlock()

	defer func() {
		h.pendingMu.Lock()
		delete(h.pendingResponses, requestID)
		h.pendingMu.Unlock()
	}()

	// Forward request to subprocess
	h.mu.Lock()
	stdin := h.processManager.Stdin()
	if stdin == nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusServiceUnavailable, "MCP server not available")
		return
	}

	// Write request to subprocess stdin
	if _, err := stdin.Write(body); err != nil {
		h.mu.Unlock()
		h.logger.Error("failed to write to subprocess", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to communicate with MCP server")
		return
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		h.mu.Unlock()
		h.logger.Error("failed to write newline to subprocess", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to communicate with MCP server")
		return
	}
	h.mu.Unlock()

	h.logger.Debug("forwarded request to subprocess",
		"method", req.Method,
		"id", req.ID.String(),
		"session_id", session.ID,
	)

	// Wait for response with timeout
	timeout := h.readTimeout
	if deadline, ok := r.Context().Deadline(); ok {
		timeout = time.Until(deadline)
	}

	select {
	case response := <-responseChan:
		// Check if this is an initialize response
		if req.Method == "initialize" {
			session.SetInitialized(true)
			// Parse and store capabilities
			var resp protocol.Response
			if err := json.Unmarshal(response, &resp); err == nil && resp.Result != nil {
				var result map[string]interface{}
				if err := json.Unmarshal(resp.Result, &result); err == nil {
					if caps, ok := result["capabilities"]; ok {
						session.SetCapabilities(caps)
					}
				}
			}
		}

		// Write response
		w.Header().Set(MCPSessionIDHeader, session.ID)
		if wantsSSE {
			flusher, ok := w.(http.Flusher)
			if !ok {
				h.writeError(w, http.StatusInternalServerError, "streaming not supported")
				return
			}
			sse := NewSSEWriter(w, flusher)
			sse.SetHeaders(w)
			_ = sse.WriteMessage(response)
			sse.Close()
		} else {
			w.Header().Set("Content-Type", ContentTypeJSON)
			w.WriteHeader(http.StatusOK)
			w.Write(response)
		}

	case <-time.After(timeout):
		h.logger.Error("timeout waiting for response",
			"method", req.Method,
			"id", req.ID.String(),
		)
		h.writeJSONRPCError(w, req.ID, protocol.NewInternalError("timeout waiting for response"))

	case <-r.Context().Done():
		h.logger.Debug("request cancelled",
			"method", req.Method,
			"id", req.ID.String(),
		)
		// Don't write anything, connection is closed
	}
}

// handleNotification handles a JSON-RPC notification message.
func (h *MCPHandler) handleNotification(w http.ResponseWriter, r *http.Request, session *Session, body []byte) {
	// Forward notification to subprocess (no response expected)
	h.mu.Lock()
	stdin := h.processManager.Stdin()
	if stdin == nil {
		h.mu.Unlock()
		h.writeError(w, http.StatusServiceUnavailable, "MCP server not available")
		return
	}

	if _, err := stdin.Write(body); err != nil {
		h.mu.Unlock()
		h.logger.Error("failed to write notification to subprocess", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to communicate with MCP server")
		return
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		h.mu.Unlock()
		h.logger.Error("failed to write newline to subprocess", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to communicate with MCP server")
		return
	}
	h.mu.Unlock()

	h.logger.Debug("forwarded notification to subprocess", "session_id", session.ID)

	// Return 202 Accepted for notifications
	w.Header().Set(MCPSessionIDHeader, session.ID)
	w.WriteHeader(http.StatusAccepted)
}

// HandleSSE handles GET requests to the /mcp endpoint for Server-Sent Events.
// It establishes a long-lived connection for server-initiated notifications.
func (h *MCPHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Check Accept header
	accept := r.Header.Get("Accept")
	if accept != ContentTypeSSE && accept != "*/*" && accept != "" {
		h.writeError(w, http.StatusNotAcceptable, "this endpoint requires Accept: text/event-stream")
		return
	}

	// Get session ID
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		h.writeError(w, http.StatusBadRequest, "Mcp-Session-Id header required")
		return
	}

	session := h.sessionManager.GetSession(sessionID)
	if session == nil {
		h.writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Check if response supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Create SSE writer
	sse := NewSSEWriter(w, flusher)
	sse.SetHeaders(w)

	// Register SSE client
	h.sseClientsMu.Lock()
	h.sseClients[sessionID] = sse
	h.sseClientsMu.Unlock()

	defer func() {
		h.sseClientsMu.Lock()
		delete(h.sseClients, sessionID)
		h.sseClientsMu.Unlock()
	}()

	h.logger.Info("SSE connection established", "session_id", sessionID)

	// Send initial connected event
	connData, _ := json.Marshal(map[string]string{"session_id": sessionID})
	if err := sse.WriteEvent("connected", string(connData)); err != nil {
		h.logger.Error("failed to send connected event", "error", err)
		return
	}

	// Keep connection alive and wait for notifications
	notifications := session.Notifications()

	for {
		select {
		case <-r.Context().Done():
			h.logger.Info("SSE connection closed", "session_id", sessionID)
			return

		case msg, ok := <-notifications:
			if !ok {
				// Session closed
				return
			}
			if err := sse.WriteMessage(msg); err != nil {
				h.logger.Error("failed to send SSE message", "error", err)
				return
			}

		case <-time.After(h.sseKeepaliveInterval):
			// Send keepalive
			if err := sse.WriteEvent("ping", ""); err != nil {
				h.logger.Debug("SSE connection lost during keepalive", "session_id", sessionID)
				return
			}
		}
	}
}

// HandleSessionClose handles DELETE requests to close an MCP session.
func (h *MCPHandler) HandleSessionClose(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		h.writeError(w, http.StatusBadRequest, "Mcp-Session-Id header required")
		return
	}

	session := h.sessionManager.GetSession(sessionID)
	if session == nil {
		h.writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Close session
	h.sessionManager.CloseSession(sessionID)

	// Close SSE connection if exists
	h.sseClientsMu.Lock()
	if sse, exists := h.sseClients[sessionID]; exists {
		_ = sse.WriteEvent("close", `{"reason":"session_closed"}`) // Best effort
		delete(h.sseClients, sessionID)
	}
	h.sseClientsMu.Unlock()

	h.logger.Info("session closed", "session_id", sessionID)

	w.WriteHeader(http.StatusNoContent)
}

// ensureReadLoop starts the read loop if not already running.
func (h *MCPHandler) ensureReadLoop() {
	h.readLoopMu.Lock()
	defer h.readLoopMu.Unlock()

	if h.readLoopStarted {
		return
	}

	h.readLoopStarted = true
	go h.readLoop()
}

// readLoop continuously reads from the subprocess stdout and routes messages.
func (h *MCPHandler) readLoop() {
	stdout := h.processManager.Stdout()
	if stdout == nil {
		h.logger.Error("subprocess stdout is nil")
		return
	}

	reader := bufio.NewReader(stdout)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				h.logger.Debug("subprocess stdout closed")
			} else {
				h.logger.Error("error reading from subprocess", "error", err)
			}

			// Mark read loop as stopped so it can be restarted
			h.readLoopMu.Lock()
			h.readLoopStarted = false
			h.readLoopMu.Unlock()
			return
		}

		// Skip empty lines
		if len(line) <= 1 {
			continue
		}

		// Parse the message
		msg, err := protocol.ParseMessage(line)
		if err != nil {
			h.logger.Warn("failed to parse message from subprocess", "error", err)
			continue
		}

		// Route based on message type
		switch msg.Type() {
		case protocol.MessageTypeResponse:
			h.routeResponse(line, msg)

		case protocol.MessageTypeNotification:
			h.routeNotification(line, msg)

		default:
			h.logger.Warn("unexpected message type from subprocess",
				"type", msg.Type().String(),
			)
		}
	}
}

// routeResponse routes a response to the waiting request handler.
func (h *MCPHandler) routeResponse(data []byte, msg *protocol.Message) {
	if msg.ID == nil {
		h.logger.Warn("response missing ID")
		return
	}

	requestID := msg.ID.String()

	h.pendingMu.Lock()
	responseChan, exists := h.pendingResponses[requestID]
	h.pendingMu.Unlock()

	if !exists {
		h.logger.Warn("no pending request for response", "id", requestID)
		return
	}

	// Send response to waiting handler
	select {
	case responseChan <- data:
	default:
		h.logger.Warn("response channel full", "id", requestID)
	}
}

// routeNotification routes a notification to all SSE clients.
func (h *MCPHandler) routeNotification(data []byte, msg *protocol.Message) {
	h.logger.Debug("routing notification", "method", msg.Method)

	// Send to all SSE clients
	h.sseClientsMu.RLock()
	for sessionID, sse := range h.sseClients {
		if err := sse.WriteMessage(data); err != nil {
			h.logger.Warn("failed to send notification to SSE client",
				"session_id", sessionID,
				"error", err,
			)
		}
	}
	h.sseClientsMu.RUnlock()

	// Also queue for sessions without active SSE connections
	h.sessionManager.BroadcastNotification(data)
}

// writeError writes an HTTP error response.
func (h *MCPHandler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message}) // Best effort
}

// writeJSONRPCError writes a JSON-RPC error response.
func (h *MCPHandler) writeJSONRPCError(w http.ResponseWriter, id protocol.RequestID, err *protocol.Error) {
	resp := protocol.NewErrorResponse(id, err)
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(http.StatusOK)        // JSON-RPC errors use 200 OK
	_ = json.NewEncoder(w).Encode(resp) // Best effort
}

// Shutdown gracefully shuts down the handler.
func (h *MCPHandler) Shutdown(ctx context.Context) error {
	// Close all SSE connections
	h.sseClientsMu.Lock()
	for sessionID, sse := range h.sseClients {
		_ = sse.WriteEvent("close", `{"reason":"server_shutdown"}`) // Best effort
		delete(h.sseClients, sessionID)
	}
	h.sseClientsMu.Unlock()

	return nil
}
