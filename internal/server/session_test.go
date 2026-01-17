package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

func TestNewSessionManager(t *testing.T) {
	testCases := []struct {
		name                    string
		cfg                     config.SessionConfig
		expectedTimeout         time.Duration
		expectedMaxSessions     int
		expectedCleanupInterval time.Duration
	}{
		{
			name: "with custom config",
			cfg: config.SessionConfig{
				Enabled:         true,
				Timeout:         1 * time.Hour,
				MaxSessions:     50,
				CleanupInterval: 10 * time.Minute,
			},
			expectedTimeout:         1 * time.Hour,
			expectedMaxSessions:     50,
			expectedCleanupInterval: 10 * time.Minute,
		},
		{
			name: "with zero values uses defaults",
			cfg: config.SessionConfig{
				Enabled: true,
			},
			expectedTimeout:         defaultSessionTimeout,
			expectedMaxSessions:     defaultMaxSessions,
			expectedCleanupInterval: defaultCleanupInterval,
		},
		{
			name: "disabled session management",
			cfg: config.SessionConfig{
				Enabled: false,
			},
			expectedTimeout:         defaultSessionTimeout,
			expectedMaxSessions:     defaultMaxSessions,
			expectedCleanupInterval: defaultCleanupInterval,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sm := NewSessionManager(tc.cfg)

			if sm == nil {
				t.Fatal("NewSessionManager() returned nil")
			}
			if sm.sessions == nil {
				t.Error("sessions map not initialized")
			}
			if sm.timeout != tc.expectedTimeout {
				t.Errorf("timeout = %v, want %v", sm.timeout, tc.expectedTimeout)
			}
			if sm.maxSessions != tc.expectedMaxSessions {
				t.Errorf("maxSessions = %d, want %d", sm.maxSessions, tc.expectedMaxSessions)
			}
			if sm.cleanupInterval != tc.expectedCleanupInterval {
				t.Errorf("cleanupInterval = %v, want %v", sm.cleanupInterval, tc.expectedCleanupInterval)
			}
			if sm.enabled != tc.cfg.Enabled {
				t.Errorf("enabled = %v, want %v", sm.enabled, tc.cfg.Enabled)
			}
		})
	}
}

func TestSessionManager_CreateSession(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 10,
	})

	session, err := sm.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if session == nil {
		t.Fatal("CreateSession() returned nil session")
	}

	if session.ID == "" {
		t.Error("session ID should not be empty")
	}

	if len(session.ID) != sessionIDLength*2 { // Hex encoding doubles the length
		t.Errorf("session ID length = %d, want %d", len(session.ID), sessionIDLength*2)
	}

	if session.CreatedAt.IsZero() {
		t.Error("session CreatedAt should be set")
	}

	if session.LastActivity.IsZero() {
		t.Error("session LastActivity should be set")
	}

	if session.notifications == nil {
		t.Error("session notifications channel should be initialized")
	}
}

func TestSessionManager_CreateSession_GeneratesUniqueIDs(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
	})

	ids := make(map[string]bool)
	const numSessions = 50

	for i := 0; i < numSessions; i++ {
		session, err := sm.CreateSession()
		if err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}

		if ids[session.ID] {
			t.Errorf("duplicate session ID: %s", session.ID)
		}
		ids[session.ID] = true
	}
}

func TestSessionManager_GetSession(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled: true,
	})

	// Create a session
	session, _ := sm.CreateSession()

	// Get the session
	retrieved := sm.GetSession(session.ID)
	if retrieved == nil {
		t.Fatal("GetSession() returned nil for existing session")
	}
	if retrieved.ID != session.ID {
		t.Errorf("retrieved session ID = %s, want %s", retrieved.ID, session.ID)
	}

	// Get non-existent session
	nonExistent := sm.GetSession("nonexistent-session-id")
	if nonExistent != nil {
		t.Error("GetSession() should return nil for non-existent session")
	}
}

func TestSessionManager_GetSession_ClosedSession(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled: true,
	})

	// Create and close a session
	session, _ := sm.CreateSession()
	session.Close()

	// GetSession should return nil for closed sessions
	retrieved := sm.GetSession(session.ID)
	if retrieved != nil {
		t.Error("GetSession() should return nil for closed session")
	}
}

