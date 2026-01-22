package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// EventEmitter generates and dispatches AgentEvents with proper sequencing.
// It provides a bridge between the agent runtime and both streaming channels and plugins.
type EventEmitter struct {
	runID    string
	sequence uint64 // atomic counter for monotonic sequencing

	// Current context
	turnIndex int
	iterIndex int

	// Sink for event dispatch (can be plugin registry, channel, or multi-sink)
	sink EventSink
}

// NewEventEmitter creates a new event emitter for an agent run with the given sink.
// If sink is nil, a NopSink is used.
func NewEventEmitter(runID string, sink EventSink) *EventEmitter {
	if sink == nil {
		sink = NopSink{}
	}
	return &EventEmitter{
		runID: runID,
		sink:  sink,
	}
}

// NewEventEmitterWithPlugins creates a new event emitter that dispatches to a plugin registry.
// This is a convenience constructor that wraps the registry in a PluginSink.
func NewEventEmitterWithPlugins(runID string, plugins *PluginRegistry) *EventEmitter {
	return NewEventEmitter(runID, NewPluginSink(plugins))
}

// SetTurn updates the current turn index for subsequent events.
func (e *EventEmitter) SetTurn(turnIndex int) {
	e.turnIndex = turnIndex
}

// SetIter updates the current iteration index for subsequent events.
func (e *EventEmitter) SetIter(iterIndex int) {
	e.iterIndex = iterIndex
}

// nextSeq returns the next sequence number (atomic, monotonic).
func (e *EventEmitter) nextSeq() uint64 {
	return atomic.AddUint64(&e.sequence, 1)
}

// base creates the base event with common fields populated.
func (e *EventEmitter) base(eventType models.AgentEventType) models.AgentEvent {
	return models.AgentEvent{
		Version:   1,
		Type:      eventType,
		Time:      time.Now(),
		Sequence:  e.nextSeq(),
		RunID:     e.runID,
		TurnIndex: e.turnIndex,
		IterIndex: e.iterIndex,
	}
}

// emit dispatches the event to the configured sink.
func (e *EventEmitter) emit(ctx context.Context, event models.AgentEvent) {
	if e.sink != nil {
		e.sink.Emit(ctx, event)
	}
}

// RunStarted emits a run.started event indicating the agent run has begun.
func (e *EventEmitter) RunStarted(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventRunStarted)
	e.emit(ctx, event)
	return event
}

// RunFinished emits a run.finished event with accumulated run statistics.
func (e *EventEmitter) RunFinished(ctx context.Context, stats *models.RunStats) models.AgentEvent {
	event := e.base(models.AgentEventRunFinished)
	if stats != nil {
		event.Stats = &models.StatsEventPayload{Run: stats}
	}
	e.emit(ctx, event)
	return event
}

// RunError emits a run.error event with the given error and retriability flag.
func (e *EventEmitter) RunError(ctx context.Context, err error, retriable bool) models.AgentEvent {
	event := e.base(models.AgentEventRunError)
	event.Error = &models.ErrorEventPayload{
		Message:   err.Error(),
		Retriable: retriable,
		Err:       err, // Preserve original error for errors.Is/errors.As
	}
	e.emit(ctx, event)
	return event
}

// RunCancelled emits a run.cancelled event when the context is explicitly cancelled.
func (e *EventEmitter) RunCancelled(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventRunCancelled)
	event.Error = &models.ErrorEventPayload{
		Message:   "run cancelled",
		Retriable: true,
		Err:       ErrContextCancelled,
	}
	e.emit(ctx, event)
	return event
}

// RunTimedOut emits a run.timed_out event when the wall time limit is exceeded.
func (e *EventEmitter) RunTimedOut(ctx context.Context, limit time.Duration) models.AgentEvent {
	event := e.base(models.AgentEventRunTimedOut)
	event.Error = &models.ErrorEventPayload{
		Message:   fmt.Sprintf("run timed out after %v", limit),
		Retriable: true,
	}
	e.emit(ctx, event)
	return event
}

// IterStarted emits an iter.started event at the beginning of a loop iteration.
func (e *EventEmitter) IterStarted(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventIterStarted)
	e.emit(ctx, event)
	return event
}

// IterFinished emits an iter.finished event at the end of a loop iteration.
func (e *EventEmitter) IterFinished(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventIterFinished)
	e.emit(ctx, event)
	return event
}

// ModelDelta emits a model.delta event containing streaming text from the LLM.
func (e *EventEmitter) ModelDelta(ctx context.Context, delta string) models.AgentEvent {
	event := e.base(models.AgentEventModelDelta)
	event.Stream = &models.StreamEventPayload{
		Delta: delta,
	}
	e.emit(ctx, event)
	return event
}

