// Package compaction provides context compaction utilities for managing
// conversation history within token budgets. It implements token estimation,
// message splitting, chunked summarization, and history pruning following
// patterns from clawdbot's agents/compaction.ts.
package compaction

import (
	"context"
	"fmt"
	"strings"
)

// Constants for compaction behavior
const (
	// BaseChunkRatio is the default ratio of context window for chunk sizing.
	BaseChunkRatio = 0.4

	// MinChunkRatio is the minimum ratio to prevent overly small chunks.
	MinChunkRatio = 0.15

	// SafetyMargin provides a 20% buffer for token estimation inaccuracy.
	SafetyMargin = 1.2

	// DefaultSummaryFallback is returned when there's no prior history to summarize.
	DefaultSummaryFallback = "No prior history."

	// DefaultParts is the default number of parts for multi-stage summarization.
	DefaultParts = 2

	// OversizedThreshold is the fraction of context window above which a single
	// message is considered too large to summarize (50%).
	OversizedThreshold = 0.5

	// CharsPerToken is the approximate character-to-token ratio for estimation.
	CharsPerToken = 4

	// DefaultContextWindow is the fallback context window size in tokens.
	DefaultContextWindow = 100000

	// DefaultMinMessagesForSplit is the minimum messages needed before splitting.
	DefaultMinMessagesForSplit = 4
)

// Message represents a conversation message for compaction.
type Message struct {
	// Role is the message role (e.g., "user", "assistant", "system").
	Role string

	// Content is the text content of the message.
	Content string

	// Timestamp is the Unix timestamp when the message was created.
	Timestamp int64

	// ID is an optional unique identifier for the message.
	ID string

	// ToolCalls contains any tool call information (serialized).
	ToolCalls string

	// ToolResults contains any tool result information (serialized).
	ToolResults string

	// Metadata contains additional message metadata.
	Metadata map[string]any
}

// EstimateTokens estimates token count for a message using a simple heuristic.
// Approximation: ~4 characters per token.
func EstimateTokens(msg *Message) int {
	if msg == nil {
		return 0
	}
	chars := len(msg.Content) + len(msg.ToolCalls) + len(msg.ToolResults)
	return (chars + CharsPerToken - 1) / CharsPerToken // Ceiling division
}

// EstimateMessagesTokens estimates total tokens across all messages.
func EstimateMessagesTokens(messages []*Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateTokens(msg)
	}
	return total
}

// SplitMessagesByTokenShare splits messages into N parts with roughly equal token counts.
// This produces balanced chunks for parallel processing.
func SplitMessagesByTokenShare(messages []*Message, parts int) [][]*Message {
	if len(messages) == 0 {
		return nil
	}
	if parts <= 0 {
		parts = DefaultParts
	}
	if parts == 1 || len(messages) < parts {
		return [][]*Message{messages}
	}

	totalTokens := EstimateMessagesTokens(messages)
	targetPerPart := totalTokens / parts

	result := make([][]*Message, 0, parts)
	currentPart := make([]*Message, 0)
	currentTokens := 0

	for i, msg := range messages {
		msgTokens := EstimateTokens(msg)
		currentPart = append(currentPart, msg)
		currentTokens += msgTokens

		// Check if we should start a new part
		remainingParts := parts - len(result) - 1
		isLastMessage := i == len(messages)-1

		if !isLastMessage && remainingParts > 0 && currentTokens >= targetPerPart {
			result = append(result, currentPart)
			currentPart = make([]*Message, 0)
			currentTokens = 0
		}
	}

	// Append any remaining messages
	if len(currentPart) > 0 {
		result = append(result, currentPart)
	}

	return result
}

// ChunkMessagesByMaxTokens splits messages into chunks where each chunk
// does not exceed maxTokens. This ensures hard limits are respected.
func ChunkMessagesByMaxTokens(messages []*Message, maxTokens int) [][]*Message {
	if len(messages) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		return [][]*Message{messages}
	}

	result := make([][]*Message, 0)
	currentChunk := make([]*Message, 0)
	currentTokens := 0

	for _, msg := range messages {
		msgTokens := EstimateTokens(msg)

		// If a single message exceeds maxTokens, it gets its own chunk
		if msgTokens > maxTokens {
			if len(currentChunk) > 0 {
				result = append(result, currentChunk)
				currentChunk = make([]*Message, 0)
				currentTokens = 0
			}
			result = append(result, []*Message{msg})
			continue
		}

		// If adding this message would exceed limit, start new chunk
		if currentTokens+msgTokens > maxTokens && len(currentChunk) > 0 {
			result = append(result, currentChunk)
			currentChunk = make([]*Message, 0)
			currentTokens = 0
		}

		currentChunk = append(currentChunk, msg)
		currentTokens += msgTokens
	}

	// Append any remaining messages
	if len(currentChunk) > 0 {
		result = append(result, currentChunk)
	}

	return result
}

