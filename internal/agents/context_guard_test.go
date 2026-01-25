package agents

import (
	"testing"
)

// mockModelsConfig implements ModelsConfigProvider for testing.
type mockModelsConfig struct {
	contextWindows map[string]int
}

func (m *mockModelsConfig) GetModelContextWindow(provider, modelID string) int {
	key := provider + "/" + modelID
	return m.contextWindows[key]
}

// mockAgentConfig implements AgentConfigProvider for testing.
type mockAgentConfig struct {
	defaultContextTokens int
}

func (m *mockAgentConfig) GetDefaultContextTokens() int {
	return m.defaultContextTokens
}

func TestNormalizePositiveInt(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected int
	}{
		{"positive integer", 100.0, 100},
		{"positive with decimal", 100.9, 100},
		{"zero", 0, 0},
		{"negative", -100.0, 0},
		{"small positive", 0.5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePositiveInt(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePositiveInt(%v) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestResolveContextWindowInfo_Priority(t *testing.T) {
	t.Run("model takes priority over all", func(t *testing.T) {
		modelsConfig := &mockModelsConfig{
			contextWindows: map[string]int{"anthropic/claude-3": 100000},
		}
		agentConfig := &mockAgentConfig{defaultContextTokens: 50000}

		info := ResolveContextWindowInfo(
			modelsConfig,
			agentConfig,
			"anthropic",
			"claude-3",
			200000, // model context window
			30000,  // default
		)

		if info.Tokens != 200000 {
			t.Errorf("expected 200000 tokens, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceModel {
			t.Errorf("expected source 'model', got %q", info.Source)
		}
	})

	t.Run("modelsConfig takes priority over agentContextTokens", func(t *testing.T) {
		modelsConfig := &mockModelsConfig{
			contextWindows: map[string]int{"anthropic/claude-3": 100000},
		}
		agentConfig := &mockAgentConfig{defaultContextTokens: 50000}

		info := ResolveContextWindowInfo(
			modelsConfig,
			agentConfig,
			"anthropic",
			"claude-3",
			0,     // no model context window
			30000, // default
		)

		if info.Tokens != 100000 {
			t.Errorf("expected 100000 tokens, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceModelsConfig {
			t.Errorf("expected source 'modelsConfig', got %q", info.Source)
		}
	})

	t.Run("agentContextTokens takes priority over default", func(t *testing.T) {
		modelsConfig := &mockModelsConfig{
			contextWindows: map[string]int{}, // no match
		}
		agentConfig := &mockAgentConfig{defaultContextTokens: 50000}

		info := ResolveContextWindowInfo(
			modelsConfig,
			agentConfig,
			"anthropic",
			"claude-3",
			0,     // no model context window
			30000, // default
		)

		if info.Tokens != 50000 {
			t.Errorf("expected 50000 tokens, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceAgentContextTokens {
			t.Errorf("expected source 'agentContextTokens', got %q", info.Source)
		}
	})

	t.Run("default used when nothing else configured", func(t *testing.T) {
		modelsConfig := &mockModelsConfig{
			contextWindows: map[string]int{},
		}
		agentConfig := &mockAgentConfig{defaultContextTokens: 0}

		info := ResolveContextWindowInfo(
			modelsConfig,
			agentConfig,
			"anthropic",
			"claude-3",
			0,     // no model context window
			30000, // default
		)

		if info.Tokens != 30000 {
			t.Errorf("expected 30000 tokens, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceDefault {
			t.Errorf("expected source 'default', got %q", info.Source)
		}
	})
}

func TestResolveContextWindowInfo_NilProviders(t *testing.T) {
	t.Run("nil modelsConfig", func(t *testing.T) {
		agentConfig := &mockAgentConfig{defaultContextTokens: 50000}

		info := ResolveContextWindowInfo(
			nil,
			agentConfig,
			"anthropic",
			"claude-3",
			0,     // no model context window
			30000, // default
		)

		if info.Tokens != 50000 {
			t.Errorf("expected 50000 tokens from agentConfig, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceAgentContextTokens {
			t.Errorf("expected source 'agentContextTokens', got %q", info.Source)
		}
	})

	t.Run("nil agentConfig", func(t *testing.T) {
		modelsConfig := &mockModelsConfig{
			contextWindows: map[string]int{"anthropic/claude-3": 100000},
		}

		info := ResolveContextWindowInfo(
			modelsConfig,
			nil,
			"anthropic",
			"claude-3",
			0,     // no model context window
			30000, // default
		)

		if info.Tokens != 100000 {
			t.Errorf("expected 100000 tokens from modelsConfig, got %d", info.Tokens)
		}
	})

	t.Run("both nil providers", func(t *testing.T) {
		info := ResolveContextWindowInfo(
			nil,
			nil,
			"anthropic",
			"claude-3",
			0,     // no model context window
			30000, // default
		)

		if info.Tokens != 30000 {
			t.Errorf("expected 30000 tokens from default, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceDefault {
			t.Errorf("expected source 'default', got %q", info.Source)
		}
	})
}

func TestResolveContextWindowInfo_NormalizesValues(t *testing.T) {
	t.Run("negative model context window falls through", func(t *testing.T) {
		agentConfig := &mockAgentConfig{defaultContextTokens: 50000}

		info := ResolveContextWindowInfo(
			nil,
			agentConfig,
			"anthropic",
			"claude-3",
			-100,  // negative model context window
			30000, // default
		)

		// Should fall through to agentConfig
		if info.Tokens != 50000 {
			t.Errorf("expected 50000 tokens, got %d", info.Tokens)
		}
		if info.Source != ContextWindowSourceAgentContextTokens {
			t.Errorf("expected source 'agentContextTokens', got %q", info.Source)
		}
	})

	t.Run("zero model context window falls through", func(t *testing.T) {
		info := ResolveContextWindowInfo(
			nil,
			nil,
			"anthropic",
			"claude-3",
			0,     // zero model context window
			30000, // default
		)

		if info.Tokens != 30000 {
			t.Errorf("expected 30000 tokens from default, got %d", info.Tokens)
		}
	})
}

func TestEvaluateContextWindowGuard_DefaultThresholds(t *testing.T) {
	t.Run("above warning threshold", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 50000,
			Source: ContextWindowSourceModel,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if result.ShouldWarn {
			t.Error("should not warn when above warning threshold")
		}
		if result.ShouldBlock {
			t.Error("should not block when above hard minimum")
		}
		if result.Tokens != 50000 {
			t.Errorf("expected 50000 tokens, got %d", result.Tokens)
		}
		if result.Source != ContextWindowSourceModel {
			t.Errorf("expected source 'model', got %q", result.Source)
		}
	})

	t.Run("below warning threshold but above hard min", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 20000, // between 16k and 32k
			Source: ContextWindowSourceModelsConfig,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if !result.ShouldWarn {
			t.Error("should warn when below warning threshold")
		}
		if result.ShouldBlock {
			t.Error("should not block when above hard minimum")
		}
	})

	t.Run("below hard minimum", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 10000, // below 16k
			Source: ContextWindowSourceDefault,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if !result.ShouldWarn {
			t.Error("should warn when below both thresholds")
		}
		if !result.ShouldBlock {
			t.Error("should block when below hard minimum")
		}
	})

	t.Run("exactly at warning threshold", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: ContextWindowWarnBelowTokens,
			Source: ContextWindowSourceModel,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if result.ShouldWarn {
			t.Error("should not warn at exactly warning threshold")
		}
		if result.ShouldBlock {
			t.Error("should not block at warning threshold")
		}
	})

	t.Run("exactly at hard minimum", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: ContextWindowHardMinTokens,
			Source: ContextWindowSourceModel,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if !result.ShouldWarn {
			t.Error("should warn at hard minimum (still below warn threshold)")
		}
		if result.ShouldBlock {
			t.Error("should not block at exactly hard minimum")
		}
	})
}

func TestEvaluateContextWindowGuard_CustomThresholds(t *testing.T) {
	t.Run("custom thresholds", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 5000,
			Source: ContextWindowSourceModel,
		}

		opts := &EvaluateContextWindowGuardOptions{
			WarnBelowTokens: 10000,
			HardMinTokens:   3000,
		}

		result := EvaluateContextWindowGuard(info, opts)

		if !result.ShouldWarn {
			t.Error("should warn below custom warn threshold")
		}
		if result.ShouldBlock {
			t.Error("should not block above custom hard min")
		}
	})

	t.Run("custom hard minimum triggers block", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 2000,
			Source: ContextWindowSourceModel,
		}

		opts := &EvaluateContextWindowGuardOptions{
			WarnBelowTokens: 10000,
			HardMinTokens:   3000,
		}

		result := EvaluateContextWindowGuard(info, opts)

		if !result.ShouldWarn {
			t.Error("should warn below custom warn threshold")
		}
		if !result.ShouldBlock {
			t.Error("should block below custom hard min")
		}
	})
}

