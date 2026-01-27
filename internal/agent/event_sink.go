package agent

import (
	"context"
	"sync/atomic"

	"github.com/haasonsaas/nexus/pkg/models"
)

// EventSink receives agent events during processing.
// Implementations should be non-blocking or handle backpressure gracefully.
type EventSink interface {
	// Emit sends an event to the sink.
	// Implementations must be safe to call from multiple goroutines.
	Emit(ctx context.Context, e models.AgentEvent)
}

// PluginSink dispatches events to a plugin registry, bridging the EventSink
// and Plugin interfaces.
type PluginSink struct {
	registry *PluginRegistry
}

// NewPluginSink creates a sink that dispatches events to all plugins in the registry.
func NewPluginSink(registry *PluginRegistry) *PluginSink {
	return &PluginSink{registry: registry}
}

// Emit dispatches the event to all plugins registered in the underlying registry.
func (s *PluginSink) Emit(ctx context.Context, e models.AgentEvent) {
	if s.registry != nil {
		s.registry.Emit(ctx, e)
	}
}

// ChanSink sends events to a channel with non-blocking behavior when the channel is full.
type ChanSink struct {
	ch chan<- models.AgentEvent
}

// NewChanSink creates a sink that sends to a channel.
// The channel should be buffered to avoid blocking.
func NewChanSink(ch chan<- models.AgentEvent) *ChanSink {
	return &ChanSink{ch: ch}
}

// Emit sends the event to the channel (non-blocking if full or context cancelled).
func (s *ChanSink) Emit(ctx context.Context, e models.AgentEvent) {
	select {
	case s.ch <- e:
	case <-ctx.Done():
	default:
		// Channel full - drop event rather than block
	}
}

// MultiSink fans out events to multiple sinks, calling each sink's Emit method.
type MultiSink struct {
	sinks []EventSink
}

// NewMultiSink creates a sink that dispatches events to multiple sinks.
// Nil sinks are filtered out automatically.
func NewMultiSink(sinks ...EventSink) *MultiSink {
	// Filter out nil sinks
	filtered := make([]EventSink, 0, len(sinks))
	for _, s := range sinks {
		if s != nil {
			filtered = append(filtered, s)
		}
	}
	return &MultiSink{sinks: filtered}
}

// Emit dispatches the event to all sinks.
func (s *MultiSink) Emit(ctx context.Context, e models.AgentEvent) {
	for _, sink := range s.sinks {
		sink.Emit(ctx, e)
	}
}

// CallbackSink wraps a function as an EventSink for inline event handling.
type CallbackSink struct {
	fn func(ctx context.Context, e models.AgentEvent)
}

// NewCallbackSink creates a sink that calls the provided function for each event.
func NewCallbackSink(fn func(ctx context.Context, e models.AgentEvent)) *CallbackSink {
	return &CallbackSink{fn: fn}
}

// Emit calls the wrapped function.
func (s *CallbackSink) Emit(ctx context.Context, e models.AgentEvent) {
	if s.fn != nil {
		s.fn(ctx, e)
	}
}

// NopSink discards all events silently. Useful for testing or when event handling is not needed.
type NopSink struct{}

// Emit does nothing.
func (NopSink) Emit(ctx context.Context, e models.AgentEvent) {}

// BackpressureConfig configures the backpressure sink buffer sizes for
// high-priority and low-priority event lanes.
type BackpressureConfig struct {
	// HighPriBuffer is the buffer size for non-droppable events.
	// Default: 32.
	HighPriBuffer int

	// LowPriBuffer is the buffer size for droppable events.
	// Default: 256.
	LowPriBuffer int
}

// DefaultBackpressureConfig returns sensible defaults.
func DefaultBackpressureConfig() BackpressureConfig {
	return BackpressureConfig{
		HighPriBuffer: 32,
		LowPriBuffer:  256,
	}
}

// BackpressureSink implements two-lane backpressure for event streaming.
// High-priority events (run lifecycle, tool lifecycle, completions) are never dropped.
// Low-priority events (model deltas, stdout/stderr) are dropped when buffer is full.
type BackpressureSink struct {
	highPri chan models.AgentEvent // Never dropped - blocks if full
	lowPri  chan models.AgentEvent // Dropped when full
	merged  chan models.AgentEvent // Output channel that prioritizes highPri
	dropped uint64                 // Atomic counter for dropped events
	closed  uint32                 // Atomic flag: 1 if closed, 0 otherwise
}

