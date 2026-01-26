/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package protocol

import (
	"encoding/json"
	"testing"
)

// TestMCPVersionConstants tests that MCP version constants are correct.
func TestMCPVersionConstants(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"MCPVersion20250618", MCPVersion20250618, "2025-06-18"},
		{"MCPVersion20250326", MCPVersion20250326, "2025-03-26"},
		{"MCPVersion20241105", MCPVersion20241105, "2024-11-05"},
		{"MCPVersionLatest", MCPVersionLatest, "2025-06-18"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.version != tt.want {
				t.Errorf("%s = %s, want %s", tt.name, tt.version, tt.want)
			}
		})
	}
}

// TestMCPMethodConstants tests that MCP method constants are defined correctly.
func TestMCPMethodConstants(t *testing.T) {
	tests := []struct {
		name   string
		method string
		want   string
	}{
		{"MethodInitialize", MethodInitialize, "initialize"},
		{"MethodInitialized", MethodInitialized, "notifications/initialized"},
		{"MethodShutdown", MethodShutdown, "shutdown"},
		{"MethodExit", MethodExit, "exit"},
		{"MethodToolsList", MethodToolsList, "tools/list"},
		{"MethodToolsCall", MethodToolsCall, "tools/call"},
		{"MethodResourcesList", MethodResourcesList, "resources/list"},
		{"MethodResourcesRead", MethodResourcesRead, "resources/read"},
		{"MethodResourcesSubscribe", MethodResourcesSubscribe, "resources/subscribe"},
		{"MethodPromptsList", MethodPromptsList, "prompts/list"},
		{"MethodPromptsGet", MethodPromptsGet, "prompts/get"},
		{"MethodLoggingSetLevel", MethodLoggingSetLevel, "logging/setLevel"},
		{"MethodProgress", MethodProgress, "notifications/progress"},
		{"MethodCancelled", MethodCancelled, "notifications/cancelled"},
		{"MethodResourcesUpdated", MethodResourcesUpdated, "notifications/resources/updated"},
		{"MethodResourcesListChanged", MethodResourcesListChanged, "notifications/resources/list_changed"},
		{"MethodToolsListChanged", MethodToolsListChanged, "notifications/tools/list_changed"},
		{"MethodPromptsListChanged", MethodPromptsListChanged, "notifications/prompts/list_changed"},
		{"MethodMessage", MethodMessage, "notifications/message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.method != tt.want {
				t.Errorf("%s = %s, want %s", tt.name, tt.method, tt.want)
			}
		})
	}
}

// TestImplementation_MarshalUnmarshal tests Implementation marshaling and unmarshaling.
func TestImplementation_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		impl Implementation
	}{
		{
			name: "basic implementation",
			impl: Implementation{
				Name:    "test-server",
				Version: "1.0.0",
			},
		},
		{
			name: "empty version",
			impl: Implementation{
				Name:    "test-server",
				Version: "",
			},
		},
		{
			name: "complex version",
			impl: Implementation{
				Name:    "mcp-bridge",
				Version: "2.1.0-beta.1+build.123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.impl)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got Implementation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Name != tt.impl.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.impl.Name)
			}
			if got.Version != tt.impl.Version {
				t.Errorf("Version = %s, want %s", got.Version, tt.impl.Version)
			}
		})
	}
}

