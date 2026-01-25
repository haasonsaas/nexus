package compaction

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		msg      *Message
		expected int
	}{
		{"nil message", nil, 0},
		{"empty message", &Message{}, 0},
		{"short content", &Message{Content: "Hello"}, 2},          // 5 chars / 4 = 1.25 -> 2
		{"exact multiple", &Message{Content: "12345678"}, 2},      // 8 chars / 4 = 2
		{"with tool calls", &Message{Content: "Hi", ToolCalls: "call"}, 2}, // 6 chars / 4 = 1.5 -> 2
		{"with tool results", &Message{Content: "Hi", ToolResults: "result"}, 2}, // 8 chars / 4 = 2
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.msg)
			if result != tt.expected {
				t.Errorf("EstimateTokens() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	messages := []*Message{
		{Content: "Hello"},    // 2 tokens
		{Content: "World"},    // 2 tokens
		{Content: "12345678"}, // 2 tokens
	}

	result := EstimateMessagesTokens(messages)
	if result != 6 {
		t.Errorf("EstimateMessagesTokens() = %d, want 6", result)
	}

	// Empty slice
	if EstimateMessagesTokens(nil) != 0 {
		t.Error("EstimateMessagesTokens(nil) should return 0")
	}
}

func TestSplitMessagesByTokenShare(t *testing.T) {
	tests := []struct {
		name          string
		messages      []*Message
		parts         int
		expectedParts int
	}{
		{"empty messages", nil, 2, 0},
		{"single message", []*Message{{Content: "test"}}, 2, 1},
		{"zero parts", []*Message{{Content: "test"}}, 0, 1},
		{"one part", []*Message{{Content: "test"}, {Content: "test2"}}, 1, 1},
		{"fewer messages than parts", []*Message{{Content: "t"}}, 3, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitMessagesByTokenShare(tt.messages, tt.parts)
			if len(result) != tt.expectedParts {
				t.Errorf("SplitMessagesByTokenShare() returned %d parts, want %d", len(result), tt.expectedParts)
			}
		})
	}

	// Test balanced splitting
	t.Run("balanced split", func(t *testing.T) {
		messages := make([]*Message, 10)
		for i := range messages {
			messages[i] = &Message{Content: strings.Repeat("a", 40)} // 10 tokens each
		}
		result := SplitMessagesByTokenShare(messages, 2)
		if len(result) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(result))
		}
		// Both parts should have roughly equal messages
		diff := len(result[0]) - len(result[1])
		if diff < -2 || diff > 2 {
			t.Errorf("unbalanced split: %d vs %d messages", len(result[0]), len(result[1]))
		}
	})
}

func TestChunkMessagesByMaxTokens(t *testing.T) {
	tests := []struct {
		name           string
		messages       []*Message
		maxTokens      int
		expectedChunks int
	}{
		{"empty messages", nil, 100, 0},
		{"zero max tokens", []*Message{{Content: "test"}}, 0, 1},
		{"single message fits", []*Message{{Content: "test"}}, 100, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ChunkMessagesByMaxTokens(tt.messages, tt.maxTokens)
			if len(result) != tt.expectedChunks {
				t.Errorf("ChunkMessagesByMaxTokens() = %d chunks, want %d", len(result), tt.expectedChunks)
			}
		})
	}

	t.Run("respects max tokens", func(t *testing.T) {
		messages := make([]*Message, 5)
		for i := range messages {
			messages[i] = &Message{Content: strings.Repeat("a", 40)} // 10 tokens each
		}
		result := ChunkMessagesByMaxTokens(messages, 25) // Should fit 2 per chunk
		if len(result) < 2 {
			t.Errorf("expected at least 2 chunks, got %d", len(result))
		}
		for i, chunk := range result {
			tokens := EstimateMessagesTokens(chunk)
			if tokens > 25 && len(chunk) > 1 {
				t.Errorf("chunk %d has %d tokens, exceeds max 25", i, tokens)
			}
		}
	})

	t.Run("oversized single message", func(t *testing.T) {
		messages := []*Message{
			{Content: "small"},
			{Content: strings.Repeat("a", 200)}, // 50 tokens, oversized
			{Content: "small2"},
		}
		result := ChunkMessagesByMaxTokens(messages, 10)
		// Oversized message should be in its own chunk
		foundOversized := false
		for _, chunk := range result {
			if len(chunk) == 1 && EstimateTokens(chunk[0]) > 10 {
				foundOversized = true
				break
			}
		}
		if !foundOversized {
			t.Error("oversized message should be in its own chunk")
		}
	})
}

