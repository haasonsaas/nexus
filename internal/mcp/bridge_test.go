package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeToolCaller struct {
	serverID string
	toolName string
	args     map[string]any
	result   *ToolCallResult
	err      error
}

func (f *fakeToolCaller) CallTool(ctx context.Context, serverID, toolName string, arguments map[string]any) (*ToolCallResult, error) {
	f.serverID = serverID
	f.toolName = toolName
	f.args = arguments
	return f.result, f.err
}

type fakeResourceReader struct {
	serverID string
	uri      string
	result   []*ResourceContent
	err      error
}

func (f *fakeResourceReader) ReadResource(ctx context.Context, serverID, uri string) ([]*ResourceContent, error) {
	f.serverID = serverID
	f.uri = uri
	return f.result, f.err
}

type fakePromptGetter struct {
	serverID string
	name     string
	args     map[string]string
	result   *GetPromptResult
	err      error
}

func (f *fakePromptGetter) GetPrompt(ctx context.Context, serverID, name string, arguments map[string]string) (*GetPromptResult, error) {
	f.serverID = serverID
	f.name = name
	f.args = arguments
	return f.result, f.err
}

func TestSafeToolNameSanitizes(t *testing.T) {
	used := make(map[string]struct{})
	name := safeToolName("git-hub", "search/repo", used)
	if name != "mcp_git_hub_search_repo" {
		t.Fatalf("expected sanitized name, got %q", name)
	}
}

func TestSafeToolNameDeduplicates(t *testing.T) {
	used := make(map[string]struct{})
	first := safeToolName("foo-bar", "baz", used)
	second := safeToolName("foo_bar", "baz", used)

	if first == second {
		t.Fatalf("expected unique name for duplicate tool, got %q", second)
	}
	if !strings.HasPrefix(second, first+"_") {
		t.Fatalf("expected duplicate name to include hash suffix, got %q", second)
	}
}

func TestSafeToolNameTruncates(t *testing.T) {
	used := make(map[string]struct{})
	serverID := strings.Repeat("server", 10)
	toolName := strings.Repeat("tool", 10)
	name := safeToolName(serverID, toolName, used)

	if len(name) > maxToolNameLen {
		t.Fatalf("expected name length <= %d, got %d (%q)", maxToolNameLen, len(name), name)
	}
	if !strings.HasSuffix(name, toolNameHash(serverID, toolName)) {
		t.Fatalf("expected truncated name to include hash suffix, got %q", name)
	}
}

