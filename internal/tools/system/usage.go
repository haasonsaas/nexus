// Package system provides system-level tools for health, usage, and diagnostics.
package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/usage"
)

// UsageProvider provides usage data retrieval.
type UsageProvider interface {
	Get(ctx context.Context, provider string) (*usage.ProviderUsage, error)
	GetAll(ctx context.Context) []*usage.ProviderUsage
}

// UsageTool provides provider usage information to the agent.
type UsageTool struct {
	provider UsageProvider
}

// NewUsageTool creates a new usage tool.
func NewUsageTool(provider UsageProvider) *UsageTool {
	return &UsageTool{provider: provider}
}

// Name returns the tool name.
func (t *UsageTool) Name() string { return "provider_usage" }

// Description returns the tool description.
func (t *UsageTool) Description() string {
	return "Get LLM provider usage statistics including tokens and costs."
}

// Schema returns the JSON schema for the tool parameters.
func (t *UsageTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":        "string",
				"description": "Specific provider to get usage for (anthropic, openai, gemini). If not specified, returns all.",
			},
		},
		"required": []string{},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

// Execute retrieves usage data.
func (t *UsageTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.provider == nil {
		return toolError("usage provider unavailable"), nil
	}

	var input struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	providerName := strings.TrimSpace(strings.ToLower(input.Provider))

	if providerName != "" {
		u, err := t.provider.Get(ctx, providerName)
		if err != nil {
			return toolError(fmt.Sprintf("get usage failed: %v", err)), nil
		}
		return &agent.ToolResult{Content: usage.FormatProviderUsage(u)}, nil
	}

	// Get all providers
	usages := t.provider.GetAll(ctx)
	if len(usages) == 0 {
		return &agent.ToolResult{Content: "No provider usage data available."}, nil
	}

	var result strings.Builder
	for i, u := range usages {
		if i > 0 {
			result.WriteString("\n---\n\n")
		}
		result.WriteString(usage.FormatProviderUsage(u))
	}

	return &agent.ToolResult{Content: result.String()}, nil
}
