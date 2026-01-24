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
	host    *canvascore.Host
	manager *canvascore.Manager
}

// NewTool creates a canvas tool.
func NewTool(host *canvascore.Host, manager *canvascore.Manager) *Tool {
	return &Tool{host: host, manager: manager}
}

func (t *Tool) Name() string { return "canvas" }

func (t *Tool) Description() string {
	return "Manage canvas sessions, URLs, and realtime updates."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: url, present, push, reset, or snapshot.",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Canvas session id for URL or realtime updates.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Optional path under the canvas root to link to.",
			},
			"payload": map[string]interface{}{
				"type":        "object",
				"description": "Payload for canvas push events.",
			},
			"state": map[string]interface{}{
				"type":        "object",
				"description": "State snapshot for canvas reset.",
			},
			"role": map[string]interface{}{
				"type":        "string",
				"description": "Optional role for signed canvas URLs.",
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
	var input struct {
		Action    string          `json:"action"`
		Path      string          `json:"path"`
		SessionID string          `json:"session_id"`
		Payload   json.RawMessage `json:"payload"`
		State     json.RawMessage `json:"state"`
		Role      string          `json:"role"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}
	switch action {
	case "url", "present":
		if t.host == nil {
			return toolError("canvas host unavailable"), nil
		}
		url := t.host.CanvasURL("")
		if sessionID := strings.TrimSpace(input.SessionID); sessionID != "" {
			if signed, err := t.host.SignedSessionURL(canvascore.CanvasURLParams{}, sessionID, strings.TrimSpace(input.Role)); err == nil {
				url = signed
			} else {
				url = t.host.CanvasSessionURL(canvascore.CanvasURLParams{}, sessionID)
			}
		}
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
	case "push":
		if t.manager == nil {
			return toolError("canvas manager unavailable"), nil
		}
		if strings.TrimSpace(input.SessionID) == "" {
			return toolError("session_id is required"), nil
		}
		if len(input.Payload) == 0 {
			return toolError("payload is required"), nil
		}
		msg, err := t.manager.Push(ctx, strings.TrimSpace(input.SessionID), input.Payload)
		if err != nil {
			return toolError(fmt.Sprintf("push failed: %v", err)), nil
		}
		payload, err := json.MarshalIndent(map[string]interface{}{
			"ok":      true,
			"message": msg,
		}, "", "  ")
		if err != nil {
			return toolError(fmt.Sprintf("encode result: %v", err)), nil
		}
		return &agent.ToolResult{Content: string(payload)}, nil
	case "reset":
		if t.manager == nil {
			return toolError("canvas manager unavailable"), nil
		}
		if strings.TrimSpace(input.SessionID) == "" {
			return toolError("session_id is required"), nil
		}
		if len(input.State) == 0 {
			return toolError("state is required"), nil
		}
		msg, err := t.manager.Reset(ctx, strings.TrimSpace(input.SessionID), input.State)
		if err != nil {
			return toolError(fmt.Sprintf("reset failed: %v", err)), nil
		}
		payload, err := json.MarshalIndent(map[string]interface{}{
			"ok":      true,
			"message": msg,
		}, "", "  ")
		if err != nil {
			return toolError(fmt.Sprintf("encode result: %v", err)), nil
		}
		return &agent.ToolResult{Content: string(payload)}, nil
	case "snapshot":
		if t.manager == nil {
			return toolError("canvas manager unavailable"), nil
		}
		if strings.TrimSpace(input.SessionID) == "" {
			return toolError("session_id is required"), nil
		}
		state, events, err := t.manager.Snapshot(ctx, strings.TrimSpace(input.SessionID))
		if err != nil {
			return toolError(fmt.Sprintf("snapshot failed: %v", err)), nil
		}
		response := map[string]interface{}{
			"state":  nil,
			"events": events,
		}
		if state != nil {
			response["state"] = json.RawMessage(state.StateJSON)
		}
		payload, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return toolError(fmt.Sprintf("encode result: %v", err)), nil
		}
		return &agent.ToolResult{Content: string(payload)}, nil
	default:
		return toolError("unsupported action"), nil
	}
}

func toolError(message string) *agent.ToolResult {
	payload, _ := json.Marshal(map[string]string{"error": message})
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
