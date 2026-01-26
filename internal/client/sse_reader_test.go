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
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// stringReadCloser wraps a strings.Reader to implement io.ReadCloser
type stringReadCloser struct {
	*strings.Reader
	closed bool
}

func newStringReadCloser(s string) *stringReadCloser {
	return &stringReadCloser{Reader: strings.NewReader(s)}
}

func (s *stringReadCloser) Close() error {
	s.closed = true
	return nil
}

func TestNewSSEReader(t *testing.T) {
	body := newStringReadCloser("data: test\n\n")
	reader := NewSSEReader(body)

	if reader == nil {
		t.Fatal("Expected non-nil reader")
	}

	if reader.body != body {
		t.Error("Body not set correctly")
	}

	if reader.Closed() {
		t.Error("Reader should not be closed initially")
	}
}

func TestSSEReader_ReadEvent_Basic(t *testing.T) {
	body := newStringReadCloser("event: message\ndata: hello world\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	event, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event != "message" {
		t.Errorf("Expected event 'message', got '%s'", event)
	}

	if data != "hello world" {
		t.Errorf("Expected data 'hello world', got '%s'", data)
	}
}

func TestSSEReader_ReadEvent_MultipleLines(t *testing.T) {
	body := newStringReadCloser("data: line1\ndata: line2\ndata: line3\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	event, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event != "" {
		t.Errorf("Expected empty event type, got '%s'", event)
	}

	expected := "line1\nline2\nline3"
	if data != expected {
		t.Errorf("Expected data %q, got %q", expected, data)
	}
}

func TestSSEReader_ReadEvent_WithID(t *testing.T) {
	body := newStringReadCloser("id: 123\ndata: test\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	_, _, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if reader.LastEventID() != "123" {
		t.Errorf("Expected LastEventID '123', got '%s'", reader.LastEventID())
	}
}

func TestSSEReader_ReadEvent_MultipleEvents(t *testing.T) {
	body := newStringReadCloser("event: first\ndata: data1\n\nevent: second\ndata: data2\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	// First event
	event1, data1, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("First ReadEvent failed: %v", err)
	}

	if event1 != "first" || data1 != "data1" {
		t.Errorf("First event: expected ('first', 'data1'), got ('%s', '%s')", event1, data1)
	}

	// Second event
	event2, data2, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("Second ReadEvent failed: %v", err)
	}

	if event2 != "second" || data2 != "data2" {
		t.Errorf("Second event: expected ('second', 'data2'), got ('%s', '%s')", event2, data2)
	}
}

func TestSSEReader_ReadEvent_Comment(t *testing.T) {
	body := newStringReadCloser(": this is a comment\ndata: actual data\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	event, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	// Comment should be ignored
	if data != "actual data" {
		t.Errorf("Expected data 'actual data', got '%s'", data)
	}

	if event != "" {
		t.Errorf("Expected empty event, got '%s'", event)
	}
}

func TestSSEReader_ReadEvent_EmptyLines(t *testing.T) {
	// Multiple empty lines should not create empty events
	body := newStringReadCloser("\n\n\ndata: test\n\n\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	event, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if data != "test" {
		t.Errorf("Expected data 'test', got '%s'", data)
	}

	if event != "" {
		t.Errorf("Expected empty event, got '%s'", event)
	}
}

func TestSSEReader_ReadEvent_EOF(t *testing.T) {
	body := newStringReadCloser("")
	reader := NewSSEReader(body)
	defer reader.Close()

	_, _, err := reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("Expected io.EOF, got %v", err)
	}
}

func TestSSEReader_ReadEvent_EOFWithData(t *testing.T) {
	// Data with newlines but no blank line terminator at EOF
	// The SSE parser needs at least a newline to recognize the data line
	body := newStringReadCloser("data: final\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	_, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if data != "final" {
		t.Errorf("Expected data 'final', got '%s'", data)
	}
}

func TestSSEReader_ReadEvent_Closed(t *testing.T) {
	body := newStringReadCloser("data: test\n\n")
	reader := NewSSEReader(body)
	reader.Close()

	_, _, err := reader.ReadEvent()
	if !errors.Is(err, ErrSSEReaderClosed) {
		t.Errorf("Expected ErrSSEReaderClosed, got %v", err)
	}
}

