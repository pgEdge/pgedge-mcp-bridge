package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
)

// TestCodec_NewCodec tests creating a new Codec.
func TestCodec_NewCodec(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}

	codec := NewCodec(r, w)
	if codec == nil {
		t.Fatal("NewCodec() returned nil")
	}
	if codec.reader == nil {
		t.Error("reader should not be nil")
	}
	if codec.writer == nil {
		t.Error("writer should not be nil")
	}
	if codec.maxMessageSize != 0 {
		t.Errorf("maxMessageSize = %d, want 0", codec.maxMessageSize)
	}
}

// TestCodec_NewCodecWithOptions tests creating a Codec with custom options.
func TestCodec_NewCodecWithOptions(t *testing.T) {
	r := strings.NewReader("")
	w := &bytes.Buffer{}
	maxSize := 1024

	codec := NewCodecWithOptions(r, w, maxSize)
	if codec == nil {
		t.Fatal("NewCodecWithOptions() returned nil")
	}
	if codec.maxMessageSize != maxSize {
		t.Errorf("maxMessageSize = %d, want %d", codec.maxMessageSize, maxSize)
	}
}

// TestCodec_ReadMessage tests reading messages from a reader.
func TestCodec_ReadMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "single message",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n",
			want:  []string{`{"jsonrpc":"2.0","id":1,"method":"test"}`},
		},
		{
			name: "multiple messages",
			input: `{"jsonrpc":"2.0","id":1,"method":"test1"}` + "\n" +
				`{"jsonrpc":"2.0","id":2,"method":"test2"}` + "\n",
			want: []string{
				`{"jsonrpc":"2.0","id":1,"method":"test1"}`,
				`{"jsonrpc":"2.0","id":2,"method":"test2"}`,
			},
		},
		{
			name:  "message without trailing newline",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			want:  []string{`{"jsonrpc":"2.0","id":1,"method":"test"}`},
		},
		{
			name: "messages with empty lines",
			input: `{"jsonrpc":"2.0","id":1,"method":"test1"}` + "\n" +
				"\n" +
				`{"jsonrpc":"2.0","id":2,"method":"test2"}` + "\n",
			want: []string{
				`{"jsonrpc":"2.0","id":1,"method":"test1"}`,
				`{"jsonrpc":"2.0","id":2,"method":"test2"}`,
			},
		},
		{
			name:  "message with whitespace",
			input: "  " + `{"jsonrpc":"2.0","id":1,"method":"test"}` + "  \n",
			want:  []string{`{"jsonrpc":"2.0","id":1,"method":"test"}`},
		},
		{
			name:  "empty input",
			input: "",
			want:  []string{}, // Empty input yields no messages (EOF is not an error)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			codec := NewCodec(r, nil)

			var messages []string
			for {
				msg, err := codec.ReadMessage()
				if err != nil {
					if err == io.EOF {
						break
					}
					if tt.wantErr {
						return
					}
					t.Fatalf("ReadMessage() error = %v", err)
				}
				messages = append(messages, string(msg))
			}

			if tt.wantErr {
				t.Error("expected error but got none")
				return
			}

			if len(messages) != len(tt.want) {
				t.Errorf("got %d messages, want %d", len(messages), len(tt.want))
				return
			}

			for i, want := range tt.want {
				if messages[i] != want {
					t.Errorf("message[%d] = %s, want %s", i, messages[i], want)
				}
			}
		})
	}
}

