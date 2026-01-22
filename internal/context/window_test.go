package context

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantMin  int
		wantMax  int
	}{
		{
			name:    "empty",
			text:    "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "single char",
			text:    "a",
			wantMin: 1,
			wantMax: 1,
		},
		{
			name:    "short text",
			text:    "Hello, world!",
			wantMin: 1,
			wantMax: 10,
		},
		{
			name:    "longer text",
			text:    "This is a longer piece of text that should have more tokens.",
			wantMin: 10,
			wantMax: 30,
		},
		{
			name:    "unicode text",
			text:    "你好世界",
			wantMin: 1,
			wantMax: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateTokens(%q) = %d, want between %d and %d",
					tt.text, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestWindow_Basic(t *testing.T) {
	w := NewWindow(100000, "test")

	// Initial state
	info := w.Info()
	if info.TotalTokens != 100000 {
		t.Errorf("TotalTokens = %d, want 100000", info.TotalTokens)
	}
	if info.UsedTokens != 0 {
		t.Errorf("UsedTokens = %d, want 0", info.UsedTokens)
	}
	if info.RemainingTokens != 100000 {
		t.Errorf("RemainingTokens = %d, want 100000", info.RemainingTokens)
	}

	// Add tokens
	w.Add(50000)
	info = w.Info()
	if info.UsedTokens != 50000 {
		t.Errorf("UsedTokens = %d, want 50000", info.UsedTokens)
	}
	if info.RemainingTokens != 50000 {
		t.Errorf("RemainingTokens = %d, want 50000", info.RemainingTokens)
	}

	// Reset
	w.Reset()
	info = w.Info()
	if info.UsedTokens != 0 {
		t.Errorf("after reset UsedTokens = %d, want 0", info.UsedTokens)
	}
}

func TestWindow_AddText(t *testing.T) {
	w := NewWindow(100000, "test")

	text := "Hello, this is some sample text."
	tokens := w.AddText(text)

	if tokens <= 0 {
		t.Error("AddText should return positive tokens")
	}

	info := w.Info()
	if info.UsedTokens != tokens {
		t.Errorf("UsedTokens = %d, want %d", info.UsedTokens, tokens)
	}
}

func TestWindow_CanFit(t *testing.T) {
	w := NewWindow(1000, "test")
	w.Add(900)

	if !w.CanFit(50) {
		t.Error("should fit 50 tokens with 100 remaining")
	}

	if !w.CanFit(100) {
		t.Error("should fit 100 tokens with 100 remaining")
	}

	if w.CanFit(101) {
		t.Error("should not fit 101 tokens with 100 remaining")
	}
}

func TestWindow_Warnings(t *testing.T) {
	w := NewWindow(50000, "test")

	// Initially OK
	info := w.Info()
	if info.ShouldWarn() {
		t.Error("should not warn with full context")
	}
	if info.ShouldBlock() {
		t.Error("should not block with full context")
	}
	if info.Status() != "ok" {
		t.Errorf("status = %s, want ok", info.Status())
	}

	// Use most of the context
	w.Add(30000)
	info = w.Info()
	if !info.ShouldWarn() {
		t.Error("should warn with 20000 remaining")
	}
	if info.ShouldBlock() {
		t.Error("should not block with 20000 remaining")
	}
	if info.Status() != "warning" {
		t.Errorf("status = %s, want warning", info.Status())
	}

	// Use almost all
	w.Add(18000)
	info = w.Info()
	if !info.ShouldWarn() {
		t.Error("should warn with 2000 remaining")
	}
	if !info.ShouldBlock() {
		t.Error("should block with 2000 remaining")
	}
	if info.Status() != "critical" {
		t.Errorf("status = %s, want critical", info.Status())
	}
}

func TestNewWindowForModel(t *testing.T) {
	tests := []struct {
		model      string
		wantTokens int
		wantSource string
	}{
		{"claude-3-opus", 200000, "model"},
		{"gpt-4-turbo", 128000, "model"},
		{"gpt-4-turbo-preview", 128000, "model"}, // Prefix match
		{"unknown-model", DefaultContextWindow, "default"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			w := NewWindowForModel(tt.model)
			info := w.Info()

			if info.TotalTokens != tt.wantTokens {
				t.Errorf("TotalTokens = %d, want %d", info.TotalTokens, tt.wantTokens)
			}
			if info.Source != tt.wantSource {
				t.Errorf("Source = %s, want %s", info.Source, tt.wantSource)
			}
		})
	}
}

func TestGetModelContextWindow(t *testing.T) {
	tokens, ok := GetModelContextWindow("claude-3-opus")
	if !ok {
		t.Error("expected to find claude-3-opus")
	}
	if tokens != 200000 {
		t.Errorf("tokens = %d, want 200000", tokens)
	}

	_, ok = GetModelContextWindow("unknown-model")
	if ok {
		t.Error("expected to not find unknown-model")
	}
}

func TestTruncator_NoTruncationNeeded(t *testing.T) {
	truncator := NewTruncator(TruncateOldest, 10000)

	messages := []Message{
		{Role: "system", Content: "System prompt", Tokens: 100},
		{Role: "user", Content: "Hello", Tokens: 10},
		{Role: "assistant", Content: "Hi there!", Tokens: 20},
	}

	result, tr := truncator.Truncate(messages)

	if tr.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0", tr.RemovedCount)
	}
	if len(result) != len(messages) {
		t.Errorf("len(result) = %d, want %d", len(result), len(messages))
	}
}