func TestSSEReader_ReadMessage_Basic(t *testing.T) {
	body := newStringReadCloser("event: message\ndata: {\"test\":true}\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	data, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	expected := `{"test":true}`
	if string(data) != expected {
		t.Errorf("Expected data %q, got %q", expected, string(data))
	}
}

func TestSSEReader_ReadMessage_SkipsNonMessage(t *testing.T) {
	body := newStringReadCloser("event: ping\ndata: keepalive\n\nevent: message\ndata: actual\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	data, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	// Should skip ping and return message
	if string(data) != "actual" {
		t.Errorf("Expected data 'actual', got '%s'", string(data))
	}
}

func TestSSEReader_ReadMessage_EmptyEventIsMessage(t *testing.T) {
	// Empty event type defaults to "message"
	body := newStringReadCloser("data: no event type\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	data, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if string(data) != "no event type" {
		t.Errorf("Expected data 'no event type', got '%s'", string(data))
	}
}

func TestSSEReader_ReadEvents_Channel(t *testing.T) {
	body := newStringReadCloser("data: event1\n\ndata: event2\n\ndata: event3\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	events := reader.ReadEvents()

	received := []*SSEEvent{}
	for event := range events {
		received = append(received, event)
	}

	if len(received) != 3 {
		t.Errorf("Expected 3 events, got %d", len(received))
	}

	expectedData := []string{"event1", "event2", "event3"}
	for i, evt := range received {
		if evt.Data != expectedData[i] {
			t.Errorf("Event %d: expected data %q, got %q", i, expectedData[i], evt.Data)
		}
	}
}

func TestSSEReader_Close(t *testing.T) {
	body := newStringReadCloser("data: test\n\n")
	reader := NewSSEReader(body)

	if reader.Closed() {
		t.Error("Reader should not be closed initially")
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if !reader.Closed() {
		t.Error("Reader should be closed after Close()")
	}

	if !body.closed {
		t.Error("Underlying body should be closed")
	}

	// Second close should be safe
	err = reader.Close()
	if err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

func TestSSEEvent_IsMessage(t *testing.T) {
	tests := []struct {
		event     string
		isMessage bool
	}{
		{"", true},
		{"message", true},
		{"ping", false},
		{"error", false},
		{"endpoint", false},
	}

	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			evt := &SSEEvent{Event: tt.event}
			if evt.IsMessage() != tt.isMessage {
				t.Errorf("Expected IsMessage()=%v for event %q", tt.isMessage, tt.event)
			}
		})
	}
}

func TestSSEEvent_IsError(t *testing.T) {
	tests := []struct {
		event   string
		isError bool
	}{
		{"error", true},
		{"", false},
		{"message", false},
		{"ping", false},
	}

	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			evt := &SSEEvent{Event: tt.event}
			if evt.IsError() != tt.isError {
				t.Errorf("Expected IsError()=%v for event %q", tt.isError, tt.event)
			}
		})
	}
}

func TestSSEEvent_DataBytes(t *testing.T) {
	evt := &SSEEvent{Data: "test data"}
	data := evt.DataBytes()

	if !bytes.Equal(data, []byte("test data")) {
		t.Errorf("Expected 'test data', got %q", string(data))
	}
}

func TestParseSSELine(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		expectedField string
		expectedValue string
	}{
		{
			name:          "field with colon space",
			line:          "data: hello world",
			expectedField: "data",
			expectedValue: "hello world",
		},
		{
			name:          "field with colon no space",
			line:          "data:hello",
			expectedField: "data",
			expectedValue: "hello",
		},
		{
			name:          "field only no value",
			line:          "retry",
			expectedField: "retry",
			expectedValue: "",
		},
		{
			name:          "event type",
			line:          "event: message",
			expectedField: "event",
			expectedValue: "message",
		},
		{
			name:          "id field",
			line:          "id: 123",
			expectedField: "id",
			expectedValue: "123",
		},
		{
			name:          "empty value",
			line:          "data:",
			expectedField: "data",
			expectedValue: "",
		},
		{
			name:          "value with colons",
			line:          "data: http://example.com:8080",
			expectedField: "data",
			expectedValue: "http://example.com:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, value := parseSSELine([]byte(tt.line))

			if field != tt.expectedField {
				t.Errorf("Expected field %q, got %q", tt.expectedField, field)
			}

			if value != tt.expectedValue {
				t.Errorf("Expected value %q, got %q", tt.expectedValue, value)
			}
		})
	}
}

