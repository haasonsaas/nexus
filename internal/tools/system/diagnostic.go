// Package system provides system-level tools for health, usage, and diagnostics.
package system

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/infra"
)

// DiagnosticProvider provides diagnostic information.
type DiagnosticProvider interface {
	GetActivityStats() channels.ActivityStats
	GetMigrationStatus() (current, latest infra.MigrationVersion, pending int, err error)
}

// DiagnosticTool provides diagnostic information to the agent.
type DiagnosticTool struct {
	provider DiagnosticProvider
}

// NewDiagnosticTool creates a new diagnostic tool.
func NewDiagnosticTool(provider DiagnosticProvider) *DiagnosticTool {
	return &DiagnosticTool{provider: provider}
}

// Name returns the tool name.
func (t *DiagnosticTool) Name() string { return "system_diagnostic" }

// Description returns the tool description.
func (t *DiagnosticTool) Description() string {
	return "Get system diagnostic information including activity stats and migration status."
}

// Schema returns the JSON schema for the tool parameters.
func (t *DiagnosticTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"section": map[string]interface{}{
				"type":        "string",
				"description": "Diagnostic section: 'activity', 'migrations', or 'all' (default).",
				"default":     "all",
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

// Execute retrieves diagnostic information.
func (t *DiagnosticTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.provider == nil {
		return toolError("diagnostic provider unavailable"), nil
	}

	var input struct {
		Section string `json:"section"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("invalid parameters: %v", err)), nil
	}

	section := input.Section
	if section == "" {
		section = "all"
	}

	result := make(map[string]interface{})

	if section == "all" || section == "activity" {
		stats := t.provider.GetActivityStats()
		result["activity"] = map[string]interface{}{
			"total_channels":  stats.TotalChannels,
			"total_inbound":   stats.TotalInbound,
			"total_outbound":  stats.TotalOutbound,
			"recent_inbound":  stats.RecentInbound,
			"recent_outbound": stats.RecentOutbound,
			"by_channel":      stats.ByChannel,
		}
	}

	if section == "all" || section == "migrations" {
		current, latest, pending, err := t.provider.GetMigrationStatus()
		if err != nil {
			result["migrations"] = map[string]interface{}{
				"error": err.Error(),
			}
		} else {
			result["migrations"] = map[string]interface{}{
				"current_version": current,
				"latest_version":  latest,
				"pending_count":   pending,
			}
		}
	}

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}

	return &agent.ToolResult{Content: string(encoded)}, nil
}
