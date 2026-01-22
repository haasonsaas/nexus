package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// mockSummarizer implements Summarizer for testing.
type mockSummarizer struct {
	summarizeFunc func(ctx context.Context, messages []*models.Message, prompt string) (string, error)
}

func (m *mockSummarizer) Summarize(ctx context.Context, messages []*models.Message, prompt string) (string, error) {
	if m.summarizeFunc != nil {
		return m.summarizeFunc(ctx, messages, prompt)
	}
	return "Test summary of " + string(rune(len(messages))) + " messages", nil
}

// TestDefaultCompactionConfig tests the default configuration.
func TestDefaultCompactionConfig(t *testing.T) {
	cfg := DefaultCompactionConfig()

	if cfg.Enabled {
		t.Error("Enabled should default to false")
	}
	if cfg.Strategy != StrategyHybrid {
		t.Errorf("Strategy should default to hybrid, got %s", cfg.Strategy)
	}
	if cfg.MaxMessages != 100 {
		t.Errorf("MaxMessages should default to 100, got %d", cfg.MaxMessages)
	}
	if cfg.MaxTokens != 50000 {
		t.Errorf("MaxTokens should default to 50000, got %d", cfg.MaxTokens)
	}
	if cfg.MaxAgeHours != 24 {
		t.Errorf("MaxAgeHours should default to 24, got %d", cfg.MaxAgeHours)
	}
	if cfg.KeepLastN != 20 {
		t.Errorf("KeepLastN should default to 20, got %d", cfg.KeepLastN)
	}
	if !cfg.PreserveSystemMessages {
		t.Error("PreserveSystemMessages should default to true")
	}
	if !cfg.PreserveImportantMessages {
		t.Error("PreserveImportantMessages should default to true")
	}
	if cfg.SummaryPrompt == "" {
		t.Error("SummaryPrompt should have a default value")
	}
}

// TestNewCompactor tests compactor creation.
func TestNewCompactor(t *testing.T) {
	cfg := DefaultCompactionConfig()
	store := NewMemoryStore()
	summarizer := &mockSummarizer{}

	compactor := NewCompactor(cfg, store, summarizer)

	if compactor == nil {
		t.Error("NewCompactor should return a non-nil compactor")
	}
}

// TestCompactor_ShouldCompact tests the ShouldCompact method.
func TestCompactor_ShouldCompact(t *testing.T) {
	tests := []struct {
		name          string
		config        CompactionConfig
		setupMessages func(store *MemoryStore, sessionID string)
		wantCompact   bool
		wantReason    string
	}{
		{
			name: "compaction disabled",
			config: CompactionConfig{
				Enabled:     false,
				MaxMessages: 10,
			},
			setupMessages: func(store *MemoryStore, sessionID string) {
				for i := 0; i < 20; i++ {
					msg := &models.Message{Role: models.RoleUser, Content: "test"}
					store.AppendMessage(context.Background(), sessionID, msg)
				}
			},
			wantCompact: false,
		},
		{
			name: "message count exceeds threshold",
			config: CompactionConfig{
				Enabled:     true,
				MaxMessages: 10,
			},
			setupMessages: func(store *MemoryStore, sessionID string) {
				for i := 0; i < 15; i++ {
					msg := &models.Message{Role: models.RoleUser, Content: "test"}
					store.AppendMessage(context.Background(), sessionID, msg)
				}
			},
			wantCompact: true,
			wantReason:  "message count",
		},
		{
			name: "message count below threshold",
			config: CompactionConfig{
				Enabled:     true,
				MaxMessages: 100,
			},
			setupMessages: func(store *MemoryStore, sessionID string) {
				for i := 0; i < 10; i++ {
					msg := &models.Message{Role: models.RoleUser, Content: "test"}
					store.AppendMessage(context.Background(), sessionID, msg)
				}
			},
			wantCompact: false,
		},
		{
			name: "token estimate exceeds threshold",
			config: CompactionConfig{
				Enabled:   true,
				MaxTokens: 100, // Very low threshold
			},
			setupMessages: func(store *MemoryStore, sessionID string) {
				// Add messages with long content
				for i := 0; i < 10; i++ {
					msg := &models.Message{
						Role:    models.RoleUser,
						Content: "This is a fairly long message that will contribute significantly to the token count estimate.",
					}
					store.AppendMessage(context.Background(), sessionID, msg)
				}
			},
			wantCompact: true,
			wantReason:  "estimated tokens",
		},
		{
			name: "oldest message exceeds age threshold",
			config: CompactionConfig{
				Enabled:     true,
				MaxAgeHours: 1,
			},
			setupMessages: func(store *MemoryStore, sessionID string) {
				// Add an old message
				msg := &models.Message{
					Role:      models.RoleUser,
					Content:   "old message",
					CreatedAt: time.Now().Add(-2 * time.Hour),
				}
				store.AppendMessage(context.Background(), sessionID, msg)
			},
			wantCompact: true,
			wantReason:  "oldest message",
		},
		{
			name: "no messages",
			config: CompactionConfig{
				Enabled:     true,
				MaxMessages: 10,
			},
			setupMessages: func(store *MemoryStore, sessionID string) {
				// Don't add any messages
			},
			wantCompact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			ctx := context.Background()

			// Create session
			session := &models.Session{ID: "test-session", AgentID: "agent-1"}
			if err := store.Create(ctx, session); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			tt.setupMessages(store, "test-session")

			compactor := NewCompactor(tt.config, store, nil)
			shouldCompact, reason := compactor.ShouldCompact(ctx, "test-session")

			if shouldCompact != tt.wantCompact {
				t.Errorf("ShouldCompact() = %v, want %v (reason: %s)", shouldCompact, tt.wantCompact, reason)
			}
			if tt.wantCompact && tt.wantReason != "" {
				if !contains(reason, tt.wantReason) {
					t.Errorf("reason should contain %q, got %q", tt.wantReason, reason)
				}
			}
		})
	}
}

