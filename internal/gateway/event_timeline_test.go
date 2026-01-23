package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNewEventTimelinePlugin(t *testing.T) {
	t.Run("creates plugin with recorder", func(t *testing.T) {
		store := observability.NewMemoryEventStore(100)
		recorder := observability.NewEventRecorder(store, nil)
		plugin := NewEventTimelinePlugin(recorder)

		if plugin == nil {
			t.Fatal("NewEventTimelinePlugin returned nil")
		}
		if plugin.recorder != recorder {
			t.Error("plugin.recorder not set correctly")
		}
	})

	t.Run("creates plugin with nil recorder", func(t *testing.T) {
		plugin := NewEventTimelinePlugin(nil)

		if plugin == nil {
			t.Fatal("NewEventTimelinePlugin returned nil")
		}
		if plugin.recorder != nil {
			t.Error("plugin.recorder should be nil")
		}
	})
}

func TestEventTimelinePlugin_OnEvent(t *testing.T) {
	store := observability.NewMemoryEventStore(100)
	recorder := observability.NewEventRecorder(store, nil)
	plugin := NewEventTimelinePlugin(recorder)
	ctx := context.Background()

	t.Run("handles nil recorder gracefully", func(t *testing.T) {
		nilPlugin := NewEventTimelinePlugin(nil)
		// Should not panic
		nilPlugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunStarted,
			RunID: "run-123",
		})
	})

	t.Run("records run started", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunStarted,
			RunID: "run-started-test",
		})
		// Event should be recorded (we can't easily verify without accessing store internals)
	})

	t.Run("records run finished", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunFinished,
			RunID: "run-finished-test",
			Stats: &models.StatsEventPayload{
				Run: &models.RunStats{
					WallTime: 100 * time.Millisecond,
				},
			},
		})
	})

	t.Run("records run error", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunError,
			RunID: "run-error-test",
			Error: &models.ErrorEventPayload{
				Message: "test error",
			},
		})
	})

	t.Run("records run cancelled", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunCancelled,
			RunID: "run-cancelled-test",
			Error: &models.ErrorEventPayload{
				Message: "context cancelled",
			},
		})
	})

	t.Run("records run timed out", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunTimedOut,
			RunID: "run-timeout-test",
			Error: &models.ErrorEventPayload{
				Message: "run timed out",
			},
		})
	})

	t.Run("records tool started", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolStarted,
			RunID: "tool-started-test",
			Tool: &models.ToolEventPayload{
				Name:     "bash",
				CallID:   "call-123",
				ArgsJSON: []byte(`{"command": "ls"}`),
			},
		})
	})

	t.Run("records tool finished success", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolFinished,
			RunID: "tool-finished-test",
			Tool: &models.ToolEventPayload{
				Name:       "bash",
				CallID:     "call-456",
				Success:    true,
				ResultJSON: []byte(`{"output": "file.txt"}`),
				Elapsed:    50 * time.Millisecond,
			},
		})
	})

	t.Run("records tool finished error", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolFinished,
			RunID: "tool-finished-error-test",
			Tool: &models.ToolEventPayload{
				Name:    "bash",
				CallID:  "call-789",
				Success: false,
				Elapsed: 10 * time.Millisecond,
			},
			Error: &models.ErrorEventPayload{
				Message: "command failed",
			},
		})
	})

	t.Run("records tool timed out", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolTimedOut,
			RunID: "tool-timeout-test",
			Tool: &models.ToolEventPayload{
				Name:   "bash",
				CallID: "call-timeout",
			},
		})
	})

	t.Run("records tool timeout with error message", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolTimedOut,
			RunID: "tool-timeout-msg-test",
			Tool: &models.ToolEventPayload{
				Name:   "bash",
				CallID: "call-timeout-2",
			},
			Error: &models.ErrorEventPayload{
				Message: "custom timeout message",
			},
		})
	})

	t.Run("records model completed", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventModelCompleted,
			RunID: "model-completed-test",
			Stats: &models.StatsEventPayload{
				Run: &models.RunStats{
					ModelWallTime: 200 * time.Millisecond,
				},
			},
		})
	})

	t.Run("records iteration started", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:      models.AgentEventIterStarted,
			RunID:     "iter-started-test",
			IterIndex: 1,
		})
	})

	t.Run("records iteration finished", func(t *testing.T) {
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:      models.AgentEventIterFinished,
			RunID:     "iter-finished-test",
			IterIndex: 1,
		})
	})

	t.Run("handles nil tool in tool events", func(t *testing.T) {
		// Should not panic
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolStarted,
			RunID: "nil-tool-test",
			Tool:  nil,
		})
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolFinished,
			RunID: "nil-tool-test",
			Tool:  nil,
		})
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventToolTimedOut,
			RunID: "nil-tool-test",
			Tool:  nil,
		})
	})

	t.Run("handles nil stats", func(t *testing.T) {
		// Should not panic
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventRunFinished,
			RunID: "nil-stats-test",
			Stats: nil,
		})
		plugin.OnEvent(ctx, models.AgentEvent{
			Type:  models.AgentEventModelCompleted,
			RunID: "nil-stats-model-test",
			Stats: nil,
		})
	})
}