func TestTruncator_TruncateOldest(t *testing.T) {
	truncator := NewTruncator(TruncateOldest, 200)
	truncator.SetKeepFirst(1)
	truncator.SetKeepLast(1)

	messages := []Message{
		{Role: "system", Content: "System", Tokens: 50},
		{Role: "user", Content: "First", Tokens: 50},
		{Role: "assistant", Content: "Response 1", Tokens: 50},
		{Role: "user", Content: "Second", Tokens: 50},
		{Role: "assistant", Content: "Response 2", Tokens: 50},
		{Role: "user", Content: "Last", Tokens: 50},
	}

	result, tr := truncator.Truncate(messages)

	if tr.RemovedCount == 0 {
		t.Error("expected some messages to be removed")
	}

	// First and last should be preserved
	if result[0].Content != "System" {
		t.Error("system message should be first")
	}
	if result[len(result)-1].Content != "Last" {
		t.Error("last message should be preserved")
	}
}

func TestTruncator_PinnedMessages(t *testing.T) {
	truncator := NewTruncator(TruncateOldest, 100)
	truncator.SetKeepFirst(0)
	truncator.SetKeepLast(0)

	messages := []Message{
		{Role: "user", Content: "First", Tokens: 50},
		{Role: "user", Content: "Pinned", Tokens: 50, Pinned: true},
		{Role: "user", Content: "Third", Tokens: 50},
	}

	result, _ := truncator.Truncate(messages)

	// Pinned message should be preserved
	hasPinned := false
	for _, msg := range result {
		if msg.Content == "Pinned" {
			hasPinned = true
			break
		}
	}

	if !hasPinned {
		t.Error("pinned message should be preserved")
	}
}

func TestTruncator_TruncateMiddle(t *testing.T) {
	truncator := NewTruncator(TruncateMiddle, 150)
	truncator.SetKeepFirst(1)
	truncator.SetKeepLast(1)

	messages := []Message{
		{Role: "system", Content: "System", Tokens: 50},
		{Role: "user", Content: "Middle 1", Tokens: 50},
		{Role: "assistant", Content: "Middle 2", Tokens: 50},
		{Role: "user", Content: "Last", Tokens: 50},
	}

	result, tr := truncator.Truncate(messages)

	if tr.RemovedCount == 0 {
		t.Error("expected some messages to be removed")
	}

	// First and last should be preserved
	if result[0].Content != "System" {
		t.Error("system message should be first")
	}
	if result[len(result)-1].Content != "Last" {
		t.Error("last message should be preserved")
	}
}

func TestWindowInfo_String(t *testing.T) {
	info := &WindowInfo{
		TotalTokens:     100000,
		UsedTokens:      50000,
		RemainingTokens: 50000,
		UsedPercent:     50.0,
		Source:          "model",
	}

	str := info.String()
	if !strings.Contains(str, "50000") {
		t.Error("string should contain used tokens")
	}
	if !strings.Contains(str, "100000") {
		t.Error("string should contain total tokens")
	}
	if !strings.Contains(str, "ok") {
		t.Error("string should contain status")
	}
}
