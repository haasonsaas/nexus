package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ToolExecConfig configures tool execution behavior including concurrency,
// timeouts, and retry settings.
type ToolExecConfig struct {
	// Concurrency is the maximum number of concurrent tool executions.
	// Default: 4.
	Concurrency int

	// PerToolTimeout is the timeout for individual tool executions.
	// Default: 30 seconds.
	PerToolTimeout time.Duration

	// MaxAttempts is the number of attempts per tool call (default 1).
	MaxAttempts int

	// RetryBackoff waits between retries.
	RetryBackoff time.Duration
}

// DefaultToolExecConfig returns sensible defaults for tool execution with
// 4 concurrent tools and 30 second timeout.
func DefaultToolExecConfig() ToolExecConfig {
	return ToolExecConfig{
		Concurrency:    4,
		PerToolTimeout: 30 * time.Second,
		MaxAttempts:    1,
		RetryBackoff:   0,
	}
}

// ToolExecutor handles concurrent tool execution with timeouts and retry logic.
type ToolExecutor struct {
	registry *ToolRegistry
	config   ToolExecConfig
}

// NewToolExecutor creates a new tool executor with the given registry and configuration.
// Default values are applied if config fields are zero.
func NewToolExecutor(registry *ToolRegistry, config ToolExecConfig) *ToolExecutor {
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	if config.PerToolTimeout <= 0 {
		config.PerToolTimeout = 30 * time.Second
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	return &ToolExecutor{
		registry: registry,
		config:   config,
	}
}

// ToolExecResult contains the result of a tool execution including timing and timeout information.
type ToolExecResult struct {
	Index     int
	ToolCall  models.ToolCall
	Result    models.ToolResult
	StartTime time.Time
	EndTime   time.Time
	TimedOut  bool
}

// EventCallback is a non-blocking callback invoked for tool lifecycle events during execution.
type EventCallback func(*models.RuntimeEvent)

// ExecuteConcurrently executes multiple tool calls with concurrency limits and timeouts.
// Results are returned in the same order as the input tool calls.
// The emit callback is called for lifecycle events (non-blocking, never blocks execution).
func (e *ToolExecutor) ExecuteConcurrently(ctx context.Context, toolCalls []models.ToolCall, emit EventCallback) []ToolExecResult {
	results := make([]ToolExecResult, len(toolCalls))

	// Semaphore for concurrency limiting
	sem := make(chan struct{}, e.config.Concurrency)
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, call models.ToolCall) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = ToolExecResult{
					Index:    idx,
					ToolCall: call,
					Result: models.ToolResult{
						ToolCallID: call.ID,
						Content:    "context canceled",
						IsError:    true,
					},
				}
				return
			}

			startTime := time.Now()
			var result models.ToolResult
			var timedOut bool
			maxAttempts := e.config.MaxAttempts
			if maxAttempts <= 0 {
				maxAttempts = 1
			}

			for attempt := 1; attempt <= maxAttempts; attempt++ {
				// Emit tool_started event
				if emit != nil {
					emit(models.NewToolEvent(models.EventToolStarted, call.Name, call.ID).
						WithMeta("attempt", attempt))
				}

				// Execute with timeout and add correlation ID
				toolCtx, cancel := context.WithTimeout(ctx, e.config.PerToolTimeout)
				toolCtx = observability.AddToolCallID(toolCtx, call.ID)
				result, timedOut = e.executeWithTimeout(toolCtx, call)
				cancel()

				if !result.IsError {
					break
				}

				if attempt < maxAttempts {
					if emit != nil {
						eventType := models.EventToolFailed
						if timedOut {
							eventType = models.EventToolTimeout
						}
						emit(models.NewToolEvent(eventType, call.Name, call.ID).
							WithMeta("attempt", attempt).
							WithMeta("retrying", true))
					}
					if e.config.RetryBackoff > 0 {
						canceled := false
						select {
						case <-time.After(e.config.RetryBackoff):
						case <-ctx.Done():
							result = models.ToolResult{
								ToolCallID: call.ID,
								Content:    "tool execution canceled",
								IsError:    true,
							}
							canceled = true
						}
						if canceled {
							break
						}
					}
				}
			}

			endTime := time.Now()

			results[idx] = ToolExecResult{
				Index:     idx,
				ToolCall:  call,
				Result:    result,
				StartTime: startTime,
				EndTime:   endTime,
				TimedOut:  timedOut,
			}

			// Emit completion event
			if emit != nil {
				var eventType models.RuntimeEventType
				if timedOut {
					eventType = models.EventToolTimeout
				} else if result.IsError {
					eventType = models.EventToolFailed
				} else {
					eventType = models.EventToolCompleted
				}
				event := models.NewToolEvent(eventType, call.Name, call.ID)
				event.WithMeta("duration_ms", endTime.Sub(startTime).Milliseconds())
				emit(event)
			}
		}(i, tc)
	}

	wg.Wait()
	return results
}