func TestSessionManager_CloseSession(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled: true,
	})

	// Create a session
	session, _ := sm.CreateSession()
	sessionID := session.ID

	// Verify session exists
	if sm.GetSession(sessionID) == nil {
		t.Fatal("session should exist before closing")
	}

	// Close the session
	sm.CloseSession(sessionID)

	// Verify session is removed
	if sm.GetSession(sessionID) != nil {
		t.Error("session should be nil after closing")
	}

	// Closing non-existent session should not panic
	sm.CloseSession("nonexistent-session")
}

func TestSessionManager_TouchSession(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled: true,
	})

	session, _ := sm.CreateSession()
	initialActivity := session.LastActivity

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Touch the session
	sm.TouchSession(session.ID)

	// Verify LastActivity was updated
	if !session.LastActivity.After(initialActivity) {
		t.Error("LastActivity should have been updated")
	}

	// Touching non-existent session should not panic
	sm.TouchSession("nonexistent-session")
}

func TestSessionManager_SessionCount(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
	})

	if count := sm.SessionCount(); count != 0 {
		t.Errorf("initial SessionCount() = %d, want 0", count)
	}

	// Create some sessions
	for i := 0; i < 5; i++ {
		sm.CreateSession()
	}

	if count := sm.SessionCount(); count != 5 {
		t.Errorf("SessionCount() = %d, want 5", count)
	}

	// Close one session
	sessions := sm.ListSessions()
	sm.CloseSession(sessions[0].ID)

	if count := sm.SessionCount(); count != 4 {
		t.Errorf("SessionCount() after close = %d, want 4", count)
	}
}

func TestSessionManager_MaxSessionsLimit(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 3,
	})

	// Create sessions up to the limit
	for i := 0; i < 3; i++ {
		_, err := sm.CreateSession()
		if err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
	}

	// Try to create one more
	_, err := sm.CreateSession()
	if err == nil {
		t.Error("CreateSession() should return error when max sessions reached")
	}
	if err.Error() != "maximum sessions reached" {
		t.Errorf("unexpected error message: %v", err)
	}

	// After closing one, we should be able to create another
	sessions := sm.ListSessions()
	sm.CloseSession(sessions[0].ID)

	_, err = sm.CreateSession()
	if err != nil {
		t.Errorf("CreateSession() should succeed after closing a session: %v", err)
	}
}

func TestSessionManager_CloseAllSessions(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
	})

	// Create multiple sessions
	for i := 0; i < 5; i++ {
		sm.CreateSession()
	}

	if count := sm.SessionCount(); count != 5 {
		t.Errorf("SessionCount() = %d, want 5", count)
	}

	// Close all sessions
	sm.CloseAllSessions()

	if count := sm.SessionCount(); count != 0 {
		t.Errorf("SessionCount() after CloseAllSessions() = %d, want 0", count)
	}
}