// TestInitializeParams_MarshalUnmarshal tests InitializeParams marshaling and unmarshaling.
func TestInitializeParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params InitializeParams
	}{
		{
			name: "minimal params",
			params: InitializeParams{
				ProtocolVersion: MCPVersionLatest,
				ClientInfo: Implementation{
					Name:    "test-client",
					Version: "1.0",
				},
			},
		},
		{
			name: "params with capabilities",
			params: InitializeParams{
				ProtocolVersion: MCPVersionLatest,
				ClientInfo: Implementation{
					Name:    "test-client",
					Version: "1.0",
				},
				Capabilities: ClientCapabilities{
					Roots: &RootsCapability{
						ListChanged: true,
					},
					Sampling: &SamplingCapability{},
				},
			},
		},
		{
			name: "params with experimental capabilities",
			params: InitializeParams{
				ProtocolVersion: MCPVersionLatest,
				ClientInfo: Implementation{
					Name:    "test-client",
					Version: "1.0",
				},
				Capabilities: ClientCapabilities{
					Experimental: map[string]any{
						"customFeature": true,
						"anotherFeature": map[string]any{
							"nested": "value",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got InitializeParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.ProtocolVersion != tt.params.ProtocolVersion {
				t.Errorf("ProtocolVersion = %s, want %s", got.ProtocolVersion, tt.params.ProtocolVersion)
			}
			if got.ClientInfo.Name != tt.params.ClientInfo.Name {
				t.Errorf("ClientInfo.Name = %s, want %s", got.ClientInfo.Name, tt.params.ClientInfo.Name)
			}
		})
	}
}

// TestInitializeResult_MarshalUnmarshal tests InitializeResult marshaling and unmarshaling.
func TestInitializeResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result InitializeResult
	}{
		{
			name: "minimal result",
			result: InitializeResult{
				ProtocolVersion: MCPVersionLatest,
				ServerInfo: Implementation{
					Name:    "test-server",
					Version: "1.0",
				},
			},
		},
		{
			name: "result with all capabilities",
			result: InitializeResult{
				ProtocolVersion: MCPVersionLatest,
				ServerInfo: Implementation{
					Name:    "test-server",
					Version: "1.0",
				},
				Capabilities: ServerCapabilities{
					Logging: &LoggingCapability{},
					Prompts: &PromptsCapability{
						ListChanged: true,
					},
					Resources: &ResourcesCapability{
						Subscribe:   true,
						ListChanged: true,
					},
					Tools: &ToolsCapability{
						ListChanged: true,
					},
				},
				Instructions: "Use tools responsibly",
			},
		},
		{
			name: "result with experimental capabilities",
			result: InitializeResult{
				ProtocolVersion: MCPVersionLatest,
				ServerInfo: Implementation{
					Name:    "test-server",
					Version: "1.0",
				},
				Capabilities: ServerCapabilities{
					Experimental: map[string]any{
						"customFeature": "enabled",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got InitializeResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.ProtocolVersion != tt.result.ProtocolVersion {
				t.Errorf("ProtocolVersion = %s, want %s", got.ProtocolVersion, tt.result.ProtocolVersion)
			}
			if got.ServerInfo.Name != tt.result.ServerInfo.Name {
				t.Errorf("ServerInfo.Name = %s, want %s", got.ServerInfo.Name, tt.result.ServerInfo.Name)
			}
			if got.Instructions != tt.result.Instructions {
				t.Errorf("Instructions = %s, want %s", got.Instructions, tt.result.Instructions)
			}
		})
	}
}

// TestTool_MarshalUnmarshal tests Tool marshaling and unmarshaling.
func TestTool_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		tool Tool
	}{
		{
			name: "minimal tool",
			tool: Tool{
				Name: "my-tool",
				InputSchema: &JSONSchema{
					Type: "object",
				},
			},
		},
		{
			name: "tool with description",
			tool: Tool{
				Name:        "my-tool",
				Description: "A tool that does something",
				InputSchema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"param1": {Type: "string", Description: "First parameter"},
						"param2": {Type: "integer", Description: "Second parameter"},
					},
					Required: []string{"param1"},
				},
			},
		},
		{
			name: "tool with annotations",
			tool: Tool{
				Name:        "read-tool",
				Description: "A read-only tool",
				InputSchema: &JSONSchema{
					Type: "object",
				},
				Annotations: &ToolAnnotations{
					Title:           "Read Tool",
					ReadOnlyHint:    true,
					DestructiveHint: false,
					IdempotentHint:  true,
					OpenWorldHint:   false,
				},
			},
		},
		{
			name: "tool with complex schema",
			tool: Tool{
				Name:        "complex-tool",
				Description: "A tool with complex input schema",
				InputSchema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"items": {
							Type: "array",
							Items: &JSONSchema{
								Type: "object",
								Properties: map[string]*JSONSchema{
									"id":   {Type: "string"},
									"name": {Type: "string"},
								},
							},
						},
						"options": {
							Type:                 "object",
							AdditionalProperties: true,
						},
					},
					Required: []string{"items"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.tool)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got Tool
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Name != tt.tool.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.tool.Name)
			}
			if got.Description != tt.tool.Description {
				t.Errorf("Description = %s, want %s", got.Description, tt.tool.Description)
			}
		})
	}
}

// TestToolsListResult_MarshalUnmarshal tests ToolsListResult marshaling and unmarshaling.
func TestToolsListResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result ToolsListResult
	}{
		{
			name: "empty tools list",
			result: ToolsListResult{
				Tools: []Tool{},
			},
		},
		{
			name: "tools list with pagination",
			result: ToolsListResult{
				Tools: []Tool{
					{Name: "tool1", InputSchema: &JSONSchema{Type: "object"}},
					{Name: "tool2", InputSchema: &JSONSchema{Type: "object"}},
				},
				NextCursor: "cursor-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ToolsListResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if len(got.Tools) != len(tt.result.Tools) {
				t.Errorf("len(Tools) = %d, want %d", len(got.Tools), len(tt.result.Tools))
			}
			if got.NextCursor != tt.result.NextCursor {
				t.Errorf("NextCursor = %s, want %s", got.NextCursor, tt.result.NextCursor)
			}
		})
	}
}

