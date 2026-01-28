package models

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/models"
)

// Tool exposes model catalog discovery.
type Tool struct {
	catalog *models.Catalog
	bedrock *models.BedrockDiscovery
}

// NewTool creates a models tool.
func NewTool(catalog *models.Catalog, bedrock *models.BedrockDiscovery) *Tool {
	return &Tool{catalog: catalog, bedrock: bedrock}
}

func (t *Tool) Name() string { return "models" }

func (t *Tool) Description() string {
	return "List available LLM models and refresh discovery (bedrock)."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: list, providers, refresh.",
			},
			"provider": map[string]interface{}{
				"type":        "string",
				"description": "Filter by provider (list).",
			},
			"capability": map[string]interface{}{
				"type":        "string",
				"description": "Filter by capability (list).",
			},
			"tier": map[string]interface{}{
				"type":        "string",
				"description": "Filter by tier (list).",
			},
			"include_deprecated": map[string]interface{}{
				"type":        "boolean",
				"description": "Include deprecated models.",
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
	if t.catalog == nil {
		return toolError("model catalog unavailable"), nil
	}
	var input struct {
		Action            string `json:"action"`
		Provider          string `json:"provider"`
		Capability        string `json:"capability"`
		Tier              string `json:"tier"`
		IncludeDeprecated bool   `json:"include_deprecated"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}

	switch action {
	case "list":
		filter := models.Filter{}
		if provider := strings.TrimSpace(input.Provider); provider != "" {
			filter.Providers = []models.Provider{models.Provider(strings.ToLower(provider))}
		}
		if capability := strings.TrimSpace(input.Capability); capability != "" {
			filter.RequiredCapabilities = []models.Capability{models.Capability(strings.ToLower(capability))}
		}
		if tier := strings.TrimSpace(input.Tier); tier != "" {
			filter.Tiers = []models.Tier{models.Tier(strings.ToLower(tier))}
		}
		entries := t.catalog.List(&filter)
		items := make([]*models.Model, 0, len(entries))
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			if entry.Deprecated && !input.IncludeDeprecated {
				continue
			}
			items = append(items, entry)
		}
		return jsonResult(map[string]any{"models": items}), nil
	case "providers":
		providers := map[string]bool{}
		for _, entry := range t.catalog.List(nil) {
			if entry == nil {
				continue
			}
			providers[string(entry.Provider)] = true
		}
		out := make([]string, 0, len(providers))
		for provider := range providers {
			out = append(out, provider)
		}
		return jsonResult(map[string]any{"providers": out}), nil
	case "refresh":
		if t.bedrock == nil {
			return toolError("bedrock discovery not configured (set llm.bedrock.enabled)"), nil
		}
		if err := t.bedrock.RegisterWithCatalog(ctx, t.catalog); err != nil {
			return toolError(fmt.Sprintf("refresh: %v", err)), nil
		}
		return jsonResult(map[string]any{"status": "refreshed"}), nil
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
