// Package context provides context management for agent conversations.
//
// This package handles:
//   - Context packing: selecting which messages to include in LLM requests
//   - Rolling summaries: compressing old history into summaries
//   - Budget management: staying within token/char limits
package context

import (
	"github.com/haasonsaas/nexus/pkg/models"
)

// PackOptions configures how messages are packed into context.
type PackOptions struct {
	// MaxMessages is the hard cap on number of messages to include (e.g. 60).
	MaxMessages int

	// MaxChars is the approximate character budget (cheap proxy for tokens).
	// Default: 30000 (~7500 tokens at 4 chars/token).
	MaxChars int

	// MaxToolResultChars is the max chars per tool result content.
	// Longer results are truncated. Default: 6000.
	MaxToolResultChars int

	// IncludeSummary controls whether to include the rolling summary.
	IncludeSummary bool

	// SummaryMetadataKey is the metadata key marking summary messages.
	// Default: "nexus_summary".
	SummaryMetadataKey string
}

// DefaultPackOptions returns sensible defaults for context packing.
func DefaultPackOptions() PackOptions {
	return PackOptions{
		MaxMessages:        60,
		MaxChars:           30000,
		MaxToolResultChars: 6000,
		IncludeSummary:     true,
		SummaryMetadataKey: SummaryMetadataKey,
	}
}

// Packer selects and prepares messages for LLM context.
type Packer struct {
	opts PackOptions
}

// NewPacker creates a new context packer with the given options.
func NewPacker(opts PackOptions) *Packer {
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = 60
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = 30000
	}
	if opts.MaxToolResultChars <= 0 {
		opts.MaxToolResultChars = 6000
	}
	if opts.SummaryMetadataKey == "" {
		opts.SummaryMetadataKey = SummaryMetadataKey
	}
	return &Packer{opts: opts}
}

// Pack selects messages from history to fit within budget.
//
// The packed result includes (in order):
//  1. Summary message (if IncludeSummary and summary exists)
//  2. Recent messages from history (newest first, up to budget)
//  3. The incoming user message
//
// Tool result content is truncated to MaxToolResultChars.
// Messages are selected from the end (most recent) backwards until
// either MaxMessages or MaxChars is reached.
func (p *Packer) Pack(history []*models.Message, incoming *models.Message, summary *models.Message) ([]*models.Message, error) {
	var result []*models.Message

	// Track budget
	totalChars := 0
	totalMsgs := 0

	// Reserve space for incoming message (only if present)
	if incoming != nil {
		incomingChars := p.messageChars(incoming)
		totalChars += incomingChars
		totalMsgs++
	}

	// Reserve space for summary if present and enabled
	if p.opts.IncludeSummary && summary != nil {
		summaryChars := p.messageChars(summary)
		totalChars += summaryChars
		totalMsgs++
	}

	// Filter out summary messages from history (they're handled separately)
	filtered := make([]*models.Message, 0, len(history))
	for _, m := range history {
		if m == nil {
			continue
		}
		if p.isSummaryMessage(m) {
			continue
		}
		filtered = append(filtered, m)
	}

	// Select messages from the end (most recent) backwards
	// Build in reverse order, then reverse once (O(n) instead of O(n²))
	selectedReverse := make([]*models.Message, 0)
	for i := len(filtered) - 1; i >= 0; i-- {
		m := filtered[i]
		msgChars := p.messageChars(m)

		// Check if we'd exceed budget
		if totalMsgs+1 > p.opts.MaxMessages {
			break
		}
		if totalChars+msgChars > p.opts.MaxChars {
			break
		}

		selectedReverse = append(selectedReverse, m)
		totalMsgs++
		totalChars += msgChars
	}

	// Reverse selectedReverse to get chronological order
	selected := make([]*models.Message, len(selectedReverse))
	for i, m := range selectedReverse {
		selected[len(selectedReverse)-1-i] = m
	}

	// Build final result in order
	// 1. Summary (if present and enabled)
	if p.opts.IncludeSummary && summary != nil {
		result = append(result, summary)
	}

	// 2. Selected history messages (now in chronological order)
	for _, m := range selected {
		// Truncate tool results if needed
		packed := p.truncateToolResults(m)
		result = append(result, packed)
	}

	// 3. Incoming message
	if incoming != nil {
		result = append(result, incoming)
	}

	return result, nil
}

// messageChars estimates the character count for a message.
func (p *Packer) messageChars(m *models.Message) int {
	if m == nil {
		return 0
	}
	chars := len(m.Content)
	for _, tc := range m.ToolCalls {
		chars += len(tc.Name) + len(tc.Input)
	}
	for _, tr := range m.ToolResults {
		chars += len(tr.Content)
	}
	return chars
}

// isSummaryMessage checks if a message is a summary marker.
func (p *Packer) isSummaryMessage(m *models.Message) bool {
	if m.Metadata == nil {
		return false
	}
	val, ok := m.Metadata[p.opts.SummaryMetadataKey]
	if !ok {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// truncateToolResults returns a copy with truncated tool result content.
func (p *Packer) truncateToolResults(m *models.Message) *models.Message {
	if len(m.ToolResults) == 0 {
		return m
	}

	// Check if any truncation needed
	needsTruncation := false
	for _, tr := range m.ToolResults {
		if len(tr.Content) > p.opts.MaxToolResultChars {
			needsTruncation = true
			break
		}
	}
	if !needsTruncation {
		return m
	}

	// Create copy with truncated results
	copy := *m
	copy.ToolResults = make([]models.ToolResult, len(m.ToolResults))
	for i, tr := range m.ToolResults {
		if len(tr.Content) > p.opts.MaxToolResultChars {
			truncated := tr
			truncated.Content = tr.Content[:p.opts.MaxToolResultChars] + "\n...[truncated]"
			copy.ToolResults[i] = truncated
		} else {
			copy.ToolResults[i] = tr
		}
	}
	return &copy
}