func TestSessionManager_BroadcastNotification(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
	})

	// Create multiple sessions
	session1, _ := sm.CreateSession()
	session2, _ := sm.CreateSession()
	session3, _ := sm.CreateSession()

	notification := []byte(`{"jsonrpc":"2.0","method":"test"}`)

	// Broadcast notification
	sm.BroadcastNotification(notification)

	// Check all sessions received the notification
	select {
	case msg := <-session1.Notifications():
		if string(msg) != string(notification) {
			t.Errorf("session1 received wrong notification: %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("session1 did not receive notification")
	}

	select {
	case msg := <-session2.Notifications():
		if string(msg) != string(notification) {
			t.Errorf("session2 received wrong notification: %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("session2 did not receive notification")
	}

	select {
	case msg := <-session3.Notifications():
		if string(msg) != string(notification) {
			t.Errorf("session3 received wrong notification: %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("session3 did not receive notification")
	}
}

func TestSessionManager_ListSessions(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 100,
	})

	// Create sessions
	session1, _ := sm.CreateSession()
	session2, _ := sm.CreateSession()
	session1.SetInitialized(true)

	sessions := sm.ListSessions()

	if len(sessions) != 2 {
		t.Fatalf("ListSessions() returned %d sessions, want 2", len(sessions))
	}

	// Verify session info
	found1, found2 := false, false
	for _, info := range sessions {
		if info.ID == session1.ID {
			found1 = true
			if !info.Initialized {
				t.Error("session1 should be initialized")
			}
		}
		if info.ID == session2.ID {
			found2 = true
			if info.Initialized {
				t.Error("session2 should not be initialized")
			}
		}
	}

	if !found1 {
		t.Error("session1 not found in ListSessions()")
	}
	if !found2 {
		t.Error("session2 not found in ListSessions()")
	}
}

func TestSessionManager_StartStopCleanup(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:         true,
		CleanupInterval: 50 * time.Millisecond,
		Timeout:         100 * time.Millisecond,
	})

	ctx := context.Background()
	sm.StartCleanup(ctx)

	// Create a session
	session, _ := sm.CreateSession()

	// Session should exist
	if sm.GetSession(session.ID) == nil {
		t.Fatal("session should exist initially")
	}

	// Wait for session to expire and be cleaned up
	time.Sleep(200 * time.Millisecond)

	// Session should be cleaned up
	if sm.GetSession(session.ID) != nil {
		t.Error("session should have been cleaned up after expiry")
	}

	// Stop cleanup
	sm.StopCleanup()
}

func TestSessionManager_CleanupDisabled(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:         false,
		CleanupInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	sm.StartCleanup(ctx) // Should not start anything

	// Create a session
	session, _ := sm.CreateSession()

	// Wait
	time.Sleep(100 * time.Millisecond)

	// Session should still exist (cleanup not running)
	if sm.GetSession(session.ID) == nil {
		t.Error("session should still exist when cleanup is disabled")
	}

	// StopCleanup should not panic even when not started
	sm.StopCleanup()
}

func TestSessionManager_CleanupExpiredSessions(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled: true,
		Timeout: 50 * time.Millisecond,
	})

	// Create sessions
	session1, _ := sm.CreateSession()
	session2, _ := sm.CreateSession()

	// Wait for sessions to expire
	time.Sleep(60 * time.Millisecond)

	// Create a fresh session
	session3, _ := sm.CreateSession()

	// Manually trigger cleanup
	sm.cleanupExpiredSessions()

	// Old sessions should be cleaned up
	if sm.GetSession(session1.ID) != nil {
		t.Error("session1 should have been cleaned up")
	}
	if sm.GetSession(session2.ID) != nil {
		t.Error("session2 should have been cleaned up")
	}

	// New session should still exist
	if sm.GetSession(session3.ID) == nil {
		t.Error("session3 should still exist")
	}
}

// Session tests

func TestSession_Touch(t *testing.T) {
	session := newSession("test-id")
	initialActivity := session.LastActivity

	time.Sleep(10 * time.Millisecond)
	session.Touch()

	if !session.LastActivity.After(initialActivity) {
		t.Error("Touch() should update LastActivity")
	}
}

func TestSession_SetInitialized(t *testing.T) {
	session := newSession("test-id")

	if session.IsInitialized() {
		t.Error("session should not be initialized initially")
	}

	session.SetInitialized(true)
	if !session.IsInitialized() {
		t.Error("session should be initialized after SetInitialized(true)")
	}

	session.SetInitialized(false)
	if session.IsInitialized() {
		t.Error("session should not be initialized after SetInitialized(false)")
	}
}

func TestSession_ClientInfo(t *testing.T) {
	session := newSession("test-id")

	// Initial client info should be empty
	info := session.GetClientInfo()
	if len(info) != 0 {
		t.Error("initial client info should be empty")
	}

	// Set client info
	newInfo := map[string]interface{}{
		"name":    "test-client",
		"version": "1.0.0",
	}
	session.SetClientInfo(newInfo)

	// Get client info
	retrieved := session.GetClientInfo()
	if retrieved["name"] != "test-client" {
		t.Errorf("name = %v, want test-client", retrieved["name"])
	}
	if retrieved["version"] != "1.0.0" {
		t.Errorf("version = %v, want 1.0.0", retrieved["version"])
	}

	// Verify it returns a copy
	retrieved["name"] = "modified"
	original := session.GetClientInfo()
	if original["name"] != "test-client" {
		t.Error("GetClientInfo() should return a copy")
	}
}

