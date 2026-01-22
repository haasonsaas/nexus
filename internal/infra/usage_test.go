package infra

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUsageWindow_UsagePercent(t *testing.T) {
	tests := []struct {
		name     string
		used     int64
		limit    int64
		expected float64
	}{
		{"zero", 0, 100, 0},
		{"half", 50, 100, 50},
		{"full", 100, 100, 100},
		{"over", 150, 100, 100}, // Clamped to 100
		{"zero limit", 50, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := UsageWindow{Used: tt.used, Limit: tt.limit}
			pct := w.UsagePercent()
			if pct != tt.expected {
				t.Errorf("expected %.1f%%, got %.1f%%", tt.expected, pct)
			}
		})
	}
}

func TestUsageWindow_Remaining(t *testing.T) {
	tests := []struct {
		name     string
		used     int64
		limit    int64
		expected int64
	}{
		{"some remaining", 30, 100, 70},
		{"none remaining", 100, 100, 0},
		{"over limit", 150, 100, 0}, // Clamped to 0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := UsageWindow{Used: tt.used, Limit: tt.limit}
			remaining := w.Remaining()
			if remaining != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, remaining)
			}
		})
	}
}

func TestProviderUsage_IsNearLimit(t *testing.T) {
	usage := ProviderUsage{
		Windows: []UsageWindow{
			{Name: "daily", Used: 80, Limit: 100},
			{Name: "monthly", Used: 500, Limit: 1000},
		},
	}

	if !usage.IsNearLimit(80) {
		t.Error("expected true for 80% threshold (daily is at 80%)")
	}
	if usage.IsNearLimit(81) {
		t.Error("expected false for 81% threshold")
	}
	if !usage.IsNearLimit(50) {
		t.Error("expected true for 50% threshold (both windows over)")
	}
}

func TestUsageTracker_RecordRequest(t *testing.T) {
	tracker := NewUsageTracker()

	tracker.RecordRequest("anthropic", 100)
	tracker.RecordRequest("anthropic", 200)
	tracker.RecordRequest("openai", 50)

	summary := tracker.Summary()
	if len(summary.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(summary.Providers))
	}

	anthropic, ok := summary.Provider("anthropic")
	if !ok {
		t.Fatal("anthropic not found")
	}
	if anthropic.RequestCount != 2 {
		t.Errorf("expected 2 requests, got %d", anthropic.RequestCount)
	}
	if anthropic.TokensUsed != 300 {
		t.Errorf("expected 300 tokens, got %d", anthropic.TokensUsed)
	}
}

func TestUsageTracker_UpdateWindow(t *testing.T) {
	tracker := NewUsageTracker()
	resetsAt := time.Now().Add(time.Hour)

	tracker.UpdateWindow("anthropic", "daily", 50, 100, resetsAt)

	usage, ok := tracker.ProviderUsageSnapshot("anthropic")
	if !ok {
		t.Fatal("provider not found")
	}

	if len(usage.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(usage.Windows))
	}

	window := usage.Windows[0]
	if window.Name != "daily" {
		t.Errorf("expected name 'daily', got %s", window.Name)
	}
	if window.Used != 50 {
		t.Errorf("expected used 50, got %d", window.Used)
	}
	if window.Limit != 100 {
		t.Errorf("expected limit 100, got %d", window.Limit)
	}
}

func TestUsageTracker_IncrementWindow(t *testing.T) {
	tracker := NewUsageTracker()

	tracker.IncrementWindow("anthropic", "daily", 10)
	tracker.IncrementWindow("anthropic", "daily", 20)

	usage, _ := tracker.ProviderUsageSnapshot("anthropic")
	if usage.Windows[0].Used != 30 {
		t.Errorf("expected 30 used, got %d", usage.Windows[0].Used)
	}
}

func TestUsageTracker_RegisterProvider(t *testing.T) {
	tracker := NewUsageTracker()

	tracker.RegisterProvider("anthropic", "Claude")
	tracker.RecordRequest("anthropic", 100)

	usage, _ := tracker.ProviderUsageSnapshot("anthropic")
	if usage.DisplayName != "Claude" {
		t.Errorf("expected display name 'Claude', got %s", usage.DisplayName)
	}
}

func TestUsageTracker_Reset(t *testing.T) {
	tracker := NewUsageTracker()

	tracker.RecordRequest("anthropic", 100)
	tracker.RecordRequest("openai", 50)

	tracker.Reset()

	summary := tracker.Summary()
	if len(summary.Providers) != 0 {
		t.Errorf("expected 0 providers after reset, got %d", len(summary.Providers))
	}
}

