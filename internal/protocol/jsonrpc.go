/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package protocol provides JSON-RPC 2.0 and MCP protocol types for the MCP HTTP bridge.
package protocol

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 version constant.
const JSONRPCVersion = "2.0"

// Standard JSON-RPC 2.0 error codes.
const (
	// ParseError indicates invalid JSON was received by the server.
	ParseError = -32700

	// InvalidRequest indicates the JSON sent is not a valid Request object.
	InvalidRequest = -32600

	// MethodNotFound indicates the method does not exist or is not available.
	MethodNotFound = -32601

	// InvalidParams indicates invalid method parameter(s).
	InvalidParams = -32602

	// InternalError indicates an internal JSON-RPC error.
	InternalError = -32603

	// ServerErrorStart is the start of the server error range.
	ServerErrorStart = -32099

	// ServerErrorEnd is the end of the server error range.
	ServerErrorEnd = -32000
)

// RequestID represents a JSON-RPC request identifier that can be a string or number.
// According to the JSON-RPC 2.0 specification, the id can be a String, Number, or Null.
type RequestID struct {
	value any
}

// NewStringID creates a RequestID from a string value.
func NewStringID(s string) RequestID {
	return RequestID{value: s}
}

// NewNumberID creates a RequestID from a numeric value.
func NewNumberID(n int64) RequestID {
	return RequestID{value: n}
}

// IsNull returns true if the RequestID is null or unset.
func (id RequestID) IsNull() bool {
	return id.value == nil
}

// String returns the string representation of the RequestID.
func (id RequestID) String() string {
	switch v := id.value.(type) {
	case string:
		return v
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		return ""
	}
}

// Value returns the underlying value of the RequestID.
func (id RequestID) Value() any {
	return id.value
}

// MarshalJSON implements json.Marshaler for RequestID.
func (id RequestID) MarshalJSON() ([]byte, error) {
	if id.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(id.value)
}

// UnmarshalJSON implements json.Unmarshaler for RequestID.
func (id *RequestID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		id.value = nil
		return nil
	}

	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id.value = s
		return nil
	}

	// Try to unmarshal as number
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		// Check if it's an integer
		if n == float64(int64(n)) {
			id.value = int64(n)
		} else {
			id.value = n
		}
		return nil
	}

	return fmt.Errorf("invalid request id: %s", string(data))
}

// Request represents a JSON-RPC 2.0 request object.
type Request struct {
	// JSONRPC specifies the version of the JSON-RPC protocol, must be "2.0".
	JSONRPC string `json:"jsonrpc"`

	// ID is the identifier established by the client.
	ID RequestID `json:"id"`

	// Method is the name of the method to be invoked.
	Method string `json:"method"`

	// Params holds the parameter values to be used during the invocation of the method.
	Params json.RawMessage `json:"params,omitempty"`
}

// Validate checks if the request is valid according to JSON-RPC 2.0 specification.
func (r *Request) Validate() error {
	if r.JSONRPC != JSONRPCVersion {
		return fmt.Errorf("invalid jsonrpc version: %s", r.JSONRPC)
	}
	if r.Method == "" {
		return fmt.Errorf("method is required")
	}
	if r.ID.IsNull() {
		return fmt.Errorf("id is required for requests")
	}
	return nil
}

// Response represents a JSON-RPC 2.0 response object.
type Response struct {
	// JSONRPC specifies the version of the JSON-RPC protocol, must be "2.0".
	JSONRPC string `json:"jsonrpc"`

	// ID is the identifier matching the request.
	ID RequestID `json:"id"`

	// Result contains the result of the method invocation.
	// This member is required on success and must not exist if there was an error.
	Result json.RawMessage `json:"result,omitempty"`

	// Error contains the error object if there was an error.
	// This member is required on error and must not exist if there was no error.
	Error *Error `json:"error,omitempty"`
}

// NewResponse creates a successful response with the given id and result.
func NewResponse(id RequestID, result any) (*Response, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  data,
	}, nil
}

// NewErrorResponse creates an error response with the given id and error.
func NewErrorResponse(id RequestID, err *Error) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   err,
	}
}

// IsSuccess returns true if the response indicates success (no error).
func (r *Response) IsSuccess() bool {
	return r.Error == nil
}

// Notification represents a JSON-RPC 2.0 notification object.
// A notification is a request object without an "id" member.
type Notification struct {
	// JSONRPC specifies the version of the JSON-RPC protocol, must be "2.0".
	JSONRPC string `json:"jsonrpc"`

	// Method is the name of the method to be invoked.
	Method string `json:"method"`

	// Params holds the parameter values to be used during the invocation of the method.
	Params json.RawMessage `json:"params,omitempty"`
}

// NewNotification creates a new notification with the given method and params.
func NewNotification(method string, params any) (*Notification, error) {
	var data json.RawMessage
	if params != nil {
		var err error
		data, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}
	return &Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  data,
	}, nil
}