func TestSSEReader_ReadEvent_RetryField(t *testing.T) {
	// Retry field should be parsed without error (even if not used)
	body := newStringReadCloser("retry: 5000\ndata: test\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	_, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if data != "test" {
		t.Errorf("Expected data 'test', got '%s'", data)
	}
}

func TestSSEReader_ReadEvent_CRLFLineEndings(t *testing.T) {
	// Windows-style line endings
	body := newStringReadCloser("event: message\r\ndata: test\r\n\r\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	event, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event != "message" {
		t.Errorf("Expected event 'message', got '%s'", event)
	}

	if data != "test" {
		t.Errorf("Expected data 'test', got '%s'", data)
	}
}

func TestSSEReader_ReadEvent_MixedLineEndings(t *testing.T) {
	// Mix of LF and CRLF
	body := newStringReadCloser("event: message\ndata: line1\r\ndata: line2\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	event, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event != "message" {
		t.Errorf("Expected event 'message', got '%s'", event)
	}

	expected := "line1\nline2"
	if data != expected {
		t.Errorf("Expected data %q, got %q", expected, data)
	}
}

func TestSSEReader_LastEventID_Updates(t *testing.T) {
	body := newStringReadCloser("id: first\ndata: d1\n\nid: second\ndata: d2\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	// First event
	reader.ReadEvent()
	if reader.LastEventID() != "first" {
		t.Errorf("Expected LastEventID 'first', got '%s'", reader.LastEventID())
	}

	// Second event
	reader.ReadEvent()
	if reader.LastEventID() != "second" {
		t.Errorf("Expected LastEventID 'second', got '%s'", reader.LastEventID())
	}
}

func TestBufferedSSEReader_PeekEvent(t *testing.T) {
	body := newStringReadCloser("data: first\n\ndata: second\n\n")
	reader := NewBufferedSSEReader(body)
	defer reader.Close()

	// Peek should return but not consume
	evt1, err := reader.PeekEvent()
	if err != nil {
		t.Fatalf("PeekEvent failed: %v", err)
	}

	if evt1.Data != "first" {
		t.Errorf("Expected data 'first', got '%s'", evt1.Data)
	}

	// Peek again should return same event
	evt2, err := reader.PeekEvent()
	if err != nil {
		t.Fatalf("Second PeekEvent failed: %v", err)
	}

	if evt2.Data != "first" {
		t.Errorf("Expected same event on second peek, got '%s'", evt2.Data)
	}

	// Buffer size should be 1
	if reader.BufferSize() != 1 {
		t.Errorf("Expected buffer size 1, got %d", reader.BufferSize())
	}
}

func TestBufferedSSEReader_NextEvent(t *testing.T) {
	body := newStringReadCloser("data: first\n\ndata: second\n\n")
	reader := NewBufferedSSEReader(body)
	defer reader.Close()

	// Next should consume the event
	evt1, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("NextEvent failed: %v", err)
	}

	if evt1.Data != "first" {
		t.Errorf("Expected data 'first', got '%s'", evt1.Data)
	}

	// Next again should return the second event
	evt2, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("Second NextEvent failed: %v", err)
	}

	if evt2.Data != "second" {
		t.Errorf("Expected data 'second', got '%s'", evt2.Data)
	}
}

func TestBufferedSSEReader_PeekThenNext(t *testing.T) {
	body := newStringReadCloser("data: first\n\ndata: second\n\n")
	reader := NewBufferedSSEReader(body)
	defer reader.Close()

	// Peek first
	peeked, err := reader.PeekEvent()
	if err != nil {
		t.Fatalf("PeekEvent failed: %v", err)
	}

	// Next should return same as peeked
	next, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("NextEvent failed: %v", err)
	}

	if peeked.Data != next.Data {
		t.Errorf("Peek and Next should return same event")
	}

	// Next should return the second event
	second, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("Second NextEvent failed: %v", err)
	}

	if second.Data != "second" {
		t.Errorf("Expected data 'second', got '%s'", second.Data)
	}
}

