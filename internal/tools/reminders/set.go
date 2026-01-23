// Package reminders provides tools for setting and managing reminders.
package reminders

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tasks"
	"github.com/haasonsaas/nexus/pkg/models"
)

// SetTool creates a reminder that will send a message at a specified time.
type SetTool struct {
	store tasks.Store
}

// NewSetTool creates a new reminder set tool.
func NewSetTool(store tasks.Store) *SetTool {
	return &SetTool{store: store}
}

func (t *SetTool) Name() string { return "reminder_set" }

func (t *SetTool) Description() string {
	return "Set a reminder to send a message at a specified time. Use relative times like 'in 5 minutes', 'in 2 hours', or absolute times."
}

func (t *SetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "The reminder message to send when triggered"
			},
			"when": {
				"type": "string",
				"description": "When to send the reminder: 'in X minutes', 'in X hours', 'in X days', or an ISO8601 timestamp"
			},
			"title": {
				"type": "string",
				"description": "Optional short title for the reminder"
			}
		},
		"required": ["message", "when"]
	}`)
}

// SetInput is the input for the reminder set tool.
type SetInput struct {
	Message string `json:"message"`
	When    string `json:"when"`
	Title   string `json:"title"`
}

// Execute creates a reminder.
func (t *SetTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return &agent.ToolResult{Content: "reminder store unavailable", IsError: true}, nil
	}

	var input SetInput
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if input.Message == "" {
		return &agent.ToolResult{Content: "message is required", IsError: true}, nil
	}
	if input.When == "" {
		return &agent.ToolResult{Content: "when is required", IsError: true}, nil
	}

	// Parse the time specification
	triggerAt, err := parseWhen(input.When)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid time: %v", err), IsError: true}, nil
	}

	// Don't allow reminders in the past
	if triggerAt.Before(time.Now()) {
		return &agent.ToolResult{Content: "cannot set reminder in the past", IsError: true}, nil
	}

	// Get context from the current execution
	channelType := getChannelFromContext(ctx)
	channelID := getChannelIDFromContext(ctx)
	agentID := getAgentIDFromContext(ctx)

	// Create the scheduled task for the reminder
	task := &tasks.ScheduledTask{
		ID:          uuid.NewString(),
		Name:        formatReminderName(input.Title, input.Message),
		Description: "User-created reminder",
		AgentID:     agentID,
		// Use a one-time schedule that fires at the specified time
		Schedule:  fmt.Sprintf("@at %s", triggerAt.Format(time.RFC3339)),
		Prompt:    input.Message,
		Status:    tasks.TaskStatusActive,
		NextRunAt: triggerAt,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Config: tasks.TaskConfig{
			Channel:   string(channelType),
			ChannelID: channelID,
			// Set execution type to "message" for direct sending
			ExecutionType: tasks.ExecutionTypeMessage,
			MaxRetries:    2, // Retry a couple times if sending fails
		},
		Metadata: map[string]any{
			"type":        "reminder",
			"trigger_at":  triggerAt.Format(time.RFC3339),
			"original_at": input.When,
		},
	}

	if err := t.store.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("create reminder: %w", err)
	}

	// Format response
	duration := time.Until(triggerAt).Round(time.Second)
	response := fmt.Sprintf("Reminder set for %s (in %s)\nID: %s\nMessage: %s",
		triggerAt.Format("Mon Jan 2 3:04 PM"),
		formatDuration(duration),
		task.ID,
		input.Message,
	)

	return &agent.ToolResult{Content: response}, nil
}

// parseWhen parses a time specification into an absolute time.
// Supports formats like:
// - "in 5 minutes"
// - "in 2 hours"
// - "in 1 day"
// - ISO8601 timestamps
func parseWhen(when string) (time.Time, error) {
	when = strings.TrimSpace(strings.ToLower(when))

	// Try relative time first
	if strings.HasPrefix(when, "in ") {
		return parseRelativeTime(strings.TrimPrefix(when, "in "))
	}

	// Try various absolute formats
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"Jan 2 15:04",
		"Jan 2 3:04 PM",
		"3:04 PM",
		"15:04",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, when); err == nil {
			// If no date component, assume today
			if t.Year() == 0 {
				now := time.Now()
				t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
				// If the time has passed today, assume tomorrow
				if t.Before(now) {
					t = t.Add(24 * time.Hour)
				}
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse time: %s", when)
}

var relativeTimePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(seconds?|minutes?|mins?|hours?|hrs?|days?|weeks?)$`)

func parseRelativeTime(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	matches := relativeTimePattern.FindStringSubmatch(s)
	if matches == nil {
		return time.Time{}, fmt.Errorf("invalid relative time: %s", s)
	}

	amount, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid number: %s", matches[1])
	}

	unit := matches[2]
	var duration time.Duration

	switch {
	case strings.HasPrefix(unit, "second"):
		duration = time.Duration(amount * float64(time.Second))
	case strings.HasPrefix(unit, "min"):
		duration = time.Duration(amount * float64(time.Minute))
	case strings.HasPrefix(unit, "hour"), strings.HasPrefix(unit, "hr"):
		duration = time.Duration(amount * float64(time.Hour))
	case strings.HasPrefix(unit, "day"):
		duration = time.Duration(amount * float64(24*time.Hour))
	case strings.HasPrefix(unit, "week"):
		duration = time.Duration(amount * float64(7*24*time.Hour))
	default:
		return time.Time{}, fmt.Errorf("unknown unit: %s", unit)
	}

	return time.Now().Add(duration), nil
}

func formatReminderName(title, message string) string {
	if title != "" {
		return fmt.Sprintf("Reminder: %s", title)
	}
	// Use first 50 chars of message
	if len(message) > 50 {
		return fmt.Sprintf("Reminder: %s...", message[:47])
	}
	return fmt.Sprintf("Reminder: %s", message)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	if d < 24*time.Hour {
		hrs := d.Hours()
		if hrs < 2 {
			return "1 hour"
		}
		return fmt.Sprintf("%.1f hours", hrs)
	}
	days := d.Hours() / 24
	if days < 2 {
		return "1 day"
	}
	return fmt.Sprintf("%.1f days", days)
}

// getChannelFromContext extracts channel type from the session in context.
func getChannelFromContext(ctx context.Context) models.ChannelType {
	if session := agent.SessionFromContext(ctx); session != nil {
		return session.Channel
	}
	return models.ChannelWhatsApp // Default fallback
}

// getChannelIDFromContext extracts channel ID from the session in context.
func getChannelIDFromContext(ctx context.Context) string {
	if session := agent.SessionFromContext(ctx); session != nil {
		return session.ChannelID
	}
	return ""
}

// getAgentIDFromContext extracts agent ID from the session in context.
func getAgentIDFromContext(ctx context.Context) string {
	if session := agent.SessionFromContext(ctx); session != nil {
		return session.AgentID
	}
	return "default"
}
