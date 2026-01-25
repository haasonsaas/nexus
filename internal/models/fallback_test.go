package models

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestModelKey(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		expected string
	}{
		{"anthropic", "claude-3", "anthropic/claude-3"},
		{"OpenAI", "GPT-4", "openai/gpt-4"},
		{"GOOGLE", "Gemini", "google/gemini"},
	}

	for _, tt := range tests {
		result := ModelKey(tt.provider, tt.model)
		if result != tt.expected {
			t.Errorf("ModelKey(%q, %q) = %q, want %q", tt.provider, tt.model, result, tt.expected)
		}
	}
}

func TestParseModelRef(t *testing.T) {
	tests := []struct {
		ref      string
		defProv  string
		expected *ModelCandidate
	}{
		{"anthropic/claude-3", "", &ModelCandidate{"anthropic", "claude-3"}},
		{"claude-3", "anthropic", &ModelCandidate{"anthropic", "claude-3"}},
		{"openai/gpt-4", "anthropic", &ModelCandidate{"openai", "gpt-4"}},
		{"", "anthropic", nil},
		{"  ", "anthropic", nil},
	}

	for _, tt := range tests {
		result := ParseModelRef(tt.ref, tt.defProv)
		if tt.expected == nil {
			if result != nil {
				t.Errorf("ParseModelRef(%q, %q) = %v, want nil", tt.ref, tt.defProv, result)
			}
			continue
		}
		if result == nil {
			t.Errorf("ParseModelRef(%q, %q) = nil, want %v", tt.ref, tt.defProv, tt.expected)
			continue
		}
		if result.Provider != tt.expected.Provider || result.Model != tt.expected.Model {
			t.Errorf("ParseModelRef(%q, %q) = %v, want %v", tt.ref, tt.defProv, result, tt.expected)
		}
	}
}

func TestBuildFallbackCandidates(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4", "google/gemini"},
	}

	candidates := BuildFallbackCandidates(config)

	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3", len(candidates))
	}

	expected := []ModelCandidate{
		{"anthropic", "claude-3"},
		{"openai", "gpt-4"},
		{"google", "gemini"},
	}

	for i, c := range candidates {
		if c.Provider != expected[i].Provider || c.Model != expected[i].Model {
			t.Errorf("candidate %d: got %v, want %v", i, c, expected[i])
		}
	}
}

func TestBuildFallbackCandidates_Deduplication(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"anthropic/claude-3", "openai/gpt-4"},
	}

	candidates := BuildFallbackCandidates(config)

	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2 (primary duplicate should be removed)", len(candidates))
	}
}

func TestBuildFallbackCandidates_DefaultProvider(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"claude-3-haiku"},
	}

	candidates := BuildFallbackCandidates(config)

	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(candidates))
	}

	if candidates[1].Provider != "anthropic" {
		t.Errorf("fallback provider = %q, want %q", candidates[1].Provider, "anthropic")
	}
}

