// Package agents provides shared agent functionality including context window management.
package agents

import (
	"math"
)

// Constants for context window thresholds.
const (
	// ContextWindowHardMinTokens is the minimum context window size below which agents should not operate.
	ContextWindowHardMinTokens = 16_000
	// ContextWindowWarnBelowTokens is the threshold below which a warning should be issued.
	ContextWindowWarnBelowTokens = 32_000
)

// ContextWindowSource indicates where the context window value was resolved from.
type ContextWindowSource string

const (
	// ContextWindowSourceModel indicates the value came from the model itself.
	ContextWindowSourceModel ContextWindowSource = "model"
	// ContextWindowSourceModelsConfig indicates the value came from models configuration.
	ContextWindowSourceModelsConfig ContextWindowSource = "modelsConfig"
	// ContextWindowSourceAgentContextTokens indicates the value came from agent context tokens config.
	ContextWindowSourceAgentContextTokens ContextWindowSource = "agentContextTokens"
	// ContextWindowSourceDefault indicates the default value was used.
	ContextWindowSourceDefault ContextWindowSource = "default"
)

// ContextWindowInfo contains resolved context window information.
type ContextWindowInfo struct {
	Tokens int                 `json:"tokens"`
	Source ContextWindowSource `json:"source"`
}

// ContextWindowGuardResult contains the result of context window evaluation.
type ContextWindowGuardResult struct {
	ContextWindowInfo
	ShouldWarn  bool `json:"should_warn"`
	ShouldBlock bool `json:"should_block"`
}

// ModelsConfigProvider is an interface for accessing models configuration.
type ModelsConfigProvider interface {
	// GetModelContextWindow returns the context window for a specific provider/model combination.
	// Returns 0 if not configured.
	GetModelContextWindow(provider, modelID string) int
}

// AgentConfigProvider is an interface for accessing agent configuration.
type AgentConfigProvider interface {
	// GetDefaultContextTokens returns the default context tokens from agent config.
	// Returns 0 if not configured.
	GetDefaultContextTokens() int
}

// normalizePositiveInt normalizes a value to a positive integer.
// Returns 0 if the value is not a positive finite number.
func normalizePositiveInt(value float64) int {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	intVal := int(math.Floor(value))
	if intVal > 0 {
		return intVal
	}
	return 0
}

// ResolveContextWindowInfo resolves the context window size from various sources.
// Priority order: model > modelsConfig > agentContextTokens > default
func ResolveContextWindowInfo(
	modelsConfig ModelsConfigProvider,
	agentConfig AgentConfigProvider,
	provider string,
	modelID string,
	modelContextWindow int,
	defaultTokens int,
) ContextWindowInfo {
	// Priority 1: Model-provided context window
	fromModel := normalizePositiveInt(float64(modelContextWindow))
	if fromModel > 0 {
		return ContextWindowInfo{
			Tokens: fromModel,
			Source: ContextWindowSourceModel,
		}
	}

	// Priority 2: Models config
	if modelsConfig != nil {
		fromModelsConfig := normalizePositiveInt(float64(modelsConfig.GetModelContextWindow(provider, modelID)))
		if fromModelsConfig > 0 {
			return ContextWindowInfo{
				Tokens: fromModelsConfig,
				Source: ContextWindowSourceModelsConfig,
			}
		}
	}

	// Priority 3: Agent context tokens config
	if agentConfig != nil {
		fromAgentConfig := normalizePositiveInt(float64(agentConfig.GetDefaultContextTokens()))
		if fromAgentConfig > 0 {
			return ContextWindowInfo{
				Tokens: fromAgentConfig,
				Source: ContextWindowSourceAgentContextTokens,
			}
		}
	}

	// Priority 4: Default
	return ContextWindowInfo{
		Tokens: defaultTokens,
		Source: ContextWindowSourceDefault,
	}
}

// EvaluateContextWindowGuardOptions contains options for EvaluateContextWindowGuard.
type EvaluateContextWindowGuardOptions struct {
	// WarnBelowTokens is the threshold below which a warning should be issued.
	// If zero, ContextWindowWarnBelowTokens is used.
	WarnBelowTokens int
	// HardMinTokens is the minimum tokens below which operation should be blocked.
	// If zero, ContextWindowHardMinTokens is used.
	HardMinTokens int
}

// EvaluateContextWindowGuard evaluates the context window and returns warning/blocking status.
func EvaluateContextWindowGuard(info ContextWindowInfo, opts *EvaluateContextWindowGuardOptions) ContextWindowGuardResult {
	warnBelow := ContextWindowWarnBelowTokens
	hardMin := ContextWindowHardMinTokens

	if opts != nil {
		if opts.WarnBelowTokens > 0 {
			warnBelow = opts.WarnBelowTokens
		}
		if opts.HardMinTokens > 0 {
			hardMin = opts.HardMinTokens
		}
	}

	// Ensure thresholds are at least 1
	if warnBelow < 1 {
		warnBelow = 1
	}
	if hardMin < 1 {
		hardMin = 1
	}

	tokens := info.Tokens
	if tokens < 0 {
		tokens = 0
	}

	return ContextWindowGuardResult{
		ContextWindowInfo: ContextWindowInfo{
			Tokens: tokens,
			Source: info.Source,
		},
		ShouldWarn:  tokens > 0 && tokens < warnBelow,
		ShouldBlock: tokens > 0 && tokens < hardMin,
	}
}