// ComputeAdaptiveChunkRatio computes chunk ratio based on average message size.
// When messages are large, uses smaller chunks to avoid exceeding model limits.
func ComputeAdaptiveChunkRatio(messages []*Message, contextWindow int) float64 {
	if len(messages) == 0 || contextWindow <= 0 {
		return BaseChunkRatio
	}

	totalTokens := EstimateMessagesTokens(messages)
	avgTokensPerMsg := float64(totalTokens) / float64(len(messages))
	windowRatio := avgTokensPerMsg / float64(contextWindow)

	// Scale down ratio for larger messages
	// As messages get larger relative to context, use smaller chunks
	ratio := BaseChunkRatio * (1 - windowRatio*SafetyMargin)
	if ratio < MinChunkRatio {
		ratio = MinChunkRatio
	}
	if ratio > BaseChunkRatio {
		ratio = BaseChunkRatio
	}

	return ratio
}

// IsOversizedForSummary returns true if a single message is too large to summarize.
// A message is considered oversized if it exceeds 50% of the context window.
func IsOversizedForSummary(msg *Message, contextWindow int) bool {
	if msg == nil || contextWindow <= 0 {
		return false
	}
	msgTokens := EstimateTokens(msg)
	threshold := float64(contextWindow) * OversizedThreshold
	return float64(msgTokens) > threshold
}

// SummarizationConfig for summarization operations.
type SummarizationConfig struct {
	// Model is the LLM model identifier to use for summarization.
	Model string

	// APIKey is the API key for the LLM provider.
	APIKey string

	// ReserveTokens is the number of tokens to reserve for the response.
	ReserveTokens int

	// MaxChunkTokens is the maximum tokens per chunk for summarization.
	MaxChunkTokens int

	// ContextWindow is the total context window size in tokens.
	ContextWindow int

	// CustomInstructions are additional instructions for the summarizer.
	CustomInstructions string

	// PreviousSummary is the previous summary to build upon.
	PreviousSummary string

	// Parts is the number of parts for multi-stage summarization.
	Parts int

	// MinMessagesForSplit is the minimum messages required before splitting.
	MinMessagesForSplit int
}

// DefaultSummarizationConfig returns a config with sensible defaults.
func DefaultSummarizationConfig() *SummarizationConfig {
	return &SummarizationConfig{
		ReserveTokens:       2000,
		MaxChunkTokens:      20000,
		ContextWindow:       DefaultContextWindow,
		Parts:               DefaultParts,
		MinMessagesForSplit: DefaultMinMessagesForSplit,
	}
}

// Summarizer interface for generating summaries.
type Summarizer interface {
	// GenerateSummary generates a summary of the given messages.
	GenerateSummary(ctx context.Context, messages []*Message, config *SummarizationConfig) (string, error)
}

// SummarizeChunks summarizes messages in chunks, then merges the chunk summaries.
func SummarizeChunks(ctx context.Context, messages []*Message, summarizer Summarizer, config *SummarizationConfig) (string, error) {
	if len(messages) == 0 {
		return DefaultSummaryFallback, nil
	}
	if summarizer == nil {
		return "", fmt.Errorf("summarizer is nil")
	}
	if config == nil {
		config = DefaultSummarizationConfig()
	}

	maxChunkTokens := config.MaxChunkTokens
	if maxChunkTokens <= 0 {
		maxChunkTokens = int(float64(config.ContextWindow) * BaseChunkRatio)
	}

	chunks := ChunkMessagesByMaxTokens(messages, maxChunkTokens)
	if len(chunks) == 0 {
		return DefaultSummaryFallback, nil
	}

	// If only one chunk, summarize directly
	if len(chunks) == 1 {
		return summarizer.GenerateSummary(ctx, chunks[0], config)
	}

	// Summarize each chunk
	chunkSummaries := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		summary, err := summarizer.GenerateSummary(ctx, chunk, config)
		if err != nil {
			return "", fmt.Errorf("summarizing chunk %d: %w", i, err)
		}
		chunkSummaries = append(chunkSummaries, summary)
	}

	// Merge chunk summaries
	return mergeSummaries(ctx, chunkSummaries, summarizer, config)
}