func TestEvaluateContextWindowGuard_ZeroTokens(t *testing.T) {
	t.Run("zero tokens does not warn or block", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 0,
			Source: ContextWindowSourceDefault,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if result.ShouldWarn {
			t.Error("zero tokens should not warn")
		}
		if result.ShouldBlock {
			t.Error("zero tokens should not block")
		}
	})

	t.Run("negative tokens normalized to zero", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: -100,
			Source: ContextWindowSourceDefault,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if result.Tokens != 0 {
			t.Errorf("expected 0 tokens after normalization, got %d", result.Tokens)
		}
		if result.ShouldWarn {
			t.Error("negative tokens (normalized to zero) should not warn")
		}
		if result.ShouldBlock {
			t.Error("negative tokens (normalized to zero) should not block")
		}
	})
}

func TestEvaluateContextWindowGuard_EdgeCases(t *testing.T) {
	t.Run("one token below warning threshold", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: ContextWindowWarnBelowTokens - 1,
			Source: ContextWindowSourceModel,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if !result.ShouldWarn {
			t.Error("should warn at one below threshold")
		}
	})

	t.Run("one token below hard minimum", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: ContextWindowHardMinTokens - 1,
			Source: ContextWindowSourceModel,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if !result.ShouldBlock {
			t.Error("should block at one below hard minimum")
		}
	})

	t.Run("one token", func(t *testing.T) {
		info := ContextWindowInfo{
			Tokens: 1,
			Source: ContextWindowSourceModel,
		}

		result := EvaluateContextWindowGuard(info, nil)

		if !result.ShouldWarn {
			t.Error("1 token should warn")
		}
		if !result.ShouldBlock {
			t.Error("1 token should block")
		}
	})
}

func TestConstants(t *testing.T) {
	if ContextWindowHardMinTokens != 16000 {
		t.Errorf("expected hard min 16000, got %d", ContextWindowHardMinTokens)
	}
	if ContextWindowWarnBelowTokens != 32000 {
		t.Errorf("expected warn below 32000, got %d", ContextWindowWarnBelowTokens)
	}
	if ContextWindowHardMinTokens >= ContextWindowWarnBelowTokens {
		t.Error("hard min should be less than warn threshold")
	}
}

func TestContextWindowSourceConstants(t *testing.T) {
	sources := []ContextWindowSource{
		ContextWindowSourceModel,
		ContextWindowSourceModelsConfig,
		ContextWindowSourceAgentContextTokens,
		ContextWindowSourceDefault,
	}

	expected := []string{"model", "modelsConfig", "agentContextTokens", "default"}

	for i, src := range sources {
		if string(src) != expected[i] {
			t.Errorf("expected source %q, got %q", expected[i], src)
		}
	}
}
