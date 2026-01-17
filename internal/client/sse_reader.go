package client

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
)

// SSE event types as defined in the Server-Sent Events specification.
const (
	// SSEEventMessage is the default event type for messages.
	SSEEventMessage = "message"

	// SSEEventError is the event type for error messages.
	SSEEventError = "error"

	// SSEEventPing is the event type for keepalive pings.
	SSEEventPing = "ping"

	// SSEEventEndpoint is the event type for endpoint updates (MCP specific).
	SSEEventEndpoint = "endpoint"
)

// SSE parsing errors.
var (
	// ErrSSEReaderClosed indicates the reader has been closed.
	ErrSSEReaderClosed = errors.New("sse reader is closed")

	// ErrInvalidSSEFormat indicates the data is not valid SSE format.
	ErrInvalidSSEFormat = errors.New("invalid SSE format")

	// ErrEmptyEvent indicates an empty event was received.
	ErrEmptyEvent = errors.New("empty event")
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	// Event is the event type (e.g., "message", "error").
	// Empty string defaults to "message".
	Event string

	// Data is the event data. Multiple data lines are joined with newlines.
	Data string

	// ID is the event ID, used for reconnection.
	ID string

	// Retry is the suggested reconnection time in milliseconds.
	Retry int
}

// IsMessage returns true if this is a message event.
func (e *SSEEvent) IsMessage() bool {
	return e.Event == "" || e.Event == SSEEventMessage
}

// IsError returns true if this is an error event.
func (e *SSEEvent) IsError() bool {
	return e.Event == SSEEventError
}

// DataBytes returns the event data as bytes.
func (e *SSEEvent) DataBytes() []byte {
	return []byte(e.Data)
}

// SSEReader reads Server-Sent Events from an HTTP response body.
// It implements buffered reading and proper SSE parsing according to
// the W3C specification.
type SSEReader struct {
	// Underlying body reader
	body io.ReadCloser

	// Buffered reader for line-based reading
	reader *bufio.Reader

	// Current event being built (events can span multiple lines)
	currentEvent *SSEEvent

	// Last event ID for reconnection
	lastEventID string

	// State management
	closed   bool
	closedMu sync.RWMutex
}

// NewSSEReader creates a new SSE reader from an HTTP response body.
func NewSSEReader(body io.ReadCloser) *SSEReader {
	return &SSEReader{
		body:   body,
		reader: bufio.NewReaderSize(body, 64*1024), // 64KB buffer
	}
}

// ReadEvent reads and returns the next complete SSE event.
// It parses the SSE format and handles multi-line data fields.
// Returns the event type, data, and any error.
func (r *SSEReader) ReadEvent() (event string, data string, err error) {
	r.closedMu.RLock()
	if r.closed {
		r.closedMu.RUnlock()
		return "", "", ErrSSEReaderClosed
	}
	r.closedMu.RUnlock()

	// Reset current event
	r.currentEvent = &SSEEvent{}
	var dataBuffer bytes.Buffer

	for {
		line, err := r.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// If we have accumulated data, return it as the final event
				if dataBuffer.Len() > 0 {
					r.currentEvent.Data = strings.TrimSuffix(dataBuffer.String(), "\n")
					return r.currentEvent.Event, r.currentEvent.Data, nil
				}
				return "", "", io.EOF
			}
			return "", "", err
		}

		// Remove trailing newline
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))

		// Empty line signals end of event
		if len(line) == 0 {
			if dataBuffer.Len() > 0 {
				// Remove trailing newline from data
				r.currentEvent.Data = strings.TrimSuffix(dataBuffer.String(), "\n")
				return r.currentEvent.Event, r.currentEvent.Data, nil
			}
			// Continue reading if no data accumulated yet
			continue
		}

		// Parse the field
		if bytes.HasPrefix(line, []byte(":")) {
			// Comment line, ignore
			continue
		}

		field, value := parseSSELine(line)
		switch field {
		case "event":
			r.currentEvent.Event = value
		case "data":
			dataBuffer.WriteString(value)
			dataBuffer.WriteByte('\n')
		case "id":
			r.currentEvent.ID = value
			r.lastEventID = value
		case "retry":
			// Parse retry value (milliseconds)
			// We don't use this directly but store it
			// r.currentEvent.Retry = parseRetry(value)
		}
	}
}

