package models

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ModelCandidate represents a provider/model pair to try
type ModelCandidate struct {
	Provider string
	Model    string
}

// String returns a string representation of the candidate
func (c ModelCandidate) String() string {
	return ModelKey(c.Provider, c.Model)
}

// FallbackAttempt records details of a failed attempt
type FallbackAttempt struct {
	Provider string
	Model    string
	Error    string
	Reason   string // e.g., "rate_limit", "auth_error", "timeout"
	Status   int    // HTTP status if applicable
	Code     string // Error code if applicable
}

// FallbackResult contains the successful result and attempt history
type FallbackResult[T any] struct {
	Result   T
	Provider string
	Model    string
	Attempts []FallbackAttempt
}

// FallbackConfig configures model fallback behavior
type FallbackConfig struct {
	PrimaryProvider string
	PrimaryModel    string
	Fallbacks       []string        // "provider/model" strings
	AllowedModels   map[string]bool // Optional allowlist
}

// RunFunc is the function signature for the operation to run
type RunFunc[T any] func(ctx context.Context, provider, model string) (T, error)

// OnErrorFunc is called when an attempt fails
type OnErrorFunc func(provider, model string, err error, attempt, total int)

// FailoverError represents an error that should trigger fallback
type FailoverError struct {
	Err      error
	Provider string
	Model    string
	Reason   string
	Status   int
	Code     string
}

func (e *FailoverError) Error() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("[%s]", e.Reason))

	if e.Provider != "" {
		parts = append(parts, e.Provider)
	}

	if e.Model != "" {
		parts = append(parts, fmt.Sprintf("model=%s", e.Model))
	}

	if e.Status != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.Status))
	}

	if e.Code != "" {
		parts = append(parts, fmt.Sprintf("code=%s", e.Code))
	}

	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}

	return strings.Join(parts, " ")
}

func (e *FailoverError) Unwrap() error {
	return e.Err
}

// Common error reasons
const (
	ReasonRateLimit    = "rate_limit"
	ReasonAuthError    = "auth_error"
	ReasonTimeout      = "timeout"
	ReasonServerError  = "server_error"
	ReasonBilling      = "billing"
	ReasonUnavailable  = "model_unavailable"
	ReasonAbort        = "abort"
	ReasonInvalid      = "invalid_request"
	ReasonContentBlock = "content_blocked"
	ReasonUnknown      = "unknown"
)

// Sentinel errors for abort conditions
var (
	// ErrAborted indicates user-initiated abort (should not retry)
	ErrAborted = errors.New("operation aborted")

	// ErrAllCandidatesFailed indicates all fallback candidates failed
	ErrAllCandidatesFailed = errors.New("all model candidates failed")
)

// IsFailoverError checks if an error should trigger fallback
func IsFailoverError(err error) bool {
	if err == nil {
		return false
	}

	// Check for FailoverError type
	var failoverErr *FailoverError
	if errors.As(err, &failoverErr) {
		// Abort errors should not failover
		if failoverErr.Reason == ReasonAbort {
			return false
		}
		return true
	}

	// Check for abort conditions
	if IsAbortError(err) {
		return false
	}

	// Check for retryable conditions by error content
	reason := classifyErrorReason(err)
	switch reason {
	case ReasonRateLimit, ReasonServerError, ReasonTimeout, ReasonBilling,
		ReasonAuthError, ReasonUnavailable:
		return true
	default:
		return false
	}
}

// IsAbortError checks if the error is a user abort (should not retry)
func IsAbortError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation
	if errors.Is(err, context.Canceled) {
		return true
	}

	// Check for our abort sentinel
	if errors.Is(err, ErrAborted) {
		return true
	}

	// Check for FailoverError with abort reason
	var failoverErr *FailoverError
	if errors.As(err, &failoverErr) {
		return failoverErr.Reason == ReasonAbort
	}

	// Check error message for abort patterns
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "aborted") ||
		strings.Contains(errStr, "cancelled") ||
		strings.Contains(errStr, "user abort")
}

