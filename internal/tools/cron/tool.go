package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	croncore "github.com/haasonsaas/nexus/internal/cron"
)

// Tool exposes cron scheduler actions.
type Tool struct {
	scheduler *croncore.Scheduler
}

// NewTool creates a cron tool.
func NewTool(scheduler *croncore.Scheduler) *Tool {
	return &Tool{scheduler: scheduler}
}

func (t *Tool) Name() string { return "cron" }

func (t *Tool) Description() string {
	return "Inspect and trigger configured cron jobs (list/status/run)."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: list, status, run.",
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Job id for run action.",
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
	if t.scheduler == nil {
		return toolError("cron scheduler unavailable"), nil
	}
	var input struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}

	switch action {
	case "list", "status":
		jobs := t.scheduler.Jobs()
		payload, _ := json.MarshalIndent(map[string]interface{}{
			"jobs": jobs,
		}, "", "  ")
		return &agent.ToolResult{Content: string(payload)}, nil
	case "run":
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return toolError("id is required"), nil
		}
		if err := t.scheduler.RunJob(ctx, id); err != nil {
			return toolError(fmt.Sprintf("run job: %v", err)), nil
		}
		payload, _ := json.MarshalIndent(map[string]interface{}{
			"status": "ran",
			"id":     id,
		}, "", "  ")
		return &agent.ToolResult{Content: string(payload)}, nil
	default:
		return toolError("unsupported action"), nil
	}
}

func toolError(message string) *agent.ToolResult {
	payload, _ := json.Marshal(map[string]string{"error": message})
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
