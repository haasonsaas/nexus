package status

import (
	"math"
	"testing"
)

func TestResolveModelCostConfig(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		model     string
		wantNil   bool
		wantInput float64
	}{
		{
			name:     "empty provider",
			provider: "",
			model:    "claude-sonnet-4-20250514",
			wantNil:  true,
		},
		{
			name:     "empty model",
			provider: "anthropic",
			model:    "",
			wantNil:  true,
		},
		{
			name:      "anthropic sonnet exact match",
			provider:  "anthropic",
			model:     "claude-3-5-sonnet-20241022",
			wantNil:   false,
			wantInput: 3.0,
		},
		{
			name:      "anthropic sonnet pattern",
			provider:  "anthropic",
			model:     "claude-sonnet-4-20250514",
			wantNil:   false,
			wantInput: 3.0,
		},
		{
			name:      "anthropic haiku 3.5",
			provider:  "anthropic",
			model:     "claude-3-5-haiku-20241022",
			wantNil:   false,
			wantInput: 1.0,
		},
		{
			name:      "anthropic opus",
			provider:  "anthropic",
			model:     "claude-3-opus-20240229",
			wantNil:   false,
			wantInput: 15.0,
		},
		{
			name:      "anthropic opus pattern",
			provider:  "anthropic",
			model:     "claude-opus-4-20250514",
			wantNil:   false,
			wantInput: 15.0,
		},
		{
			name:      "openai gpt-4o",
			provider:  "openai",
			model:     "gpt-4o",
			wantNil:   false,
			wantInput: 2.50,
		},
		{
			name:      "openai gpt-4o-mini",
			provider:  "openai",
			model:     "gpt-4o-mini",
			wantNil:   false,
			wantInput: 0.15,
		},
		{
			name:      "openai o1",
			provider:  "openai",
			model:     "o1",
			wantNil:   false,
			wantInput: 15.0,
		},
		{
			name:      "google gemini 1.5 pro",
			provider:  "google",
			model:     "gemini-1.5-pro",
			wantNil:   false,
			wantInput: 1.25,
		},
		{
			name:      "google gemini 2.0 flash",
			provider:  "google",
			model:     "gemini-2.0-flash-latest",
			wantNil:   false,
			wantInput: 0.10,
		},
		{
			name:     "unknown provider",
			provider: "unknown",
			model:    "some-model",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveModelCostConfig(tt.provider, tt.model, nil)
			if tt.wantNil {
				if result != nil {
					t.Errorf("ResolveModelCostConfig(%q, %q) = %+v, want nil",
						tt.provider, tt.model, result)
				}
				return
			}
			if result == nil {
				t.Errorf("ResolveModelCostConfig(%q, %q) = nil, want non-nil",
					tt.provider, tt.model)
				return
			}
			if result.InputPer1M != tt.wantInput {
				t.Errorf("ResolveModelCostConfig(%q, %q).InputPer1M = %f, want %f",
					tt.provider, tt.model, result.InputPer1M, tt.wantInput)
			}
		})
	}
}

func TestEstimateUsageCost(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		output   int
		cost     *ModelCostConfig
		expected float64
	}{
		{
			name:     "nil cost",
			input:    1000,
			output:   500,
			cost:     nil,
			expected: 0,
		},
		{
			name:   "sonnet pricing",
			input:  1000,
			output: 500,
			cost: &ModelCostConfig{
				InputPer1M:  3.0,
				OutputPer1M: 15.0,
			},
			// (1000 * 3.0 + 500 * 15.0) / 1_000_000 = 10500 / 1_000_000 = 0.0105
			expected: 0.0105,
		},
		{
			name:   "large usage",
			input:  100000,
			output: 50000,
			cost: &ModelCostConfig{
				InputPer1M:  3.0,
				OutputPer1M: 15.0,
			},
			// (100000 * 3.0 + 50000 * 15.0) / 1_000_000 = 1050000 / 1_000_000 = 1.05
			expected: 1.05,
		},
		{
			name:   "zero usage",
			input:  0,
			output: 0,
			cost: &ModelCostConfig{
				InputPer1M:  3.0,
				OutputPer1M: 15.0,
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateUsageCost(tt.input, tt.output, tt.cost)
			if math.Abs(result-tt.expected) > 0.0001 {
				t.Errorf("EstimateUsageCost(%d, %d, %+v) = %f, want %f",
					tt.input, tt.output, tt.cost, result, tt.expected)
			}
		})
	}
}

func TestEstimateUsageCostWithCache(t *testing.T) {
	cost := &ModelCostConfig{
		InputPer1M:       3.0,
		OutputPer1M:      15.0,
		CachedInputPer1M: 0.30,
	}

	result := EstimateUsageCostWithCache(1000, 500, 5000, cost)
	// (1000 * 3.0 + 500 * 15.0 + 5000 * 0.30) / 1_000_000
	// = (3000 + 7500 + 1500) / 1_000_000
	// = 12000 / 1_000_000 = 0.012
	expected := 0.012

	if math.Abs(result-expected) > 0.0001 {
		t.Errorf("EstimateUsageCostWithCache() = %f, want %f", result, expected)
	}
}

