package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/controlplane"
)

// Controller provides gateway control plane operations.
type Controller interface {
	controlplane.ConfigManager
	controlplane.GatewayManager
}

// Tool exposes gateway control-plane actions.
type Tool struct {
	controller Controller
}

// NewTool creates a gateway tool.
func NewTool(controller Controller) *Tool {
	return &Tool{controller: controller}
}

func (t *Tool) Name() string { return "gateway" }

func (t *Tool) Description() string {
	return "Gateway control plane actions: status, config.get, config.schema, config.apply."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: status, config.get, config.schema, config.apply.",
			},
			"raw": map[string]interface{}{
				"type":        "string",
				"description": "Raw config content for config.apply.",
			},
			"base_hash": map[string]interface{}{
				"type":        "string",
				"description": "Base hash for optimistic config.apply.",
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
	if t.controller == nil {
		return toolError("gateway controller unavailable"), nil
	}

	var input struct {
		Action   string `json:"action"`
		Raw      string `json:"raw"`
		BaseHash string `json:"base_hash"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}

	switch action {
	case "status":
		status, err := t.controller.GatewayStatus(ctx)
		if err != nil {
			return toolError(fmt.Sprintf("status: %v", err)), nil
		}
		return jsonResult(status), nil
	case "config.get":
		snapshot, err := t.controller.ConfigSnapshot(ctx)
		if err != nil {
			return toolError(fmt.Sprintf("config.get: %v", err)), nil
		}
		return jsonResult(snapshot), nil
	case "config.schema":
		schema, err := t.controller.ConfigSchema(ctx)
		if err != nil {
			return toolError(fmt.Sprintf("config.schema: %v", err)), nil
		}
		return &agent.ToolResult{Content: string(schema)}, nil
	case "config.apply":
		if strings.TrimSpace(input.Raw) == "" {
			return toolError("raw is required for config.apply"), nil
		}
		result, err := t.controller.ApplyConfig(ctx, input.Raw, strings.TrimSpace(input.BaseHash))
		if err != nil {
			return toolError(fmt.Sprintf("config.apply: %v", err)), nil
		}
		return jsonResult(result), nil
	default:
		return toolError("unsupported action"), nil
	}
}

func jsonResult(payload any) *agent.ToolResult {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err))
	}
	return &agent.ToolResult{Content: string(encoded)}
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
