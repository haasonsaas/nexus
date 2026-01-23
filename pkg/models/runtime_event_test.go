package models

import (
	"encoding/json"
	"testing"
)

func TestRuntimeEventType_Constants(t *testing.T) {
	tests := []struct {
		constant RuntimeEventType
		expected string
	}{
		{EventThinkingStart, "thinking_start"},
		{EventThinkingEnd, "thinking_end"},
		{EventToolQueued, "tool_queued"},
		{EventToolStarted, "tool_started"},
		{EventToolCompleted, "tool_completed"},
		{EventToolFailed, "tool_failed"},
		{EventToolTimeout, "tool_timeout"},
		{EventSummarizing, "summarizing"},
		{EventIterationStart, "iteration_start"},
		{EventIterationEnd, "iteration_end"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestRuntimeEvent_Struct(t *testing.T) {
	event := RuntimeEvent{
		Type:       EventToolStarted,
		Message:    "Starting web_search tool",
		ToolName:   "web_search",
		ToolCallID: "call-123",
		Iteration:  2,
		Meta:       map[string]any{"query": "test"},
	}

	if event.Type != EventToolStarted {
		t.Errorf("Type = %v, want %v", event.Type, EventToolStarted)
	}
	if event.ToolName != "web_search" {
		t.Errorf("ToolName = %q, want %q", event.ToolName, "web_search")
	}
	if event.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", event.Iteration)
	}
}

func TestRuntimeEvent_JSONRoundTrip(t *testing.T) {
	original := RuntimeEvent{
		Type:       EventToolCompleted,
		Message:    "Tool completed successfully",
		ToolName:   "calculator",
		ToolCallID: "call-456",
		Iteration:  1,
		Meta:       map[string]any{"result": "42"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded RuntimeEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, original.Type)
	}
	if decoded.ToolName != original.ToolName {
		t.Errorf("ToolName = %q, want %q", decoded.ToolName, original.ToolName)
	}
	if decoded.Meta["result"] != "42" {
		t.Errorf("Meta[result] = %v, want %q", decoded.Meta["result"], "42")
	}
}

func TestNewToolEvent(t *testing.T) {
	event := NewToolEvent(EventToolStarted, "web_search", "call-123")

	if event == nil {
		t.Fatal("event is nil")
	}
	if event.Type != EventToolStarted {
		t.Errorf("Type = %v, want %v", event.Type, EventToolStarted)
	}
	if event.ToolName != "web_search" {
		t.Errorf("ToolName = %q, want %q", event.ToolName, "web_search")
	}
	if event.ToolCallID != "call-123" {
		t.Errorf("ToolCallID = %q, want %q", event.ToolCallID, "call-123")
	}
}

func TestRuntimeEvent_WithMessage(t *testing.T) {
	event := NewToolEvent(EventToolStarted, "tool", "call-1")
	result := event.WithMessage("Test message")

	// Should return same event for chaining
	if result != event {
		t.Error("WithMessage should return the same event")
	}
	if event.Message != "Test message" {
		t.Errorf("Message = %q, want %q", event.Message, "Test message")
	}
}

func TestRuntimeEvent_WithIteration(t *testing.T) {
	event := NewToolEvent(EventIterationStart, "", "")
	result := event.WithIteration(5)

	// Should return same event for chaining
	if result != event {
		t.Error("WithIteration should return the same event")
	}
	if event.Iteration != 5 {
		t.Errorf("Iteration = %d, want 5", event.Iteration)
	}
}

func TestRuntimeEvent_WithMeta(t *testing.T) {
	t.Run("adds single meta field", func(t *testing.T) {
		event := NewToolEvent(EventToolCompleted, "tool", "call-1")
		result := event.WithMeta("key", "value")

		if result != event {
			t.Error("WithMeta should return the same event")
		}
		if event.Meta == nil {
			t.Fatal("Meta should be initialized")
		}
		if event.Meta["key"] != "value" {
			t.Errorf("Meta[key] = %v, want %q", event.Meta["key"], "value")
		}
	})

	t.Run("adds multiple meta fields", func(t *testing.T) {
		event := NewToolEvent(EventToolCompleted, "tool", "call-1").
			WithMeta("key1", "value1").
			WithMeta("key2", 42).
			WithMeta("key3", true)

		if event.Meta["key1"] != "value1" {
			t.Errorf("Meta[key1] = %v, want %q", event.Meta["key1"], "value1")
		}
		if event.Meta["key2"] != 42 {
			t.Errorf("Meta[key2] = %v, want 42", event.Meta["key2"])
		}
		if event.Meta["key3"] != true {
			t.Errorf("Meta[key3] = %v, want true", event.Meta["key3"])
		}
	})
}

func TestRuntimeEvent_Chaining(t *testing.T) {
	event := NewToolEvent(EventToolStarted, "web_search", "call-123").
		WithMessage("Starting search").
		WithIteration(3).
		WithMeta("query", "test query")

	if event.Type != EventToolStarted {
		t.Errorf("Type = %v, want %v", event.Type, EventToolStarted)
	}
	if event.Message != "Starting search" {
		t.Errorf("Message = %q, want %q", event.Message, "Starting search")
	}
	if event.Iteration != 3 {
		t.Errorf("Iteration = %d, want 3", event.Iteration)
	}
	if event.Meta["query"] != "test query" {
		t.Errorf("Meta[query] = %v, want %q", event.Meta["query"], "test query")
	}
}
