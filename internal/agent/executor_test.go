package agent

import (
	"context"
	"encoding/json"
	"errors"
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

func (m *mockTool) Name() string             { return m.name }
func (m *mockTool) Description() string      { return m.description }
func (m *mockTool) Schema() json.RawMessage  { return m.schema }
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
		Timeout: 100 * time.Millisecond,
		Retries: 5,
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
