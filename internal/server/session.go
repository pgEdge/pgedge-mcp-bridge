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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

const (
	// defaultSessionTimeout is the default timeout for inactive sessions.
	defaultSessionTimeout = 30 * time.Minute

	// defaultMaxSessions is the default maximum number of concurrent sessions.
	defaultMaxSessions = 100

	// defaultCleanupInterval is the default interval for cleaning up expired sessions.
	defaultCleanupInterval = 5 * time.Minute

	// sessionIDLength is the length of generated session IDs in bytes.
	sessionIDLength = 16

	// notificationBufferSize is the size of the notification buffer per session.
	notificationBufferSize = 100
)

// Session represents an MCP session.
// It tracks session state, client information, and buffered notifications.
type Session struct {
	// ID is the unique session identifier.
	ID string

	// CreatedAt is when the session was created.
	CreatedAt time.Time

	// LastActivity is when the session was last accessed.
	LastActivity time.Time

	// Initialized indicates whether the MCP initialize handshake is complete.
	Initialized bool

	// clientInfo holds information about the connected client.
	clientInfo map[string]interface{}

	// capabilities holds the negotiated MCP capabilities.
	capabilities interface{}

	// notifications is a channel for queued notifications.
	notifications chan []byte

	// closed indicates whether the session has been closed.
	closed bool

	// mu protects concurrent access to session fields.
	mu sync.RWMutex
}

// newSession creates a new session with the given ID.
func newSession(id string) *Session {
	return &Session{
		ID:            id,
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
		clientInfo:    make(map[string]interface{}),
		notifications: make(chan []byte, notificationBufferSize),
	}
}

// Touch updates the last activity timestamp.
func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActivity = time.Now()
}

// SetInitialized marks the session as initialized.
func (s *Session) SetInitialized(initialized bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Initialized = initialized
}

// IsInitialized returns whether the session has been initialized.
func (s *Session) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Initialized
}

// SetClientInfo sets client information for the session.
func (s *Session) SetClientInfo(info map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientInfo = info
}

// GetClientInfo returns the client information.
func (s *Session) GetClientInfo() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to prevent external modification
	info := make(map[string]interface{}, len(s.clientInfo))
	for k, v := range s.clientInfo {
		info[k] = v
	}
	return info
}

// SetCapabilities sets the negotiated capabilities.
func (s *Session) SetCapabilities(caps interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.capabilities = caps
}

// GetCapabilities returns the negotiated capabilities.
func (s *Session) GetCapabilities() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.capabilities
}

// Notifications returns the notification channel for this session.
func (s *Session) Notifications() <-chan []byte {
	return s.notifications
}

// QueueNotification queues a notification for the session.
// Returns false if the session is closed or the buffer is full.
func (s *Session) QueueNotification(data []byte) bool {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return false
	}
	s.mu.RUnlock()

	select {
	case s.notifications <- data:
		return true
	default:
		// Buffer full, drop notification
		return false
	}
}

// Close closes the session and its notification channel.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.notifications)
}

// IsClosed returns whether the session has been closed.
func (s *Session) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// IsExpired returns whether the session has expired based on the given timeout.
func (s *Session) IsExpired(timeout time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.LastActivity) > timeout
}

// SessionManager manages MCP sessions.
// It handles session creation, retrieval, expiration, and cleanup.
type SessionManager struct {
	// sessions maps session IDs to Session objects.
	sessions map[string]*Session

	// mu protects concurrent access to sessions.
	mu sync.RWMutex

	// maxSessions is the maximum number of concurrent sessions.
	maxSessions int

	// timeout is the duration after which inactive sessions expire.
	timeout time.Duration

	// cleanupInterval is how often to run the cleanup routine.
	cleanupInterval time.Duration

	// enabled indicates whether session management is enabled.
	enabled bool

	// cleanupCancel cancels the cleanup goroutine.
	cleanupCancel context.CancelFunc

	// cleanupDone signals when cleanup has stopped.
	cleanupDone chan struct{}
}

