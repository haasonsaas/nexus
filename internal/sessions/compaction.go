package sessions

import (
	"context"
	"fmt"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// CompactionStrategy defines how sessions should be compacted.
type CompactionStrategy string

const (
	// StrategyLastN keeps only the last N messages.
	StrategyLastN CompactionStrategy = "last_n"

	// StrategySummarize summarizes older messages using an LLM.
	StrategySummarize CompactionStrategy = "summarize"

	// StrategyImportantOnly keeps only messages marked as important.
	StrategyImportantOnly CompactionStrategy = "important_only"

	// StrategyHybrid combines summarization with keeping recent messages.
	StrategyHybrid CompactionStrategy = "hybrid"

	// StrategyTruncateOld removes oldest messages beyond a threshold.
	StrategyTruncateOld CompactionStrategy = "truncate_old"
)

// CompactionConfig configures session compaction behavior.
type CompactionConfig struct {
	// Enabled determines if compaction is active.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Strategy is the compaction approach to use.
	Strategy CompactionStrategy `json:"strategy" yaml:"strategy"`

	// Trigger conditions (any one triggers compaction):

	// MaxMessages triggers compaction when message count exceeds this.
	MaxMessages int `json:"max_messages" yaml:"max_messages"`

	// MaxTokens triggers compaction when estimated token count exceeds this.
	MaxTokens int `json:"max_tokens" yaml:"max_tokens"`

	// MaxAgeHours triggers compaction for messages older than this.
	MaxAgeHours int `json:"max_age_hours" yaml:"max_age_hours"`

	// Strategy-specific options:

	// KeepLastN is used with StrategyLastN and StrategyHybrid.
	KeepLastN int `json:"keep_last_n" yaml:"keep_last_n"`

	// SummaryPrompt is used with StrategySummarize and StrategyHybrid.
	SummaryPrompt string `json:"summary_prompt" yaml:"summary_prompt"`

	// PreserveSystemMessages keeps system messages during compaction.
	PreserveSystemMessages bool `json:"preserve_system_messages" yaml:"preserve_system_messages"`

	// PreserveImportantMessages keeps messages marked important.
	PreserveImportantMessages bool `json:"preserve_important_messages" yaml:"preserve_important_messages"`
}

// DefaultCompactionConfig returns a sensible default compaction configuration.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		Enabled:                   false,
		Strategy:                  StrategyHybrid,
		MaxMessages:               100,
		MaxTokens:                 50000,
		MaxAgeHours:               24,
		KeepLastN:                 20,
		PreserveSystemMessages:    true,
		PreserveImportantMessages: true,
		SummaryPrompt: `Summarize the following conversation concisely, preserving:
- Key decisions and outcomes
- Important context and facts
- User preferences mentioned
- Any pending tasks or action items

Conversation:
{{messages}}

Summary:`,
	}
}

// Compactor handles session message compaction for long-running conversations.
// This implements the Clawdbot pattern of compacting sessions to manage context window limits.
type Compactor struct {
	config     CompactionConfig
	store      Store
	summarizer Summarizer
}

// Summarizer generates summaries of message history.
type Summarizer interface {
	Summarize(ctx context.Context, messages []*models.Message, prompt string) (string, error)
}

// CompactionResult contains the result of a compaction operation.
type CompactionResult struct {
	// SessionID is the compacted session.
	SessionID string

	// MessagesBeforeCompaction is the count before compaction.
	MessagesBeforeCompaction int

	// MessagesAfterCompaction is the count after compaction.
	MessagesAfterCompaction int

	// TokensEstimateBefore is the estimated token count before.
	TokensEstimateBefore int

	// TokensEstimateAfter is the estimated token count after.
	TokensEstimateAfter int

	// Summary is the generated summary (if using summarize strategy).
	Summary string

	// RemovedMessageIDs are the IDs of removed messages.
	RemovedMessageIDs []string

	// CompactedAt is when compaction occurred.
	CompactedAt time.Time

	// Strategy is the strategy that was used.
	Strategy CompactionStrategy
}

// NewCompactor creates a new session compactor.
func NewCompactor(config CompactionConfig, store Store, summarizer Summarizer) *Compactor {
	return &Compactor{
		config:     config,
		store:      store,
		summarizer: summarizer,
	}
}