func TestFormatUSD(t *testing.T) {
	tests := []struct {
		amount   float64
		expected string
	}{
		{0, ""},
		{-1, ""},
		{math.NaN(), ""},
		{math.Inf(1), ""},
		{0.001, "$0.0010"},
		{0.005, "$0.0050"},
		{0.01, "$0.01"},
		{0.05, "$0.05"},
		{0.99, "$0.99"},
		{1.0, "$1.00"},
		{1.23, "$1.23"},
		{10.5, "$10.50"},
		{100.00, "$100.00"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatUSD(tt.amount)
			if result != tt.expected {
				t.Errorf("FormatUSD(%f) = %q, want %q", tt.amount, result, tt.expected)
			}
		})
	}
}

func TestComputeCostSummary(t *testing.T) {
	cost := &ModelCostConfig{
		InputPer1M:       3.0,
		OutputPer1M:      15.0,
		CachedInputPer1M: 0.30,
	}

	summary := ComputeCostSummary(10000, 5000, 50000, cost)

	if summary == nil {
		t.Fatal("ComputeCostSummary() returned nil")
	}

	if summary.InputTokens != 10000 {
		t.Errorf("InputTokens = %d, want 10000", summary.InputTokens)
	}
	if summary.OutputTokens != 5000 {
		t.Errorf("OutputTokens = %d, want 5000", summary.OutputTokens)
	}
	if summary.CachedTokens != 50000 {
		t.Errorf("CachedTokens = %d, want 50000", summary.CachedTokens)
	}

	// Input: 10000 * 3.0 / 1M = 0.03
	if math.Abs(summary.InputCost-0.03) > 0.0001 {
		t.Errorf("InputCost = %f, want 0.03", summary.InputCost)
	}

	// Output: 5000 * 15.0 / 1M = 0.075
	if math.Abs(summary.OutputCost-0.075) > 0.0001 {
		t.Errorf("OutputCost = %f, want 0.075", summary.OutputCost)
	}

	// Cached: 50000 * 0.30 / 1M = 0.015
	if math.Abs(summary.CachedCost-0.015) > 0.0001 {
		t.Errorf("CachedCost = %f, want 0.015", summary.CachedCost)
	}

	// Total: 0.03 + 0.075 + 0.015 = 0.12
	if math.Abs(summary.TotalCost-0.12) > 0.0001 {
		t.Errorf("TotalCost = %f, want 0.12", summary.TotalCost)
	}
}

func TestComputeCostSummary_NilCost(t *testing.T) {
	summary := ComputeCostSummary(10000, 5000, 0, nil)

	if summary == nil {
		t.Fatal("ComputeCostSummary() returned nil")
	}

	if summary.InputTokens != 10000 {
		t.Errorf("InputTokens = %d, want 10000", summary.InputTokens)
	}
	if summary.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0", summary.TotalCost)
	}
}

func TestFormatCostSummary(t *testing.T) {
	tests := []struct {
		name     string
		summary  *CostSummary
		contains []string
		empty    bool
	}{
		{
			name:    "nil summary",
			summary: nil,
			empty:   true,
		},
		{
			name: "zero cost",
			summary: &CostSummary{
				TotalCost: 0,
			},
			empty: true,
		},
		{
			name: "valid cost",
			summary: &CostSummary{
				TotalCost:  0.12,
				InputCost:  0.03,
				OutputCost: 0.09,
			},
			contains: []string{"Cost:", "$0.12", "$0.03", "$0.09"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCostSummary(tt.summary)
			if tt.empty && result != "" {
				t.Errorf("FormatCostSummary() = %q, expected empty", result)
			}
			if !tt.empty && result == "" {
				t.Errorf("FormatCostSummary() = empty, expected content")
			}
			for _, s := range tt.contains {
				if result != "" && !containsSubstring(result, s) {
					t.Errorf("FormatCostSummary() = %q, expected to contain %q", result, s)
				}
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInMiddle(s, substr)))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDefaultModelCosts(t *testing.T) {
	// Verify default costs are populated correctly
	if len(DefaultModelCosts) == 0 {
		t.Error("DefaultModelCosts is empty")
	}

	// Check Anthropic models exist
	anthropic, ok := DefaultModelCosts["anthropic"]
	if !ok {
		t.Error("DefaultModelCosts missing anthropic provider")
	}
	if _, ok := anthropic["claude-3-5-sonnet-20241022"]; !ok {
		t.Error("DefaultModelCosts missing claude-3-5-sonnet-20241022")
	}

	// Check OpenAI models exist
	openai, ok := DefaultModelCosts["openai"]
	if !ok {
		t.Error("DefaultModelCosts missing openai provider")
	}
	if _, ok := openai["gpt-4o"]; !ok {
		t.Error("DefaultModelCosts missing gpt-4o")
	}

	// Check Google models exist
	google, ok := DefaultModelCosts["google"]
	if !ok {
		t.Error("DefaultModelCosts missing google provider")
	}
	if _, ok := google["gemini-1.5-pro"]; !ok {
		t.Error("DefaultModelCosts missing gemini-1.5-pro")
	}
}