// executeWithTimeout executes a single tool call with timeout handling.
func (e *ToolExecutor) executeWithTimeout(ctx context.Context, call models.ToolCall) (models.ToolResult, bool) {
	type execResult struct {
		result *ToolResult
		err    error
	}

	resultChan := make(chan execResult, 1)

	go func() {
		result, err := e.registry.Execute(ctx, call.Name, call.Input)
		// Use non-blocking send to prevent goroutine leak if context is already done
		select {
		case resultChan <- execResult{result: result, err: err}:
		default:
			// Context cancelled/timed out before execution completed - log for observability
			runID := observability.GetRunID(ctx)
			sessionID := observability.GetSessionID(ctx)
			slog.Warn(
				"tool execution completed after timeout, result discarded",
				"tool", call.Name,
				"tool_call_id", call.ID,
				"run_id", runID,
				"session_id", sessionID,
			)
		}
	}()

	select {
	case <-ctx.Done():
		// Distinguish between timeout and cancellation
		var content string
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			content = fmt.Sprintf("tool execution timed out after %v", e.config.PerToolTimeout)
		} else {
			content = "tool execution canceled"
		}
		return models.ToolResult{
			ToolCallID: call.ID,
			Content:    content,
			IsError:    true,
		}, errors.Is(ctx.Err(), context.DeadlineExceeded)
	case res := <-resultChan:
		if res.err != nil {
			return models.ToolResult{
				ToolCallID: call.ID,
				Content:    res.err.Error(),
				IsError:    true,
			}, false
		}
		return models.ToolResult{
			ToolCallID: call.ID,
			Content:    res.result.Content,
			IsError:    res.result.IsError,
		}, false
	}
}

// ExecuteSequentially executes tool calls one at a time in order.
// Results are returned in the same order as the input calls.
func (e *ToolExecutor) ExecuteSequentially(ctx context.Context, toolCalls []models.ToolCall) []ToolExecResult {
	results := make([]ToolExecResult, len(toolCalls))

	for i, tc := range toolCalls {
		startTime := time.Now()
		maxAttempts := e.config.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 1
		}
		var result models.ToolResult
		var timedOut bool
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			toolCtx, cancel := context.WithTimeout(ctx, e.config.PerToolTimeout)
			toolCtx = observability.AddToolCallID(toolCtx, tc.ID)
			result, timedOut = e.executeWithTimeout(toolCtx, tc)
			cancel()
			if !result.IsError {
				break
			}
			if attempt < maxAttempts && e.config.RetryBackoff > 0 {
				select {
				case <-time.After(e.config.RetryBackoff):
				case <-ctx.Done():
					result = models.ToolResult{
						ToolCallID: tc.ID,
						Content:    "tool execution canceled",
						IsError:    true,
					}
					break
				}
			}
		}
		endTime := time.Now()

		results[i] = ToolExecResult{
			Index:     i,
			ToolCall:  tc,
			Result:    result,
			StartTime: startTime,
			EndTime:   endTime,
			TimedOut:  timedOut,
		}
	}

	return results
}

// ExecuteSingle executes a single tool call by name with timeout and retry logic.
func (e *ToolExecutor) ExecuteSingle(ctx context.Context, name string, input json.RawMessage) (*ToolResult, error) {
	maxAttempts := e.config.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		toolCtx, cancel := context.WithTimeout(ctx, e.config.PerToolTimeout)
		// Note: ExecuteSingle doesn't have a tool call ID, but the context
		// may already have one from the caller
		result, err := e.registry.Execute(toolCtx, name, input)
		cancel()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt < maxAttempts && e.config.RetryBackoff > 0 {
			select {
			case <-time.After(e.config.RetryBackoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}
