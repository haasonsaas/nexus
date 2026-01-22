package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ExecutorConfig configures the parallel tool executor behavior including
// concurrency limits, timeouts, and retry strategies.
type ExecutorConfig struct {
	// MaxConcurrency limits the number of parallel tool executions
	// Default: 5
	MaxConcurrency int

	// DefaultTimeout is the default timeout for tool execution
	// Default: 30s
	DefaultTimeout time.Duration

	// DefaultRetries is the default number of retries for retryable errors
	// Default: 2
	DefaultRetries int

	// RetryBackoff is the initial backoff duration between retries
	// Default: 100ms
	RetryBackoff time.Duration

	// MaxRetryBackoff caps the exponential backoff
	// Default: 5s
	MaxRetryBackoff time.Duration
}

// DefaultExecutorConfig returns the default executor configuration.
func DefaultExecutorConfig() *ExecutorConfig {
	return &ExecutorConfig{
		MaxConcurrency:  5,
		DefaultTimeout:  30 * time.Second,
		DefaultRetries:  2,
		RetryBackoff:    100 * time.Millisecond,
		MaxRetryBackoff: 5 * time.Second,
	}
}

// ToolConfig holds per-tool configuration overrides for timeout, retry, and priority settings.
type ToolConfig struct {
	// Timeout overrides the default timeout for this tool
	Timeout time.Duration

	// Retries overrides the default retries for this tool
	Retries int

	// RetryBackoff overrides the initial backoff for this tool
	RetryBackoff time.Duration

	// Priority affects execution order (higher = first)
	// Default: 0
	Priority int
}

// Executor manages parallel tool execution with retry and backpressure handling.
// It provides concurrency limiting via semaphores and tracks execution metrics.
type Executor struct {
	registry   *ToolRegistry
	config     *ExecutorConfig
	toolConfig map[string]*ToolConfig
	mu         sync.RWMutex

	// Semaphore for concurrency limiting
	sem chan struct{}

	// Metrics
	metrics *ExecutorMetrics
}

// ExecutorMetrics tracks executor performance metrics including execution counts,
// retries, failures, timeouts, and panics.
type ExecutorMetrics struct {
	mu              sync.Mutex
	TotalExecutions int64
	TotalRetries    int64
	TotalFailures   int64
	TotalTimeouts   int64
	TotalPanics     int64
}

// NewExecutor creates a new parallel tool executor with the given registry and configuration.
// If config is nil, DefaultExecutorConfig is used.
func NewExecutor(registry *ToolRegistry, config *ExecutorConfig) *Executor {
	if config == nil {
		config = DefaultExecutorConfig()
	}

	return &Executor{
		registry:   registry,
		config:     config,
		toolConfig: make(map[string]*ToolConfig),
		sem:        make(chan struct{}, config.MaxConcurrency),
		metrics:    &ExecutorMetrics{},
	}
}

// ConfigureTool sets per-tool configuration overrides for the named tool.
func (e *Executor) ConfigureTool(name string, config *ToolConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.toolConfig[name] = config
}

// GetToolConfig returns the configuration for a tool.
func (e *Executor) getToolConfig(name string) *ToolConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if tc, ok := e.toolConfig[name]; ok {
		return tc
	}
	return nil
}

// ExecutionResult holds the result of a single tool execution including
// timing information and retry attempts.
type ExecutionResult struct {
	ToolCallID string
	ToolName   string
	Result     *ToolResult
	Error      error
	Duration   time.Duration
	Attempts   int
}

// ExecuteAll executes multiple tool calls in parallel with concurrency limits.
// Results are returned in the same order as the input calls.
func (e *Executor) ExecuteAll(ctx context.Context, calls []models.ToolCall) []*ExecutionResult {
	if len(calls) == 0 {
		return nil
	}

	results := make([]*ExecutionResult, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc models.ToolCall) {
			defer wg.Done()
			results[idx] = e.Execute(ctx, tc)
		}(i, call)
	}

	wg.Wait()
	return results
}