// ModelCompleted emits a model.completed event with provider and token usage information.
func (e *EventEmitter) ModelCompleted(ctx context.Context, provider, model string, inputTokens, outputTokens int) models.AgentEvent {
	event := e.base(models.AgentEventModelCompleted)
	event.Stream = &models.StreamEventPayload{
		Provider:     provider,
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	e.emit(ctx, event)
	return event
}

// ToolStarted emits a tool.started event when a tool execution begins.
func (e *EventEmitter) ToolStarted(ctx context.Context, callID, name string, argsJSON []byte) models.AgentEvent {
	event := e.base(models.AgentEventToolStarted)
	event.Tool = &models.ToolEventPayload{
		CallID:   callID,
		Name:     name,
		ArgsJSON: argsJSON,
	}
	e.emit(ctx, event)
	return event
}

// ToolStdout emits a tool.stdout event containing streaming standard output from a tool.
func (e *EventEmitter) ToolStdout(ctx context.Context, callID, name, chunk string) models.AgentEvent {
	event := e.base(models.AgentEventToolStdout)
	event.Tool = &models.ToolEventPayload{
		CallID: callID,
		Name:   name,
		Chunk:  chunk,
	}
	e.emit(ctx, event)
	return event
}

// ToolStderr emits a tool.stderr event containing streaming standard error from a tool.
func (e *EventEmitter) ToolStderr(ctx context.Context, callID, name, chunk string) models.AgentEvent {
	event := e.base(models.AgentEventToolStderr)
	event.Tool = &models.ToolEventPayload{
		CallID: callID,
		Name:   name,
		Chunk:  chunk,
	}
	e.emit(ctx, event)
	return event
}

// ToolFinished emits a tool.finished event when a tool execution completes.
func (e *EventEmitter) ToolFinished(ctx context.Context, callID, name string, success bool, resultJSON []byte, elapsed time.Duration) models.AgentEvent {
	event := e.base(models.AgentEventToolFinished)
	event.Tool = &models.ToolEventPayload{
		CallID:     callID,
		Name:       name,
		Success:    success,
		ResultJSON: resultJSON,
		Elapsed:    elapsed,
	}
	e.emit(ctx, event)
	return event
}

// ToolTimedOut emits a tool.timed_out event when a tool execution exceeds its timeout.
func (e *EventEmitter) ToolTimedOut(ctx context.Context, callID, name string, timeout time.Duration) models.AgentEvent {
	event := e.base(models.AgentEventToolTimedOut)
	event.Tool = &models.ToolEventPayload{
		CallID:  callID,
		Name:    name,
		Success: false,
		Elapsed: timeout,
	}
	event.Error = &models.ErrorEventPayload{
		Message:   fmt.Sprintf("tool %s timed out after %v", name, timeout),
		Retriable: true,
	}
	e.emit(ctx, event)
	return event
}

// ContextPacked emits a context.packed event with packing diagnostics including usage and dropped items.
func (e *EventEmitter) ContextPacked(ctx context.Context, diag *models.ContextEventPayload) models.AgentEvent {
	event := e.base(models.AgentEventContextPacked)
	event.Context = diag
	// Also update Stats for backwards compatibility with aggregation
	event.Stats = &models.StatsEventPayload{
		Run: &models.RunStats{
			DroppedItems: diag.Dropped,
		},
	}
	e.emit(ctx, event)
	return event
}

// StatsCollector accumulates run statistics by processing AgentEvents.
// It tracks iterations, tokens, tool calls, timing, and errors.
type StatsCollector struct {
	stats      models.RunStats
	modelStart time.Time
	toolStarts map[string]time.Time
}

// NewStatsCollector creates a new stats collector for the given run ID.
func NewStatsCollector(runID string) *StatsCollector {
	return &StatsCollector{
		stats: models.RunStats{
			RunID:     runID,
			StartedAt: time.Now(),
		},
		toolStarts: make(map[string]time.Time),
	}
}

// OnEvent processes an event and updates the accumulated statistics accordingly.
func (c *StatsCollector) OnEvent(ctx context.Context, e models.AgentEvent) {
	switch e.Type {
	case models.AgentEventRunStarted:
		c.stats.StartedAt = e.Time

	case models.AgentEventIterStarted:
		c.stats.Iters++
		c.modelStart = e.Time

	case models.AgentEventModelCompleted:
		if !c.modelStart.IsZero() {
			c.stats.ModelWallTime += e.Time.Sub(c.modelStart)
			c.modelStart = time.Time{}
		}
		if e.Stream != nil {
			c.stats.InputTokens += e.Stream.InputTokens
			c.stats.OutputTokens += e.Stream.OutputTokens
		}

	case models.AgentEventToolStarted:
		c.stats.ToolCalls++
		if e.Tool != nil {
			c.toolStarts[e.Tool.CallID] = e.Time
		}

	case models.AgentEventToolFinished:
		if e.Tool != nil {
			if start, ok := c.toolStarts[e.Tool.CallID]; ok {
				c.stats.ToolWallTime += e.Time.Sub(start)
				delete(c.toolStarts, e.Tool.CallID)
			}
			if !e.Tool.Success {
				c.stats.Errors++
			}
		}

	case models.AgentEventToolTimedOut:
		c.stats.ToolTimeouts++
		c.stats.Errors++
		if e.Tool != nil {
			if start, ok := c.toolStarts[e.Tool.CallID]; ok {
				c.stats.ToolWallTime += e.Time.Sub(start)
				delete(c.toolStarts, e.Tool.CallID)
			}
		}

	case models.AgentEventContextPacked:
		c.stats.ContextPacks++
		if e.Stats != nil && e.Stats.Run != nil {
			c.stats.DroppedItems += e.Stats.Run.DroppedItems
		}

	case models.AgentEventRunError:
		c.stats.Errors++

	case models.AgentEventRunCancelled:
		c.stats.Cancelled = true
		c.stats.Errors++

	case models.AgentEventRunTimedOut:
		c.stats.TimedOut = true
		c.stats.Errors++

	case models.AgentEventRunFinished:
		c.stats.FinishedAt = e.Time
		c.stats.WallTime = e.Time.Sub(c.stats.StartedAt)
	}
}

// Stats returns a copy of the accumulated statistics.
func (c *StatsCollector) Stats() *models.RunStats {
	// Copy to avoid mutation
	stats := c.stats
	if stats.FinishedAt.IsZero() {
		stats.FinishedAt = time.Now()
		stats.WallTime = stats.FinishedAt.Sub(stats.StartedAt)
	}
	return &stats
}