// TestCodec_ReadMessage_MaxSize tests max message size enforcement.
func TestCodec_ReadMessage_MaxSize(t *testing.T) {
	// Create a message larger than max size
	largeMessage := `{"jsonrpc":"2.0","id":1,"method":"test","params":{"data":"` + strings.Repeat("x", 100) + `"}}` + "\n"
	smallMessage := `{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n"

	tests := []struct {
		name    string
		input   string
		maxSize int
		wantErr bool
	}{
		{
			name:    "message within limit",
			input:   smallMessage,
			maxSize: 1000,
			wantErr: false,
		},
		{
			name:    "message exceeds limit",
			input:   largeMessage,
			maxSize: 50,
			wantErr: true,
		},
		{
			name:    "no limit",
			input:   largeMessage,
			maxSize: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			codec := NewCodecWithOptions(r, nil, tt.maxSize)

			_, err := codec.ReadMessage()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error for message exceeding max size")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCodec_WriteMessage tests writing messages to a writer.
func TestCodec_WriteMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     any
		want    string
		wantErr bool
	}{
		{
			name: "write request",
			msg: &Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Method:  "test",
			},
			want: `{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n",
		},
		{
			name: "write response",
			msg: &Response{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
			want: `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}` + "\n",
		},
		{
			name: "write notification",
			msg: &Notification{
				JSONRPC: "2.0",
				Method:  "notifications/test",
			},
			want: `{"jsonrpc":"2.0","method":"notifications/test"}` + "\n",
		},
		{
			name: "write map",
			msg: map[string]any{
				"key": "value",
			},
			want: `{"key":"value"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			codec := NewCodec(nil, buf)

			err := codec.WriteMessage(tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("WriteMessage() error = %v", err)
			}

			got := buf.String()
			if got != tt.want {
				t.Errorf("WriteMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCodec_WriteMessage_MaxSize tests max message size enforcement for writing.
func TestCodec_WriteMessage_MaxSize(t *testing.T) {
	largeMsg := map[string]string{
		"data": strings.Repeat("x", 100),
	}

	tests := []struct {
		name    string
		msg     any
		maxSize int
		wantErr bool
	}{
		{
			name:    "message within limit",
			msg:     map[string]string{"key": "value"},
			maxSize: 1000,
			wantErr: false,
		},
		{
			name:    "message exceeds limit",
			msg:     largeMsg,
			maxSize: 50,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			codec := NewCodecWithOptions(nil, buf, tt.maxSize)

			err := codec.WriteMessage(tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error for message exceeding max size")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCodec_WriteMessage_MarshalError tests error handling for unmarshalable types.
func TestCodec_WriteMessage_MarshalError(t *testing.T) {
	buf := &bytes.Buffer{}
	codec := NewCodec(nil, buf)

	// Create a value that cannot be marshaled (channel)
	ch := make(chan int)
	err := codec.WriteMessage(ch)
	if err == nil {
		t.Error("expected error for unmarshalable type")
	}
}

// TestCodec_WriteRaw tests writing raw bytes.
func TestCodec_WriteRaw(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    string
		maxSize int
		wantErr bool
	}{
		{
			name: "write raw JSON",
			data: []byte(`{"key":"value"}`),
			want: `{"key":"value"}` + "\n",
		},
		{
			name:    "exceeds max size",
			data:    []byte(strings.Repeat("x", 100)),
			maxSize: 50,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			codec := NewCodecWithOptions(nil, buf, tt.maxSize)

			err := codec.WriteRaw(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("WriteRaw() error = %v", err)
			}

			got := buf.String()
			if got != tt.want {
				t.Errorf("WriteRaw() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCodec_Concurrency tests concurrent read/write operations.
func TestCodec_Concurrency(t *testing.T) {
	// Create a pipe for concurrent read/write
	pr, pw := io.Pipe()
	// Use separate codecs for reader and writer since they use internal mutex
	// that would otherwise cause deadlock with io.Pipe
	readerCodec := NewCodec(pr, nil)
	writerCodec := NewCodec(nil, pw)

	var wg sync.WaitGroup
	numMessages := 10

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pw.Close()
		for i := 0; i < numMessages; i++ {
			msg := &Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(int64(i)),
				Method:  "test",
			}
			if err := writerCodec.WriteMessage(msg); err != nil {
				t.Errorf("WriteMessage() error = %v", err)
				return
			}
		}
	}()

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		count := 0
		for {
			_, err := readerCodec.ReadMessage()
			if err != nil {
				if err == io.EOF {
					break
				}
				t.Errorf("ReadMessage() error = %v", err)
				return
			}
			count++
		}
		if count != numMessages {
			t.Errorf("read %d messages, want %d", count, numMessages)
		}
	}()

	wg.Wait()
}

// TestParseMessage tests parsing generic JSON-RPC messages.
func TestParseMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType MessageType
		wantErr  bool
	}{
		{
			name:     "parse request",
			input:    `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			wantType: MessageTypeRequest,
		},
		{
			name:     "parse response with result",
			input:    `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}`,
			wantType: MessageTypeResponse,
		},
		{
			name:     "parse response with error",
			input:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantType: MessageTypeResponse,
		},
		{
			name:     "parse notification",
			input:    `{"jsonrpc":"2.0","method":"notifications/test"}`,
			wantType: MessageTypeNotification,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid json}`,
			wantErr: true,
		},
		{
			name:    "wrong jsonrpc version",
			input:   `{"jsonrpc":"1.0","id":1,"method":"test"}`,
			wantErr: true,
		},
		{
			name:    "missing jsonrpc version",
			input:   `{"id":1,"method":"test"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMessage() error = %v", err)
			}

			if msg.Type() != tt.wantType {
				t.Errorf("Type() = %s, want %s", msg.Type(), tt.wantType)
			}
		})
	}
}

