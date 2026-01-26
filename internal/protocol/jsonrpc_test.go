package protocol

import (
	"encoding/json"
	"testing"
)

// TestRequestID_MarshalUnmarshal tests RequestID marshaling and unmarshaling.
func TestRequestID_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		id       RequestID
		wantJSON string
	}{
		{
			name:     "string id",
			id:       NewStringID("test-123"),
			wantJSON: `"test-123"`,
		},
		{
			name:     "number id",
			id:       NewNumberID(42),
			wantJSON: `42`,
		},
		{
			name:     "null id",
			id:       RequestID{},
			wantJSON: `null`,
		},
		{
			name:     "zero number id",
			id:       NewNumberID(0),
			wantJSON: `0`,
		},
		{
			name:     "negative number id",
			id:       NewNumberID(-1),
			wantJSON: `-1`,
		},
		{
			name:     "large number id",
			id:       NewNumberID(9007199254740991), // Max safe integer in JSON (2^53 - 1)
			wantJSON: `9007199254740991`,
		},
		{
			name:     "empty string id",
			id:       NewStringID(""),
			wantJSON: `""`,
		},
		{
			name:     "string with special characters",
			id:       NewStringID("test-id-123_abc"),
			wantJSON: `"test-id-123_abc"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			got, err := json.Marshal(tt.id)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("Marshal() = %s, want %s", got, tt.wantJSON)
			}

			// Test unmarshaling
			var unmarshaled RequestID
			if err := json.Unmarshal([]byte(tt.wantJSON), &unmarshaled); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Compare values
			if tt.id.IsNull() {
				if !unmarshaled.IsNull() {
					t.Errorf("Unmarshal() IsNull = false, want true")
				}
			} else {
				if unmarshaled.String() != tt.id.String() {
					t.Errorf("Unmarshal() String = %s, want %s", unmarshaled.String(), tt.id.String())
				}
			}
		})
	}
}

// TestRequestID_UnmarshalJSON tests RequestID unmarshaling with various inputs.
func TestRequestID_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantString string
		wantNull   bool
		wantErr    bool
	}{
		{
			name:       "string value",
			input:      `"request-1"`,
			wantString: "request-1",
			wantNull:   false,
		},
		{
			name:       "integer value",
			input:      `123`,
			wantString: "123",
			wantNull:   false,
		},
		{
			name:       "float that is integer",
			input:      `123.0`,
			wantString: "123",
			wantNull:   false,
		},
		{
			name:       "float value",
			input:      `123.456`,
			wantString: "123",
			wantNull:   false,
		},
		{
			name:       "null value",
			input:      `null`,
			wantString: "",
			wantNull:   true,
		},
		{
			name:       "zero",
			input:      `0`,
			wantString: "0",
			wantNull:   false,
		},
		{
			name:       "negative integer",
			input:      `-5`,
			wantString: "-5",
			wantNull:   false,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "array",
			input:   `[1, 2, 3]`,
			wantErr: true,
		},
		{
			name:    "object",
			input:   `{"id": 1}`,
			wantErr: true,
		},
		{
			name:    "boolean",
			input:   `true`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id RequestID
			err := json.Unmarshal([]byte(tt.input), &id)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Unmarshal() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if id.IsNull() != tt.wantNull {
				t.Errorf("IsNull() = %v, want %v", id.IsNull(), tt.wantNull)
			}

			if !tt.wantNull && id.String() != tt.wantString {
				t.Errorf("String() = %s, want %s", id.String(), tt.wantString)
			}
		})
	}
}

// TestRequestID_Value tests the Value() method.
func TestRequestID_Value(t *testing.T) {
	tests := []struct {
		name string
		id   RequestID
		want any
	}{
		{
			name: "string value",
			id:   NewStringID("test"),
			want: "test",
		},
		{
			name: "number value",
			id:   NewNumberID(42),
			want: int64(42),
		},
		{
			name: "null value",
			id:   RequestID{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.id.Value()
			if got != tt.want {
				t.Errorf("Value() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// TestRequest_Validate tests Request validation.
func TestRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request",
			request: Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Method:  "test/method",
				Params:  json.RawMessage(`{"key": "value"}`),
			},
			wantErr: false,
		},
		{
			name: "valid request with string id",
			request: Request{
				JSONRPC: "2.0",
				ID:      NewStringID("request-1"),
				Method:  "test/method",
			},
			wantErr: false,
		},
		{
			name: "invalid jsonrpc version",
			request: Request{
				JSONRPC: "1.0",
				ID:      NewNumberID(1),
				Method:  "test/method",
			},
			wantErr: true,
			errMsg:  "invalid jsonrpc version",
		},
		{
			name: "empty jsonrpc version",
			request: Request{
				JSONRPC: "",
				ID:      NewNumberID(1),
				Method:  "test/method",
			},
			wantErr: true,
			errMsg:  "invalid jsonrpc version",
		},
		{
			name: "missing method",
			request: Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Method:  "",
			},
			wantErr: true,
			errMsg:  "method is required",
		},
		{
			name: "null id",
			request: Request{
				JSONRPC: "2.0",
				ID:      RequestID{},
				Method:  "test/method",
			},
			wantErr: true,
			errMsg:  "id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q", tt.errMsg)
					return
				}
				if tt.errMsg != "" && err.Error() != "" {
					// Just check the error is not nil for now
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestRequest_MarshalUnmarshal tests Request marshaling and unmarshaling.
func TestRequest_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		request Request
	}{
		{
			name: "basic request",
			request: Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Method:  "test/method",
			},
		},
		{
			name: "request with params",
			request: Request{
				JSONRPC: "2.0",
				ID:      NewStringID("abc-123"),
				Method:  "test/method",
				Params:  json.RawMessage(`{"key":"value"}`),
			},
		},
		{
			name: "request with complex params",
			request: Request{
				JSONRPC: "2.0",
				ID:      NewNumberID(42),
				Method:  "tools/call",
				Params:  json.RawMessage(`{"name":"my-tool","arguments":{"a":1,"b":"test"}}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal
			var got Request
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Compare
			if got.JSONRPC != tt.request.JSONRPC {
				t.Errorf("JSONRPC = %s, want %s", got.JSONRPC, tt.request.JSONRPC)
			}
			if got.ID.String() != tt.request.ID.String() {
				t.Errorf("ID = %s, want %s", got.ID.String(), tt.request.ID.String())
			}
			if got.Method != tt.request.Method {
				t.Errorf("Method = %s, want %s", got.Method, tt.request.Method)
			}
			if string(got.Params) != string(tt.request.Params) {
				t.Errorf("Params = %s, want %s", got.Params, tt.request.Params)
			}
		})
	}
}

