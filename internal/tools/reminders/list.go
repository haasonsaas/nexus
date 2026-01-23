package reminders

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tasks"
)

// ListTool lists active reminders for the current user/session.
type ListTool struct {
	store tasks.Store
}

// NewListTool creates a new reminder list tool.
func NewListTool(store tasks.Store) *ListTool {
	return &ListTool{store: store}
}

func (t *ListTool) Name() string { return "reminder_list" }

func (t *ListTool) Description() string {
	return "List all active reminders"
}

func (t *ListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"include_completed": {
				"type": "boolean",
				"description": "Include completed/fired reminders (default false)"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of reminders to return (default 20)"
			}
		}
	}`)
}

// ListInput is the input for the reminder list tool.
type ListInput struct {
	IncludeCompleted bool `json:"include_completed"`
	Limit            int  `json:"limit"`
}

// Execute lists reminders.
func (t *ListTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return &agent.ToolResult{Content: "reminder store unavailable", IsError: true}, nil
	}

	var input ListInput
	if len(params) > 0 {
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
	}

	if input.Limit <= 0 {
		input.Limit = 20
	}

	agentID := getAgentIDFromContext(ctx)

	// List tasks for this agent
	taskList, err := t.store.ListTasks(ctx, tasks.ListTasksOptions{
		AgentID: agentID,
		Limit:   input.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}

	// Filter to only reminder-type tasks
	var reminders []*tasks.ScheduledTask
	for _, task := range taskList {
		if task.Metadata == nil {
			continue
		}
		if taskType, ok := task.Metadata["type"].(string); ok && taskType == "reminder" {
			// Filter by status unless including completed
			if !input.IncludeCompleted && task.Status != tasks.TaskStatusActive {
				continue
			}
			reminders = append(reminders, task)
		}
	}

	if len(reminders) == 0 {
		return &agent.ToolResult{Content: "No active reminders found."}, nil
	}

	// Format output
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d reminder(s):\n\n", len(reminders)))

	for i, r := range reminders {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, r.Name))
		sb.WriteString(fmt.Sprintf("   ID: %s\n", r.ID))
		sb.WriteString(fmt.Sprintf("   Message: %s\n", r.Prompt))

		if !r.NextRunAt.IsZero() {
			duration := time.Until(r.NextRunAt)
			if duration > 0 {
				sb.WriteString(fmt.Sprintf("   Fires: %s (in %s)\n", r.NextRunAt.Format("Mon Jan 2 3:04 PM"), formatDuration(duration)))
			} else {
				sb.WriteString(fmt.Sprintf("   Fires: %s\n", r.NextRunAt.Format("Mon Jan 2 3:04 PM")))
			}
		}

		sb.WriteString(fmt.Sprintf("   Status: %s\n", r.Status))
		sb.WriteString("\n")
	}

	return &agent.ToolResult{Content: sb.String()}, nil
}