func TestIsFailoverError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"rate limit error", errors.New("rate limit exceeded"), true},
		{"timeout error", errors.New("request timeout"), true},
		{"server error", errors.New("internal server error 500"), true},
		{"auth error", errors.New("unauthorized: invalid api key"), true},
		{"billing error", errors.New("billing quota exceeded"), true},
		{"abort error", ErrAborted, false},
		{"context canceled", context.Canceled, false},
		{"invalid request", errors.New("invalid request: missing field"), false},
		{"unknown error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFailoverError(tt.err)
			if result != tt.expected {
				t.Errorf("IsFailoverError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsAbortError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, true},
		{"abort sentinel", ErrAborted, true},
		{"abort message", errors.New("operation aborted by user"), true},
		{"cancelled message", errors.New("request cancelled"), true},
		{"rate limit", errors.New("rate limit"), false},
		{"timeout", errors.New("timeout"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAbortError(tt.err)
			if result != tt.expected {
				t.Errorf("IsAbortError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"timeout message", errors.New("connection timeout"), true},
		{"etimedout", errors.New("connect ETIMEDOUT"), true},
		{"rate limit", errors.New("rate limit"), false},
		{"abort", ErrAborted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTimeoutError(tt.err)
			if result != tt.expected {
				t.Errorf("IsTimeoutError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestCoerceToFailoverError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		result := CoerceToFailoverError(nil, "provider", "model")
		if result != nil {
			t.Error("expected nil for nil error")
		}
	})

	t.Run("regular error", func(t *testing.T) {
		err := errors.New("rate limit exceeded")
		result := CoerceToFailoverError(err, "anthropic", "claude-3")

		if result.Provider != "anthropic" {
			t.Errorf("Provider = %q, want %q", result.Provider, "anthropic")
		}
		if result.Model != "claude-3" {
			t.Errorf("Model = %q, want %q", result.Model, "claude-3")
		}
		if result.Reason != ReasonRateLimit {
			t.Errorf("Reason = %q, want %q", result.Reason, ReasonRateLimit)
		}
	})

	t.Run("existing FailoverError", func(t *testing.T) {
		existing := &FailoverError{
			Err:    errors.New("test"),
			Reason: ReasonTimeout,
			Status: 504,
		}
		result := CoerceToFailoverError(existing, "anthropic", "claude-3")

		if result.Provider != "anthropic" {
			t.Errorf("Provider = %q, want %q", result.Provider, "anthropic")
		}
		if result.Reason != ReasonTimeout {
			t.Errorf("Reason should be preserved: got %q, want %q", result.Reason, ReasonTimeout)
		}
		if result.Status != 504 {
			t.Errorf("Status should be preserved: got %d, want %d", result.Status, 504)
		}
	})
}

func TestFailoverError_Error(t *testing.T) {
	err := &FailoverError{
		Err:      errors.New("connection failed"),
		Provider: "anthropic",
		Model:    "claude-3",
		Reason:   ReasonTimeout,
		Status:   504,
		Code:     "gateway_timeout",
	}

	errStr := err.Error()

	if !contains(errStr, "[timeout]") {
		t.Error("error should contain reason")
	}
	if !contains(errStr, "anthropic") {
		t.Error("error should contain provider")
	}
	if !contains(errStr, "model=claude-3") {
		t.Error("error should contain model")
	}
	if !contains(errStr, "status=504") {
		t.Error("error should contain status")
	}
	if !contains(errStr, "code=gateway_timeout") {
		t.Error("error should contain code")
	}
}

func TestRunWithModelFallback_PrimarySuccess(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	var callCount atomic.Int32
	run := func(ctx context.Context, provider, model string) (string, error) {
		callCount.Add(1)
		return "success", nil
	}

	result, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Result != "success" {
		t.Errorf("Result = %q, want %q", result.Result, "success")
	}
	if result.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", result.Provider, "anthropic")
	}
	if callCount.Load() != 1 {
		t.Errorf("call count = %d, want 1", callCount.Load())
	}
	if len(result.Attempts) != 0 {
		t.Errorf("should have no failed attempts")
	}
}

func TestRunWithModelFallback_FallbackOnError(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	var calls []string
	run := func(ctx context.Context, provider, model string) (string, error) {
		calls = append(calls, ModelKey(provider, model))
		if provider == "anthropic" {
			return "", errors.New("rate limit exceeded")
		}
		return "fallback success", nil
	}

	result, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Result != "fallback success" {
		t.Errorf("Result = %q, want %q", result.Result, "fallback success")
	}
	if result.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", result.Provider, "openai")
	}
	if len(calls) != 2 {
		t.Errorf("call count = %d, want 2", len(calls))
	}
	if len(result.Attempts) != 1 {
		t.Errorf("should have 1 failed attempt, got %d", len(result.Attempts))
	}
}

func TestRunWithModelFallback_AllFail(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	run := func(ctx context.Context, provider, model string) (string, error) {
		return "", fmt.Errorf("%s server error 500", provider)
	}

	result, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err == nil {
		t.Fatal("expected error when all fail")
	}

	if result != nil {
		t.Error("result should be nil on failure")
	}

	if !errors.Is(err, ErrAllCandidatesFailed) {
		t.Errorf("error should wrap ErrAllCandidatesFailed")
	}
}

func TestRunWithModelFallback_AbortNoRetry(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	var callCount atomic.Int32
	run := func(ctx context.Context, provider, model string) (string, error) {
		callCount.Add(1)
		return "", ErrAborted
	}

	_, err := RunWithModelFallback(context.Background(), config, run, nil)
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}

	if callCount.Load() != 1 {
		t.Errorf("should not retry on abort, got %d calls", callCount.Load())
	}
}

func TestRunWithModelFallback_ContextCanceled(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run := func(ctx context.Context, provider, model string) (string, error) {
		return "success", nil
	}

	_, err := RunWithModelFallback(ctx, config, run, nil)
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted on canceled context, got %v", err)
	}
}

func TestRunWithModelFallback_OnErrorCallback(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	var errorCalls []struct {
		provider string
		model    string
		attempt  int
		total    int
	}

	onError := func(provider, model string, err error, attempt, total int) {
		errorCalls = append(errorCalls, struct {
			provider string
			model    string
			attempt  int
			total    int
		}{provider, model, attempt, total})
	}

	run := func(ctx context.Context, provider, model string) (string, error) {
		if provider == "anthropic" {
			return "", errors.New("rate limit")
		}
		return "success", nil
	}

	_, err := RunWithModelFallback(context.Background(), config, run, onError)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(errorCalls) != 1 {
		t.Fatalf("expected 1 error callback, got %d", len(errorCalls))
	}

	if errorCalls[0].provider != "anthropic" {
		t.Errorf("callback provider = %q, want %q", errorCalls[0].provider, "anthropic")
	}
	if errorCalls[0].attempt != 1 {
		t.Errorf("callback attempt = %d, want 1", errorCalls[0].attempt)
	}
	if errorCalls[0].total != 2 {
		t.Errorf("callback total = %d, want 2", errorCalls[0].total)
	}
}

func TestRunWithModelFallback_AllowedModels(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4", "google/gemini"},
		AllowedModels: map[string]bool{
			"openai/gpt-4": true,
		},
	}

	var calls []string
	run := func(ctx context.Context, provider, model string) (string, error) {
		calls = append(calls, ModelKey(provider, model))
		return "success", nil
	}

	result, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 1 {
		t.Errorf("should only call allowed model, got %d calls", len(calls))
	}
	if calls[0] != "openai/gpt-4" {
		t.Errorf("should call allowed model, got %q", calls[0])
	}
	if result.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", result.Provider, "openai")
	}
}

func TestRunWithModelFallback_NoAllowedModels(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		AllowedModels: map[string]bool{
			"openai/gpt-4": true,
		},
	}

	run := func(ctx context.Context, provider, model string) (string, error) {
		return "success", nil
	}

	_, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err == nil {
		t.Fatal("expected error when no allowed candidates")
	}
}

