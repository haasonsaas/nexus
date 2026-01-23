package mcp

import (
	"encoding/json"
	"testing"
	"time"
)

func TestServerConfigTransportTypes(t *testing.T) {
	tests := []struct {
		name      string
		transport TransportType
	}{
		{"stdio", TransportStdio},
		{"http", TransportHTTP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServerConfig{
				ID:        "test",
				Name:      "Test Server",
				Transport: tt.transport,
			}

			if cfg.Transport != tt.transport {
				t.Errorf("expected transport %v, got %v", tt.transport, cfg.Transport)
			}
		})
	}
}

func TestServerConfigJSON(t *testing.T) {
	cfg := &ServerConfig{
		ID:        "test-server",
		Name:      "Test Server",
		Transport: TransportStdio,
		Command:   "/usr/bin/mcp-server",
		Args:      []string{"--config", "test.yaml"},
		Env:       map[string]string{"DEBUG": "true"},
		WorkDir:   "/tmp",
		Timeout:   30 * time.Second,
		AutoStart: true,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ServerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.ID != cfg.ID {
		t.Errorf("expected ID %q, got %q", cfg.ID, decoded.ID)
	}
	if decoded.Command != cfg.Command {
		t.Errorf("expected Command %q, got %q", cfg.Command, decoded.Command)
	}
	if len(decoded.Args) != len(cfg.Args) {
		t.Errorf("expected %d args, got %d", len(cfg.Args), len(decoded.Args))
	}
}

func TestHTTPServerConfigJSON(t *testing.T) {
	cfg := &ServerConfig{
		ID:        "http-server",
		Name:      "HTTP Server",
		Transport: TransportHTTP,
		URL:       "https://mcp.example.com/api",
		Headers:   map[string]string{"Authorization": "Bearer token"},
		Timeout:   60 * time.Second,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ServerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.URL != cfg.URL {
		t.Errorf("expected URL %q, got %q", cfg.URL, decoded.URL)
	}
	if decoded.Headers["Authorization"] != "Bearer token" {
		t.Error("expected Authorization header")
	}
}

func TestMCPToolJSON(t *testing.T) {
	tool := &MCPTool{
		Name:        "search",
		Description: "Search for files",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	}

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded MCPTool
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Name != tool.Name {
		t.Errorf("expected Name %q, got %q", tool.Name, decoded.Name)
	}
	if decoded.Description != tool.Description {
		t.Errorf("expected Description %q, got %q", tool.Description, decoded.Description)
	}
}

func TestMCPResourceJSON(t *testing.T) {
	resource := &MCPResource{
		URI:         "file:///path/to/file.txt",
		Name:        "file.txt",
		Description: "A text file",
		MimeType:    "text/plain",
	}

	data, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded MCPResource
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.URI != resource.URI {
		t.Errorf("expected URI %q, got %q", resource.URI, decoded.URI)
	}
	if decoded.MimeType != resource.MimeType {
		t.Errorf("expected MimeType %q, got %q", resource.MimeType, decoded.MimeType)
	}
}

func TestMCPPromptJSON(t *testing.T) {
	prompt := &MCPPrompt{
		Name:        "code-review",
		Description: "Review code changes",
		Arguments: []PromptArgument{
			{Name: "file", Description: "File to review", Required: true},
			{Name: "language", Description: "Programming language", Required: false},
		},
	}

	data, err := json.Marshal(prompt)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded MCPPrompt
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(decoded.Arguments) != 2 {
		t.Fatalf("expected 2 arguments, got %d", len(decoded.Arguments))
	}
	if decoded.Arguments[0].Name != "file" {
		t.Errorf("expected first arg name 'file', got %q", decoded.Arguments[0].Name)
	}
	if !decoded.Arguments[0].Required {
		t.Error("expected first arg to be required")
	}
}

func TestResourceContentJSON(t *testing.T) {
	content := &ResourceContent{
		URI:      "file:///test.txt",
		MimeType: "text/plain",
		Text:     "Hello World",
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ResourceContent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Text != content.Text {
		t.Errorf("expected Text %q, got %q", content.Text, decoded.Text)
	}
}

func TestResourceContentWithBlob(t *testing.T) {
	content := &ResourceContent{
		URI:      "file:///image.png",
		MimeType: "image/png",
		Blob:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ResourceContent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Blob != content.Blob {
		t.Error("expected Blob to match")
	}
}

func TestPromptMessageJSON(t *testing.T) {
	msg := &PromptMessage{
		Role: "assistant",
		Content: MessageContent{
			Type: "text",
			Text: "Here is the response",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded PromptMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Role != msg.Role {
		t.Errorf("expected Role %q, got %q", msg.Role, decoded.Role)
	}
	if decoded.Content.Text != msg.Content.Text {
		t.Errorf("expected Content.Text %q, got %q", msg.Content.Text, decoded.Content.Text)
	}
}

func TestToolCallResultJSON(t *testing.T) {
	result := &ToolCallResult{
		Content: []ToolResultContent{
			{Type: "text", Text: "Result 1"},
			{Type: "text", Text: "Result 2"},
		},
		IsError: false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ToolCallResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(decoded.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(decoded.Content))
	}
	if decoded.IsError {
		t.Error("expected IsError to be false")
	}
}

func TestToolCallResultError(t *testing.T) {
	result := &ToolCallResult{
		Content: []ToolResultContent{
			{Type: "text", Text: "Error: something went wrong"},
		},
		IsError: true,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ToolCallResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !decoded.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestJSONRPCRequestJSON(t *testing.T) {
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search","arguments":{"query":"test"}}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded JSONRPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC '2.0', got %q", decoded.JSONRPC)
	}
	if decoded.Method != req.Method {
		t.Errorf("expected Method %q, got %q", req.Method, decoded.Method)
	}
}

func TestJSONRPCResponseJSON(t *testing.T) {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result:  json.RawMessage(`{"content":[{"type":"text","text":"result"}]}`),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded JSONRPCResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Error != nil {
		t.Error("expected no error in response")
	}
}

func TestJSONRPCResponseWithError(t *testing.T) {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Error: &JSONRPCError{
			Code:    ErrCodeMethodNotFound,
			Message: "Method not found",
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded JSONRPCResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Error == nil {
		t.Fatal("expected error in response")
	}
	if decoded.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("expected error code %d, got %d", ErrCodeMethodNotFound, decoded.Error.Code)
	}
}

func TestJSONRPCNotificationJSON(t *testing.T) {
	notif := &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/toolListChanged",
	}

	data, err := json.Marshal(notif)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded JSONRPCNotification
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Method != notif.Method {
		t.Errorf("expected Method %q, got %q", notif.Method, decoded.Method)
	}
}

func TestJSONRPCErrorCodes(t *testing.T) {
	tests := []struct {
		code int
		name string
	}{
		{ErrCodeParseError, "ParseError"},
		{ErrCodeInvalidRequest, "InvalidRequest"},
		{ErrCodeMethodNotFound, "MethodNotFound"},
		{ErrCodeInvalidParams, "InvalidParams"},
		{ErrCodeInternalError, "InternalError"},
		{ErrCodeResourceNotFound, "ResourceNotFound"},
		{ErrCodeToolNotFound, "ToolNotFound"},
		{ErrCodePromptNotFound, "PromptNotFound"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &JSONRPCError{
				Code:    tt.code,
				Message: tt.name + " error",
			}

			if err.Code != tt.code {
				t.Errorf("expected code %d, got %d", tt.code, err.Code)
			}
		})
	}
}

func TestInitializeResultJSON(t *testing.T) {
	result := &InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: Capabilities{
			Tools:     &ToolsCapability{ListChanged: true},
			Resources: &ResourcesCapability{Subscribe: true, ListChanged: true},
			Prompts:   &PromptsCapability{ListChanged: true},
		},
		ServerInfo: ServerInfo{
			Name:    "Test Server",
			Version: "1.0.0",
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded InitializeResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.ProtocolVersion != result.ProtocolVersion {
		t.Errorf("expected ProtocolVersion %q, got %q", result.ProtocolVersion, decoded.ProtocolVersion)
	}
	if decoded.ServerInfo.Name != result.ServerInfo.Name {
		t.Errorf("expected ServerInfo.Name %q, got %q", result.ServerInfo.Name, decoded.ServerInfo.Name)
	}
}

func TestSamplingRequestJSON(t *testing.T) {
	req := &SamplingRequest{
		Messages: []SamplingMessage{
			{
				Role: "user",
				Content: MessageContent{
					Type: "text",
					Text: "Hello",
				},
			},
		},
		ModelPrefs: &ModelPreferences{
			Hints: []ModelHint{
				{Name: "claude-3-opus"},
			},
		},
		SystemPrompt: "You are a helpful assistant",
		MaxTokens:    1000,
		Model:        "claude-3-opus",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded SamplingRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(decoded.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(decoded.Messages))
	}
	if decoded.MaxTokens != 1000 {
		t.Errorf("expected MaxTokens 1000, got %d", decoded.MaxTokens)
	}
}

func TestSamplingResponseJSON(t *testing.T) {
	resp := &SamplingResponse{
		Role: "assistant",
		Content: MessageContent{
			Type: "text",
			Text: "Hello! How can I help you?",
		},
		Model:      "claude-3-opus",
		StopReason: "end_turn",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded SamplingResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Role != resp.Role {
		t.Errorf("expected Role %q, got %q", resp.Role, decoded.Role)
	}
	if decoded.StopReason != resp.StopReason {
		t.Errorf("expected StopReason %q, got %q", resp.StopReason, decoded.StopReason)
	}
}

func TestCallToolParamsJSON(t *testing.T) {
	params := &CallToolParams{
		Name:      "search",
		Arguments: json.RawMessage(`{"query":"test"}`),
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded CallToolParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Name != params.Name {
		t.Errorf("expected Name %q, got %q", params.Name, decoded.Name)
	}
}

func TestServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty ID returns error",
			config:  &ServerConfig{},
			wantErr: true,
			errMsg:  "server ID is required",
		},
		{
			name: "valid stdio config",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportStdio,
				Command:   "/usr/bin/mcp-server",
			},
			wantErr: false,
		},
		{
			name: "valid http config",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportHTTP,
				URL:       "https://example.com/api",
			},
			wantErr: false,
		},
		{
			name: "stdio missing command",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportStdio,
			},
			wantErr: true,
			errMsg:  "command is required",
		},
		{
			name: "http missing URL",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportHTTP,
			},
			wantErr: true,
			errMsg:  "URL is required",
		},
		{
			name: "http invalid URL scheme",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportHTTP,
				URL:       "ftp://example.com",
			},
			wantErr: true,
			errMsg:  "URL must start with http://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestContainsShellMetachars(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"normal arg", false},
		{"--config=value", false},
		{"/path/to/file", false},
		{"$(whoami)", true},
		{"${USER}", true},
		{"`command`", true},
		{"cmd && other", true},
		{"cmd || other", true},
		{"cmd ; other", true},
		{"cmd | other", true},
		{"cmd > file", true},
		{"cmd < file", true},
		{"arg\nother", true},
		{"arg\rother", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := containsShellMetachars(tt.input)
			if result != tt.expected {
				t.Errorf("containsShellMetachars(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		path      string
		fieldName string
		wantErr   bool
	}{
		{"", "command", false},
		{"/usr/bin/command", "command", false},
		{"./relative/path", "command", false},
		{"../parent/../traversal", "command", true},
		{"../../outside", "command", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := validatePath(tt.path, tt.fieldName)
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestServerConfig_ValidateStdioConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *ServerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportStdio,
				Command:   "/usr/bin/mcp-server",
				Args:      []string{"--config", "test.yaml"},
			},
			wantErr: false,
		},
		{
			name: "command with path traversal",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportStdio,
				Command:   "../../../etc/passwd",
			},
			wantErr: true,
		},
		{
			name: "workdir with path traversal",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportStdio,
				Command:   "/usr/bin/mcp",
				WorkDir:   "../../../tmp",
			},
			wantErr: true,
		},
		{
			name: "args with shell metachar",
			config: &ServerConfig{
				ID:        "test",
				Transport: TransportStdio,
				Command:   "/usr/bin/mcp",
				Args:      []string{"--config", "$(whoami)"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCapabilities_Struct(t *testing.T) {
	caps := Capabilities{
		Tools:     &ToolsCapability{ListChanged: true},
		Resources: &ResourcesCapability{Subscribe: true, ListChanged: true},
		Prompts:   &PromptsCapability{ListChanged: true},
		Sampling:  &SamplingCapability{},
		Roots:     &RootsCapability{ListChanged: true},
	}

	if caps.Tools == nil || !caps.Tools.ListChanged {
		t.Error("expected Tools.ListChanged to be true")
	}
	if caps.Resources == nil || !caps.Resources.Subscribe {
		t.Error("expected Resources.Subscribe to be true")
	}
	if caps.Sampling == nil {
		t.Error("expected Sampling to be non-nil")
	}
}

func TestListResultsJSON(t *testing.T) {
	t.Run("ListToolsResult", func(t *testing.T) {
		result := &ListToolsResult{
			Tools: []*MCPTool{
				{Name: "tool1", Description: "First tool"},
				{Name: "tool2", Description: "Second tool"},
			},
		}
		data, _ := json.Marshal(result)
		var decoded ListToolsResult
		json.Unmarshal(data, &decoded)
		if len(decoded.Tools) != 2 {
			t.Errorf("expected 2 tools, got %d", len(decoded.Tools))
		}
	})

	t.Run("ListResourcesResult", func(t *testing.T) {
		result := &ListResourcesResult{
			Resources: []*MCPResource{
				{URI: "file:///a.txt", Name: "a.txt"},
			},
		}
		data, _ := json.Marshal(result)
		var decoded ListResourcesResult
		json.Unmarshal(data, &decoded)
		if len(decoded.Resources) != 1 {
			t.Errorf("expected 1 resource, got %d", len(decoded.Resources))
		}
	})

	t.Run("ListPromptsResult", func(t *testing.T) {
		result := &ListPromptsResult{
			Prompts: []*MCPPrompt{
				{Name: "prompt1", Description: "First prompt"},
			},
		}
		data, _ := json.Marshal(result)
		var decoded ListPromptsResult
		json.Unmarshal(data, &decoded)
		if len(decoded.Prompts) != 1 {
			t.Errorf("expected 1 prompt, got %d", len(decoded.Prompts))
		}
	})
}

func TestReadResourceResult(t *testing.T) {
	result := &ReadResourceResult{
		Contents: []*ResourceContent{
			{URI: "file:///test.txt", MimeType: "text/plain", Text: "content"},
		},
	}
	data, _ := json.Marshal(result)
	var decoded ReadResourceResult
	json.Unmarshal(data, &decoded)
	if len(decoded.Contents) != 1 {
		t.Errorf("expected 1 content, got %d", len(decoded.Contents))
	}
}

func TestGetPromptResult(t *testing.T) {
	result := &GetPromptResult{
		Description: "A test prompt",
		Messages: []PromptMessage{
			{Role: "user", Content: MessageContent{Type: "text", Text: "Hello"}},
		},
	}
	data, _ := json.Marshal(result)
	var decoded GetPromptResult
	json.Unmarshal(data, &decoded)
	if decoded.Description != "A test prompt" {
		t.Errorf("expected description %q, got %q", "A test prompt", decoded.Description)
	}
	if len(decoded.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(decoded.Messages))
	}
}

func TestClientInfo_Struct(t *testing.T) {
	info := ClientInfo{
		Name:    "test-client",
		Version: "1.0.0",
	}
	if info.Name != "test-client" {
		t.Errorf("expected name %q, got %q", "test-client", info.Name)
	}
}

func TestToolResultContent_Struct(t *testing.T) {
	content := ToolResultContent{
		Type:     "image",
		Data:     "base64data",
		MimeType: "image/png",
	}
	if content.Type != "image" {
		t.Errorf("expected type %q, got %q", "image", content.Type)
	}
	if content.Data != "base64data" {
		t.Errorf("expected data %q, got %q", "base64data", content.Data)
	}
}

func TestMessageContent_WithResource(t *testing.T) {
	content := MessageContent{
		Type: "resource",
		Resource: &ResourceContent{
			URI:      "file:///test.txt",
			MimeType: "text/plain",
			Text:     "content",
		},
	}
	data, _ := json.Marshal(content)
	var decoded MessageContent
	json.Unmarshal(data, &decoded)
	if decoded.Resource == nil {
		t.Fatal("expected resource to be non-nil")
	}
	if decoded.Resource.URI != "file:///test.txt" {
		t.Errorf("expected URI %q, got %q", "file:///test.txt", decoded.Resource.URI)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
