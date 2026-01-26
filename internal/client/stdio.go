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
	"bufio"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
)

// StdioHandler errors.
var (
	// ErrStdioHandlerClosed indicates the handler has been closed.
	ErrStdioHandlerClosed = errors.New("stdio handler is closed")

	// ErrWriteFailed indicates a write operation failed.
	ErrWriteFailed = errors.New("write failed")
)

// StdioHandler manages reading from stdin and writing to stdout for the client.
// It provides thread-safe operations and proper buffering for newline-delimited
// JSON messages used in MCP communication.
type StdioHandler struct {
	// Input/output streams
	in  io.Reader
	out io.Writer

	// Buffered reader for efficient line-based reading
	reader *bufio.Reader

	// Write mutex for thread-safe writing
	writeMu sync.Mutex

	// Logger
	logger *logging.Logger

	// State management
	closed   bool
	closedMu sync.RWMutex
}

// NewStdioHandler creates a new StdioHandler for reading and writing messages.
// The in parameter is typically os.Stdin and out is typically os.Stdout.
func NewStdioHandler(in io.Reader, out io.Writer, logger *logging.Logger) *StdioHandler {
	if logger == nil {
		logger = logging.Default()
	}

	return &StdioHandler{
		in:     in,
		out:    out,
		reader: bufio.NewReaderSize(in, 64*1024), // 64KB buffer
		logger: logger.WithFields(map[string]any{"component": "stdio"}),
	}
}

// ReadMessages returns a channel that yields messages read from stdin.
// Each message is a newline-delimited JSON object.
// The channel is closed when stdin is closed or the context is cancelled.
func (h *StdioHandler) ReadMessages(ctx context.Context) <-chan []byte {
	messages := make(chan []byte, 10)

	go func() {
		defer close(messages)

		for {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				h.logger.Debug("stopping stdin reader due to context cancellation")
				return
			default:
			}

			// Check if handler is closed
			h.closedMu.RLock()
			if h.closed {
				h.closedMu.RUnlock()
				h.logger.Debug("stopping stdin reader due to handler closed")
				return
			}
			h.closedMu.RUnlock()

			// Read the next line
			line, err := h.reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					h.logger.Debug("stdin EOF reached")
					return
				}
				if errors.Is(err, io.ErrClosedPipe) {
					h.logger.Debug("stdin pipe closed")
					return
				}
				h.logger.Error("error reading from stdin", "error", err)
				return
			}

			// Skip empty lines
			if len(line) == 0 {
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

			h.logger.Debug("read message from stdin", "size", len(msg))

			// Send to channel
			select {
			case messages <- msg:
			case <-ctx.Done():
				h.logger.Debug("stopping stdin reader due to context cancellation during send")
				return
			}
		}
	}()

	return messages
}

// ReadMessage reads a single newline-delimited message from stdin.
// This is a blocking call that waits for input.
func (h *StdioHandler) ReadMessage() ([]byte, error) {
	h.closedMu.RLock()
	if h.closed {
		h.closedMu.RUnlock()
		return nil, ErrStdioHandlerClosed
	}
	h.closedMu.RUnlock()

	line, err := h.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, err
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

// WriteMessage writes a message to stdout with a trailing newline.
// This method is thread-safe and can be called from multiple goroutines.
func (h *StdioHandler) WriteMessage(msg []byte) error {
	h.closedMu.RLock()
	if h.closed {
		h.closedMu.RUnlock()
		return ErrStdioHandlerClosed
	}
	h.closedMu.RUnlock()

	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	// Write message
	n, err := h.out.Write(msg)
	if err != nil {
		return err
	}
	if n != len(msg) {
		return ErrWriteFailed
	}

	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		_, err = h.out.Write([]byte{'\n'})
		if err != nil {
			return err
		}
	}

	h.logger.Debug("wrote message to stdout", "size", len(msg))

	return nil
}

// WriteRaw writes raw bytes to stdout without adding a newline.
// This method is thread-safe.
func (h *StdioHandler) WriteRaw(data []byte) error {
	h.closedMu.RLock()
	if h.closed {
		h.closedMu.RUnlock()
		return ErrStdioHandlerClosed
	}
	h.closedMu.RUnlock()

	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	n, err := h.out.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return ErrWriteFailed
	}

	return nil
}

// Reader returns the underlying io.Reader for stdin.
func (h *StdioHandler) Reader() io.Reader {
	return h.in
}

// Writer returns the underlying io.Writer for stdout.
func (h *StdioHandler) Writer() io.Writer {
	return h.out
}

// Close marks the handler as closed.
// Note: This does not close the underlying readers/writers as they are
// typically os.Stdin and os.Stdout which should not be closed.
func (h *StdioHandler) Close() error {
	h.closedMu.Lock()
	defer h.closedMu.Unlock()

	if h.closed {
		return nil
	}
	h.closed = true

	h.logger.Debug("stdio handler closed")

	return nil
}

// Closed returns true if the handler has been closed.
func (h *StdioHandler) Closed() bool {
	h.closedMu.RLock()
	defer h.closedMu.RUnlock()
	return h.closed
}

// ThreadSafeWriter wraps an io.Writer with mutex protection.
// This is useful when multiple goroutines need to write to stdout.
type ThreadSafeWriter struct {
	w  io.Writer
	mu sync.Mutex
}

// NewThreadSafeWriter creates a new ThreadSafeWriter.
func NewThreadSafeWriter(w io.Writer) *ThreadSafeWriter {
	return &ThreadSafeWriter{w: w}
}

// Write implements io.Writer with thread safety.
func (w *ThreadSafeWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// WriteString writes a string to the underlying writer.
func (w *ThreadSafeWriter) WriteString(s string) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return io.WriteString(w.w, s)
}

// ThreadSafeReader wraps an io.Reader with mutex protection.
type ThreadSafeReader struct {
	r  io.Reader
	mu sync.Mutex
}

// NewThreadSafeReader creates a new ThreadSafeReader.
func NewThreadSafeReader(r io.Reader) *ThreadSafeReader {
	return &ThreadSafeReader{r: r}
}

// Read implements io.Reader with thread safety.
func (r *ThreadSafeReader) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Read(p)
}