func TestRunWithModelFallback_NonFailoverError(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	var callCount atomic.Int32
	run := func(ctx context.Context, provider, model string) (string, error) {
		callCount.Add(1)
		return "", errors.New("invalid request: missing required field")
	}

	_, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	if callCount.Load() != 1 {
		t.Errorf("should not failover on invalid request, got %d calls", callCount.Load())
	}
}

func TestRunWithModelFallback_TimeoutContinues(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "anthropic",
		PrimaryModel:    "claude-3",
		Fallbacks:       []string{"openai/gpt-4"},
	}

	var callCount atomic.Int32
	run := func(ctx context.Context, provider, model string) (string, error) {
		callCount.Add(1)
		if provider == "anthropic" {
			return "", errors.New("connection timeout")
		}
		return "success", nil
	}

	result, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount.Load() != 2 {
		t.Errorf("should failover on timeout, got %d calls", callCount.Load())
	}
	if result.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", result.Provider, "openai")
	}
}

func TestRunWithModelFallback_NoCandidates(t *testing.T) {
	config := &FallbackConfig{}

	run := func(ctx context.Context, provider, model string) (string, error) {
		return "success", nil
	}

	_, err := RunWithModelFallback(context.Background(), config, run, nil)
	if err == nil {
		t.Fatal("expected error with no candidates")
	}
}

