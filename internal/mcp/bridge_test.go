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