// Validate checks if the notification is valid according to JSON-RPC 2.0 specification.
func (n *Notification) Validate() error {
	if n.JSONRPC != JSONRPCVersion {
		return fmt.Errorf("invalid jsonrpc version: %s", n.JSONRPC)
	}
	if n.Method == "" {
		return fmt.Errorf("method is required")
	}
	return nil
}

// Error represents a JSON-RPC 2.0 error object.
type Error struct {
	// Code is a number that indicates the error type that occurred.
	Code int `json:"code"`

	// Message is a short description of the error.
	Message string `json:"message"`

	// Data is a primitive or structured value that contains additional
	// information about the error. This may be omitted.
	Data any `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("JSON-RPC error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewError creates a new JSON-RPC error with the given code, message, and optional data.
func NewError(code int, message string, data any) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// NewParseError creates a new parse error.
func NewParseError(data any) *Error {
	return NewError(ParseError, "Parse error", data)
}

// NewInvalidRequestError creates a new invalid request error.
func NewInvalidRequestError(data any) *Error {
	return NewError(InvalidRequest, "Invalid Request", data)
}

// NewMethodNotFoundError creates a new method not found error.
func NewMethodNotFoundError(method string) *Error {
	return NewError(MethodNotFound, "Method not found", method)
}

// NewInvalidParamsError creates a new invalid params error.
func NewInvalidParamsError(data any) *Error {
	return NewError(InvalidParams, "Invalid params", data)
}

// NewInternalError creates a new internal error.
func NewInternalError(data any) *Error {
	return NewError(InternalError, "Internal error", data)
}

// BatchRequest represents a batch of JSON-RPC 2.0 requests.
type BatchRequest []json.RawMessage

// BatchResponse represents a batch of JSON-RPC 2.0 responses.
type BatchResponse []*Response

// Message represents a generic JSON-RPC 2.0 message that could be a request,
// response, or notification. This is used for initial parsing before determining
// the specific message type.
type Message struct {
	// JSONRPC specifies the version of the JSON-RPC protocol.
	JSONRPC string `json:"jsonrpc"`

	// ID is present for requests and responses, absent for notifications.
	ID *RequestID `json:"id,omitempty"`

	// Method is present for requests and notifications.
	Method string `json:"method,omitempty"`

	// Params is present for requests and notifications.
	Params json.RawMessage `json:"params,omitempty"`

	// Result is present for successful responses.
	Result json.RawMessage `json:"result,omitempty"`

	// Error is present for error responses.
	Error *Error `json:"error,omitempty"`
}

// MessageType indicates the type of JSON-RPC message.
type MessageType int

const (
	// MessageTypeUnknown indicates an unknown or invalid message type.
	MessageTypeUnknown MessageType = iota

	// MessageTypeRequest indicates a request message.
	MessageTypeRequest

	// MessageTypeResponse indicates a response message.
	MessageTypeResponse

	// MessageTypeNotification indicates a notification message.
	MessageTypeNotification
)

// String returns the string representation of the message type.
func (t MessageType) String() string {
	switch t {
	case MessageTypeRequest:
		return "request"
	case MessageTypeResponse:
		return "response"
	case MessageTypeNotification:
		return "notification"
	default:
		return "unknown"
	}
}

// Type returns the type of the message.
func (m *Message) Type() MessageType {
	// A response has either result or error
	if m.Result != nil || m.Error != nil {
		return MessageTypeResponse
	}

	// A request has method and id
	if m.Method != "" && m.ID != nil && !m.ID.IsNull() {
		return MessageTypeRequest
	}

	// A notification has method but no id
	if m.Method != "" && (m.ID == nil || m.ID.IsNull()) {
		return MessageTypeNotification
	}

	return MessageTypeUnknown
}

// ToRequest converts the message to a Request if it is a request message.
func (m *Message) ToRequest() *Request {
	if m.Type() != MessageTypeRequest {
		return nil
	}
	return &Request{
		JSONRPC: m.JSONRPC,
		ID:      *m.ID,
		Method:  m.Method,
		Params:  m.Params,
	}
}

// ToResponse converts the message to a Response if it is a response message.
func (m *Message) ToResponse() *Response {
	if m.Type() != MessageTypeResponse {
		return nil
	}
	id := RequestID{}
	if m.ID != nil {
		id = *m.ID
	}
	return &Response{
		JSONRPC: m.JSONRPC,
		ID:      id,
		Result:  m.Result,
		Error:   m.Error,
	}
}

// ToNotification converts the message to a Notification if it is a notification message.
func (m *Message) ToNotification() *Notification {
	if m.Type() != MessageTypeNotification {
		return nil
	}
	return &Notification{
		JSONRPC: m.JSONRPC,
		Method:  m.Method,
		Params:  m.Params,
	}
}
