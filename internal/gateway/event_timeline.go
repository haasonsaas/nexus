// Package gateway provides the main Nexus gateway server.
//
// event_timeline.go bridges AgentEvents from the runtime to the observability EventStore.
package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/pkg/models"
)

// EventTimelinePlugin converts AgentEvents to observability Events and records them.
// It implements the agent.Plugin interface so it can be registered with the runtime.
type EventTimelinePlugin struct {
	recorder *observability.EventRecorder
}

// NewEventTimelinePlugin creates a new plugin that records events to the timeline.
func NewEventTimelinePlugin(recorder *observability.EventRecorder) *EventTimelinePlugin {
	return &EventTimelinePlugin{recorder: recorder}
}

// OnEvent converts an AgentEvent to an observability Event and records it.
// This implements the agent.Plugin interface.
// Event recording errors are intentionally ignored - these are best-effort records
// and should not block or fail the agent execution.
//
//nolint:errcheck // Best-effort event recording - errors should not block agent execution
func (p *EventTimelinePlugin) OnEvent(ctx context.Context, e models.AgentEvent) {
	if p.recorder == nil {
		return
	}

	// Add correlation IDs to context
	if e.RunID != "" {
		ctx = observability.AddRunID(ctx, e.RunID)
	}
	if e.Tool != nil && e.Tool.CallID != "" {
		ctx = observability.AddToolCallID(ctx, e.Tool.CallID)
	}

	// Convert AgentEvent to observability Event
	switch e.Type {
	case models.AgentEventRunStarted:
		_ = p.recorder.RecordRunStart(ctx, e.RunID, nil)

	case models.AgentEventRunFinished:
		var duration time.Duration
		if e.Stats != nil && e.Stats.Run != nil {
			duration = e.Stats.Run.WallTime
		}
		_ = p.recorder.RecordRunEnd(ctx, duration, nil)

	case models.AgentEventRunError, models.AgentEventRunCancelled, models.AgentEventRunTimedOut:
		var err error
		if e.Error != nil {
			err = errors.New(e.Error.Message)
		}
		data := map[string]interface{}{
			"type": string(e.Type),
		}
		_ = p.recorder.RecordError(ctx, observability.EventTypeRunError, "run_error", err, data)

	case models.AgentEventToolStarted:
		if e.Tool != nil {
			input := ""
			if len(e.Tool.ArgsJSON) > 0 {
				input = string(e.Tool.ArgsJSON)
			}
			_ = p.recorder.RecordToolStart(ctx, e.Tool.Name, input)
		}

	case models.AgentEventToolFinished:
		if e.Tool != nil {
			output := ""
			if len(e.Tool.ResultJSON) > 0 {
				output = string(e.Tool.ResultJSON)
			}
			var err error
			if !e.Tool.Success && e.Error != nil {
				err = errors.New(e.Error.Message)
			}
			_ = p.recorder.RecordToolEnd(ctx, e.Tool.Name, e.Tool.Elapsed, output, err)
		}

	case models.AgentEventToolTimedOut:
		if e.Tool != nil {
			errMsg := "tool execution timed out"
			if e.Error != nil && e.Error.Message != "" {
				errMsg = e.Error.Message
			}
			_ = p.recorder.RecordError(ctx, observability.EventTypeToolError, e.Tool.Name, errors.New(errMsg), map[string]interface{}{
				"tool_call_id": e.Tool.CallID,
			})
		}

	case models.AgentEventModelCompleted:
		data := map[string]interface{}{}
		if e.Stream != nil {
			if e.Stream.Provider != "" {
				data["provider"] = e.Stream.Provider
			}
			if e.Stream.Model != "" {
				data["model"] = e.Stream.Model
			}
			data["input_tokens"] = e.Stream.InputTokens
			data["output_tokens"] = e.Stream.OutputTokens
		}
		if e.Stats != nil && e.Stats.Run != nil {
			data["model_wall_time_ms"] = e.Stats.Run.ModelWallTime.Milliseconds()
		}
		_ = p.recorder.Record(ctx, observability.EventTypeLLMResponse, "llm_response", data)

	case models.AgentEventIterStarted:
		_ = p.recorder.Record(ctx, observability.EventTypeCustom, "iteration_started", map[string]interface{}{
			"iteration": e.IterIndex,
		})

	case models.AgentEventIterFinished:
		_ = p.recorder.Record(ctx, observability.EventTypeCustom, "iteration_finished", map[string]interface{}{
			"iteration": e.IterIndex,
		})
	}
}

// GetEventTimelinePlugin returns a Plugin that records to the server's event timeline.
// Register this with the runtime via runtime.Use().
func (s *Server) GetEventTimelinePlugin() agent.Plugin {
	if s.eventRecorder == nil {
		return nil
	}
	return NewEventTimelinePlugin(s.eventRecorder)
}

// EventStore returns the server's event store for querying events.
func (s *Server) EventStore() *observability.MemoryEventStore {
	return s.eventStore
}

// EventRecorder returns the server's event recorder.
func (s *Server) EventRecorder() *observability.EventRecorder {
	return s.eventRecorder
}
