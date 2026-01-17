package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Codec provides methods for encoding and decoding JSON-RPC 2.0 messages.
// It handles newline-delimited JSON (NDJSON) format commonly used for
// streaming JSON-RPC over stdio.
type Codec struct {
	// mu protects concurrent access to the codec.
	mu sync.Mutex

	// reader is a buffered reader for reading messages.
	reader *bufio.Reader

	// writer is the underlying writer for writing messages.
	writer io.Writer

	// maxMessageSize is the maximum allowed message size in bytes.
	// Zero means no limit.
	maxMessageSize int
}

// NewCodec creates a new Codec with the given reader and writer.
func NewCodec(r io.Reader, w io.Writer) *Codec {
	return &Codec{
		reader: bufio.NewReader(r),
		writer: w,
	}
}

// NewCodecWithOptions creates a new Codec with custom options.
func NewCodecWithOptions(r io.Reader, w io.Writer, maxMessageSize int) *Codec {
	return &Codec{
		reader:         bufio.NewReader(r),
		writer:         w,
		maxMessageSize: maxMessageSize,
	}
}

// ReadMessage reads a single newline-delimited JSON message from the reader.
// It blocks until a complete message is available or an error occurs.
func (c *Codec) ReadMessage() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.readMessageLocked()
}

// readMessageLocked reads a message without acquiring the lock.
func (c *Codec) readMessageLocked() ([]byte, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(line) > 0 {
			// Handle the case where the last message doesn't have a trailing newline
			return bytes.TrimSpace(line), nil
		}
		return nil, err
	}

	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		// Skip empty lines
		return c.readMessageLocked()
	}

	if c.maxMessageSize > 0 && len(line) > c.maxMessageSize {
		return nil, fmt.Errorf("message size %d exceeds maximum %d", len(line), c.maxMessageSize)
	}

	return line, nil
}

// WriteMessage writes a message as newline-delimited JSON to the writer.
func (c *Codec) WriteMessage(msg any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if c.maxMessageSize > 0 && len(data) > c.maxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), c.maxMessageSize)
	}

	// Write the JSON followed by a newline
	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if _, err := c.writer.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// WriteRaw writes raw bytes followed by a newline to the writer.
func (c *Codec) WriteRaw(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxMessageSize > 0 && len(data) > c.maxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), c.maxMessageSize)
	}

	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if _, err := c.writer.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// ParseMessage parses a JSON-RPC 2.0 message from raw bytes.
// It returns a Message struct that can be used to determine the message type
// and convert to the appropriate concrete type.
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	// Validate JSON-RPC version
	if msg.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("invalid JSON-RPC version: %s", msg.JSONRPC)
	}

	return &msg, nil
}

// ParseRequest parses a JSON-RPC 2.0 request from raw bytes.
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	return &req, nil
}

// ParseResponse parses a JSON-RPC 2.0 response from raw bytes.
func ParseResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("invalid JSON-RPC version: %s", resp.JSONRPC)
	}

	return &resp, nil
}

// ParseNotification parses a JSON-RPC 2.0 notification from raw bytes.
func ParseNotification(data []byte) (*Notification, error) {
	var notif Notification
	if err := json.Unmarshal(data, &notif); err != nil {
		return nil, fmt.Errorf("failed to parse notification: %w", err)
	}

	if err := notif.Validate(); err != nil {
		return nil, err
	}

	return &notif, nil
}

// IsRequest returns true if the data appears to be a JSON-RPC request.
// A request has method, id, and no result/error fields.
func IsRequest(data []byte) bool {
	msg, err := ParseMessage(data)
	if err != nil {
		return false
	}
	return msg.Type() == MessageTypeRequest
}

// IsResponse returns true if the data appears to be a JSON-RPC response.
// A response has id and either result or error fields.
func IsResponse(data []byte) bool {
	msg, err := ParseMessage(data)
	if err != nil {
		return false
	}
	return msg.Type() == MessageTypeResponse
}

// IsNotification returns true if the data appears to be a JSON-RPC notification.
// A notification has method but no id field.
func IsNotification(data []byte) bool {
	msg, err := ParseMessage(data)
	if err != nil {
		return false
	}
	return msg.Type() == MessageTypeNotification
}

// IsBatch returns true if the data appears to be a JSON-RPC batch request.
func IsBatch(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && data[0] == '['
}

// ParseBatch parses a JSON-RPC batch from raw bytes.
// It returns the raw messages in the batch, which can then be individually parsed.
func ParseBatch(data []byte) ([]json.RawMessage, error) {
	var batch []json.RawMessage
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, fmt.Errorf("failed to parse batch: %w", err)
	}
	return batch, nil
}

// EncodeMessage encodes a message to JSON bytes.
func EncodeMessage(msg any) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeParams decodes the params field of a request or notification into the target type.
func DecodeParams[T any](params json.RawMessage) (*T, error) {
	if params == nil {
		return nil, nil
	}

	var target T
	if err := json.Unmarshal(params, &target); err != nil {
		return nil, fmt.Errorf("failed to decode params: %w", err)
	}
	return &target, nil
}

// DecodeResult decodes the result field of a response into the target type.
func DecodeResult[T any](result json.RawMessage) (*T, error) {
	if result == nil {
		return nil, nil
	}

	var target T
	if err := json.Unmarshal(result, &target); err != nil {
		return nil, fmt.Errorf("failed to decode result: %w", err)
	}
	return &target, nil
}

// ReadMessageFrom reads a single newline-delimited JSON message from the given reader.
// This is a convenience function for one-off reads without creating a Codec.
func ReadMessageFrom(r io.Reader) ([]byte, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(line) > 0 {
			return bytes.TrimSpace(line), nil
		}
		return nil, err
	}
	return bytes.TrimSpace(line), nil
}

// WriteMessageTo writes a message as newline-delimited JSON to the given writer.
// This is a convenience function for one-off writes without creating a Codec.
func WriteMessageTo(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// MessageReader provides an iterator-like interface for reading messages.
type MessageReader struct {
	codec *Codec
	err   error
	data  []byte
}

// NewMessageReader creates a new MessageReader from the given reader.
func NewMessageReader(r io.Reader) *MessageReader {
	return &MessageReader{
		codec: NewCodec(r, nil),
	}
}

// Next reads the next message. Returns true if a message was read successfully,
// false if there are no more messages or an error occurred.
func (r *MessageReader) Next() bool {
	r.data, r.err = r.codec.ReadMessage()
	return r.err == nil
}

// Message returns the current message data.
func (r *MessageReader) Message() []byte {
	return r.data
}

// Err returns any error that occurred during reading.
func (r *MessageReader) Err() error {
	if r.err == io.EOF {
		return nil
	}
	return r.err
}

// MessageWriter provides a buffered interface for writing messages.
type MessageWriter struct {
	codec *Codec
}

// NewMessageWriter creates a new MessageWriter to the given writer.
func NewMessageWriter(w io.Writer) *MessageWriter {
	return &MessageWriter{
		codec: NewCodec(nil, w),
	}
}

// Write writes a message to the underlying writer.
func (w *MessageWriter) Write(msg any) error {
	return w.codec.WriteMessage(msg)
}

// WriteRaw writes raw bytes to the underlying writer.
func (w *MessageWriter) WriteRaw(data []byte) error {
	return w.codec.WriteRaw(data)
}
