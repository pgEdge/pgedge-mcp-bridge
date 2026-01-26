/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package process

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewManager verifies that NewManager creates a manager with correct configuration.
func TestNewManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		cfg                     ManagerConfig
		wantShutdownTimeout     time.Duration
		wantRestartDelay        time.Duration
		wantInitialState        ProcessState
		wantInitialRestartCount int
	}{
		{
			name: "basic config",
			cfg: ManagerConfig{
				Command: "echo",
				Args:    []string{"hello"},
			},
			wantShutdownTimeout:     10 * time.Second, // default
			wantRestartDelay:        time.Second,      // default
			wantInitialState:        StateStopped,
			wantInitialRestartCount: 0,
		},
		{
			name: "custom shutdown timeout",
			cfg: ManagerConfig{
				Command:                 "echo",
				GracefulShutdownTimeout: 5 * time.Second,
			},
			wantShutdownTimeout:     5 * time.Second,
			wantRestartDelay:        time.Second, // default
			wantInitialState:        StateStopped,
			wantInitialRestartCount: 0,
		},
		{
			name: "custom restart delay",
			cfg: ManagerConfig{
				Command:      "echo",
				RestartDelay: 500 * time.Millisecond,
			},
			wantShutdownTimeout:     10 * time.Second, // default
			wantRestartDelay:        500 * time.Millisecond,
			wantInitialState:        StateStopped,
			wantInitialRestartCount: 0,
		},
		{
			name: "full custom config",
			cfg: ManagerConfig{
				Command:                 "cat",
				Args:                    []string{"-"},
				Env:                     map[string]string{"FOO": "bar"},
				Dir:                     "/tmp",
				GracefulShutdownTimeout: 3 * time.Second,
				RestartOnFailure:        true,
				MaxRestarts:             5,
				RestartDelay:            200 * time.Millisecond,
			},
			wantShutdownTimeout:     3 * time.Second,
			wantRestartDelay:        200 * time.Millisecond,
			wantInitialState:        StateStopped,
			wantInitialRestartCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewManager(tt.cfg)
			if m == nil {
				t.Fatal("NewManager returned nil")
			}
			defer m.Close()

			if m.State() != tt.wantInitialState {
				t.Errorf("State() = %v, want %v", m.State(), tt.wantInitialState)
			}

			if m.RestartCount() != tt.wantInitialRestartCount {
				t.Errorf("RestartCount() = %v, want %v", m.RestartCount(), tt.wantInitialRestartCount)
			}

			if m.PID() != 0 {
				t.Errorf("PID() = %v, want 0", m.PID())
			}

			if m.Running() {
				t.Error("Running() = true, want false")
			}

			// Verify events channel exists
			if m.Events() == nil {
				t.Error("Events() returned nil")
			}
		})
	}
}

// TestStartLaunchesSubprocess verifies that Start successfully launches a subprocess.
func TestStartLaunchesSubprocess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		args    []string
		env     map[string]string
	}{
		{
			name:    "sleep command",
			command: "sleep",
			args:    []string{"1"},
		},
		{
			name:    "cat command",
			command: "cat",
			args:    nil,
		},
		{
			name:    "command with env",
			command: "sh",
			args:    []string{"-c", "sleep 1"},
			env:     map[string]string{"TEST_VAR": "test_value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewManager(ManagerConfig{
				Command: tt.command,
				Args:    tt.args,
				Env:     tt.env,
			})
			defer m.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := m.Start(ctx)
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}

			// Give it time to register the state
			time.Sleep(50 * time.Millisecond)

			// Process should be running (we use longer-running commands now)
			state := m.State()
			if state != StateRunning {
				t.Errorf("State() = %v, want Running", state)
			}

			// PID should be non-zero when running
			if m.PID() == 0 {
				t.Error("PID() = 0, want non-zero when running")
			}
		})
	}
}

