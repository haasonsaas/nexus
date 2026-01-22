package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ToolExecConfig configures tool execution behavior.
type ToolExecConfig struct {
	// Concurrency is the maximum number of concurrent tool executions.
	// Default: 4.
	Concurrency int

	// PerToolTimeout is the timeout for individual tool executions.
	// Default: 30 seconds.
	PerToolTimeout time.Duration
}

// DefaultToolExecConfig returns sensible defaults for tool execution.
func DefaultToolExecConfig() ToolExecConfig {
	return ToolExecConfig{
		Concurrency:    4,
		PerToolTimeout: 30 * time.Second,
	}
}

// ToolExecutor handles concurrent tool execution with timeouts.
type ToolExecutor struct {
	registry *ToolRegistry
	config   ToolExecConfig
}

// NewToolExecutor creates a new tool executor.
func NewToolExecutor(registry *ToolRegistry, config ToolExecConfig) *ToolExecutor {
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	if config.PerToolTimeout <= 0 {
		config.PerToolTimeout = 30 * time.Second
	}
	return &ToolExecutor{
		registry: registry,
		config:   config,
	}
}

// ToolExecResult contains the result of a tool execution.
type ToolExecResult struct {
	Index      int
	ToolCall   models.ToolCall
	Result     models.ToolResult
	StartTime  time.Time
	EndTime    time.Time
	TimedOut   bool
}

// ExecuteConcurrently executes multiple tool calls with concurrency limits and timeouts.
// Results are returned in the same order as the input tool calls.
func (e *ToolExecutor) ExecuteConcurrently(ctx context.Context, toolCalls []models.ToolCall, eventChan chan<- *models.RuntimeEvent) []ToolExecResult {
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

			// Emit tool_started event
			if eventChan != nil {
				eventChan <- models.NewToolEvent(models.EventToolStarted, call.Name, call.ID)
			}

			// Execute with timeout
			startTime := time.Now()
			toolCtx, cancel := context.WithTimeout(ctx, e.config.PerToolTimeout)
			result, timedOut := e.executeWithTimeout(toolCtx, call)
			cancel()
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
			if eventChan != nil {
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
				eventChan <- event
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
		resultChan <- execResult{result: result, err: err}
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

// ExecuteSequentially executes tool calls one at a time (for backward compatibility).
func (e *ToolExecutor) ExecuteSequentially(ctx context.Context, toolCalls []models.ToolCall) []ToolExecResult {
	results := make([]ToolExecResult, len(toolCalls))

	for i, tc := range toolCalls {
		startTime := time.Now()
		toolCtx, cancel := context.WithTimeout(ctx, e.config.PerToolTimeout)
		result, timedOut := e.executeWithTimeout(toolCtx, tc)
		cancel()
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

// ExecuteSingle executes a single tool call with timeout.
func (e *ToolExecutor) ExecuteSingle(ctx context.Context, name string, input json.RawMessage) (*ToolResult, error) {
	toolCtx, cancel := context.WithTimeout(ctx, e.config.PerToolTimeout)
	defer cancel()
	return e.registry.Execute(toolCtx, name, input)
}