// TestParseRequest tests parsing JSON-RPC requests.
func TestParseRequest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{
			name:   "valid request",
			input:  `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			wantID: "1",
		},
		{
			name:   "request with string id",
			input:  `{"jsonrpc":"2.0","id":"req-1","method":"test"}`,
			wantID: "req-1",
		},
		{
			name:   "request with params",
			input:  `{"jsonrpc":"2.0","id":1,"method":"test","params":{"key":"value"}}`,
			wantID: "1",
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "missing method",
			input:   `{"jsonrpc":"2.0","id":1}`,
			wantErr: true,
		},
		{
			name:    "missing id",
			input:   `{"jsonrpc":"2.0","method":"test"}`,
			wantErr: true,
		},
		{
			name:    "wrong version",
			input:   `{"jsonrpc":"1.0","id":1,"method":"test"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := ParseRequest([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRequest() error = %v", err)
			}

			if req.ID.String() != tt.wantID {
				t.Errorf("ID = %s, want %s", req.ID.String(), tt.wantID)
			}
		})
	}
}

// TestParseResponse tests parsing JSON-RPC responses.
func TestParseResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		isSuccess bool
		wantErr   bool
	}{
		{
			name:      "success response",
			input:     `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}`,
			wantID:    "1",
			isSuccess: true,
		},
		{
			name:      "error response",
			input:     `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantID:    "1",
			isSuccess: false,
		},
		{
			name:      "null result",
			input:     `{"jsonrpc":"2.0","id":"req-1","result":null}`,
			wantID:    "req-1",
			isSuccess: true,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "wrong version",
			input:   `{"jsonrpc":"1.0","id":1,"result":{}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := ParseResponse([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResponse() error = %v", err)
			}

			if resp.ID.String() != tt.wantID {
				t.Errorf("ID = %s, want %s", resp.ID.String(), tt.wantID)
			}
			if resp.IsSuccess() != tt.isSuccess {
				t.Errorf("IsSuccess() = %v, want %v", resp.IsSuccess(), tt.isSuccess)
			}
		})
	}
}

