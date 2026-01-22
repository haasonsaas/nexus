package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// testExecTool implements Tool for testing tool execution.
type testExecTool struct {
	name     string
	execFunc func(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}

func (m *testExecTool) Name() string            { return m.name }
func (m *testExecTool) Description() string     { return "test exec tool" }
func (m *testExecTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (m *testExecTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	return m.execFunc(ctx, params)
}

func TestExecuteConcurrently_RespectsConcurrencyLimit(t *testing.T) {
	const maxConcurrency = 2
	const numTools = 6

	// Track concurrent execution count
	var concurrent int32
	var maxConcurrent int32
	var mu sync.Mutex

	// Create a tool that tracks concurrency
	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "blocking",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			// Increment concurrent counter
			current := atomic.AddInt32(&concurrent, 1)

			// Track max concurrent
			mu.Lock()
			if current > maxConcurrent {
				maxConcurrent = current
			}
			mu.Unlock()

			// Simulate work
			time.Sleep(50 * time.Millisecond)

			// Decrement counter
			atomic.AddInt32(&concurrent, -1)

			return &ToolResult{Content: "done"}, nil
		},
	})

	config := ToolExecConfig{
		Concurrency:    maxConcurrency,
		PerToolTimeout: 5 * time.Second,
	}
	executor := NewToolExecutor(registry, config)

	// Create multiple tool calls
	toolCalls := make([]models.ToolCall, numTools)
	for i := 0; i < numTools; i++ {
		toolCalls[i] = models.ToolCall{
			ID:    string(rune('a' + i)),
			Name:  "blocking",
			Input: json.RawMessage(`{}`),
		}
	}

	// Execute concurrently
	results := executor.ExecuteConcurrently(context.Background(), toolCalls, nil)

	// Verify all completed
	if len(results) != numTools {
		t.Errorf("got %d results, want %d", len(results), numTools)
	}

	// Verify concurrency was limited
	if maxConcurrent > int32(maxConcurrency) {
		t.Errorf("max concurrent was %d, should not exceed %d", maxConcurrent, maxConcurrency)
	}

	// Verify all succeeded
	for i, r := range results {
		if r.Result.IsError {
			t.Errorf("result %d failed: %s", i, r.Result.Content)
		}
	}
}

func TestExecuteConcurrently_TimesOut(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "slow",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			// Block until context is cancelled
			<-ctx.Done()
			return &ToolResult{Content: "should not reach"}, nil
		},
	})

	config := ToolExecConfig{
		Concurrency:    4,
		PerToolTimeout: 50 * time.Millisecond, // Very short timeout
	}
	executor := NewToolExecutor(registry, config)

	toolCalls := []models.ToolCall{
		{ID: "1", Name: "slow", Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := executor.ExecuteConcurrently(context.Background(), toolCalls, nil)
	elapsed := time.Since(start)

	// Should complete around the timeout, not block forever
	if elapsed > 200*time.Millisecond {
		t.Errorf("took %v, expected to timeout around 50ms", elapsed)
	}

	// Verify timeout
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	r := results[0]
	if !r.TimedOut {
		t.Error("expected TimedOut to be true")
	}
	if !r.Result.IsError {
		t.Error("expected IsError to be true for timeout")
	}
	if r.Result.Content == "" {
		t.Error("expected timeout error message")
	}
}

func TestExecuteConcurrently_PreservesOrder(t *testing.T) {
	registry := NewToolRegistry()

	// Create tools with different execution times to force out-of-order completion
	registry.Register(&testExecTool{
		name: "tool_slow",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			time.Sleep(100 * time.Millisecond)
			return &ToolResult{Content: "slow"}, nil
		},
	})
	registry.Register(&testExecTool{
		name: "tool_fast",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			time.Sleep(10 * time.Millisecond)
			return &ToolResult{Content: "fast"}, nil
		},
	})

	config := ToolExecConfig{
		Concurrency:    4,
		PerToolTimeout: 5 * time.Second,
	}
	executor := NewToolExecutor(registry, config)

	// Call slow first, then fast - fast will complete first
	toolCalls := []models.ToolCall{
		{ID: "0", Name: "tool_slow", Input: json.RawMessage(`{}`)},
		{ID: "1", Name: "tool_fast", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "tool_slow", Input: json.RawMessage(`{}`)},
		{ID: "3", Name: "tool_fast", Input: json.RawMessage(`{}`)},
	}

	results := executor.ExecuteConcurrently(context.Background(), toolCalls, nil)

	// Verify results are in input order, not completion order
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}

	// Check each result has correct index and content
	for i, r := range results {
		if r.Index != i {
			t.Errorf("result[%d].Index = %d, want %d", i, r.Index, i)
		}
		if r.ToolCall.ID != toolCalls[i].ID {
			t.Errorf("result[%d].ToolCall.ID = %s, want %s", i, r.ToolCall.ID, toolCalls[i].ID)
		}

		expectedContent := "slow"
		if i%2 == 1 {
			expectedContent = "fast"
		}
		if r.Result.Content != expectedContent {
			t.Errorf("result[%d].Content = %q, want %q", i, r.Result.Content, expectedContent)
		}
	}
}

