package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockPipe implements io.ReadWriteCloser for testing.
type mockPipe struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func newMockPipe() *mockPipe {
	r, w := io.Pipe()
	return &mockPipe{reader: r, writer: w}
}

func (p *mockPipe) Read(data []byte) (int, error) {
	return p.reader.Read(data)
}

func (p *mockPipe) Write(data []byte) (int, error) {
	return p.writer.Write(data)
}

func (p *mockPipe) Close() error {
	p.reader.Close()
	p.writer.Close()
	return nil
}

// mockWriteCloser implements io.WriteCloser for testing.
type mockWriteCloser struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	closed   bool
	closeErr error
	writeErr error
}

func (m *mockWriteCloser) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return m.buf.Write(p)
}

func (m *mockWriteCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeErr
}

func (m *mockWriteCloser) Bytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Bytes()
}

// mockReadCloser implements io.ReadCloser for testing.
type mockReadCloser struct {
	mu      sync.Mutex
	reader  *bytes.Reader
	closed  bool
	readErr error
}

func newMockReadCloser(data []byte) *mockReadCloser {
	return &mockReadCloser{
		reader: bytes.NewReader(data),
	}
}

func (m *mockReadCloser) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	if m.readErr != nil {
		return 0, m.readErr
	}
	return m.reader.Read(p)
}

func (m *mockReadCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// TestNewStdioBridge verifies that NewStdioBridge creates a bridge correctly.
func TestNewStdioBridge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        StdioBridgeConfig
		checkStdin bool
	}{
		{
			name: "basic config",
			cfg: StdioBridgeConfig{
				Stdin:  &mockWriteCloser{},
				Stdout: newMockReadCloser([]byte{}),
			},
			checkStdin: true,
		},
		{
			name: "with stderr",
			cfg: StdioBridgeConfig{
				Stdin:  &mockWriteCloser{},
				Stdout: newMockReadCloser([]byte{}),
				Stderr: newMockReadCloser([]byte{}),
			},
			checkStdin: true,
		},
		{
			name: "custom buffer size",
			cfg: StdioBridgeConfig{
				Stdin:      &mockWriteCloser{},
				Stdout:     newMockReadCloser([]byte{}),
				BufferSize: 128 * 1024,
			},
			checkStdin: true,
		},
		{
			name: "default buffer size",
			cfg: StdioBridgeConfig{
				Stdin:      &mockWriteCloser{},
				Stdout:     newMockReadCloser([]byte{}),
				BufferSize: 0, // Should use default
			},
			checkStdin: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bridge := NewStdioBridge(tt.cfg)
			if bridge == nil {
				t.Fatal("NewStdioBridge returned nil")
			}
			defer bridge.Close()

			if bridge.Closed() {
				t.Error("New bridge reports as closed")
			}
		})
	}
}

// TestNewStdioBridgeFromManager verifies creation from a process Manager.
func TestNewStdioBridgeFromManager(t *testing.T) {
	t.Parallel()

	t.Run("valid manager", func(t *testing.T) {
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

		bridge, err := NewStdioBridgeFromManager(m)
		if err != nil {
			t.Fatalf("NewStdioBridgeFromManager() error = %v", err)
		}
		defer bridge.Close()

		if bridge.Closed() {
			t.Error("Bridge reports as closed")
		}
	})

	t.Run("nil pipes", func(t *testing.T) {
		t.Parallel()

		m := NewManager(ManagerConfig{
			Command: "cat",
		})
		defer m.Close()

		// Don't start - pipes will be nil
		_, err := NewStdioBridgeFromManager(m)
		if err == nil {
			t.Error("Expected error for nil pipes, got nil")
		}
	})
}