// TestCompactor_Compact_StrategyLastN tests LastN compaction strategy.
func TestCompactor_Compact_StrategyLastN(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create session
	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add messages
	for i := 0; i < 20; i++ {
		role := models.RoleUser
		if i%2 == 1 {
			role = models.RoleAssistant
		}
		msg := &models.Message{Role: role, Content: "message"}
		if err := store.AppendMessage(ctx, "test-session", msg); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	// Add a system message
	sysMsg := &models.Message{Role: models.RoleSystem, Content: "system prompt"}
	if err := store.AppendMessage(ctx, "test-session", sysMsg); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	config := CompactionConfig{
		Enabled:                true,
		Strategy:               StrategyLastN,
		KeepLastN:              5,
		PreserveSystemMessages: true,
	}

	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	if result.MessagesBeforeCompaction != 21 {
		t.Errorf("MessagesBeforeCompaction = %d, want 21", result.MessagesBeforeCompaction)
	}

	// Should keep 5 non-system messages + 1 system message = 6 total
	if result.MessagesAfterCompaction != 6 {
		t.Errorf("MessagesAfterCompaction = %d, want 6", result.MessagesAfterCompaction)
	}

	if result.Strategy != StrategyLastN {
		t.Errorf("Strategy = %s, want %s", result.Strategy, StrategyLastN)
	}

	if len(result.RemovedMessageIDs) != 15 {
		t.Errorf("RemovedMessageIDs count = %d, want 15", len(result.RemovedMessageIDs))
	}
}

// TestCompactor_Compact_StrategySummarize tests summarization strategy.
func TestCompactor_Compact_StrategySummarize(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Create session
	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add messages
	for i := 0; i < 20; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "message"}
		if err := store.AppendMessage(ctx, "test-session", msg); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	summarizer := &mockSummarizer{
		summarizeFunc: func(ctx context.Context, messages []*models.Message, prompt string) (string, error) {
			return "Summary: This is a test summary", nil
		},
	}

	config := CompactionConfig{
		Enabled:                true,
		Strategy:               StrategySummarize,
		KeepLastN:              5,
		PreserveSystemMessages: true,
	}

	compactor := NewCompactor(config, store, summarizer)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}

	// Should have summary message + 5 recent messages = 6 total
	// (result includes a new system message with the summary)
	if result.MessagesAfterCompaction < 5 {
		t.Errorf("MessagesAfterCompaction = %d, want at least 5", result.MessagesAfterCompaction)
	}
}

// TestCompactor_Compact_StrategySummarize_NoSummarizer tests fallback when no summarizer.
func TestCompactor_Compact_StrategySummarize_NoSummarizer(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	for i := 0; i < 20; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "message"}
		store.AppendMessage(ctx, "test-session", msg)
	}

	config := CompactionConfig{
		Enabled:   true,
		Strategy:  StrategySummarize,
		KeepLastN: 5,
	}

	// No summarizer provided
	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	// Should fall back to LastN strategy
	if result.Summary != "" {
		t.Error("Summary should be empty when no summarizer")
	}
}

