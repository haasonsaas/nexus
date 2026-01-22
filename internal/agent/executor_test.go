package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// mockTool implements Tool for testing
type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
	execFunc    func(ctx context.Context, params json.RawMessage) (*ToolResult, error)
	execCount   atomic.Int32
}

func (m *mockTool) Name() string            { return m.name }
func (m *mockTool) Description() string     { return m.description }
func (m *mockTool) Schema() json.RawMessage { return m.schema }
func (m *mockTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	m.execCount.Add(1)
	if m.execFunc != nil {
		return m.execFunc(ctx, params)
	}
	return &ToolResult{Content: "success"}, nil
}

func TestExecutor_Execute_Success(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "test_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "result"}, nil
		},
	})

	executor := NewExecutor(registry, nil)
	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "test_tool",
		Input: json.RawMessage(`{}`),
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Result.Content != "result" {
		t.Errorf("content = %q, want %q", result.Result.Content, "result")
	}
	if result.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", result.Attempts)
	}
}

func TestExecutor_Execute_Retry(t *testing.T) {
	attempts := 0
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "flaky_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("timeout: connection timeout")
			}
			return &ToolResult{Content: "success"}, nil
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultRetries = 3
	config.RetryBackoff = 10 * time.Millisecond

	executor := NewExecutor(registry, config)
	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "flaky_tool",
		Input: json.RawMessage(`{}`),
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", result.Attempts)
	}
}

func TestExecutor_Execute_NonRetryable(t *testing.T) {
	attempts := 0
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "bad_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			attempts++
			return nil, errors.New("invalid input: missing required field")
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultRetries = 3

	executor := NewExecutor(registry, config)
	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "bad_tool",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("expected error")
	}
	// Should not retry invalid input errors
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry for non-retryable)", attempts)
	}
}

func TestExecutor_Execute_Timeout(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "slow_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			select {
			case <-time.After(5 * time.Second):
				return &ToolResult{Content: "done"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultTimeout = 50 * time.Millisecond
	config.DefaultRetries = 0 // Don't retry

	executor := NewExecutor(registry, config)
	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "slow_tool",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("expected timeout error")
	}
	if !IsToolError(result.Error) {
		t.Errorf("expected ToolError, got %T", result.Error)
	}
	toolErr, _ := GetToolError(result.Error)
	if toolErr.Type != ToolErrorTimeout {
		t.Errorf("type = %s, want timeout", toolErr.Type)
	}
}

func TestExecutor_ExecuteAll_Parallel(t *testing.T) {
	var running atomic.Int32
	var maxConcurrent atomic.Int32

	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "concurrent_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			current := running.Add(1)
			defer running.Add(-1)

			// Track max concurrent
			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			return &ToolResult{Content: "done"}, nil
		},
	})

	config := DefaultExecutorConfig()
	config.MaxConcurrency = 3

	executor := NewExecutor(registry, config)

	calls := make([]models.ToolCall, 5)
	for i := range calls {
		calls[i] = models.ToolCall{
			ID:    "call-" + string(rune('0'+i)),
			Name:  "concurrent_tool",
			Input: json.RawMessage(`{}`),
		}
	}

	results := executor.ExecuteAll(context.Background(), calls)

	if len(results) != 5 {
		t.Errorf("got %d results, want 5", len(results))
	}

	for i, r := range results {
		if r.Error != nil {
			t.Errorf("result %d: unexpected error: %v", i, r.Error)
		}
	}

	// Max concurrent should not exceed 3
	if maxConcurrent.Load() > 3 {
		t.Errorf("max concurrent = %d, want <= 3", maxConcurrent.Load())
	}
}

func TestExecutor_Backpressure(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "blocking_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			time.Sleep(100 * time.Millisecond)
			return &ToolResult{Content: "done"}, nil
		},
	})

	config := DefaultExecutorConfig()
	config.MaxConcurrency = 1

	executor := NewExecutor(registry, config)

	// Start one blocking call
	go executor.Execute(context.Background(), models.ToolCall{
		ID:    "blocking",
		Name:  "blocking_tool",
		Input: json.RawMessage(`{}`),
	})

	// Give it time to acquire the semaphore
	time.Sleep(10 * time.Millisecond)

	// Try another with short context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result := executor.Execute(ctx, models.ToolCall{
		ID:    "waiting",
		Name:  "blocking_tool",
		Input: json.RawMessage(`{}`),
	})

	// Should timeout waiting for semaphore
	if result.Error == nil {
		t.Fatal("expected error due to backpressure")
	}
}

