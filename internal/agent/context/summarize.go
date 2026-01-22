package context

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

// SummarizationConfig configures the summarization behavior.
type SummarizationConfig struct {
	// MaxMsgsBeforeSummary is the threshold for triggering summarization.
	// Default: 30 messages since last summary.
	MaxMsgsBeforeSummary int

	// KeepRecentMessages is how many recent messages to keep un-summarized.
	// Default: 10.
	KeepRecentMessages int

	// MaxSummaryLength is the target length for summaries in characters.
	// Default: 2000.
	MaxSummaryLength int
}

// DefaultSummarizationConfig returns sensible defaults.
func DefaultSummarizationConfig() SummarizationConfig {
	return SummarizationConfig{
		MaxMsgsBeforeSummary: 30,
		KeepRecentMessages:   10,
		MaxSummaryLength:     2000,
	}
}

// SummaryProvider is the interface for generating summaries.
// This allows injecting a fake provider for testing.
type SummaryProvider interface {
	// Summarize generates a summary of the given messages.
	Summarize(ctx context.Context, messages []*models.Message, maxLength int) (string, error)
}

// Summarizer handles conversation summarization.
type Summarizer struct {
	provider SummaryProvider
	config   SummarizationConfig
}

// NewSummarizer creates a new summarizer with the given provider and config.
func NewSummarizer(provider SummaryProvider, config SummarizationConfig) *Summarizer {
	if config.MaxMsgsBeforeSummary <= 0 {
		config.MaxMsgsBeforeSummary = 30
	}
	if config.KeepRecentMessages <= 0 {
		config.KeepRecentMessages = 10
	}
	if config.MaxSummaryLength <= 0 {
		config.MaxSummaryLength = 2000
	}
	return &Summarizer{
		provider: provider,
		config:   config,
	}
}

// ShouldSummarize checks if summarization is needed based on history state.
func (s *Summarizer) ShouldSummarize(history []*models.Message, currentSummary *models.Message) bool {
	return NeedsSummarization(history, currentSummary, s.config.MaxMsgsBeforeSummary)
}

// Summarize generates a new summary message if needed.
// Returns the new summary message, or nil if no summarization was needed.
func (s *Summarizer) Summarize(ctx context.Context, sessionID string, history []*models.Message, currentSummary *models.Message) (*models.Message, error) {
	if !s.ShouldSummarize(history, currentSummary) {
		return nil, nil
	}

	// Get messages to summarize (older messages, keeping recent ones)
	toSummarize := GetMessagesToSummarize(history, currentSummary, s.config.KeepRecentMessages)
	if len(toSummarize) == 0 {
		return nil, nil
	}

	// Generate summary
	summaryContent, err := s.provider.Summarize(ctx, toSummarize, s.config.MaxSummaryLength)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	// Find the last message that was summarized
	var coversUntilMsgID string
	if len(toSummarize) > 0 {
		lastMsg := toSummarize[len(toSummarize)-1]
		if lastMsg != nil {
			coversUntilMsgID = lastMsg.ID
		}
	}

	// Create summary message
	summaryMsg := &models.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      models.RoleSystem,
		Content:   summaryContent,
		Metadata: map[string]any{
			SummaryMetadataKey: true,
			SummaryVersionKey:  1,
			CoversUntilKey:     coversUntilMsgID,
		},
		CreatedAt: time.Now(),
	}

	return summaryMsg, nil
}

// BuildSummarizationPrompt creates the prompt for summarizing messages.
// This is used by LLM-based summary providers.
func BuildSummarizationPrompt(messages []*models.Message, maxLength int) string {
	var sb strings.Builder

	sb.WriteString("Please summarize the following conversation concisely. ")
	sb.WriteString(fmt.Sprintf("Keep the summary under %d characters. ", maxLength))
	sb.WriteString("Focus on:\n")
	sb.WriteString("- Key topics discussed\n")
	sb.WriteString("- Important decisions or conclusions\n")
	sb.WriteString("- Any pending tasks or questions\n")
	sb.WriteString("- Tool executions and their outcomes\n\n")
	sb.WriteString("Conversation:\n\n")

	for _, m := range messages {
		if m == nil {
			continue
		}

		// Format role
		role := string(m.Role)
		sb.WriteString(fmt.Sprintf("[%s]: ", role))

		// Add content
		if m.Content != "" {
			sb.WriteString(m.Content)
		}

		// Add tool calls
		for _, tc := range m.ToolCalls {
			sb.WriteString(fmt.Sprintf("\n  [Called tool: %s]", tc.Name))
		}

		// Add tool results (abbreviated)
		for _, tr := range m.ToolResults {
			content := tr.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			status := "success"
			if tr.IsError {
				status = "error"
			}
			sb.WriteString(fmt.Sprintf("\n  [Tool result (%s): %s]", status, content))
		}

		sb.WriteString("\n\n")
	}

	sb.WriteString("---\nProvide a concise summary:")
	return sb.String()
}