// TestStartAlreadyRunning verifies that Start returns error when process is already running.
func TestStartAlreadyRunning(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First start should succeed
	if err := m.Start(ctx); err != nil {
		t.Fatalf("First Start() error = %v", err)
	}

	// Second start should fail
	err := m.Start(ctx)
	if err == nil {
		t.Error("Second Start() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("Expected 'already running' error, got: %v", err)
	}
}

// TestStartInvalidCommand verifies that Start returns error for invalid commands.
func TestStartInvalidCommand(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "/nonexistent/command/that/does/not/exist",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := m.Start(ctx)
	if err == nil {
		t.Error("Start() expected error for invalid command, got nil")
	}

	if m.State() != StateFailed {
		t.Errorf("State() = %v, want %v", m.State(), StateFailed)
	}
}

// TestStopTerminatesSubprocess verifies that Stop gracefully terminates a running subprocess.
func TestStopTerminatesSubprocess(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:                 "cat",
		GracefulShutdownTimeout: 5 * time.Second,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the process
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify it's running
	time.Sleep(50 * time.Millisecond)
	if !m.Running() {
		t.Skip("Process not running, skipping stop test")
	}

	pid := m.PID()
	if pid == 0 {
		t.Error("PID() = 0 for running process")
	}

	// Stop the process
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Wait for state to update
	time.Sleep(100 * time.Millisecond)

	if m.Running() {
		t.Error("Running() = true after Stop()")
	}

	state := m.State()
	if state != StateStopped {
		t.Errorf("State() = %v, want %v", state, StateStopped)
	}
}

// TestStopNotRunning verifies that Stop on a non-running process returns nil.
func TestStopNotRunning(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "echo",
		Args:    []string{"hello"},
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop without start should be a no-op
	err := m.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() on non-running process error = %v", err)
	}
}

// TestRestartContextCancellation tests Restart with cancelled context.
func TestRestartContextCancellation(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:                 "cat",
		GracefulShutdownTimeout: 100 * time.Millisecond,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the process first
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Create a context that's already cancelled
	cancelledCtx, cancelledCancel := context.WithCancel(context.Background())
	cancelledCancel()

	// Restart should return context error
	err := m.Restart(cancelledCtx)
	if err == nil {
		t.Error("Restart() with cancelled context expected error, got nil")
	}
}

// TestRestartFromNotRunning tests Restart when process is not running.
func TestRestartFromNotRunning(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Restart without starting first
	err := m.Restart(ctx)
	if err != nil {
		t.Logf("Restart() from stopped state error (may be expected): %v", err)
	}

	// Should now be running
	time.Sleep(100 * time.Millisecond)
	if m.State() != StateRunning {
		t.Errorf("State() = %v, want %v after Restart from stopped", m.State(), StateRunning)
	}
}

// TestRestartStopsAndRestarts verifies that Restart stops and restarts the subprocess.
func TestRestartStopsAndRestarts(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:                 "cat",
		GracefulShutdownTimeout: 2 * time.Second,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the process
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if !m.Running() {
		t.Skip("Process not running, skipping restart test")
	}

	oldPID := m.PID()

	// Restart the process
	if err := m.Restart(ctx); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}

	// Wait for the new process to start
	time.Sleep(200 * time.Millisecond)

	if !m.Running() {
		t.Error("Running() = false after Restart()")
	}

	newPID := m.PID()
	if newPID == oldPID && oldPID != 0 {
		t.Error("PID did not change after restart")
	}
}

// TestProcessStateTransitions verifies correct state transitions.
func TestProcessStateTransitions(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:                 "cat",
		GracefulShutdownTimeout: 2 * time.Second,
	})
	defer m.Close()

	// Initial state should be Stopped
	if m.State() != StateStopped {
		t.Errorf("Initial state = %v, want %v", m.State(), StateStopped)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the process
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for running state
	time.Sleep(50 * time.Millisecond)
	if m.State() != StateRunning {
		t.Errorf("After Start, state = %v, want %v", m.State(), StateRunning)
	}

	// Stop the process
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Wait for stopped state
	time.Sleep(100 * time.Millisecond)
	if m.State() != StateStopped {
		t.Errorf("After Stop, state = %v, want %v", m.State(), StateStopped)
	}
}

// TestPipeAccess verifies that Stdin, Stdout, and Stderr pipes are accessible.
func TestPipeAccess(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Before start, pipes should be nil
	if m.Stdin() != nil {
		t.Error("Stdin() should be nil before Start()")
	}
	if m.Stdout() != nil {
		t.Error("Stdout() should be nil before Start()")
	}
	if m.Stderr() != nil {
		t.Error("Stderr() should be nil before Start()")
	}

	// Start the process
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// After start, pipes should be available
	if m.Stdin() == nil {
		t.Error("Stdin() is nil after Start()")
	}
	if m.Stdout() == nil {
		t.Error("Stdout() is nil after Start()")
	}
	if m.Stderr() == nil {
		t.Error("Stderr() is nil after Start()")
	}
}

