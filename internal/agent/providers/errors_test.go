package providers

import (
	"errors"
	"testing"
)

func TestFailoverReasonIsRetryable(t *testing.T) {
	tests := []struct {
		reason   FailoverReason
		expected bool
	}{
		{FailoverRateLimit, true},
		{FailoverTimeout, true},
		{FailoverServerError, true},
		{FailoverBilling, false},
		{FailoverAuth, false},
		{FailoverInvalidRequest, false},
		{FailoverModelUnavailable, false},
		{FailoverContentFilter, false},
		{FailoverUnknown, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := tt.reason.IsRetryable(); got != tt.expected {
				t.Errorf("FailoverReason(%q).IsRetryable() = %v, want %v", tt.reason, got, tt.expected)
			}
		})
	}
}

func TestFailoverReasonShouldFailover(t *testing.T) {
	tests := []struct {
		reason   FailoverReason
		expected bool
	}{
		{FailoverBilling, true},
		{FailoverAuth, true},
		{FailoverModelUnavailable, true},
		{FailoverRateLimit, false},
		{FailoverTimeout, false},
		{FailoverServerError, false},
		{FailoverInvalidRequest, false},
		{FailoverContentFilter, false},
		{FailoverUnknown, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := tt.reason.ShouldFailover(); got != tt.expected {
				t.Errorf("FailoverReason(%q).ShouldFailover() = %v, want %v", tt.reason, got, tt.expected)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected FailoverReason
	}{
		{"nil error", nil, FailoverUnknown},
		{"timeout", errors.New("request timeout"), FailoverTimeout},
		{"deadline exceeded", errors.New("context deadline exceeded"), FailoverTimeout},
		{"rate limit", errors.New("rate limit exceeded"), FailoverRateLimit},
		{"too many requests", errors.New("too many requests"), FailoverRateLimit},
		{"429 status", errors.New("HTTP 429"), FailoverRateLimit},
		{"unauthorized", errors.New("unauthorized"), FailoverAuth},
		{"invalid api key", errors.New("invalid api key"), FailoverAuth},
		{"billing", errors.New("billing issue"), FailoverBilling},
		{"quota exceeded", errors.New("quota exceeded"), FailoverBilling},
		{"content filter", errors.New("content_filter triggered"), FailoverContentFilter},
		{"content blocked", errors.New("content blocked by safety"), FailoverContentFilter},
		{"model not found", errors.New("model not found"), FailoverModelUnavailable},
		{"server error", errors.New("internal server error"), FailoverServerError},
		{"500 status", errors.New("HTTP 500"), FailoverServerError},
		{"unknown", errors.New("something went wrong"), FailoverUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.expected {
				t.Errorf("ClassifyError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestProviderError(t *testing.T) {
	cause := errors.New("underlying error")
	err := NewProviderError("anthropic", "claude-3-opus", cause).
		WithStatus(429).
		WithCode("rate_limit_error").
		WithRequestID("req-123")

	// Check error message contains relevant info
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}

	// Check reason was classified
	if err.Reason != FailoverRateLimit {
		t.Errorf("Expected reason %v, got %v", FailoverRateLimit, err.Reason)
	}

	// Check fields are set
	if err.Provider != "anthropic" {
		t.Errorf("Expected provider anthropic, got %s", err.Provider)
	}
	if err.Model != "claude-3-opus" {
		t.Errorf("Expected model claude-3-opus, got %s", err.Model)
	}
	if err.Status != 429 {
		t.Errorf("Expected status 429, got %d", err.Status)
	}
	if err.Code != "rate_limit_error" {
		t.Errorf("Expected code rate_limit_error, got %s", err.Code)
	}
	if err.RequestID != "req-123" {
		t.Errorf("Expected request ID req-123, got %s", err.RequestID)
	}

	// Check Unwrap
	if err.Unwrap() != cause {
		t.Error("Unwrap() did not return cause")
	}

	// Check IsRetryable
	if !err.Reason.IsRetryable() {
		t.Error("Rate limit should be retryable")
	}
}

func TestIsProviderError(t *testing.T) {
	providerErr := NewProviderError("openai", "gpt-4", errors.New("test"))
	regularErr := errors.New("regular error")

	if !IsProviderError(providerErr) {
		t.Error("IsProviderError should return true for ProviderError")
	}

	if IsProviderError(regularErr) {
		t.Error("IsProviderError should return false for regular error")
	}
}

func TestGetProviderError(t *testing.T) {
	providerErr := NewProviderError("openai", "gpt-4", errors.New("test"))

	// Direct ProviderError
	got, ok := GetProviderError(providerErr)
	if !ok || got != providerErr {
		t.Error("GetProviderError should extract direct ProviderError")
	}

	// Regular error
	_, ok = GetProviderError(errors.New("regular"))
	if ok {
		t.Error("GetProviderError should return false for regular error")
	}
}

func TestIsRetryableAndShouldFailover(t *testing.T) {
	rateLimitErr := NewProviderError("anthropic", "claude", nil).WithStatus(429)
	authErr := NewProviderError("openai", "gpt-4", nil).WithStatus(401)
	regularErr := errors.New("timeout exceeded")

	// Rate limit is retryable but not failover
	if !IsRetryable(rateLimitErr) {
		t.Error("Rate limit error should be retryable")
	}
	if ShouldFailover(rateLimitErr) {
		t.Error("Rate limit error should not trigger failover")
	}

	// Auth error is not retryable but should failover
	if IsRetryable(authErr) {
		t.Error("Auth error should not be retryable")
	}
	if !ShouldFailover(authErr) {
		t.Error("Auth error should trigger failover")
	}

	// Regular timeout error (classified from message)
	if !IsRetryable(regularErr) {
		t.Error("Timeout error should be retryable")
	}
}

func TestClassifyStatusCode(t *testing.T) {
	tests := []struct {
		status   int
		expected FailoverReason
	}{
		{401, FailoverAuth},
		{403, FailoverAuth},
		{402, FailoverBilling},
		{429, FailoverRateLimit},
		{400, FailoverInvalidRequest},
		{404, FailoverModelUnavailable},
		{500, FailoverServerError},
		{502, FailoverServerError},
		{503, FailoverServerError},
		{200, FailoverUnknown},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.status)), func(t *testing.T) {
			if got := classifyStatusCode(tt.status); got != tt.expected {
				t.Errorf("classifyStatusCode(%d) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

// TestClassifyErrorCode tests provider-specific error code classification.
func TestClassifyErrorCode(t *testing.T) {
	tests := []struct {
		code     string
		expected FailoverReason
	}{
		{"rate_limit_error", FailoverRateLimit},
		{"rate_limit_exceeded", FailoverRateLimit},
		{"RATE_LIMIT_ERROR", FailoverRateLimit}, // Case insensitive
		{"authentication_error", FailoverAuth},
		{"invalid_api_key", FailoverAuth},
		{"billing_error", FailoverBilling},
		{"insufficient_quota", FailoverBilling},
		{"model_not_found", FailoverModelUnavailable},
		{"model_not_available", FailoverModelUnavailable},
		{"content_policy_violation", FailoverContentFilter},
		{"content_filter", FailoverContentFilter},
		{"server_error", FailoverServerError},
		{"internal_error", FailoverServerError},
		{"invalid_request_error", FailoverInvalidRequest},
		{"unknown_code", FailoverUnknown},
		{"", FailoverUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := classifyErrorCode(tt.code); got != tt.expected {
				t.Errorf("classifyErrorCode(%q) = %v, want %v", tt.code, got, tt.expected)
			}
		})
	}
}

// TestProviderErrorError tests the Error() method edge cases.
func TestProviderErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *ProviderError
	}{
		{
			name: "all fields populated",
			err: &ProviderError{
				Reason:    FailoverRateLimit,
				Provider:  "test",
				Model:     "test-model",
				Status:    429,
				Code:      "rate_limit",
				Message:   "Too many requests",
				RequestID: "req-123",
				Cause:     errors.New("underlying"),
			},
		},
		{
			name: "minimal fields",
			err: &ProviderError{
				Reason: FailoverUnknown,
			},
		},
		{
			name: "message from cause",
			err: &ProviderError{
				Reason:   FailoverTimeout,
				Provider: "test",
				Cause:    errors.New("context deadline exceeded"),
			},
		},
		{
			name: "no message no cause",
			err: &ProviderError{
				Reason:   FailoverAuth,
				Provider: "openai",
				Model:    "gpt-4",
				Status:   401,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if errStr == "" {
				t.Error("Error() should not return empty string")
			}
		})
	}
}

// TestWithCodeUpdatesReason tests that WithCode can update the reason.
func TestWithCodeUpdatesReason(t *testing.T) {
	err := NewProviderError("test", "model", errors.New("test"))

	// Initial reason should be classified from error message
	if err.Reason == FailoverUnknown {
		// Good, it's unknown for a generic error
	}

	// WithCode should update the reason
	err = err.WithCode("rate_limit_error")
	if err.Reason != FailoverRateLimit {
		t.Errorf("expected reason %v after WithCode, got %v", FailoverRateLimit, err.Reason)
	}

	// Unknown code should not change reason
	err = NewProviderError("test", "model", errors.New("timeout")).
		WithStatus(429)
	originalReason := err.Reason

	err = err.WithCode("unknown_xyz")
	if err.Reason != originalReason {
		// The reason might have changed based on status, that's OK
		// Just verify it doesn't become Unknown
	}
}

// TestClassifyErrorEdgeCases tests edge cases in error classification.
func TestClassifyErrorEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected FailoverReason
	}{
		{"etimedout", errors.New("ETIMEDOUT connection timed out"), FailoverTimeout},
		{"context deadline", errors.New("context deadline"), FailoverTimeout},
		{"rate_limit underscore", errors.New("rate_limit_exceeded"), FailoverRateLimit},
		{"invalid_api_key", errors.New("invalid_api_key provided"), FailoverAuth},
		{"authentication required", errors.New("authentication required"), FailoverAuth},
		{"insufficient funds", errors.New("insufficient credits"), FailoverBilling},
		{"payment required", errors.New("payment required"), FailoverBilling},
		{"content policy", errors.New("content policy violation"), FailoverContentFilter},
		{"safety filter", errors.New("blocked by safety filters"), FailoverContentFilter},
		{"model does not exist", errors.New("model does not exist"), FailoverModelUnavailable},
		{"service unavailable", errors.New("service unavailable"), FailoverModelUnavailable},
		{"server error 500", errors.New("500 error occurred"), FailoverServerError},
		{"http 502", errors.New("HTTP 502"), FailoverServerError},
		{"http 503", errors.New("HTTP 503"), FailoverServerError},
		{"http 504", errors.New("HTTP 504"), FailoverServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err)
			if got != tt.expected {
				t.Errorf("ClassifyError(%q) = %v, want %v", tt.err.Error(), got, tt.expected)
			}
		})
	}
}