func TestExecutor_Metrics(t *testing.T) {
	registry := NewToolRegistry()

	attempts := 0
	registry.Register(&mockTool{
		name: "flaky",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("timeout: first attempt")
			}
			return &ToolResult{Content: "ok"}, nil
		},
	})

	registry.Register(&mockTool{
		name: "failing",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return nil, errors.New("permanent failure")
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultRetries = 2
	config.RetryBackoff = time.Millisecond

	executor := NewExecutor(registry, config)

	// Successful with retry
	executor.Execute(context.Background(), models.ToolCall{
		ID:    "1",
		Name:  "flaky",
		Input: json.RawMessage(`{}`),
	})

	// Permanent failure
	executor.Execute(context.Background(), models.ToolCall{
		ID:    "2",
		Name:  "failing",
		Input: json.RawMessage(`{}`),
	})

	metrics := executor.Metrics()
	if metrics.TotalExecutions != 2 {
		t.Errorf("TotalExecutions = %d, want 2", metrics.TotalExecutions)
	}
	if metrics.TotalRetries != 1 {
		t.Errorf("TotalRetries = %d, want 1", metrics.TotalRetries)
	}
	if metrics.TotalFailures != 1 {
		t.Errorf("TotalFailures = %d, want 1", metrics.TotalFailures)
	}
}

func TestToolConfig(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "custom_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "ok"}, nil
		},
	})

	config := DefaultExecutorConfig()
	executor := NewExecutor(registry, config)

	// Configure tool with custom settings
	executor.ConfigureTool("custom_tool", &ToolConfig{
		Timeout:  100 * time.Millisecond,
		Retries:  5,
		Priority: 10,
	})

	tc := executor.getToolConfig("custom_tool")
	if tc == nil {
		t.Fatal("expected tool config")
	}
	if tc.Timeout != 100*time.Millisecond {
		t.Errorf("timeout = %v, want 100ms", tc.Timeout)
	}
	if tc.Retries != 5 {
		t.Errorf("retries = %d, want 5", tc.Retries)
	}
}

func TestExecutor_Execute_Panic(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "panicking_tool",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			panic("unexpected panic!")
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultRetries = 0
	executor := NewExecutor(registry, config)

	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "panicking_tool",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("expected error for panic")
	}

	toolErr, ok := GetToolError(result.Error)
	if !ok {
		t.Fatalf("expected ToolError, got %T", result.Error)
	}
	if toolErr.Type != ToolErrorPanic {
		t.Errorf("type = %s, want panic", toolErr.Type)
	}

	// Verify metrics
	metrics := executor.Metrics()
	if metrics.TotalPanics != 1 {
		t.Errorf("TotalPanics = %d, want 1", metrics.TotalPanics)
	}
}

func TestExecutor_Execute_ContextCancelDuringSemaphore(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "blocking",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			time.Sleep(time.Second)
			return &ToolResult{Content: "done"}, nil
		},
	})

	config := DefaultExecutorConfig()
	config.MaxConcurrency = 1
	executor := NewExecutor(registry, config)

	// Start blocking call
	go executor.Execute(context.Background(), models.ToolCall{
		ID:    "blocking",
		Name:  "blocking",
		Input: json.RawMessage(`{}`),
	})

	time.Sleep(10 * time.Millisecond)

	// Try another with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := executor.Execute(ctx, models.ToolCall{
		ID:    "waiting",
		Name:  "blocking",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestExecutor_Execute_ToolNotFound(t *testing.T) {
	registry := NewToolRegistry()
	config := DefaultExecutorConfig()
	executor := NewExecutor(registry, config)

	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "nonexistent",
		Input: json.RawMessage(`{}`),
	})

	// Result should contain error from registry
	if result.Result == nil {
		t.Fatal("expected result")
	}
	if !result.Result.IsError {
		t.Error("expected IsError=true")
	}
}

func TestExecutor_ExecuteAll_Empty(t *testing.T) {
	registry := NewToolRegistry()
	executor := NewExecutor(registry, nil)

	results := executor.ExecuteAll(context.Background(), nil)
	if results != nil {
		t.Error("expected nil for empty calls")
	}

	results = executor.ExecuteAll(context.Background(), []models.ToolCall{})
	if results != nil {
		t.Error("expected nil for empty slice")
	}
}

func TestExecutor_Execute_RetryBackoff(t *testing.T) {
	var callTimes []time.Time
	var mu sync.Mutex
	attempts := 0

	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "flaky",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			mu.Lock()
			callTimes = append(callTimes, time.Now())
			attempts++
			a := attempts
			mu.Unlock()

			if a < 3 {
				return nil, errors.New("timeout: temporary failure")
			}
			return &ToolResult{Content: "success"}, nil
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultRetries = 3
	config.RetryBackoff = 50 * time.Millisecond
	config.MaxRetryBackoff = 200 * time.Millisecond

	executor := NewExecutor(registry, config)

	start := time.Now()
	result := executor.Execute(context.Background(), models.ToolCall{
		ID:    "call-1",
		Name:  "flaky",
		Input: json.RawMessage(`{}`),
	})
	elapsed := time.Since(start)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Should have taken at least backoff time
	minExpected := 50*time.Millisecond + 100*time.Millisecond // 50ms + 100ms (exponential)
	if elapsed < minExpected/2 {
		t.Errorf("elapsed = %v, expected at least %v", elapsed, minExpected)
	}
}

