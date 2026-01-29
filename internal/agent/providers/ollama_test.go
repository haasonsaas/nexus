package providers

import (
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestBuildOllamaMessages_ToolCallsAndResults(t *testing.T) {
	req := &agent.CompletionRequest{
		System: "sys",
		Messages: []agent.CompletionMessage{
			{Role: "user", Content: "hi"},
			{
				Role: "assistant",
				ToolCalls: []models.ToolCall{
					{ID: "call-1", Name: "lookup", Input: json.RawMessage(`{"q":"test"}`)},
				},
			},
			{
				Role: "tool",
				ToolResults: []models.ToolResult{
					{ToolCallID: "call-1", Content: "ok"},
				},
			},
		},
	}

	msgs := buildOllamaMessages(req)
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "sys" {
		t.Fatalf("system message mismatch: %+v", msgs[0])
	}
	if msgs[2].Role != "assistant" || len(msgs[2].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls missing: %+v", msgs[2])
	}
	if msgs[2].ToolCalls[0].Function.Name != "lookup" {
		t.Errorf("tool name = %q, want %q", msgs[2].ToolCalls[0].Function.Name, "lookup")
	}
	if string(msgs[2].ToolCalls[0].Function.Arguments) != `{"q":"test"}` {
		t.Errorf("tool args = %s, want %s", string(msgs[2].ToolCalls[0].Function.Arguments), `{"q":"test"}`)
	}
	if msgs[3].Role != "tool" || msgs[3].ToolName != "lookup" || msgs[3].Content != "ok" {
		t.Errorf("tool result message mismatch: %+v", msgs[3])
	}
}