func TestComputeAdaptiveChunkRatio(t *testing.T) {
	tests := []struct {
		name          string
		messages      []*Message
		contextWindow int
		minRatio      float64
		maxRatio      float64
	}{
		{"empty messages", nil, 100000, BaseChunkRatio, BaseChunkRatio},
		{"zero context window", []*Message{{Content: "test"}}, 0, BaseChunkRatio, BaseChunkRatio},
		{"small messages", []*Message{{Content: "small"}}, 100000, MinChunkRatio, BaseChunkRatio},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeAdaptiveChunkRatio(tt.messages, tt.contextWindow)
			if result < tt.minRatio || result > tt.maxRatio {
				t.Errorf("ComputeAdaptiveChunkRatio() = %f, want between %f and %f", result, tt.minRatio, tt.maxRatio)
			}
		})
	}

	t.Run("large messages reduce ratio", func(t *testing.T) {
		smallMsgs := []*Message{{Content: "small"}}
		largeMsgs := []*Message{{Content: strings.Repeat("a", 40000)}}

		smallRatio := ComputeAdaptiveChunkRatio(smallMsgs, 100000)
		largeRatio := ComputeAdaptiveChunkRatio(largeMsgs, 100000)

		if largeRatio >= smallRatio {
			t.Errorf("large messages should have smaller ratio: %f >= %f", largeRatio, smallRatio)
		}
	})
}

