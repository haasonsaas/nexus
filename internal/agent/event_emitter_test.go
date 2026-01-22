package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestEventEmitter_Sequencing(t *testing.T) {
	emitter := NewEventEmitter("test-run", nil)

	// Emit multiple events
	e1 := emitter.RunStarted(context.Background())
	e2 := emitter.IterStarted(context.Background())
	e3 := emitter.ModelDelta(context.Background(), "hello")
	e4 := emitter.IterFinished(context.Background())

	// Verify monotonic sequencing
	if e1.Sequence >= e2.Sequence {
		t.Errorf("sequence should be monotonic: %d >= %d", e1.Sequence, e2.Sequence)
	}
	if e2.Sequence >= e3.Sequence {
		t.Errorf("sequence should be monotonic: %d >= %d", e2.Sequence, e3.Sequence)
	}
	if e3.Sequence >= e4.Sequence {
		t.Errorf("sequence should be monotonic: %d >= %d", e3.Sequence, e4.Sequence)
	}
}

func TestEventEmitter_RunID(t *testing.T) {
	emitter := NewEventEmitter("my-run-id", nil)

	event := emitter.RunStarted(context.Background())

	if event.RunID != "my-run-id" {
		t.Errorf("RunID = %q, want %q", event.RunID, "my-run-id")
	}
}

func TestEventEmitter_TurnAndIterIndex(t *testing.T) {
	emitter := NewEventEmitter("test", nil)

	emitter.SetTurn(2)
	emitter.SetIter(3)

	event := emitter.ModelDelta(context.Background(), "x")

	if event.TurnIndex != 2 {
		t.Errorf("TurnIndex = %d, want 2", event.TurnIndex)
	}
	if event.IterIndex != 3 {
		t.Errorf("IterIndex = %d, want 3", event.IterIndex)
	}
}

func TestEventEmitter_Version(t *testing.T) {
	emitter := NewEventEmitter("test", nil)

	event := emitter.RunStarted(context.Background())

	if event.Version != 1 {
		t.Errorf("Version = %d, want 1", event.Version)
	}
}

func TestEventEmitter_DispatchesToPlugins(t *testing.T) {
	registry := NewPluginRegistry()

	var received []models.AgentEvent
	var mu sync.Mutex

	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	}))

	emitter := NewEventEmitterWithPlugins("test", registry)

	emitter.RunStarted(context.Background())
	emitter.IterStarted(context.Background())
	emitter.ModelDelta(context.Background(), "hi")

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Errorf("expected 3 events dispatched to plugin, got %d", len(received))
	}
}

func TestEventEmitter_ModelDelta(t *testing.T) {
	emitter := NewEventEmitter("test", nil)

	event := emitter.ModelDelta(context.Background(), "hello world")

	if event.Type != models.AgentEventModelDelta {
		t.Errorf("Type = %s, want model.delta", event.Type)
	}
	if event.Stream == nil {
		t.Fatal("Stream payload should not be nil")
	}
	if event.Stream.Delta != "hello world" {
		t.Errorf("Delta = %q, want %q", event.Stream.Delta, "hello world")
	}
}

func TestEventEmitter_ToolLifecycle(t *testing.T) {
	emitter := NewEventEmitter("test", nil)

	started := emitter.ToolStarted(context.Background(), "call-1", "search", []byte(`{"q":"test"}`))
	finished := emitter.ToolFinished(context.Background(), "call-1", "search", true, []byte(`"result"`), 100*time.Millisecond)

	// Started
	if started.Type != models.AgentEventToolStarted {
		t.Errorf("started.Type = %s, want tool.started", started.Type)
	}
	if started.Tool == nil || started.Tool.CallID != "call-1" {
		t.Error("started.Tool.CallID mismatch")
	}
	if started.Tool.Name != "search" {
		t.Error("started.Tool.Name mismatch")
	}

	// Finished
	if finished.Type != models.AgentEventToolFinished {
		t.Errorf("finished.Type = %s, want tool.finished", finished.Type)
	}
	if finished.Tool == nil || !finished.Tool.Success {
		t.Error("finished.Tool.Success should be true")
	}
	if finished.Tool.Elapsed != 100*time.Millisecond {
		t.Errorf("Elapsed = %v, want 100ms", finished.Tool.Elapsed)
	}
}

