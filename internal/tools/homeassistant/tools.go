package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// CallServiceTool calls Home Assistant services (domain.service).
type CallServiceTool struct {
	client *Client
}

func NewCallServiceTool(client *Client) *CallServiceTool {
	return &CallServiceTool{client: client}
}

func (t *CallServiceTool) Name() string { return "ha_call_service" }

func (t *CallServiceTool) Description() string {
	return "Call a Home Assistant service (domain + service) with optional service_data."
}

func (t *CallServiceTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "domain": { "type": "string", "description": "Service domain (e.g., light, switch)" },
    "service": { "type": "string", "description": "Service name (e.g., turn_on, turn_off)" },
    "service_data": {
      "type": "object",
      "description": "Service data payload (e.g., {\"entity_id\":\"light.kitchen\"}).",
      "additionalProperties": true
    }
  },
  "required": ["domain", "service"]
}`)
}

func (t *CallServiceTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t == nil || t.client == nil {
		return toolError("Home Assistant client not configured (enable channels.homeassistant)"), nil
	}

	var input struct {
		Domain      string         `json:"domain"`
		Service     string         `json:"service"`
		ServiceData map[string]any `json:"service_data"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	payload, err := t.client.CallService(ctx, input.Domain, input.Service, input.ServiceData)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return jsonResult(payload), nil
}

// GetStateTool fetches a Home Assistant entity state.
type GetStateTool struct {
	client *Client
}

func NewGetStateTool(client *Client) *GetStateTool {
	return &GetStateTool{client: client}
}

func (t *GetStateTool) Name() string { return "ha_get_state" }

func (t *GetStateTool) Description() string {
	return "Get the current state + attributes for a Home Assistant entity_id."
}

func (t *GetStateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "entity_id": { "type": "string", "description": "Entity ID (e.g., light.kitchen)" }
  },
  "required": ["entity_id"]
}`)
}

func (t *GetStateTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t == nil || t.client == nil {
		return toolError("Home Assistant client not configured (enable channels.homeassistant)"), nil
	}

	var input struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	payload, err := t.client.GetState(ctx, input.EntityID)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return jsonResult(payload), nil
}

// ListEntitiesTool lists entity summaries from /api/states.
type ListEntitiesTool struct {
	client *Client
}

func NewListEntitiesTool(client *Client) *ListEntitiesTool {
	return &ListEntitiesTool{client: client}
}

func (t *ListEntitiesTool) Name() string { return "ha_list_entities" }

func (t *ListEntitiesTool) Description() string {
	return "List Home Assistant entities. Optional domain filter (e.g., \"light\")."
}

func (t *ListEntitiesTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "domain": { "type": "string", "description": "Optional domain filter (e.g., light, switch)." },
    "limit": { "type": "integer", "description": "Max entities to return (default 200).", "default": 200 }
  }
}`)
}

func (t *ListEntitiesTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t == nil || t.client == nil {
		return toolError("Home Assistant client not configured (enable channels.homeassistant)"), nil
	}

	var input struct {
		Domain string `json:"domain"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	if input.Limit <= 0 {
		input.Limit = 200
	}

	payload, err := t.client.ListStates(ctx)
	if err != nil {
		return toolError(err.Error()), nil
	}

	var states []map[string]any
	if err := json.Unmarshal(payload, &states); err != nil {
		return toolError(fmt.Sprintf("decode states: %v", err)), nil
	}

	type entitySummary struct {
		EntityID      string `json:"entity_id"`
		State         string `json:"state"`
		FriendlyName  string `json:"friendly_name,omitempty"`
		LastChanged   string `json:"last_changed,omitempty"`
		LastUpdated   string `json:"last_updated,omitempty"`
		Icon          string `json:"icon,omitempty"`
		DeviceClass   string `json:"device_class,omitempty"`
		UnitOfMeasure string `json:"unit_of_measurement,omitempty"`
	}

	domain := strings.ToLower(strings.TrimSpace(input.Domain))
	prefix := ""
	if domain != "" {
		prefix = domain + "."
	}

	out := make([]entitySummary, 0, min(input.Limit, len(states)))
	for _, item := range states {
		entityID, ok := item["entity_id"].(string)
		if !ok || entityID == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(entityID), prefix) {
			continue
		}

		summary := entitySummary{
			EntityID:    entityID,
			State:       fmt.Sprint(item["state"]),
			LastChanged: fmt.Sprint(item["last_changed"]),
			LastUpdated: fmt.Sprint(item["last_updated"]),
		}

		if attrs, ok := item["attributes"].(map[string]any); ok {
			if v, ok := attrs["friendly_name"].(string); ok {
				summary.FriendlyName = v
			}
			if v, ok := attrs["icon"].(string); ok {
				summary.Icon = v
			}
			if v, ok := attrs["device_class"].(string); ok {
				summary.DeviceClass = v
			}
			if v, ok := attrs["unit_of_measurement"].(string); ok {
				summary.UnitOfMeasure = v
			}
		}

		out = append(out, summary)
		if len(out) >= input.Limit {
			break
		}
	}

	encoded, err := json.MarshalIndent(map[string]any{
		"entities": out,
		"total":    len(out),
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(encoded)}, nil
}

func jsonResult(payload json.RawMessage) *agent.ToolResult {
	var anyValue any
	if err := json.Unmarshal(payload, &anyValue); err == nil {
		if indented, err := json.MarshalIndent(anyValue, "", "  "); err == nil {
			return &agent.ToolResult{Content: string(indented)}
		}
	}
	return &agent.ToolResult{Content: strings.TrimSpace(string(payload))}
}

func toolError(message string) *agent.ToolResult {
	encoded, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(encoded), IsError: true}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