func TestRunWithImageModelFallback(t *testing.T) {
	config := &FallbackConfig{
		PrimaryProvider: "openai",
		PrimaryModel:    "gpt-4-vision",
		Fallbacks:       []string{"anthropic/claude-3"},
	}

	run := func(ctx context.Context, provider, model string) (string, error) {
		return "image processed", nil
	}

	result, err := RunWithImageModelFallback(context.Background(), config, run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Result != "image processed" {
		t.Errorf("Result = %q, want %q", result.Result, "image processed")
	}
}

func TestModelCandidate_String(t *testing.T) {
	c := ModelCandidate{Provider: "Anthropic", Model: "Claude-3"}
	if c.String() != "anthropic/claude-3" {
		t.Errorf("String() = %q, want %q", c.String(), "anthropic/claude-3")
	}
}

func TestClassifyErrorReason(t *testing.T) {
	tests := []struct {
		err      error
		expected string
	}{
		{nil, ReasonUnknown},
		{context.Canceled, ReasonAbort},
		{context.DeadlineExceeded, ReasonTimeout},
		{errors.New("rate limit exceeded"), ReasonRateLimit},
		{errors.New("429 too many requests"), ReasonRateLimit},
		{errors.New("unauthorized"), ReasonAuthError},
		{errors.New("invalid api key"), ReasonAuthError},
		{errors.New("billing quota exceeded"), ReasonBilling},
		{errors.New("payment required 402"), ReasonBilling},
		{errors.New("model not found"), ReasonUnavailable},
		{errors.New("content_filter triggered"), ReasonContentBlock},
		{errors.New("internal server error"), ReasonServerError},
		{errors.New("bad gateway 502"), ReasonServerError},
		{errors.New("invalid request"), ReasonInvalid},
		{errors.New("connection timeout"), ReasonTimeout},
		{errors.New("user abort"), ReasonAbort},
		{errors.New("random error"), ReasonUnknown},
	}

	for _, tt := range tests {
		result := classifyErrorReason(tt.err)
		if result != tt.expected {
			errStr := "nil"
			if tt.err != nil {
				errStr = tt.err.Error()
			}
			t.Errorf("classifyErrorReason(%q) = %q, want %q", errStr, result, tt.expected)
		}
	}
}

func TestNewFailoverError(t *testing.T) {
	cause := errors.New("connection failed")
	err := NewFailoverError(cause, "anthropic", "claude-3", ReasonTimeout)

	if err.Err != cause {
		t.Error("cause not preserved")
	}
	if err.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", err.Provider, "anthropic")
	}
	if err.Model != "claude-3" {
		t.Errorf("Model = %q, want %q", err.Model, "claude-3")
	}
	if err.Reason != ReasonTimeout {
		t.Errorf("Reason = %q, want %q", err.Reason, ReasonTimeout)
	}
}

func TestFailoverError_WithMethods(t *testing.T) {
	err := NewFailoverError(errors.New("test"), "p", "m", ReasonTimeout)

	err.WithStatus(504).WithCode("gateway_timeout")

	if err.Status != 504 {
		t.Errorf("Status = %d, want 504", err.Status)
	}
	if err.Code != "gateway_timeout" {
		t.Errorf("Code = %q, want %q", err.Code, "gateway_timeout")
	}
}

func TestFailoverError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := NewFailoverError(cause, "p", "m", ReasonTimeout)

	if !errors.Is(err, cause) {
		t.Error("Unwrap should allow errors.Is to find cause")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
