package agent

import (
	"context"

	"github.com/haasonsaas/nexus/pkg/models"
)

// EventSink receives agent events during processing.
// Implementations should be non-blocking or handle backpressure gracefully.
type EventSink interface {
	// Emit sends an event to the sink.
	// Implementations must be safe to call from multiple goroutines.
	Emit(ctx context.Context, e models.AgentEvent)
}

// PluginSink dispatches events to a plugin registry.
type PluginSink struct {
	registry *PluginRegistry
}

// NewPluginSink creates a sink that dispatches to plugins.
func NewPluginSink(registry *PluginRegistry) *PluginSink {
	return &PluginSink{registry: registry}
}

// Emit dispatches the event to all registered plugins.
func (s *PluginSink) Emit(ctx context.Context, e models.AgentEvent) {
	if s.registry != nil {
		s.registry.Emit(ctx, e)
	}
}

// ChanSink sends events to a channel (non-blocking with fallback).
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

// MultiSink fans out events to multiple sinks.
type MultiSink struct {
	sinks []EventSink
}

// NewMultiSink creates a sink that dispatches to multiple sinks.
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

// CallbackSink wraps a function as an EventSink.
type CallbackSink struct {
	fn func(ctx context.Context, e models.AgentEvent)
}

// NewCallbackSink creates a sink from a callback function.
func NewCallbackSink(fn func(ctx context.Context, e models.AgentEvent)) *CallbackSink {
	return &CallbackSink{fn: fn}
}

// Emit calls the wrapped function.
func (s *CallbackSink) Emit(ctx context.Context, e models.AgentEvent) {
	if s.fn != nil {
		s.fn(ctx, e)
	}
}

// NopSink discards all events (useful for testing).
type NopSink struct{}

// Emit does nothing.
func (NopSink) Emit(ctx context.Context, e models.AgentEvent) {}

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

	case models.AgentEventRunError:
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
		// Convert to legacy RuntimeEvent for backwards compatibility
		return &ResponseChunk{
			Event: legacyEventFromAgentEvent(e),
		}
	}

	return nil
}

// AgentError implements error for agent-level errors.
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
