package usage

import (
	"testing"
	"time"
)

func TestUsage_Total(t *testing.T) {
	u := &Usage{
		InputTokens:      100,
		OutputTokens:     200,
		CacheReadTokens:  50,
		CacheWriteTokens: 25,
	}

	if u.Total() != 375 {
		t.Errorf("Total() = %d, want 375", u.Total())
	}
}

func TestUsage_Add(t *testing.T) {
	u1 := &Usage{InputTokens: 100, OutputTokens: 200}
	u2 := &Usage{InputTokens: 50, OutputTokens: 75}

	u1.Add(u2)

	if u1.InputTokens != 150 {
		t.Errorf("InputTokens = %d, want 150", u1.InputTokens)
	}
	if u1.OutputTokens != 275 {
		t.Errorf("OutputTokens = %d, want 275", u1.OutputTokens)
	}
}

func TestUsage_AddNil(t *testing.T) {
	u := &Usage{InputTokens: 100}
	u.Add(nil)
	if u.InputTokens != 100 {
		t.Error("adding nil should not change usage")
	}
}

func TestCost_Estimate(t *testing.T) {
	cost := &Cost{
		Input:      3.0,  // $3 per million
		Output:     15.0, // $15 per million
		CacheRead:  0.3,
		CacheWrite: 3.75,
	}

	usage := &Usage{
		InputTokens:     1000,
		OutputTokens:    500,
		CacheReadTokens: 100,
	}

	estimated := cost.Estimate(usage)
	// (1000 * 3 + 500 * 15 + 100 * 0.3) / 1_000_000
	// = (3000 + 7500 + 30) / 1_000_000
	// = 10530 / 1_000_000 = 0.01053
	expected := 0.01053

	if diff := estimated - expected; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("Estimate() = %f, want %f", estimated, expected)
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		count int64
		want  string
	}{
		{0, "0"},
		{-10, "0"},
		{500, "500"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10k"},
		{15000, "15k"},
		{100000, "100k"},
		{1000000, "1.0m"},
		{1500000, "1.5m"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatTokenCount(tt.count)
			if got != tt.want {
				t.Errorf("FormatTokenCount(%d) = %q, want %q", tt.count, got, tt.want)
			}
		})
	}
}

func TestFormatUSD(t *testing.T) {
	tests := []struct {
		amount float64
		want   string
	}{
		{0, ""},
		{-1, ""},
		{0.001, "$0.0010"},
		{0.0099, "$0.0099"},
		{0.0123, "$0.01"}, // >= 0.01 uses 2 decimal places
		{0.12, "$0.12"},
		{1.5, "$1.50"},
		{10.99, "$10.99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatUSD(tt.amount)
			if got != tt.want {
				t.Errorf("FormatUSD(%f) = %q, want %q", tt.amount, got, tt.want)
			}
		})
	}
}

func TestTracker_Record(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig())

	tracker.Record(Record{
		Provider: "anthropic",
		Model:    "claude-3-sonnet",
		UserID:   "user1",
		Usage:    Usage{InputTokens: 100, OutputTokens: 200},
	})

	totals := tracker.GetTotals("anthropic", "claude-3-sonnet")
	if totals == nil {
		t.Fatal("expected totals")
	}
	if totals.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", totals.InputTokens)
	}

	userTotals := tracker.GetUserTotals("user1")
	if userTotals == nil {
		t.Fatal("expected user totals")
	}
	if userTotals.Total() != 300 {
		t.Errorf("user total = %d, want 300", userTotals.Total())
	}
}

func TestTracker_GetRecentRecords(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig())

	for i := 0; i < 5; i++ {
		tracker.Record(Record{
			ID:       string(rune('A' + i)),
			Provider: "test",
			Model:    "test",
			Usage:    Usage{InputTokens: int64(i * 100)},
		})
	}

	records := tracker.GetRecentRecords(3)
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
	// Should be most recent
	if records[0].ID != "C" {
		t.Errorf("first record ID = %s, want C", records[0].ID)
	}
}

func TestTracker_GetSummary(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig())

	tracker.Record(Record{Provider: "anthropic", Model: "claude", Usage: Usage{InputTokens: 100}})
	tracker.Record(Record{Provider: "anthropic", Model: "claude", Usage: Usage{InputTokens: 200}})
	tracker.Record(Record{Provider: "openai", Model: "gpt-4", Usage: Usage{InputTokens: 50}})

	summary := tracker.GetSummary()

	if len(summary) != 2 {
		t.Errorf("expected 2 models in summary, got %d", len(summary))
	}

	claudeUsage := summary["anthropic:claude"]
	if claudeUsage == nil || claudeUsage.InputTokens != 300 {
		t.Error("claude usage incorrect")
	}
}

func TestTracker_PruneOld(t *testing.T) {
	config := TrackerConfig{
		MaxAge:   100 * time.Millisecond,
		MaxCount: 1000,
	}
	tracker := NewTracker(config)

	tracker.Record(Record{
		Provider:  "test",
		Model:     "test",
		Usage:     Usage{InputTokens: 100},
		Timestamp: time.Now().Add(-200 * time.Millisecond),
	})

	// This should trigger pruning
	tracker.Record(Record{
		Provider: "test",
		Model:    "test",
		Usage:    Usage{InputTokens: 50},
	})

	records := tracker.GetRecentRecords(100)
	if len(records) != 1 {
		t.Errorf("expected 1 record after pruning, got %d", len(records))
	}
}

func TestFormatUsage(t *testing.T) {
	u := &Usage{InputTokens: 1500, OutputTokens: 500}
	formatted := FormatUsage(u)
	if formatted != "2.0k tokens" {
		t.Errorf("FormatUsage() = %q", formatted)
	}
}

func TestFormatUsageDetailed(t *testing.T) {
	u := &Usage{
		InputTokens:  1000,
		OutputTokens: 500,
	}
	formatted := FormatUsageDetailed(u)
	if formatted != "1.5k (in: 1.0k, out: 500)" {
		t.Errorf("FormatUsageDetailed() = %q", formatted)
	}
}

func TestFormatUsageNil(t *testing.T) {
	if FormatUsage(nil) != "0 tokens" {
		t.Error("nil usage should format as '0 tokens'")
	}
	if FormatUsageDetailed(nil) != "No usage" {
		t.Error("nil usage detailed should format as 'No usage'")
	}
}
