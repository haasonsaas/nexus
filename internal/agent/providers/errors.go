package providers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// FailoverReason categorizes why a provider request failed.
// This enables intelligent retry and failover logic.
type FailoverReason string

const (
	// FailoverBilling indicates payment/quota issues (HTTP 402)
	FailoverBilling FailoverReason = "billing"

	// FailoverRateLimit indicates rate limiting (HTTP 429)
	FailoverRateLimit FailoverReason = "rate_limit"

	// FailoverAuth indicates authentication failure (HTTP 401, 403)
	FailoverAuth FailoverReason = "auth"

	// FailoverTimeout indicates request timeout
	FailoverTimeout FailoverReason = "timeout"

	// FailoverServerError indicates server-side issues (HTTP 5xx)
	FailoverServerError FailoverReason = "server_error"

	// FailoverInvalidRequest indicates client-side issues (HTTP 400)
	FailoverInvalidRequest FailoverReason = "invalid_request"

	// FailoverModelUnavailable indicates the model is not available
	FailoverModelUnavailable FailoverReason = "model_unavailable"

	// FailoverContentFilter indicates content was blocked by safety filters
	FailoverContentFilter FailoverReason = "content_filter"

	// FailoverUnknown indicates an unclassified error
	FailoverUnknown FailoverReason = "unknown"
)

// IsRetryable returns true if the failover reason suggests retrying may succeed.
func (r FailoverReason) IsRetryable() bool {
	switch r {
	case FailoverRateLimit, FailoverTimeout, FailoverServerError:
		return true
	default:
		return false
	}
}

// ShouldFailover returns true if the error warrants trying a different provider/model.
func (r FailoverReason) ShouldFailover() bool {
	switch r {
	case FailoverBilling, FailoverAuth, FailoverModelUnavailable:
		return true
	default:
		return false
	}
}

// ProviderError represents a structured error from an LLM provider.
// It captures context needed for retry logic, failover decisions, and debugging.
type ProviderError struct {
	// Reason categorizes the error for retry/failover logic
	Reason FailoverReason

	// Provider is the name of the provider (e.g., "anthropic", "openai")
	Provider string

	// Model is the model that was requested
	Model string

	// Status is the HTTP status code, if applicable
	Status int

	// Code is the provider-specific error code
	Code string

	// Message is the human-readable error message
	Message string

	// RequestID is the provider's request ID for debugging
	RequestID string

	// Cause is the underlying error
	Cause error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
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

	if e.Message != "" {
		parts = append(parts, e.Message)
	} else if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}

	return strings.Join(parts, " ")
}

// Unwrap returns the underlying error.
func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// NewProviderError creates a new ProviderError with the given parameters.
func NewProviderError(provider, model string, cause error) *ProviderError {
	err := &ProviderError{
		Provider: provider,
		Model:    model,
		Cause:    cause,
		Reason:   FailoverUnknown,
	}

	if cause != nil {
		err.Message = cause.Error()
		err.Reason = ClassifyError(cause)
	}

	return err
}

// WithStatus adds HTTP status to the error and reclassifies if needed.
func (e *ProviderError) WithStatus(status int) *ProviderError {
	e.Status = status
	e.Reason = classifyStatusCode(status)
	return e
}

// WithCode adds a provider-specific error code.
func (e *ProviderError) WithCode(code string) *ProviderError {
	e.Code = code
	// Reclassify based on known codes
	if reason := classifyErrorCode(code); reason != FailoverUnknown {
		e.Reason = reason
	}
	return e
}

// WithRequestID adds the provider's request ID.
func (e *ProviderError) WithRequestID(id string) *ProviderError {
	e.RequestID = id
	return e
}

// WithMessage sets the error message.
func (e *ProviderError) WithMessage(msg string) *ProviderError {
	e.Message = msg
	return e
}

// ClassifyError inspects an error and returns the appropriate FailoverReason.
func ClassifyError(err error) FailoverReason {
	if err == nil {
		return FailoverUnknown
	}

	errStr := strings.ToLower(err.Error())

	// Check for timeout patterns
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline") ||
		strings.Contains(errStr, "etimedout") {
		return FailoverTimeout
	}

	// Check for rate limit patterns
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "429") {
		return FailoverRateLimit
	}

	// Check for authentication patterns
	if strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "invalid api key") ||
		strings.Contains(errStr, "invalid_api_key") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") {
		return FailoverAuth
	}

	// Check for billing patterns
	if strings.Contains(errStr, "billing") ||
		strings.Contains(errStr, "payment") ||
		strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "insufficient") ||
		strings.Contains(errStr, "402") {
		return FailoverBilling
	}

	// Check for content filter patterns
	if strings.Contains(errStr, "content_filter") ||
		strings.Contains(errStr, "content policy") ||
		strings.Contains(errStr, "safety") ||
		strings.Contains(errStr, "blocked") {
		return FailoverContentFilter
	}

	// Check for model availability patterns
	if strings.Contains(errStr, "model not found") ||
		strings.Contains(errStr, "model_not_found") ||
		strings.Contains(errStr, "does not exist") ||
		strings.Contains(errStr, "unavailable") {
		return FailoverModelUnavailable
	}

	// Check for server error patterns
	if strings.Contains(errStr, "internal server") ||
		strings.Contains(errStr, "server error") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "504") {
		return FailoverServerError
	}

	return FailoverUnknown
}

// classifyStatusCode returns a FailoverReason based on HTTP status code.
func classifyStatusCode(status int) FailoverReason {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return FailoverAuth
	case status == http.StatusPaymentRequired:
		return FailoverBilling
	case status == http.StatusTooManyRequests:
		return FailoverRateLimit
	case status == http.StatusBadRequest:
		return FailoverInvalidRequest
	case status == http.StatusNotFound:
		return FailoverModelUnavailable
	case status >= 500:
		return FailoverServerError
	default:
		return FailoverUnknown
	}
}

// classifyErrorCode returns a FailoverReason based on provider-specific error codes.
func classifyErrorCode(code string) FailoverReason {
	code = strings.ToLower(code)

	switch code {
	case "rate_limit_error", "rate_limit_exceeded":
		return FailoverRateLimit
	case "authentication_error", "invalid_api_key":
		return FailoverAuth
	case "billing_error", "insufficient_quota":
		return FailoverBilling
	case "model_not_found", "model_not_available":
		return FailoverModelUnavailable
	case "content_policy_violation", "content_filter":
		return FailoverContentFilter
	case "server_error", "internal_error":
		return FailoverServerError
	case "invalid_request_error":
		return FailoverInvalidRequest
	default:
		return FailoverUnknown
	}
}

// IsProviderError checks if an error is a ProviderError.
func IsProviderError(err error) bool {
	var providerErr *ProviderError
	return errors.As(err, &providerErr)
}

// GetProviderError extracts a ProviderError from an error chain.
func GetProviderError(err error) (*ProviderError, bool) {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr, true
	}
	return nil, false
}

// IsRetryable checks if an error should be retried.
func IsRetryable(err error) bool {
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.IsRetryable()
	}
	// Classify raw errors
	return ClassifyError(err).IsRetryable()
}

// ShouldFailover checks if an error warrants trying a different provider.
func ShouldFailover(err error) bool {
	if providerErr, ok := GetProviderError(err); ok {
		return providerErr.Reason.ShouldFailover()
	}
	return ClassifyError(err).ShouldFailover()
}
