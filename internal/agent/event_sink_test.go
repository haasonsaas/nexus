package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestPluginSink_Emit(t *testing.T) {
	registry := NewPluginRegistry()

	var received []models.AgentEvent
	var mu sync.Mutex

	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	}))

	sink := NewPluginSink(registry)

	event := models.AgentEvent{Type: models.AgentEventRunStarted, RunID: "test"}
	sink.Emit(context.Background(), event)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Errorf("expected 1 event, got %d", len(received))
	}
	if received[0].RunID != "test" {
		t.Errorf("RunID = %q, want %q", received[0].RunID, "test")
	}
}

func TestPluginSink_NilRegistry(t *testing.T) {
	sink := NewPluginSink(nil)

	// Should not panic
	sink.Emit(context.Background(), models.AgentEvent{})
}

func TestChanSink_Emit(t *testing.T) {
	ch := make(chan models.AgentEvent, 10)
	sink := NewChanSink(ch)

	event := models.AgentEvent{Type: models.AgentEventModelDelta, RunID: "test"}
	sink.Emit(context.Background(), event)

	select {
	case received := <-ch:
		if received.RunID != "test" {
			t.Errorf("RunID = %q, want %q", received.RunID, "test")
		}
	default:
		t.Error("expected event in channel")
	}
}

func TestChanSink_FullChannel(t *testing.T) {
	ch := make(chan models.AgentEvent, 1)
	sink := NewChanSink(ch)

	// Fill the channel
	sink.Emit(context.Background(), models.AgentEvent{RunID: "first"})

	// This should not block (drops the event)
	done := make(chan struct{})
	go func() {
		sink.Emit(context.Background(), models.AgentEvent{RunID: "second"})
		close(done)
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("ChanSink.Emit blocked on full channel")
	}
}

func TestChanSink_ContextCancelled(t *testing.T) {
	ch := make(chan models.AgentEvent, 1)
	sink := NewChanSink(ch)

	// Fill the channel
	sink.Emit(context.Background(), models.AgentEvent{RunID: "first"})

	// Emit with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		sink.Emit(ctx, models.AgentEvent{RunID: "cancelled"})
		close(done)
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("ChanSink.Emit blocked with cancelled context")
	}
}

func TestMultiSink_Emit(t *testing.T) {
	var order []string
	var mu sync.Mutex

	sink1 := NewCallbackSink(func(ctx context.Context, e models.AgentEvent) {
		mu.Lock()
		order = append(order, "sink1")
		mu.Unlock()
	})
	sink2 := NewCallbackSink(func(ctx context.Context, e models.AgentEvent) {
		mu.Lock()
		order = append(order, "sink2")
		mu.Unlock()
	})

	multi := NewMultiSink(sink1, sink2)
	multi.Emit(context.Background(), models.AgentEvent{})

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(order))
	}
	if order[0] != "sink1" || order[1] != "sink2" {
		t.Errorf("order = %v, want [sink1 sink2]", order)
	}
}

func TestMultiSink_FiltersNil(t *testing.T) {
	var called bool
	sink := NewCallbackSink(func(ctx context.Context, e models.AgentEvent) {
		called = true
	})

	multi := NewMultiSink(nil, sink, nil)
	multi.Emit(context.Background(), models.AgentEvent{})

	if !called {
		t.Error("expected non-nil sink to be called")
	}
}

func TestCallbackSink_Emit(t *testing.T) {
	var received models.AgentEvent
	sink := NewCallbackSink(func(ctx context.Context, e models.AgentEvent) {
		received = e
	})

	event := models.AgentEvent{Type: models.AgentEventRunStarted, RunID: "callback-test"}
	sink.Emit(context.Background(), event)

	if received.RunID != "callback-test" {
		t.Errorf("RunID = %q, want %q", received.RunID, "callback-test")
	}
}

func TestCallbackSink_NilFunc(t *testing.T) {
	sink := NewCallbackSink(nil)

	// Should not panic
	sink.Emit(context.Background(), models.AgentEvent{})
}

func TestNopSink_Emit(t *testing.T) {
	sink := NopSink{}

	// Should not panic
	sink.Emit(context.Background(), models.AgentEvent{})
}

func TestChunkAdapterSink_ModelDelta(t *testing.T) {
	ch := make(chan *ResponseChunk, 10)
	sink := NewChunkAdapterSink(ch)

	event := models.AgentEvent{
		Type: models.AgentEventModelDelta,
		Stream: &models.StreamEventPayload{
			Delta: "hello world",
		},
	}
	sink.Emit(context.Background(), event)

	select {
	case chunk := <-ch:
		if chunk.Text != "hello world" {
			t.Errorf("Text = %q, want %q", chunk.Text, "hello world")
		}
	default:
		t.Error("expected chunk in channel")
	}
}