// TestToolCallParams_MarshalUnmarshal tests ToolCallParams marshaling and unmarshaling.
func TestToolCallParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params ToolCallParams
	}{
		{
			name: "params without arguments",
			params: ToolCallParams{
				Name: "my-tool",
			},
		},
		{
			name: "params with arguments",
			params: ToolCallParams{
				Name: "my-tool",
				Arguments: map[string]any{
					"param1": "value1",
					"param2": 42,
					"param3": true,
				},
			},
		},
		{
			name: "params with complex arguments",
			params: ToolCallParams{
				Name: "complex-tool",
				Arguments: map[string]any{
					"nested": map[string]any{
						"key": "value",
					},
					"list": []any{"a", "b", "c"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ToolCallParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Name != tt.params.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.params.Name)
			}
		})
	}
}

// TestToolCallResult_MarshalUnmarshal tests ToolCallResult marshaling and unmarshaling.
func TestToolCallResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result ToolCallResult
	}{
		{
			name: "result with text content",
			result: ToolCallResult{
				Content: []Content{
					TextContent{Type: ContentTypeText, Text: "Hello, world!"},
				},
			},
		},
		{
			name: "result with error",
			result: ToolCallResult{
				Content: []Content{
					TextContent{Type: ContentTypeText, Text: "Error occurred"},
				},
				IsError: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Verify JSON structure
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map error = %v", err)
			}

			if tt.result.IsError {
				if isErr, ok := raw["isError"].(bool); !ok || !isErr {
					t.Errorf("isError should be true")
				}
			}
		})
	}
}

// TestTextContent_MarshalUnmarshal tests TextContent marshaling and unmarshaling.
func TestTextContent_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		content TextContent
	}{
		{
			name: "simple text",
			content: TextContent{
				Text: "Hello, world!",
			},
		},
		{
			name: "text with annotations",
			content: TextContent{
				Text: "Important message",
				Annotations: &ContentAnnotations{
					Audience: []string{"user", "assistant"},
					Priority: 0.8,
				},
			},
		},
		{
			name: "empty text",
			content: TextContent{
				Text: "",
			},
		},
		{
			name: "multiline text",
			content: TextContent{
				Text: "Line 1\nLine 2\nLine 3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Verify type is set correctly
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map error = %v", err)
			}

			if raw["type"] != string(ContentTypeText) {
				t.Errorf("type = %v, want %s", raw["type"], ContentTypeText)
			}

			// Unmarshal back to TextContent
			var got TextContent
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Text != tt.content.Text {
				t.Errorf("Text = %s, want %s", got.Text, tt.content.Text)
			}

			// Verify contentType() method
			if got.contentType() != ContentTypeText {
				t.Errorf("contentType() = %s, want %s", got.contentType(), ContentTypeText)
			}
		})
	}
}

// TestImageContent_MarshalUnmarshal tests ImageContent marshaling and unmarshaling.
func TestImageContent_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		content ImageContent
	}{
		{
			name: "simple image",
			content: ImageContent{
				Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
				MimeType: "image/png",
			},
		},
		{
			name: "image with annotations",
			content: ImageContent{
				Data:     "base64data",
				MimeType: "image/jpeg",
				Annotations: &ContentAnnotations{
					Audience: []string{"user"},
					Priority: 1.0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Verify type is set correctly
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map error = %v", err)
			}

			if raw["type"] != string(ContentTypeImage) {
				t.Errorf("type = %v, want %s", raw["type"], ContentTypeImage)
			}

			// Unmarshal back to ImageContent
			var got ImageContent
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Data != tt.content.Data {
				t.Errorf("Data = %s, want %s", got.Data, tt.content.Data)
			}
			if got.MimeType != tt.content.MimeType {
				t.Errorf("MimeType = %s, want %s", got.MimeType, tt.content.MimeType)
			}

			// Verify contentType() method
			if got.contentType() != ContentTypeImage {
				t.Errorf("contentType() = %s, want %s", got.contentType(), ContentTypeImage)
			}
		})
	}
}

// TestResourceContent_MarshalUnmarshal tests ResourceContent marshaling and unmarshaling.
func TestResourceContent_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		content ResourceContent
	}{
		{
			name: "text resource",
			content: ResourceContent{
				Resource: EmbeddedResource{
					URI:      "file:///path/to/file.txt",
					MimeType: "text/plain",
					Text:     "File content here",
				},
			},
		},
		{
			name: "binary resource",
			content: ResourceContent{
				Resource: EmbeddedResource{
					URI:      "file:///path/to/image.png",
					MimeType: "image/png",
					Blob:     "base64encodeddata",
				},
			},
		},
		{
			name: "resource with annotations",
			content: ResourceContent{
				Resource: EmbeddedResource{
					URI:  "file:///path/to/file.txt",
					Text: "Content",
				},
				Annotations: &ContentAnnotations{
					Priority: 0.5,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Verify type is set correctly
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal to map error = %v", err)
			}

			if raw["type"] != string(ContentTypeResource) {
				t.Errorf("type = %v, want %s", raw["type"], ContentTypeResource)
			}

			// Unmarshal back to ResourceContent
			var got ResourceContent
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Resource.URI != tt.content.Resource.URI {
				t.Errorf("Resource.URI = %s, want %s", got.Resource.URI, tt.content.Resource.URI)
			}

			// Verify contentType() method
			if got.contentType() != ContentTypeResource {
				t.Errorf("contentType() = %s, want %s", got.contentType(), ContentTypeResource)
			}
		})
	}
}

