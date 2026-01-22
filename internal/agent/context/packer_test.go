package context

import (
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestPacker_IncludesIncomingMessage(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi there"},
	}
	incoming := &models.Message{ID: "3", Role: models.RoleUser, Content: "How are you?"}

	packed, err := packer.Pack(history, incoming, nil)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Should have history + incoming
	if len(packed) != 3 {
		t.Errorf("expected 3 messages, got %d", len(packed))
	}

	// Last message should be incoming
	last := packed[len(packed)-1]
	if last.ID != "3" {
		t.Errorf("last message should be incoming, got ID %s", last.ID)
	}
	if last.Content != "How are you?" {
		t.Errorf("last message content mismatch")
	}
}

func TestPacker_RespectsMaxMessages(t *testing.T) {
	opts := DefaultPackOptions()
	opts.MaxMessages = 3 // Only allow 3 messages total
	packer := NewPacker(opts)

	// Create 10 history messages
	history := make([]*models.Message, 10)
	for i := 0; i < 10; i++ {
		history[i] = &models.Message{
			ID:      string(rune('a' + i)),
			Role:    models.RoleUser,
			Content: strings.Repeat("x", 100),
		}
	}
	incoming := &models.Message{ID: "incoming", Role: models.RoleUser, Content: "hi"}

	packed, err := packer.Pack(history, incoming, nil)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Should respect MaxMessages (3)
	if len(packed) > opts.MaxMessages {
		t.Errorf("packed %d messages, exceeds MaxMessages %d", len(packed), opts.MaxMessages)
	}

	// Should include incoming message
	found := false
	for _, m := range packed {
		if m.ID == "incoming" {
			found = true
			break
		}
	}
	if !found {
		t.Error("incoming message not included in packed result")
	}
}

func TestPacker_RespectsMaxChars(t *testing.T) {
	opts := DefaultPackOptions()
	opts.MaxChars = 500 // Very small char budget
	packer := NewPacker(opts)

	// Create messages with 200 chars each
	history := make([]*models.Message, 5)
	for i := 0; i < 5; i++ {
		history[i] = &models.Message{
			ID:      string(rune('a' + i)),
			Role:    models.RoleUser,
			Content: strings.Repeat("x", 200),
		}
	}
	incoming := &models.Message{ID: "incoming", Role: models.RoleUser, Content: strings.Repeat("y", 50)}

	packed, err := packer.Pack(history, incoming, nil)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Count total chars
	totalChars := 0
	for _, m := range packed {
		totalChars += len(m.Content)
	}

	if totalChars > opts.MaxChars {
		t.Errorf("total chars %d exceeds MaxChars %d", totalChars, opts.MaxChars)
	}

	// Should prioritize recent messages (history should include most recent ones)
	// The last history message should be included if budget allows
	if len(packed) > 1 {
		// Check that we have the most recent history message (e) if any history included
		foundRecent := false
		for _, m := range packed {
			if m.ID == "e" { // Last history message
				foundRecent = true
				break
			}
		}
		// If any history is included, it should be the most recent
		for _, m := range packed {
			if m.ID != "incoming" && m.ID == "a" && foundRecent {
				t.Error("oldest message included but not newest")
			}
		}
	}
}

func TestPacker_TruncatesToolResults(t *testing.T) {
	opts := DefaultPackOptions()
	opts.MaxToolResultChars = 100
	packer := NewPacker(opts)

	history := []*models.Message{
		{
			ID:   "1",
			Role: models.RoleTool,
			ToolResults: []models.ToolResult{
				{
					ToolCallID: "tc1",
					Content:    strings.Repeat("x", 500), // Exceeds limit
				},
			},
		},
	}
	incoming := &models.Message{ID: "2", Role: models.RoleUser, Content: "hi"}

	packed, err := packer.Pack(history, incoming, nil)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Find the tool result message
	var toolMsg *models.Message
	for _, m := range packed {
		if len(m.ToolResults) > 0 {
			toolMsg = m
			break
		}
	}

	if toolMsg == nil {
		t.Fatal("tool message not found in packed result")
	}

	// Check truncation
	content := toolMsg.ToolResults[0].Content
	if len(content) > opts.MaxToolResultChars+20 { // +20 for truncation suffix
		t.Errorf("tool result not truncated: len=%d, expected ~%d", len(content), opts.MaxToolResultChars)
	}
	if !strings.Contains(content, "...[truncated]") {
		t.Error("truncated tool result missing truncation marker")
	}
}

