package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// SSEWriter provides methods for writing Server-Sent Events to an HTTP response.
// It handles the SSE protocol format including event types, data fields, and keepalives.
type SSEWriter struct {
	// w is the underlying response writer.
	w http.ResponseWriter

	// flusher is used to flush the response after each event.
	flusher http.Flusher

	// mu protects concurrent writes.
	mu sync.Mutex

	// closed indicates whether the writer has been closed.
	closed bool
}

// NewSSEWriter creates a new SSEWriter wrapping the given ResponseWriter and Flusher.
func NewSSEWriter(w http.ResponseWriter, f http.Flusher) *SSEWriter {
	return &SSEWriter{
		w:       w,
		flusher: f,
	}
}

// SetHeaders sets the required HTTP headers for SSE connections.
// This should be called before writing any events.
func (s *SSEWriter) SetHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentTypeSSE)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Write headers
	w.WriteHeader(http.StatusOK)

	// Flush to ensure headers are sent
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// WriteEvent writes an SSE event with the given event type and data.
// The event parameter is the event type (empty string for default "message" event).
// The data parameter is the event data, which will be properly escaped.
func (s *SSEWriter) WriteEvent(event, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	var sb strings.Builder

	// Write event type if specified
	if event != "" {
		sb.WriteString("event: ")
		sb.WriteString(event)
		sb.WriteString("\n")
	}

	// Write data field(s)
	// Split data by newlines and write each line as a separate "data:" field
	if data != "" {
		lines := strings.Split(data, "\n")
		for _, line := range lines {
			sb.WriteString("data: ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	} else {
		// Write empty data field for events without data (like ping)
		sb.WriteString("data: \n")
	}

	// End of event
	sb.WriteString("\n")

	// Write to response
	if _, err := fmt.Fprint(s.w, sb.String()); err != nil {
		return fmt.Errorf("writing SSE event: %w", err)
	}

	// Flush to send immediately
	s.Flush()

	return nil
}

// WriteMessage writes raw message data as an SSE "message" event.
// This is a convenience method for the common case of sending JSON-RPC messages.
func (s *SSEWriter) WriteMessage(msg []byte) error {
	return s.WriteEvent("message", string(msg))
}

// WriteData writes raw data without an event type (uses default "message" event).
func (s *SSEWriter) WriteData(data string) error {
	return s.WriteEvent("", data)
}

// WriteID writes an SSE event with an ID field.
// IDs allow clients to resume from the last received event after reconnection.
func (s *SSEWriter) WriteEventWithID(id, event, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	var sb strings.Builder

	// Write ID if specified
	if id != "" {
		sb.WriteString("id: ")
		sb.WriteString(id)
		sb.WriteString("\n")
	}

	// Write event type if specified
	if event != "" {
		sb.WriteString("event: ")
		sb.WriteString(event)
		sb.WriteString("\n")
	}

	// Write data field(s)
	if data != "" {
		lines := strings.Split(data, "\n")
		for _, line := range lines {
			sb.WriteString("data: ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("data: \n")
	}

	// End of event
	sb.WriteString("\n")

	// Write to response
	if _, err := fmt.Fprint(s.w, sb.String()); err != nil {
		return fmt.Errorf("writing SSE event: %w", err)
	}

	// Flush to send immediately
	s.Flush()

	return nil
}

// WriteRetry writes a retry field indicating how long (in milliseconds)
// the client should wait before reconnecting.
func (s *SSEWriter) WriteRetry(milliseconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	retry := fmt.Sprintf("retry: %d\n\n", milliseconds)
	if _, err := fmt.Fprint(s.w, retry); err != nil {
		return fmt.Errorf("writing SSE retry: %w", err)
	}

	s.Flush()
	return nil
}

// WriteComment writes an SSE comment.
// Comments are prefixed with a colon and can be used for keepalives.
func (s *SSEWriter) WriteComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	// Comments start with a colon
	commentLine := fmt.Sprintf(": %s\n\n", comment)
	if _, err := fmt.Fprint(s.w, commentLine); err != nil {
		return fmt.Errorf("writing SSE comment: %w", err)
	}

	s.Flush()
	return nil
}

// Flush flushes the response to ensure data is sent immediately.
func (s *SSEWriter) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// Close marks the writer as closed.
// After calling Close, all subsequent write operations will return an error.
func (s *SSEWriter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// IsClosed returns whether the writer has been closed.
func (s *SSEWriter) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	// ID is the event identifier for reconnection tracking.
	ID string

	// Event is the event type (empty string for default "message" event).
	Event string

	// Data is the event data payload.
	Data string

	// Retry is the reconnection time in milliseconds (0 to skip).
	Retry int
}

// WriteSSEEvent writes a complete SSEEvent to the writer.
func (s *SSEWriter) WriteSSEEvent(evt SSEEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("SSE writer is closed")
	}

	var sb strings.Builder

	// Write ID if specified
	if evt.ID != "" {
		sb.WriteString("id: ")
		sb.WriteString(evt.ID)
		sb.WriteString("\n")
	}

	// Write event type if specified
	if evt.Event != "" {
		sb.WriteString("event: ")
		sb.WriteString(evt.Event)
		sb.WriteString("\n")
	}

	// Write retry if specified
	if evt.Retry > 0 {
		sb.WriteString(fmt.Sprintf("retry: %d\n", evt.Retry))
	}

	// Write data field(s)
	if evt.Data != "" {
		lines := strings.Split(evt.Data, "\n")
		for _, line := range lines {
			sb.WriteString("data: ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("data: \n")
	}

	// End of event
	sb.WriteString("\n")

	// Write to response
	if _, err := fmt.Fprint(s.w, sb.String()); err != nil {
		return fmt.Errorf("writing SSE event: %w", err)
	}

	// Flush to send immediately
	s.Flush()

	return nil
}
