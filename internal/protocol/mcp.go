package protocol

import (
	"encoding/json"
	"fmt"
)

// MCP protocol version constants.
const (
	// MCPVersion20250618 is the latest MCP protocol version.
	MCPVersion20250618 = "2025-06-18"

	// MCPVersion20250326 is a previous MCP protocol version.
	MCPVersion20250326 = "2025-03-26"

	// MCPVersion20241105 is an earlier MCP protocol version.
	MCPVersion20241105 = "2024-11-05"

	// MCPVersionLatest is the latest supported MCP protocol version.
	MCPVersionLatest = MCPVersion20250618
)

// MCP method constants.
const (
	MethodInitialize           = "initialize"
	MethodInitialized          = "notifications/initialized"
	MethodShutdown             = "shutdown"
	MethodExit                 = "exit"
	MethodToolsList            = "tools/list"
	MethodToolsCall            = "tools/call"
	MethodResourcesList        = "resources/list"
	MethodResourcesRead        = "resources/read"
	MethodResourcesSubscribe   = "resources/subscribe"
	MethodPromptsList          = "prompts/list"
	MethodPromptsGet           = "prompts/get"
	MethodLoggingSetLevel      = "logging/setLevel"
	MethodProgress             = "notifications/progress"
	MethodCancelled            = "notifications/cancelled"
	MethodResourcesUpdated     = "notifications/resources/updated"
	MethodResourcesListChanged = "notifications/resources/list_changed"
	MethodToolsListChanged     = "notifications/tools/list_changed"
	MethodPromptsListChanged   = "notifications/prompts/list_changed"
	MethodMessage              = "notifications/message"
)

// Implementation represents client or server implementation information.
type Implementation struct {
	// Name is the name of the implementation.
	Name string `json:"name"`

	// Version is the version of the implementation.
	Version string `json:"version"`
}