// TestParseNotification tests parsing JSON-RPC notifications.
func TestParseNotification(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantMethod string
		wantErr    bool
	}{
		{
			name:       "valid notification",
			input:      `{"jsonrpc":"2.0","method":"notifications/test"}`,
			wantMethod: "notifications/test",
		},
		{
			name:       "notification with params",
			input:      `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":0.5}}`,
			wantMethod: "notifications/progress",
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "missing method",
			input:   `{"jsonrpc":"2.0"}`,
			wantErr: true,
		},
		{
			name:    "wrong version",
			input:   `{"jsonrpc":"1.0","method":"test"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notif, err := ParseNotification([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseNotification() error = %v", err)
			}

			if notif.Method != tt.wantMethod {
				t.Errorf("Method = %s, want %s", notif.Method, tt.wantMethod)
			}
		})
	}
}

// TestIsRequest tests IsRequest function.
func TestIsRequest(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "is request",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			want:  true,
		},
		{
			name:  "is response",
			input: `{"jsonrpc":"2.0","id":1,"result":{}}`,
			want:  false,
		},
		{
			name:  "is notification",
			input: `{"jsonrpc":"2.0","method":"test"}`,
			want:  false,
		},
		{
			name:  "invalid JSON",
			input: `{invalid}`,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRequest([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsResponse tests IsResponse function.
func TestIsResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "is response with result",
			input: `{"jsonrpc":"2.0","id":1,"result":{}}`,
			want:  true,
		},
		{
			name:  "is response with error",
			input: `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`,
			want:  true,
		},
		{
			name:  "is request",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			want:  false,
		},
		{
			name:  "is notification",
			input: `{"jsonrpc":"2.0","method":"test"}`,
			want:  false,
		},
		{
			name:  "invalid JSON",
			input: `{invalid}`,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsResponse([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsNotification tests IsNotification function.
func TestIsNotification(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "is notification",
			input: `{"jsonrpc":"2.0","method":"notifications/test"}`,
			want:  true,
		},
		{
			name:  "is request",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			want:  false,
		},
		{
			name:  "is response",
			input: `{"jsonrpc":"2.0","id":1,"result":{}}`,
			want:  false,
		},
		{
			name:  "invalid JSON",
			input: `{invalid}`,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNotification([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsBatch tests IsBatch function.
func TestIsBatch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "is batch",
			input: `[{"jsonrpc":"2.0","id":1,"method":"test"}]`,
			want:  true,
		},
		{
			name:  "is single request",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			want:  false,
		},
		{
			name:  "empty array",
			input: `[]`,
			want:  true,
		},
		{
			name:  "with whitespace",
			input: `  [{"jsonrpc":"2.0","id":1,"method":"test"}]`,
			want:  true,
		},
		{
			name:  "empty string",
			input: ``,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBatch([]byte(tt.input))
			if got != tt.want {
				t.Errorf("IsBatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseBatch tests ParseBatch function.
func TestParseBatch(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "single item batch",
			input:     `[{"jsonrpc":"2.0","id":1,"method":"test"}]`,
			wantCount: 1,
		},
		{
			name:      "multiple items",
			input:     `[{"jsonrpc":"2.0","id":1,"method":"test1"},{"jsonrpc":"2.0","id":2,"method":"test2"}]`,
			wantCount: 2,
		},
		{
			name:      "empty batch",
			input:     `[]`,
			wantCount: 0,
		},
		{
			name:      "mixed batch",
			input:     `[{"jsonrpc":"2.0","id":1,"method":"test"},{"jsonrpc":"2.0","method":"notify"}]`,
			wantCount: 2,
		},
		{
			name:    "invalid JSON",
			input:   `[{invalid}]`,
			wantErr: true,
		},
		{
			name:    "not an array",
			input:   `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch, err := ParseBatch([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBatch() error = %v", err)
			}

			if len(batch) != tt.wantCount {
				t.Errorf("len(batch) = %d, want %d", len(batch), tt.wantCount)
			}
		})
	}
}

// TestEncodeMessage tests EncodeMessage function.
func TestEncodeMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     any
		wantErr bool
	}{
		{
			name: "encode request",
			msg: &Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Method:  "test",
			},
		},
		{
			name: "encode response",
			msg: &Response{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Result:  json.RawMessage(`{}`),
			},
		},
		{
			name:    "encode unmarshalable",
			msg:     make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeMessage(tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("EncodeMessage() error = %v", err)
			}

			if len(data) == 0 {
				t.Error("EncodeMessage() returned empty data")
			}
		})
	}
}

// TestDecodeParams tests DecodeParams function.
func TestDecodeParams(t *testing.T) {
	tests := []struct {
		name    string
		params  json.RawMessage
		wantErr bool
	}{
		{
			name:   "decode valid params",
			params: json.RawMessage(`{"name":"test","value":42}`),
		},
		{
			name:   "decode nil params",
			params: nil,
		},
		{
			name:    "decode invalid params",
			params:  json.RawMessage(`{invalid}`),
			wantErr: true,
		},
	}

	type TestParams struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DecodeParams[TestParams](tt.params)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeParams() error = %v", err)
			}

			if tt.params != nil && result == nil {
				t.Error("DecodeParams() returned nil for non-nil params")
			}
		})
	}
}

