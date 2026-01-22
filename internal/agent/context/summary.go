package context

import (
	"github.com/haasonsaas/nexus/pkg/models"
)

// SummaryMetadataKey is the metadata key used to identify summary messages.
const SummaryMetadataKey = "nexus_summary"

// SummaryVersionKey is the metadata key for summary version tracking.
const SummaryVersionKey = "summary_version"

// CoversUntilKey is the metadata key indicating which message ID the summary covers up to.
const CoversUntilKey = "covers_until"

// FindLatestSummary finds the most recent summary message in history.
// Returns nil if no summary exists.
func FindLatestSummary(history []*models.Message) *models.Message {
	// Scan from end (most recent) to find latest summary
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m == nil || m.Metadata == nil {
			continue
		}
		if val, ok := m.Metadata[SummaryMetadataKey]; ok {
			if b, ok := val.(bool); ok && b {
				return m
			}
		}
	}
	return nil
}

// MessagesSinceSummary returns messages that came after the given summary.
// If summary is nil, returns all messages.
func MessagesSinceSummary(history []*models.Message, summary *models.Message) []*models.Message {
	if summary == nil {
		return history
	}

	// Find the summary's position in history
	summaryIdx := -1
	for i, m := range history {
		if m != nil && m.ID == summary.ID {
			summaryIdx = i
			break
		}
	}

	// If summary not found in history, return all messages
	if summaryIdx < 0 {
		return history
	}

	// Return messages after the summary
	if summaryIdx+1 >= len(history) {
		return nil
	}
	return history[summaryIdx+1:]
}

// NeedsSummarization checks if the history needs summarization based on thresholds.
func NeedsSummarization(history []*models.Message, summary *models.Message, maxMsgsBeforeSummary int) bool {
	messagesSince := MessagesSinceSummary(history, summary)
	return len(messagesSince) > maxMsgsBeforeSummary
}

// CreateSummaryMessage creates a new summary message with proper metadata.
func CreateSummaryMessage(sessionID, summaryContent, coversUntilMsgID string) *models.Message {
	return &models.Message{
		SessionID: sessionID,
		Role:      models.RoleSystem,
		Content:   summaryContent,
		Metadata: map[string]any{
			SummaryMetadataKey: true,
			SummaryVersionKey:  1,
			CoversUntilKey:     coversUntilMsgID,
		},
	}
}

// GetMessagesToSummarize returns older messages that should be summarized.
// It keeps the most recent `keepRecent` messages and returns the rest for summarization.
func GetMessagesToSummarize(history []*models.Message, summary *models.Message, keepRecent int) []*models.Message {
	messages := MessagesSinceSummary(history, summary)

	// Filter out summary messages
	filtered := make([]*models.Message, 0, len(messages))
	for _, m := range messages {
		if m == nil || m.Metadata == nil {
			filtered = append(filtered, m)
			continue
		}
		if val, ok := m.Metadata[SummaryMetadataKey]; ok {
			if b, ok := val.(bool); ok && b {
				continue // Skip summary messages
			}
		}
		filtered = append(filtered, m)
	}

	// Return older messages (everything except the last keepRecent)
	if len(filtered) <= keepRecent {
		return nil
	}
	return filtered[:len(filtered)-keepRecent]
}