// ReadMessage reads the next "message" event and returns its data.
// This is a convenience method that filters for message events.
func (r *SSEReader) ReadMessage() ([]byte, error) {
	for {
		event, data, err := r.ReadEvent()
		if err != nil {
			return nil, err
		}

		// Check if this is a message event (empty event type = message)
		if event == "" || event == SSEEventMessage {
			return []byte(data), nil
		}

		// Skip non-message events (like ping, endpoint, etc.)
		// Could optionally handle them here
	}
}

// ReadEvents returns a channel that yields SSE events.
// The channel is closed when the stream ends or an error occurs.
func (r *SSEReader) ReadEvents() <-chan *SSEEvent {
	events := make(chan *SSEEvent, 10)

	go func() {
		defer close(events)

		for {
			event, data, err := r.ReadEvent()
			if err != nil {
				return
			}

			evt := &SSEEvent{
				Event: event,
				Data:  data,
				ID:    r.lastEventID,
			}

			events <- evt
		}
	}()

	return events
}

// LastEventID returns the ID of the last received event.
// This can be used for reconnection.
func (r *SSEReader) LastEventID() string {
	return r.lastEventID
}

// Close closes the SSE reader and the underlying body.
func (r *SSEReader) Close() error {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	if r.body != nil {
		return r.body.Close()
	}

	return nil
}

// Closed returns true if the reader has been closed.
func (r *SSEReader) Closed() bool {
	r.closedMu.RLock()
	defer r.closedMu.RUnlock()
	return r.closed
}

// parseSSELine parses a single SSE line into field and value.
// According to the SSE spec, lines are in the format "field: value" or "field:value".
func parseSSELine(line []byte) (field string, value string) {
	idx := bytes.IndexByte(line, ':')
	if idx == -1 {
		// No colon means the entire line is the field name with empty value
		return string(line), ""
	}

	field = string(line[:idx])
	value = string(line[idx+1:])

	// Remove leading space from value if present (per SSE spec)
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}

	return field, value
}

// BufferedSSEReader provides additional buffering capabilities for SSE streams.
// It can peek at upcoming events without consuming them.
type BufferedSSEReader struct {
	*SSEReader

	// Buffer for peeked events
	buffer []*SSEEvent
	bufMu  sync.Mutex
}

// NewBufferedSSEReader creates a new buffered SSE reader.
func NewBufferedSSEReader(body io.ReadCloser) *BufferedSSEReader {
	return &BufferedSSEReader{
		SSEReader: NewSSEReader(body),
		buffer:    make([]*SSEEvent, 0),
	}
}

// PeekEvent returns the next event without consuming it.
func (r *BufferedSSEReader) PeekEvent() (*SSEEvent, error) {
	r.bufMu.Lock()
	defer r.bufMu.Unlock()

	if len(r.buffer) > 0 {
		return r.buffer[0], nil
	}

	event, data, err := r.SSEReader.ReadEvent()
	if err != nil {
		return nil, err
	}

	evt := &SSEEvent{
		Event: event,
		Data:  data,
		ID:    r.lastEventID,
	}

	r.buffer = append(r.buffer, evt)
	return evt, nil
}

// NextEvent returns and consumes the next event.
func (r *BufferedSSEReader) NextEvent() (*SSEEvent, error) {
	r.bufMu.Lock()

	if len(r.buffer) > 0 {
		evt := r.buffer[0]
		r.buffer = r.buffer[1:]
		r.bufMu.Unlock()
		return evt, nil
	}
	r.bufMu.Unlock()

	event, data, err := r.SSEReader.ReadEvent()
	if err != nil {
		return nil, err
	}

	return &SSEEvent{
		Event: event,
		Data:  data,
		ID:    r.lastEventID,
	}, nil
}

// UnreadEvent pushes an event back to the buffer to be read again.
func (r *BufferedSSEReader) UnreadEvent(evt *SSEEvent) {
	r.bufMu.Lock()
	defer r.bufMu.Unlock()

	// Prepend to buffer
	r.buffer = append([]*SSEEvent{evt}, r.buffer...)
}

// BufferSize returns the number of events currently buffered.
func (r *BufferedSSEReader) BufferSize() int {
	r.bufMu.Lock()
	defer r.bufMu.Unlock()
	return len(r.buffer)
}

// DrainBuffer returns and clears all buffered events.
func (r *BufferedSSEReader) DrainBuffer() []*SSEEvent {
	r.bufMu.Lock()
	defer r.bufMu.Unlock()

	events := r.buffer
	r.buffer = make([]*SSEEvent, 0)
	return events
}