func TestExecutor_Execute_ContextCancelDuringRetryBackoff(t *testing.T) {
	attempts := 0
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "always_fails",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			attempts++
			return nil, errors.New("timeout: always failing")
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultRetries = 10
	config.RetryBackoff = time.Second // Long backoff

	executor := NewExecutor(registry, config)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := executor.Execute(ctx, models.ToolCall{
		ID:    "call-1",
		Name:  "always_fails",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("expected error")
	}

	// Should have been cancelled during backoff, not completed all retries
	if attempts > 3 {
		t.Errorf("too many attempts (%d), should have been cancelled", attempts)
	}
}

func TestDefaultExecutorConfig(t *testing.T) {
	config := DefaultExecutorConfig()

	if config.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency = %d, want 5", config.MaxConcurrency)
	}
	if config.DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", config.DefaultTimeout)
	}
	if config.DefaultRetries != 2 {
		t.Errorf("DefaultRetries = %d, want 2", config.DefaultRetries)
	}
	if config.RetryBackoff != 100*time.Millisecond {
		t.Errorf("RetryBackoff = %v, want 100ms", config.RetryBackoff)
	}
	if config.MaxRetryBackoff != 5*time.Second {
		t.Errorf("MaxRetryBackoff = %v, want 5s", config.MaxRetryBackoff)
	}
}

func TestResultsToMessages(t *testing.T) {
	results := []*ExecutionResult{
		{
			ToolCallID: "call-1",
			Result:     &ToolResult{Content: "success"},
		},
		{
			ToolCallID: "call-2",
			Error:      errors.New("failed"),
		},
		{
			ToolCallID: "call-3",
			Result:     &ToolResult{Content: "error content", IsError: true},
		},
	}

	messages := ResultsToMessages(results)

	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}

	// First: success
	if messages[0].ToolCallID != "call-1" {
		t.Errorf("msg 0 ToolCallID = %q, want %q", messages[0].ToolCallID, "call-1")
	}
	if messages[0].Content != "success" {
		t.Errorf("msg 0 Content = %q, want %q", messages[0].Content, "success")
	}
	if messages[0].IsError {
		t.Error("msg 0 should not be error")
	}

	// Second: error from Error field
	if messages[1].ToolCallID != "call-2" {
		t.Errorf("msg 1 ToolCallID = %q, want %q", messages[1].ToolCallID, "call-2")
	}
	if !messages[1].IsError {
		t.Error("msg 1 should be error")
	}

	// Third: error from Result
	if !messages[2].IsError {
		t.Error("msg 2 should be error")
	}
}

func TestAnyErrors(t *testing.T) {
	noErrors := []*ExecutionResult{
		{ToolCallID: "1", Result: &ToolResult{Content: "ok"}},
		{ToolCallID: "2", Result: &ToolResult{Content: "ok"}},
	}

	if AnyErrors(noErrors) {
		t.Error("should return false when no errors")
	}

	withErrors := []*ExecutionResult{
		{ToolCallID: "1", Result: &ToolResult{Content: "ok"}},
		{ToolCallID: "2", Error: errors.New("failed")},
	}

	if !AnyErrors(withErrors) {
		t.Error("should return true when errors present")
	}

	if AnyErrors(nil) {
		t.Error("should return false for nil")
	}
}

func TestAsJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"json.RawMessage", json.RawMessage(`{"key":"value"}`), `{"key":"value"}`},
		{"[]byte", []byte(`{"key":"value"}`), `{"key":"value"}`},
		{"string", `{"key":"value"}`, `{"key":"value"}`},
		{"struct", struct{ Name string }{Name: "test"}, `{"Name":"test"}`},
		{"int", 42, `42`},
		{"nil", nil, `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AsJSON(tt.input)
			if string(result) != tt.expected {
				t.Errorf("AsJSON() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestExecutor_NilConfig(t *testing.T) {
	registry := NewToolRegistry()
	executor := NewExecutor(registry, nil)

	// Should use defaults
	if executor.config.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency = %d, want 5 (default)", executor.config.MaxConcurrency)
	}
}

func TestExecutor_GetToolConfig_NotFound(t *testing.T) {
	registry := NewToolRegistry()
	executor := NewExecutor(registry, nil)

	tc := executor.getToolConfig("nonexistent")
	if tc != nil {
		t.Error("expected nil for unconfigured tool")
	}
}

func TestExecutor_ToolConfigOverridesDefaults(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{
		name: "custom",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "ok"}, nil
		},
	})

	config := DefaultExecutorConfig()
	config.DefaultTimeout = 5 * time.Second
	config.DefaultRetries = 1

	executor := NewExecutor(registry, config)

	// Configure tool with different settings
	executor.ConfigureTool("custom", &ToolConfig{
		Timeout:      1 * time.Second,
		Retries:      0, // Zero retries explicitly set
		RetryBackoff: 10 * time.Millisecond,
	})

	tc := executor.getToolConfig("custom")
	if tc.Timeout != 1*time.Second {
		t.Errorf("Timeout = %v, want 1s", tc.Timeout)
	}
	// Note: Zero value for Retries means use default in the actual implementation
}
