package status

import (
	"fmt"
	"math"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// ModelCostConfig contains pricing information per million tokens.
type ModelCostConfig struct {
	InputPer1M       float64
	OutputPer1M      float64
	CachedInputPer1M float64
}

// DefaultModelCosts contains default pricing for common models.
// Prices are per million tokens.
var DefaultModelCosts = map[string]map[string]ModelCostConfig{
	"anthropic": {
		"claude-3-5-sonnet-20241022": {InputPer1M: 3.0, OutputPer1M: 15.0, CachedInputPer1M: 0.30},
		"claude-3-5-sonnet-latest":   {InputPer1M: 3.0, OutputPer1M: 15.0, CachedInputPer1M: 0.30},
		"claude-sonnet-4-20250514":   {InputPer1M: 3.0, OutputPer1M: 15.0, CachedInputPer1M: 0.30},
		"claude-3-5-haiku-20241022":  {InputPer1M: 1.0, OutputPer1M: 5.0, CachedInputPer1M: 0.10},
		"claude-3-5-haiku-latest":    {InputPer1M: 1.0, OutputPer1M: 5.0, CachedInputPer1M: 0.10},
		"claude-3-opus-20240229":     {InputPer1M: 15.0, OutputPer1M: 75.0, CachedInputPer1M: 1.50},
		"claude-3-opus-latest":       {InputPer1M: 15.0, OutputPer1M: 75.0, CachedInputPer1M: 1.50},
		"claude-opus-4-20250514":     {InputPer1M: 15.0, OutputPer1M: 75.0, CachedInputPer1M: 1.50},
		"claude-3-haiku-20240307":    {InputPer1M: 0.25, OutputPer1M: 1.25, CachedInputPer1M: 0.03},
	},
	"openai": {
		"gpt-4o":            {InputPer1M: 2.50, OutputPer1M: 10.0, CachedInputPer1M: 1.25},
		"gpt-4o-2024-11-20": {InputPer1M: 2.50, OutputPer1M: 10.0, CachedInputPer1M: 1.25},
		"gpt-4o-mini":       {InputPer1M: 0.15, OutputPer1M: 0.60, CachedInputPer1M: 0.075},
		"gpt-4-turbo":       {InputPer1M: 10.0, OutputPer1M: 30.0},
		"gpt-4":             {InputPer1M: 30.0, OutputPer1M: 60.0},
		"gpt-3.5-turbo":     {InputPer1M: 0.50, OutputPer1M: 1.50},
		"o1":                {InputPer1M: 15.0, OutputPer1M: 60.0, CachedInputPer1M: 7.50},
		"o1-mini":           {InputPer1M: 3.0, OutputPer1M: 12.0, CachedInputPer1M: 1.50},
		"o1-preview":        {InputPer1M: 15.0, OutputPer1M: 60.0},
	},
	"google": {
		"gemini-1.5-pro":        {InputPer1M: 1.25, OutputPer1M: 5.0},
		"gemini-1.5-pro-latest": {InputPer1M: 1.25, OutputPer1M: 5.0},
		"gemini-1.5-flash":      {InputPer1M: 0.075, OutputPer1M: 0.30},
		"gemini-2.0-flash":      {InputPer1M: 0.10, OutputPer1M: 0.40},
		"gemini-pro":            {InputPer1M: 0.50, OutputPer1M: 1.50},
	},
	"mistral": {
		"mistral-large":  {InputPer1M: 2.0, OutputPer1M: 6.0},
		"mistral-medium": {InputPer1M: 2.7, OutputPer1M: 8.1},
		"mistral-small":  {InputPer1M: 0.2, OutputPer1M: 0.6},
	},
}

// ResolveModelCostConfig looks up pricing for a model.
// It first checks the config for custom pricing, then falls back to defaults.
func ResolveModelCostConfig(provider, model string, cfg *config.Config) *ModelCostConfig {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)

	if provider == "" || model == "" {
		return nil
	}

	// Check default costs
	if providerCosts, ok := DefaultModelCosts[provider]; ok {
		if cost, ok := providerCosts[model]; ok {
			return &cost
		}

		// Try partial match for versioned models
		for modelID, cost := range providerCosts {
			if strings.HasPrefix(model, modelID) || strings.HasPrefix(modelID, model) {
				costCopy := cost
				return &costCopy
			}
		}
	}

	// Check for alias patterns
	switch provider {
	case "anthropic":
		if strings.Contains(model, "sonnet") {
			if strings.Contains(model, "3-5") || strings.Contains(model, "3.5") || strings.Contains(model, "4") {
				return &ModelCostConfig{InputPer1M: 3.0, OutputPer1M: 15.0, CachedInputPer1M: 0.30}
			}
		}
		if strings.Contains(model, "haiku") {
			if strings.Contains(model, "3-5") || strings.Contains(model, "3.5") {
				return &ModelCostConfig{InputPer1M: 1.0, OutputPer1M: 5.0, CachedInputPer1M: 0.10}
			}
			return &ModelCostConfig{InputPer1M: 0.25, OutputPer1M: 1.25, CachedInputPer1M: 0.03}
		}
		if strings.Contains(model, "opus") {
			return &ModelCostConfig{InputPer1M: 15.0, OutputPer1M: 75.0, CachedInputPer1M: 1.50}
		}

	case "openai":
		if strings.HasPrefix(model, "gpt-4o-mini") {
			return &ModelCostConfig{InputPer1M: 0.15, OutputPer1M: 0.60, CachedInputPer1M: 0.075}
		}
		if strings.HasPrefix(model, "gpt-4o") {
			return &ModelCostConfig{InputPer1M: 2.50, OutputPer1M: 10.0, CachedInputPer1M: 1.25}
		}
		if strings.HasPrefix(model, "o1-mini") {
			return &ModelCostConfig{InputPer1M: 3.0, OutputPer1M: 12.0, CachedInputPer1M: 1.50}
		}
		if strings.HasPrefix(model, "o1") {
			return &ModelCostConfig{InputPer1M: 15.0, OutputPer1M: 60.0, CachedInputPer1M: 7.50}
		}

	case "google":
		if strings.Contains(model, "gemini-2") && strings.Contains(model, "flash") {
			return &ModelCostConfig{InputPer1M: 0.10, OutputPer1M: 0.40}
		}
		if strings.Contains(model, "gemini-1.5-flash") {
			return &ModelCostConfig{InputPer1M: 0.075, OutputPer1M: 0.30}
		}
		if strings.Contains(model, "gemini-1.5-pro") {
			return &ModelCostConfig{InputPer1M: 1.25, OutputPer1M: 5.0}
		}
	}

	return nil
}

