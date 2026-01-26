package toolconv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/haasonsaas/nexus/internal/agent"
)

type stubTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (t stubTool) Name() string        { return t.name }
func (t stubTool) Description() string { return t.description }
func (t stubTool) Schema() json.RawMessage {
	return t.schema
}
func (t stubTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{Content: "ok"}, nil
}

func TestToBedrockTools(t *testing.T) {
	tools := []agent.Tool{
		stubTool{
			name:        "search",
			description: "Search tool",
			schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		},
		stubTool{
			name:        "broken",
			description: "Bad schema",
			schema:      json.RawMessage(`{not-json}`),
		},
	}

	cfg := ToBedrockTools(tools)
	if cfg == nil || len(cfg.Tools) != 2 {
		t.Fatalf("expected 2 bedrock tools, got %#v", cfg)
	}

	spec, ok := cfg.Tools[0].(*types.ToolMemberToolSpec)
	if !ok {
		t.Fatalf("expected ToolMemberToolSpec, got %T", cfg.Tools[0])
	}
	if spec.Value.Name == nil || *spec.Value.Name != "search" {
		t.Fatalf("unexpected tool name: %#v", spec.Value.Name)
	}
	if spec.Value.InputSchema == nil {
		t.Fatalf("expected input schema to be set")
	}
}
