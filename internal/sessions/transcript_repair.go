package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

// TranscriptRepairReport contains the results of transcript repair.
type TranscriptRepairReport struct {
	// Messages is the repaired message list
	Messages []*models.Message
	// Added contains synthetic tool results that were inserted
	Added []*models.Message
	// DroppedDuplicateCount is the number of duplicate tool results dropped
	DroppedDuplicateCount int
	// DroppedOrphanCount is the number of orphan tool results dropped
	DroppedOrphanCount int
	// Moved indicates if any tool results were moved/reordered
	Moved bool
}

// RepairToolCallPairing ensures all assistant tool calls have matching tool results.
// This is critical for Anthropic-compatible APIs which reject transcripts where
// assistant tool calls are not immediately followed by matching tool results.
//
// The function:
// - Moves matching toolResult messages directly after their assistant toolCall turn
// - Inserts synthetic error toolResults for missing IDs
// - Drops duplicate toolResults for the same ID
// - Drops orphan toolResults that don't match any tool call
func RepairToolCallPairing(messages []*models.Message) TranscriptRepairReport {
	report := TranscriptRepairReport{
		Messages: make([]*models.Message, 0, len(messages)),
	}

	seenToolResultIDs := make(map[string]bool)
	changed := false

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg == nil {
			continue
		}

		// Non-assistant messages: pass through user/system, handle tool results specially
		if msg.Role != models.RoleAssistant {
			if msg.Role == models.RoleTool && len(msg.ToolResults) > 0 {
				// Tool results should only appear directly after matching assistant tool call
				// Drop orphan tool results
				report.DroppedOrphanCount += len(msg.ToolResults)
				changed = true
				continue
			}
			report.Messages = append(report.Messages, msg)
			continue
		}

		// Assistant message: check for tool calls
		if len(msg.ToolCalls) == 0 {
			report.Messages = append(report.Messages, msg)
			continue
		}

		// Build map of tool call IDs for this assistant turn
		toolCallIDs := make(map[string]*models.ToolCall)
		pending := make(map[string]struct{}, len(msg.ToolCalls))
		pendingOrder := make([]string, 0, len(msg.ToolCalls))
		for idx := range msg.ToolCalls {
			tc := &msg.ToolCalls[idx]
			toolCallIDs[tc.ID] = tc
			if tc.ID != "" {
				pending[tc.ID] = struct{}{}
				pendingOrder = append(pendingOrder, tc.ID)
			}
		}

		// Collect tool results from following messages until next assistant turn
		toolResults := make(map[string]*models.Message)
		remainder := make([]*models.Message, 0)

		removePending := func(id string) {
			delete(pending, id)
			for idx, pendingID := range pendingOrder {
				if pendingID == id {
					copy(pendingOrder[idx:], pendingOrder[idx+1:])
					pendingOrder = pendingOrder[:len(pendingOrder)-1]
					return
				}
			}
		}
		assignPending := func() string {
			for len(pendingOrder) > 0 {
				id := pendingOrder[0]
				pendingOrder = pendingOrder[1:]
				if _, ok := pending[id]; ok {
					delete(pending, id)
					return id
				}
			}
			return ""
		}

		j := i + 1
		for ; j < len(messages); j++ {
			next := messages[j]
			if next == nil {
				continue
			}

			// Stop at next assistant message
			if next.Role == models.RoleAssistant {
				break
			}

			// Collect tool results that match our tool calls
			if next.Role == models.RoleTool && len(next.ToolResults) > 0 {
				needsClone := false
				kept := make([]models.ToolResult, 0, len(next.ToolResults))
				for _, tr := range next.ToolResults {
					toolCallID := tr.ToolCallID
					if toolCallID == "" {
						toolCallID = assignPending()
						if toolCallID != "" {
							needsClone = true
							tr.ToolCallID = toolCallID
						}
					}

					// If we still can't determine the tool call, drop it as orphan.
					if toolCallID == "" {
						report.DroppedOrphanCount++
						needsClone = true
						changed = true
						continue
					}

					// Check if this result matches a tool call we're looking for
					if _, ok := toolCallIDs[toolCallID]; ok {
						// Check for duplicate
						if seenToolResultIDs[toolCallID] {
							report.DroppedDuplicateCount++
							changed = true
							needsClone = true
							continue
						}
						removePending(toolCallID)
						seenToolResultIDs[toolCallID] = true
						kept = append(kept, tr)
					} else {
						// Orphan tool result - drop it
						report.DroppedOrphanCount++
						changed = true
						needsClone = true
					}
				}
				if len(kept) == 0 {
					continue
				}

				processed := next
				if needsClone {
					copied := *next
					copied.ToolResults = kept
					processed = &copied
					changed = true
				}
				for _, tr := range kept {
					if tr.ToolCallID != "" {
						toolResults[tr.ToolCallID] = processed
					}
				}
				continue
			}

			// Other messages go to remainder
			remainder = append(remainder, next)
		}

		// Emit the assistant message
		report.Messages = append(report.Messages, msg)

		// Check if we moved any results
		if len(toolResults) > 0 && len(remainder) > 0 {
			report.Moved = true
			changed = true
		}

		// Emit tool results in order of tool calls, inserting synthetic ones if missing
		emitted := make(map[*models.Message]bool)
		for _, tc := range msg.ToolCalls {
			if resultMsg, ok := toolResults[tc.ID]; ok {
				if resultMsg != nil && !emitted[resultMsg] {
					report.Messages = append(report.Messages, resultMsg)
					emitted[resultMsg] = true
				}
			} else if !seenToolResultIDs[tc.ID] {
				// Insert synthetic error result
				synthetic := makeMissingToolResult(tc.ID, tc.Name)
				synthetic.SessionID = msg.SessionID
				synthetic.Channel = msg.Channel
				synthetic.ChannelID = msg.ChannelID
				if !msg.CreatedAt.IsZero() {
					synthetic.CreatedAt = msg.CreatedAt.Add(time.Nanosecond)
				}
				report.Added = append(report.Added, synthetic)
				report.Messages = append(report.Messages, synthetic)
				seenToolResultIDs[tc.ID] = true
				changed = true
			}
		}

		// Emit remaining non-tool-result messages
		report.Messages = append(report.Messages, remainder...)

		// Skip to where we left off
		i = j - 1
	}

	if !changed {
		report.Messages = messages
	}

	return report
}