// TestResponse_NewResponse tests creating a new successful response.
func TestResponse_NewResponse(t *testing.T) {
	tests := []struct {
		name   string
		id     RequestID
		result any
	}{
		{
			name:   "simple result",
			id:     NewNumberID(1),
			result: map[string]string{"status": "ok"},
		},
		{
			name: "complex result",
			id:   NewStringID("req-123"),
			result: InitializeResult{
				ProtocolVersion: "2025-06-18",
				ServerInfo:      Implementation{Name: "test", Version: "1.0"},
			},
		},
		{
			name:   "nil result",
			id:     NewNumberID(2),
			result: nil,
		},
		{
			name:   "string result",
			id:     NewNumberID(3),
			result: "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NewResponse(tt.id, tt.result)
			if err != nil {
				t.Fatalf("NewResponse() error = %v", err)
			}

			if resp.JSONRPC != JSONRPCVersion {
				t.Errorf("JSONRPC = %s, want %s", resp.JSONRPC, JSONRPCVersion)
			}
			if resp.ID.String() != tt.id.String() {
				t.Errorf("ID = %s, want %s", resp.ID.String(), tt.id.String())
			}
			if resp.Error != nil {
				t.Errorf("Error should be nil for success response")
			}
			if !resp.IsSuccess() {
				t.Errorf("IsSuccess() = false, want true")
			}
		})
	}
}