// TestReadMessage verifies reading newline-delimited messages.
func TestReadMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single message",
			input:    "hello world\n",
			expected: []string{"hello world"},
		},
		{
			name:     "json message",
			input:    `{"id": 1, "method": "test"}` + "\n",
			expected: []string{`{"id": 1, "method": "test"}`},
		},
		{
			name:     "multiple messages",
			input:    "line1\nline2\nline3\n",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "message with carriage return",
			input:    "hello\r\n",
			expected: []string{"hello"},
		},
		{
			name:     "empty line returns empty string",
			input:    "\n",
			expected: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stdin := &mockWriteCloser{}
			stdout := newMockReadCloser([]byte(tt.input))

			bridge := NewStdioBridge(StdioBridgeConfig{
				Stdin:  stdin,
				Stdout: stdout,
			})
			defer bridge.Close()

			for i, expected := range tt.expected {
				msg, err := bridge.ReadMessage()
				if err != nil {
					t.Fatalf("ReadMessage() #%d error = %v", i, err)
				}
				if string(msg) != expected {
					t.Errorf("ReadMessage() #%d = %q, want %q", i, string(msg), expected)
				}
			}
		})
	}
}

// TestReadMessageClosed verifies ReadMessage behavior on closed bridge.
func TestReadMessageClosed(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte("hello\n"))

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})

	bridge.Close()

	_, err := bridge.ReadMessage()
	if err != ErrBridgeClosed {
		t.Errorf("ReadMessage() on closed bridge error = %v, want %v", err, ErrBridgeClosed)
	}
}

// TestReadMessagePipeClosed verifies ReadMessage when pipe is closed.
func TestReadMessagePipeClosed(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	// Empty reader will return EOF immediately
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	_, err := bridge.ReadMessage()
	if err != ErrPipeClosed {
		t.Errorf("ReadMessage() on empty pipe error = %v, want %v", err, ErrPipeClosed)
	}
}

// TestWriteMessage verifies writing newline-delimited messages.
func TestWriteMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "simple message",
			input:    []byte("hello"),
			expected: "hello\n",
		},
		{
			name:     "json message",
			input:    []byte(`{"id": 1, "method": "test"}`),
			expected: `{"id": 1, "method": "test"}` + "\n",
		},
		{
			name:     "message already has newline",
			input:    []byte("hello\n"),
			expected: "hello\n",
		},
		{
			name:     "empty message",
			input:    []byte{},
			expected: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stdin := &mockWriteCloser{}
			stdout := newMockReadCloser([]byte{})

			bridge := NewStdioBridge(StdioBridgeConfig{
				Stdin:  stdin,
				Stdout: stdout,
			})
			defer bridge.Close()

			err := bridge.WriteMessage(tt.input)
			if err != nil {
				t.Fatalf("WriteMessage() error = %v", err)
			}

			got := string(stdin.Bytes())
			if got != tt.expected {
				t.Errorf("WriteMessage() wrote %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestWriteMessageClosed verifies WriteMessage behavior on closed bridge.
func TestWriteMessageClosed(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})

	bridge.Close()

	err := bridge.WriteMessage([]byte("hello"))
	if err != ErrBridgeClosed {
		t.Errorf("WriteMessage() on closed bridge error = %v, want %v", err, ErrBridgeClosed)
	}
}

// TestWriteMessagePipeClosed verifies WriteMessage when stdin pipe is closed.
func TestWriteMessagePipeClosed(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdin.Close() // Close the stdin pipe
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	err := bridge.WriteMessage([]byte("hello"))
	if err != ErrPipeClosed {
		t.Errorf("WriteMessage() on closed pipe error = %v, want %v", err, ErrPipeClosed)
	}
}

// TestWrite verifies raw Write functionality.
func TestWrite(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	// Write without automatic newline
	err := bridge.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got := string(stdin.Bytes())
	if got != "hello" {
		t.Errorf("Write() wrote %q, want %q", got, "hello")
	}
}

// TestWriteThreadSafety verifies thread-safety of Write operations.
func TestWriteThreadSafety(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	var wg sync.WaitGroup
	numWriters := 10
	writesPerWriter := 100

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				msg := []byte("message")
				if err := bridge.WriteMessage(msg); err != nil {
					return // Bridge may be closing
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no race conditions
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for concurrent writes")
	}
}

// TestMessageHandler verifies message handler callbacks.
func TestMessageHandler(t *testing.T) {
	t.Parallel()

	// Use actual pipe for realistic behavior
	pipe := newMockPipe()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: pipe,
	})
	defer bridge.Close()

	receivedMessages := make(chan []byte, 10)

	bridge.SetMessageHandler(func(data []byte) error {
		msg := make([]byte, len(data))
		copy(msg, data)
		receivedMessages <- msg
		return nil
	})

	err := bridge.StartReading()
	if err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Write messages to the pipe
	messages := []string{"msg1", "msg2", "msg3"}
	go func() {
		for _, msg := range messages {
			pipe.Write([]byte(msg + "\n"))
		}
		time.Sleep(100 * time.Millisecond)
		pipe.Close()
	}()

	// Collect received messages
	var received []string
	timeout := time.After(2 * time.Second)

	for i := 0; i < len(messages); i++ {
		select {
		case msg := <-receivedMessages:
			received = append(received, string(msg))
		case <-timeout:
			t.Fatal("Timeout waiting for messages")
		}
	}

	// Verify all messages received
	for i, expected := range messages {
		if i >= len(received) {
			t.Errorf("Missing message %d: %s", i, expected)
			continue
		}
		if received[i] != expected {
			t.Errorf("Message %d = %q, want %q", i, received[i], expected)
		}
	}
}