func TestSession_Capabilities(t *testing.T) {
	session := newSession("test-id")

	// Initial capabilities should be nil
	if session.GetCapabilities() != nil {
		t.Error("initial capabilities should be nil")
	}

	// Set capabilities
	caps := map[string]interface{}{
		"tools": true,
		"prompts": map[string]interface{}{
			"list": true,
		},
	}
	session.SetCapabilities(caps)

	// Get capabilities
	retrieved := session.GetCapabilities()
	if retrieved == nil {
		t.Fatal("GetCapabilities() returned nil")
	}

	capsMap := retrieved.(map[string]interface{})
	if capsMap["tools"] != true {
		t.Error("tools capability not set correctly")
	}
}

func TestSession_QueueNotification(t *testing.T) {
	session := newSession("test-id")

	// Queue a notification
	notification := []byte(`{"test":"notification"}`)
	if !session.QueueNotification(notification) {
		t.Error("QueueNotification() should return true")
	}

	// Receive the notification
	select {
	case msg := <-session.Notifications():
		if string(msg) != string(notification) {
			t.Errorf("received wrong notification: %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive notification")
	}
}

func TestSession_QueueNotification_BufferFull(t *testing.T) {
	session := newSession("test-id")

	// Fill the notification buffer
	for i := 0; i < notificationBufferSize; i++ {
		session.QueueNotification([]byte("test"))
	}

	// Next notification should be dropped
	if session.QueueNotification([]byte("overflow")) {
		t.Error("QueueNotification() should return false when buffer is full")
	}
}

func TestSession_QueueNotification_Closed(t *testing.T) {
	session := newSession("test-id")
	session.Close()

	// Should return false for closed session
	if session.QueueNotification([]byte("test")) {
		t.Error("QueueNotification() should return false for closed session")
	}
}

func TestSession_Close(t *testing.T) {
	session := newSession("test-id")

	if session.IsClosed() {
		t.Error("session should not be closed initially")
	}

	session.Close()

	if !session.IsClosed() {
		t.Error("session should be closed after Close()")
	}

	// Closing again should not panic
	session.Close()
}

func TestSession_IsExpired(t *testing.T) {
	session := newSession("test-id")
	timeout := 50 * time.Millisecond

	// Should not be expired initially
	if session.IsExpired(timeout) {
		t.Error("session should not be expired initially")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Should be expired now
	if !session.IsExpired(timeout) {
		t.Error("session should be expired after timeout")
	}

	// Touch to reset
	session.Touch()

	// Should not be expired after touch
	if session.IsExpired(timeout) {
		t.Error("session should not be expired after Touch()")
	}
}

func TestSession_ConcurrentAccess(t *testing.T) {
	session := newSession("test-id")

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Concurrent reads and writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				session.Touch()
				session.IsInitialized()
				session.SetInitialized(id%2 == 0)
				session.GetClientInfo()
				session.SetClientInfo(map[string]interface{}{"id": id})
				session.GetCapabilities()
				session.SetCapabilities(map[string]interface{}{"id": id})
				session.IsClosed()
				session.IsExpired(time.Hour)
			}
		}(i)
	}

	wg.Wait()
}

func TestSessionManager_ConcurrentAccess(t *testing.T) {
	sm := NewSessionManager(config.SessionConfig{
		Enabled:     true,
		MaxSessions: 1000,
	})

	var wg sync.WaitGroup
	const numGoroutines = 20
	sessionsChan := make(chan *Session, 100)

	// Concurrent session creation
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				session, err := sm.CreateSession()
				if err == nil && session != nil {
					sessionsChan <- session
				}
			}
		}()
	}

	// Concurrent session operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				sm.SessionCount()
				sm.ListSessions()
			}
		}()
	}

	// Wait for creation to complete
	wg.Wait()
	close(sessionsChan)

	// Collect created sessions
	var sessions []*Session
	for s := range sessionsChan {
		sessions = append(sessions, s)
	}

	// Concurrent close operations
	for _, session := range sessions {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			sm.CloseSession(s.ID)
		}(session)
	}

	wg.Wait()

	// All sessions should be closed
	if sm.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after concurrent close, got %d", sm.SessionCount())
	}
}

func TestGenerateSessionID(t *testing.T) {
	ids := make(map[string]bool)
	const numIDs = 1000

	for i := 0; i < numIDs; i++ {
		id, err := generateSessionID()
		if err != nil {
			t.Fatalf("generateSessionID() error = %v", err)
		}

		if len(id) != sessionIDLength*2 { // Hex encoding doubles length
			t.Errorf("id length = %d, want %d", len(id), sessionIDLength*2)
		}

		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}
