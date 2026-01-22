package multiagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ContextManager handles context sharing between agents during handoffs.
// It supports multiple sharing modes: full, summary, filtered, and none.
type ContextManager struct {
	orchestrator *Orchestrator

	// summarizer generates conversation summaries.
	summarizer ContextSummarizer

	// defaultMode is the default context sharing mode.
	defaultMode ContextSharingMode

	// maxMessages is the maximum number of messages to include in full context.
	maxMessages int

	// maxSummaryLength is the maximum length for generated summaries.
	maxSummaryLength int
}

// ContextSummarizer generates summaries of conversation context.
type ContextSummarizer interface {
	// Summarize generates a summary of the given messages.
	Summarize(ctx context.Context, messages []*models.Message, maxLength int) (string, error)
}

// NewContextManager creates a new context manager.
func NewContextManager(orchestrator *Orchestrator) *ContextManager {
	defaultMode := ContextFull
	if orchestrator.config != nil && orchestrator.config.DefaultContextMode != "" {
		defaultMode = orchestrator.config.DefaultContextMode
	}

	return &ContextManager{
		orchestrator:     orchestrator,
		defaultMode:      defaultMode,
		maxMessages:      50,
		maxSummaryLength: 1000,
	}
}

// SetSummarizer sets the context summarizer.
func (cm *ContextManager) SetSummarizer(summarizer ContextSummarizer) {
	cm.summarizer = summarizer
}

// SetMaxMessages sets the maximum messages for full context.
func (cm *ContextManager) SetMaxMessages(max int) {
	cm.maxMessages = max
}

// SetMaxSummaryLength sets the maximum summary length.
func (cm *ContextManager) SetMaxSummaryLength(max int) {
	cm.maxSummaryLength = max
}

// BuildSharedContext creates shared context for a handoff based on the context mode.
func (cm *ContextManager) BuildSharedContext(ctx context.Context, session *models.Session, request *HandoffRequest) (*SharedContext, error) {
	// Determine context mode
	mode := cm.defaultMode

	// Check if the source agent's handoff rule specifies a mode
	if agent, ok := cm.orchestrator.GetAgent(request.FromAgentID); ok {
		if rule := agent.GetHandoffTarget(TriggerExplicit, request.ToAgentID); rule != nil && rule.ContextMode != "" {
			mode = rule.ContextMode
		}
	}

	// Get session history
	history, err := cm.orchestrator.Sessions().GetHistory(ctx, session.ID, cm.maxMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to get session history: %w", err)
	}

	shared := &SharedContext{
		Task:           request.Reason,
		PreviousAgents: []string{request.FromAgentID},
		Variables:      make(map[string]any),
		Metadata:       make(map[string]any),
	}

	// Add any existing context from the request
	if request.Context != nil {
		if request.Context.Summary != "" {
			shared.Summary = request.Context.Summary
		}
		if request.Context.Task != "" {
			shared.Task = request.Context.Task
		}
		for k, v := range request.Context.Variables {
			shared.Variables[k] = v
		}
		for k, v := range request.Context.Metadata {
			shared.Metadata[k] = v
		}
		shared.PreviousAgents = append(shared.PreviousAgents, request.Context.PreviousAgents...)
	}

	switch mode {
	case ContextFull:
		shared.Messages = cm.convertToSharedMessages(history)

	case ContextSummary:
		summary, err := cm.generateSummary(ctx, history)
		if err != nil {
			// Fall back to basic summary on error
			summary = cm.buildBasicSummary(history)
		}
		shared.Summary = summary

	case ContextFiltered:
		shared.Messages = cm.filterMessages(history, request)

	case ContextLastN:
		n := cm.getLastNCount(request)
		if n > len(history) {
			n = len(history)
		}
		shared.Messages = cm.convertToSharedMessages(history[len(history)-n:])

	case ContextNone:
		// Only include the task/reason

	default:
		// Default to full context
		shared.Messages = cm.convertToSharedMessages(history)
	}

	// Extract useful variables from the conversation
	cm.extractVariables(history, shared)

	return shared, nil
}

// convertToSharedMessages converts models.Message to SharedMessage.
func (cm *ContextManager) convertToSharedMessages(messages []*models.Message) []SharedMessage {
	shared := make([]SharedMessage, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		sm := SharedMessage{
			Role:      string(msg.Role),
			Content:   msg.Content,
			Timestamp: msg.CreatedAt,
		}
		// Try to get agent ID from metadata
		if msg.Metadata != nil {
			if agentID, ok := msg.Metadata["agent_id"].(string); ok {
				sm.AgentID = agentID
			}
		}
		shared = append(shared, sm)
	}
	return shared
}

// generateSummary uses the summarizer to create a context summary.
func (cm *ContextManager) generateSummary(ctx context.Context, messages []*models.Message) (string, error) {
	if cm.summarizer == nil {
		return cm.buildBasicSummary(messages), nil
	}
	return cm.summarizer.Summarize(ctx, messages, cm.maxSummaryLength)
}