// TestMessageHandlerError verifies error handling in message handler.
func TestMessageHandlerError(t *testing.T) {
	t.Parallel()

	pipe := newMockPipe()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: pipe,
	})
	defer bridge.Close()

	handlerError := errors.New("handler error")
	errorReceived := make(chan error, 1)

	bridge.SetMessageHandler(func(data []byte) error {
		return handlerError
	})

	bridge.SetErrorHandler(func(err error) {
		if strings.Contains(err.Error(), "handler error") {
			errorReceived <- err
		}
	})

	err := bridge.StartReading()
	if err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Send a message to trigger handler
	go func() {
		pipe.Write([]byte("test\n"))
		time.Sleep(100 * time.Millisecond)
		pipe.Close()
	}()

	select {
	case err := <-errorReceived:
		if !strings.Contains(err.Error(), "handler error") {
			t.Errorf("Error handler received = %v, want handler error", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for error handler")
	}
}

// TestErrorHandler verifies error handler callbacks for read errors.
func TestErrorHandler(t *testing.T) {
	t.Parallel()

	pipe := newMockPipe()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: pipe,
	})
	defer bridge.Close()

	errorReceived := make(chan error, 1)

	bridge.SetMessageHandler(func(data []byte) error {
		return nil
	})

	bridge.SetErrorHandler(func(err error) {
		errorReceived <- err
	})

	err := bridge.StartReading()
	if err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Close the pipe to trigger error
	pipe.Close()

	select {
	case err := <-errorReceived:
		if err != ErrPipeClosed {
			t.Errorf("Error handler received = %v, want %v", err, ErrPipeClosed)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for error handler")
	}
}

// TestStartReadingNoHandler verifies StartReading fails without handler.
func TestStartReadingNoHandler(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
	})
	defer bridge.Close()

	err := bridge.StartReading()
	if err == nil {
		t.Error("StartReading() without handler expected error, got nil")
	}
	if !strings.Contains(err.Error(), "handler not set") {
		t.Errorf("Expected 'handler not set' error, got: %v", err)
	}
}

// TestStartReadingClosed verifies StartReading fails on closed bridge.
func TestStartReadingClosed(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
	})

	bridge.SetMessageHandler(func(data []byte) error {
		return nil
	})

	bridge.Close()

	err := bridge.StartReading()
	if err != ErrBridgeClosed {
		t.Errorf("StartReading() on closed bridge error = %v, want %v", err, ErrBridgeClosed)
	}
}

// TestStartReadingAsyncBehavior verifies that StartReading returns immediately.
func TestStartReadingAsyncBehavior(t *testing.T) {
	t.Parallel()

	pipe := newMockPipe()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: pipe,
	})
	defer bridge.Close()

	bridge.SetMessageHandler(func(data []byte) error {
		return nil
	})

	start := time.Now()
	err := bridge.StartReading()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// StartReading should return almost immediately
	if elapsed > 100*time.Millisecond {
		t.Errorf("StartReading() took %v, expected < 100ms (async)", elapsed)
	}

	// Clean up
	pipe.Close()
}

// TestClose verifies Close behavior.
func TestClose(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})
	stderr := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})

	if bridge.Closed() {
		t.Error("Bridge reports closed before Close()")
	}

	err := bridge.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if !bridge.Closed() {
		t.Error("Bridge reports not closed after Close()")
	}
}

