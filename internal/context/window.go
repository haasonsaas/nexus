// Package context provides context window management for LLM conversations.
package context

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Default token limits
const (
	// DefaultContextWindow is the default context window size in tokens
	DefaultContextWindow = 128000

	// MinContextWindow is the minimum viable context window
	MinContextWindow = 16000

	// WarnBelowTokens triggers a warning when remaining tokens drop below this
	WarnBelowTokens = 32000

	// TokensPerChar is a rough estimate of tokens per character (conservative)
	TokensPerChar = 0.25
)

// ModelContextWindows maps model IDs to their context window sizes.
var ModelContextWindows = map[string]int{
	// Anthropic models
	"claude-3-opus":          200000,
	"claude-3-sonnet":        200000,
	"claude-3-haiku":         200000,
	"claude-3-5-sonnet":      200000,
	"claude-3-5-haiku":       200000,
	"claude-opus-4":          200000,

	// OpenAI models
	"gpt-4":                  8192,
	"gpt-4-32k":              32768,
	"gpt-4-turbo":            128000,
	"gpt-4o":                 128000,
	"gpt-4o-mini":            128000,
	"gpt-3.5-turbo":          16385,
	"gpt-3.5-turbo-16k":      16385,
	"o1":                     200000,
	"o1-mini":                128000,
	"o1-preview":             128000,
	"o3-mini":                200000,

	// Google models
	"gemini-pro":             32768,
	"gemini-1.5-pro":         2097152,
	"gemini-1.5-flash":       1048576,
	"gemini-2.0-flash":       1048576,
}

// WindowInfo holds information about a context window.
type WindowInfo struct {
	// Total tokens available
	TotalTokens int `json:"total_tokens"`

	// Used tokens
	UsedTokens int `json:"used_tokens"`

	// Remaining tokens
	RemainingTokens int `json:"remaining_tokens"`

	// Percentage used
	UsedPercent float64 `json:"used_percent"`

	// Source of the window size (model, config, default)
	Source string `json:"source"`
}

// Status returns a descriptive status of the context window.
func (w *WindowInfo) Status() string {
	if w.ShouldBlock() {
		return "critical"
	}
	if w.ShouldWarn() {
		return "warning"
	}
	return "ok"
}

// ShouldWarn returns true if the context is getting low.
func (w *WindowInfo) ShouldWarn() bool {
	return w.RemainingTokens < WarnBelowTokens
}

// ShouldBlock returns true if the context is too low to continue.
func (w *WindowInfo) ShouldBlock() bool {
	return w.RemainingTokens < MinContextWindow
}

// String returns a human-readable description.
func (w *WindowInfo) String() string {
	return fmt.Sprintf("%d/%d tokens (%.1f%% used, %s)",
		w.UsedTokens, w.TotalTokens, w.UsedPercent, w.Status())
}

// Window manages context window tracking and token counting.
type Window struct {
	totalTokens int
	usedTokens  int
	source      string
}

// NewWindow creates a new context window manager.
func NewWindow(totalTokens int, source string) *Window {
	if totalTokens <= 0 {
		totalTokens = DefaultContextWindow
		source = "default"
	}
	return &Window{
		totalTokens: totalTokens,
		source:      source,
	}
}

// NewWindowForModel creates a context window for a specific model.
func NewWindowForModel(modelID string) *Window {
	// Try exact match first
	if tokens, ok := ModelContextWindows[modelID]; ok {
		return NewWindow(tokens, "model")
	}

	// Try prefix match - find longest matching prefix
	// (e.g., "gpt-4-turbo-preview" should match "gpt-4-turbo" not "gpt-4")
	bestMatch := ""
	bestTokens := 0
	for prefix, tokens := range ModelContextWindows {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(bestMatch) {
			bestMatch = prefix
			bestTokens = tokens
		}
	}

	if bestMatch != "" {
		return NewWindow(bestTokens, "model")
	}

	return NewWindow(DefaultContextWindow, "default")
}

// Add adds tokens to the used count.
func (w *Window) Add(tokens int) {
	w.usedTokens += tokens
}

// AddText estimates and adds tokens for text content.
func (w *Window) AddText(text string) int {
	tokens := EstimateTokens(text)
	w.Add(tokens)
	return tokens
}

// Reset resets the used token count.
func (w *Window) Reset() {
	w.usedTokens = 0
}

// SetUsed sets the used token count directly.
func (w *Window) SetUsed(tokens int) {
	w.usedTokens = tokens
}

// Info returns the current window information.
func (w *Window) Info() *WindowInfo {
	remaining := w.totalTokens - w.usedTokens
	if remaining < 0 {
		remaining = 0
	}

	var usedPercent float64
	if w.totalTokens > 0 {
		usedPercent = float64(w.usedTokens) / float64(w.totalTokens) * 100
	}

	return &WindowInfo{
		TotalTokens:     w.totalTokens,
		UsedTokens:      w.usedTokens,
		RemainingTokens: remaining,
		UsedPercent:     usedPercent,
		Source:          w.source,
	}
}

// Remaining returns the remaining tokens.
func (w *Window) Remaining() int {
	remaining := w.totalTokens - w.usedTokens
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CanFit returns true if the given number of tokens will fit.
func (w *Window) CanFit(tokens int) bool {
	return w.Remaining() >= tokens
}

// CanFitText returns true if the estimated tokens for text will fit.
func (w *Window) CanFitText(text string) bool {
	return w.CanFit(EstimateTokens(text))
}

// EstimateTokens estimates the number of tokens in text.
// Uses a conservative estimate of ~4 characters per token.
func EstimateTokens(text string) int {
	// Count characters (Unicode-aware)
	charCount := utf8.RuneCountInString(text)

	// Apply conservative ratio
	tokens := int(float64(charCount) * TokensPerChar)

	// Minimum of 1 token for non-empty text
	if tokens == 0 && charCount > 0 {
		return 1
	}

	return tokens
}

// EstimateTokensForMessages estimates tokens for a batch of messages.
func EstimateTokensForMessages(contents []string) int {
	total := 0
	for _, content := range contents {
		total += EstimateTokens(content)
		// Add overhead per message (role, formatting)
		total += 4
	}
	return total
}

// GetModelContextWindow returns the context window for a model ID.
func GetModelContextWindow(modelID string) (int, bool) {
	if tokens, ok := ModelContextWindows[modelID]; ok {
		return tokens, true
	}

	// Try prefix match - find longest matching prefix
	bestMatch := ""
	bestTokens := 0
	for prefix, tokens := range ModelContextWindows {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(bestMatch) {
			bestMatch = prefix
			bestTokens = tokens
		}
	}

	if bestMatch != "" {
		return bestTokens, true
	}

	return 0, false
}

// RegisterModelContextWindow registers a context window size for a model.
func RegisterModelContextWindow(modelID string, tokens int) {
	ModelContextWindows[modelID] = tokens
}