func TestPacker_IncludesSummary(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
	}
	incoming := &models.Message{ID: "2", Role: models.RoleUser, Content: "hi"}
	summary := &models.Message{
		ID:      "summary",
		Role:    models.RoleSystem,
		Content: "This is a summary",
		Metadata: map[string]any{
			SummaryMetadataKey: true,
		},
	}

	packed, err := packer.Pack(history, incoming, summary)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Summary should be first
	if len(packed) < 1 {
		t.Fatal("packed result is empty")
	}
	if packed[0].ID != "summary" {
		t.Errorf("summary should be first, got ID %s", packed[0].ID)
	}
}

func TestPacker_FiltersSummaryMessagesFromHistory(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())

	// History contains a summary message
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{
			ID:      "old-summary",
			Role:    models.RoleSystem,
			Content: "Old summary",
			Metadata: map[string]any{
				SummaryMetadataKey: true,
			},
		},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi"},
	}
	incoming := &models.Message{ID: "3", Role: models.RoleUser, Content: "hi"}

	// Pass a different summary
	newSummary := &models.Message{
		ID:      "new-summary",
		Role:    models.RoleSystem,
		Content: "New summary",
		Metadata: map[string]any{
			SummaryMetadataKey: true,
		},
	}

	packed, err := packer.Pack(history, incoming, newSummary)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Should not include old summary from history
	for _, m := range packed {
		if m.ID == "old-summary" {
			t.Error("old summary from history should be filtered out")
		}
	}

	// Should include new summary
	found := false
	for _, m := range packed {
		if m.ID == "new-summary" {
			found = true
			break
		}
	}
	if !found {
		t.Error("new summary should be included")
	}
}

func TestFindLatestSummary(t *testing.T) {
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{
			ID:      "summary1",
			Role:    models.RoleSystem,
			Content: "First summary",
			Metadata: map[string]any{
				SummaryMetadataKey: true,
			},
		},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi"},
		{
			ID:      "summary2",
			Role:    models.RoleSystem,
			Content: "Second summary",
			Metadata: map[string]any{
				SummaryMetadataKey: true,
			},
		},
		{ID: "3", Role: models.RoleUser, Content: "Thanks"},
	}

	summary := FindLatestSummary(history)
	if summary == nil {
		t.Fatal("expected to find summary")
	}
	if summary.ID != "summary2" {
		t.Errorf("expected latest summary (summary2), got %s", summary.ID)
	}
}

func TestFindLatestSummary_NoSummary(t *testing.T) {
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi"},
	}

	summary := FindLatestSummary(history)
	if summary != nil {
		t.Error("expected nil when no summary exists")
	}
}

func TestMessagesSinceSummary(t *testing.T) {
	summary := &models.Message{
		ID:      "summary",
		Role:    models.RoleSystem,
		Content: "Summary",
		Metadata: map[string]any{
			SummaryMetadataKey: true,
		},
	}

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		summary,
		{ID: "2", Role: models.RoleAssistant, Content: "Hi"},
		{ID: "3", Role: models.RoleUser, Content: "Thanks"},
	}

	since := MessagesSinceSummary(history, summary)
	if len(since) != 2 {
		t.Errorf("expected 2 messages after summary, got %d", len(since))
	}
	if since[0].ID != "2" || since[1].ID != "3" {
		t.Error("messages after summary are incorrect")
	}
}

