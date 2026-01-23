package attention

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ListAttentionTool lists items in the attention feed.
type ListAttentionTool struct {
	feed *Feed
}

// NewListAttentionTool creates a new list attention tool.
func NewListAttentionTool(feed *Feed) *ListAttentionTool {
	return &ListAttentionTool{feed: feed}
}

func (t *ListAttentionTool) Name() string {
	return "attention_list"
}

func (t *ListAttentionTool) Description() string {
	return "List items requiring attention across all channels (email, Teams, ServiceNow, etc.). Can filter by channel, priority, status, and time range."
}

func (t *ListAttentionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"channel": {
				"type": "string",
				"description": "Filter by channel: email, teams, slack, servicenow, telegram, discord",
				"enum": ["email", "teams", "slack", "servicenow", "telegram", "discord"]
			},
			"status": {
				"type": "string",
				"description": "Filter by status: new, viewed, in_progress, snoozed, handled",
				"enum": ["new", "viewed", "in_progress", "snoozed", "handled"]
			},
			"priority": {
				"type": "string",
				"description": "Minimum priority: low, normal, high, urgent, critical",
				"enum": ["low", "normal", "high", "urgent", "critical"]
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of items to return (default 10)",
				"default": 10
			},
			"sort": {
				"type": "string",
				"description": "Sort order: newest, oldest, priority_high, priority_low",
				"enum": ["newest", "oldest", "priority_high", "priority_low"],
				"default": "newest"
			}
		}
	}`)
}

func (t *ListAttentionTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		Channel  string `json:"channel"`
		Status   string `json:"status"`
		Priority string `json:"priority"`
		Limit    int    `json:"limit"`
		Sort     string `json:"sort"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	opts := FeedOptions{
		Limit: input.Limit,
	}
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	// Map channel
	if input.Channel != "" {
		opts.Channels = []models.ChannelType{models.ChannelType(input.Channel)}
	}

	// Map status
	if input.Status != "" {
		opts.Statuses = []Status{Status(input.Status)}
		if input.Status == "snoozed" {
			opts.IncludeSnoozed = true
		}
	}

	// Map priority
	switch strings.ToLower(input.Priority) {
	case "low":
		opts.MinPriority = PriorityLow
	case "normal":
		opts.MinPriority = PriorityNormal
	case "high":
		opts.MinPriority = PriorityHigh
	case "urgent":
		opts.MinPriority = PriorityUrgent
	case "critical":
		opts.MinPriority = PriorityCritical
	}

	// Map sort
	switch strings.ToLower(input.Sort) {
	case "oldest":
		opts.SortBy = SortByReceivedAsc
	case "priority_high":
		opts.SortBy = SortByPriorityDesc
	case "priority_low":
		opts.SortBy = SortByPriorityAsc
	default:
		opts.SortBy = SortByReceivedDesc
	}

	items := t.feed.List(opts)

	if len(items) == 0 {
		return &agent.ToolResult{
			Content: "No attention items found matching the criteria.",
		}, nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d items requiring attention:\n\n", len(items)))

	for i, item := range items {
		result.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, strings.ToUpper(string(item.Channel)), item.Title))
		result.WriteString(fmt.Sprintf("   ID: %s | Priority: %s | Status: %s\n", item.ID, priorityName(item.Priority), item.Status))
		result.WriteString(fmt.Sprintf("   From: %s | Received: %s\n", item.Sender.Name, item.ReceivedAt.Format(time.RFC822)))
		if item.Preview != "" && item.Preview != item.Title {
			result.WriteString(fmt.Sprintf("   Preview: %s\n", item.Preview))
		}
		if i < len(items)-1 {
			result.WriteString("\n")
		}
	}

	return &agent.ToolResult{
		Content: result.String(),
	}, nil
}

// GetAttentionTool gets details of a specific attention item.
type GetAttentionTool struct {
	feed *Feed
}

// NewGetAttentionTool creates a new get attention tool.
func NewGetAttentionTool(feed *Feed) *GetAttentionTool {
	return &GetAttentionTool{feed: feed}
}

func (t *GetAttentionTool) Name() string {
	return "attention_get"
}

func (t *GetAttentionTool) Description() string {
	return "Get detailed information about a specific attention item by ID"
}

func (t *GetAttentionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "The attention item ID"
			}
		},
		"required": ["id"]
	}`)
}

func (t *GetAttentionTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.ID == "" {
		return &agent.ToolResult{
			Content: "id is required",
			IsError: true,
		}, nil
	}

	item, found := t.feed.Get(input.ID)
	if !found {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Attention item not found: %s", input.ID),
			IsError: true,
		}, nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Attention Item: %s\n", item.ID))
	result.WriteString(fmt.Sprintf("Type: %s | Channel: %s\n", item.Type, item.Channel))
	result.WriteString(fmt.Sprintf("Title: %s\n", item.Title))
	result.WriteString(fmt.Sprintf("Status: %s | Priority: %s\n", item.Status, priorityName(item.Priority)))
	result.WriteString(fmt.Sprintf("\nFrom: %s", item.Sender.Name))
	if item.Sender.Email != "" {
		result.WriteString(fmt.Sprintf(" <%s>", item.Sender.Email))
	}
	result.WriteString(fmt.Sprintf("\nReceived: %s\n", item.ReceivedAt.Format(time.RFC1123)))

	if item.ViewedAt != nil {
		result.WriteString(fmt.Sprintf("Viewed: %s\n", item.ViewedAt.Format(time.RFC1123)))
	}
	if item.SnoozedUntil != nil {
		result.WriteString(fmt.Sprintf("Snoozed until: %s\n", item.SnoozedUntil.Format(time.RFC1123)))
	}
	if item.HandledAt != nil {
		result.WriteString(fmt.Sprintf("Handled: %s\n", item.HandledAt.Format(time.RFC1123)))
	}

	if len(item.Tags) > 0 {
		result.WriteString(fmt.Sprintf("\nTags: %s\n", strings.Join(item.Tags, ", ")))
	}

	result.WriteString(fmt.Sprintf("\nContent:\n%s\n", item.Content))

	return &agent.ToolResult{
		Content: result.String(),
	}, nil
}

// HandleAttentionTool marks an attention item as handled.
type HandleAttentionTool struct {
	feed *Feed
}

// NewHandleAttentionTool creates a new handle attention tool.
func NewHandleAttentionTool(feed *Feed) *HandleAttentionTool {
	return &HandleAttentionTool{feed: feed}
}

func (t *HandleAttentionTool) Name() string {
	return "attention_handle"
}

func (t *HandleAttentionTool) Description() string {
	return "Mark an attention item as handled/resolved"
}

func (t *HandleAttentionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "The attention item ID to mark as handled"
			}
		},
		"required": ["id"]
	}`)
}