func TestMCPToolBridgeExecute(t *testing.T) {
	caller := &fakeToolCaller{
		result: &ToolCallResult{
			Content: []ToolResultContent{{Type: "text", Text: "ok"}},
		},
	}
	tool := &MCPTool{
		Name:        "do_thing",
		Description: "Does the thing",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
	}
	bridge := NewToolBridge(caller, "server", tool, "mcp_server_do_thing")

	result, err := bridge.Execute(context.Background(), json.RawMessage(`{"value":"hi"}`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("expected content %q, got %q", "ok", result.Content)
	}
	if caller.serverID != "server" || caller.toolName != "do_thing" {
		t.Fatalf("expected call server/tool %q/%q, got %q/%q", "server", "do_thing", caller.serverID, caller.toolName)
	}
	if caller.args["value"] != "hi" {
		t.Fatalf("expected arg value %q, got %v", "hi", caller.args["value"])
	}
}

func TestResourceReadBridgeExecute(t *testing.T) {
	reader := &fakeResourceReader{
		result: []*ResourceContent{{URI: "file://note.txt", Text: "hello"}},
	}
	bridge := NewResourceReadBridge(reader, "server", "mcp_server_resource_read")

	result, err := bridge.Execute(context.Background(), json.RawMessage(`{"uri":"file://note.txt"}`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", result.Content)
	}
	if reader.serverID != "server" || reader.uri != "file://note.txt" {
		t.Fatalf("expected reader called with server/uri, got %q/%q", reader.serverID, reader.uri)
	}
}

func TestPromptGetBridgeExecute(t *testing.T) {
	getter := &fakePromptGetter{
		result: &GetPromptResult{
			Messages: []PromptMessage{
				{Role: "assistant", Content: MessageContent{Type: "text", Text: "hello"}},
			},
		},
	}
	bridge := NewPromptGetBridge(getter, "server", "mcp_server_prompt_get")

	result, err := bridge.Execute(context.Background(), json.RawMessage(`{"name":"greet","arguments":{"name":"Jane"}}`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", result.Content)
	}
	if getter.serverID != "server" || getter.name != "greet" {
		t.Fatalf("expected getter called with server/name, got %q/%q", getter.serverID, getter.name)
	}
	if getter.args["name"] != "Jane" {
		t.Fatalf("expected prompt arg %q, got %v", "Jane", getter.args["name"])
	}
}

func TestToolBridgeName(t *testing.T) {
	tool := &MCPTool{Name: "search"}
	bridge := NewToolBridge(nil, "server", tool, "mcp_server_search")
	if bridge.Name() != "mcp_server_search" {
		t.Errorf("expected name 'mcp_server_search', got %q", bridge.Name())
	}
}

func TestToolBridgeDescription(t *testing.T) {
	tests := []struct {
		name        string
		tool        *MCPTool
		serverID    string
		wantContain string
	}{
		{
			name:        "with description",
			tool:        &MCPTool{Name: "search", Description: "Search for files"},
			serverID:    "server",
			wantContain: "Search for files",
		},
		{
			name:        "without description",
			tool:        &MCPTool{Name: "search"},
			serverID:    "server",
			wantContain: "MCP tool server.search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := NewToolBridge(nil, tt.serverID, tt.tool, "mcp_server_search")
			desc := bridge.Description()
			if !strings.Contains(desc, tt.wantContain) {
				t.Errorf("expected description to contain %q, got %q", tt.wantContain, desc)
			}
		})
	}
}

func TestToolBridgeSchema(t *testing.T) {
	tests := []struct {
		name     string
		tool     *MCPTool
		expected string
	}{
		{
			name:     "with schema",
			tool:     &MCPTool{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)},
			expected: `{"type":"object"}`,
		},
		{
			name:     "without schema",
			tool:     &MCPTool{Name: "search"},
			expected: `{"type":"object"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := NewToolBridge(nil, "server", tt.tool, "mcp_server_search")
			schema := bridge.Schema()
			if string(schema) != tt.expected {
				t.Errorf("expected schema %q, got %q", tt.expected, string(schema))
			}
		})
	}
}

func TestResourceListBridgeName(t *testing.T) {
	bridge := NewResourceListBridge(nil, "server", "mcp_server_resources_list")
	if bridge.Name() != "mcp_server_resources_list" {
		t.Errorf("expected name 'mcp_server_resources_list', got %q", bridge.Name())
	}
}

func TestResourceListBridgeDescription(t *testing.T) {
	bridge := NewResourceListBridge(nil, "server", "mcp_server_resources_list")
	desc := bridge.Description()
	if !strings.Contains(desc, "server") {
		t.Errorf("expected description to contain 'server', got %q", desc)
	}
}

func TestResourceListBridgeSchema(t *testing.T) {
	bridge := NewResourceListBridge(nil, "server", "mcp_server_resources_list")
	schema := bridge.Schema()
	if string(schema) != `{"type":"object"}` {
		t.Errorf("expected schema %q, got %q", `{"type":"object"}`, string(schema))
	}
}

func TestResourceReadBridgeName(t *testing.T) {
	bridge := NewResourceReadBridge(nil, "server", "mcp_server_resource_read")
	if bridge.Name() != "mcp_server_resource_read" {
		t.Errorf("expected name 'mcp_server_resource_read', got %q", bridge.Name())
	}
}

func TestResourceReadBridgeDescription(t *testing.T) {
	bridge := NewResourceReadBridge(nil, "server", "mcp_server_resource_read")
	desc := bridge.Description()
	if !strings.Contains(desc, "server") {
		t.Errorf("expected description to contain 'server', got %q", desc)
	}
}

func TestResourceReadBridgeSchema(t *testing.T) {
	bridge := NewResourceReadBridge(nil, "server", "mcp_server_resource_read")
	schema := bridge.Schema()
	if !strings.Contains(string(schema), "uri") {
		t.Errorf("expected schema to contain 'uri', got %q", string(schema))
	}
}

func TestResourceReadBridgeExecuteEmptyURI(t *testing.T) {
	reader := &fakeResourceReader{}
	bridge := NewResourceReadBridge(reader, "server", "mcp_server_resource_read")

	_, err := bridge.Execute(context.Background(), json.RawMessage(`{"uri":""}`))
	if err == nil {
		t.Error("expected error for empty URI")
	}
}

func TestPromptListBridgeName(t *testing.T) {
	bridge := NewPromptListBridge(nil, "server", "mcp_server_prompts_list")
	if bridge.Name() != "mcp_server_prompts_list" {
		t.Errorf("expected name 'mcp_server_prompts_list', got %q", bridge.Name())
	}
}

func TestPromptListBridgeDescription(t *testing.T) {
	bridge := NewPromptListBridge(nil, "server", "mcp_server_prompts_list")
	desc := bridge.Description()
	if !strings.Contains(desc, "server") {
		t.Errorf("expected description to contain 'server', got %q", desc)
	}
}

func TestPromptListBridgeSchema(t *testing.T) {
	bridge := NewPromptListBridge(nil, "server", "mcp_server_prompts_list")
	schema := bridge.Schema()
	if string(schema) != `{"type":"object"}` {
		t.Errorf("expected schema %q, got %q", `{"type":"object"}`, string(schema))
	}
}

func TestPromptGetBridgeName(t *testing.T) {
	bridge := NewPromptGetBridge(nil, "server", "mcp_server_prompt_get")
	if bridge.Name() != "mcp_server_prompt_get" {
		t.Errorf("expected name 'mcp_server_prompt_get', got %q", bridge.Name())
	}
}

func TestPromptGetBridgeDescription(t *testing.T) {
	bridge := NewPromptGetBridge(nil, "server", "mcp_server_prompt_get")
	desc := bridge.Description()
	if !strings.Contains(desc, "server") {
		t.Errorf("expected description to contain 'server', got %q", desc)
	}
}

func TestPromptGetBridgeSchema(t *testing.T) {
	bridge := NewPromptGetBridge(nil, "server", "mcp_server_prompt_get")
	schema := bridge.Schema()
	if !strings.Contains(string(schema), "name") {
		t.Errorf("expected schema to contain 'name', got %q", string(schema))
	}
}

func TestPromptGetBridgeExecuteEmptyName(t *testing.T) {
	getter := &fakePromptGetter{}
	bridge := NewPromptGetBridge(getter, "server", "mcp_server_prompt_get")

	_, err := bridge.Execute(context.Background(), json.RawMessage(`{"name":""}`))
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestSanitizeToolPart(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"UPPER", "upper"},
		{"with-dash", "with_dash"},
		{"with.dot", "with_dot"},
		{"with/slash", "with_slash"},
		{"with space", "with_space"},
		{"  trimmed  ", "trimmed"},
		{"123numbers", "123numbers"},
		{"___", "tool"},
		{"", "tool"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeToolPart(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeToolPart(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatToolCallResult(t *testing.T) {
	tests := []struct {
		name     string
		result   *ToolCallResult
		wantText string
		wantErr  bool
	}{
		{
			name:     "nil result",
			result:   nil,
			wantText: "",
			wantErr:  false,
		},
		{
			name:     "empty content",
			result:   &ToolCallResult{Content: []ToolResultContent{}},
			wantText: "",
			wantErr:  false,
		},
		{
			name: "single text",
			result: &ToolCallResult{
				Content: []ToolResultContent{{Type: "text", Text: "hello"}},
			},
			wantText: "hello",
			wantErr:  false,
		},
		{
			name: "multiple text",
			result: &ToolCallResult{
				Content: []ToolResultContent{
					{Type: "text", Text: "line1"},
					{Type: "text", Text: "line2"},
				},
			},
			wantText: "line1\nline2",
			wantErr:  false,
		},
		{
			name: "error result",
			result: &ToolCallResult{
				Content: []ToolResultContent{{Type: "text", Text: "error message"}},
				IsError: true,
			},
			wantText: "error message",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := formatToolCallResult(tt.result)
			if text != tt.wantText {
				t.Errorf("formatToolCallResult() text = %q, want %q", text, tt.wantText)
			}
			if isErr != tt.wantErr {
				t.Errorf("formatToolCallResult() isError = %v, want %v", isErr, tt.wantErr)
			}
		})
	}
}

func TestFormatResourceContents(t *testing.T) {
	tests := []struct {
		name     string
		contents []*ResourceContent
		wantText string
	}{
		{
			name:     "nil contents",
			contents: nil,
			wantText: "",
		},
		{
			name:     "empty contents",
			contents: []*ResourceContent{},
			wantText: "",
		},
		{
			name: "single text content",
			contents: []*ResourceContent{
				{URI: "file://test.txt", Text: "hello world"},
			},
			wantText: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, _ := formatResourceContents(tt.contents)
			if text != tt.wantText {
				t.Errorf("formatResourceContents() = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestFormatPromptResult(t *testing.T) {
	tests := []struct {
		name     string
		result   *GetPromptResult
		wantText string
	}{
		{
			name:     "nil result",
			result:   nil,
			wantText: "",
		},
		{
			name:     "empty messages",
			result:   &GetPromptResult{Messages: []PromptMessage{}},
			wantText: "",
		},
		{
			name: "single text message",
			result: &GetPromptResult{
				Messages: []PromptMessage{
					{Role: "assistant", Content: MessageContent{Type: "text", Text: "hello"}},
				},
			},
			wantText: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, _ := formatPromptResult(tt.result)
			if text != tt.wantText {
				t.Errorf("formatPromptResult() = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestCanonicalNames(t *testing.T) {
	if got := canonicalToolName("server", "search"); got != "mcp:server.search" {
		t.Errorf("canonicalToolName() = %q", got)
	}
	if got := canonicalResourceList("server"); got != "mcp:server.resources.list" {
		t.Errorf("canonicalResourceList() = %q", got)
	}
	if got := canonicalResourceRead("server"); got != "mcp:server.resources.read" {
		t.Errorf("canonicalResourceRead() = %q", got)
	}
	if got := canonicalPromptList("server"); got != "mcp:server.prompts.list" {
		t.Errorf("canonicalPromptList() = %q", got)
	}
	if got := canonicalPromptGet("server"); got != "mcp:server.prompts.get" {
		t.Errorf("canonicalPromptGet() = %q", got)
	}
}
