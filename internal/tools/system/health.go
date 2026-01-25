// Package system provides system-level tools for health, usage, and diagnostics.
package system

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/commands"
)

// HealthProvider provides health check functionality.
type HealthProvider interface {
	Check(ctx context.Context, opts *commands.HealthCheckOptions) (*commands.HealthSummary, error)
}

// HealthTool provides health check capabilities to the agent.
type HealthTool struct {
	provider HealthProvider
}

// NewHealthTool creates a new health check tool.
func NewHealthTool(provider HealthProvider) *HealthTool {
	return &HealthTool{provider: provider}
}

// Name returns the tool name.
func (t *HealthTool) Name() string { return "system_health" }

// Description returns the tool description.
func (t *HealthTool) Description() string {
	return "Check system health status including channels, agents, and sessions."
}

// Schema returns the JSON schema for the tool parameters.
func (t *HealthTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"probe_channels": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to actively probe channels (may be slower).",
				"default":     false,
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in milliseconds for health checks.",
				"default":     10000,
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

// Execute performs the health check.
func (t *HealthTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.provider == nil {
		return toolError("health provider unavailable"), nil
	}

	var input struct {
		ProbeChannels bool  `json:"probe_channels"`
		TimeoutMs     int64 `json:"timeout_ms"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	opts := &commands.HealthCheckOptions{
		TimeoutMs:     input.TimeoutMs,
		ProbeChannels: &input.ProbeChannels,
	}

	summary, err := t.provider.Check(ctx, opts)
	if err != nil {
		return toolError(fmt.Sprintf("health check failed: %v", err)), nil
	}

	formatted := commands.FormatHealthSummary(summary)
	return &agent.ToolResult{Content: formatted}, nil
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
