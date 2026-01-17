package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// ErrBridgeClosed is returned when operations are attempted on a closed bridge.
var ErrBridgeClosed = errors.New("stdio bridge is closed")

// ErrPipeClosed is returned when a pipe is unexpectedly closed.
var ErrPipeClosed = errors.New("pipe closed")

// MessageHandler is called for each message read from stdout.
type MessageHandler func(data []byte) error

// ErrorHandler is called when an error occurs during reading.
type ErrorHandler func(err error)

// StdioBridge manages bidirectional communication with a subprocess via stdio.
// It handles buffered reading of newline-delimited JSON messages from stdout
// and thread-safe writing to stdin.
type StdioBridge struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	stdinMu sync.Mutex
	reader  *bufio.Reader

	// Handlers
	messageHandler MessageHandler
	errorHandler   ErrorHandler

	// State
	closed   atomic.Bool
	closedMu sync.Mutex
	closeWg  sync.WaitGroup

	// Read loop control
	ctx    context.Context
	cancel context.CancelFunc
}

// StdioBridgeConfig contains configuration for creating a StdioBridge.
type StdioBridgeConfig struct {
	// Stdin is the writer to the subprocess stdin.
	Stdin io.WriteCloser
	// Stdout is the reader from the subprocess stdout.
	Stdout io.ReadCloser
	// Stderr is the reader from the subprocess stderr (optional).
	Stderr io.ReadCloser
	// BufferSize is the size of the read buffer (default: 64KB).
	BufferSize int
}

// NewStdioBridge creates a new StdioBridge for communicating with a subprocess.
func NewStdioBridge(cfg StdioBridgeConfig) *StdioBridge {
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = 64 * 1024 // 64KB default
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &StdioBridge{
		stdin:  cfg.Stdin,
		stdout: cfg.Stdout,
		stderr: cfg.Stderr,
		reader: bufio.NewReaderSize(cfg.Stdout, bufSize),
		ctx:    ctx,
		cancel: cancel,
	}
}

// NewStdioBridgeFromManager creates a StdioBridge from a process Manager.
func NewStdioBridgeFromManager(m Manager) (*StdioBridge, error) {
	stdin := m.Stdin()
	stdout := m.Stdout()
	stderr := m.Stderr()

	if stdin == nil || stdout == nil {
		return nil, errors.New("manager stdin or stdout is nil")
	}

	return NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}), nil
}

// SetMessageHandler sets the handler for incoming messages.
// Must be called before StartReading.
func (b *StdioBridge) SetMessageHandler(h MessageHandler) {
	b.messageHandler = h
}

// SetErrorHandler sets the handler for read errors.
// Must be called before StartReading.
func (b *StdioBridge) SetErrorHandler(h ErrorHandler) {
	b.errorHandler = h
}

// StartReading begins reading messages from stdout in a goroutine.
// Each newline-delimited message is passed to the MessageHandler.
// Returns immediately; reading happens in the background.
func (b *StdioBridge) StartReading() error {
	if b.closed.Load() {
		return ErrBridgeClosed
	}

	if b.messageHandler == nil {
		return errors.New("message handler not set")
	}

	b.closeWg.Add(1)
	go b.readLoop()

	return nil
}

// readLoop continuously reads newline-delimited messages from stdout.
func (b *StdioBridge) readLoop() {
	defer b.closeWg.Done()

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		// Read until newline
		line, err := b.reader.ReadBytes('\n')
		if err != nil {
			if b.closed.Load() {
				return
			}

			// Handle errors
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				if b.errorHandler != nil {
					b.errorHandler(ErrPipeClosed)
				}
				return
			}

			if b.errorHandler != nil {
				b.errorHandler(fmt.Errorf("reading from stdout: %w", err))
			}

			// Continue trying to read on transient errors
			continue
		}

		// Skip empty lines
		if len(line) == 0 || (len(line) == 1 && line[0] == '\n') {
			continue
		}

		// Remove trailing newline
		if line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		// Handle Windows line endings
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		// Skip if empty after trimming
		if len(line) == 0 {
			continue
		}

		// Make a copy to avoid buffer reuse issues
		msg := make([]byte, len(line))
		copy(msg, line)

		// Call handler
		if err := b.messageHandler(msg); err != nil {
			if b.errorHandler != nil {
				b.errorHandler(fmt.Errorf("message handler error: %w", err))
			}
		}
	}
}

// Write sends data to the subprocess stdin.
// This method is thread-safe.
func (b *StdioBridge) Write(data []byte) error {
	if b.closed.Load() {
		return ErrBridgeClosed
	}

	b.stdinMu.Lock()
	defer b.stdinMu.Unlock()

	if b.stdin == nil {
		return ErrPipeClosed
	}

	_, err := b.stdin.Write(data)
	if err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return ErrPipeClosed
		}
		return fmt.Errorf("writing to stdin: %w", err)
	}

	return nil
}