// TestRawContent_ToContent tests RawContent.ToContent() conversion.
func TestRawContent_ToContent(t *testing.T) {
	tests := []struct {
		name    string
		raw     RawContent
		wantErr bool
	}{
		{
			name: "text content",
			raw: RawContent{
				Type: ContentTypeText,
				Text: "Hello",
			},
			wantErr: false,
		},
		{
			name: "image content",
			raw: RawContent{
				Type:     ContentTypeImage,
				Data:     "base64data",
				MimeType: "image/png",
			},
			wantErr: false,
		},
		{
			name: "resource content",
			raw: RawContent{
				Type: ContentTypeResource,
				Resource: &EmbeddedResource{
					URI:  "file:///path/to/file",
					Text: "Content",
				},
			},
			wantErr: false,
		},
		{
			name: "resource content missing resource field",
			raw: RawContent{
				Type: ContentTypeResource,
			},
			wantErr: true,
		},
		{
			name: "unknown content type",
			raw: RawContent{
				Type: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := tt.raw.ToContent()
			if tt.wantErr {
				if err == nil {
					t.Errorf("ToContent() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ToContent() error = %v", err)
			}
			if content == nil {
				t.Errorf("ToContent() returned nil")
			}
		})
	}
}

// TestResource_MarshalUnmarshal tests Resource marshaling and unmarshaling.
func TestResource_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
	}{
		{
			name: "minimal resource",
			resource: Resource{
				URI:  "file:///path/to/file.txt",
				Name: "file.txt",
			},
		},
		{
			name: "resource with description",
			resource: Resource{
				URI:         "db://table/users",
				Name:        "Users Table",
				Description: "The users database table",
				MimeType:    "application/json",
			},
		},
		{
			name: "resource with annotations",
			resource: Resource{
				URI:  "file:///path/to/config.json",
				Name: "config.json",
				Annotations: &ResourceAnnotations{
					Audience: []string{"admin"},
					Priority: 0.9,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.resource)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got Resource
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.URI != tt.resource.URI {
				t.Errorf("URI = %s, want %s", got.URI, tt.resource.URI)
			}
			if got.Name != tt.resource.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.resource.Name)
			}
			if got.Description != tt.resource.Description {
				t.Errorf("Description = %s, want %s", got.Description, tt.resource.Description)
			}
		})
	}
}

// TestResourcesListResult_MarshalUnmarshal tests ResourcesListResult marshaling and unmarshaling.
func TestResourcesListResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result ResourcesListResult
	}{
		{
			name: "empty resources list",
			result: ResourcesListResult{
				Resources: []Resource{},
			},
		},
		{
			name: "resources list with pagination",
			result: ResourcesListResult{
				Resources: []Resource{
					{URI: "file:///a.txt", Name: "a.txt"},
					{URI: "file:///b.txt", Name: "b.txt"},
				},
				NextCursor: "next-page",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ResourcesListResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if len(got.Resources) != len(tt.result.Resources) {
				t.Errorf("len(Resources) = %d, want %d", len(got.Resources), len(tt.result.Resources))
			}
			if got.NextCursor != tt.result.NextCursor {
				t.Errorf("NextCursor = %s, want %s", got.NextCursor, tt.result.NextCursor)
			}
		})
	}
}

// TestResourcesReadParams_MarshalUnmarshal tests ResourcesReadParams marshaling and unmarshaling.
func TestResourcesReadParams_MarshalUnmarshal(t *testing.T) {
	params := ResourcesReadParams{URI: "file:///path/to/file.txt"}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ResourcesReadParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.URI != params.URI {
		t.Errorf("URI = %s, want %s", got.URI, params.URI)
	}
}