// makeMissingToolResult creates a synthetic tool result for a missing tool call.
func makeMissingToolResult(toolCallID, toolName string) *models.Message {
	if toolName == "" {
		toolName = "unknown"
	}

	return &models.Message{
		ID:        uuid.NewString(),
		Role:      models.RoleTool,
		Direction: models.DirectionInbound,
		ToolResults: []models.ToolResult{
			{
				ToolCallID: toolCallID,
				Content:    "[nexus] Missing tool result in session history; inserted synthetic error result for transcript repair.",
				IsError:    true,
			},
		},
		Metadata: map[string]any{
			"synthetic": true,
			"tool_name": toolName,
		},
		CreatedAt: time.Now(),
	}
}

// SanitizeTranscript repairs tool call/result pairing and returns only the messages.
func SanitizeTranscript(messages []*models.Message) []*models.Message {
	return RepairToolCallPairing(messages).Messages
}

// ExtractToolCallIDs extracts tool call IDs from an assistant message.
func ExtractToolCallIDs(msg *models.Message) []string {
	if msg == nil || msg.Role != models.RoleAssistant || len(msg.ToolCalls) == 0 {
		return nil
	}

	ids := make([]string, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		ids[i] = tc.ID
	}
	return ids
}

// ExtractToolResultID extracts the tool call ID from a tool result message.
func ExtractToolResultID(msg *models.Message) string {
	if msg == nil || msg.Role != models.RoleTool || len(msg.ToolResults) == 0 {
		return ""
	}
	return msg.ToolResults[0].ToolCallID
}

// ValidateToolCallPairing checks if all tool calls have matching results.
// Returns a list of missing tool call IDs.
func ValidateToolCallPairing(messages []*models.Message) []string {
	pendingToolCalls := make(map[string]bool)
	var missing []string

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		switch msg.Role {
		case models.RoleAssistant:
			// Clear pending and add new tool calls
			for id := range pendingToolCalls {
				missing = append(missing, id)
			}
			pendingToolCalls = make(map[string]bool)
			for _, tc := range msg.ToolCalls {
				pendingToolCalls[tc.ID] = true
			}

		case models.RoleTool:
			// Mark tool results as received
			for _, tr := range msg.ToolResults {
				delete(pendingToolCalls, tr.ToolCallID)
			}
		}
	}

	// Check for any remaining pending tool calls
	for id := range pendingToolCalls {
		missing = append(missing, id)
	}

	return missing
}

// ToolCallGuard provides runtime protection for tool call/result pairing.
// It tracks pending tool calls and can generate synthetic results for missing ones.
type ToolCallGuard struct {
	pending map[string]string // toolCallID -> toolName
}

// NewToolCallGuard creates a new tool call guard.
func NewToolCallGuard() *ToolCallGuard {
	return &ToolCallGuard{
		pending: make(map[string]string),
	}
}

// TrackToolCalls records tool calls that need results.
func (g *ToolCallGuard) TrackToolCalls(msg *models.Message) {
	if msg == nil || msg.Role != models.RoleAssistant {
		return
	}

	for _, tc := range msg.ToolCalls {
		g.pending[tc.ID] = tc.Name
	}
}

// RecordToolResult marks a tool result as received.
func (g *ToolCallGuard) RecordToolResult(toolCallID string) {
	delete(g.pending, toolCallID)
}

// GetPendingIDs returns IDs of tool calls that are still pending results.
func (g *ToolCallGuard) GetPendingIDs() []string {
	ids := make([]string, 0, len(g.pending))
	for id := range g.pending {
		ids = append(ids, id)
	}
	return ids
}

// HasPending returns true if there are pending tool calls without results.
func (g *ToolCallGuard) HasPending() bool {
	return len(g.pending) > 0
}