// IsTimeoutError checks if the error is a timeout
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context deadline
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for FailoverError with timeout reason
	var failoverErr *FailoverError
	if errors.As(err, &failoverErr) {
		return failoverErr.Reason == ReasonTimeout
	}

	// Check error message for timeout patterns
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline") ||
		strings.Contains(errStr, "etimedout")
}

// CoerceToFailoverError wraps an error as a FailoverError if appropriate
func CoerceToFailoverError(err error, provider, model string) *FailoverError {
	if err == nil {
		return nil
	}

	// Already a FailoverError, update provider/model if not set
	var existing *FailoverError
	if errors.As(err, &existing) {
		if existing.Provider == "" {
			existing.Provider = provider
		}
		if existing.Model == "" {
			existing.Model = model
		}
		return existing
	}

	// Create new FailoverError
	return &FailoverError{
		Err:      err,
		Provider: provider,
		Model:    model,
		Reason:   classifyErrorReason(err),
	}
}

// classifyErrorReason determines the reason from error content
func classifyErrorReason(err error) string {
	if err == nil {
		return ReasonUnknown
	}

	// Check for context errors
	if errors.Is(err, context.Canceled) {
		return ReasonAbort
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}

	errStr := strings.ToLower(err.Error())

	// Abort patterns
	if strings.Contains(errStr, "aborted") ||
		strings.Contains(errStr, "cancelled") ||
		strings.Contains(errStr, "user abort") {
		return ReasonAbort
	}

	// Timeout patterns
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline") ||
		strings.Contains(errStr, "etimedout") {
		return ReasonTimeout
	}

	// Rate limit patterns
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "429") {
		return ReasonRateLimit
	}

	// Auth patterns
	if strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "invalid api key") ||
		strings.Contains(errStr, "invalid_api_key") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") {
		return ReasonAuthError
	}

	// Billing patterns
	if strings.Contains(errStr, "billing") ||
		strings.Contains(errStr, "payment") ||
		strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "insufficient") ||
		strings.Contains(errStr, "402") {
		return ReasonBilling
	}

	// Model unavailable patterns
	if strings.Contains(errStr, "model not found") ||
		strings.Contains(errStr, "model_not_found") ||
		strings.Contains(errStr, "does not exist") ||
		strings.Contains(errStr, "unavailable") {
		return ReasonUnavailable
	}

	// Content block patterns
	if strings.Contains(errStr, "content_filter") ||
		strings.Contains(errStr, "content policy") ||
		strings.Contains(errStr, "safety") ||
		strings.Contains(errStr, "blocked") {
		return ReasonContentBlock
	}

	// Server error patterns
	if strings.Contains(errStr, "internal server") ||
		strings.Contains(errStr, "server error") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "504") {
		return ReasonServerError
	}

	// Invalid request patterns
	if strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "bad request") ||
		strings.Contains(errStr, "400") {
		return ReasonInvalid
	}

	return ReasonUnknown
}

// RunWithModelFallback executes a function with model fallback
func RunWithModelFallback[T any](ctx context.Context, config *FallbackConfig, run RunFunc[T], onError OnErrorFunc) (*FallbackResult[T], error) {
	candidates := BuildFallbackCandidates(config)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no model candidates configured")
	}

	// Filter by allowlist if configured
	if len(config.AllowedModels) > 0 {
		filtered := make([]ModelCandidate, 0, len(candidates))
		for _, c := range candidates {
			key := ModelKey(c.Provider, c.Model)
			if config.AllowedModels[key] {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("no allowed model candidates available")
		}
		candidates = filtered
	}

	var attempts []FallbackAttempt
	total := len(candidates)

	for i, candidate := range candidates {
		// Check context before each attempt
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ErrAborted
			}
			return nil, ctx.Err()
		}

		result, err := run(ctx, candidate.Provider, candidate.Model)
		if err == nil {
			// Success
			return &FallbackResult[T]{
				Result:   result,
				Provider: candidate.Provider,
				Model:    candidate.Model,
				Attempts: attempts,
			}, nil
		}

		// Record attempt
		failoverErr := CoerceToFailoverError(err, candidate.Provider, candidate.Model)
		attempt := FallbackAttempt{
			Provider: candidate.Provider,
			Model:    candidate.Model,
			Error:    err.Error(),
			Reason:   failoverErr.Reason,
			Status:   failoverErr.Status,
			Code:     failoverErr.Code,
		}
		attempts = append(attempts, attempt)

		// Call error callback if provided
		if onError != nil {
			onError(candidate.Provider, candidate.Model, err, i+1, total)
		}

		// Check for abort (not timeout) - should not retry
		if IsAbortError(err) && !IsTimeoutError(err) {
			return nil, err
		}

		// If this is the last candidate, don't check for failover
		if i == len(candidates)-1 {
			break
		}

		// Check if we should try next candidate
		if !IsFailoverError(err) {
			// Non-failover error, return immediately
			return nil, err
		}
	}

	// All candidates failed, return aggregated error
	return nil, buildAggregatedError(attempts)
}