// TestResourcesReadResult_MarshalUnmarshal tests ResourcesReadResult marshaling and unmarshaling.
func TestResourcesReadResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result ResourcesReadResult
	}{
		{
			name: "text resource",
			result: ResourcesReadResult{
				Contents: []ResourceContents{
					{
						URI:      "file:///path/to/file.txt",
						MimeType: "text/plain",
						Text:     "File content",
					},
				},
			},
		},
		{
			name: "binary resource",
			result: ResourcesReadResult{
				Contents: []ResourceContents{
					{
						URI:      "file:///path/to/image.png",
						MimeType: "image/png",
						Blob:     "base64data",
					},
				},
			},
		},
		{
			name: "multiple contents",
			result: ResourcesReadResult{
				Contents: []ResourceContents{
					{URI: "file:///a.txt", Text: "Content A"},
					{URI: "file:///b.txt", Text: "Content B"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ResourcesReadResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if len(got.Contents) != len(tt.result.Contents) {
				t.Errorf("len(Contents) = %d, want %d", len(got.Contents), len(tt.result.Contents))
			}
		})
	}
}

// TestPrompt_MarshalUnmarshal tests Prompt marshaling and unmarshaling.
func TestPrompt_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		prompt Prompt
	}{
		{
			name: "minimal prompt",
			prompt: Prompt{
				Name: "my-prompt",
			},
		},
		{
			name: "prompt with description",
			prompt: Prompt{
				Name:        "code-review",
				Description: "A prompt for code review",
			},
		},
		{
			name: "prompt with arguments",
			prompt: Prompt{
				Name:        "greeting",
				Description: "A greeting prompt",
				Arguments: []PromptArgument{
					{Name: "name", Description: "The name to greet", Required: true},
					{Name: "language", Description: "The language", Required: false},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.prompt)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got Prompt
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Name != tt.prompt.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.prompt.Name)
			}
			if got.Description != tt.prompt.Description {
				t.Errorf("Description = %s, want %s", got.Description, tt.prompt.Description)
			}
			if len(got.Arguments) != len(tt.prompt.Arguments) {
				t.Errorf("len(Arguments) = %d, want %d", len(got.Arguments), len(tt.prompt.Arguments))
			}
		})
	}
}

// TestPromptsListResult_MarshalUnmarshal tests PromptsListResult marshaling and unmarshaling.
func TestPromptsListResult_MarshalUnmarshal(t *testing.T) {
	result := PromptsListResult{
		Prompts: []Prompt{
			{Name: "prompt1"},
			{Name: "prompt2"},
		},
		NextCursor: "cursor-abc",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got PromptsListResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(got.Prompts) != len(result.Prompts) {
		t.Errorf("len(Prompts) = %d, want %d", len(got.Prompts), len(result.Prompts))
	}
	if got.NextCursor != result.NextCursor {
		t.Errorf("NextCursor = %s, want %s", got.NextCursor, result.NextCursor)
	}
}

// TestPromptsGetParams_MarshalUnmarshal tests PromptsGetParams marshaling and unmarshaling.
func TestPromptsGetParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params PromptsGetParams
	}{
		{
			name: "params without arguments",
			params: PromptsGetParams{
				Name: "my-prompt",
			},
		},
		{
			name: "params with arguments",
			params: PromptsGetParams{
				Name: "greeting",
				Arguments: map[string]string{
					"name":     "Alice",
					"language": "en",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got PromptsGetParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Name != tt.params.Name {
				t.Errorf("Name = %s, want %s", got.Name, tt.params.Name)
			}
		})
	}
}

// TestProgressToken_MarshalUnmarshal tests ProgressToken marshaling and unmarshaling.
func TestProgressToken_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		token    ProgressToken
		wantJSON string
	}{
		{
			name:     "string token",
			token:    NewStringProgressToken("token-123"),
			wantJSON: `"token-123"`,
		},
		{
			name:     "number token",
			token:    NewNumberProgressToken(42),
			wantJSON: `42`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.token)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			if string(data) != tt.wantJSON {
				t.Errorf("Marshal() = %s, want %s", data, tt.wantJSON)
			}

			var got ProgressToken
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
		})
	}
}

// TestProgressToken_UnmarshalJSON tests ProgressToken unmarshaling edge cases.
func TestProgressToken_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "string value",
			input:   `"token-abc"`,
			wantErr: false,
		},
		{
			name:    "integer value",
			input:   `123`,
			wantErr: false,
		},
		{
			name:    "float value that is integer",
			input:   `123.0`,
			wantErr: false,
		},
		{
			name:    "float value",
			input:   `123.456`,
			wantErr: false,
		},
		{
			name:    "invalid - array",
			input:   `[1, 2, 3]`,
			wantErr: true,
		},
		{
			name:    "invalid - object",
			input:   `{"id": 1}`,
			wantErr: true,
		},
		{
			name:    "invalid - boolean",
			input:   `true`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var token ProgressToken
			err := json.Unmarshal([]byte(tt.input), &token)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Unmarshal() expected error")
				}
			} else {
				if err != nil {
					t.Errorf("Unmarshal() error = %v", err)
				}
			}
		})
	}
}