func (t *HandleAttentionTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.ID == "" {
		return &agent.ToolResult{
			Content: "id is required",
			IsError: true,
		}, nil
	}

	if !t.feed.MarkHandled(input.ID) {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Attention item not found: %s", input.ID),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Marked item %s as handled", input.ID),
	}, nil
}

// SnoozeAttentionTool snoozes an attention item.
type SnoozeAttentionTool struct {
	feed *Feed
}

// NewSnoozeAttentionTool creates a new snooze attention tool.
func NewSnoozeAttentionTool(feed *Feed) *SnoozeAttentionTool {
	return &SnoozeAttentionTool{feed: feed}
}

func (t *SnoozeAttentionTool) Name() string {
	return "attention_snooze"
}

func (t *SnoozeAttentionTool) Description() string {
	return "Snooze an attention item for a specified duration"
}

func (t *SnoozeAttentionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "The attention item ID to snooze"
			},
			"duration": {
				"type": "string",
				"description": "How long to snooze (e.g., '1h', '30m', '2h30m', '1d')"
			}
		},
		"required": ["id", "duration"]
	}`)
}

func (t *SnoozeAttentionTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		ID       string `json:"id"`
		Duration string `json:"duration"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.ID == "" {
		return &agent.ToolResult{
			Content: "id is required",
			IsError: true,
		}, nil
	}

	// Parse duration (support "d" for days)
	durationStr := strings.Replace(input.Duration, "d", "h", 1)
	if strings.Contains(input.Duration, "d") {
		// Convert days to hours
		parts := strings.Split(input.Duration, "d")
		if len(parts) >= 1 {
			var days int
			fmt.Sscanf(parts[0], "%d", &days)
			durationStr = fmt.Sprintf("%dh%s", days*24, strings.Join(parts[1:], ""))
		}
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid duration format: %s", input.Duration),
			IsError: true,
		}, nil
	}

	until := time.Now().Add(duration)

	if !t.feed.Snooze(input.ID, until) {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Attention item not found: %s", input.ID),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Snoozed item %s until %s", input.ID, until.Format(time.RFC822)),
	}, nil
}

// StatsAttentionTool returns statistics about the attention feed.
type StatsAttentionTool struct {
	feed *Feed
}

// NewStatsAttentionTool creates a new stats attention tool.
func NewStatsAttentionTool(feed *Feed) *StatsAttentionTool {
	return &StatsAttentionTool{feed: feed}
}

func (t *StatsAttentionTool) Name() string {
	return "attention_stats"
}

func (t *StatsAttentionTool) Description() string {
	return "Get statistics about items across all attention channels"
}

func (t *StatsAttentionTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t *StatsAttentionTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	stats := t.feed.Stats()

	var result strings.Builder
	result.WriteString("Attention Feed Statistics\n")
	result.WriteString("========================\n\n")
	result.WriteString(fmt.Sprintf("Total Items: %d\n", stats.TotalItems))
	result.WriteString(fmt.Sprintf("  New: %d\n", stats.NewItems))
	result.WriteString(fmt.Sprintf("  Viewed/In Progress: %d\n", stats.ViewedItems))
	result.WriteString(fmt.Sprintf("  Snoozed: %d\n", stats.SnoozedItems))

	if len(stats.ByChannel) > 0 {
		result.WriteString("\nBy Channel:\n")
		for ch, count := range stats.ByChannel {
			result.WriteString(fmt.Sprintf("  %s: %d\n", ch, count))
		}
	}

	if len(stats.ByPriority) > 0 {
		result.WriteString("\nBy Priority:\n")
		for p := int(PriorityCritical); p >= int(PriorityLow); p-- {
			if count, ok := stats.ByPriority[p]; ok {
				result.WriteString(fmt.Sprintf("  %s: %d\n", priorityName(Priority(p)), count))
			}
		}
	}

	if stats.OldestItem != nil {
		result.WriteString(fmt.Sprintf("\nOldest Item: %s\n", stats.OldestItem.Format(time.RFC822)))
	}
	if stats.NewestItem != nil {
		result.WriteString(fmt.Sprintf("Newest Item: %s\n", stats.NewestItem.Format(time.RFC822)))
	}

	return &agent.ToolResult{
		Content: result.String(),
	}, nil
}

// priorityName returns a human-readable priority name.
func priorityName(p Priority) string {
	switch p {
	case PriorityLow:
		return "Low"
	case PriorityNormal:
		return "Normal"
	case PriorityHigh:
		return "High"
	case PriorityUrgent:
		return "Urgent"
	case PriorityCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}
