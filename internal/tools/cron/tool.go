package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
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
	return "Inspect and manage cron jobs (list/status/run/register/unregister/executions/prune)."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: list, status, run, register, unregister, executions, prune.",
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Job id for run/unregister actions.",
			},
			"job": map[string]interface{}{
				"type":        "object",
				"description": "Cron job configuration for register action.",
			},
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job id for executions action.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Limit for executions action.",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Offset for executions action.",
			},
			"older_than": map[string]interface{}{
				"type":        "string",
				"description": "Duration (e.g. 24h) for pruning execution history.",
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
		Action    string               `json:"action"`
		ID        string               `json:"id"`
		JobID     string               `json:"job_id"`
		Job       config.CronJobConfig `json:"job"`
		Limit     int                  `json:"limit"`
		Offset    int                  `json:"offset"`
		OlderThan string               `json:"older_than"`
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
		return jsonResult(map[string]interface{}{
			"jobs": jobs,
		}), nil
	case "run":
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return toolError("id is required"), nil
		}
		if err := t.scheduler.RunJob(ctx, id); err != nil {
			return toolError(fmt.Sprintf("run job: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"status": "ran",
			"id":     id,
		}), nil
	case "register":
		if strings.TrimSpace(input.Job.ID) == "" {
			return toolError("job.id is required"), nil
		}
		job, err := t.scheduler.RegisterJob(input.Job)
		if err != nil {
			return toolError(fmt.Sprintf("register job: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"status": "registered",
			"job":    job,
		}), nil
	case "unregister":
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return toolError("id is required"), nil
		}
		removed := t.scheduler.UnregisterJob(id)
		if !removed {
			return toolError("job not found"), nil
		}
		return jsonResult(map[string]interface{}{
			"status": "removed",
			"id":     id,
		}), nil
	case "executions":
		jobID := strings.TrimSpace(input.JobID)
		execs, err := t.scheduler.Executions(ctx, jobID, input.Limit, input.Offset)
		if err != nil {
			return toolError(fmt.Sprintf("list executions: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"job_id":     jobID,
			"executions": execs,
		}), nil
	case "prune":
		olderThan := strings.TrimSpace(input.OlderThan)
		if olderThan == "" {
			return toolError("older_than is required"), nil
		}
		duration, err := time.ParseDuration(olderThan)
		if err != nil {
			return toolError(fmt.Sprintf("invalid older_than: %v", err)), nil
		}
		count, err := t.scheduler.PruneExecutions(ctx, duration)
		if err != nil {
			return toolError(fmt.Sprintf("prune executions: %v", err)), nil
		}
		return jsonResult(map[string]interface{}{
			"status": "pruned",
			"count":  count,
		}), nil
	default:
		return toolError("unsupported action"), nil
	}
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}

func jsonResult(payload any) *agent.ToolResult {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err))
	}
	return &agent.ToolResult{Content: string(encoded)}
}