func TestExecuteConcurrently_EmitsEvents(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "simple",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "ok"}, nil
		},
	})
	registry.Register(&testExecTool{
		name: "failing",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: "error", IsError: true}, nil
		},
	})

	config := ToolExecConfig{
		Concurrency:    4,
		PerToolTimeout: 5 * time.Second,
	}
	executor := NewToolExecutor(registry, config)

	toolCalls := []models.ToolCall{
		{ID: "1", Name: "simple", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "failing", Input: json.RawMessage(`{}`)},
	}

	var events []*models.RuntimeEvent
	var mu sync.Mutex
	emit := func(e *models.RuntimeEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	executor.ExecuteConcurrently(context.Background(), toolCalls, emit)

	// Wait a bit for events to be collected
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have 4 events: 2 started + 2 completed/failed
	if len(events) != 4 {
		t.Errorf("got %d events, want 4", len(events))
	}

	// Count event types
	startedCount := 0
	completedCount := 0
	failedCount := 0
	for _, e := range events {
		switch e.Type {
		case models.EventToolStarted:
			startedCount++
		case models.EventToolCompleted:
			completedCount++
		case models.EventToolFailed:
			failedCount++
		}
	}

	if startedCount != 2 {
		t.Errorf("started events = %d, want 2", startedCount)
	}
	if completedCount != 1 {
		t.Errorf("completed events = %d, want 1", completedCount)
	}
	if failedCount != 1 {
		t.Errorf("failed events = %d, want 1", failedCount)
	}
}

func TestExecuteConcurrently_ContextCancellation(t *testing.T) {
	registry := NewToolRegistry()

	toolStarted := make(chan struct{})
	registry.Register(&testExecTool{
		name: "blocking",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			close(toolStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	config := ToolExecConfig{
		Concurrency:    4,
		PerToolTimeout: 5 * time.Second,
	}
	executor := NewToolExecutor(registry, config)

	ctx, cancel := context.WithCancel(context.Background())

	toolCalls := []models.ToolCall{
		{ID: "1", Name: "blocking", Input: json.RawMessage(`{}`)},
	}

	done := make(chan []ToolExecResult)
	go func() {
		done <- executor.ExecuteConcurrently(ctx, toolCalls, nil)
	}()

	// Wait for tool to start, then cancel
	<-toolStarted
	cancel()

	results := <-done

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	r := results[0]
	if !r.Result.IsError {
		t.Error("expected IsError for cancelled context")
	}
	// Should not be marked as timeout since it was cancellation
	if r.TimedOut {
		t.Error("TimedOut should be false for cancellation")
	}
}

func TestExecuteSequentially_Basic(t *testing.T) {
	registry := NewToolRegistry()

	var order []string
	var mu sync.Mutex

	registry.Register(&testExecTool{
		name: "tool_a",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			mu.Lock()
			order = append(order, "a")
			mu.Unlock()
			return &ToolResult{Content: "a"}, nil
		},
	})
	registry.Register(&testExecTool{
		name: "tool_b",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			mu.Lock()
			order = append(order, "b")
			mu.Unlock()
			return &ToolResult{Content: "b"}, nil
		},
	})

	config := DefaultToolExecConfig()
	executor := NewToolExecutor(registry, config)

	toolCalls := []models.ToolCall{
		{ID: "1", Name: "tool_a", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "tool_b", Input: json.RawMessage(`{}`)},
	}

	results := executor.ExecuteSequentially(context.Background(), toolCalls)

	// Verify sequential execution order
	mu.Lock()
	defer mu.Unlock()

	if len(order) != 2 {
		t.Fatalf("got %d executions, want 2", len(order))
	}
	if order[0] != "a" || order[1] != "b" {
		t.Errorf("execution order = %v, want [a b]", order)
	}

	// Verify results
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Result.Content != "a" {
		t.Errorf("result[0] = %q, want %q", results[0].Result.Content, "a")
	}
	if results[1].Result.Content != "b" {
		t.Errorf("result[1] = %q, want %q", results[1].Result.Content, "b")
	}
}

func TestExecuteSingle_Success(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&testExecTool{
		name: "echo",
		execFunc: func(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
			return &ToolResult{Content: string(params)}, nil
		},
	})

	config := DefaultToolExecConfig()
	executor := NewToolExecutor(registry, config)

	result, err := executor.ExecuteSingle(context.Background(), "echo", json.RawMessage(`"hello"`))

	if err != nil {
		t.Fatalf("ExecuteSingle failed: %v", err)
	}
	if result.Content != `"hello"` {
		t.Errorf("Content = %q, want %q", result.Content, `"hello"`)
	}
}

func TestExecuteSingle_ToolNotFound(t *testing.T) {
	registry := NewToolRegistry()
	config := DefaultToolExecConfig()
	executor := NewToolExecutor(registry, config)

	result, err := executor.ExecuteSingle(context.Background(), "nonexistent", json.RawMessage(`{}`))

	// Registry returns result with IsError=true, not an error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if !result.IsError {
		t.Error("expected IsError=true for nonexistent tool")
	}
}

func TestDefaultToolExecConfig(t *testing.T) {
	config := DefaultToolExecConfig()

	if config.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", config.Concurrency)
	}
	if config.PerToolTimeout != 30*time.Second {
		t.Errorf("PerToolTimeout = %v, want 30s", config.PerToolTimeout)
	}
}

func TestNewToolExecutor_DefaultsZeroValues(t *testing.T) {
	registry := NewToolRegistry()

	// Pass zero values
	executor := NewToolExecutor(registry, ToolExecConfig{})

	// Should default to sensible values
	if executor.config.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", executor.config.Concurrency)
	}
	if executor.config.PerToolTimeout != 30*time.Second {
		t.Errorf("PerToolTimeout = %v, want 30s", executor.config.PerToolTimeout)
	}
}