// TestProgressToken_Value tests ProgressToken.Value() method.
func TestProgressToken_Value(t *testing.T) {
	stringToken := NewStringProgressToken("test")
	numberToken := NewNumberProgressToken(42)

	if stringToken.Value() != "test" {
		t.Errorf("Value() = %v, want %v", stringToken.Value(), "test")
	}

	if numberToken.Value() != int64(42) {
		t.Errorf("Value() = %v, want %v", numberToken.Value(), int64(42))
	}
}

// TestProgressParams_MarshalUnmarshal tests ProgressParams marshaling and unmarshaling.
func TestProgressParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params ProgressParams
	}{
		{
			name: "minimal progress",
			params: ProgressParams{
				ProgressToken: NewStringProgressToken("token-1"),
				Progress:      0.5,
			},
		},
		{
			name: "progress with total",
			params: ProgressParams{
				ProgressToken: NewNumberProgressToken(1),
				Progress:      50,
				Total:         100,
			},
		},
		{
			name: "progress with message",
			params: ProgressParams{
				ProgressToken: NewStringProgressToken("download"),
				Progress:      75,
				Total:         100,
				Message:       "Downloading file...",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ProgressParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Progress != tt.params.Progress {
				t.Errorf("Progress = %f, want %f", got.Progress, tt.params.Progress)
			}
			if got.Total != tt.params.Total {
				t.Errorf("Total = %f, want %f", got.Total, tt.params.Total)
			}
			if got.Message != tt.params.Message {
				t.Errorf("Message = %s, want %s", got.Message, tt.params.Message)
			}
		})
	}
}

// TestCancelledParams_MarshalUnmarshal tests CancelledParams marshaling and unmarshaling.
func TestCancelledParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params CancelledParams
	}{
		{
			name: "without reason",
			params: CancelledParams{
				RequestID: NewNumberID(1),
			},
		},
		{
			name: "with reason",
			params: CancelledParams{
				RequestID: NewStringID("req-123"),
				Reason:    "User cancelled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got CancelledParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.RequestID.String() != tt.params.RequestID.String() {
				t.Errorf("RequestID = %s, want %s", got.RequestID.String(), tt.params.RequestID.String())
			}
			if got.Reason != tt.params.Reason {
				t.Errorf("Reason = %s, want %s", got.Reason, tt.params.Reason)
			}
		})
	}
}

// TestResourcesUpdatedParams_MarshalUnmarshal tests ResourcesUpdatedParams marshaling and unmarshaling.
func TestResourcesUpdatedParams_MarshalUnmarshal(t *testing.T) {
	params := ResourcesUpdatedParams{URI: "file:///path/to/file.txt"}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ResourcesUpdatedParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.URI != params.URI {
		t.Errorf("URI = %s, want %s", got.URI, params.URI)
	}
}

// TestLoggingSetLevelParams_MarshalUnmarshal tests LoggingSetLevelParams marshaling and unmarshaling.
func TestLoggingSetLevelParams_MarshalUnmarshal(t *testing.T) {
	levels := []LogLevel{
		LogLevelDebug,
		LogLevelInfo,
		LogLevelNotice,
		LogLevelWarning,
		LogLevelError,
		LogLevelCritical,
		LogLevelAlert,
		LogLevelEmergency,
	}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			params := LoggingSetLevelParams{Level: level}

			data, err := json.Marshal(params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got LoggingSetLevelParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Level != level {
				t.Errorf("Level = %s, want %s", got.Level, level)
			}
		})
	}
}

// TestLoggingMessageParams_MarshalUnmarshal tests LoggingMessageParams marshaling and unmarshaling.
func TestLoggingMessageParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params LoggingMessageParams
	}{
		{
			name: "simple message",
			params: LoggingMessageParams{
				Level: LogLevelInfo,
				Data:  "Log message",
			},
		},
		{
			name: "message with logger",
			params: LoggingMessageParams{
				Level:  LogLevelError,
				Logger: "mcp-server",
				Data:   "Error occurred",
			},
		},
		{
			name: "structured data",
			params: LoggingMessageParams{
				Level:  LogLevelDebug,
				Logger: "worker",
				Data: map[string]any{
					"task":     "process",
					"duration": 1.5,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got LoggingMessageParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Level != tt.params.Level {
				t.Errorf("Level = %s, want %s", got.Level, tt.params.Level)
			}
			if got.Logger != tt.params.Logger {
				t.Errorf("Logger = %s, want %s", got.Logger, tt.params.Logger)
			}
		})
	}
}

// TestPaginationParams_MarshalUnmarshal tests PaginationParams marshaling and unmarshaling.
func TestPaginationParams_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		params PaginationParams
	}{
		{
			name:   "empty cursor",
			params: PaginationParams{},
		},
		{
			name: "with cursor",
			params: PaginationParams{
				Cursor: "cursor-abc-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got PaginationParams
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Cursor != tt.params.Cursor {
				t.Errorf("Cursor = %s, want %s", got.Cursor, tt.params.Cursor)
			}
		})
	}
}

