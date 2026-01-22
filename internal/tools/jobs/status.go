package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/jobs"
)

// StatusTool exposes job status via tool call.
type StatusTool struct {
	store jobs.Store
}

// NewStatusTool returns a job status tool.
func NewStatusTool(store jobs.Store) *StatusTool {
	return &StatusTool{store: store}
}

func (t *StatusTool) Name() string { return "job_status" }

func (t *StatusTool) Description() string {
	return "Fetch job status/result by job_id"
}

func (t *StatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string"}},"required":["job_id"]}`)
}

func (t *StatusTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return &agent.ToolResult{Content: "job store unavailable", IsError: true}, nil
	}
	var input struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, err
	}
	if input.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}
	job, err := t.store.Get(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return &agent.ToolResult{Content: "job not found", IsError: true}, nil
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return nil, err
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}