// TestDecodeResult tests DecodeResult function.
func TestDecodeResult(t *testing.T) {
	tests := []struct {
		name    string
		result  json.RawMessage
		wantErr bool
	}{
		{
			name:   "decode valid result",
			result: json.RawMessage(`{"status":"ok"}`),
		},
		{
			name:   "decode nil result",
			result: nil,
		},
		{
			name:    "decode invalid result",
			result:  json.RawMessage(`{invalid}`),
			wantErr: true,
		},
	}

	type TestResult struct {
		Status string `json:"status"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DecodeResult[TestResult](tt.result)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeResult() error = %v", err)
			}

			if tt.result != nil && result == nil {
				t.Error("DecodeResult() returned nil for non-nil result")
			}
		})
	}
}

// TestReadMessageFrom tests ReadMessageFrom function.
func TestReadMessageFrom(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "read message with newline",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n",
			want:  `{"jsonrpc":"2.0","id":1,"method":"test"}`,
		},
		{
			name:  "read message without newline (EOF)",
			input: `{"jsonrpc":"2.0","id":1,"method":"test"}`,
			want:  `{"jsonrpc":"2.0","id":1,"method":"test"}`,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			msg, err := ReadMessageFrom(r)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadMessageFrom() error = %v", err)
			}

			if string(msg) != tt.want {
				t.Errorf("ReadMessageFrom() = %s, want %s", msg, tt.want)
			}
		})
	}
}

// TestWriteMessageTo tests WriteMessageTo function.
func TestWriteMessageTo(t *testing.T) {
	tests := []struct {
		name    string
		msg     any
		want    string
		wantErr bool
	}{
		{
			name: "write request",
			msg: &Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Method:  "test",
			},
			want: `{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n",
		},
		{
			name:    "write unmarshalable",
			msg:     make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteMessageTo(buf, tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("WriteMessageTo() error = %v", err)
			}

			got := buf.String()
			if got != tt.want {
				t.Errorf("WriteMessageTo() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMessageReader tests MessageReader iteration.
func TestMessageReader(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"test1"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"test2"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"test3"}` + "\n"

	r := strings.NewReader(input)
	reader := NewMessageReader(r)

	var messages [][]byte
	for reader.Next() {
		messages = append(messages, reader.Message())
	}

	if reader.Err() != nil {
		t.Fatalf("Err() = %v", reader.Err())
	}

	if len(messages) != 3 {
		t.Errorf("read %d messages, want 3", len(messages))
	}
}

// TestMessageReader_Empty tests MessageReader with empty input.
func TestMessageReader_Empty(t *testing.T) {
	r := strings.NewReader("")
	reader := NewMessageReader(r)

	if reader.Next() {
		t.Error("Next() should return false for empty input")
	}

	if reader.Err() != nil {
		t.Errorf("Err() = %v, want nil", reader.Err())
	}
}

// TestMessageWriter tests MessageWriter.
func TestMessageWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewMessageWriter(buf)

	msg := &Request{
		JSONRPC: "2.0",
		ID:      NewNumberID(1),
		Method:  "test",
	}

	if err := writer.Write(msg); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	want := `{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n"
	if buf.String() != want {
		t.Errorf("Write() = %q, want %q", buf.String(), want)
	}
}

// TestMessageWriter_WriteRaw tests MessageWriter.WriteRaw.
func TestMessageWriter_WriteRaw(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewMessageWriter(buf)

	data := []byte(`{"key":"value"}`)
	if err := writer.WriteRaw(data); err != nil {
		t.Fatalf("WriteRaw() error = %v", err)
	}

	want := `{"key":"value"}` + "\n"
	if buf.String() != want {
		t.Errorf("WriteRaw() = %q, want %q", buf.String(), want)
	}
}

// TestCodec_WriteMessage_WriterError tests error handling when writer fails.
func TestCodec_WriteMessage_WriterError(t *testing.T) {
	// Use a writer that always fails
	failWriter := &failingWriter{}
	codec := NewCodec(nil, failWriter)

	msg := &Request{
		JSONRPC: "2.0",
		ID:      NewNumberID(1),
		Method:  "test",
	}

	err := codec.WriteMessage(msg)
	if err == nil {
		t.Error("expected error when writer fails")
	}
}

// TestCodec_WriteRaw_WriterError tests error handling when writer fails for raw writes.
func TestCodec_WriteRaw_WriterError(t *testing.T) {
	failWriter := &failingWriter{}
	codec := NewCodec(nil, failWriter)

	err := codec.WriteRaw([]byte(`{"key":"value"}`))
	if err == nil {
		t.Error("expected error when writer fails")
	}
}

// failingWriter is a writer that always returns an error.
type failingWriter struct{}

func (w *failingWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}

// TestParseMessage_EdgeCases tests ParseMessage with edge cases.
func TestParseMessage_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "empty object",
			input:   `{}`,
			wantErr: true,
		},
		{
			name:    "only jsonrpc field",
			input:   `{"jsonrpc":"2.0"}`,
			wantErr: false, // Valid but unknown type
		},
		{
			name:  "request with null params",
			input: `{"jsonrpc":"2.0","id":1,"method":"test","params":null}`,
		},
		{
			name:  "response with empty result",
			input: `{"jsonrpc":"2.0","id":1,"result":{}}`,
		},
		{
			name:  "large id number",
			input: `{"jsonrpc":"2.0","id":9223372036854775807,"method":"test"}`,
		},
		{
			name:  "unicode in method",
			input: `{"jsonrpc":"2.0","id":1,"method":"test/\u4e2d\u6587"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMessage() error = %v", err)
			}
			if msg == nil {
				t.Error("ParseMessage() returned nil")
			}
		})
	}
}

// TestBatchProcessing tests processing a batch of messages.
func TestBatchProcessing(t *testing.T) {
	batch := `[
		{"jsonrpc":"2.0","id":1,"method":"test1"},
		{"jsonrpc":"2.0","method":"notify"},
		{"jsonrpc":"2.0","id":2,"method":"test2","params":{"key":"value"}}
	]`

	messages, err := ParseBatch([]byte(batch))
	if err != nil {
		t.Fatalf("ParseBatch() error = %v", err)
	}

	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}

	// First message should be a request
	msg1, err := ParseMessage(messages[0])
	if err != nil {
		t.Fatalf("ParseMessage(0) error = %v", err)
	}
	if msg1.Type() != MessageTypeRequest {
		t.Errorf("message 0 type = %s, want request", msg1.Type())
	}

	// Second message should be a notification
	msg2, err := ParseMessage(messages[1])
	if err != nil {
		t.Fatalf("ParseMessage(1) error = %v", err)
	}
	if msg2.Type() != MessageTypeNotification {
		t.Errorf("message 1 type = %s, want notification", msg2.Type())
	}

	// Third message should be a request with params
	msg3, err := ParseMessage(messages[2])
	if err != nil {
		t.Fatalf("ParseMessage(2) error = %v", err)
	}
	if msg3.Type() != MessageTypeRequest {
		t.Errorf("message 2 type = %s, want request", msg3.Type())
	}
	if msg3.Params == nil {
		t.Error("message 2 should have params")
	}
}

// TestMalformedJSON tests handling of various malformed JSON inputs.
func TestMalformedJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"unclosed brace", `{"jsonrpc":"2.0"`},
		{"unclosed quote", `{"jsonrpc":"2.0}`},
		{"trailing comma", `{"jsonrpc":"2.0","id":1,}`},
		{"double comma", `{"jsonrpc":"2.0",,"id":1}`},
		{"missing colon", `{"jsonrpc" "2.0"}`},
		{"wrong brackets", `["jsonrpc":"2.0"]`},
		{"random text", `hello world`},
		{"number", `42`},
		{"boolean", `true`},
		{"null", `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMessage([]byte(tt.input))
			if err == nil {
				t.Errorf("expected error for malformed JSON: %s", tt.input)
			}
		})
	}
}