// TestResourcesSubscribeParams_MarshalUnmarshal tests ResourcesSubscribeParams marshaling and unmarshaling.
func TestResourcesSubscribeParams_MarshalUnmarshal(t *testing.T) {
	params := ResourcesSubscribeParams{URI: "file:///path/to/watched/file.txt"}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ResourcesSubscribeParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.URI != params.URI {
		t.Errorf("URI = %s, want %s", got.URI, params.URI)
	}
}

// TestResourcesUnsubscribeParams_MarshalUnmarshal tests ResourcesUnsubscribeParams marshaling and unmarshaling.
func TestResourcesUnsubscribeParams_MarshalUnmarshal(t *testing.T) {
	params := ResourcesUnsubscribeParams{URI: "file:///path/to/watched/file.txt"}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ResourcesUnsubscribeParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got.URI != params.URI {
		t.Errorf("URI = %s, want %s", got.URI, params.URI)
	}
}

// TestRole_Values tests Role constant values.
func TestRole_Values(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if string(tt.role) != tt.want {
				t.Errorf("Role = %s, want %s", tt.role, tt.want)
			}
		})
	}
}

// TestLogLevel_Values tests LogLevel constant values.
func TestLogLevel_Values(t *testing.T) {
	tests := []struct {
		level LogLevel
		want  string
	}{
		{LogLevelDebug, "debug"},
		{LogLevelInfo, "info"},
		{LogLevelNotice, "notice"},
		{LogLevelWarning, "warning"},
		{LogLevelError, "error"},
		{LogLevelCritical, "critical"},
		{LogLevelAlert, "alert"},
		{LogLevelEmergency, "emergency"},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			if string(tt.level) != tt.want {
				t.Errorf("LogLevel = %s, want %s", tt.level, tt.want)
			}
		})
	}
}

// TestContentType_Values tests ContentType constant values.
func TestContentType_Values(t *testing.T) {
	tests := []struct {
		ctype ContentType
		want  string
	}{
		{ContentTypeText, "text"},
		{ContentTypeImage, "image"},
		{ContentTypeResource, "resource"},
	}

	for _, tt := range tests {
		t.Run(string(tt.ctype), func(t *testing.T) {
			if string(tt.ctype) != tt.want {
				t.Errorf("ContentType = %s, want %s", tt.ctype, tt.want)
			}
		})
	}
}

// TestJSONSchema_MarshalUnmarshal tests JSONSchema marshaling and unmarshaling.
func TestJSONSchema_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		schema JSONSchema
	}{
		{
			name: "simple object schema",
			schema: JSONSchema{
				Type: "object",
			},
		},
		{
			name: "schema with properties",
			schema: JSONSchema{
				Type: "object",
				Properties: map[string]*JSONSchema{
					"name":  {Type: "string", Description: "The name"},
					"count": {Type: "integer"},
				},
				Required: []string{"name"},
			},
		},
		{
			name: "array schema",
			schema: JSONSchema{
				Type: "array",
				Items: &JSONSchema{
					Type: "string",
				},
			},
		},
		{
			name: "schema with enum",
			schema: JSONSchema{
				Type: "string",
				Enum: []any{"option1", "option2", "option3"},
			},
		},
		{
			name: "schema with default",
			schema: JSONSchema{
				Type:    "string",
				Default: "default-value",
			},
		},
		{
			name: "schema with additionalProperties",
			schema: JSONSchema{
				Type:                 "object",
				AdditionalProperties: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.schema)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got JSONSchema
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Type != tt.schema.Type {
				t.Errorf("Type = %s, want %s", got.Type, tt.schema.Type)
			}
			if got.Description != tt.schema.Description {
				t.Errorf("Description = %s, want %s", got.Description, tt.schema.Description)
			}
		})
	}
}

