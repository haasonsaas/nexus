package servicenow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// ListTicketsTool lists ServiceNow incidents.
type ListTicketsTool struct {
	client *Client
}

// NewListTicketsTool creates a new list tickets tool.
func NewListTicketsTool(client *Client) *ListTicketsTool {
	return &ListTicketsTool{client: client}
}

func (t *ListTicketsTool) Name() string {
	return "servicenow_list_tickets"
}

func (t *ListTicketsTool) Description() string {
	return "List ServiceNow incidents/tickets. Can filter by state, priority, or assignment."
}

func (t *ListTicketsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"state": {
				"type": "string",
				"description": "Filter by state: new, in_progress, on_hold, resolved, closed",
				"enum": ["new", "in_progress", "on_hold", "resolved", "closed"]
			},
			"priority": {
				"type": "string",
				"description": "Filter by priority: critical, high, moderate, low",
				"enum": ["critical", "high", "moderate", "low"]
			},
			"assigned_to_me": {
				"type": "boolean",
				"description": "Show only tickets assigned to the current user"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of tickets to return (default 10)",
				"default": 10
			}
		}
	}`)
}

func (t *ListTicketsTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		State        string `json:"state"`
		Priority     string `json:"priority"`
		AssignedToMe bool   `json:"assigned_to_me"`
		Limit        int    `json:"limit"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	opts := ListIncidentsOptions{
		Limit: input.Limit,
	}

	if opts.Limit == 0 {
		opts.Limit = 10
	}

	// Map state names to ServiceNow state numbers
	switch strings.ToLower(input.State) {
	case "new":
		opts.State = "1"
	case "in_progress":
		opts.State = "2"
	case "on_hold":
		opts.State = "3"
	case "resolved":
		opts.State = "6"
	case "closed":
		opts.State = "7"
	}

	// Map priority names to ServiceNow priority numbers
	switch strings.ToLower(input.Priority) {
	case "critical":
		opts.Priority = "1"
	case "high":
		opts.Priority = "2"
	case "moderate":
		opts.Priority = "3"
	case "low":
		opts.Priority = "4"
	}

	incidents, err := t.client.ListIncidents(ctx, opts)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error listing tickets: %v", err),
			IsError: true,
		}, nil
	}

	if len(incidents) == 0 {
		return &agent.ToolResult{
			Content: "No tickets found matching the criteria.",
		}, nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d tickets:\n\n", len(incidents)))

	for i, inc := range incidents {
		result.WriteString(fmt.Sprintf("%d. %s\n", i+1, FormatIncident(&inc)))
		if i < len(incidents)-1 {
			result.WriteString("\n---\n\n")
		}
	}

	return &agent.ToolResult{
		Content: result.String(),
	}, nil
}

// GetTicketTool retrieves a specific ServiceNow incident.
type GetTicketTool struct {
	client *Client
}

// NewGetTicketTool creates a new get ticket tool.
func NewGetTicketTool(client *Client) *GetTicketTool {
	return &GetTicketTool{client: client}
}

func (t *GetTicketTool) Name() string {
	return "servicenow_get_ticket"
}

func (t *GetTicketTool) Description() string {
	return "Get details of a specific ServiceNow incident by ticket number (e.g., INC0012345)"
}