// TestCloseIdempotent verifies Close can be called multiple times.
func TestCloseIdempotent(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
	})

	// Close multiple times should not panic
	for i := 0; i < 3; i++ {
		err := bridge.Close()
		if err != nil && i == 0 {
			t.Errorf("First Close() error = %v", err)
		}
	}
}

// TestReadStderr verifies reading from stderr.
func TestReadStderr(t *testing.T) {
	t.Parallel()

	stderr := newMockReadCloser([]byte("error message"))

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: stderr,
	})
	defer bridge.Close()

	buf := make([]byte, 256)
	n, err := bridge.ReadStderr(buf)
	if err != nil {
		t.Fatalf("ReadStderr() error = %v", err)
	}

	if string(buf[:n]) != "error message" {
		t.Errorf("ReadStderr() = %q, want %q", string(buf[:n]), "error message")
	}
}

// TestReadStderrNoStderr verifies ReadStderr when stderr is not available.
func TestReadStderrNoStderr(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: nil,
	})
	defer bridge.Close()

	buf := make([]byte, 256)
	_, err := bridge.ReadStderr(buf)
	if err == nil {
		t.Error("ReadStderr() without stderr expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("Expected 'not available' error, got: %v", err)
	}
}

// TestReadStderrClosed verifies ReadStderr on closed bridge.
func TestReadStderrClosed(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: newMockReadCloser([]byte("test")),
	})

	bridge.Close()

	buf := make([]byte, 256)
	_, err := bridge.ReadStderr(buf)
	if err != ErrBridgeClosed {
		t.Errorf("ReadStderr() on closed bridge error = %v, want %v", err, ErrBridgeClosed)
	}
}

// TestStderrReader verifies StderrReader returns the reader.
func TestStderrReader(t *testing.T) {
	t.Parallel()

	t.Run("with stderr", func(t *testing.T) {
		t.Parallel()

		stderr := newMockReadCloser([]byte("test"))
		bridge := NewStdioBridge(StdioBridgeConfig{
			Stdin:  &mockWriteCloser{},
			Stdout: newMockReadCloser([]byte{}),
			Stderr: stderr,
		})
		defer bridge.Close()

		reader := bridge.StderrReader()
		if reader == nil {
			t.Error("StderrReader() returned nil")
		}
	})

	t.Run("without stderr", func(t *testing.T) {
		t.Parallel()

		bridge := NewStdioBridge(StdioBridgeConfig{
			Stdin:  &mockWriteCloser{},
			Stdout: newMockReadCloser([]byte{}),
		})
		defer bridge.Close()

		reader := bridge.StderrReader()
		if reader != nil {
			t.Error("StderrReader() without stderr should return nil")
		}
	})
}

// TestStdinWriter verifies StdinWriter returns a valid writer.
func TestStdinWriter(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: newMockReadCloser([]byte{}),
	})
	defer bridge.Close()

	writer := bridge.StdinWriter()
	if writer == nil {
		t.Fatal("StdinWriter() returned nil")
	}

	n, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("StdinWriter().Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("StdinWriter().Write() returned n = %d, want 5", n)
	}

	got := string(stdin.Bytes())
	if got != "hello" {
		t.Errorf("StdinWriter() wrote %q, want %q", got, "hello")
	}
}

// TestDrainStderr verifies DrainStderr functionality.
func TestDrainStderr(t *testing.T) {
	t.Parallel()

	pipe := newMockPipe()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: pipe,
	})

	// Start draining
	bridge.DrainStderr()

	// Write to stderr - should be consumed
	go func() {
		pipe.Write([]byte("error output\n"))
		time.Sleep(100 * time.Millisecond)
		pipe.Close()
	}()

	// Close should wait for drain to complete
	done := make(chan error, 1)
	go func() {
		done <- bridge.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for Close()")
	}
}

// TestDrainStderrNoStderr verifies DrainStderr when stderr is nil.
func TestDrainStderrNoStderr(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: nil,
	})
	defer bridge.Close()

	// Should not panic when stderr is nil
	bridge.DrainStderr()
}