// mergeSummaries combines multiple chunk summaries into a final summary.
func mergeSummaries(ctx context.Context, summaries []string, summarizer Summarizer, config *SummarizationConfig) (string, error) {
	if len(summaries) == 0 {
		return DefaultSummaryFallback, nil
	}
	if len(summaries) == 1 {
		return summaries[0], nil
	}

	// Create synthetic messages from the summaries for the merge pass
	mergeMessages := make([]*Message, len(summaries))
	for i, s := range summaries {
		mergeMessages[i] = &Message{
			Role:    "system",
			Content: fmt.Sprintf("Chunk %d summary:\n%s", i+1, s),
		}
	}

	// Create a merge config with instructions to combine summaries
	mergeConfig := *config
	mergeConfig.CustomInstructions = "Merge these chunk summaries into a single coherent summary. Preserve key details and maintain chronological flow."
	if config.CustomInstructions != "" {
		mergeConfig.CustomInstructions = config.CustomInstructions + "\n\n" + mergeConfig.CustomInstructions
	}

	return summarizer.GenerateSummary(ctx, mergeMessages, &mergeConfig)
}

// SummarizeWithFallback tries full summarization, falls back to partial if oversized.
// For oversized messages, it notes them instead of failing.
func SummarizeWithFallback(ctx context.Context, messages []*Message, summarizer Summarizer, config *SummarizationConfig) (string, error) {
	if len(messages) == 0 {
		return DefaultSummaryFallback, nil
	}
	if summarizer == nil {
		return "", fmt.Errorf("summarizer is nil")
	}
	if config == nil {
		config = DefaultSummarizationConfig()
	}

	// Separate oversized messages from normal ones
	var normal []*Message
	var oversizedNotes []string

	for _, msg := range messages {
		if IsOversizedForSummary(msg, config.ContextWindow) {
			// Note the oversized message instead of including it
			note := fmt.Sprintf("[Oversized %s message with %d tokens - content omitted]",
				msg.Role, EstimateTokens(msg))
			oversizedNotes = append(oversizedNotes, note)
		} else {
			normal = append(normal, msg)
		}
	}

	// Summarize normal messages
	var summary string
	var err error
	if len(normal) > 0 {
		summary, err = SummarizeChunks(ctx, normal, summarizer, config)
		if err != nil {
			return "", fmt.Errorf("summarizing normal messages: %w", err)
		}
	} else {
		summary = DefaultSummaryFallback
	}

	// Append notes about oversized messages
	if len(oversizedNotes) > 0 {
		summary = summary + "\n\n" + strings.Join(oversizedNotes, "\n")
	}

	return summary, nil
}

// SummarizeInStages splits messages into parts, summarizes each, then merges.
// This is useful for very long histories that benefit from parallel processing.
func SummarizeInStages(ctx context.Context, messages []*Message, summarizer Summarizer, config *SummarizationConfig) (string, error) {
	if len(messages) == 0 {
		return DefaultSummaryFallback, nil
	}
	if summarizer == nil {
		return "", fmt.Errorf("summarizer is nil")
	}
	if config == nil {
		config = DefaultSummarizationConfig()
	}

	parts := config.Parts
	if parts <= 0 {
		parts = DefaultParts
	}

	minMessages := config.MinMessagesForSplit
	if minMessages <= 0 {
		minMessages = DefaultMinMessagesForSplit
	}

	// Don't split if not enough messages
	if len(messages) < minMessages {
		return SummarizeWithFallback(ctx, messages, summarizer, config)
	}

	// Split into parts
	partitions := SplitMessagesByTokenShare(messages, parts)
	if len(partitions) <= 1 {
		return SummarizeWithFallback(ctx, messages, summarizer, config)
	}

	// Summarize each part
	partSummaries := make([]string, 0, len(partitions))
	for i, partition := range partitions {
		summary, err := SummarizeWithFallback(ctx, partition, summarizer, config)
		if err != nil {
			return "", fmt.Errorf("summarizing part %d: %w", i, err)
		}
		partSummaries = append(partSummaries, summary)
	}

	// Prepend previous summary if available
	if config.PreviousSummary != "" && config.PreviousSummary != DefaultSummaryFallback {
		partSummaries = append([]string{config.PreviousSummary}, partSummaries...)
	}

	// Merge all summaries
	return mergeSummaries(ctx, partSummaries, summarizer, config)
}

