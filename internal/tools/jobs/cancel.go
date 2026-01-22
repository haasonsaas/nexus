package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/jobs"
)

// CancelTool allows cancelling a running job.
type CancelTool struct {
	store jobs.Store
}

// NewCancelTool returns a job cancel tool.
func NewCancelTool(store jobs.Store) *CancelTool {
	return &CancelTool{store: store}
}

func (t *CancelTool) Name() string { return "job_cancel" }

func (t *CancelTool) Description() string {
	return "Cancel a running async job by job_id"
}

func (t *CancelTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","description":"The ID of the job to cancel"}},"required":["job_id"]}`)
}

func (t *CancelTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
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

	// First check if job exists
	job, err := t.store.Get(ctx, input.JobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return &agent.ToolResult{Content: "job not found", IsError: true}, nil
	}
	if job.Status != jobs.StatusRunning && job.Status != jobs.StatusQueued {
		return &agent.ToolResult{
			Content: fmt.Sprintf("job cannot be cancelled (status: %s)", job.Status),
			IsError: true,
		}, nil
	}

	// Cancel the job
	if err := t.store.Cancel(ctx, input.JobID); err != nil {
		return nil, err
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Job %s cancelled successfully", input.JobID),
	}, nil
}

// ListTool lists jobs with optional filtering.
type ListTool struct {
	store jobs.Store
}

// NewListTool returns a job list tool.
func NewListTool(store jobs.Store) *ListTool {
	return &ListTool{store: store}
}

func (t *ListTool) Name() string { return "job_list" }

func (t *ListTool) Description() string {
	return "List recent async jobs with optional filtering"
}

func (t *ListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","description":"Max number of jobs to return (default 10)","default":10},"status":{"type":"string","description":"Filter by status: queued, running, succeeded, failed"}}}`)
}

func (t *ListTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return &agent.ToolResult{Content: "job store unavailable", IsError: true}, nil
	}
	var input struct {
		Limit  int    `json:"limit"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, err
	}
	if input.Limit <= 0 {
		input.Limit = 10
	}

	jobList, err := t.store.List(ctx, input.Limit, 0)
	if err != nil {
		return nil, err
	}

	// Filter by status if specified
	if input.Status != "" {
		filtered := make([]*jobs.Job, 0)
		targetStatus := jobs.Status(input.Status)
		for _, j := range jobList {
			if j.Status == targetStatus {
				filtered = append(filtered, j)
			}
		}
		jobList = filtered
	}

	if len(jobList) == 0 {
		return &agent.ToolResult{Content: "no jobs found"}, nil
	}

	payload, err := json.Marshal(jobList)
	if err != nil {
		return nil, err
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}
