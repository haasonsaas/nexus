package agent

import (
	"context"
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

	// Plugin registry for dispatch
	plugins *PluginRegistry
}

// NewEventEmitter creates a new event emitter for an agent run.
func NewEventEmitter(runID string, plugins *PluginRegistry) *EventEmitter {
	return &EventEmitter{
		runID:   runID,
		plugins: plugins,
	}
}

// SetTurn updates the current turn index.
func (e *EventEmitter) SetTurn(turnIndex int) {
	e.turnIndex = turnIndex
}

// SetIter updates the current iteration index.
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

// emit dispatches the event to plugins (if any).
func (e *EventEmitter) emit(ctx context.Context, event models.AgentEvent) {
	if e.plugins != nil {
		e.plugins.Emit(ctx, event)
	}
}

// RunStarted emits a run.started event.
func (e *EventEmitter) RunStarted(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventRunStarted)
	e.emit(ctx, event)
	return event
}

// RunFinished emits a run.finished event with stats.
func (e *EventEmitter) RunFinished(ctx context.Context, stats *models.RunStats) models.AgentEvent {
	event := e.base(models.AgentEventRunFinished)
	if stats != nil {
		event.Stats = &models.StatsEventPayload{Run: stats}
	}
	e.emit(ctx, event)
	return event
}

// RunError emits a run.error event.
func (e *EventEmitter) RunError(ctx context.Context, err error, retriable bool) models.AgentEvent {
	event := e.base(models.AgentEventRunError)
	event.Error = &models.ErrorEventPayload{
		Message:   err.Error(),
		Retriable: retriable,
	}
	e.emit(ctx, event)
	return event
}

// IterStarted emits an iter.started event.
func (e *EventEmitter) IterStarted(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventIterStarted)
	e.emit(ctx, event)
	return event
}

// IterFinished emits an iter.finished event.
func (e *EventEmitter) IterFinished(ctx context.Context) models.AgentEvent {
	event := e.base(models.AgentEventIterFinished)
	e.emit(ctx, event)
	return event
}

// ModelDelta emits a model.delta event for streaming text.
func (e *EventEmitter) ModelDelta(ctx context.Context, delta string) models.AgentEvent {
	event := e.base(models.AgentEventModelDelta)
	event.Stream = &models.StreamEventPayload{
		Delta: delta,
	}
	e.emit(ctx, event)
	return event
}

// ModelCompleted emits a model.completed event.
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

// ToolStarted emits a tool.started event.
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

// ToolStdout emits a tool.stdout event for streaming tool output.
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

// ToolStderr emits a tool.stderr event for streaming tool errors.
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

// ToolFinished emits a tool.finished event.
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

// ContextPacked emits a context.packed event with diagnostics.
func (e *EventEmitter) ContextPacked(ctx context.Context, totalMessages, includedMessages, droppedMessages int) models.AgentEvent {
	event := e.base(models.AgentEventContextPacked)
	event.Text = &models.TextEventPayload{
		Text: "", // Could add summary text
	}
	// Store diagnostics in Stats since there's no dedicated context payload
	event.Stats = &models.StatsEventPayload{
		Run: &models.RunStats{
			DroppedItems: droppedMessages,
		},
	}
	e.emit(ctx, event)
	return event
}

// StatsCollector accumulates statistics from events.
type StatsCollector struct {
	stats       models.RunStats
	modelStart  time.Time
	toolStarts  map[string]time.Time
}

// NewStatsCollector creates a new stats collector.
func NewStatsCollector(runID string) *StatsCollector {
	return &StatsCollector{
		stats: models.RunStats{
			RunID:     runID,
			StartedAt: time.Now(),
		},
		toolStarts: make(map[string]time.Time),
	}
}

// OnEvent processes an event and updates stats.
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

	case models.AgentEventContextPacked:
		c.stats.ContextPacks++
		if e.Stats != nil && e.Stats.Run != nil {
			c.stats.DroppedItems += e.Stats.Run.DroppedItems
		}

	case models.AgentEventRunError:
		c.stats.Errors++

	case models.AgentEventRunFinished:
		c.stats.FinishedAt = e.Time
		c.stats.WallTime = e.Time.Sub(c.stats.StartedAt)
	}
}

// Stats returns the accumulated statistics.
func (c *StatsCollector) Stats() *models.RunStats {
	// Copy to avoid mutation
	stats := c.stats
	if stats.FinishedAt.IsZero() {
		stats.FinishedAt = time.Now()
		stats.WallTime = stats.FinishedAt.Sub(stats.StartedAt)
	}
	return &stats
}