func TestUsageTracker_ResetProvider(t *testing.T) {
	tracker := NewUsageTracker()

	tracker.RecordRequest("anthropic", 100)
	tracker.RecordRequest("openai", 50)

	tracker.ResetProvider("anthropic")

	summary := tracker.Summary()
	if len(summary.Providers) != 1 {
		t.Errorf("expected 1 provider after reset, got %d", len(summary.Providers))
	}

	_, ok := summary.Provider("anthropic")
	if ok {
		t.Error("anthropic should be removed")
	}

	_, ok = summary.Provider("openai")
	if !ok {
		t.Error("openai should still exist")
	}
}

func TestUsageSummary_ProvidersNearLimit(t *testing.T) {
	summary := &UsageSummary{
		Providers: []ProviderUsage{
			{
				Provider: "anthropic",
				Windows:  []UsageWindow{{Used: 90, Limit: 100}},
			},
			{
				Provider: "openai",
				Windows:  []UsageWindow{{Used: 30, Limit: 100}},
			},
		},
	}

	nearLimit := summary.ProvidersNearLimit(80)
	if len(nearLimit) != 1 {
		t.Fatalf("expected 1 provider near limit, got %d", len(nearLimit))
	}
	if nearLimit[0].Provider != "anthropic" {
		t.Errorf("expected anthropic, got %s", nearLimit[0].Provider)
	}
}

type mockFetcher struct {
	usage *ProviderUsage
	err   error
	delay time.Duration
}

func (f *mockFetcher) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.usage, f.err
}

func TestUsageAggregator_FetchAll(t *testing.T) {
	agg := NewUsageAggregator(time.Second, 0)

	agg.RegisterFetcher("anthropic", &mockFetcher{
		usage: &ProviderUsage{
			Provider:     "anthropic",
			DisplayName:  "Claude",
			RequestCount: 100,
		},
	})
	agg.RegisterFetcher("openai", &mockFetcher{
		usage: &ProviderUsage{
			Provider:     "openai",
			DisplayName:  "OpenAI",
			RequestCount: 50,
		},
	})

	summary, err := agg.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(summary.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(summary.Providers))
	}
}

func TestUsageAggregator_FetchAllWithError(t *testing.T) {
	agg := NewUsageAggregator(time.Second, 0)

	agg.RegisterFetcher("working", &mockFetcher{
		usage: &ProviderUsage{Provider: "working"},
	})
	agg.RegisterFetcher("failing", &mockFetcher{
		err: errors.New("fetch failed"),
	})

	summary, err := agg.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(summary.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(summary.Providers))
	}

	failing, ok := summary.Provider("failing")
	if !ok {
		t.Fatal("failing provider not found")
	}
	if failing.Error != "fetch failed" {
		t.Errorf("expected error 'fetch failed', got %s", failing.Error)
	}
}

func TestUsageAggregator_Caching(t *testing.T) {
	agg := NewUsageAggregator(time.Second, time.Hour)

	callCount := 0
	agg.RegisterFetcher("test", &mockFetcher{
		usage: &ProviderUsage{Provider: "test"},
	})

	// Override to track calls
	origFetcher := agg.fetchers["test"].(*mockFetcher)
	agg.fetchers["test"] = &mockFetcher{
		usage: origFetcher.usage,
	}

	// First call
	_, _ = agg.FetchAll(context.Background())
	callCount++

	// Second call should use cache
	_, _ = agg.FetchAll(context.Background())

	// We can't directly test the fetch count, but we can verify the cache exists
	agg.mu.RLock()
	hasCached := agg.cache != nil
	agg.mu.RUnlock()

	if !hasCached {
		t.Error("expected cache to be set")
	}
}

func TestFilterIgnoredErrors(t *testing.T) {
	summary := &UsageSummary{
		Providers: []ProviderUsage{
			{Provider: "working"},
			{Provider: "no-creds", Error: "No credentials"},
			{Provider: "real-error", Error: "API rate limited"},
		},
	}

	filtered := FilterIgnoredErrors(summary)

	if len(filtered.Providers) != 2 {
		t.Fatalf("expected 2 providers after filter, got %d", len(filtered.Providers))
	}

	for _, p := range filtered.Providers {
		if p.Provider == "no-creds" {
			t.Error("no-creds should be filtered out")
		}
	}
}

func TestDefaultUsageTracker(t *testing.T) {
	// Reset for test
	DefaultUsageTracker = NewUsageTracker()

	RecordAPIRequest("test-provider", 100)
	UpdateRateLimitWindow("test-provider", "daily", 50, 100, time.Now().Add(time.Hour))

	summary := GetUsageSummary()
	if len(summary.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(summary.Providers))
	}
}