// buildBasicSummary creates a simple summary without LLM assistance.
func (cm *ContextManager) buildBasicSummary(messages []*models.Message) string {
	if len(messages) == 0 {
		return "No conversation history."
	}

	var sb strings.Builder
	sb.WriteString("Conversation summary:\n")

	// Count messages by role
	userCount := 0
	assistantCount := 0
	toolCount := 0

	for _, msg := range messages {
		switch msg.Role {
		case models.RoleUser:
			userCount++
		case models.RoleAssistant:
			assistantCount++
		case models.RoleTool:
			toolCount++
		}
	}

	sb.WriteString(fmt.Sprintf("- %d user messages, %d assistant messages, %d tool interactions\n",
		userCount, assistantCount, toolCount))

	// Include the first user message (original request)
	for _, msg := range messages {
		if msg.Role == models.RoleUser && msg.Content != "" {
			sb.WriteString(fmt.Sprintf("- Original request: %s\n", truncateString(msg.Content, 200)))
			break
		}
	}

	// Include the last user message if different from first
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == models.RoleUser && messages[i].Content != "" {
			if i > 0 { // Not the same as first
				sb.WriteString(fmt.Sprintf("- Most recent request: %s\n", truncateString(messages[i].Content, 200)))
			}
			break
		}
	}

	// Note any tools that were used
	toolsUsed := make(map[string]bool)
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			toolsUsed[tc.Name] = true
		}
	}
	if len(toolsUsed) > 0 {
		tools := make([]string, 0, len(toolsUsed))
		for t := range toolsUsed {
			tools = append(tools, t)
		}
		sb.WriteString(fmt.Sprintf("- Tools used: %s\n", strings.Join(tools, ", ")))
	}

	return sb.String()
}

// filterMessages filters messages based on handoff rule criteria.
func (cm *ContextManager) filterMessages(messages []*models.Message, request *HandoffRequest) []SharedMessage {
	var filtered []SharedMessage

	// Get filter criteria from request context or use defaults
	includeRoles := map[string]bool{
		string(models.RoleUser):      true,
		string(models.RoleAssistant): true,
	}

	// Check for filter settings in request metadata
	if request.Context != nil && request.Context.Metadata != nil {
		if roles, ok := request.Context.Metadata["include_roles"].([]string); ok {
			includeRoles = make(map[string]bool)
			for _, r := range roles {
				includeRoles[r] = true
			}
		}
	}

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		// Check role filter
		if !includeRoles[string(msg.Role)] {
			continue
		}

		// Skip empty content messages (unless they have tool calls)
		if msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue
		}

		filtered = append(filtered, SharedMessage{
			Role:      string(msg.Role),
			Content:   msg.Content,
			Timestamp: msg.CreatedAt,
		})
	}

	return filtered
}

// getLastNCount gets the N value for ContextLastN mode.
func (cm *ContextManager) getLastNCount(request *HandoffRequest) int {
	// Default to 10 messages
	n := 10

	// Check for custom value in request metadata
	if request.Context != nil && request.Context.Metadata != nil {
		if count, ok := request.Context.Metadata["last_n"].(int); ok && count > 0 {
			n = count
		}
	}

	return n
}

// extractVariables extracts useful variables from the conversation.
func (cm *ContextManager) extractVariables(messages []*models.Message, shared *SharedContext) {
	// Track mentioned entities
	for _, msg := range messages {
		if msg == nil || msg.Metadata == nil {
			continue
		}

		// Copy any variables from message metadata
		if vars, ok := msg.Metadata["variables"].(map[string]any); ok {
			for k, v := range vars {
				shared.Variables[k] = v
			}
		}

		// Track extracted entities
		if entities, ok := msg.Metadata["entities"].(map[string]any); ok {
			for k, v := range entities {
				shared.Variables["entity_"+k] = v
			}
		}
	}

	// Add conversation metadata
	if len(messages) > 0 {
		shared.Variables["conversation_start"] = messages[0].CreatedAt
		shared.Variables["message_count"] = len(messages)
	}
}

// MergeContexts merges multiple shared contexts into one.
func MergeContexts(contexts ...*SharedContext) *SharedContext {
	merged := &SharedContext{
		Variables: make(map[string]any),
		Metadata:  make(map[string]any),
	}

	for _, ctx := range contexts {
		if ctx == nil {
			continue
		}

		// Combine summaries
		if ctx.Summary != "" {
			if merged.Summary != "" {
				merged.Summary += "\n---\n"
			}
			merged.Summary += ctx.Summary
		}

		// Append messages (avoiding duplicates by timestamp)
		seen := make(map[time.Time]bool)
		for _, m := range merged.Messages {
			seen[m.Timestamp] = true
		}
		for _, m := range ctx.Messages {
			if !seen[m.Timestamp] {
				merged.Messages = append(merged.Messages, m)
				seen[m.Timestamp] = true
			}
		}

		// Merge variables (later values override)
		for k, v := range ctx.Variables {
			merged.Variables[k] = v
		}

		// Use latest task
		if ctx.Task != "" {
			merged.Task = ctx.Task
		}

		// Combine previous agents (unique)
		agentSet := make(map[string]bool)
		for _, a := range merged.PreviousAgents {
			agentSet[a] = true
		}
		for _, a := range ctx.PreviousAgents {
			if !agentSet[a] {
				merged.PreviousAgents = append(merged.PreviousAgents, a)
				agentSet[a] = true
			}
		}

		// Merge metadata
		for k, v := range ctx.Metadata {
			merged.Metadata[k] = v
		}
	}

	return merged
}

