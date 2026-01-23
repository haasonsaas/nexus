package reminders

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tasks"
)

// CancelTool cancels a reminder by ID.
type CancelTool struct {
	store tasks.Store
}

// NewCancelTool creates a new reminder cancel tool.
func NewCancelTool(store tasks.Store) *CancelTool {
	return &CancelTool{store: store}
}

func (t *CancelTool) Name() string { return "reminder_cancel" }

func (t *CancelTool) Description() string {
	return "Cancel a reminder by its ID"
}

func (t *CancelTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"reminder_id": {
				"type": "string",
				"description": "The ID of the reminder to cancel"
			}
		},
		"required": ["reminder_id"]
	}`)
}

// CancelInput is the input for the reminder cancel tool.
type CancelInput struct {
	ReminderID string `json:"reminder_id"`
}

// Execute cancels a reminder.
func (t *CancelTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return &agent.ToolResult{Content: "reminder store unavailable", IsError: true}, nil
	}

	var input CancelInput
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if input.ReminderID == "" {
		return &agent.ToolResult{Content: "reminder_id is required", IsError: true}, nil
	}

	// Get the task to verify it's a reminder and exists
	task, err := t.store.GetTask(ctx, input.ReminderID)
	if err != nil {
		return nil, fmt.Errorf("get reminder: %w", err)
	}

	if task == nil {
		return &agent.ToolResult{Content: "reminder not found", IsError: true}, nil
	}

	// Verify it's a reminder
	if task.Metadata == nil {
		return &agent.ToolResult{Content: "not a reminder", IsError: true}, nil
	}
	if taskType, ok := task.Metadata["type"].(string); !ok || taskType != "reminder" {
		return &agent.ToolResult{Content: "not a reminder", IsError: true}, nil
	}

	// Check if already cancelled or completed
	if task.Status == tasks.TaskStatusDisabled {
		return &agent.ToolResult{Content: "reminder already cancelled"}, nil
	}

	// Update status to disabled (cancelled)
	task.Status = tasks.TaskStatusDisabled
	if task.Metadata == nil {
		task.Metadata = make(map[string]any)
	}
	task.Metadata["cancelled_reason"] = "user_request"

	if err := t.store.UpdateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("cancel reminder: %w", err)
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Reminder cancelled: %s\nMessage was: %s", task.Name, task.Prompt),
	}, nil
}