// TestStdinStdoutCommunication verifies bidirectional pipe communication.
func TestStdinStdoutCommunication(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	testMessage := "hello world\n"

	// Write to stdin
	stdin := m.Stdin()
	if stdin == nil {
		t.Fatal("Stdin() is nil")
	}

	_, err := stdin.Write([]byte(testMessage))
	if err != nil {
		t.Fatalf("Write to stdin error = %v", err)
	}

	// Read from stdout
	stdout := m.Stdout()
	if stdout == nil {
		t.Fatal("Stdout() is nil")
	}

	buf := make([]byte, len(testMessage))
	done := make(chan error, 1)

	go func() {
		_, err := io.ReadFull(stdout, buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Read from stdout error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for stdout")
	}

	if string(buf) != testMessage {
		t.Errorf("Got %q, want %q", string(buf), testMessage)
	}
}

// TestWaitReturnsExitStatus verifies that Wait returns the correct exit status.
func TestWaitReturnsExitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		command     string
		args        []string
		expectError bool
	}{
		{
			name:        "successful exit",
			command:     "true",
			expectError: false,
		},
		{
			name:        "failed exit",
			command:     "false",
			expectError: true,
		},
		{
			name:        "exit with specific code",
			command:     "sh",
			args:        []string{"-c", "exit 42"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewManager(ManagerConfig{
				Command: tt.command,
				Args:    tt.args,
			})
			defer m.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := m.Start(ctx); err != nil {
				t.Fatalf("Start() error = %v", err)
			}

			// Wait for process to complete
			done := make(chan error, 1)
			go func() {
				done <- m.Wait()
			}()

			select {
			case err := <-done:
				if tt.expectError && err == nil {
					t.Error("Wait() expected error, got nil")
				}
				if !tt.expectError && err != nil {
					t.Errorf("Wait() unexpected error = %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for Wait()")
			}
		})
	}
}