func TestGetMessagesToSummarize(t *testing.T) {
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello", CreatedAt: time.Now().Add(-5 * time.Hour)},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi", CreatedAt: time.Now().Add(-4 * time.Hour)},
		{ID: "3", Role: models.RoleUser, Content: "How are you?", CreatedAt: time.Now().Add(-3 * time.Hour)},
		{ID: "4", Role: models.RoleAssistant, Content: "Good!", CreatedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "5", Role: models.RoleUser, Content: "Great", CreatedAt: time.Now().Add(-1 * time.Hour)},
	}

	// Keep 2 recent, should summarize 3
	toSummarize := GetMessagesToSummarize(history, nil, 2)
	if len(toSummarize) != 3 {
		t.Errorf("expected 3 messages to summarize, got %d", len(toSummarize))
	}

	// Verify it's the older messages
	for _, m := range toSummarize {
		if m.ID == "4" || m.ID == "5" {
			t.Errorf("recent message %s should not be in summarize list", m.ID)
		}
	}
}

// =============================================================================
// Diagnostics Tests
// =============================================================================

func TestPackWithDiagnostics_BasicCounts(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi there"},
	}
	incoming := &models.Message{ID: "3", Role: models.RoleUser, Content: "How are you?"}

	result := packer.PackWithDiagnostics(history, incoming, nil)

	if result.Diagnostics == nil {
		t.Fatal("expected diagnostics")
	}

	diag := result.Diagnostics
	if diag.Candidates != 2 {
		t.Errorf("expected 2 candidates (history), got %d", diag.Candidates)
	}
	if diag.Included != 2 {
		t.Errorf("expected 2 included, got %d", diag.Included)
	}
	if diag.Dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", diag.Dropped)
	}
	if diag.SummaryUsed {
		t.Error("expected SummaryUsed=false")
	}
}

func TestPackWithDiagnostics_BudgetTracking(t *testing.T) {
	opts := DefaultPackOptions()
	opts.MaxChars = 500
	opts.MaxMessages = 10
	packer := NewPacker(opts)

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: strings.Repeat("a", 100)},
		{ID: "2", Role: models.RoleAssistant, Content: strings.Repeat("b", 100)},
	}
	incoming := &models.Message{ID: "3", Role: models.RoleUser, Content: strings.Repeat("c", 50)}

	result := packer.PackWithDiagnostics(history, incoming, nil)
	diag := result.Diagnostics

	if diag.BudgetChars != 500 {
		t.Errorf("expected BudgetChars=500, got %d", diag.BudgetChars)
	}
	if diag.BudgetMessages != 10 {
		t.Errorf("expected BudgetMessages=10, got %d", diag.BudgetMessages)
	}
	// Used chars should be tracked
	if diag.UsedChars <= 0 {
		t.Errorf("expected positive UsedChars, got %d", diag.UsedChars)
	}
	if diag.UsedMessages != 3 { // 2 history + 1 incoming
		t.Errorf("expected UsedMessages=3, got %d", diag.UsedMessages)
	}
}

func TestPackWithDiagnostics_DroppedDueToOverBudget(t *testing.T) {
	opts := DefaultPackOptions()
	opts.MaxChars = 200 // Very small budget
	packer := NewPacker(opts)

	// Create messages that exceed budget
	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: strings.Repeat("a", 100)},
		{ID: "2", Role: models.RoleAssistant, Content: strings.Repeat("b", 100)},
		{ID: "3", Role: models.RoleUser, Content: strings.Repeat("c", 100)},
	}
	incoming := &models.Message{ID: "4", Role: models.RoleUser, Content: strings.Repeat("d", 50)}

	result := packer.PackWithDiagnostics(history, incoming, nil)
	diag := result.Diagnostics

	// Should have dropped some messages
	if diag.Dropped == 0 {
		t.Error("expected some dropped messages due to budget")
	}

	// Check items have correct reasons
	var overBudgetCount int
	for _, item := range diag.Items {
		if item.Reason == models.ContextReasonOverBudget {
			overBudgetCount++
			if item.Included {
				t.Error("over_budget item should not be included")
			}
		}
	}
	if overBudgetCount == 0 {
		t.Error("expected some items with over_budget reason")
	}
}