// WriteMessage sends a newline-delimited message to the subprocess stdin.
// This method is thread-safe and appends a newline if not present.
func (b *StdioBridge) WriteMessage(data []byte) error {
	if b.closed.Load() {
		return ErrBridgeClosed
	}

	b.stdinMu.Lock()
	defer b.stdinMu.Unlock()

	if b.stdin == nil {
		return ErrPipeClosed
	}

	// Write data
	if _, err := b.stdin.Write(data); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return ErrPipeClosed
		}
		return fmt.Errorf("writing to stdin: %w", err)
	}

	// Add newline if not present
	if len(data) == 0 || data[len(data)-1] != '\n' {
		if _, err := b.stdin.Write([]byte{'\n'}); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				return ErrPipeClosed
			}
			return fmt.Errorf("writing newline to stdin: %w", err)
		}
	}

	return nil
}

// ReadMessage reads a single newline-delimited message from stdout.
// This is a blocking call and should not be used with StartReading.
func (b *StdioBridge) ReadMessage() ([]byte, error) {
	if b.closed.Load() {
		return nil, ErrBridgeClosed
	}

	line, err := b.reader.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
			return nil, ErrPipeClosed
		}
		return nil, fmt.Errorf("reading from stdout: %w", err)
	}

	// Remove trailing newline
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	// Handle Windows line endings
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return line, nil
}

// ReadStderr reads from the stderr pipe.
// Returns io.EOF when the pipe is closed.
func (b *StdioBridge) ReadStderr(buf []byte) (int, error) {
	if b.closed.Load() {
		return 0, ErrBridgeClosed
	}

	if b.stderr == nil {
		return 0, errors.New("stderr not available")
	}

	n, err := b.stderr.Read(buf)
	if err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return n, ErrPipeClosed
		}
		return n, err
	}

	return n, nil
}

// DrainStderr reads and discards all stderr output.
// Useful to prevent blocking when stderr output is not needed.
func (b *StdioBridge) DrainStderr() {
	if b.stderr == nil {
		return
	}

	b.closeWg.Add(1)
	go func() {
		defer b.closeWg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-b.ctx.Done():
				return
			default:
			}
			_, err := b.stderr.Read(buf)
			if err != nil {
				return
			}
		}
	}()
}

// Close shuts down the bridge and closes all pipes.
func (b *StdioBridge) Close() error {
	b.closedMu.Lock()
	defer b.closedMu.Unlock()

	if b.closed.Load() {
		return nil
	}
	b.closed.Store(true)

	// Cancel read loop
	b.cancel()

	// Close pipes
	var errs []error

	b.stdinMu.Lock()
	if b.stdin != nil {
		if err := b.stdin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stdin: %w", err))
		}
		b.stdin = nil
	}
	b.stdinMu.Unlock()

	if b.stdout != nil {
		if err := b.stdout.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stdout: %w", err))
		}
	}

	if b.stderr != nil {
		if err := b.stderr.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing stderr: %w", err))
		}
	}

	// Wait for goroutines
	b.closeWg.Wait()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Closed returns true if the bridge has been closed.
func (b *StdioBridge) Closed() bool {
	return b.closed.Load()
}

// ForwardStderr starts a goroutine that reads from stderr and writes to the provided writer.
// Returns immediately; forwarding happens in the background.
func (b *StdioBridge) ForwardStderr(w io.Writer) error {
	if b.closed.Load() {
		return ErrBridgeClosed
	}

	if b.stderr == nil {
		return errors.New("stderr not available")
	}

	b.closeWg.Add(1)
	go func() {
		defer b.closeWg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-b.ctx.Done():
				return
			default:
			}
			n, err := b.stderr.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				w.Write(buf[:n])
			}
		}
	}()

	return nil
}

// StderrReader returns a reader that reads from the subprocess stderr.
// Returns nil if stderr is not available.
func (b *StdioBridge) StderrReader() io.Reader {
	if b.stderr == nil {
		return nil
	}
	return b.stderr
}

// StdinWriter returns a thread-safe writer to the subprocess stdin.
func (b *StdioBridge) StdinWriter() io.Writer {
	return &stdinWriter{bridge: b}
}

// stdinWriter wraps the bridge to provide an io.Writer interface.
type stdinWriter struct {
	bridge *StdioBridge
}

func (w *stdinWriter) Write(p []byte) (n int, err error) {
	err = w.bridge.Write(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
