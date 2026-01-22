package sessions

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMemoryToolEventStore_AddToolCall(t *testing.T) {
	store := NewMemoryToolEventStore()

	call := &ToolCall{
		ID:       "call-1",
		ToolName: "search",
		InputJSON: json.RawMessage(`{"query": "test"}`),
	}

	err := store.AddToolCall(context.Background(), "session-1", "msg-1", call)
	if err != nil {
		t.Fatalf("AddToolCall failed: %v", err)
	}

	calls, err := store.GetToolCalls(context.Background(), "session-1", 10)
	if err != nil {
		t.Fatalf("GetToolCalls failed: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}

	if calls[0].ID != "call-1" {
		t.Errorf("ID = %q, want %q", calls[0].ID, "call-1")
	}
	if calls[0].ToolName != "search" {
		t.Errorf("ToolName = %q, want %q", calls[0].ToolName, "search")
	}
	if calls[0].SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", calls[0].SessionID, "session-1")
	}
}

func TestMemoryToolEventStore_AddToolResult(t *testing.T) {
	store := NewMemoryToolEventStore()

	result := &ToolResult{
		ToolCallID: "call-1",
		Content:    "found it",
		IsError:    false,
	}

	err := store.AddToolResult(context.Background(), "session-1", "msg-1", "call-1", result)
	if err != nil {
		t.Fatalf("AddToolResult failed: %v", err)
	}

	results, err := store.GetToolResults(context.Background(), "session-1", 10)
	if err != nil {
		t.Fatalf("GetToolResults failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	if results[0].ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, want %q", results[0].ToolCallID, "call-1")
	}
	if results[0].Content != "found it" {
		t.Errorf("Content = %q, want %q", results[0].Content, "found it")
	}
	if results[0].IsError {
		t.Error("IsError should be false")
	}
}

func TestMemoryToolEventStore_GetToolCallsByMessage(t *testing.T) {
	store := NewMemoryToolEventStore()

	// Add calls for different messages
	call1 := &ToolCall{ID: "call-1", ToolName: "search"}
	call2 := &ToolCall{ID: "call-2", ToolName: "browse"}
	call3 := &ToolCall{ID: "call-3", ToolName: "code"}

	store.AddToolCall(context.Background(), "session-1", "msg-1", call1)
	store.AddToolCall(context.Background(), "session-1", "msg-1", call2)
	store.AddToolCall(context.Background(), "session-1", "msg-2", call3)

	calls, err := store.GetToolCallsByMessage(context.Background(), "msg-1")
	if err != nil {
		t.Fatalf("GetToolCallsByMessage failed: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
}

func TestMemoryToolEventStore_Limit(t *testing.T) {
	store := NewMemoryToolEventStore()

	// Add 10 calls
	for i := 0; i < 10; i++ {
		call := &ToolCall{ID: "call-" + string(rune('0'+i)), ToolName: "test"}
		store.AddToolCall(context.Background(), "session-1", "", call)
	}

	// Get with limit 5
	calls, err := store.GetToolCalls(context.Background(), "session-1", 5)
	if err != nil {
		t.Fatalf("GetToolCalls failed: %v", err)
	}

	if len(calls) != 5 {
		t.Errorf("got %d calls, want 5", len(calls))
	}
}

func TestMemoryToolEventStore_NilHandling(t *testing.T) {
	store := NewMemoryToolEventStore()

	// Should not panic on nil
	err := store.AddToolCall(context.Background(), "session-1", "", nil)
	if err != nil {
		t.Errorf("AddToolCall with nil should not error: %v", err)
	}

	err = store.AddToolResult(context.Background(), "session-1", "", "", nil)
	if err != nil {
		t.Errorf("AddToolResult with nil should not error: %v", err)
	}
}

func TestMemoryToolEventStore_EmptySession(t *testing.T) {
	store := NewMemoryToolEventStore()

	calls, err := store.GetToolCalls(context.Background(), "nonexistent", 10)
	if err != nil {
		t.Fatalf("GetToolCalls failed: %v", err)
	}

	if len(calls) != 0 {
		t.Errorf("expected empty list for nonexistent session")
	}
}
