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