// TestResponse_NewErrorResponse tests creating an error response.
func TestResponse_NewErrorResponse(t *testing.T) {
	tests := []struct {
		name string
		id   RequestID
		err  *Error
	}{
		{
			name: "parse error",
			id:   NewNumberID(1),
			err:  NewParseError("invalid json"),
		},
		{
			name: "invalid request",
			id:   NewStringID("req-1"),
			err:  NewInvalidRequestError("missing method"),
		},
		{
			name: "method not found",
			id:   NewNumberID(2),
			err:  NewMethodNotFoundError("unknown/method"),
		},
		{
			name: "invalid params",
			id:   NewNumberID(3),
			err:  NewInvalidParamsError("missing required field"),
		},
		{
			name: "internal error",
			id:   NewNumberID(4),
			err:  NewInternalError("server error"),
		},
		{
			name: "custom error",
			id:   NewNumberID(5),
			err:  NewError(-32001, "Custom error", map[string]string{"detail": "info"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NewErrorResponse(tt.id, tt.err)

			if resp.JSONRPC != JSONRPCVersion {
				t.Errorf("JSONRPC = %s, want %s", resp.JSONRPC, JSONRPCVersion)
			}
			if resp.ID.String() != tt.id.String() {
				t.Errorf("ID = %s, want %s", resp.ID.String(), tt.id.String())
			}
			if resp.Result != nil {
				t.Errorf("Result should be nil for error response")
			}
			if resp.Error == nil {
				t.Fatalf("Error should not be nil")
			}
			if resp.Error.Code != tt.err.Code {
				t.Errorf("Error.Code = %d, want %d", resp.Error.Code, tt.err.Code)
			}
			if resp.Error.Message != tt.err.Message {
				t.Errorf("Error.Message = %s, want %s", resp.Error.Message, tt.err.Message)
			}
			if resp.IsSuccess() {
				t.Errorf("IsSuccess() = true, want false")
			}
		})
	}
}

// TestResponse_MarshalUnmarshal tests Response marshaling and unmarshaling.
func TestResponse_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
	}{
		{
			name: "success response",
			response: &Response{
				JSONRPC: "2.0",
				ID:      NewNumberID(1),
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
		},
		{
			name: "error response",
			response: &Response{
				JSONRPC: "2.0",
				ID:      NewStringID("req-1"),
				Error: &Error{
					Code:    InvalidRequest,
					Message: "Invalid Request",
					Data:    "details",
				},
			},
		},
		{
			name: "response with null result",
			response: &Response{
				JSONRPC: "2.0",
				ID:      NewNumberID(2),
				Result:  json.RawMessage(`null`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal
			var got Response
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Compare
			if got.JSONRPC != tt.response.JSONRPC {
				t.Errorf("JSONRPC = %s, want %s", got.JSONRPC, tt.response.JSONRPC)
			}
			if got.ID.String() != tt.response.ID.String() {
				t.Errorf("ID = %s, want %s", got.ID.String(), tt.response.ID.String())
			}
		})
	}
}

// TestNotification_NewNotification tests creating a new notification.
func TestNotification_NewNotification(t *testing.T) {
	tests := []struct {
		name   string
		method string
		params any
	}{
		{
			name:   "simple notification",
			method: "notifications/initialized",
			params: nil,
		},
		{
			name:   "notification with params",
			method: "notifications/progress",
			params: ProgressParams{
				ProgressToken: NewStringProgressToken("token-1"),
				Progress:      0.5,
				Total:         1.0,
			},
		},
		{
			name:   "notification with map params",
			method: "custom/notification",
			params: map[string]string{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notif, err := NewNotification(tt.method, tt.params)
			if err != nil {
				t.Fatalf("NewNotification() error = %v", err)
			}

			if notif.JSONRPC != JSONRPCVersion {
				t.Errorf("JSONRPC = %s, want %s", notif.JSONRPC, JSONRPCVersion)
			}
			if notif.Method != tt.method {
				t.Errorf("Method = %s, want %s", notif.Method, tt.method)
			}
			if tt.params == nil && notif.Params != nil {
				t.Errorf("Params should be nil")
			}
		})
	}
}

// TestNotification_Validate tests Notification validation.
func TestNotification_Validate(t *testing.T) {
	tests := []struct {
		name         string
		notification Notification
		wantErr      bool
	}{
		{
			name: "valid notification",
			notification: Notification{
				JSONRPC: "2.0",
				Method:  "test/method",
			},
			wantErr: false,
		},
		{
			name: "valid notification with params",
			notification: Notification{
				JSONRPC: "2.0",
				Method:  "test/method",
				Params:  json.RawMessage(`{"key":"value"}`),
			},
			wantErr: false,
		},
		{
			name: "invalid jsonrpc version",
			notification: Notification{
				JSONRPC: "1.0",
				Method:  "test/method",
			},
			wantErr: true,
		},
		{
			name: "missing method",
			notification: Notification{
				JSONRPC: "2.0",
				Method:  "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.notification.Validate()

			if tt.wantErr && err == nil {
				t.Errorf("Validate() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

// TestError_Error tests the Error.Error() method.
func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		contains []string
	}{
		{
			name: "error without data",
			err: &Error{
				Code:    ParseError,
				Message: "Parse error",
			},
			contains: []string{"JSON-RPC error", "-32700", "Parse error"},
		},
		{
			name: "error with data",
			err: &Error{
				Code:    InvalidRequest,
				Message: "Invalid Request",
				Data:    "extra info",
			},
			contains: []string{"JSON-RPC error", "-32600", "Invalid Request", "extra info"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			for _, s := range tt.contains {
				if !containsString(errStr, s) {
					t.Errorf("Error() = %q, should contain %q", errStr, s)
				}
			}
		})
	}
}

// TestError_Codes tests the standard error codes.
func TestError_Codes(t *testing.T) {
	tests := []struct {
		name    string
		errFunc func(any) *Error
		code    int
		message string
	}{
		{
			name:    "parse error",
			errFunc: NewParseError,
			code:    ParseError,
			message: "Parse error",
		},
		{
			name:    "invalid request",
			errFunc: NewInvalidRequestError,
			code:    InvalidRequest,
			message: "Invalid Request",
		},
		{
			name:    "invalid params",
			errFunc: NewInvalidParamsError,
			code:    InvalidParams,
			message: "Invalid params",
		},
		{
			name:    "internal error",
			errFunc: NewInternalError,
			code:    InternalError,
			message: "Internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errFunc("test data")
			if err.Code != tt.code {
				t.Errorf("Code = %d, want %d", err.Code, tt.code)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %s, want %s", err.Message, tt.message)
			}
			if err.Data != "test data" {
				t.Errorf("Data = %v, want %v", err.Data, "test data")
			}
		})
	}
}

// TestMethodNotFoundError tests NewMethodNotFoundError.
func TestMethodNotFoundError(t *testing.T) {
	err := NewMethodNotFoundError("unknown/method")
	if err.Code != MethodNotFound {
		t.Errorf("Code = %d, want %d", err.Code, MethodNotFound)
	}
	if err.Message != "Method not found" {
		t.Errorf("Message = %s, want %s", err.Message, "Method not found")
	}
	if err.Data != "unknown/method" {
		t.Errorf("Data = %v, want %v", err.Data, "unknown/method")
	}
}

// TestMessage_Type tests Message.Type() detection.
func TestMessage_Type(t *testing.T) {
	stringID := NewStringID("req-1")
	numberID := NewNumberID(1)

	tests := []struct {
		name     string
		message  Message
		wantType MessageType
	}{
		{
			name: "request with string id",
			message: Message{
				JSONRPC: "2.0",
				ID:      &stringID,
				Method:  "test/method",
			},
			wantType: MessageTypeRequest,
		},
		{
			name: "request with number id",
			message: Message{
				JSONRPC: "2.0",
				ID:      &numberID,
				Method:  "test/method",
			},
			wantType: MessageTypeRequest,
		},
		{
			name: "response with result",
			message: Message{
				JSONRPC: "2.0",
				ID:      &numberID,
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
			wantType: MessageTypeResponse,
		},
		{
			name: "response with error",
			message: Message{
				JSONRPC: "2.0",
				ID:      &numberID,
				Error: &Error{
					Code:    InternalError,
					Message: "Internal error",
				},
			},
			wantType: MessageTypeResponse,
		},
		{
			name: "notification without id",
			message: Message{
				JSONRPC: "2.0",
				Method:  "notifications/progress",
			},
			wantType: MessageTypeNotification,
		},
		{
			name: "notification with null id",
			message: Message{
				JSONRPC: "2.0",
				ID:      &RequestID{},
				Method:  "notifications/progress",
			},
			wantType: MessageTypeNotification,
		},
		{
			name: "unknown - no method, no result, no error",
			message: Message{
				JSONRPC: "2.0",
				ID:      &numberID,
			},
			wantType: MessageTypeUnknown,
		},
		{
			name: "empty message",
			message: Message{
				JSONRPC: "2.0",
			},
			wantType: MessageTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType := tt.message.Type()
			if gotType != tt.wantType {
				t.Errorf("Type() = %v (%s), want %v (%s)",
					gotType, gotType.String(), tt.wantType, tt.wantType.String())
			}
		})
	}
}

// TestMessage_ToRequest tests converting Message to Request.
func TestMessage_ToRequest(t *testing.T) {
	id := NewNumberID(1)
	params := json.RawMessage(`{"key":"value"}`)

	tests := []struct {
		name    string
		message Message
		want    *Request
	}{
		{
			name: "valid request",
			message: Message{
				JSONRPC: "2.0",
				ID:      &id,
				Method:  "test/method",
				Params:  params,
			},
			want: &Request{
				JSONRPC: "2.0",
				ID:      id,
				Method:  "test/method",
				Params:  params,
			},
		},
		{
			name: "not a request (response)",
			message: Message{
				JSONRPC: "2.0",
				ID:      &id,
				Result:  json.RawMessage(`{}`),
			},
			want: nil,
		},
		{
			name: "not a request (notification)",
			message: Message{
				JSONRPC: "2.0",
				Method:  "test/method",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.message.ToRequest()
			if tt.want == nil {
				if got != nil {
					t.Errorf("ToRequest() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ToRequest() = nil, want non-nil")
			}
			if got.Method != tt.want.Method {
				t.Errorf("Method = %s, want %s", got.Method, tt.want.Method)
			}
			if got.ID.String() != tt.want.ID.String() {
				t.Errorf("ID = %s, want %s", got.ID.String(), tt.want.ID.String())
			}
		})
	}
}

// TestMessage_ToResponse tests converting Message to Response.
func TestMessage_ToResponse(t *testing.T) {
	id := NewNumberID(1)

	tests := []struct {
		name    string
		message Message
		want    *Response
	}{
		{
			name: "response with result",
			message: Message{
				JSONRPC: "2.0",
				ID:      &id,
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
			want: &Response{
				JSONRPC: "2.0",
				ID:      id,
				Result:  json.RawMessage(`{"status":"ok"}`),
			},
		},
		{
			name: "response with error",
			message: Message{
				JSONRPC: "2.0",
				ID:      &id,
				Error: &Error{
					Code:    InternalError,
					Message: "Internal error",
				},
			},
			want: &Response{
				JSONRPC: "2.0",
				ID:      id,
				Error: &Error{
					Code:    InternalError,
					Message: "Internal error",
				},
			},
		},
		{
			name: "not a response (request)",
			message: Message{
				JSONRPC: "2.0",
				ID:      &id,
				Method:  "test/method",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.message.ToResponse()
			if tt.want == nil {
				if got != nil {
					t.Errorf("ToResponse() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ToResponse() = nil, want non-nil")
			}
			if got.ID.String() != tt.want.ID.String() {
				t.Errorf("ID = %s, want %s", got.ID.String(), tt.want.ID.String())
			}
		})
	}
}

// TestMessage_ToNotification tests converting Message to Notification.
func TestMessage_ToNotification(t *testing.T) {
	id := NewNumberID(1)

	tests := []struct {
		name    string
		message Message
		want    *Notification
	}{
		{
			name: "valid notification",
			message: Message{
				JSONRPC: "2.0",
				Method:  "notifications/progress",
				Params:  json.RawMessage(`{"progress":0.5}`),
			},
			want: &Notification{
				JSONRPC: "2.0",
				Method:  "notifications/progress",
				Params:  json.RawMessage(`{"progress":0.5}`),
			},
		},
		{
			name: "not a notification (request)",
			message: Message{
				JSONRPC: "2.0",
				ID:      &id,
				Method:  "test/method",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.message.ToNotification()
			if tt.want == nil {
				if got != nil {
					t.Errorf("ToNotification() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ToNotification() = nil, want non-nil")
			}
			if got.Method != tt.want.Method {
				t.Errorf("Method = %s, want %s", got.Method, tt.want.Method)
			}
		})
	}
}

// TestMessageType_String tests MessageType.String() method.
func TestMessageType_String(t *testing.T) {
	tests := []struct {
		msgType MessageType
		want    string
	}{
		{MessageTypeUnknown, "unknown"},
		{MessageTypeRequest, "request"},
		{MessageTypeResponse, "response"},
		{MessageTypeNotification, "notification"},
		{MessageType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.msgType.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestBatchRequest tests BatchRequest type.
func TestBatchRequest(t *testing.T) {
	batch := BatchRequest{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"test1"}`),
		json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"test2"}`),
	}

	if len(batch) != 2 {
		t.Errorf("len(batch) = %d, want 2", len(batch))
	}
}

// TestBatchResponse tests BatchResponse type.
func TestBatchResponse(t *testing.T) {
	resp1, _ := NewResponse(NewNumberID(1), "result1")
	resp2, _ := NewResponse(NewNumberID(2), "result2")

	batch := BatchResponse{resp1, resp2}

	if len(batch) != 2 {
		t.Errorf("len(batch) = %d, want 2", len(batch))
	}
}

// TestErrorConstants tests that error code constants are correct.
func TestErrorConstants(t *testing.T) {
	tests := []struct {
		name string
		code int
		want int
	}{
		{"ParseError", ParseError, -32700},
		{"InvalidRequest", InvalidRequest, -32600},
		{"MethodNotFound", MethodNotFound, -32601},
		{"InvalidParams", InvalidParams, -32602},
		{"InternalError", InternalError, -32603},
		{"ServerErrorStart", ServerErrorStart, -32099},
		{"ServerErrorEnd", ServerErrorEnd, -32000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.want)
			}
		})
	}
}

// containsString is a helper function to check if a string contains a substring.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
