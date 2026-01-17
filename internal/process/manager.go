// Package process provides lifecycle management for stdio-based MCP server subprocesses.
package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ProcessState represents the current state of the managed process.
type ProcessState int32

const (
	// StateStopped indicates the process is not running.
	StateStopped ProcessState = iota
	// StateStarting indicates the process is being started.
	StateStarting
	// StateRunning indicates the process is running normally.
	StateRunning
	// StateStopping indicates the process is being shut down.
	StateStopping
	// StateFailed indicates the process exited unexpectedly and won't be restarted.
	StateFailed
)

// String returns a human-readable representation of the process state.
func (s ProcessState) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// EventType represents the type of lifecycle event.
type EventType int

const (
	// EventStarted fires when the process has started successfully.
	EventStarted EventType = iota
	// EventStopped fires when the process has stopped normally.
	EventStopped
	// EventFailed fires when the process has failed unexpectedly.
	EventFailed
	// EventRestarting fires when the process is being restarted.
	EventRestarting
	// EventMaxRestartsReached fires when restart limit is exceeded.
	EventMaxRestartsReached
)

// String returns a human-readable representation of the event type.
func (e EventType) String() string {
	switch e {
	case EventStarted:
		return "started"
	case EventStopped:
		return "stopped"
	case EventFailed:
		return "failed"
	case EventRestarting:
		return "restarting"
	case EventMaxRestartsReached:
		return "max_restarts_reached"
	default:
		return "unknown"
	}
}

// Event represents a process lifecycle event.
type Event struct {
	Type      EventType
	PID       int
	ExitCode  int
	Error     error
	Timestamp time.Time
}

// ManagerConfig contains configuration for the process manager.
// This mirrors the config.MCPServerConfig structure.
type ManagerConfig struct {
	// Command is the executable to run.
	Command string
	// Args are the command line arguments.
	Args []string
	// Env contains additional environment variables (key=value format).
	Env map[string]string
	// Dir is the working directory for the process.
	Dir string
	// GracefulShutdownTimeout is the time to wait for graceful shutdown before SIGKILL.
	GracefulShutdownTimeout time.Duration
	// RestartOnFailure enables automatic restart on unexpected exits.
	RestartOnFailure bool
	// MaxRestarts is the maximum number of automatic restarts (0 = unlimited).
	MaxRestarts int
	// RestartDelay is the delay between restart attempts.
	RestartDelay time.Duration
}

// Manager defines the interface for process lifecycle management.
type Manager interface {
	// Start begins the subprocess. Returns error if already running.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the subprocess.
	Stop(ctx context.Context) error
	// Restart stops and starts the subprocess.
	Restart(ctx context.Context) error
	// Stdin returns a writer to the process stdin.
	Stdin() io.WriteCloser
	// Stdout returns a reader from the process stdout.
	Stdout() io.ReadCloser
	// Stderr returns a reader from the process stderr.
	Stderr() io.ReadCloser
	// Wait blocks until the process exits and returns the exit error.
	Wait() error
	// Running returns true if the process is currently running.
	Running() bool
	// PID returns the process ID, or 0 if not running.
	PID() int
	// State returns the current process state.
	State() ProcessState
	// RestartCount returns the number of automatic restarts performed.
	RestartCount() int
	// Events returns a channel that receives lifecycle events.
	Events() <-chan Event
	// Close shuts down the manager and releases resources.
	Close() error
}

// manager implements the Manager interface.
type manager struct {
	cfg ManagerConfig

	mu           sync.RWMutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	state        atomic.Int32
	restartCount atomic.Int32
	pid          atomic.Int32

	// Event handling
	events     chan Event
	eventsDone chan struct{}

	// Lifecycle control
	ctx        context.Context
	cancel     context.CancelFunc
	monitorWg  sync.WaitGroup
	waitCh     chan error
	closedOnce sync.Once
	closed     atomic.Bool
}