func TestIsOversizedForSummary(t *testing.T) {
	tests := []struct {
		name          string
		msg           *Message
		contextWindow int
		expected      bool
	}{
		{"nil message", nil, 100000, false},
		{"zero context window", &Message{Content: "test"}, 0, false},
		{"small message", &Message{Content: "small"}, 100000, false},
		{"oversized message", &Message{Content: strings.Repeat("a", 300000)}, 100000, true}, // 75000 tokens > 50% of 100000
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsOversizedForSummary(tt.msg, tt.contextWindow)
			if result != tt.expected {
				t.Errorf("IsOversizedForSummary() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDefaultSummarizationConfig(t *testing.T) {
	config := DefaultSummarizationConfig()

	if config.ReserveTokens <= 0 {
		t.Error("ReserveTokens should be positive")
	}
	if config.MaxChunkTokens <= 0 {
		t.Error("MaxChunkTokens should be positive")
	}
	if config.ContextWindow <= 0 {
		t.Error("ContextWindow should be positive")
	}
	if config.Parts <= 0 {
		t.Error("Parts should be positive")
	}
	if config.MinMessagesForSplit <= 0 {
		t.Error("MinMessagesForSplit should be positive")
	}
}

// mockSummarizer implements Summarizer for testing.
type mockSummarizer struct {
	summaries    []string
	callCount    int
	shouldError  bool
	errorMessage string
}

func (m *mockSummarizer) GenerateSummary(ctx context.Context, messages []*Message, config *SummarizationConfig) (string, error) {
	if m.shouldError {
		return "", fmt.Errorf("%s", m.errorMessage)
	}
	summary := fmt.Sprintf("Summary of %d messages", len(messages))
	if m.callCount < len(m.summaries) {
		summary = m.summaries[m.callCount]
	}
	m.callCount++
	return summary, nil
}

func TestSummarizeChunks(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		result, err := SummarizeChunks(context.Background(), nil, &mockSummarizer{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != DefaultSummaryFallback {
			t.Errorf("expected fallback, got: %s", result)
		}
	})

	t.Run("nil summarizer", func(t *testing.T) {
		_, err := SummarizeChunks(context.Background(), []*Message{{Content: "test"}}, nil, nil)
		if err == nil {
			t.Error("expected error for nil summarizer")
		}
	})

	t.Run("single chunk", func(t *testing.T) {
		summarizer := &mockSummarizer{summaries: []string{"Single summary"}}
		messages := []*Message{{Content: "test"}}
		result, err := SummarizeChunks(context.Background(), messages, summarizer, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "Single summary" {
			t.Errorf("expected 'Single summary', got: %s", result)
		}
		if summarizer.callCount != 1 {
			t.Errorf("expected 1 call, got %d", summarizer.callCount)
		}
	})

	t.Run("multiple chunks", func(t *testing.T) {
		summarizer := &mockSummarizer{
			summaries: []string{"Chunk 1", "Chunk 2", "Merged"},
		}
		messages := make([]*Message, 10)
		for i := range messages {
			messages[i] = &Message{Content: strings.Repeat("a", 4000)}
		}
		config := &SummarizationConfig{MaxChunkTokens: 2500, ContextWindow: 100000}
		result, err := SummarizeChunks(context.Background(), messages, summarizer, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summarizer.callCount < 2 {
			t.Errorf("expected at least 2 calls for chunking, got %d", summarizer.callCount)
		}
		_ = result
	})

	t.Run("summarizer error", func(t *testing.T) {
		summarizer := &mockSummarizer{shouldError: true, errorMessage: "test error"}
		messages := []*Message{{Content: "test"}}
		_, err := SummarizeChunks(context.Background(), messages, summarizer, nil)
		if err == nil {
			t.Error("expected error from summarizer")
		}
	})
}

func TestSummarizeWithFallback(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		result, err := SummarizeWithFallback(context.Background(), nil, &mockSummarizer{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != DefaultSummaryFallback {
			t.Errorf("expected fallback, got: %s", result)
		}
	})

	t.Run("nil summarizer", func(t *testing.T) {
		_, err := SummarizeWithFallback(context.Background(), []*Message{{Content: "test"}}, nil, nil)
		if err == nil {
			t.Error("expected error for nil summarizer")
		}
	})

	t.Run("handles oversized messages", func(t *testing.T) {
		summarizer := &mockSummarizer{summaries: []string{"Normal summary"}}
		messages := []*Message{
			{Content: "normal"},
			{Role: "user", Content: strings.Repeat("a", 300000)}, // 75000 tokens > 50% of 100000
		}
		config := &SummarizationConfig{ContextWindow: 100000, MaxChunkTokens: 50000}
		result, err := SummarizeWithFallback(context.Background(), messages, summarizer, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Oversized") {
			t.Error("result should contain note about oversized message")
		}
	})

	t.Run("all oversized", func(t *testing.T) {
		summarizer := &mockSummarizer{}
		messages := []*Message{
			{Role: "user", Content: strings.Repeat("a", 300000)}, // 75000 tokens > 50% of 100000
		}
		config := &SummarizationConfig{ContextWindow: 100000, MaxChunkTokens: 50000}
		result, err := SummarizeWithFallback(context.Background(), messages, summarizer, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, DefaultSummaryFallback) {
			t.Error("result should contain fallback when all oversized")
		}
	})
}

func TestSummarizeInStages(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		result, err := SummarizeInStages(context.Background(), nil, &mockSummarizer{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != DefaultSummaryFallback {
			t.Errorf("expected fallback, got: %s", result)
		}
	})

	t.Run("nil summarizer", func(t *testing.T) {
		_, err := SummarizeInStages(context.Background(), []*Message{{Content: "test"}}, nil, nil)
		if err == nil {
			t.Error("expected error for nil summarizer")
		}
	})

	t.Run("few messages no split", func(t *testing.T) {
		summarizer := &mockSummarizer{summaries: []string{"Summary"}}
		messages := []*Message{{Content: "test"}, {Content: "test2"}}
		config := &SummarizationConfig{
			Parts:               2,
			MinMessagesForSplit: 10, // Won't split
			ContextWindow:       100000,
			MaxChunkTokens:      50000,
		}
		_, err := SummarizeInStages(context.Background(), messages, summarizer, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("with previous summary", func(t *testing.T) {
		summarizer := &mockSummarizer{
			summaries: []string{"Part 1", "Part 2", "Merged with previous"},
		}
		messages := make([]*Message, 10)
		for i := range messages {
			messages[i] = &Message{Content: "test message"}
		}
		config := &SummarizationConfig{
			Parts:               2,
			MinMessagesForSplit: 2,
			ContextWindow:       100000,
			MaxChunkTokens:      50000,
			PreviousSummary:     "Previous context",
		}
		_, err := SummarizeInStages(context.Background(), messages, summarizer, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPruneHistoryForContextShare(t *testing.T) {
	t.Run("empty messages", func(t *testing.T) {
		result := PruneHistoryForContextShare(nil, 1000, 0.5, 2)
		if len(result.Messages) != 0 {
			t.Error("expected empty result for empty input")
		}
	})

	t.Run("zero context tokens", func(t *testing.T) {
		messages := []*Message{{Content: "test"}}
		result := PruneHistoryForContextShare(messages, 0, 0.5, 2)
		if len(result.Messages) != 1 {
			t.Error("expected original messages for zero context")
		}
	})

	t.Run("within budget", func(t *testing.T) {
		messages := []*Message{{Content: "test"}}
		result := PruneHistoryForContextShare(messages, 100000, 0.5, 2)
		if len(result.Messages) != 1 {
			t.Error("expected all messages when within budget")
		}
		if result.DroppedMessages != 0 {
			t.Errorf("expected 0 dropped, got %d", result.DroppedMessages)
		}
	})

	t.Run("exceeds budget", func(t *testing.T) {
		messages := make([]*Message, 10)
		for i := range messages {
			messages[i] = &Message{Content: strings.Repeat("a", 400)} // 100 tokens each
		}
		// Total: 1000 tokens, budget: 500
		result := PruneHistoryForContextShare(messages, 1000, 0.5, 2)
		if result.DroppedMessages == 0 {
			t.Error("expected some messages to be dropped")
		}
		if result.KeptTokens > 500 {
			t.Errorf("kept tokens %d exceeds budget 500", result.KeptTokens)
		}
	})

	t.Run("keeps most recent", func(t *testing.T) {
		messages := []*Message{
			{Content: "old", ID: "1"},
			{Content: "older", ID: "2"},
			{Content: "newest", ID: "3"},
		}
		// Very small budget - should keep only newest
		result := PruneHistoryForContextShare(messages, 10, 1.0, 2)
		if len(result.Messages) == 0 {
			t.Fatal("should keep at least one message")
		}
		if result.Messages[len(result.Messages)-1].ID != "3" {
			t.Error("should keep the newest message")
		}
	})

	t.Run("invalid share clamps to 1.0", func(t *testing.T) {
		messages := []*Message{{Content: "test"}}
		result := PruneHistoryForContextShare(messages, 1000, 1.5, 2)
		if result.BudgetTokens != 1000 {
			t.Errorf("expected budget 1000 (100%% of 1000), got %d", result.BudgetTokens)
		}
	})
}

func TestResolveContextWindowTokens(t *testing.T) {
	tests := []struct {
		name          string
		model         int
		defaultVal    int
		expected      int
	}{
		{"model value", 50000, 100000, 50000},
		{"default value", 0, 80000, 80000},
		{"both zero", 0, 0, DefaultContextWindow},
		{"negative model", -1, 80000, 80000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveContextWindowTokens(tt.model, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("ResolveContextWindowTokens(%d, %d) = %d, want %d",
					tt.model, tt.defaultVal, result, tt.expected)
			}
		})
	}
}

func TestFormatMessagesForSummary(t *testing.T) {
	messages := []*Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there", ToolCalls: "call_123"},
		nil, // Should be skipped
		{Role: "user", Content: "Thanks", ToolResults: "result data"},
	}

	result := FormatMessagesForSummary(messages)

	if !strings.Contains(result, "[user]: Hello") {
		t.Error("should contain user message")
	}
	if !strings.Contains(result, "[assistant]: Hi there") {
		t.Error("should contain assistant message")
	}
	if !strings.Contains(result, "[Tool calls:") {
		t.Error("should contain tool calls")
	}
	if !strings.Contains(result, "[Tool results:") {
		t.Error("should contain tool results")
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is to..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		result := truncateString(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestConstants(t *testing.T) {
	// Verify constants have expected values
	if BaseChunkRatio != 0.4 {
		t.Errorf("BaseChunkRatio = %f, want 0.4", BaseChunkRatio)
	}
	if MinChunkRatio != 0.15 {
		t.Errorf("MinChunkRatio = %f, want 0.15", MinChunkRatio)
	}
	if SafetyMargin != 1.2 {
		t.Errorf("SafetyMargin = %f, want 1.2", SafetyMargin)
	}
	if DefaultSummaryFallback != "No prior history." {
		t.Errorf("DefaultSummaryFallback = %q, unexpected", DefaultSummaryFallback)
	}
	if DefaultParts != 2 {
		t.Errorf("DefaultParts = %d, want 2", DefaultParts)
	}
	if CharsPerToken != 4 {
		t.Errorf("CharsPerToken = %d, want 4", CharsPerToken)
	}
}
