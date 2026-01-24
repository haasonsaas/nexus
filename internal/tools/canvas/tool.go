package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	canvascore "github.com/haasonsaas/nexus/internal/canvas"
)

// Tool exposes a minimal canvas control surface.
type Tool struct {
	host *canvascore.Host
}

// NewTool creates a canvas tool.
func NewTool(host *canvascore.Host) *Tool {
	return &Tool{host: host}
}

func (t *Tool) Name() string { return "canvas" }

func (t *Tool) Description() string {
	return "Return the canvas host URL for presenting workspace UI artifacts."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: url or present.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Optional path under the canvas root to link to.",
			},
		},
		"required": []string{"action"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *Tool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	_ = ctx
	if t.host == nil {
		return toolError("canvas host unavailable"), nil
	}
	var input struct {
		Action string `json:"action"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}
	if action != "url" && action != "present" {
		return toolError("unsupported action"), nil
	}

	url := t.host.CanvasURL("")
	if p := strings.TrimSpace(input.Path); p != "" {
		clean := path.Clean("/" + p)
		url = strings.TrimSuffix(url, "/") + clean
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"url": url,
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

func toolError(message string) *agent.ToolResult {
	payload, _ := json.Marshal(map[string]string{"error": message})
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