// EstimateUsageCost calculates estimated cost from token counts.
func EstimateUsageCost(input, output int, cost *ModelCostConfig) float64 {
	if cost == nil {
		return 0
	}

	inputCost := float64(input) * cost.InputPer1M
	outputCost := float64(output) * cost.OutputPer1M

	total := (inputCost + outputCost) / 1_000_000

	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0
	}

	return total
}

// EstimateUsageCostWithCache calculates cost including cached tokens.
func EstimateUsageCostWithCache(input, output, cachedInput int, cost *ModelCostConfig) float64 {
	if cost == nil {
		return 0
	}

	inputCost := float64(input) * cost.InputPer1M
	outputCost := float64(output) * cost.OutputPer1M
	cachedCost := float64(cachedInput) * cost.CachedInputPer1M

	total := (inputCost + outputCost + cachedCost) / 1_000_000

	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0
	}

	return total
}

// FormatUSD formats a cost as "$X.XX" or "$X.XXXX" for very small amounts.
func FormatUSD(amount float64) string {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return ""
	}
	if amount >= 1 {
		return fmt.Sprintf("$%.2f", amount)
	}
	if amount >= 0.01 {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("$%.4f", amount)
}

// CostSummary holds a cost summary with breakdown.
type CostSummary struct {
	TotalCost    float64
	InputCost    float64
	OutputCost   float64
	CachedCost   float64
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

// ComputeCostSummary computes a detailed cost summary.
func ComputeCostSummary(input, output, cached int, cost *ModelCostConfig) *CostSummary {
	if cost == nil {
		return &CostSummary{
			InputTokens:  input,
			OutputTokens: output,
			CachedTokens: cached,
		}
	}

	inputCost := float64(input) * cost.InputPer1M / 1_000_000
	outputCost := float64(output) * cost.OutputPer1M / 1_000_000
	cachedCost := float64(cached) * cost.CachedInputPer1M / 1_000_000

	return &CostSummary{
		TotalCost:    inputCost + outputCost + cachedCost,
		InputCost:    inputCost,
		OutputCost:   outputCost,
		CachedCost:   cachedCost,
		InputTokens:  input,
		OutputTokens: output,
		CachedTokens: cached,
	}
}

// FormatCostSummary formats a cost summary for display.
func FormatCostSummary(summary *CostSummary) string {
	if summary == nil || summary.TotalCost <= 0 {
		return ""
	}

	total := FormatUSD(summary.TotalCost)
	if total == "" {
		return ""
	}

	return fmt.Sprintf("Cost: %s (in: %s, out: %s)",
		total,
		FormatUSD(summary.InputCost),
		FormatUSD(summary.OutputCost))
}