// FormatContextForPrompt formats shared context for inclusion in a system prompt.
func FormatContextForPrompt(ctx *SharedContext) string {
	if ctx == nil {
		return ""
	}

	var sb strings.Builder

	if ctx.Task != "" {
		sb.WriteString("## Current Task\n")
		sb.WriteString(ctx.Task)
		sb.WriteString("\n\n")
	}

	if len(ctx.PreviousAgents) > 0 {
		sb.WriteString("## Previous Agents\n")
		sb.WriteString("This conversation has been handled by: ")
		sb.WriteString(strings.Join(ctx.PreviousAgents, " -> "))
		sb.WriteString("\n\n")
	}

	if ctx.Summary != "" {
		sb.WriteString("## Conversation Summary\n")
		sb.WriteString(ctx.Summary)
		sb.WriteString("\n\n")
	}

	if len(ctx.Variables) > 0 {
		sb.WriteString("## Context Variables\n")
		for k, v := range ctx.Variables {
			sb.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
		}
		sb.WriteString("\n")
	}

	if len(ctx.Messages) > 0 {
		sb.WriteString("## Conversation History\n")
		for _, msg := range ctx.Messages {
			roleLabel := msg.Role
			if msg.AgentID != "" {
				roleLabel = fmt.Sprintf("%s (%s)", msg.Role, msg.AgentID)
			}
			sb.WriteString(fmt.Sprintf("[%s] %s\n", roleLabel, truncateString(msg.Content, 500)))
		}
	}

	return sb.String()
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ContextFilter defines criteria for filtering context.
type ContextFilter struct {
	// IncludeRoles specifies which roles to include.
	IncludeRoles []string

	// ExcludeRoles specifies which roles to exclude.
	ExcludeRoles []string

	// IncludeAgents specifies which agent messages to include.
	IncludeAgents []string

	// ExcludeAgents specifies which agent messages to exclude.
	ExcludeAgents []string

	// MinTimestamp filters messages after this time.
	MinTimestamp *time.Time

	// MaxTimestamp filters messages before this time.
	MaxTimestamp *time.Time

	// ContainsKeywords filters messages containing any of these keywords.
	ContainsKeywords []string

	// MaxMessages limits the number of messages.
	MaxMessages int
}

// ApplyFilter applies a filter to shared messages.
func ApplyFilter(messages []SharedMessage, filter *ContextFilter) []SharedMessage {
	if filter == nil {
		return messages
	}

	includeRoles := make(map[string]bool)
	for _, r := range filter.IncludeRoles {
		includeRoles[r] = true
	}

	excludeRoles := make(map[string]bool)
	for _, r := range filter.ExcludeRoles {
		excludeRoles[r] = true
	}

	includeAgents := make(map[string]bool)
	for _, a := range filter.IncludeAgents {
		includeAgents[a] = true
	}

	excludeAgents := make(map[string]bool)
	for _, a := range filter.ExcludeAgents {
		excludeAgents[a] = true
	}

	var filtered []SharedMessage
	for _, msg := range messages {
		// Check role filters
		if len(includeRoles) > 0 && !includeRoles[msg.Role] {
			continue
		}
		if excludeRoles[msg.Role] {
			continue
		}

		// Check agent filters
		if msg.AgentID != "" {
			if len(includeAgents) > 0 && !includeAgents[msg.AgentID] {
				continue
			}
			if excludeAgents[msg.AgentID] {
				continue
			}
		}

		// Check timestamp filters
		if filter.MinTimestamp != nil && msg.Timestamp.Before(*filter.MinTimestamp) {
			continue
		}
		if filter.MaxTimestamp != nil && msg.Timestamp.After(*filter.MaxTimestamp) {
			continue
		}

		// Check keyword filter
		if len(filter.ContainsKeywords) > 0 {
			found := false
			contentLower := strings.ToLower(msg.Content)
			for _, kw := range filter.ContainsKeywords {
				if strings.Contains(contentLower, strings.ToLower(kw)) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		filtered = append(filtered, msg)

		// Check max messages
		if filter.MaxMessages > 0 && len(filtered) >= filter.MaxMessages {
			break
		}
	}

	return filtered
}