// Execute executes a single tool call with retry logic and timeout handling.
// Acquires a semaphore slot for backpressure control before execution.
func (e *Executor) Execute(ctx context.Context, call models.ToolCall) *ExecutionResult {
	start := time.Now()
	result := &ExecutionResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Attempts:   0,
	}

	// Acquire semaphore for backpressure
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		result.Error = NewToolError(call.Name, ctx.Err()).
			WithType(ToolErrorTimeout).
			WithToolCallID(call.ID)
		result.Duration = time.Since(start)
		return result
	}

	// Get tool config
	tc := e.getToolConfig(call.Name)
	timeout := e.config.DefaultTimeout
	maxRetries := e.config.DefaultRetries
	backoff := e.config.RetryBackoff

	if tc != nil {
		if tc.Timeout > 0 {
			timeout = tc.Timeout
		}
		if tc.Retries >= 0 {
			maxRetries = tc.Retries
		}
		if tc.RetryBackoff > 0 {
			backoff = tc.RetryBackoff
		}
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result.Attempts = attempt + 1

		// Execute with timeout
		execResult, execErr := e.executeWithTimeout(ctx, call, timeout)

		if execErr == nil {
			result.Result = execResult
			result.Duration = time.Since(start)

			e.metrics.mu.Lock()
			e.metrics.TotalExecutions++
			if attempt > 0 {
				e.metrics.TotalRetries += int64(attempt)
			}
			e.metrics.mu.Unlock()

			return result
		}

		lastErr = execErr

		// Check if error is retryable
		if !IsToolRetryable(execErr) {
			break
		}

		// Don't retry if context is done
		if ctx.Err() != nil {
			break
		}

		// Don't retry on last attempt
		if attempt >= maxRetries {
			break
		}

		// Exponential backoff
		sleepDuration := backoff * time.Duration(1<<uint(attempt))
		if sleepDuration > e.config.MaxRetryBackoff {
			sleepDuration = e.config.MaxRetryBackoff
		}

		select {
		case <-time.After(sleepDuration):
			// Continue to next attempt
		case <-ctx.Done():
			lastErr = NewToolError(call.Name, ctx.Err()).
				WithType(ToolErrorTimeout).
				WithToolCallID(call.ID)
			break
		}
	}

	result.Error = lastErr
	result.Duration = time.Since(start)

	e.metrics.mu.Lock()
	e.metrics.TotalExecutions++
	e.metrics.TotalFailures++
	if toolErr, ok := GetToolError(lastErr); ok {
		if toolErr.Type == ToolErrorTimeout {
			e.metrics.TotalTimeouts++
		} else if toolErr.Type == ToolErrorPanic {
			e.metrics.TotalPanics++
		}
	}
	e.metrics.mu.Unlock()

	return result
}

// executeWithTimeout executes a tool call with a timeout.
func (e *Executor) executeWithTimeout(ctx context.Context, call models.ToolCall, timeout time.Duration) (*ToolResult, error) {
	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel for result
	type execResult struct {
		result *ToolResult
		err    error
	}
	resultCh := make(chan execResult, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				err := NewToolError(call.Name, fmt.Errorf("panic: %v\n%s", r, stack)).
					WithType(ToolErrorPanic).
					WithToolCallID(call.ID)
				resultCh <- execResult{err: err}
			}
		}()

		result, err := e.registry.Execute(execCtx, call.Name, call.Input)
		if err != nil {
			toolErr := NewToolError(call.Name, err).WithToolCallID(call.ID)
			resultCh <- execResult{err: toolErr}
			return
		}
		resultCh <- execResult{result: result}
	}()

	select {
	case res := <-resultCh:
		return res.result, res.err
	case <-execCtx.Done():
		if ctx.Err() != nil {
			// Parent context cancelled
			return nil, NewToolError(call.Name, ctx.Err()).
				WithType(ToolErrorTimeout).
				WithToolCallID(call.ID).
				WithMessage("context cancelled")
		}
		// Timeout
		return nil, NewToolError(call.Name, ErrToolTimeout).
			WithType(ToolErrorTimeout).
			WithToolCallID(call.ID).
			WithMessage(fmt.Sprintf("execution timed out after %s", timeout))
	}
}

// Metrics returns a copy-safe snapshot of the executor metrics.
func (e *Executor) Metrics() *ExecutorMetricsSnapshot {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()
	return &ExecutorMetricsSnapshot{
		TotalExecutions: e.metrics.TotalExecutions,
		TotalRetries:    e.metrics.TotalRetries,
		TotalFailures:   e.metrics.TotalFailures,
		TotalTimeouts:   e.metrics.TotalTimeouts,
		TotalPanics:     e.metrics.TotalPanics,
	}
}

// ExecutorMetricsSnapshot is a thread-safe copy of executor metrics at a point in time.
type ExecutorMetricsSnapshot struct {
	TotalExecutions int64
	TotalRetries    int64
	TotalFailures   int64
	TotalTimeouts   int64
	TotalPanics     int64
}

// ResultsToMessages converts execution results to tool result messages suitable for
// including in conversation history.
func ResultsToMessages(results []*ExecutionResult) []models.ToolResult {
	toolResults := make([]models.ToolResult, len(results))

	for i, r := range results {
		if r.Error != nil {
			toolResults[i] = models.ToolResult{
				ToolCallID: r.ToolCallID,
				Content:    r.Error.Error(),
				IsError:    true,
			}
		} else if r.Result != nil {
			toolResults[i] = models.ToolResult{
				ToolCallID: r.ToolCallID,
				Content:    r.Result.Content,
				IsError:    r.Result.IsError,
			}
		}
	}

	return toolResults
}

// AnyErrors returns true if any execution result contains an error or failure.
func AnyErrors(results []*ExecutionResult) bool {
	for _, r := range results {
		if r.Error != nil {
			return true
		}
	}
	return false
}

// AsJSON converts tool input to JSON if it is not already a json.RawMessage, []byte, or string.
func AsJSON(input any) json.RawMessage {
	switch v := input.(type) {
	case json.RawMessage:
		return v
	case []byte:
		return json.RawMessage(v)
	case string:
		return json.RawMessage(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return json.RawMessage("null")
		}
		return data
	}
}