// ShouldCompact checks if a session needs compaction.
func (c *Compactor) ShouldCompact(ctx context.Context, sessionID string) (bool, string) {
	if !c.config.Enabled {
		return false, ""
	}

	history, err := c.store.GetHistory(ctx, sessionID, 0) // Get all messages
	if err != nil {
		return false, ""
	}

	// Check message count threshold
	if c.config.MaxMessages > 0 && len(history) > c.config.MaxMessages {
		return true, fmt.Sprintf("message count %d exceeds threshold %d", len(history), c.config.MaxMessages)
	}

	// Check token estimate threshold
	if c.config.MaxTokens > 0 {
		tokens := estimateTokens(history)
		if tokens > c.config.MaxTokens {
			return true, fmt.Sprintf("estimated tokens %d exceeds threshold %d", tokens, c.config.MaxTokens)
		}
	}

	// Check age threshold
	if c.config.MaxAgeHours > 0 && len(history) > 0 {
		oldest := history[0].CreatedAt
		threshold := time.Now().Add(-time.Duration(c.config.MaxAgeHours) * time.Hour)
		if oldest.Before(threshold) {
			return true, fmt.Sprintf("oldest message from %v exceeds age threshold", oldest)
		}
	}

	return false, ""
}

// Compact performs compaction on a session.
func (c *Compactor) Compact(ctx context.Context, sessionID string) (*CompactionResult, error) {
	history, err := c.store.GetHistory(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get session history: %w", err)
	}

	result := &CompactionResult{
		SessionID:                sessionID,
		MessagesBeforeCompaction: len(history),
		TokensEstimateBefore:     estimateTokens(history),
		CompactedAt:              time.Now(),
		Strategy:                 c.config.Strategy,
	}

	var compactedMessages []*models.Message
	var summary string

	switch c.config.Strategy {
	case StrategyLastN:
		compactedMessages = c.compactLastN(history)

	case StrategySummarize:
		compactedMessages, summary, err = c.compactWithSummary(ctx, history)
		if err != nil {
			return nil, err
		}
		result.Summary = summary

	case StrategyHybrid:
		compactedMessages, summary, err = c.compactHybrid(ctx, history)
		if err != nil {
			return nil, err
		}
		result.Summary = summary

	case StrategyImportantOnly:
		compactedMessages = c.compactImportantOnly(history)

	case StrategyTruncateOld:
		compactedMessages = c.compactTruncateOld(history)

	default:
		return nil, fmt.Errorf("unknown compaction strategy: %s", c.config.Strategy)
	}

	// Calculate removed messages
	keptIDs := make(map[string]bool)
	for _, msg := range compactedMessages {
		keptIDs[msg.ID] = true
	}
	for _, msg := range history {
		if !keptIDs[msg.ID] {
			result.RemovedMessageIDs = append(result.RemovedMessageIDs, msg.ID)
		}
	}

	result.MessagesAfterCompaction = len(compactedMessages)
	result.TokensEstimateAfter = estimateTokens(compactedMessages)

	return result, nil
}

// compactLastN keeps only the last N messages, preserving system messages.
func (c *Compactor) compactLastN(history []*models.Message) []*models.Message {
	if len(history) <= c.config.KeepLastN {
		return history
	}

	var result []*models.Message

	// Preserve system messages if configured
	if c.config.PreserveSystemMessages {
		for _, msg := range history {
			if msg.Role == models.RoleSystem {
				result = append(result, msg)
			}
		}
	}

	// Keep the last N non-system messages
	nonSystemCount := 0
	for i := len(history) - 1; i >= 0 && nonSystemCount < c.config.KeepLastN; i-- {
		msg := history[i]
		if msg.Role != models.RoleSystem {
			result = append([]*models.Message{msg}, result...)
			nonSystemCount++
		}
	}

	return result
}

