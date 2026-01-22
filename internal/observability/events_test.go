package observability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestContextKeys(t *testing.T) {
	ctx := context.Background()

	t.Run("run_id", func(t *testing.T) {
		ctx = AddRunID(ctx, "run-123")
		if got := GetRunID(ctx); got != "run-123" {
			t.Errorf("expected 'run-123', got %s", got)
		}
	})

	t.Run("tool_call_id", func(t *testing.T) {
		ctx = AddToolCallID(ctx, "tool-456")
		if got := GetToolCallID(ctx); got != "tool-456" {
			t.Errorf("expected 'tool-456', got %s", got)
		}
	})

	t.Run("edge_id", func(t *testing.T) {
		ctx = AddEdgeID(ctx, "edge-789")
		if got := GetEdgeID(ctx); got != "edge-789" {
			t.Errorf("expected 'edge-789', got %s", got)
		}
	})

	t.Run("agent_id", func(t *testing.T) {
		ctx = AddAgentID(ctx, "agent-abc")
		if got := GetAgentID(ctx); got != "agent-abc" {
			t.Errorf("expected 'agent-abc', got %s", got)
		}
	})

	t.Run("message_id", func(t *testing.T) {
		ctx = AddMessageID(ctx, "msg-def")
		if got := GetMessageID(ctx); got != "msg-def" {
			t.Errorf("expected 'msg-def', got %s", got)
		}
	})

	t.Run("empty context returns empty string", func(t *testing.T) {
		emptyCtx := context.Background()
		if got := GetRunID(emptyCtx); got != "" {
			t.Errorf("expected empty string, got %s", got)
		}
	})
}

func TestMemoryEventStore(t *testing.T) {
	store := NewMemoryEventStore(100)

	t.Run("record and get", func(t *testing.T) {
		event := &Event{
			Type:      EventTypeRunStart,
			RunID:     "run-1",
			SessionID: "session-1",
			Name:      "test_event",
		}

		err := store.Record(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if event.ID == "" {
			t.Error("expected ID to be generated")
		}
		if event.Timestamp.IsZero() {
			t.Error("expected timestamp to be set")
		}

		got, err := store.Get(event.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "test_event" {
			t.Errorf("expected 'test_event', got %s", got.Name)
		}
	})

	t.Run("get by run ID", func(t *testing.T) {
		// Record multiple events for same run
		for i := 0; i < 5; i++ {
			store.Record(&Event{
				Type:  EventTypeToolStart,
				RunID: "run-query-test",
				Name:  "event",
			})
		}

		events, err := store.GetByRunID("run-query-test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 5 {
			t.Errorf("expected 5 events, got %d", len(events))
		}
	})

	t.Run("get by session ID", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			store.Record(&Event{
				Type:      EventTypeMessage,
				SessionID: "session-query-test",
				Name:      "message",
			})
		}

		events, err := store.GetBySessionID("session-query-test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 3 {
			t.Errorf("expected 3 events, got %d", len(events))
		}
	})

	t.Run("get by type", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			store.Record(&Event{
				Type: EventTypeLLMRequest,
				Name: "llm",
			})
		}

		events, err := store.GetByType(EventTypeLLMRequest, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events (limited), got %d", len(events))
		}
	})

	t.Run("get by time range", func(t *testing.T) {
		start := time.Now()
		time.Sleep(10 * time.Millisecond)

		store.Record(&Event{
			Type: EventTypeCustom,
			Name: "in_range",
		})

		time.Sleep(10 * time.Millisecond)
		end := time.Now()

		events, err := store.GetByTimeRange(start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, e := range events {
			if e.Name == "in_range" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find 'in_range' event")
		}
	})

	t.Run("delete old events", func(t *testing.T) {
		deleteStore := NewMemoryEventStore(100)

		// Record old event
		oldEvent := &Event{
			Type:      EventTypeRunEnd,
			Timestamp: time.Now().Add(-2 * time.Hour),
			Name:      "old_event",
		}
		deleteStore.Record(oldEvent)

		// Record new event
		newEvent := &Event{
			Type: EventTypeRunStart,
			Name: "new_event",
		}
		deleteStore.Record(newEvent)

		deleted, err := deleteStore.Delete(time.Hour)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted != 1 {
			t.Errorf("expected 1 deleted, got %d", deleted)
		}

		// Old event should be gone
		_, err = deleteStore.Get(oldEvent.ID)
		if err == nil {
			t.Error("expected old event to be deleted")
		}

		// New event should still exist
		_, err = deleteStore.Get(newEvent.ID)
		if err != nil {
			t.Error("expected new event to still exist")
		}
	})

	t.Run("max size eviction", func(t *testing.T) {
		smallStore := NewMemoryEventStore(10)

		for i := 0; i < 15; i++ {
			smallStore.Record(&Event{
				Type: EventTypeCustom,
				Name: "overflow",
			})
		}

		// Should have evicted some events
		if len(smallStore.events) > 10 {
			t.Errorf("expected max 10 events, got %d", len(smallStore.events))
		}
	})

	t.Run("nil event error", func(t *testing.T) {
		err := store.Record(nil)
		if err == nil {
			t.Error("expected error for nil event")
		}
	})

	t.Run("not found error", func(t *testing.T) {
		_, err := store.Get("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent event")
		}
	})
}