// NewBackpressureSink creates a backpressure-aware sink with merged output channel.
// The returned channel should be consumed by the caller.
func NewBackpressureSink(config BackpressureConfig) (*BackpressureSink, <-chan models.AgentEvent) {
	if config.HighPriBuffer <= 0 {
		config.HighPriBuffer = 32
	}
	if config.LowPriBuffer <= 0 {
		config.LowPriBuffer = 256
	}

	s := &BackpressureSink{
		highPri: make(chan models.AgentEvent, config.HighPriBuffer),
		lowPri:  make(chan models.AgentEvent, config.LowPriBuffer),
		merged:  make(chan models.AgentEvent, config.HighPriBuffer), // Merged output
	}

	// Start merge goroutine that prioritizes high-priority events
	go s.mergeLoop()

	return s, s.merged
}

// mergeLoop reads from both channels, prioritizing high-priority events.
func (s *BackpressureSink) mergeLoop() {
	defer close(s.merged)

	for {
		// Always check high-priority first (non-blocking)
		select {
		case e, ok := <-s.highPri:
			if ok {
				s.merged <- e
				continue
			}
			// High-pri closed, drain low-pri and exit
			for e := range s.lowPri {
				s.merged <- e
			}
			return
		default:
		}

		// No high-pri event available, check both channels
		select {
		case e, ok := <-s.highPri:
			if ok {
				s.merged <- e
			} else {
				// High-pri closed, drain low-pri and exit
				for e := range s.lowPri {
					s.merged <- e
				}
				return
			}
		case e, ok := <-s.lowPri:
			if ok {
				s.merged <- e
			}
			// If lowPri is closed, just continue - highPri will close eventually
		}
	}
}

// Emit sends an event through the appropriate lane.
// Non-droppable events block if buffer is full; droppable events are dropped.
// Returns immediately if the sink is closed.
func (s *BackpressureSink) Emit(ctx context.Context, e models.AgentEvent) {
	// Check if closed before attempting to send
	if atomic.LoadUint32(&s.closed) == 1 {
		return
	}
	if isDroppableEvent(e.Type) {
		// Low-priority: drop if buffer is full
		select {
		case s.lowPri <- e:
			// Sent successfully
		default:
			// Buffer full, drop and count
			atomic.AddUint64(&s.dropped, 1)
		}
	} else {
		// High-priority: block until space available or context cancelled
		select {
		case s.highPri <- e:
			// Sent successfully
		case <-ctx.Done():
			// Context cancelled, still try to send (for terminal events)
			select {
			case s.highPri <- e:
			default:
				// Last resort: drop (shouldn't happen with proper buffer sizing)
				atomic.AddUint64(&s.dropped, 1)
			}
		}
	}
}

// DroppedCount returns the number of low-priority events dropped due to backpressure.
func (s *BackpressureSink) DroppedCount() uint64 {
	return atomic.LoadUint64(&s.dropped)
}

// Close signals the sink to stop and closes the output channel.
// After Close, no more events should be emitted.
func (s *BackpressureSink) Close() {
	// Mark as closed first to prevent new Emit calls
	if !atomic.CompareAndSwapUint32(&s.closed, 0, 1) {
		return // Already closed
	}
	// Close highPri first - this triggers mergeLoop to drain lowPri and exit
	close(s.highPri)
	// Close lowPri after so mergeLoop can drain it
	close(s.lowPri)
}

// isDroppableEvent returns true for event types that can be dropped under backpressure.
// Non-droppable events include all lifecycle events that must be delivered for correctness.
func isDroppableEvent(t models.AgentEventType) bool {
	switch t {
	case models.AgentEventModelDelta,
		models.AgentEventToolStdout,
		models.AgentEventToolStderr:
		return true
	default:
		// All other events are non-droppable:
		// run.*, iter.*, tool.started/finished/timed_out, model.completed, context.packed
		return false
	}
}

