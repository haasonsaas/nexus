package agent

import (
	"errors"
	"testing"
)

func TestToolErrorType_IsRetryable(t *testing.T) {
	tests := []struct {
		typ  ToolErrorType
		want bool
	}{
		{ToolErrorTimeout, true},
		{ToolErrorNetwork, true},
		{ToolErrorRateLimit, true},
		{ToolErrorNotFound, false},
		{ToolErrorInvalidInput, false},
		{ToolErrorPermission, false},
		{ToolErrorExecution, false},
		{ToolErrorPanic, false},
		{ToolErrorUnknown, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.typ), func(t *testing.T) {
			if got := tt.typ.IsRetryable(); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolError_Error(t *testing.T) {
	err := NewToolError("test_tool", errors.New("connection refused")).
		WithType(ToolErrorNetwork).
		WithToolCallID("call-123").
		WithAttempts(3)

	errStr := err.Error()
	if errStr == "" {
		t.Error("error string should not be empty")
	}

	// Should contain key information
	tests := []string{"tool:network", "test_tool", "attempts=3"}
	for _, want := range tests {
		if !contains(errStr, want) {
			t.Errorf("error string %q should contain %q", errStr, want)
		}
	}
}

func TestNewToolError_Classification(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		wantType ToolErrorType
	}{
		{"timeout", "context deadline exceeded", ToolErrorTimeout},
		{"network", "connection refused", ToolErrorNetwork},
		{"rate_limit", "rate limit exceeded", ToolErrorRateLimit},
		{"permission", "permission denied", ToolErrorPermission},
		{"invalid", "invalid input parameter", ToolErrorInvalidInput},
		{"unknown", "some random error", ToolErrorExecution},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewToolError("tool", errors.New(tt.errMsg))
			if err.Type != tt.wantType {
				t.Errorf("Type = %s, want %s", err.Type, tt.wantType)
			}
		})
	}
}

func TestToolError_Unwrap(t *testing.T) {
	cause := errors.New("underlying cause")
	err := NewToolError("tool", cause)

	if !errors.Is(err, cause) {
		t.Error("should unwrap to underlying cause")
	}
}

func TestIsToolError(t *testing.T) {
	toolErr := NewToolError("tool", errors.New("test"))
	regularErr := errors.New("regular error")

	if !IsToolError(toolErr) {
		t.Error("should recognize ToolError")
	}
	if IsToolError(regularErr) {
		t.Error("should not recognize regular error as ToolError")
	}
}

func TestGetToolError(t *testing.T) {
	toolErr := NewToolError("tool", errors.New("test"))

	got, ok := GetToolError(toolErr)
	if !ok {
		t.Fatal("should extract ToolError")
	}
	if got.ToolName != "tool" {
		t.Errorf("ToolName = %q, want %q", got.ToolName, "tool")
	}
}

func TestIsToolRetryable(t *testing.T) {
	retryable := NewToolError("tool", errors.New("timeout")).WithType(ToolErrorTimeout)
	nonRetryable := NewToolError("tool", errors.New("invalid")).WithType(ToolErrorInvalidInput)

	if !IsToolRetryable(retryable) {
		t.Error("timeout error should be retryable")
	}
	if IsToolRetryable(nonRetryable) {
		t.Error("invalid input error should not be retryable")
	}

	// Test with raw errors
	if !IsToolRetryable(errors.New("connection timeout")) {
		t.Error("raw timeout error should be retryable")
	}
}

func TestLoopError(t *testing.T) {
	cause := errors.New("provider error")
	err := &LoopError{
		Phase:     PhaseStream,
		Iteration: 3,
		Message:   "streaming failed",
		Cause:     cause,
	}

	errStr := err.Error()
	if !contains(errStr, "stream") {
		t.Errorf("error should contain phase: %s", errStr)
	}
	if !contains(errStr, "3") {
		t.Errorf("error should contain iteration: %s", errStr)
	}
	if !contains(errStr, "streaming failed") {
		t.Errorf("error should contain message: %s", errStr)
	}

	if !errors.Is(err, cause) {
		t.Error("should unwrap to cause")
	}
}

func TestLoopPhases(t *testing.T) {
	phases := []LoopPhase{
		PhaseInit,
		PhaseStream,
		PhaseExecuteTools,
		PhaseContinue,
		PhaseComplete,
	}

	for _, p := range phases {
		if string(p) == "" {
			t.Errorf("phase %v should have string representation", p)
		}
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrMaxIterations,
		ErrContextCancelled,
		ErrNoProvider,
		ErrToolNotFound,
		ErrToolTimeout,
		ErrToolPanic,
		ErrBackpressure,
	}

	for _, err := range sentinels {
		if err == nil {
			t.Error("sentinel error should not be nil")
		}
		if err.Error() == "" {
			t.Errorf("sentinel %v should have message", err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