// TestCompactor_Compact_StrategySummarize_SummarizerError tests summarizer error handling.
func TestCompactor_Compact_StrategySummarize_SummarizerError(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	for i := 0; i < 20; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "message"}
		store.AppendMessage(ctx, "test-session", msg)
	}

	summarizer := &mockSummarizer{
		summarizeFunc: func(ctx context.Context, messages []*models.Message, prompt string) (string, error) {
			return "", errors.New("summarization failed")
		},
	}

	config := CompactionConfig{
		Enabled:   true,
		Strategy:  StrategySummarize,
		KeepLastN: 5,
	}

	compactor := NewCompactor(config, store, summarizer)
	_, err := compactor.Compact(ctx, "test-session")
	if err == nil {
		t.Error("expected error when summarizer fails")
	}
}

// TestCompactor_Compact_StrategyImportantOnly tests important-only strategy.
func TestCompactor_Compact_StrategyImportantOnly(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add regular messages
	for i := 0; i < 15; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "regular message"}
		store.AppendMessage(ctx, "test-session", msg)
	}

	// Add important messages
	for i := 0; i < 3; i++ {
		msg := &models.Message{
			Role:     models.RoleUser,
			Content:  "important message",
			Metadata: map[string]any{"important": true},
		}
		store.AppendMessage(ctx, "test-session", msg)
	}

	// Add high priority message
	msg := &models.Message{
		Role:     models.RoleUser,
		Content:  "high priority message",
		Metadata: map[string]any{"priority": "high"},
	}
	store.AppendMessage(ctx, "test-session", msg)

	// Add system message
	sysMsg := &models.Message{Role: models.RoleSystem, Content: "system prompt"}
	store.AppendMessage(ctx, "test-session", sysMsg)

	config := CompactionConfig{
		Enabled:                   true,
		Strategy:                  StrategyImportantOnly,
		PreserveSystemMessages:    true,
		PreserveImportantMessages: true,
	}

	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	// Should keep: 3 important + 1 high priority + 1 system = 5
	if result.MessagesAfterCompaction != 5 {
		t.Errorf("MessagesAfterCompaction = %d, want 5", result.MessagesAfterCompaction)
	}
}

// TestCompactor_Compact_StrategyTruncateOld tests age-based truncation.
func TestCompactor_Compact_StrategyTruncateOld(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Add old messages
	for i := 0; i < 10; i++ {
		msg := &models.Message{
			Role:      models.RoleUser,
			Content:   "old message",
			CreatedAt: time.Now().Add(-48 * time.Hour),
		}
		store.AppendMessage(ctx, "test-session", msg)
	}

	// Add recent messages
	for i := 0; i < 5; i++ {
		msg := &models.Message{
			Role:      models.RoleUser,
			Content:   "recent message",
			CreatedAt: time.Now(),
		}
		store.AppendMessage(ctx, "test-session", msg)
	}

	// Add system message (old but should be preserved)
	sysMsg := &models.Message{
		Role:      models.RoleSystem,
		Content:   "system prompt",
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	store.AppendMessage(ctx, "test-session", sysMsg)

	config := CompactionConfig{
		Enabled:                true,
		Strategy:               StrategyTruncateOld,
		MaxAgeHours:            24,
		PreserveSystemMessages: true,
	}

	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	// Should keep: 5 recent + 1 system (preserved) = 6
	if result.MessagesAfterCompaction != 6 {
		t.Errorf("MessagesAfterCompaction = %d, want 6", result.MessagesAfterCompaction)
	}
}

// TestCompactor_Compact_StrategyTruncateOld_NoMaxAge tests truncation with zero max age.
func TestCompactor_Compact_StrategyTruncateOld_NoMaxAge(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	store.Create(ctx, session)

	for i := 0; i < 10; i++ {
		msg := &models.Message{
			Role:      models.RoleUser,
			Content:   "message",
			CreatedAt: time.Now().Add(-48 * time.Hour),
		}
		store.AppendMessage(ctx, "test-session", msg)
	}

	config := CompactionConfig{
		Enabled:     true,
		Strategy:    StrategyTruncateOld,
		MaxAgeHours: 0, // Zero means no age limit
	}

	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	// No messages should be removed when MaxAgeHours is 0
	if result.MessagesAfterCompaction != 10 {
		t.Errorf("MessagesAfterCompaction = %d, want 10", result.MessagesAfterCompaction)
	}
}

// TestCompactor_Compact_StrategyHybrid tests hybrid strategy.
func TestCompactor_Compact_StrategyHybrid(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	store.Create(ctx, session)

	for i := 0; i < 20; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "message"}
		store.AppendMessage(ctx, "test-session", msg)
	}

	summarizer := &mockSummarizer{
		summarizeFunc: func(ctx context.Context, messages []*models.Message, prompt string) (string, error) {
			return "Hybrid summary", nil
		},
	}

	config := CompactionConfig{
		Enabled:   true,
		Strategy:  StrategyHybrid,
		KeepLastN: 5,
	}

	compactor := NewCompactor(config, store, summarizer)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	if result.Summary == "" {
		t.Error("Summary should not be empty for hybrid strategy")
	}
}