// NewManager creates a new process manager with the given configuration.
func NewManager(cfg ManagerConfig) Manager {
	// Apply defaults
	if cfg.GracefulShutdownTimeout == 0 {
		cfg.GracefulShutdownTimeout = 10 * time.Second
	}
	if cfg.RestartDelay == 0 {
		cfg.RestartDelay = time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &manager{
		cfg:        cfg,
		events:     make(chan Event, 16),
		eventsDone: make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
		waitCh:     make(chan error, 1),
	}

	m.state.Store(int32(StateStopped))

	return m
}

// Start begins the subprocess.
func (m *manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed.Load() {
		return errors.New("manager is closed")
	}

	currentState := ProcessState(m.state.Load())
	if currentState == StateRunning || currentState == StateStarting {
		return errors.New("process is already running or starting")
	}

	return m.startLocked(ctx)
}

// startLocked starts the process. Caller must hold m.mu.
func (m *manager) startLocked(ctx context.Context) error {
	m.state.Store(int32(StateStarting))

	// Build command
	cmd := exec.CommandContext(m.ctx, m.cfg.Command, m.cfg.Args...)

	// Set working directory
	if m.cfg.Dir != "" {
		cmd.Dir = m.cfg.Dir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range m.cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.state.Store(int32(StateFailed))
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		m.state.Store(int32(StateFailed))
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		m.state.Store(int32(StateFailed))
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		m.state.Store(int32(StateFailed))
		return fmt.Errorf("starting process: %w", err)
	}

	m.cmd = cmd
	m.stdin = stdin
	m.stdout = stdout
	m.stderr = stderr
	m.pid.Store(int32(cmd.Process.Pid))
	m.state.Store(int32(StateRunning))

	// Clear the wait channel
	select {
	case <-m.waitCh:
	default:
	}
	m.waitCh = make(chan error, 1)

	// Start monitor goroutine
	m.monitorWg.Add(1)
	go m.monitor()

	// Send started event
	m.sendEvent(Event{
		Type:      EventStarted,
		PID:       cmd.Process.Pid,
		Timestamp: time.Now(),
	})

	return nil
}

// Stop gracefully shuts down the subprocess.
func (m *manager) Stop(ctx context.Context) error {
	m.mu.Lock()

	if m.closed.Load() {
		m.mu.Unlock()
		return errors.New("manager is closed")
	}

	currentState := ProcessState(m.state.Load())
	if currentState != StateRunning {
		m.mu.Unlock()
		return nil
	}

	m.state.Store(int32(StateStopping))
	cmd := m.cmd
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		m.state.Store(int32(StateStopped))
		return nil
	}

	// Close stdin to signal the process
	m.mu.Lock()
	if m.stdin != nil {
		m.stdin.Close()
	}
	m.mu.Unlock()

	// Send SIGTERM
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited
		if !errors.Is(err, os.ErrProcessDone) {
			// Try SIGKILL immediately
			cmd.Process.Kill()
		}
	}

	// Wait for process to exit or timeout
	timeout := m.cfg.GracefulShutdownTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-m.waitCh:
		// Process exited
	case <-timer.C:
		// Timeout, send SIGKILL
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		// Wait for the kill to take effect
		select {
		case <-m.waitCh:
		case <-time.After(time.Second):
		}
	case <-ctx.Done():
		// Context cancelled, force kill
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return ctx.Err()
	}

	return nil
}

// Restart stops and starts the subprocess.
func (m *manager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return fmt.Errorf("stopping process: %w", err)
	}

	// Wait a moment for cleanup
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("starting process: %w", err)
	}

	return nil
}

// Stdin returns a writer to the process stdin.
func (m *manager) Stdin() io.WriteCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stdin
}

// Stdout returns a reader from the process stdout.
func (m *manager) Stdout() io.ReadCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stdout
}

// Stderr returns a reader from the process stderr.
func (m *manager) Stderr() io.ReadCloser {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stderr
}

