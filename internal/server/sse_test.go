package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockResponseWriter implements http.ResponseWriter for testing
type mockResponseWriter struct {
	header     http.Header
	body       strings.Builder
	statusCode int
	mu         sync.Mutex
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		header:     make(http.Header),
		statusCode: 0,
	}
}

func (m *mockResponseWriter) Header() http.Header {
	return m.header
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.body.Write(b)
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

func (m *mockResponseWriter) GetBody() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.body.String()
}

// mockFlusher implements http.Flusher
type mockFlusher struct {
	flushed    bool
	flushCount int
	mu         sync.Mutex
}

func newMockFlusher() *mockFlusher {
	return &mockFlusher{}
}

func (m *mockFlusher) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed = true
	m.flushCount++
}

func (m *mockFlusher) GetFlushCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushCount
}

func (m *mockFlusher) WasFlushed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushed
}

// Helper to create an SSEWriter for testing
type testSSESetup struct {
	writer  *mockResponseWriter
	flusher *mockFlusher
	sse     *SSEWriter
}

func newTestSSESetup() *testSSESetup {
	w := newMockResponseWriter()
	f := newMockFlusher()
	return &testSSESetup{
		writer:  w,
		flusher: f,
		sse:     NewSSEWriter(w, f),
	}
}

func TestNewSSEWriter(t *testing.T) {
	w := newMockResponseWriter()
	f := newMockFlusher()

	sse := NewSSEWriter(w, f)

	if sse == nil {
		t.Fatal("NewSSEWriter() returned nil")
	}
	if sse.w != w {
		t.Error("writer not set correctly")
	}
	if sse.flusher != f {
		t.Error("flusher not set correctly")
	}
	if sse.closed {
		t.Error("SSEWriter should not be closed initially")
	}
}

func TestSSEWriter_SetHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	f := newMockFlusher()
	sse := NewSSEWriter(rr, f)

	sse.SetHeaders(rr)

	// Check Content-Type header
	contentType := rr.Header().Get("Content-Type")
	if contentType != ContentTypeSSE {
		t.Errorf("Content-Type = %s, want %s", contentType, ContentTypeSSE)
	}

	// Check Cache-Control header
	cacheControl := rr.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Cache-Control = %s, want no-cache", cacheControl)
	}

	// Check Connection header
	connection := rr.Header().Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Connection = %s, want keep-alive", connection)
	}

	// Check X-Accel-Buffering header (nginx)
	accelBuffering := rr.Header().Get("X-Accel-Buffering")
	if accelBuffering != "no" {
		t.Errorf("X-Accel-Buffering = %s, want no", accelBuffering)
	}

	// Check status code
	if rr.Code != 200 {
		t.Errorf("status code = %d, want 200", rr.Code)
	}

	// Check flush was called
	if !f.WasFlushed() {
		t.Error("Flush() should have been called")
	}
}

func TestSSEWriter_WriteEvent(t *testing.T) {
	testCases := []struct {
		name     string
		event    string
		data     string
		expected string
	}{
		{
			name:     "event with type and data",
			event:    "message",
			data:     `{"test":"data"}`,
			expected: "event: message\ndata: {\"test\":\"data\"}\n\n",
		},
		{
			name:     "event without type",
			event:    "",
			data:     `{"test":"data"}`,
			expected: "data: {\"test\":\"data\"}\n\n",
		},
		{
			name:     "event with empty data",
			event:    "ping",
			data:     "",
			expected: "event: ping\ndata: \n\n",
		},
		{
			name:     "event with multiline data",
			event:    "message",
			data:     "line1\nline2\nline3",
			expected: "event: message\ndata: line1\ndata: line2\ndata: line3\n\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newTestSSESetup()

			err := setup.sse.WriteEvent(tc.event, tc.data)
			if err != nil {
				t.Fatalf("WriteEvent() error = %v", err)
			}

			body := setup.writer.GetBody()
			if body != tc.expected {
				t.Errorf("WriteEvent() output = %q, want %q", body, tc.expected)
			}

			if !setup.flusher.WasFlushed() {
				t.Error("Flush() should have been called")
			}
		})
	}
}

func TestSSEWriter_WriteEvent_Closed(t *testing.T) {
	setup := newTestSSESetup()
	setup.sse.Close()

	err := setup.sse.WriteEvent("test", "data")
	if err == nil {
		t.Error("WriteEvent() should return error when closed")
	}
	if err.Error() != "SSE writer is closed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSSEWriter_WriteMessage(t *testing.T) {
	setup := newTestSSESetup()

	msg := []byte(`{"jsonrpc":"2.0","method":"test"}`)
	err := setup.sse.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	body := setup.writer.GetBody()
	expected := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"test\"}\n\n"
	if body != expected {
		t.Errorf("WriteMessage() output = %q, want %q", body, expected)
	}
}