func TestBufferedSSEReader_UnreadEvent(t *testing.T) {
	body := newStringReadCloser("data: first\n\ndata: second\n\n")
	reader := NewBufferedSSEReader(body)
	defer reader.Close()

	// Read first event
	evt1, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("NextEvent failed: %v", err)
	}

	// Unread it
	reader.UnreadEvent(evt1)

	// Buffer should have 1 event
	if reader.BufferSize() != 1 {
		t.Errorf("Expected buffer size 1, got %d", reader.BufferSize())
	}

	// Next should return the unread event
	evt2, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("NextEvent after unread failed: %v", err)
	}

	if evt2.Data != evt1.Data {
		t.Errorf("Expected unread event, got '%s'", evt2.Data)
	}
}

func TestBufferedSSEReader_DrainBuffer(t *testing.T) {
	body := newStringReadCloser("data: first\n\ndata: second\n\n")
	reader := NewBufferedSSEReader(body)
	defer reader.Close()

	// Peek to buffer an event
	reader.PeekEvent()

	// Unread another event
	reader.UnreadEvent(&SSEEvent{Data: "unread"})

	// Buffer should have 2 events
	if reader.BufferSize() != 2 {
		t.Errorf("Expected buffer size 2, got %d", reader.BufferSize())
	}

	// Drain buffer
	events := reader.DrainBuffer()

	if len(events) != 2 {
		t.Errorf("Expected 2 events from drain, got %d", len(events))
	}

	// Buffer should be empty
	if reader.BufferSize() != 0 {
		t.Errorf("Expected empty buffer after drain, got %d", reader.BufferSize())
	}
}

func TestSSEConstants(t *testing.T) {
	// Verify constants are set correctly
	if SSEEventMessage != "message" {
		t.Errorf("SSEEventMessage should be 'message', got '%s'", SSEEventMessage)
	}

	if SSEEventError != "error" {
		t.Errorf("SSEEventError should be 'error', got '%s'", SSEEventError)
	}

	if SSEEventPing != "ping" {
		t.Errorf("SSEEventPing should be 'ping', got '%s'", SSEEventPing)
	}

	if SSEEventEndpoint != "endpoint" {
		t.Errorf("SSEEventEndpoint should be 'endpoint', got '%s'", SSEEventEndpoint)
	}
}

func TestSSEReader_JSONData(t *testing.T) {
	jsonData := `{"jsonrpc":"2.0","result":{"name":"test"},"id":1}`
	body := newStringReadCloser("event: message\ndata: " + jsonData + "\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	data, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if string(data) != jsonData {
		t.Errorf("Expected JSON data %q, got %q", jsonData, string(data))
	}
}

func TestSSEReader_MultilineJSONData(t *testing.T) {
	// JSON split across multiple data lines
	// Each data: line adds its content followed by a newline when joined
	body := newStringReadCloser("data: {\"foo\":\ndata: \"bar\"}\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	_, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	// Multiple data lines are joined with newlines
	expected := "{\"foo\":\n\"bar\"}"
	if data != expected {
		t.Errorf("Expected data %q, got %q", expected, data)
	}
}

func TestSSEReader_LargeEvent(t *testing.T) {
	// Create a large data payload
	largeData := strings.Repeat("x", 100000)
	body := newStringReadCloser("data: " + largeData + "\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	_, data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if data != largeData {
		t.Errorf("Data length mismatch: expected %d, got %d", len(largeData), len(data))
	}
}

func TestSSEReader_OnlyComments(t *testing.T) {
	body := newStringReadCloser(": comment 1\n: comment 2\n\n")
	reader := NewSSEReader(body)
	defer reader.Close()

	// Should read until empty event (no data)
	_, _, err := reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("Expected EOF when only comments, got %v", err)
	}
}

func TestBufferedSSEReader_NextEventFromBuffer(t *testing.T) {
	body := newStringReadCloser("data: test\n\n")
	reader := NewBufferedSSEReader(body)
	defer reader.Close()

	// Peek to add to buffer
	reader.PeekEvent()

	// Add another to buffer
	reader.UnreadEvent(&SSEEvent{Data: "prepended"})

	// Should get prepended event first (it was prepended)
	evt, err := reader.NextEvent()
	if err != nil {
		t.Fatalf("NextEvent failed: %v", err)
	}

	if evt.Data != "prepended" {
		t.Errorf("Expected 'prepended', got '%s'", evt.Data)
	}
}

func TestSSEReader_NilBody(t *testing.T) {
	reader := &SSEReader{
		body: nil,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("Close with nil body should not error: %v", err)
	}
}