func TestPackWithDiagnostics_SummaryTracking(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
	}
	incoming := &models.Message{ID: "2", Role: models.RoleUser, Content: "hi"}
	summary := &models.Message{
		ID:      "summary",
		Role:    models.RoleSystem,
		Content: strings.Repeat("x", 200),
		Metadata: map[string]any{
			SummaryMetadataKey: true,
		},
	}

	result := packer.PackWithDiagnostics(history, incoming, summary)
	diag := result.Diagnostics

	if !diag.SummaryUsed {
		t.Error("expected SummaryUsed=true")
	}
	if diag.SummaryChars != 200 {
		t.Errorf("expected SummaryChars=200, got %d", diag.SummaryChars)
	}

	// Check items include summary with reserved reason
	var foundSummaryItem bool
	for _, item := range diag.Items {
		if item.Kind == models.ContextItemSummary {
			foundSummaryItem = true
			if item.Reason != models.ContextReasonReserved {
				t.Errorf("expected summary reason=reserved, got %s", item.Reason)
			}
			if !item.Included {
				t.Error("summary item should be included")
			}
		}
	}
	if !foundSummaryItem {
		t.Error("expected summary item in diagnostics")
	}
}

func TestPackWithDiagnostics_ItemKindClassification(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())

	history := []*models.Message{
		{ID: "1", Role: models.RoleUser, Content: "Hello"},
		{ID: "2", Role: models.RoleAssistant, Content: "Hi", ToolCalls: []models.ToolCall{{ID: "tc1", Name: "test"}}},
		{ID: "3", Role: models.RoleTool, ToolResults: []models.ToolResult{{ToolCallID: "tc1", Content: "result"}}},
	}
	incoming := &models.Message{ID: "4", Role: models.RoleUser, Content: "thanks"}

	result := packer.PackWithDiagnostics(history, incoming, nil)
	diag := result.Diagnostics

	kindCounts := make(map[models.ContextItemKind]int)
	for _, item := range diag.Items {
		kindCounts[item.Kind]++
	}

	if kindCounts[models.ContextItemHistory] != 1 { // User message without tools
		t.Errorf("expected 1 history item, got %d", kindCounts[models.ContextItemHistory])
	}
	if kindCounts[models.ContextItemTool] != 2 { // Assistant with tool calls + tool results
		t.Errorf("expected 2 tool items, got %d", kindCounts[models.ContextItemTool])
	}
	if kindCounts[models.ContextItemIncoming] != 1 {
		t.Errorf("expected 1 incoming item, got %d", kindCounts[models.ContextItemIncoming])
	}
}

func TestPackWithDiagnostics_ItemIDs(t *testing.T) {
	packer := NewPacker(DefaultPackOptions())

	history := []*models.Message{
		{ID: "msg-1", Role: models.RoleUser, Content: "Hello"},
		{ID: "msg-2", Role: models.RoleAssistant, Content: "Hi"},
	}
	incoming := &models.Message{ID: "msg-3", Role: models.RoleUser, Content: "How are you?"}

	result := packer.PackWithDiagnostics(history, incoming, nil)
	diag := result.Diagnostics

	// All items should have non-empty IDs (hashes)
	for i, item := range diag.Items {
		if item.ID == "" {
			t.Errorf("item %d has empty ID", i)
		}
		if len(item.ID) != 12 { // Our hash is truncated to 12 chars
			t.Errorf("item %d ID has unexpected length: %d", i, len(item.ID))
		}
	}
}