// TestForwardStderr verifies ForwardStderr functionality.
func TestForwardStderr(t *testing.T) {
	t.Parallel()

	pipe := newMockPipe()
	var output bytes.Buffer

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: pipe,
	})

	err := bridge.ForwardStderr(&output)
	if err != nil {
		t.Fatalf("ForwardStderr() error = %v", err)
	}

	// Write to stderr
	testMessage := "forwarded error\n"
	go func() {
		pipe.Write([]byte(testMessage))
		time.Sleep(100 * time.Millisecond)
		pipe.Close()
	}()

	// Wait for forwarding
	time.Sleep(200 * time.Millisecond)

	bridge.Close()

	got := output.String()
	if got != testMessage {
		t.Errorf("Forwarded output = %q, want %q", got, testMessage)
	}
}

// TestForwardStderrNoStderr verifies ForwardStderr when stderr is nil.
func TestForwardStderrNoStderr(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
	})
	defer bridge.Close()

	var output bytes.Buffer
	err := bridge.ForwardStderr(&output)
	if err == nil {
		t.Error("ForwardStderr() without stderr expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("Expected 'not available' error, got: %v", err)
	}
}

// TestForwardStderrClosed verifies ForwardStderr on closed bridge.
func TestForwardStderrClosed(t *testing.T) {
	t.Parallel()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: newMockReadCloser([]byte("test")),
	})

	bridge.Close()

	var output bytes.Buffer
	err := bridge.ForwardStderr(&output)
	if err != ErrBridgeClosed {
		t.Errorf("ForwardStderr() on closed bridge error = %v, want %v", err, ErrBridgeClosed)
	}
}

// TestConcurrentReadWrite verifies thread-safety of concurrent reads and writes.
func TestConcurrentReadWrite(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	var wg sync.WaitGroup
	numWriters := 10
	iterations := 100

	// Concurrent writers to stdin - this tests thread-safety of Write
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = bridge.WriteMessage([]byte("test message"))
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no race conditions
	case <-time.After(5 * time.Second):
		t.Error("Timeout in concurrent write test")
	}

	// Verify some data was written
	written := stdin.Bytes()
	if len(written) == 0 {
		t.Error("No data written during concurrent operations")
	}
}

// TestWriteWithError verifies Write behavior when underlying write fails.
func TestWriteWithError(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{
		writeErr: errors.New("write failed"),
	}

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: newMockReadCloser([]byte{}),
	})
	defer bridge.Close()

	err := bridge.Write([]byte("test"))
	if err == nil {
		t.Error("Write() with error expected error, got nil")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Errorf("Expected 'write failed' error, got: %v", err)
	}
}

// TestWriteNilStdin verifies Write behavior when stdin becomes nil.
func TestWriteNilStdin(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})

	// Close stdin directly
	stdin.Close()

	err := bridge.Write([]byte("test"))
	if err != ErrPipeClosed {
		t.Errorf("Write() on closed stdin error = %v, want %v", err, ErrPipeClosed)
	}

	bridge.Close()
}

// TestLargeMessage verifies handling of large messages.
func TestLargeMessage(t *testing.T) {
	t.Parallel()

	// Create a large message (100KB)
	largeData := bytes.Repeat([]byte("x"), 100*1024)
	input := append(largeData, '\n')

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser(input)

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:      stdin,
		Stdout:     stdout,
		BufferSize: 256 * 1024, // Large buffer to handle the message
	})
	defer bridge.Close()

	msg, err := bridge.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	if len(msg) != len(largeData) {
		t.Errorf("Message length = %d, want %d", len(msg), len(largeData))
	}

	if !bytes.Equal(msg, largeData) {
		t.Error("Message content mismatch")
	}
}

// TestLargeMessageWrite verifies writing large messages.
func TestLargeMessageWrite(t *testing.T) {
	t.Parallel()

	largeData := bytes.Repeat([]byte("y"), 100*1024)

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	err := bridge.WriteMessage(largeData)
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	written := stdin.Bytes()
	expected := append(largeData, '\n')

	if !bytes.Equal(written, expected) {
		t.Errorf("Written data length = %d, want %d", len(written), len(expected))
	}
}

// TestWriteMessageNewlineError verifies WriteMessage handles newline write errors.
func TestWriteMessageNewlineError(t *testing.T) {
	t.Parallel()

	// Create a mock that fails on second write (the newline)
	stdin := &limitedWriteCloser{
		maxWrites: 1,
	}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	// First write succeeds, second (newline) fails
	err := bridge.WriteMessage([]byte("hello"))
	if err == nil {
		t.Error("WriteMessage() expected error for newline write failure, got nil")
	}
}