// NewSessionManager creates a new session manager with the given configuration.
func NewSessionManager(cfg config.SessionConfig) *SessionManager {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultSessionTimeout
	}

	maxSessions := cfg.MaxSessions
	if maxSessions == 0 {
		maxSessions = defaultMaxSessions
	}

	cleanupInterval := cfg.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = defaultCleanupInterval
	}

	return &SessionManager{
		sessions:        make(map[string]*Session),
		maxSessions:     maxSessions,
		timeout:         timeout,
		cleanupInterval: cleanupInterval,
		enabled:         cfg.Enabled,
		cleanupDone:     make(chan struct{}),
	}
}

// CreateSession creates a new session with a unique ID.
// Returns an error if the maximum number of sessions has been reached.
func (sm *SessionManager) CreateSession() (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check session limit
	if len(sm.sessions) >= sm.maxSessions {
		return nil, errors.New("maximum sessions reached")
	}

	// Generate unique session ID
	id, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	// Ensure uniqueness
	for sm.sessions[id] != nil {
		id, err = generateSessionID()
		if err != nil {
			return nil, err
		}
	}

	session := newSession(id)
	sm.sessions[id] = session

	return session, nil
}

// GetSession retrieves a session by ID.
// Returns nil if the session does not exist or has been closed.
func (sm *SessionManager) GetSession(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[id]
	if !exists {
		return nil
	}

	if session.IsClosed() {
		return nil
	}

	return session
}

// TouchSession updates the last activity timestamp for a session.
func (sm *SessionManager) TouchSession(id string) {
	session := sm.GetSession(id)
	if session != nil {
		session.Touch()
	}
}

// CloseSession closes and removes a session.
func (sm *SessionManager) CloseSession(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[id]
	if !exists {
		return
	}

	session.Close()
	delete(sm.sessions, id)
}

// CloseAllSessions closes all active sessions.
func (sm *SessionManager) CloseAllSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for id, session := range sm.sessions {
		session.Close()
		delete(sm.sessions, id)
	}
}

// SessionCount returns the number of active sessions.
func (sm *SessionManager) SessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// BroadcastNotification sends a notification to all sessions.
func (sm *SessionManager) BroadcastNotification(data []byte) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, session := range sm.sessions {
		session.QueueNotification(data)
	}
}

// StartCleanup starts the background cleanup goroutine.
func (sm *SessionManager) StartCleanup(ctx context.Context) {
	if !sm.enabled {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	sm.cleanupCancel = cancel

	go sm.cleanupLoop(ctx)
}

// StopCleanup stops the background cleanup goroutine.
func (sm *SessionManager) StopCleanup() {
	if sm.cleanupCancel != nil {
		sm.cleanupCancel()
		<-sm.cleanupDone
	}
}

// cleanupLoop periodically removes expired sessions.
func (sm *SessionManager) cleanupLoop(ctx context.Context) {
	defer close(sm.cleanupDone)

	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.cleanupExpiredSessions()
		}
	}
}

// cleanupExpiredSessions removes all expired sessions.
func (sm *SessionManager) cleanupExpiredSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var expired []string

	for id, session := range sm.sessions {
		if session.IsExpired(sm.timeout) {
			expired = append(expired, id)
		}
	}

	for _, id := range expired {
		if session, exists := sm.sessions[id]; exists {
			session.Close()
			delete(sm.sessions, id)
		}
	}
}

// ListSessions returns information about all active sessions.
// This is useful for monitoring and debugging.
func (sm *SessionManager) ListSessions() []SessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		session.mu.RLock()
		infos = append(infos, SessionInfo{
			ID:           session.ID,
			CreatedAt:    session.CreatedAt,
			LastActivity: session.LastActivity,
			Initialized:  session.Initialized,
		})
		session.mu.RUnlock()
	}

	return infos
}

// SessionInfo contains summary information about a session.
type SessionInfo struct {
	ID           string
	CreatedAt    time.Time
	LastActivity time.Time
	Initialized  bool
}

// generateSessionID generates a cryptographically random session ID.
func generateSessionID() (string, error) {
	bytes := make([]byte, sessionIDLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