// ChunkAdapterSink converts AgentEvents to ResponseChunks and sends to a channel.
// This provides backwards compatibility for consumers expecting ResponseChunks.
type ChunkAdapterSink struct {
	ch chan<- *ResponseChunk
}

// NewChunkAdapterSink creates a sink that converts events to ResponseChunks.
func NewChunkAdapterSink(ch chan<- *ResponseChunk) *ChunkAdapterSink {
	return &ChunkAdapterSink{ch: ch}
}

// Emit converts the event to a ResponseChunk and sends it (non-blocking).
func (s *ChunkAdapterSink) Emit(ctx context.Context, e models.AgentEvent) {
	chunk := eventToChunk(e)
	if chunk == nil {
		return
	}

	// Try to send - prioritize this over context cancellation
	// so that error events are delivered even when context is cancelled
	select {
	case s.ch <- chunk:
		return
	default:
	}

	// Channel was full, check context before retrying
	if chunk.Error != nil {
		// Never drop terminal errors; block until delivered or context is done.
		select {
		case s.ch <- chunk:
		case <-ctx.Done():
		}
		return
	}

	select {
	case s.ch <- chunk:
	case <-ctx.Done():
	default:
		// Channel still full - drop chunk rather than block
	}
}

// eventToChunk converts an AgentEvent to a ResponseChunk.
// Returns nil if the event doesn't map to a chunk type.
func eventToChunk(e models.AgentEvent) *ResponseChunk {
	switch e.Type {
	case models.AgentEventModelDelta:
		if e.Stream != nil && e.Stream.Delta != "" {
			return &ResponseChunk{Text: e.Stream.Delta}
		}

	case models.AgentEventToolFinished:
		if e.Tool != nil {
			return &ResponseChunk{
				ToolResult: &models.ToolResult{
					ToolCallID: e.Tool.CallID,
					Content:    string(e.Tool.ResultJSON),
					IsError:    !e.Tool.Success,
				},
			}
		}

	case models.AgentEventToolTimedOut:
		if e.Tool != nil {
			content := "tool execution timed out"
			if e.Error != nil && e.Error.Message != "" {
				content = e.Error.Message
			}
			return &ResponseChunk{
				ToolResult: &models.ToolResult{
					ToolCallID: e.Tool.CallID,
					Content:    content,
					IsError:    true,
				},
			}
		}

	case models.AgentEventRunError, models.AgentEventRunCancelled, models.AgentEventRunTimedOut:
		if e.Error != nil {
			// Prefer original error if available (preserves error type for errors.Is)
			var err error
			if e.Error.Err != nil {
				err = e.Error.Err
			} else {
				err = &AgentError{Message: e.Error.Message}
			}
			return &ResponseChunk{Error: err}
		}

	case models.AgentEventIterStarted, models.AgentEventIterFinished,
		models.AgentEventToolStarted, models.AgentEventToolStdout, models.AgentEventToolStderr:
		// Convert to compatibility RuntimeEvent for older clients
		return &ResponseChunk{
			Event: legacyEventFromAgentEvent(e),
		}
	}

	return nil
}

// AgentError implements the error interface for agent-level errors.
type AgentError struct {
	Message string
}

func (e *AgentError) Error() string {
	return e.Message
}

// legacyEventFromAgentEvent converts AgentEvent to RuntimeEvent for backwards compatibility.
func legacyEventFromAgentEvent(e models.AgentEvent) *models.RuntimeEvent {
	var eventType models.RuntimeEventType

	switch e.Type {
	case models.AgentEventIterStarted:
		eventType = models.EventIterationStart
	case models.AgentEventIterFinished:
		eventType = models.EventIterationEnd
	case models.AgentEventToolStarted:
		eventType = models.EventToolStarted
	case models.AgentEventToolFinished:
		if e.Tool != nil && e.Tool.Success {
			eventType = models.EventToolCompleted
		} else {
			eventType = models.EventToolFailed
		}
	default:
		return nil
	}

	event := &models.RuntimeEvent{
		Type:      eventType,
		Iteration: e.IterIndex,
	}

	if e.Tool != nil {
		event.ToolName = e.Tool.Name
		event.ToolCallID = e.Tool.CallID
	}

	return event
}