// ClientCapabilities defines the capabilities supported by the client.
type ClientCapabilities struct {
	// Experimental contains experimental capabilities.
	Experimental map[string]any `json:"experimental,omitempty"`

	// Roots indicates the client supports roots.
	Roots *RootsCapability `json:"roots,omitempty"`

	// Sampling indicates the client supports sampling.
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// RootsCapability defines the roots capability.
type RootsCapability struct {
	// ListChanged indicates the client supports list changed notifications.
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability defines the sampling capability.
type SamplingCapability struct{}

// ServerCapabilities defines the capabilities supported by the server.
type ServerCapabilities struct {
	// Experimental contains experimental capabilities.
	Experimental map[string]any `json:"experimental,omitempty"`

	// Logging indicates the server supports logging.
	Logging *LoggingCapability `json:"logging,omitempty"`

	// Prompts indicates the server supports prompts.
	Prompts *PromptsCapability `json:"prompts,omitempty"`

	// Resources indicates the server supports resources.
	Resources *ResourcesCapability `json:"resources,omitempty"`

	// Tools indicates the server supports tools.
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// LoggingCapability defines the logging capability.
type LoggingCapability struct{}

// PromptsCapability defines the prompts capability.
type PromptsCapability struct {
	// ListChanged indicates the server supports list changed notifications.
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability defines the resources capability.
type ResourcesCapability struct {
	// Subscribe indicates the server supports resource subscriptions.
	Subscribe bool `json:"subscribe,omitempty"`

	// ListChanged indicates the server supports list changed notifications.
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolsCapability defines the tools capability.
type ToolsCapability struct {
	// ListChanged indicates the server supports list changed notifications.
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeParams represents the parameters for the initialize request.
type InitializeParams struct {
	// ProtocolVersion is the latest version of the MCP protocol the client supports.
	ProtocolVersion string `json:"protocolVersion"`

	// Capabilities are the capabilities the client supports.
	Capabilities ClientCapabilities `json:"capabilities"`

	// ClientInfo contains information about the client implementation.
	ClientInfo Implementation `json:"clientInfo"`
}

// InitializeResult represents the result of the initialize request.
type InitializeResult struct {
	// ProtocolVersion is the version of the MCP protocol the server is using.
	ProtocolVersion string `json:"protocolVersion"`

	// Capabilities are the capabilities the server supports.
	Capabilities ServerCapabilities `json:"capabilities"`

	// ServerInfo contains information about the server implementation.
	ServerInfo Implementation `json:"serverInfo"`

	// Instructions are optional instructions for how the client should use the server.
	Instructions string `json:"instructions,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	// Name is the unique name of the tool.
	Name string `json:"name"`

	// Description is a human-readable description of the tool.
	Description string `json:"description,omitempty"`

	// InputSchema is the JSON Schema for the tool's input parameters.
	InputSchema *JSONSchema `json:"inputSchema"`

	// Annotations contains additional metadata about the tool.
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolAnnotations contains metadata about a tool.
type ToolAnnotations struct {
	// Title is a human-readable title for the tool.
	Title string `json:"title,omitempty"`

	// ReadOnlyHint indicates the tool does not modify state.
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`

	// DestructiveHint indicates the tool may have destructive effects.
	DestructiveHint bool `json:"destructiveHint,omitempty"`

	// IdempotentHint indicates the tool is idempotent.
	IdempotentHint bool `json:"idempotentHint,omitempty"`

	// OpenWorldHint indicates the tool may interact with external systems.
	OpenWorldHint bool `json:"openWorldHint,omitempty"`
}

// JSONSchema represents a JSON Schema definition.
type JSONSchema struct {
	// Type is the JSON Schema type.
	Type string `json:"type,omitempty"`

	// Properties contains the schema properties for object types.
	Properties map[string]*JSONSchema `json:"properties,omitempty"`

	// Required lists the required properties.
	Required []string `json:"required,omitempty"`

	// Items is the schema for array items.
	Items *JSONSchema `json:"items,omitempty"`

	// Description is a description of the schema.
	Description string `json:"description,omitempty"`

	// Enum lists the allowed values.
	Enum []any `json:"enum,omitempty"`

	// Default is the default value.
	Default any `json:"default,omitempty"`

	// AdditionalProperties controls additional properties for objects.
	AdditionalProperties any `json:"additionalProperties,omitempty"`
}

// ToolsListResult represents the result of tools/list.
type ToolsListResult struct {
	// Tools is the list of available tools.
	Tools []Tool `json:"tools"`

	// NextCursor is used for pagination.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ToolCallParams represents the parameters for tools/call.
type ToolCallParams struct {
	// Name is the name of the tool to call.
	Name string `json:"name"`

	// Arguments are the arguments to pass to the tool.
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ToolCallResult represents the result of tools/call.
type ToolCallResult struct {
	// Content is the content returned by the tool.
	Content []Content `json:"content"`

	// IsError indicates if the tool call resulted in an error.
	IsError bool `json:"isError,omitempty"`
}

// ContentType represents the type of content.
type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeImage    ContentType = "image"
	ContentTypeResource ContentType = "resource"
)

// Content is the interface for different content types.
type Content interface {
	contentType() ContentType
}

// TextContent represents text content.
type TextContent struct {
	// Type is always "text".
	Type ContentType `json:"type"`

	// Text is the text content.
	Text string `json:"text"`

	// Annotations contains optional metadata.
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
}

func (t TextContent) contentType() ContentType { return ContentTypeText }

// MarshalJSON implements json.Marshaler.
func (t TextContent) MarshalJSON() ([]byte, error) {
	type Alias TextContent
	a := Alias(t)
	a.Type = ContentTypeText
	return json.Marshal(a)
}

// ImageContent represents image content.
type ImageContent struct {
	// Type is always "image".
	Type ContentType `json:"type"`

	// Data is the base64-encoded image data.
	Data string `json:"data"`

	// MimeType is the MIME type of the image.
	MimeType string `json:"mimeType"`

	// Annotations contains optional metadata.
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
}

func (i ImageContent) contentType() ContentType { return ContentTypeImage }

// MarshalJSON implements json.Marshaler.
func (i ImageContent) MarshalJSON() ([]byte, error) {
	type Alias ImageContent
	a := Alias(i)
	a.Type = ContentTypeImage
	return json.Marshal(a)
}

// ResourceContent represents embedded resource content.
type ResourceContent struct {
	// Type is always "resource".
	Type ContentType `json:"type"`

	// Resource is the embedded resource.
	Resource EmbeddedResource `json:"resource"`

	// Annotations contains optional metadata.
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
}

func (r ResourceContent) contentType() ContentType { return ContentTypeResource }

// MarshalJSON implements json.Marshaler.
func (r ResourceContent) MarshalJSON() ([]byte, error) {
	type Alias ResourceContent
	a := Alias(r)
	a.Type = ContentTypeResource
	return json.Marshal(a)
}

// ContentAnnotations contains optional metadata for content.
type ContentAnnotations struct {
	// Audience specifies who the content is intended for.
	Audience []string `json:"audience,omitempty"`

	// Priority indicates the priority of the content (0.0 to 1.0).
	Priority float64 `json:"priority,omitempty"`
}

// RawContent is used for unmarshaling content when the type is not known.
type RawContent struct {
	Type        ContentType         `json:"type"`
	Text        string              `json:"text,omitempty"`
	Data        string              `json:"data,omitempty"`
	MimeType    string              `json:"mimeType,omitempty"`
	Resource    *EmbeddedResource   `json:"resource,omitempty"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
}

// ToContent converts RawContent to the appropriate Content type.
func (r *RawContent) ToContent() (Content, error) {
	switch r.Type {
	case ContentTypeText:
		return TextContent{
			Type:        r.Type,
			Text:        r.Text,
			Annotations: r.Annotations,
		}, nil
	case ContentTypeImage:
		return ImageContent{
			Type:        r.Type,
			Data:        r.Data,
			MimeType:    r.MimeType,
			Annotations: r.Annotations,
		}, nil
	case ContentTypeResource:
		if r.Resource == nil {
			return nil, fmt.Errorf("resource content missing resource field")
		}
		return ResourceContent{
			Type:        r.Type,
			Resource:    *r.Resource,
			Annotations: r.Annotations,
		}, nil
	default:
		return nil, fmt.Errorf("unknown content type: %s", r.Type)
	}
}

// Resource represents an MCP resource.
type Resource struct {
	// URI is the unique identifier for the resource.
	URI string `json:"uri"`

	// Name is a human-readable name for the resource.
	Name string `json:"name"`

	// Description is a description of the resource.
	Description string `json:"description,omitempty"`

	// MimeType is the MIME type of the resource.
	MimeType string `json:"mimeType,omitempty"`

	// Annotations contains additional metadata.
	Annotations *ResourceAnnotations `json:"annotations,omitempty"`
}

// ResourceAnnotations contains metadata about a resource.
type ResourceAnnotations struct {
	// Audience specifies who the resource is intended for.
	Audience []string `json:"audience,omitempty"`

	// Priority indicates the priority of the resource (0.0 to 1.0).
	Priority float64 `json:"priority,omitempty"`
}

// EmbeddedResource represents an embedded resource in content.
type EmbeddedResource struct {
	// URI is the unique identifier for the resource.
	URI string `json:"uri"`

	// MimeType is the MIME type of the resource.
	MimeType string `json:"mimeType,omitempty"`

	// Text is the text content (if applicable).
	Text string `json:"text,omitempty"`

	// Blob is the base64-encoded binary content (if applicable).
	Blob string `json:"blob,omitempty"`
}

// ResourcesListResult represents the result of resources/list.
type ResourcesListResult struct {
	// Resources is the list of available resources.
	Resources []Resource `json:"resources"`

	// NextCursor is used for pagination.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ResourcesReadParams represents the parameters for resources/read.
type ResourcesReadParams struct {
	// URI is the URI of the resource to read.
	URI string `json:"uri"`
}

// ResourcesReadResult represents the result of resources/read.
type ResourcesReadResult struct {
	// Contents is the content of the resource.
	Contents []ResourceContents `json:"contents"`
}

// ResourceContents represents the contents of a resource.
type ResourceContents struct {
	// URI is the URI of the resource.
	URI string `json:"uri"`

	// MimeType is the MIME type of the content.
	MimeType string `json:"mimeType,omitempty"`

	// Text is the text content (for text resources).
	Text string `json:"text,omitempty"`

	// Blob is the base64-encoded binary content (for binary resources).
	Blob string `json:"blob,omitempty"`
}

// Prompt represents an MCP prompt.
type Prompt struct {
	// Name is the unique name of the prompt.
	Name string `json:"name"`

	// Description is a description of the prompt.
	Description string `json:"description,omitempty"`

	// Arguments is the list of arguments the prompt accepts.
	Arguments []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument represents an argument to a prompt.
type PromptArgument struct {
	// Name is the name of the argument.
	Name string `json:"name"`

	// Description is a description of the argument.
	Description string `json:"description,omitempty"`

	// Required indicates if the argument is required.
	Required bool `json:"required,omitempty"`
}

// PromptsListResult represents the result of prompts/list.
type PromptsListResult struct {
	// Prompts is the list of available prompts.
	Prompts []Prompt `json:"prompts"`

	// NextCursor is used for pagination.
	NextCursor string `json:"nextCursor,omitempty"`
}

// PromptsGetParams represents the parameters for prompts/get.
type PromptsGetParams struct {
	// Name is the name of the prompt to get.
	Name string `json:"name"`

	// Arguments are the arguments to pass to the prompt.
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptsGetResult represents the result of prompts/get.
type PromptsGetResult struct {
	// Description is a description of the prompt.
	Description string `json:"description,omitempty"`

	// Messages is the list of messages in the prompt.
	Messages []PromptMessage `json:"messages"`
}

// PromptMessage represents a message in a prompt.
type PromptMessage struct {
	// Role is the role of the message sender.
	Role Role `json:"role"`

	// Content is the content of the message.
	Content Content `json:"content"`
}

// Role represents the role of a message sender.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// LogLevel represents a logging level.
type LogLevel string

const (
	LogLevelDebug     LogLevel = "debug"
	LogLevelInfo      LogLevel = "info"
	LogLevelNotice    LogLevel = "notice"
	LogLevelWarning   LogLevel = "warning"
	LogLevelError     LogLevel = "error"
	LogLevelCritical  LogLevel = "critical"
	LogLevelAlert     LogLevel = "alert"
	LogLevelEmergency LogLevel = "emergency"
)

// LoggingSetLevelParams represents the parameters for logging/setLevel.
type LoggingSetLevelParams struct {
	// Level is the logging level to set.
	Level LogLevel `json:"level"`
}

// LoggingMessageParams represents the parameters for a logging message notification.
type LoggingMessageParams struct {
	// Level is the logging level of the message.
	Level LogLevel `json:"level"`

	// Logger is the name of the logger.
	Logger string `json:"logger,omitempty"`

	// Data is the log data.
	Data any `json:"data"`
}

// ProgressParams represents the parameters for a progress notification.
type ProgressParams struct {
	// ProgressToken is the token identifying the operation.
	ProgressToken ProgressToken `json:"progressToken"`

	// Progress is the current progress value.
	Progress float64 `json:"progress"`

	// Total is the total progress value (if known).
	Total float64 `json:"total,omitempty"`

	// Message is an optional progress message.
	Message string `json:"message,omitempty"`
}

// ProgressToken represents a progress token that can be a string or number.
type ProgressToken struct {
	value any
}

// NewStringProgressToken creates a ProgressToken from a string value.
func NewStringProgressToken(s string) ProgressToken {
	return ProgressToken{value: s}
}

// NewNumberProgressToken creates a ProgressToken from a numeric value.
func NewNumberProgressToken(n int64) ProgressToken {
	return ProgressToken{value: n}
}

// Value returns the underlying value of the ProgressToken.
func (t ProgressToken) Value() any {
	return t.value
}

// MarshalJSON implements json.Marshaler for ProgressToken.
func (t ProgressToken) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.value)
}

// UnmarshalJSON implements json.Unmarshaler for ProgressToken.
func (t *ProgressToken) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t.value = s
		return nil
	}

	// Try to unmarshal as number
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		if n == float64(int64(n)) {
			t.value = int64(n)
		} else {
			t.value = n
		}
		return nil
	}

	return fmt.Errorf("invalid progress token: %s", string(data))
}

// CancelledParams represents the parameters for a cancelled notification.
type CancelledParams struct {
	// RequestID is the ID of the request that was cancelled.
	RequestID RequestID `json:"requestId"`

	// Reason is an optional reason for the cancellation.
	Reason string `json:"reason,omitempty"`
}

// ResourcesUpdatedParams represents the parameters for a resources/updated notification.
type ResourcesUpdatedParams struct {
	// URI is the URI of the resource that was updated.
	URI string `json:"uri"`
}

// PaginationParams represents common pagination parameters.
type PaginationParams struct {
	// Cursor is the cursor for pagination.
	Cursor string `json:"cursor,omitempty"`
}

// ResourcesSubscribeParams represents the parameters for resources/subscribe.
type ResourcesSubscribeParams struct {
	// URI is the URI of the resource to subscribe to.
	URI string `json:"uri"`
}

// ResourcesUnsubscribeParams represents the parameters for resources/unsubscribe.
type ResourcesUnsubscribeParams struct {
	// URI is the URI of the resource to unsubscribe from.
	URI string `json:"uri"`
}