func TestSSEWriter_WriteData(t *testing.T) {
	setup := newTestSSESetup()

	err := setup.sse.WriteData("test data")
	if err != nil {
		t.Fatalf("WriteData() error = %v", err)
	}

	body := setup.writer.GetBody()
	// WriteData uses empty event string, so no "event:" line
	expected := "data: test data\n\n"
	if body != expected {
		t.Errorf("WriteData() output = %q, want %q", body, expected)
	}
}

func TestSSEWriter_WriteEventWithID(t *testing.T) {
	testCases := []struct {
		name     string
		id       string
		event    string
		data     string
		expected string
	}{
		{
			name:     "event with id",
			id:       "123",
			event:    "message",
			data:     "test",
			expected: "id: 123\nevent: message\ndata: test\n\n",
		},
		{
			name:     "event without id",
			id:       "",
			event:    "message",
			data:     "test",
			expected: "event: message\ndata: test\n\n",
		},
		{
			name:     "event with id and no event type",
			id:       "456",
			event:    "",
			data:     "test",
			expected: "id: 456\ndata: test\n\n",
		},
		{
			name:     "event with id and empty data",
			id:       "789",
			event:    "ping",
			data:     "",
			expected: "id: 789\nevent: ping\ndata: \n\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newTestSSESetup()

			err := setup.sse.WriteEventWithID(tc.id, tc.event, tc.data)
			if err != nil {
				t.Fatalf("WriteEventWithID() error = %v", err)
			}

			body := setup.writer.GetBody()
			if body != tc.expected {
				t.Errorf("WriteEventWithID() output = %q, want %q", body, tc.expected)
			}
		})
	}
}

func TestSSEWriter_WriteEventWithID_Closed(t *testing.T) {
	setup := newTestSSESetup()
	setup.sse.Close()

	err := setup.sse.WriteEventWithID("123", "test", "data")
	if err == nil {
		t.Error("WriteEventWithID() should return error when closed")
	}
}

func TestSSEWriter_WriteRetry(t *testing.T) {
	setup := newTestSSESetup()

	err := setup.sse.WriteRetry(5000)
	if err != nil {
		t.Fatalf("WriteRetry() error = %v", err)
	}

	body := setup.writer.GetBody()
	expected := "retry: 5000\n\n"
	if body != expected {
		t.Errorf("WriteRetry() output = %q, want %q", body, expected)
	}

	if !setup.flusher.WasFlushed() {
		t.Error("Flush() should have been called")
	}
}

func TestSSEWriter_WriteRetry_Closed(t *testing.T) {
	setup := newTestSSESetup()
	setup.sse.Close()

	err := setup.sse.WriteRetry(5000)
	if err == nil {
		t.Error("WriteRetry() should return error when closed")
	}
}

func TestSSEWriter_WriteComment(t *testing.T) {
	setup := newTestSSESetup()

	err := setup.sse.WriteComment("keepalive")
	if err != nil {
		t.Fatalf("WriteComment() error = %v", err)
	}

	body := setup.writer.GetBody()
	expected := ": keepalive\n\n"
	if body != expected {
		t.Errorf("WriteComment() output = %q, want %q", body, expected)
	}

	if !setup.flusher.WasFlushed() {
		t.Error("Flush() should have been called")
	}
}

func TestSSEWriter_WriteComment_Closed(t *testing.T) {
	setup := newTestSSESetup()
	setup.sse.Close()

	err := setup.sse.WriteComment("test")
	if err == nil {
		t.Error("WriteComment() should return error when closed")
	}
}

func TestSSEWriter_Flush(t *testing.T) {
	setup := newTestSSESetup()

	setup.sse.Flush()

	if !setup.flusher.WasFlushed() {
		t.Error("Flush() should call the underlying flusher")
	}
}

func TestSSEWriter_Flush_NilFlusher(t *testing.T) {
	w := newMockResponseWriter()
	sse := NewSSEWriter(w, nil)

	// Should not panic
	sse.Flush()
}

func TestSSEWriter_Close(t *testing.T) {
	setup := newTestSSESetup()

	if setup.sse.IsClosed() {
		t.Error("SSEWriter should not be closed initially")
	}

	setup.sse.Close()

	if !setup.sse.IsClosed() {
		t.Error("SSEWriter should be closed after Close()")
	}
}