func TestChunkAdapterSink_ToolFinished(t *testing.T) {
	ch := make(chan *ResponseChunk, 10)
	sink := NewChunkAdapterSink(ch)

	event := models.AgentEvent{
		Type: models.AgentEventToolFinished,
		Tool: &models.ToolEventPayload{
			CallID:     "tc-1",
			Name:       "search",
			Success:    true,
			ResultJSON: []byte(`"search result"`),
		},
	}
	sink.Emit(context.Background(), event)

	select {
	case chunk := <-ch:
		if chunk.ToolResult == nil {
			t.Fatal("expected ToolResult")
		}
		if chunk.ToolResult.ToolCallID != "tc-1" {
			t.Errorf("ToolCallID = %q, want %q", chunk.ToolResult.ToolCallID, "tc-1")
		}
		if chunk.ToolResult.IsError {
			t.Error("IsError should be false for success")
		}
	default:
		t.Error("expected chunk in channel")
	}
}

func TestChunkAdapterSink_ToolFinished_Error(t *testing.T) {
	ch := make(chan *ResponseChunk, 10)
	sink := NewChunkAdapterSink(ch)

	event := models.AgentEvent{
		Type: models.AgentEventToolFinished,
		Tool: &models.ToolEventPayload{
			CallID:  "tc-1",
			Success: false,
		},
	}
	sink.Emit(context.Background(), event)

	select {
	case chunk := <-ch:
		if chunk.ToolResult == nil {
			t.Fatal("expected ToolResult")
		}
		if !chunk.ToolResult.IsError {
			t.Error("IsError should be true for failure")
		}
	default:
		t.Error("expected chunk in channel")
	}
}

func TestChunkAdapterSink_RunError(t *testing.T) {
	ch := make(chan *ResponseChunk, 10)
	sink := NewChunkAdapterSink(ch)

	event := models.AgentEvent{
		Type: models.AgentEventRunError,
		Error: &models.ErrorEventPayload{
			Message: "something went wrong",
		},
	}
	sink.Emit(context.Background(), event)

	select {
	case chunk := <-ch:
		if chunk.Error == nil {
			t.Fatal("expected Error")
		}
		if chunk.Error.Error() != "something went wrong" {
			t.Errorf("Error = %q, want %q", chunk.Error.Error(), "something went wrong")
		}
	default:
		t.Error("expected chunk in channel")
	}
}

func TestChunkAdapterSink_IgnoresNonMappableEvents(t *testing.T) {
	ch := make(chan *ResponseChunk, 10)
	sink := NewChunkAdapterSink(ch)

	// These events don't produce ResponseChunks
	events := []models.AgentEvent{
		{Type: models.AgentEventRunStarted},
		{Type: models.AgentEventRunFinished},
		{Type: models.AgentEventContextPacked},
	}

	for _, e := range events {
		sink.Emit(context.Background(), e)
	}

	select {
	case chunk := <-ch:
		t.Errorf("unexpected chunk for non-mappable event: %+v", chunk)
	default:
		// Expected - no chunks for these events
	}
}

func TestEventToChunk_LegacyEvents(t *testing.T) {
	events := []struct {
		agentType  models.AgentEventType
		legacyType models.RuntimeEventType
	}{
		{models.AgentEventIterStarted, models.EventIterationStart},
		{models.AgentEventIterFinished, models.EventIterationEnd},
		{models.AgentEventToolStarted, models.EventToolStarted},
	}

	for _, tc := range events {
		event := models.AgentEvent{
			Type:      tc.agentType,
			IterIndex: 2,
			Tool: &models.ToolEventPayload{
				CallID: "tc-1",
				Name:   "test",
			},
		}

		chunk := eventToChunk(event)
		if chunk == nil {
			t.Errorf("expected chunk for %s", tc.agentType)
			continue
		}
		if chunk.Event == nil {
			t.Errorf("expected Event in chunk for %s", tc.agentType)
			continue
		}
		if chunk.Event.Type != tc.legacyType {
			t.Errorf("Event.Type = %s, want %s", chunk.Event.Type, tc.legacyType)
		}
		if chunk.Event.Iteration != 2 {
			t.Errorf("Iteration = %d, want 2", chunk.Event.Iteration)
		}
	}
}