func TestEventEmitter_RunError(t *testing.T) {
	emitter := NewEventEmitter("test", nil)

	event := emitter.RunError(context.Background(), context.Canceled, true)

	if event.Type != models.AgentEventRunError {
		t.Errorf("Type = %s, want run.error", event.Type)
	}
	if event.Error == nil {
		t.Fatal("Error payload should not be nil")
	}
	if event.Error.Message != "context canceled" {
		t.Errorf("Message = %q", event.Error.Message)
	}
	if !event.Error.Retriable {
		t.Error("Retriable should be true")
	}
}

func TestStatsCollector_Basic(t *testing.T) {
	collector := NewStatsCollector("test-run")

	ctx := context.Background()

	// Simulate a run
	collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventRunStarted, Time: time.Now()})
	collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventIterStarted, Time: time.Now()})
	collector.OnEvent(ctx, models.AgentEvent{
		Type: models.AgentEventModelCompleted,
		Time: time.Now(),
		Stream: &models.StreamEventPayload{
			InputTokens:  100,
			OutputTokens: 50,
		},
	})
	collector.OnEvent(ctx, models.AgentEvent{
		Type: models.AgentEventToolStarted,
		Time: time.Now(),
		Tool: &models.ToolEventPayload{CallID: "tc-1", Name: "search"},
	})
	collector.OnEvent(ctx, models.AgentEvent{
		Type: models.AgentEventToolFinished,
		Time: time.Now().Add(50 * time.Millisecond),
		Tool: &models.ToolEventPayload{CallID: "tc-1", Name: "search", Success: true},
	})
	collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventIterFinished, Time: time.Now()})
	collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventRunFinished, Time: time.Now()})

	stats := collector.Stats()

	if stats.RunID != "test-run" {
		t.Errorf("RunID = %q, want %q", stats.RunID, "test-run")
	}
	if stats.Iters != 1 {
		t.Errorf("Iters = %d, want 1", stats.Iters)
	}
	if stats.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", stats.ToolCalls)
	}
	if stats.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", stats.InputTokens)
	}
	if stats.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", stats.OutputTokens)
	}
}

func TestStatsCollector_ErrorCounting(t *testing.T) {
	collector := NewStatsCollector("test")

	ctx := context.Background()

	collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventRunError})
	collector.OnEvent(ctx, models.AgentEvent{
		Type: models.AgentEventToolFinished,
		Tool: &models.ToolEventPayload{CallID: "tc-1", Success: false},
	})

	stats := collector.Stats()

	// One run error + one tool failure
	if stats.Errors != 2 {
		t.Errorf("Errors = %d, want 2", stats.Errors)
	}
}

func TestStatsCollector_MultipleIterations(t *testing.T) {
	collector := NewStatsCollector("test")

	ctx := context.Background()

	// 3 iterations
	for i := 0; i < 3; i++ {
		collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventIterStarted, Time: time.Now()})
		collector.OnEvent(ctx, models.AgentEvent{Type: models.AgentEventIterFinished, Time: time.Now()})
	}

	stats := collector.Stats()

	if stats.Iters != 3 {
		t.Errorf("Iters = %d, want 3", stats.Iters)
	}
}

func TestStatsCollector_ContextPacking(t *testing.T) {
	collector := NewStatsCollector("test")

	ctx := context.Background()

	collector.OnEvent(ctx, models.AgentEvent{
		Type: models.AgentEventContextPacked,
		Stats: &models.StatsEventPayload{
			Run: &models.RunStats{DroppedItems: 5},
		},
	})
	collector.OnEvent(ctx, models.AgentEvent{
		Type: models.AgentEventContextPacked,
		Stats: &models.StatsEventPayload{
			Run: &models.RunStats{DroppedItems: 3},
		},
	})

	stats := collector.Stats()

	if stats.ContextPacks != 2 {
		t.Errorf("ContextPacks = %d, want 2", stats.ContextPacks)
	}
	if stats.DroppedItems != 8 {
		t.Errorf("DroppedItems = %d, want 8", stats.DroppedItems)
	}
}