// TestCompactor_Compact_UnknownStrategy tests error for unknown strategy.
func TestCompactor_Compact_UnknownStrategy(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	store.Create(ctx, session)

	msg := &models.Message{Role: models.RoleUser, Content: "message"}
	store.AppendMessage(ctx, "test-session", msg)

	config := CompactionConfig{
		Enabled:  true,
		Strategy: "unknown_strategy",
	}

	compactor := NewCompactor(config, store, nil)
	_, err := compactor.Compact(ctx, "test-session")
	if err == nil {
		t.Error("expected error for unknown strategy")
	}
}

// TestCompactor_Compact_GetHistoryError tests error handling when GetHistory fails.
func TestCompactor_Compact_GetHistoryError(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Don't create session - GetHistory will return empty

	config := CompactionConfig{
		Enabled:  true,
		Strategy: StrategyLastN,
	}

	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "non-existent-session")

	// Should succeed with 0 messages
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MessagesBeforeCompaction != 0 {
		t.Errorf("expected 0 messages, got %d", result.MessagesBeforeCompaction)
	}
}

// TestCompactor_compactLastN_LessThanLimit tests when messages are less than limit.
func TestCompactor_compactLastN_LessThanLimit(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	session := &models.Session{ID: "test-session", AgentID: "agent-1"}
	store.Create(ctx, session)

	// Add only 5 messages (less than KeepLastN = 10)
	for i := 0; i < 5; i++ {
		msg := &models.Message{Role: models.RoleUser, Content: "message"}
		store.AppendMessage(ctx, "test-session", msg)
	}

	config := CompactionConfig{
		Enabled:   true,
		Strategy:  StrategyLastN,
		KeepLastN: 10,
	}

	compactor := NewCompactor(config, store, nil)
	result, err := compactor.Compact(ctx, "test-session")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	// All messages should be kept
	if result.MessagesAfterCompaction != 5 {
		t.Errorf("MessagesAfterCompaction = %d, want 5", result.MessagesAfterCompaction)
	}
	if len(result.RemovedMessageIDs) != 0 {
		t.Errorf("RemovedMessageIDs should be empty, got %d", len(result.RemovedMessageIDs))
	}
}