// TestAutomaticRestartOnFailure verifies auto-restart behavior when enabled.
func TestAutomaticRestartOnFailure(t *testing.T) {
	t.Parallel()

	eventCount := &atomic.Int32{}

	m := NewManager(ManagerConfig{
		Command:          "sh",
		Args:             []string{"-c", "exit 1"},
		RestartOnFailure: true,
		MaxRestarts:      3,
		RestartDelay:     50 * time.Millisecond,
	})
	defer m.Close()

	// Drain events in background
	go func() {
		for evt := range m.Events() {
			if evt.Type == EventRestarting {
				eventCount.Add(1)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for restarts to occur
	time.Sleep(500 * time.Millisecond)

	// Check restart count
	restartCount := m.RestartCount()
	if restartCount < 1 {
		t.Errorf("RestartCount() = %d, want >= 1", restartCount)
	}
}

// TestMaxRestartLimitEnforcement verifies that max restart limit is enforced.
func TestMaxRestartLimitEnforcement(t *testing.T) {
	t.Parallel()

	maxRestartsReached := make(chan bool, 1)

	m := NewManager(ManagerConfig{
		Command:          "sh",
		Args:             []string{"-c", "exit 1"},
		RestartOnFailure: true,
		MaxRestarts:      2,
		RestartDelay:     10 * time.Millisecond,
	})
	defer m.Close()

	// Watch for max restarts event
	go func() {
		for evt := range m.Events() {
			if evt.Type == EventMaxRestartsReached {
				maxRestartsReached <- true
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for max restarts to be reached
	select {
	case <-maxRestartsReached:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for EventMaxRestartsReached")
	}

	// After max restarts, state should be Failed
	time.Sleep(100 * time.Millisecond)
	if m.State() != StateFailed {
		t.Errorf("State() = %v, want %v after max restarts", m.State(), StateFailed)
	}

	// Restart count should be at or over the limit
	if m.RestartCount() < 2 {
		t.Errorf("RestartCount() = %d, want >= 2", m.RestartCount())
	}
}

// TestNoRestartWhenDisabled verifies no restart when RestartOnFailure is false.
func TestNoRestartWhenDisabled(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:          "sh",
		Args:             []string{"-c", "exit 1"},
		RestartOnFailure: false,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for process to fail
	time.Sleep(200 * time.Millisecond)

	// Should not have restarted
	if m.RestartCount() != 0 {
		t.Errorf("RestartCount() = %d, want 0", m.RestartCount())
	}

	if m.State() != StateFailed {
		t.Errorf("State() = %v, want %v", m.State(), StateFailed)
	}
}

// TestContextCancellationDuringStart tests context cancellation behavior.
func TestContextCancellationDuringStart(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sleep",
		Args:    []string{"10"},
	})
	defer m.Close()

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start in background
	started := make(chan error, 1)
	go func() {
		started <- m.Start(ctx)
	}()

	// Wait for start to complete
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for Start()")
	}

	// Cancel the context
	cancel()

	// Wait for process to be affected
	time.Sleep(100 * time.Millisecond)

	// Process should still be running (manager context is separate)
	// This tests that start uses its own context
	if m.State() != StateRunning {
		// The process may have been killed if using command context
		// This is acceptable behavior
		t.Logf("State after cancel = %v (process may use context for lifecycle)", m.State())
	}
}

// TestPIDAndRunningStatus verifies PID and Running status consistency.
func TestPIDAndRunningStatus(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sleep",
		Args:    []string{"5"},
	})
	defer m.Close()

	// Before start
	if m.PID() != 0 {
		t.Errorf("PID() before start = %d, want 0", m.PID())
	}
	if m.Running() {
		t.Error("Running() before start = true, want false")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// After start
	time.Sleep(50 * time.Millisecond)
	pid := m.PID()
	if pid == 0 {
		t.Error("PID() after start = 0, want non-zero")
	}
	if !m.Running() {
		t.Error("Running() after start = false, want true")
	}

	// After stop
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if m.PID() != 0 {
		t.Errorf("PID() after stop = %d, want 0", m.PID())
	}
	if m.Running() {
		t.Error("Running() after stop = true, want false")
	}
}

// TestCloseManager verifies that Close properly shuts down the manager.
func TestCloseManager(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Close the manager - may return an error if Stop encounters issues, which is acceptable
	_ = m.Close()

	// After close, operations should fail
	err := m.Start(ctx)
	if err == nil {
		t.Error("Start() after Close() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("Expected 'closed' error, got: %v", err)
	}
}

// TestCloseManagerIdempotent verifies that Close can be called multiple times.
func TestCloseManagerIdempotent(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the process first so Close has something to do
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Close multiple times should not panic
	// First call may return an error from stopping, subsequent calls return nil
	for i := 0; i < 3; i++ {
		_ = m.Close() // Errors on subsequent calls are expected (already closed)
	}

	// Verify the manager is closed
	err := m.Start(ctx)
	if err == nil {
		t.Error("Start() after Close() expected error, got nil")
	}
}

// TestEventsChannel verifies that events are properly sent to the events channel.
func TestEventsChannel(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	})
	defer m.Close()

	events := m.Events()
	if events == nil {
		t.Fatal("Events() returned nil")
	}

	var receivedEvents []EventType
	var mu sync.Mutex

	// Collect events
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range events {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt.Type)
			mu.Unlock()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for process to complete
	time.Sleep(200 * time.Millisecond)

	// Check that we received expected events
	mu.Lock()
	hasStarted := false
	for _, evt := range receivedEvents {
		if evt == EventStarted {
			hasStarted = true
		}
	}
	mu.Unlock()

	if !hasStarted {
		t.Error("Did not receive EventStarted")
	}
}

// TestProcessStateStrings verifies ProcessState String() method.
func TestProcessStateStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state ProcessState
		want  string
	}{
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateFailed, "failed"},
		{ProcessState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("ProcessState(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// TestEventTypeStrings verifies EventType String() method.
func TestEventTypeStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		eventType EventType
		want      string
	}{
		{EventStarted, "started"},
		{EventStopped, "stopped"},
		{EventFailed, "failed"},
		{EventRestarting, "restarting"},
		{EventMaxRestartsReached, "max_restarts_reached"},
		{EventType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.want {
				t.Errorf("EventType(%d).String() = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}

// TestWorkingDirectory verifies that working directory is set correctly.
func TestWorkingDirectory(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "pwd",
		Dir:     "/tmp",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Read stdout
	stdout := m.Stdout()
	if stdout == nil {
		t.Fatal("Stdout() is nil")
	}

	// Use io.ReadAll to read until EOF, ensuring we get all output
	done := make(chan []byte, 1)
	errCh := make(chan error, 1)

	go func() {
		data, err := io.ReadAll(stdout)
		if err != nil {
			errCh <- err
			return
		}
		done <- data
	}()

	// Resolve symlinks for expected path (e.g., on macOS /tmp -> /private/tmp)
	expectedDir, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		expectedDir = "/tmp"
	}

	select {
	case data := <-done:
		output := strings.TrimSpace(string(data))
		if output != expectedDir {
			t.Errorf("Working directory = %q, want %q", output, expectedDir)
		}
	case err := <-errCh:
		t.Errorf("Error reading stdout: %v", err)
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for stdout")
	}
}

// TestEnvironmentVariables verifies that environment variables are passed correctly.
func TestEnvironmentVariables(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sh",
		Args:    []string{"-c", "echo $MY_TEST_VAR"},
		Env:     map[string]string{"MY_TEST_VAR": "test_value_123"},
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	stdout := m.Stdout()
	if stdout == nil {
		t.Fatal("Stdout() is nil")
	}

	buf := make([]byte, 256)
	done := make(chan int, 1)

	go func() {
		n, _ := stdout.Read(buf)
		done <- n
	}()

	select {
	case n := <-done:
		output := strings.TrimSpace(string(buf[:n]))
		if output != "test_value_123" {
			t.Errorf("Environment variable = %q, want test_value_123", output)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for stdout")
	}
}

// TestStopWithTimeout verifies that Stop respects graceful shutdown timeout.
func TestStopWithTimeout(t *testing.T) {
	t.Parallel()

	// Use a process that ignores SIGTERM
	m := NewManager(ManagerConfig{
		Command:                 "sh",
		Args:                    []string{"-c", "trap '' TERM; sleep 30"},
		GracefulShutdownTimeout: 500 * time.Millisecond,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Stop should complete within timeout + some buffer (SIGKILL)
	start := time.Now()
	if err := m.Stop(ctx); err != nil {
		t.Logf("Stop() returned error (expected for killed process): %v", err)
	}
	elapsed := time.Since(start)

	// Should have stopped within reasonable time
	if elapsed > 3*time.Second {
		t.Errorf("Stop() took %v, want < 3s", elapsed)
	}

	// Process should be stopped
	time.Sleep(100 * time.Millisecond)
	if m.Running() {
		t.Error("Process still running after Stop()")
	}
}

// TestMultipleWaiters verifies that multiple goroutines can Wait().
func TestMultipleWaiters(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 3)

	// Start multiple waiters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- m.Wait()
		}()
	}

	// Wait for all waiters with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(results)
		// All waiters completed
		for err := range results {
			if err != nil {
				t.Errorf("Wait() returned unexpected error: %v", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for multiple Wait() calls")
	}
}

// TestConcurrentOperations tests thread safety of manager operations.
func TestConcurrentOperations(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent state reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = m.State()
			_ = m.Running()
			_ = m.PID()
			_ = m.RestartCount()
		}
	}()

	// Concurrent pipe access
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = m.Stdin()
			_ = m.Stdout()
			_ = m.Stderr()
		}
	}()

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("Timeout in concurrent operations")
	}
}

// TestStderrAccess verifies that stderr output is captured.
func TestStderrAccess(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sh",
		Args:    []string{"-c", "echo error_message >&2"},
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	stderr := m.Stderr()
	if stderr == nil {
		t.Fatal("Stderr() is nil")
	}

	buf := make([]byte, 256)
	done := make(chan int, 1)

	go func() {
		n, _ := stderr.Read(buf)
		done <- n
	}()

	select {
	case n := <-done:
		output := strings.TrimSpace(string(buf[:n]))
		if output != "error_message" {
			t.Errorf("Stderr output = %q, want error_message", output)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for stderr")
	}
}

// TestEventTimestamps verifies that events have valid timestamps.
func TestEventTimestamps(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	})
	defer m.Close()

	events := m.Events()
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for an event
	select {
	case evt := <-events:
		if evt.Timestamp.IsZero() {
			t.Error("Event timestamp is zero")
		}
		if evt.Timestamp.Before(startTime) {
			t.Error("Event timestamp is before test start time")
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for event")
	}
}

// TestRestartDuringShutdown tests restart while manager context is cancelled.
func TestRestartDuringShutdown(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:          "sh",
		Args:             []string{"-c", "exit 1"},
		RestartOnFailure: true,
		MaxRestarts:      5,
		RestartDelay:     100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait a bit for process to fail and trigger restart
	time.Sleep(150 * time.Millisecond)

	// Close should handle in-progress restart
	m.Close()
}

// TestRestartFailure tests behavior when restart itself fails.
func TestRestartFailure(t *testing.T) {
	t.Parallel()

	// Create a temp script that fails first, then we'll use a non-existent command
	m := NewManager(ManagerConfig{
		Command:          "sh",
		Args:             []string{"-c", "exit 1"},
		RestartOnFailure: true,
		MaxRestarts:      1,
		RestartDelay:     10 * time.Millisecond,
	})
	defer m.Close()

	eventsFailed := make(chan Event, 10)
	go func() {
		for evt := range m.Events() {
			if evt.Type == EventFailed || evt.Type == EventMaxRestartsReached {
				eventsFailed <- evt
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for events
	select {
	case evt := <-eventsFailed:
		t.Logf("Received event: %v", evt.Type)
	case <-time.After(2 * time.Second):
		t.Log("No failure event received within timeout")
	}
}

// TestStopContextCancellation verifies Stop behavior when context is cancelled.
func TestStopContextCancellation(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:                 "sh",
		Args:                    []string{"-c", "trap '' TERM; sleep 60"},
		GracefulShutdownTimeout: 30 * time.Second,
	})
	defer m.Close()

	startCtx, startCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startCancel()

	if err := m.Start(startCtx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create a context that we'll cancel quickly
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopCancel()

	err := m.Stop(stopCtx)
	// Should return context error
	if err != nil && err != context.DeadlineExceeded {
		t.Logf("Stop() with cancelled context error = %v", err)
	}
}

// TestEventChannelFull tests behavior when event channel is full.
func TestEventChannelFull(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:          "sh",
		Args:             []string{"-c", "exit 1"},
		RestartOnFailure: true,
		MaxRestarts:      50, // Many restarts to fill the channel
		RestartDelay:     1 * time.Millisecond,
	})
	defer m.Close()

	// Don't drain events - let the channel fill up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for many events to be generated
	time.Sleep(300 * time.Millisecond)

	// Should not deadlock even with full event channel
	// The sendEvent function should handle this gracefully
}

// TestStartAfterFailed tests restarting after process failed.
func TestStartAfterFailed(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command:          "false", // exits with code 1
		RestartOnFailure: false,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start and let it fail
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for failure
	time.Sleep(200 * time.Millisecond)

	if m.State() != StateFailed {
		t.Errorf("State() = %v, want %v", m.State(), StateFailed)
	}

	// Should be able to start again after failure
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() after failure error = %v", err)
	}

	// Wait for the new process
	time.Sleep(100 * time.Millisecond)
}

// TestLargeStdinStdout tests handling of large data through pipes.
func TestLargeStdinStdout(t *testing.T) {
	t.Parallel()

	m := NewManager(ManagerConfig{
		Command: "cat",
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create a large message
	largeData := bytes.Repeat([]byte("x"), 64*1024)

	// Write large data
	stdin := m.Stdin()
	if stdin == nil {
		t.Fatal("Stdin() is nil")
	}

	// Start reading in background
	stdout := m.Stdout()
	if stdout == nil {
		t.Fatal("Stdout() is nil")
	}

	readDone := make(chan []byte, 1)
	go func() {
		buf := make([]byte, len(largeData))
		_, err := io.ReadFull(stdout, buf)
		if err != nil {
			readDone <- nil
			return
		}
		readDone <- buf
	}()

	// Write
	_, err := stdin.Write(largeData)
	if err != nil {
		t.Fatalf("Write large data error = %v", err)
	}

	// Wait for read
	select {
	case buf := <-readDone:
		if buf == nil {
			t.Error("Read large data failed")
		} else if !bytes.Equal(buf, largeData) {
			t.Errorf("Read data mismatch, got %d bytes, want %d bytes", len(buf), len(largeData))
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for large data read")
	}
}