// PruneResult contains results from pruning history.
type PruneResult struct {
	// Messages is the pruned message list.
	Messages []*Message

	// DroppedChunks is the number of chunks that were dropped.
	DroppedChunks int

	// DroppedMessages is the total number of messages dropped.
	DroppedMessages int

	// DroppedTokens is the estimated tokens dropped.
	DroppedTokens int

	// KeptTokens is the estimated tokens kept.
	KeptTokens int

	// BudgetTokens is the token budget that was used.
	BudgetTokens int
}

// PruneHistoryForContextShare prunes history to fit within a token budget.
// It keeps the most recent messages up to the budget while respecting chunk boundaries.
func PruneHistoryForContextShare(messages []*Message, maxContextTokens int, maxHistoryShare float64, parts int) *PruneResult {
	result := &PruneResult{
		Messages:     messages,
		BudgetTokens: maxContextTokens,
	}

	if len(messages) == 0 || maxContextTokens <= 0 {
		return result
	}

	if maxHistoryShare <= 0 || maxHistoryShare > 1 {
		maxHistoryShare = 1.0
	}

	budgetTokens := int(float64(maxContextTokens) * maxHistoryShare)
	result.BudgetTokens = budgetTokens

	totalTokens := EstimateMessagesTokens(messages)
	if totalTokens <= budgetTokens {
		result.KeptTokens = totalTokens
		return result
	}

	// Need to prune - work from the end (most recent) backwards
	keptMessages := make([]*Message, 0)
	keptTokens := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		msgTokens := EstimateTokens(msg)

		if keptTokens+msgTokens > budgetTokens {
			// Can't fit this message
			break
		}

		keptMessages = append([]*Message{msg}, keptMessages...)
		keptTokens += msgTokens
	}

	droppedCount := len(messages) - len(keptMessages)
	droppedTokens := totalTokens - keptTokens

	// Count dropped chunks if we have parts
	droppedChunks := 0
	if parts > 0 && droppedCount > 0 {
		chunks := SplitMessagesByTokenShare(messages, parts)
		for _, chunk := range chunks {
			allDropped := true
			for _, msg := range chunk {
				for _, kept := range keptMessages {
					if msg == kept {
						allDropped = false
						break
					}
				}
				if !allDropped {
					break
				}
			}
			if allDropped {
				droppedChunks++
			}
		}
	}

	result.Messages = keptMessages
	result.DroppedChunks = droppedChunks
	result.DroppedMessages = droppedCount
	result.DroppedTokens = droppedTokens
	result.KeptTokens = keptTokens

	return result
}

// ResolveContextWindowTokens resolves context window size with fallback.
func ResolveContextWindowTokens(modelContextWindow, defaultContextWindow int) int {
	if modelContextWindow > 0 {
		return modelContextWindow
	}
	if defaultContextWindow > 0 {
		return defaultContextWindow
	}
	return DefaultContextWindow
}

// FormatMessagesForSummary formats messages into a string suitable for summarization.
func FormatMessagesForSummary(messages []*Message) string {
	var sb strings.Builder

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		sb.WriteString(fmt.Sprintf("[%s]: ", msg.Role))
		sb.WriteString(msg.Content)

		if msg.ToolCalls != "" {
			sb.WriteString(fmt.Sprintf("\n  [Tool calls: %s]", truncateString(msg.ToolCalls, 200)))
		}
		if msg.ToolResults != "" {
			sb.WriteString(fmt.Sprintf("\n  [Tool results: %s]", truncateString(msg.ToolResults, 200)))
		}

		sb.WriteString("\n\n")
	}

	return sb.String()
}

// truncateString truncates a string to maxLen with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