func (t *GetTicketTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ticket_number": {
				"type": "string",
				"description": "The ticket number (e.g., INC0012345) or sys_id"
			}
		},
		"required": ["ticket_number"]
	}`)
}

func (t *GetTicketTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		TicketNumber string `json:"ticket_number"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.TicketNumber == "" {
		return &agent.ToolResult{
			Content: "ticket_number is required",
			IsError: true,
		}, nil
	}

	incident, err := t.client.GetIncident(ctx, input.TicketNumber)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error getting ticket: %v", err),
			IsError: true,
		}, nil
	}

	// Build detailed response
	state := incident.State
	if name, ok := IncidentState[incident.State]; ok {
		state = name
	}

	priority := incident.Priority
	if name, ok := IncidentPriority[incident.Priority]; ok {
		priority = name
	}

	result := fmt.Sprintf(`Ticket: %s
Short Description: %s
State: %s
Priority: %s
Impact: %s
Urgency: %s

Assigned To: %s
Assignment Group: %s
Caller: %s

Category: %s / %s

Opened: %s
`,
		incident.Number,
		incident.ShortDescription,
		state,
		priority,
		incident.Impact,
		incident.Urgency,
		incident.AssignedTo,
		incident.AssignmentGroup,
		incident.CallerID,
		incident.Category,
		incident.Subcategory,
		incident.OpenedAt,
	)

	if incident.Description != "" {
		result += fmt.Sprintf("\nDescription:\n%s\n", incident.Description)
	}

	if incident.ResolvedAt != "" {
		result += fmt.Sprintf("\nResolved: %s\n", incident.ResolvedAt)
	}

	if incident.CloseNotes != "" {
		result += fmt.Sprintf("\nResolution Notes:\n%s\n", incident.CloseNotes)
	}

	return &agent.ToolResult{
		Content: result,
	}, nil
}

// AddCommentTool adds a comment to a ServiceNow incident.
type AddCommentTool struct {
	client *Client
}

// NewAddCommentTool creates a new add comment tool.
func NewAddCommentTool(client *Client) *AddCommentTool {
	return &AddCommentTool{client: client}
}

func (t *AddCommentTool) Name() string {
	return "servicenow_add_comment"
}

func (t *AddCommentTool) Description() string {
	return "Add a work note or customer-visible comment to a ServiceNow incident"
}

func (t *AddCommentTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ticket_number": {
				"type": "string",
				"description": "The ticket number (e.g., INC0012345)"
			},
			"comment": {
				"type": "string",
				"description": "The comment text to add"
			},
			"type": {
				"type": "string",
				"description": "Comment type: work_note (internal) or comment (customer visible)",
				"enum": ["work_note", "comment"],
				"default": "work_note"
			}
		},
		"required": ["ticket_number", "comment"]
	}`)
}

func (t *AddCommentTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		TicketNumber string `json:"ticket_number"`
		Comment      string `json:"comment"`
		Type         string `json:"type"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.TicketNumber == "" {
		return &agent.ToolResult{
			Content: "ticket_number is required",
			IsError: true,
		}, nil
	}

	if input.Comment == "" {
		return &agent.ToolResult{
			Content: "comment is required",
			IsError: true,
		}, nil
	}

	// First get the incident to get sys_id
	incident, err := t.client.GetIncident(ctx, input.TicketNumber)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error finding ticket: %v", err),
			IsError: true,
		}, nil
	}

	// Add the comment
	if input.Type == "comment" {
		err = t.client.AddComment(ctx, incident.SysID, input.Comment)
	} else {
		err = t.client.AddWorkNote(ctx, incident.SysID, input.Comment)
	}

	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error adding comment: %v", err),
			IsError: true,
		}, nil
	}

	commentType := "work note"
	if input.Type == "comment" {
		commentType = "customer comment"
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Added %s to %s:\n\n%s", commentType, incident.Number, input.Comment),
	}, nil
}

// ResolveTicketTool resolves a ServiceNow incident.
type ResolveTicketTool struct {
	client *Client
}

// NewResolveTicketTool creates a new resolve ticket tool.
func NewResolveTicketTool(client *Client) *ResolveTicketTool {
	return &ResolveTicketTool{client: client}
}

func (t *ResolveTicketTool) Name() string {
	return "servicenow_resolve_ticket"
}

func (t *ResolveTicketTool) Description() string {
	return "Resolve a ServiceNow incident with a resolution note"
}