func TestSSEWriter_WriteSSEEvent(t *testing.T) {
	testCases := []struct {
		name     string
		event    SSEEvent
		expected string
	}{
		{
			name: "full event",
			event: SSEEvent{
				ID:    "123",
				Event: "message",
				Data:  "test data",
				Retry: 5000,
			},
			expected: "id: 123\nevent: message\nretry: 5000\ndata: test data\n\n",
		},
		{
			name: "event without ID",
			event: SSEEvent{
				Event: "notification",
				Data:  "test",
			},
			expected: "event: notification\ndata: test\n\n",
		},
		{
			name: "event without event type",
			event: SSEEvent{
				ID:   "456",
				Data: "test",
			},
			expected: "id: 456\ndata: test\n\n",
		},
		{
			name: "event without retry",
			event: SSEEvent{
				ID:    "789",
				Event: "test",
				Data:  "data",
			},
			expected: "id: 789\nevent: test\ndata: data\n\n",
		},
		{
			name: "event with empty data",
			event: SSEEvent{
				Event: "ping",
			},
			expected: "event: ping\ndata: \n\n",
		},
		{
			name: "event with multiline data",
			event: SSEEvent{
				Event: "message",
				Data:  "line1\nline2",
			},
			expected: "event: message\ndata: line1\ndata: line2\n\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newTestSSESetup()

			err := setup.sse.WriteSSEEvent(tc.event)
			if err != nil {
				t.Fatalf("WriteSSEEvent() error = %v", err)
			}

			body := setup.writer.GetBody()
			if body != tc.expected {
				t.Errorf("WriteSSEEvent() output = %q, want %q", body, tc.expected)
			}
		})
	}
}

func TestSSEWriter_WriteSSEEvent_Closed(t *testing.T) {
	setup := newTestSSESetup()
	setup.sse.Close()

	err := setup.sse.WriteSSEEvent(SSEEvent{Event: "test", Data: "data"})
	if err == nil {
		t.Error("WriteSSEEvent() should return error when closed")
	}
}

func TestSSEWriter_ConcurrentWrites(t *testing.T) {
	setup := newTestSSESetup()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numWrites = 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numWrites; j++ {
				setup.sse.WriteEvent("test", "data")
			}
		}(i)
	}

	wg.Wait()

	// Check that all writes completed
	body := setup.writer.GetBody()
	expectedWrites := numGoroutines * numWrites
	actualWrites := strings.Count(body, "event: test")

	if actualWrites != expectedWrites {
		t.Errorf("expected %d writes, got %d", expectedWrites, actualWrites)
	}
}

func TestSSEWriter_FlushCount(t *testing.T) {
	setup := newTestSSESetup()

	// Each write should flush
	setup.sse.WriteEvent("test1", "data1")
	setup.sse.WriteEvent("test2", "data2")
	setup.sse.WriteRetry(1000)
	setup.sse.WriteComment("comment")

	expectedFlushCount := 4
	actualFlushCount := setup.flusher.GetFlushCount()

	if actualFlushCount != expectedFlushCount {
		t.Errorf("flush count = %d, want %d", actualFlushCount, expectedFlushCount)
	}
}

func TestSSEEvent_Structure(t *testing.T) {
	event := SSEEvent{
		ID:    "test-id",
		Event: "test-event",
		Data:  "test-data",
		Retry: 1000,
	}

	if event.ID != "test-id" {
		t.Errorf("ID = %s, want test-id", event.ID)
	}
	if event.Event != "test-event" {
		t.Errorf("Event = %s, want test-event", event.Event)
	}
	if event.Data != "test-data" {
		t.Errorf("Data = %s, want test-data", event.Data)
	}
	if event.Retry != 1000 {
		t.Errorf("Retry = %d, want 1000", event.Retry)
	}
}

func TestSSEWriter_SetHeaders_WithNilFlusher(t *testing.T) {
	rr := httptest.NewRecorder()
	sse := NewSSEWriter(rr, nil)

	// Should not panic even with nil flusher
	sse.SetHeaders(rr)

	// Headers should still be set
	contentType := rr.Header().Get("Content-Type")
	if contentType != ContentTypeSSE {
		t.Errorf("Content-Type = %s, want %s", contentType, ContentTypeSSE)
	}
}

// Test that the HTTP header type is correctly used
func TestHTTPHeaderType(t *testing.T) {
	var h http.Header = make(map[string][]string)
	h.Set("Content-Type", ContentTypeSSE)

	if h.Get("Content-Type") != ContentTypeSSE {
		t.Error("http.Header not working as expected")
	}
}

func TestSSEWriter_IsClosed_Concurrent(t *testing.T) {
	setup := newTestSSESetup()

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Concurrent IsClosed calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				setup.sse.IsClosed()
			}
		}()
	}

	// Close in the middle
	go func() {
		setup.sse.Close()
	}()

	wg.Wait()
}

func TestSSEWriter_MultipleClose(t *testing.T) {
	setup := newTestSSESetup()

	// Multiple Close calls should not panic
	setup.sse.Close()
	setup.sse.Close()
	setup.sse.Close()

	if !setup.sse.IsClosed() {
		t.Error("SSEWriter should be closed")
	}
}