// Wait blocks until the process exits and returns the exit error.
func (m *manager) Wait() error {
	m.mu.RLock()
	waitCh := m.waitCh
	m.mu.RUnlock()

	select {
	case err := <-waitCh:
		// Put it back for other waiters
		select {
		case waitCh <- err:
		default:
		}
		return err
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

// Running returns true if the process is currently running.
func (m *manager) Running() bool {
	return ProcessState(m.state.Load()) == StateRunning
}

// PID returns the process ID, or 0 if not running.
func (m *manager) PID() int {
	return int(m.pid.Load())
}

// State returns the current process state.
func (m *manager) State() ProcessState {
	return ProcessState(m.state.Load())
}

// RestartCount returns the number of automatic restarts performed.
func (m *manager) RestartCount() int {
	return int(m.restartCount.Load())
}

// Events returns a channel that receives lifecycle events.
func (m *manager) Events() <-chan Event {
	return m.events
}

// Close shuts down the manager and releases resources.
func (m *manager) Close() error {
	var closeErr error

	m.closedOnce.Do(func() {
		m.closed.Store(true)

		// Stop the process if running
		ctx, cancel := context.WithTimeout(context.Background(), m.cfg.GracefulShutdownTimeout)
		defer cancel()

		if err := m.Stop(ctx); err != nil {
			closeErr = err
		}

		// Cancel the manager context
		m.cancel()

		// Wait for monitor to exit
		m.monitorWg.Wait()

		// Close event channel
		close(m.events)
	})

	return closeErr
}

// monitor watches the process and handles restarts.
func (m *manager) monitor() {
	defer m.monitorWg.Done()

	m.mu.RLock()
	cmd := m.cmd
	m.mu.RUnlock()

	if cmd == nil {
		return
	}

	// Wait for process to exit
	err := cmd.Wait()

	// Determine exit code
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	m.mu.Lock()
	currentState := ProcessState(m.state.Load())
	wasStopping := currentState == StateStopping
	m.pid.Store(0)

	// Clean up pipes
	if m.stdin != nil {
		m.stdin.Close()
		m.stdin = nil
	}
	if m.stdout != nil {
		m.stdout.Close()
		m.stdout = nil
	}
	if m.stderr != nil {
		m.stderr.Close()
		m.stderr = nil
	}
	m.cmd = nil
	m.mu.Unlock()

	// Send wait result
	select {
	case m.waitCh <- err:
	default:
	}

	// If we were stopping intentionally, just mark as stopped
	if wasStopping || m.closed.Load() {
		m.state.Store(int32(StateStopped))
		m.sendEvent(Event{
			Type:      EventStopped,
			ExitCode:  exitCode,
			Timestamp: time.Now(),
		})
		return
	}

	// Process exited unexpectedly
	m.sendEvent(Event{
		Type:      EventFailed,
		ExitCode:  exitCode,
		Error:     err,
		Timestamp: time.Now(),
	})

	// Check if we should restart
	if !m.cfg.RestartOnFailure {
		m.state.Store(int32(StateFailed))
		return
	}

	// Check restart count
	restarts := m.restartCount.Add(1)
	if m.cfg.MaxRestarts > 0 && int(restarts) > m.cfg.MaxRestarts {
		m.state.Store(int32(StateFailed))
		m.sendEvent(Event{
			Type:      EventMaxRestartsReached,
			Timestamp: time.Now(),
		})
		return
	}

	// Wait before restart
	m.sendEvent(Event{
		Type:      EventRestarting,
		Timestamp: time.Now(),
	})

	select {
	case <-time.After(m.cfg.RestartDelay):
	case <-m.ctx.Done():
		m.state.Store(int32(StateStopped))
		return
	}

	// Attempt restart
	m.mu.Lock()
	if m.closed.Load() {
		m.mu.Unlock()
		m.state.Store(int32(StateStopped))
		return
	}
	restartErr := m.startLocked(m.ctx)
	m.mu.Unlock()

	if restartErr != nil {
		m.state.Store(int32(StateFailed))
		m.sendEvent(Event{
			Type:      EventFailed,
			Error:     fmt.Errorf("restart failed: %w", restartErr),
			Timestamp: time.Now(),
		})
	}
}

// sendEvent sends an event to the events channel without blocking.
func (m *manager) sendEvent(evt Event) {
	select {
	case m.events <- evt:
	default:
		// Channel full, drop oldest event and try again
		select {
		case <-m.events:
		default:
		}
		select {
		case m.events <- evt:
		default:
		}
	}
}