// TestEstimateTokens tests token estimation.
func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []*models.Message
		wantMin  int // Minimum expected tokens
		wantMax  int // Maximum expected tokens
	}{
		{
			name:     "empty messages",
			messages: []*models.Message{},
			wantMin:  0,
			wantMax:  0,
		},
		{
			name: "single short message",
			messages: []*models.Message{
				{Content: "Hello"},
			},
			wantMin: 1,
			wantMax: 10,
		},
		{
			name: "multiple messages",
			messages: []*models.Message{
				{Content: "Hello, how are you?"},
				{Content: "I am doing well, thank you for asking."},
			},
			wantMin: 10,
			wantMax: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.messages)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("estimateTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestMarkMessageImportant tests marking messages as important.
func TestMarkMessageImportant(t *testing.T) {
	msg := &models.Message{
		Role:    models.RoleUser,
		Content: "important content",
	}

	MarkMessageImportant(msg)

	if msg.Metadata == nil {
		t.Fatal("Metadata should be initialized")
	}
	if !msg.Metadata["important"].(bool) {
		t.Error("Message should be marked as important")
	}
	if msg.Metadata["marked_important_at"] == nil {
		t.Error("marked_important_at should be set")
	}
}

// TestMarkMessageImportant_ExistingMetadata tests marking with existing metadata.
func TestMarkMessageImportant_ExistingMetadata(t *testing.T) {
	msg := &models.Message{
		Role:     models.RoleUser,
		Content:  "content",
		Metadata: map[string]any{"existing": "value"},
	}

	MarkMessageImportant(msg)

	if msg.Metadata["existing"] != "value" {
		t.Error("Existing metadata should be preserved")
	}
	if !msg.Metadata["important"].(bool) {
		t.Error("Message should be marked as important")
	}
}

// TestIsMessageImportant tests checking if message is important.
func TestIsMessageImportant(t *testing.T) {
	tests := []struct {
		name     string
		message  *models.Message
		expected bool
	}{
		{
			name:     "nil metadata",
			message:  &models.Message{},
			expected: false,
		},
		{
			name: "not marked important",
			message: &models.Message{
				Metadata: map[string]any{"other": "value"},
			},
			expected: false,
		},
		{
			name: "marked important true",
			message: &models.Message{
				Metadata: map[string]any{"important": true},
			},
			expected: true,
		},
		{
			name: "marked important false",
			message: &models.Message{
				Metadata: map[string]any{"important": false},
			},
			expected: false,
		},
		{
			name: "important wrong type",
			message: &models.Message{
				Metadata: map[string]any{"important": "yes"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMessageImportant(tt.message)
			if got != tt.expected {
				t.Errorf("IsMessageImportant() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestGetCompactionInfo tests retrieving compaction info from session.
func TestGetCompactionInfo(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		session := &models.Session{}
		info := GetCompactionInfo(session)
		if info != nil {
			t.Error("expected nil info")
		}
	})

	t.Run("no compaction info", func(t *testing.T) {
		session := &models.Session{
			Metadata: map[string]any{"other": "value"},
		}
		info := GetCompactionInfo(session)
		if info != nil {
			t.Error("expected nil info")
		}
	})

	t.Run("with compaction info", func(t *testing.T) {
		compactionInfo := &CompactionInfo{
			LastCompactedAt: time.Now(),
			Strategy:        StrategyHybrid,
			CompactionCount: 5,
		}
		session := &models.Session{
			Metadata: map[string]any{
				MetaKeyCompactionInfo: compactionInfo,
			},
		}
		info := GetCompactionInfo(session)
		if info == nil {
			t.Fatal("expected non-nil info")
		}
		if info.CompactionCount != 5 {
			t.Errorf("CompactionCount = %d, want 5", info.CompactionCount)
		}
	})
}

// TestSetCompactionInfo tests storing compaction info in session.
func TestSetCompactionInfo(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		session := &models.Session{}
		info := &CompactionInfo{
			LastCompactedAt: time.Now(),
			Strategy:        StrategyLastN,
			CompactionCount: 1,
		}

		SetCompactionInfo(session, info)

		if session.Metadata == nil {
			t.Fatal("Metadata should be initialized")
		}
		if session.Metadata[MetaKeyCompactionInfo] != info {
			t.Error("CompactionInfo should be stored")
		}
		if session.Metadata[MetaKeyLastCompactedAt] == nil {
			t.Error("LastCompactedAt should be stored")
		}
	})

	t.Run("existing metadata", func(t *testing.T) {
		session := &models.Session{
			Metadata: map[string]any{"existing": "value"},
		}
		info := &CompactionInfo{
			LastCompactedAt: time.Now(),
		}

		SetCompactionInfo(session, info)

		if session.Metadata["existing"] != "value" {
			t.Error("Existing metadata should be preserved")
		}
	})
}

// TestCompactionResult tests CompactionResult fields.
func TestCompactionResult(t *testing.T) {
	now := time.Now()
	result := &CompactionResult{
		SessionID:                "session-1",
		MessagesBeforeCompaction: 100,
		MessagesAfterCompaction:  25,
		TokensEstimateBefore:     5000,
		TokensEstimateAfter:      1250,
		Summary:                  "Test summary",
		RemovedMessageIDs:        []string{"msg-1", "msg-2"},
		CompactedAt:              now,
		Strategy:                 StrategyHybrid,
	}

	if result.SessionID != "session-1" {
		t.Errorf("SessionID = %s, want session-1", result.SessionID)
	}
	if result.MessagesBeforeCompaction != 100 {
		t.Errorf("MessagesBeforeCompaction = %d, want 100", result.MessagesBeforeCompaction)
	}
	if len(result.RemovedMessageIDs) != 2 {
		t.Errorf("RemovedMessageIDs count = %d, want 2", len(result.RemovedMessageIDs))
	}
}
