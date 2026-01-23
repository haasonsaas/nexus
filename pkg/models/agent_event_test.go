package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentEventType_Constants(t *testing.T) {
	tests := []struct {
		constant AgentEventType
		expected string
	}{
		// Run lifecycle
		{AgentEventRunStarted, "run.started"},
		{AgentEventRunFinished, "run.finished"},
		{AgentEventRunError, "run.error"},
		{AgentEventRunCancelled, "run.cancelled"},
		{AgentEventRunTimedOut, "run.timed_out"},

		// Turn/iteration lifecycle
		{AgentEventTurnStarted, "turn.started"},
		{AgentEventTurnFinished, "turn.finished"},
		{AgentEventIterStarted, "iter.started"},
		{AgentEventIterFinished, "iter.finished"},

		// Model streaming
		{AgentEventModelDelta, "model.delta"},
		{AgentEventModelCompleted, "model.completed"},

		// Tool execution
		{AgentEventToolStarted, "tool.started"},
		{AgentEventToolStdout, "tool.stdout"},
		{AgentEventToolStderr, "tool.stderr"},
		{AgentEventToolFinished, "tool.finished"},
		{AgentEventToolTimedOut, "tool.timed_out"},

		// Context packing
		{AgentEventContextPacked, "context.packed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestAgentEvent_Struct(t *testing.T) {
	now := time.Now()
	event := AgentEvent{
		Version:   1,
		Type:      AgentEventRunStarted,
		Time:      now,
		Sequence:  1,
		RunID:     "run-123",
		TurnIndex: 0,
		IterIndex: 0,
	}

	if event.Version != 1 {
		t.Errorf("Version = %d, want 1", event.Version)
	}
	if event.Type != AgentEventRunStarted {
		t.Errorf("Type = %v, want %v", event.Type, AgentEventRunStarted)
	}
	if event.RunID != "run-123" {
		t.Errorf("RunID = %q, want %q", event.RunID, "run-123")
	}
}

func TestAgentEvent_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := AgentEvent{
		Version:   1,
		Type:      AgentEventModelDelta,
		Time:      now,
		Sequence:  5,
		RunID:     "run-123",
		TurnIndex: 1,
		IterIndex: 2,
		Stream: &StreamEventPayload{
			Delta:        "Hello",
			Provider:     "openai",
			Model:        "gpt-4",
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded AgentEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, original.Type)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", decoded.Sequence, original.Sequence)
	}
	if decoded.Stream == nil {
		t.Fatal("Stream payload is nil")
	}
	if decoded.Stream.Delta != "Hello" {
		t.Errorf("Stream.Delta = %q, want %q", decoded.Stream.Delta, "Hello")
	}
}

func TestTextEventPayload_Struct(t *testing.T) {
	payload := TextEventPayload{Text: "Test message"}
	if payload.Text != "Test message" {
		t.Errorf("Text = %q, want %q", payload.Text, "Test message")
	}
}

func TestStreamEventPayload_Struct(t *testing.T) {
	payload := StreamEventPayload{
		Delta:        "Hello",
		Final:        "Hello World",
		Provider:     "anthropic",
		Model:        "claude-3",
		InputTokens:  150,
		OutputTokens: 75,
	}

	if payload.Delta != "Hello" {
		t.Errorf("Delta = %q, want %q", payload.Delta, "Hello")
	}
	if payload.InputTokens != 150 {
		t.Errorf("InputTokens = %d, want 150", payload.InputTokens)
	}
}

func TestToolEventPayload_Struct(t *testing.T) {
	payload := ToolEventPayload{
		CallID:     "call-123",
		Name:       "web_search",
		ArgsJSON:   []byte(`{"query":"test"}`),
		Chunk:      "output chunk",
		Success:    true,
		ResultJSON: []byte(`{"results":[]}`),
		Elapsed:    5 * time.Second,
	}

	if payload.CallID != "call-123" {
		t.Errorf("CallID = %q, want %q", payload.CallID, "call-123")
	}
	if payload.Name != "web_search" {
		t.Errorf("Name = %q, want %q", payload.Name, "web_search")
	}
	if !payload.Success {
		t.Error("Success should be true")
	}
	if payload.Elapsed != 5*time.Second {
		t.Errorf("Elapsed = %v, want %v", payload.Elapsed, 5*time.Second)
	}
}

func TestErrorEventPayload_Struct(t *testing.T) {
	payload := ErrorEventPayload{
		Message:   "Something went wrong",
		Code:      "E001",
		Retriable: true,
	}

	if payload.Message != "Something went wrong" {
		t.Errorf("Message = %q, want %q", payload.Message, "Something went wrong")
	}
	if payload.Code != "E001" {
		t.Errorf("Code = %q, want %q", payload.Code, "E001")
	}
	if !payload.Retriable {
		t.Error("Retriable should be true")
	}
}

func TestStatsEventPayload_Struct(t *testing.T) {
	now := time.Now()
	payload := StatsEventPayload{
		Run: &RunStats{
			RunID:      "run-123",
			StartedAt:  now,
			FinishedAt: now.Add(10 * time.Second),
			WallTime:   10 * time.Second,
			Turns:      3,
			Iters:      5,
			ToolCalls:  2,
		},
	}

	if payload.Run == nil {
		t.Fatal("Run is nil")
	}
	if payload.Run.RunID != "run-123" {
		t.Errorf("Run.RunID = %q, want %q", payload.Run.RunID, "run-123")
	}
	if payload.Run.Turns != 3 {
		t.Errorf("Run.Turns = %d, want 3", payload.Run.Turns)
	}
}

func TestRunStats_Struct(t *testing.T) {
	now := time.Now()
	stats := RunStats{
		RunID:         "run-123",
		StartedAt:     now,
		FinishedAt:    now.Add(30 * time.Second),
		WallTime:      30 * time.Second,
		Turns:         5,
		Iters:         10,
		ToolCalls:     3,
		ToolWallTime:  5 * time.Second,
		ToolTimeouts:  1,
		ModelWallTime: 20 * time.Second,
		InputTokens:   500,
		OutputTokens:  250,
		ContextPacks:  2,
		DroppedItems:  5,
		Cancelled:     false,
		TimedOut:      false,
		DroppedEvents: 0,
		Errors:        1,
	}

	if stats.RunID != "run-123" {
		t.Errorf("RunID = %q, want %q", stats.RunID, "run-123")
	}
	if stats.WallTime != 30*time.Second {
		t.Errorf("WallTime = %v, want %v", stats.WallTime, 30*time.Second)
	}
	if stats.InputTokens != 500 {
		t.Errorf("InputTokens = %d, want 500", stats.InputTokens)
	}
	if stats.Errors != 1 {
		t.Errorf("Errors = %d, want 1", stats.Errors)
	}
}

func TestContextEventPayload_Struct(t *testing.T) {
	payload := ContextEventPayload{
		BudgetChars:    10000,
		BudgetMessages: 50,
		UsedChars:      8000,
		UsedMessages:   40,
		Candidates:     60,
		Included:       40,
		Dropped:        20,
		SummaryUsed:    true,
		SummaryChars:   500,
	}

	if payload.BudgetChars != 10000 {
		t.Errorf("BudgetChars = %d, want 10000", payload.BudgetChars)
	}
	if payload.Dropped != 20 {
		t.Errorf("Dropped = %d, want 20", payload.Dropped)
	}
	if !payload.SummaryUsed {
		t.Error("SummaryUsed should be true")
	}
}

func TestContextItemKind_Constants(t *testing.T) {
	tests := []struct {
		constant ContextItemKind
		expected string
	}{
		{ContextItemSystem, "system"},
		{ContextItemHistory, "history"},
		{ContextItemTool, "tool"},
		{ContextItemSummary, "summary"},
		{ContextItemIncoming, "incoming"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestContextPackReason_Constants(t *testing.T) {
	tests := []struct {
		constant ContextPackReason
		expected string
	}{
		{ContextReasonIncluded, "included"},
		{ContextReasonReserved, "reserved"},
		{ContextReasonOverBudget, "over_budget"},
		{ContextReasonTooOld, "too_old"},
		{ContextReasonFiltered, "filtered"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestContextPackItem_Struct(t *testing.T) {
	item := ContextPackItem{
		ID:       "item-123",
		Kind:     ContextItemHistory,
		Chars:    500,
		Included: true,
		Reason:   ContextReasonIncluded,
	}

	if item.ID != "item-123" {
		t.Errorf("ID = %q, want %q", item.ID, "item-123")
	}
	if item.Kind != ContextItemHistory {
		t.Errorf("Kind = %v, want %v", item.Kind, ContextItemHistory)
	}
	if !item.Included {
		t.Error("Included should be true")
	}
	if item.Reason != ContextReasonIncluded {
		t.Errorf("Reason = %v, want %v", item.Reason, ContextReasonIncluded)
	}
}