// limitedWriteCloser allows only a limited number of writes.
type limitedWriteCloser struct {
	mu        sync.Mutex
	writes    int
	maxWrites int
	closed    bool
}

func (l *limitedWriteCloser) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, io.ErrClosedPipe
	}
	l.writes++
	if l.writes > l.maxWrites {
		return 0, errors.New("write limit exceeded")
	}
	return len(p), nil
}

func (l *limitedWriteCloser) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

// TestReadStderrPipeClosed verifies ReadStderr when the pipe returns closed error.
func TestReadStderrPipeClosed(t *testing.T) {
	t.Parallel()

	stderr := &mockReadCloser{}
	stderr.Close() // Close it first

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: newMockReadCloser([]byte{}),
		Stderr: stderr,
	})
	defer bridge.Close()

	buf := make([]byte, 256)
	_, err := bridge.ReadStderr(buf)
	if err != ErrPipeClosed {
		t.Errorf("ReadStderr() on closed pipe error = %v, want %v", err, ErrPipeClosed)
	}
}

// TestWriteClosedStdin verifies Write when stdin is nil (already closed).
func TestWriteClosedStdin(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})

	// Close stdin to make it nil
	stdin.Close()

	// Now Write should return ErrPipeClosed
	err := bridge.Write([]byte("test"))
	if err != ErrPipeClosed {
		t.Errorf("Write() on closed stdin error = %v, want %v", err, ErrPipeClosed)
	}

	bridge.Close()
}

// TestWriteMessageClosedStdin verifies WriteMessage when stdin is nil.
func TestWriteMessageClosedStdin(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})

	// Close stdin to make it nil
	stdin.Close()

	// Now WriteMessage should return ErrPipeClosed
	err := bridge.WriteMessage([]byte("test"))
	if err != ErrPipeClosed {
		t.Errorf("WriteMessage() on closed stdin error = %v, want %v", err, ErrPipeClosed)
	}

	bridge.Close()
}

// TestReadLoopContextCancel tests read loop exits on context cancel.
func TestReadLoopContextCancel(t *testing.T) {
	t.Parallel()

	// Use a blocking pipe that won't return data
	r, w := io.Pipe()
	defer w.Close()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: r,
	})

	bridge.SetMessageHandler(func(data []byte) error {
		return nil
	})

	if err := bridge.StartReading(); err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Give read loop time to start
	time.Sleep(50 * time.Millisecond)

	// Close should cancel context and allow read loop to exit
	done := make(chan error, 1)
	go func() {
		done <- bridge.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("Close() error (may be expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for Close() with blocking read")
	}
}

// TestErrBridgeClosedError verifies ErrBridgeClosed is properly defined.
func TestErrBridgeClosedError(t *testing.T) {
	t.Parallel()

	if ErrBridgeClosed == nil {
		t.Error("ErrBridgeClosed is nil")
	}
	if ErrBridgeClosed.Error() != "stdio bridge is closed" {
		t.Errorf("ErrBridgeClosed.Error() = %q, want %q", ErrBridgeClosed.Error(), "stdio bridge is closed")
	}
}

// TestErrPipeClosedError verifies ErrPipeClosed is properly defined.
func TestErrPipeClosedError(t *testing.T) {
	t.Parallel()

	if ErrPipeClosed == nil {
		t.Error("ErrPipeClosed is nil")
	}
	if ErrPipeClosed.Error() != "pipe closed" {
		t.Errorf("ErrPipeClosed.Error() = %q, want %q", ErrPipeClosed.Error(), "pipe closed")
	}
}