// FlushPending generates synthetic results for all pending tool calls.
func (g *ToolCallGuard) FlushPending() []*models.Message {
	if len(g.pending) == 0 {
		return nil
	}

	results := make([]*models.Message, 0, len(g.pending))
	for id, name := range g.pending {
		results = append(results, makeMissingToolResult(id, name))
	}

	g.pending = make(map[string]string)
	return results
}

// ToolResultTransformer applies an optional transformation to tool results before persistence.
type ToolResultTransformer func(msg *models.Message, meta ToolResultMeta) *models.Message

// ToolResultMeta contains metadata about a tool result.
type ToolResultMeta struct {
	ToolCallID  string
	ToolName    string
	IsSynthetic bool
}

// GuardedSessionStore wraps a session store with tool result guard functionality.
type GuardedSessionStore struct {
	Store
	guard       *ToolCallGuard
	transformer ToolResultTransformer
}

// NewGuardedSessionStore creates a new guarded session store.
func NewGuardedSessionStore(store Store, transformer ToolResultTransformer) *GuardedSessionStore {
	return &GuardedSessionStore{
		Store:       store,
		guard:       NewToolCallGuard(),
		transformer: transformer,
	}
}

// AppendMessage appends a message with tool call guard protection.
func (s *GuardedSessionStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	if msg == nil {
		return nil
	}

	// Handle tool results
	if msg.Role == models.RoleTool && len(msg.ToolResults) > 0 {
		for _, tr := range msg.ToolResults {
			s.guard.RecordToolResult(tr.ToolCallID)
		}

		// Apply transformer if set
		if s.transformer != nil {
			meta := ToolResultMeta{
				ToolCallID:  msg.ToolResults[0].ToolCallID,
				IsSynthetic: false,
			}
			msg = s.transformer(msg, meta)
		}

		return s.Store.AppendMessage(ctx, sessionID, msg)
	}

	// Flush pending tool results before non-tool messages
	if s.guard.HasPending() && (msg.Role != models.RoleAssistant || len(msg.ToolCalls) == 0) {
		synthetics := s.guard.FlushPending()
		for _, synthetic := range synthetics {
			if s.transformer != nil {
				meta := ToolResultMeta{
					IsSynthetic: true,
				}
				if len(synthetic.ToolResults) > 0 {
					meta.ToolCallID = synthetic.ToolResults[0].ToolCallID
				}
				if name, ok := synthetic.Metadata["tool_name"].(string); ok {
					meta.ToolName = name
				}
				synthetic = s.transformer(synthetic, meta)
			}
			if err := s.Store.AppendMessage(ctx, sessionID, synthetic); err != nil {
				return err
			}
		}
	}

	// Append the message
	if err := s.Store.AppendMessage(ctx, sessionID, msg); err != nil {
		return err
	}

	// Track new tool calls
	s.guard.TrackToolCalls(msg)

	return nil
}

// FlushPendingToolResults generates and appends synthetic results for pending tool calls.
func (s *GuardedSessionStore) FlushPendingToolResults(ctx context.Context, sessionID string) error {
	synthetics := s.guard.FlushPending()
	for _, synthetic := range synthetics {
		if s.transformer != nil {
			meta := ToolResultMeta{
				IsSynthetic: true,
			}
			if len(synthetic.ToolResults) > 0 {
				meta.ToolCallID = synthetic.ToolResults[0].ToolCallID
			}
			synthetic = s.transformer(synthetic, meta)
		}
		if err := s.Store.AppendMessage(ctx, sessionID, synthetic); err != nil {
			return err
		}
	}
	return nil
}

// MarshalToolInput safely marshals tool input for comparison/logging.
func MarshalToolInput(input json.RawMessage) string {
	if input == nil {
		return "{}"
	}
	return string(input)
}

// RepairTranscript fixes malformed session transcripts to ensure tool call/result pairing.
// This is an alias for RepairToolCallPairing that matches the clawdbot pattern.
//
// The function:
// - Moves tool results directly after their corresponding assistant tool calls
// - Inserts synthetic error results for tool calls with missing results
// - Drops duplicate tool results for the same tool call ID
// - Drops orphan tool results that don't match any tool call
func RepairTranscript(messages []*models.Message) TranscriptRepairReport {
	return RepairToolCallPairing(messages)
}

// AddedSyntheticResults returns the count of synthetic results added during repair.
func (r TranscriptRepairReport) AddedSyntheticResults() int {
	return len(r.Added)
}

// DroppedDuplicates returns the count of duplicate tool results dropped.
func (r TranscriptRepairReport) DroppedDuplicates() int {
	return r.DroppedDuplicateCount
}

// DroppedOrphans returns the count of orphan tool results dropped.
func (r TranscriptRepairReport) DroppedOrphans() int {
	return r.DroppedOrphanCount
}

// SanitizeToolUseResultPairing is a convenience wrapper that returns just the repaired messages.
// This matches the clawdbot pattern for transcript sanitization.
func SanitizeToolUseResultPairing(messages []*models.Message) []*models.Message {
	return RepairTranscript(messages).Messages
}