func TestEventRecorder(t *testing.T) {
	store := NewMemoryEventStore(100)
	recorder := NewEventRecorder(store, nil)

	t.Run("record with context", func(t *testing.T) {
		ctx := context.Background()
		ctx = AddRunID(ctx, "run-recorder")
		ctx = AddSessionID(ctx, "session-recorder")
		ctx = AddEdgeID(ctx, "edge-recorder")

		err := recorder.Record(ctx, EventTypeCustom, "test_event", map[string]interface{}{
			"key": "value",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-recorder")
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		e := events[0]
		if e.RunID != "run-recorder" {
			t.Errorf("expected run ID 'run-recorder', got %s", e.RunID)
		}
		if e.SessionID != "session-recorder" {
			t.Errorf("expected session ID 'session-recorder', got %s", e.SessionID)
		}
		if e.EdgeID != "edge-recorder" {
			t.Errorf("expected edge ID 'edge-recorder', got %s", e.EdgeID)
		}
	})

	t.Run("record error", func(t *testing.T) {
		ctx := AddRunID(context.Background(), "run-error")
		testErr := errors.New("something went wrong")

		err := recorder.RecordError(ctx, EventTypeRunError, "error_event", testErr, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-error")
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		e := events[0]
		if e.Error != "something went wrong" {
			t.Errorf("expected error message, got %s", e.Error)
		}
	})

	t.Run("record tool start", func(t *testing.T) {
		ctx := AddRunID(context.Background(), "run-tool")

		err := recorder.RecordToolStart(ctx, "web_search", map[string]string{"query": "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-tool")
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}

		e := events[0]
		if e.Type != EventTypeToolStart {
			t.Errorf("expected tool.start type, got %s", e.Type)
		}
		if e.Name != "web_search" {
			t.Errorf("expected name 'web_search', got %s", e.Name)
		}
	})

	t.Run("record tool end success", func(t *testing.T) {
		ctx := AddRunID(context.Background(), "run-tool-end")

		err := recorder.RecordToolEnd(ctx, "web_search", 100*time.Millisecond, "result", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-tool-end")
		e := events[0]
		if e.Type != EventTypeToolEnd {
			t.Errorf("expected tool.end type, got %s", e.Type)
		}
	})

	t.Run("record tool end error", func(t *testing.T) {
		ctx := AddRunID(context.Background(), "run-tool-error")
		testErr := errors.New("tool failed")

		err := recorder.RecordToolEnd(ctx, "web_search", 50*time.Millisecond, nil, testErr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-tool-error")
		e := events[0]
		if e.Type != EventTypeToolError {
			t.Errorf("expected tool.error type, got %s", e.Type)
		}
		if e.Error != "tool failed" {
			t.Errorf("expected error 'tool failed', got %s", e.Error)
		}
	})

	t.Run("record run start/end", func(t *testing.T) {
		ctx := context.Background()

		err := recorder.RecordRunStart(ctx, "run-lifecycle", map[string]interface{}{
			"input": "test message",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx = AddRunID(ctx, "run-lifecycle")
		err = recorder.RecordRunEnd(ctx, 500*time.Millisecond, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-lifecycle")
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
	})

	t.Run("record edge event", func(t *testing.T) {
		ctx := AddRunID(context.Background(), "run-edge")

		err := recorder.RecordEdgeEvent(ctx, EventTypeEdgeConnect, "my-macbook", map[string]interface{}{
			"ip": "192.168.1.100",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		events, _ := store.GetByRunID("run-edge")
		e := events[0]
		if e.EdgeID != "my-macbook" {
			t.Errorf("expected edge ID 'my-macbook', got %s", e.EdgeID)
		}
	})
}

func TestTimeline(t *testing.T) {
	t.Run("build timeline", func(t *testing.T) {
		events := []*Event{
			{
				ID:        "1",
				Type:      EventTypeRunStart,
				Timestamp: time.Now().Add(-100 * time.Millisecond),
				RunID:     "run-timeline",
				SessionID: "session-timeline",
			},
			{
				ID:        "2",
				Type:      EventTypeToolStart,
				Timestamp: time.Now().Add(-80 * time.Millisecond),
				RunID:     "run-timeline",
			},
			{
				ID:        "3",
				Type:      EventTypeToolEnd,
				Timestamp: time.Now().Add(-60 * time.Millisecond),
				RunID:     "run-timeline",
				Duration:  20 * time.Millisecond,
			},
			{
				ID:        "4",
				Type:      EventTypeLLMRequest,
				Timestamp: time.Now().Add(-50 * time.Millisecond),
				RunID:     "run-timeline",
			},
			{
				ID:        "5",
				Type:      EventTypeLLMError,
				Timestamp: time.Now().Add(-30 * time.Millisecond),
				RunID:     "run-timeline",
				Error:     "rate limited",
			},
			{
				ID:        "6",
				Type:      EventTypeRunEnd,
				Timestamp: time.Now(),
				RunID:     "run-timeline",
			},
		}

		timeline := BuildTimeline(events)

		if timeline.RunID != "run-timeline" {
			t.Errorf("expected run ID 'run-timeline', got %s", timeline.RunID)
		}
		if timeline.SessionID != "session-timeline" {
			t.Errorf("expected session ID 'session-timeline', got %s", timeline.SessionID)
		}
		if timeline.Summary.TotalEvents != 6 {
			t.Errorf("expected 6 total events, got %d", timeline.Summary.TotalEvents)
		}
		if timeline.Summary.ErrorCount != 1 {
			t.Errorf("expected 1 error, got %d", timeline.Summary.ErrorCount)
		}
		if timeline.Summary.ToolCalls != 1 {
			t.Errorf("expected 1 tool call, got %d", timeline.Summary.ToolCalls)
		}
		if timeline.Summary.LLMCalls != 1 {
			t.Errorf("expected 1 LLM call, got %d", timeline.Summary.LLMCalls)
		}
	})

	t.Run("empty timeline", func(t *testing.T) {
		timeline := BuildTimeline([]*Event{})
		if timeline.Summary == nil {
			t.Error("expected summary to be non-nil")
		}
		if timeline.Summary.TotalEvents != 0 {
			t.Errorf("expected 0 events, got %d", timeline.Summary.TotalEvents)
		}
	})

	t.Run("format timeline", func(t *testing.T) {
		events := []*Event{
			{
				ID:        "1",
				Type:      EventTypeRunStart,
				Timestamp: time.Now().Add(-100 * time.Millisecond),
				RunID:     "run-format",
				Name:      "run_start",
			},
			{
				ID:        "2",
				Type:      EventTypeToolStart,
				Timestamp: time.Now().Add(-50 * time.Millisecond),
				RunID:     "run-format",
				Name:      "web_search",
				EdgeID:    "my-mac",
			},
			{
				ID:        "3",
				Type:      EventTypeToolError,
				Timestamp: time.Now(),
				RunID:     "run-format",
				Name:      "web_search",
				Error:     "timeout",
				Duration:  50 * time.Millisecond,
			},
		}

		timeline := BuildTimeline(events)
		output := FormatTimeline(timeline)

		if !strings.Contains(output, "run-format") {
			t.Error("expected output to contain run ID")
		}
		if !strings.Contains(output, "web_search") {
			t.Error("expected output to contain tool name")
		}
		if !strings.Contains(output, "my-mac") {
			t.Error("expected output to contain edge ID")
		}
		if !strings.Contains(output, "timeout") {
			t.Error("expected output to contain error")
		}
		if !strings.Contains(output, "❌") {
			t.Error("expected output to contain error marker")
		}
	})

	t.Run("format nil timeline", func(t *testing.T) {
		output := FormatTimeline(nil)
		if output != "No events found" {
			t.Errorf("expected 'No events found', got %s", output)
		}
	})
}

func TestEventTypes(t *testing.T) {
	// Verify event type constants
	types := []EventType{
		EventTypeRunStart,
		EventTypeRunEnd,
		EventTypeRunError,
		EventTypeToolStart,
		EventTypeToolEnd,
		EventTypeToolError,
		EventTypeToolProgress,
		EventTypeEdgeConnect,
		EventTypeEdgeDisconnect,
		EventTypeEdgeHeartbeat,
		EventTypeApprovalReq,
		EventTypeApprovalDec,
		EventTypeLLMRequest,
		EventTypeLLMResponse,
		EventTypeLLMError,
		EventTypeMessage,
		EventTypeCustom,
	}

	for _, et := range types {
		if string(et) == "" {
			t.Errorf("event type %v has empty string value", et)
		}
	}
}