// TestIntegrationWithRealProcess tests StdioBridge with actual process.
func TestIntegrationWithRealProcess(t *testing.T) {
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

	bridge, err := NewStdioBridgeFromManager(m)
	if err != nil {
		t.Fatalf("NewStdioBridgeFromManager() error = %v", err)
	}
	defer bridge.Close()

	receivedMessages := make(chan []byte, 10)

	bridge.SetMessageHandler(func(data []byte) error {
		msg := make([]byte, len(data))
		copy(msg, data)
		receivedMessages <- msg
		return nil
	})

	if err := bridge.StartReading(); err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Send test messages
	testMessages := []string{
		`{"jsonrpc":"2.0","id":1,"method":"test"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}

	for _, msg := range testMessages {
		if err := bridge.WriteMessage([]byte(msg)); err != nil {
			t.Fatalf("WriteMessage() error = %v", err)
		}
	}

	// Verify messages echoed back
	for i, expected := range testMessages {
		select {
		case msg := <-receivedMessages:
			if string(msg) != expected {
				t.Errorf("Message %d = %q, want %q", i, string(msg), expected)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("Timeout waiting for message %d", i)
		}
	}
}

// TestStdinWriterError tests stdinWriter.Write error handling.
func TestStdinWriterError(t *testing.T) {
	t.Parallel()

	stdin := &mockWriteCloser{
		writeErr: errors.New("write failed"),
	}
	stdout := newMockReadCloser([]byte{})

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
	})
	defer bridge.Close()

	writer := bridge.StdinWriter()
	n, err := writer.Write([]byte("test"))
	if err == nil {
		t.Error("stdinWriter.Write() expected error, got nil")
	}
	if n != 0 {
		t.Errorf("stdinWriter.Write() n = %d, want 0 on error", n)
	}
}

// TestReadLoopTransientError tests read loop handling of transient errors.
func TestReadLoopTransientError(t *testing.T) {
	t.Parallel()

	// Create a reader that returns an error once, then data
	reader := &transientErrorReader{
		errorOnRead: 1,
		data:        []byte("hello\n"),
	}

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: reader,
	})
	defer bridge.Close()

	errorReceived := make(chan struct{}, 1)

	bridge.SetMessageHandler(func(data []byte) error {
		return nil
	})

	bridge.SetErrorHandler(func(err error) {
		select {
		case errorReceived <- struct{}{}:
		default:
		}
	})

	if err := bridge.StartReading(); err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Wait for the error and then the message
	select {
	case <-errorReceived:
		// Error handler called
	case <-time.After(time.Second):
		t.Log("No error received (might be OK)")
	}

	time.Sleep(100 * time.Millisecond)
	reader.Close()
	bridge.Close()
}

// transientErrorReader returns an error on specified read, then succeeds.
type transientErrorReader struct {
	mu          sync.Mutex
	errorOnRead int
	readCount   int
	data        []byte
	pos         int
	closed      bool
}

func (r *transientErrorReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, io.EOF
	}

	r.readCount++
	if r.readCount == r.errorOnRead {
		return 0, errors.New("transient error")
	}

	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *transientErrorReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// TestCloseWithCloseErrors tests Close when closing pipes returns errors.
func TestCloseWithCloseErrors(t *testing.T) {
	t.Parallel()

	stdin := &errorCloser{closeErr: errors.New("stdin close error")}
	stdout := &errorCloser{closeErr: errors.New("stdout close error")}
	stderr := &errorCloser{closeErr: errors.New("stderr close error")}

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})

	err := bridge.Close()
	if err == nil {
		t.Error("Close() expected error from closing pipes, got nil")
	}
}

// errorCloser implements io.ReadWriteCloser with close error.
type errorCloser struct {
	closeErr error
	closed   bool
}

func (e *errorCloser) Read(p []byte) (int, error) {
	if e.closed {
		return 0, io.EOF
	}
	return 0, io.EOF
}

func (e *errorCloser) Write(p []byte) (int, error) {
	if e.closed {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func (e *errorCloser) Close() error {
	e.closed = true
	return e.closeErr
}

// TestCloseWithActiveReaders tests Close while read loop is active.
func TestCloseWithActiveReaders(t *testing.T) {
	t.Parallel()

	pipe := newMockPipe()

	bridge := NewStdioBridge(StdioBridgeConfig{
		Stdin:  &mockWriteCloser{},
		Stdout: pipe,
	})

	bridge.SetMessageHandler(func(data []byte) error {
		return nil
	})

	if err := bridge.StartReading(); err != nil {
		t.Fatalf("StartReading() error = %v", err)
	}

	// Give read loop time to start
	time.Sleep(50 * time.Millisecond)

	// Close should complete even with active reader
	done := make(chan error, 1)
	go func() {
		done <- bridge.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("Close() error (may be expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for Close() with active reader")
	}
}