func (t *ResolveTicketTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ticket_number": {
				"type": "string",
				"description": "The ticket number (e.g., INC0012345)"
			},
			"resolution": {
				"type": "string",
				"description": "Resolution notes explaining how the issue was resolved"
			},
			"close_code": {
				"type": "string",
				"description": "Close code (ServiceNow-defined values)",
				"enum": ["Solved (Permanently)", "Solved (Workaround)", "Not Solved (Not Reproducible)", "Not Solved (Too Costly)", "Closed/Resolved by Caller"]
			}
		},
		"required": ["ticket_number", "resolution"]
	}`)
}

func (t *ResolveTicketTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		TicketNumber string `json:"ticket_number"`
		Resolution   string `json:"resolution"`
		CloseCode    string `json:"close_code"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.TicketNumber == "" {
		return &agent.ToolResult{
			Content: "ticket_number is required",
			IsError: true,
		}, nil
	}

	if input.Resolution == "" {
		return &agent.ToolResult{
			Content: "resolution is required",
			IsError: true,
		}, nil
	}

	// First get the incident to get sys_id
	incident, err := t.client.GetIncident(ctx, input.TicketNumber)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error finding ticket: %v", err),
			IsError: true,
		}, nil
	}

	// Resolve the incident
	updated, err := t.client.ResolveIncident(ctx, incident.SysID, input.Resolution, input.CloseCode)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error resolving ticket: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Resolved %s\n\nResolution: %s\n\nNew state: Resolved",
			updated.Number,
			input.Resolution,
		),
	}, nil
}

// UpdateTicketTool updates fields on a ServiceNow incident.
type UpdateTicketTool struct {
	client *Client
}

// NewUpdateTicketTool creates a new update ticket tool.
func NewUpdateTicketTool(client *Client) *UpdateTicketTool {
	return &UpdateTicketTool{client: client}
}

func (t *UpdateTicketTool) Name() string {
	return "servicenow_update_ticket"
}

func (t *UpdateTicketTool) Description() string {
	return "Update fields on a ServiceNow incident (state, priority, assignment, etc.)"
}

func (t *UpdateTicketTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ticket_number": {
				"type": "string",
				"description": "The ticket number (e.g., INC0012345)"
			},
			"state": {
				"type": "string",
				"description": "New state",
				"enum": ["new", "in_progress", "on_hold", "resolved", "closed"]
			},
			"priority": {
				"type": "string",
				"description": "New priority",
				"enum": ["critical", "high", "moderate", "low"]
			},
			"assigned_to": {
				"type": "string",
				"description": "User to assign the ticket to"
			},
			"assignment_group": {
				"type": "string",
				"description": "Group to assign the ticket to"
			}
		},
		"required": ["ticket_number"]
	}`)
}

func (t *UpdateTicketTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		TicketNumber    string `json:"ticket_number"`
		State           string `json:"state"`
		Priority        string `json:"priority"`
		AssignedTo      string `json:"assigned_to"`
		AssignmentGroup string `json:"assignment_group"`
	}

	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if input.TicketNumber == "" {
		return &agent.ToolResult{
			Content: "ticket_number is required",
			IsError: true,
		}, nil
	}

	// First get the incident to get sys_id
	incident, err := t.client.GetIncident(ctx, input.TicketNumber)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error finding ticket: %v", err),
			IsError: true,
		}, nil
	}

	// Build updates
	updates := make(map[string]string)

	switch strings.ToLower(input.State) {
	case "new":
		updates["state"] = "1"
	case "in_progress":
		updates["state"] = "2"
	case "on_hold":
		updates["state"] = "3"
	case "resolved":
		updates["state"] = "6"
	case "closed":
		updates["state"] = "7"
	}

	switch strings.ToLower(input.Priority) {
	case "critical":
		updates["priority"] = "1"
	case "high":
		updates["priority"] = "2"
	case "moderate":
		updates["priority"] = "3"
	case "low":
		updates["priority"] = "4"
	}

	if input.AssignedTo != "" {
		updates["assigned_to"] = input.AssignedTo
	}

	if input.AssignmentGroup != "" {
		updates["assignment_group"] = input.AssignmentGroup
	}

	if len(updates) == 0 {
		return &agent.ToolResult{
			Content: "No updates specified",
			IsError: true,
		}, nil
	}

	// Update the incident
	updated, err := t.client.UpdateIncident(ctx, incident.SysID, updates)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Error updating ticket: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Updated %s:\n\n%s", updated.Number, FormatIncident(updated)),
	}, nil
}