// TestCapabilities_ServerCapabilities tests ServerCapabilities marshaling and unmarshaling.
func TestCapabilities_ServerCapabilities(t *testing.T) {
	tests := []struct {
		name string
		caps ServerCapabilities
	}{
		{
			name: "empty capabilities",
			caps: ServerCapabilities{},
		},
		{
			name: "all capabilities enabled",
			caps: ServerCapabilities{
				Logging:   &LoggingCapability{},
				Prompts:   &PromptsCapability{ListChanged: true},
				Resources: &ResourcesCapability{Subscribe: true, ListChanged: true},
				Tools:     &ToolsCapability{ListChanged: true},
			},
		},
		{
			name: "experimental capabilities",
			caps: ServerCapabilities{
				Experimental: map[string]any{
					"feature1": true,
					"feature2": map[string]any{"nested": "value"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.caps)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ServerCapabilities
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Check logging capability
			if (tt.caps.Logging == nil) != (got.Logging == nil) {
				t.Errorf("Logging mismatch")
			}

			// Check tools capability
			if (tt.caps.Tools == nil) != (got.Tools == nil) {
				t.Errorf("Tools mismatch")
			}
			if tt.caps.Tools != nil && got.Tools != nil {
				if tt.caps.Tools.ListChanged != got.Tools.ListChanged {
					t.Errorf("Tools.ListChanged = %v, want %v", got.Tools.ListChanged, tt.caps.Tools.ListChanged)
				}
			}
		})
	}
}

// TestCapabilities_ClientCapabilities tests ClientCapabilities marshaling and unmarshaling.
func TestCapabilities_ClientCapabilities(t *testing.T) {
	tests := []struct {
		name string
		caps ClientCapabilities
	}{
		{
			name: "empty capabilities",
			caps: ClientCapabilities{},
		},
		{
			name: "all capabilities enabled",
			caps: ClientCapabilities{
				Roots:    &RootsCapability{ListChanged: true},
				Sampling: &SamplingCapability{},
			},
		},
		{
			name: "experimental capabilities",
			caps: ClientCapabilities{
				Experimental: map[string]any{
					"feature1": "enabled",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.caps)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ClientCapabilities
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Check roots capability
			if (tt.caps.Roots == nil) != (got.Roots == nil) {
				t.Errorf("Roots mismatch")
			}
			if tt.caps.Roots != nil && got.Roots != nil {
				if tt.caps.Roots.ListChanged != got.Roots.ListChanged {
					t.Errorf("Roots.ListChanged = %v, want %v", got.Roots.ListChanged, tt.caps.Roots.ListChanged)
				}
			}

			// Check sampling capability
			if (tt.caps.Sampling == nil) != (got.Sampling == nil) {
				t.Errorf("Sampling mismatch")
			}
		})
	}
}

// TestEmbeddedResource_MarshalUnmarshal tests EmbeddedResource marshaling and unmarshaling.
func TestEmbeddedResource_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		resource EmbeddedResource
	}{
		{
			name: "text resource",
			resource: EmbeddedResource{
				URI:      "file:///path/to/file.txt",
				MimeType: "text/plain",
				Text:     "Hello, world!",
			},
		},
		{
			name: "binary resource",
			resource: EmbeddedResource{
				URI:      "file:///path/to/image.png",
				MimeType: "image/png",
				Blob:     "base64encodedcontent",
			},
		},
		{
			name: "minimal resource",
			resource: EmbeddedResource{
				URI: "db://table/data",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.resource)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got EmbeddedResource
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.URI != tt.resource.URI {
				t.Errorf("URI = %s, want %s", got.URI, tt.resource.URI)
			}
			if got.MimeType != tt.resource.MimeType {
				t.Errorf("MimeType = %s, want %s", got.MimeType, tt.resource.MimeType)
			}
			if got.Text != tt.resource.Text {
				t.Errorf("Text = %s, want %s", got.Text, tt.resource.Text)
			}
			if got.Blob != tt.resource.Blob {
				t.Errorf("Blob = %s, want %s", got.Blob, tt.resource.Blob)
			}
		})
	}
}

// TestToolAnnotations_MarshalUnmarshal tests ToolAnnotations marshaling and unmarshaling.
func TestToolAnnotations_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name        string
		annotations ToolAnnotations
	}{
		{
			name:        "empty annotations",
			annotations: ToolAnnotations{},
		},
		{
			name: "read-only tool",
			annotations: ToolAnnotations{
				Title:        "Read Tool",
				ReadOnlyHint: true,
			},
		},
		{
			name: "destructive tool",
			annotations: ToolAnnotations{
				Title:           "Delete Tool",
				DestructiveHint: true,
				OpenWorldHint:   true,
			},
		},
		{
			name: "idempotent tool",
			annotations: ToolAnnotations{
				Title:          "Update Tool",
				IdempotentHint: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.annotations)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var got ToolAnnotations
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Title != tt.annotations.Title {
				t.Errorf("Title = %s, want %s", got.Title, tt.annotations.Title)
			}
			if got.ReadOnlyHint != tt.annotations.ReadOnlyHint {
				t.Errorf("ReadOnlyHint = %v, want %v", got.ReadOnlyHint, tt.annotations.ReadOnlyHint)
			}
			if got.DestructiveHint != tt.annotations.DestructiveHint {
				t.Errorf("DestructiveHint = %v, want %v", got.DestructiveHint, tt.annotations.DestructiveHint)
			}
			if got.IdempotentHint != tt.annotations.IdempotentHint {
				t.Errorf("IdempotentHint = %v, want %v", got.IdempotentHint, tt.annotations.IdempotentHint)
			}
			if got.OpenWorldHint != tt.annotations.OpenWorldHint {
				t.Errorf("OpenWorldHint = %v, want %v", got.OpenWorldHint, tt.annotations.OpenWorldHint)
			}
		})
	}
}