// RunWithImageModelFallback is specialized for image model fallback
// It follows the same pattern but is useful for type clarity and future
// image-specific fallback logic (e.g., vision capability filtering)
func RunWithImageModelFallback[T any](ctx context.Context, config *FallbackConfig, run RunFunc[T], onError OnErrorFunc) (*FallbackResult[T], error) {
	// For now, use the same logic as standard fallback
	// In the future, this could filter candidates by vision capability
	return RunWithModelFallback(ctx, config, run, onError)
}

// ModelKey creates a unique key for a provider/model pair
func ModelKey(provider, model string) string {
	return fmt.Sprintf("%s/%s", strings.ToLower(provider), strings.ToLower(model))
}

// ParseModelRef parses a "provider/model" string
func ParseModelRef(ref, defaultProvider string) *ModelCandidate {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}

	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 1 {
		// Just model name, use default provider
		return &ModelCandidate{
			Provider: defaultProvider,
			Model:    parts[0],
		}
	}

	return &ModelCandidate{
		Provider: parts[0],
		Model:    parts[1],
	}
}

// BuildFallbackCandidates builds the list of candidates from config
func BuildFallbackCandidates(config *FallbackConfig) []ModelCandidate {
	if config == nil {
		return nil
	}

	candidates := make([]ModelCandidate, 0, 1+len(config.Fallbacks))

	// Add primary model first
	if config.PrimaryProvider != "" && config.PrimaryModel != "" {
		candidates = append(candidates, ModelCandidate{
			Provider: config.PrimaryProvider,
			Model:    config.PrimaryModel,
		})
	}

	// Add fallbacks
	for _, ref := range config.Fallbacks {
		candidate := ParseModelRef(ref, config.PrimaryProvider)
		if candidate != nil {
			// Deduplicate: skip if same as primary
			if candidate.Provider == config.PrimaryProvider &&
				candidate.Model == config.PrimaryModel {
				continue
			}
			candidates = append(candidates, *candidate)
		}
	}

	return candidates
}

// buildAggregatedError creates an error summarizing all failed attempts
func buildAggregatedError(attempts []FallbackAttempt) error {
	if len(attempts) == 0 {
		return ErrAllCandidatesFailed
	}

	var sb strings.Builder
	sb.WriteString("all model candidates failed:\n")

	for i, a := range attempts {
		sb.WriteString(fmt.Sprintf("  %d. %s/%s: [%s] %s",
			i+1, a.Provider, a.Model, a.Reason, a.Error))
		if a.Status != 0 {
			sb.WriteString(fmt.Sprintf(" (status=%d)", a.Status))
		}
		if a.Code != "" {
			sb.WriteString(fmt.Sprintf(" (code=%s)", a.Code))
		}
		if i < len(attempts)-1 {
			sb.WriteString("\n")
		}
	}

	return fmt.Errorf("%w: %s", ErrAllCandidatesFailed, sb.String())
}

// NewFailoverError creates a new FailoverError
func NewFailoverError(err error, provider, model, reason string) *FailoverError {
	return &FailoverError{
		Err:      err,
		Provider: provider,
		Model:    model,
		Reason:   reason,
	}
}

// WithStatus adds HTTP status to the error
func (e *FailoverError) WithStatus(status int) *FailoverError {
	e.Status = status
	return e
}

// WithCode adds an error code
func (e *FailoverError) WithCode(code string) *FailoverError {
	e.Code = code
	return e
}