// compactWithSummary summarizes older messages.
func (c *Compactor) compactWithSummary(ctx context.Context, history []*models.Message) ([]*models.Message, string, error) {
	if c.summarizer == nil {
		return c.compactLastN(history), "", nil
	}

	// Split into messages to summarize and messages to keep
	keepCount := c.config.KeepLastN
	if keepCount <= 0 {
		keepCount = 10
	}

	var toSummarize []*models.Message
	var toKeep []*models.Message
	var systemMessages []*models.Message

	for i, msg := range history {
		if msg.Role == models.RoleSystem && c.config.PreserveSystemMessages {
			systemMessages = append(systemMessages, msg)
			continue
		}
		if i < len(history)-keepCount {
			toSummarize = append(toSummarize, msg)
		} else {
			toKeep = append(toKeep, msg)
		}
	}

	// Generate summary
	var summary string
	if len(toSummarize) > 0 {
		var err error
		summary, err = c.summarizer.Summarize(ctx, toSummarize, c.config.SummaryPrompt)
		if err != nil {
			return nil, "", fmt.Errorf("summarization failed: %w", err)
		}
	}

	// Build result: system messages + summary message + recent messages
	result := append([]*models.Message{}, systemMessages...)
	if summary != "" {
		result = append(result, &models.Message{
			Role:    models.RoleSystem,
			Content: fmt.Sprintf("[Conversation Summary]\n%s", summary),
			Metadata: map[string]any{
				"compaction_summary": true,
				"summarized_count":   len(toSummarize),
				"summarized_at":      time.Now().Format(time.RFC3339),
			},
		})
	}
	result = append(result, toKeep...)

	return result, summary, nil
}

// compactHybrid combines summarization with recent message retention.
func (c *Compactor) compactHybrid(ctx context.Context, history []*models.Message) ([]*models.Message, string, error) {
	return c.compactWithSummary(ctx, history)
}

// compactImportantOnly keeps only messages marked as important.
func (c *Compactor) compactImportantOnly(history []*models.Message) []*models.Message {
	var result []*models.Message

	for _, msg := range history {
		keep := false

		// Always keep system messages if configured
		if msg.Role == models.RoleSystem && c.config.PreserveSystemMessages {
			keep = true
		}

		// Check if marked important
		if c.config.PreserveImportantMessages && msg.Metadata != nil {
			if important, ok := msg.Metadata["important"].(bool); ok && important {
				keep = true
			}
			if priority, ok := msg.Metadata["priority"].(string); ok && priority == "high" {
				keep = true
			}
		}

		if keep {
			result = append(result, msg)
		}
	}

	return result
}

// compactTruncateOld removes messages older than the threshold.
func (c *Compactor) compactTruncateOld(history []*models.Message) []*models.Message {
	if c.config.MaxAgeHours <= 0 {
		return history
	}

	threshold := time.Now().Add(-time.Duration(c.config.MaxAgeHours) * time.Hour)
	var result []*models.Message

	for _, msg := range history {
		// Keep system messages regardless of age
		if msg.Role == models.RoleSystem && c.config.PreserveSystemMessages {
			result = append(result, msg)
			continue
		}

		// Keep messages newer than threshold
		if msg.CreatedAt.After(threshold) {
			result = append(result, msg)
		}
	}

	return result
}

// estimateTokens provides a rough token estimate for messages.
// Uses a simple heuristic: ~4 characters per token.
func estimateTokens(messages []*models.Message) int {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content)
		// Add overhead for role, metadata, etc.
		totalChars += 20
	}
	return totalChars / 4
}

// MarkMessageImportant marks a message as important for compaction preservation.
func MarkMessageImportant(msg *models.Message) {
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata["important"] = true
	msg.Metadata["marked_important_at"] = time.Now().Format(time.RFC3339)
}

// IsMessageImportant checks if a message is marked as important.
func IsMessageImportant(msg *models.Message) bool {
	if msg.Metadata == nil {
		return false
	}
	if important, ok := msg.Metadata["important"].(bool); ok {
		return important
	}
	return false
}

// CompactionInfo stores compaction metadata in sessions.
type CompactionInfo struct {
	LastCompactedAt          time.Time          `json:"last_compacted_at"`
	Strategy                 CompactionStrategy `json:"strategy"`
	MessagesBeforeCompaction int                `json:"messages_before_compaction"`
	MessagesAfterCompaction  int                `json:"messages_after_compaction"`
	TokensSaved              int                `json:"tokens_saved"`
	CompactionCount          int                `json:"compaction_count"`
}

// GetCompactionInfo retrieves compaction info from session metadata.
func GetCompactionInfo(session *models.Session) *CompactionInfo {
	if session.Metadata == nil {
		return nil
	}
	if info, ok := session.Metadata[MetaKeyCompactionInfo].(*CompactionInfo); ok {
		return info
	}
	return nil
}

// SetCompactionInfo stores compaction info in session metadata.
func SetCompactionInfo(session *models.Session, info *CompactionInfo) {
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}
	session.Metadata[MetaKeyCompactionInfo] = info
	session.Metadata[MetaKeyLastCompactedAt] = info.LastCompactedAt.Format(time.RFC3339)
}
