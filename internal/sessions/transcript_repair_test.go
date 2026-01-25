package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Helper functions for creating test messages
func makeAssistantMsg(id string, toolCalls ...models.ToolCall) *models.Message {
	return &models.Message{
		ID:        id,
		Role:      models.RoleAssistant,
		Direction: models.DirectionOutbound,
		Content:   "assistant message",
		ToolCalls: toolCalls,
		CreatedAt: time.Now(),
	}
}

func makeToolCall(id, name string) models.ToolCall {
	return models.ToolCall{
		ID:    id,
		Name:  name,
		Input: json.RawMessage(`{}`),
	}
}

func makeToolResultMsg(id, toolCallID, content string) *models.Message {
	return &models.Message{
		ID:        id,
		Role:      models.RoleTool,
		Direction: models.DirectionInbound,
		ToolResults: []models.ToolResult{
			{
				ToolCallID: toolCallID,
				Content:    content,
				IsError:    false,
			},
		},
		CreatedAt: time.Now(),
	}
}

func makeUserMsg(id, content string) *models.Message {
	return &models.Message{
		ID:        id,
		Role:      models.RoleUser,
		Direction: models.DirectionInbound,
		Content:   content,
		CreatedAt: time.Now(),
	}
}

func TestRepairTranscript_NoRepairNeeded(t *testing.T) {
	// Well-formed transcript: assistant with tool call followed by matching result
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1", makeToolCall("tc1", "read_file")),
		makeToolResultMsg("tr1", "tc1", "file contents"),
		makeAssistantMsg("a2"),
	}

	report := RepairTranscript(messages)

	if report.AddedSyntheticResults() != 0 {
		t.Errorf("expected 0 synthetic results, got %d", report.AddedSyntheticResults())
	}
	if report.DroppedDuplicates() != 0 {
		t.Errorf("expected 0 dropped duplicates, got %d", report.DroppedDuplicates())
	}
	if report.DroppedOrphans() != 0 {
		t.Errorf("expected 0 dropped orphans, got %d", report.DroppedOrphans())
	}
	if report.Moved {
		t.Error("expected no moves")
	}
	if len(report.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(report.Messages))
	}
}

func TestRepairTranscript_MissingToolResult(t *testing.T) {
	// Assistant makes tool call but no result follows
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1", makeToolCall("tc1", "read_file")),
		makeAssistantMsg("a2"), // Next assistant message without result
	}

	report := RepairTranscript(messages)

	if report.AddedSyntheticResults() != 1 {
		t.Errorf("expected 1 synthetic result, got %d", report.AddedSyntheticResults())
	}
	if len(report.Messages) != 4 {
		t.Errorf("expected 4 messages (user, assistant, synthetic, assistant), got %d", len(report.Messages))
	}

	// Verify synthetic result is in correct position
	if report.Messages[2].Role != models.RoleTool {
		t.Errorf("expected message at index 2 to be tool result, got %s", report.Messages[2].Role)
	}
	if len(report.Messages[2].ToolResults) == 0 || report.Messages[2].ToolResults[0].ToolCallID != "tc1" {
		t.Error("synthetic result should match tool call ID tc1")
	}
	if !report.Messages[2].ToolResults[0].IsError {
		t.Error("synthetic result should be marked as error")
	}
}

func TestRepairTranscript_MultipleToolCallsMissingResults(t *testing.T) {
	// Assistant makes multiple tool calls, none have results
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1",
			makeToolCall("tc1", "read_file"),
			makeToolCall("tc2", "write_file"),
			makeToolCall("tc3", "list_dir"),
		),
		makeUserMsg("u2", "continue"),
	}

	report := RepairTranscript(messages)

	if report.AddedSyntheticResults() != 3 {
		t.Errorf("expected 3 synthetic results, got %d", report.AddedSyntheticResults())
	}
	// user + assistant + 3 synthetic + user = 6
	if len(report.Messages) != 6 {
		t.Errorf("expected 6 messages, got %d", len(report.Messages))
	}
}

func TestRepairTranscript_DisplacedToolResult(t *testing.T) {
	// Tool result is not immediately after tool call
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1", makeToolCall("tc1", "read_file")),
		makeUserMsg("u2", "wait"),
		makeToolResultMsg("tr1", "tc1", "file contents"),
	}

	report := RepairTranscript(messages)

	if !report.Moved {
		t.Error("expected Moved to be true when tool result is displaced")
	}
	// Verify order: user, assistant, tool_result, user
	if len(report.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(report.Messages))
	}
	if report.Messages[0].Role != models.RoleUser {
		t.Error("message 0 should be user")
	}
	if report.Messages[1].Role != models.RoleAssistant {
		t.Error("message 1 should be assistant")
	}
	if report.Messages[2].Role != models.RoleTool {
		t.Error("message 2 should be tool result")
	}
	if report.Messages[3].Role != models.RoleUser {
		t.Error("message 3 should be user")
	}
}

func TestRepairTranscript_DuplicateToolResult(t *testing.T) {
	// Same tool call ID has multiple results
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1", makeToolCall("tc1", "read_file")),
		makeToolResultMsg("tr1", "tc1", "first result"),
		makeToolResultMsg("tr2", "tc1", "duplicate result"),
	}

	report := RepairTranscript(messages)

	if report.DroppedDuplicates() != 1 {
		t.Errorf("expected 1 dropped duplicate, got %d", report.DroppedDuplicates())
	}
	if len(report.Messages) != 3 {
		t.Errorf("expected 3 messages (user, assistant, tool_result), got %d", len(report.Messages))
	}
}

func TestRepairTranscript_OrphanToolResult(t *testing.T) {
	// Tool result without matching tool call
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeToolResultMsg("tr1", "nonexistent-tc", "orphan result"),
		makeAssistantMsg("a1"),
	}

	report := RepairTranscript(messages)

	if report.DroppedOrphans() != 1 {
		t.Errorf("expected 1 dropped orphan, got %d", report.DroppedOrphans())
	}
	if len(report.Messages) != 2 {
		t.Errorf("expected 2 messages (user, assistant), got %d", len(report.Messages))
	}
}

func TestRepairTranscript_ComplexScenario(t *testing.T) {
	// Complex scenario with multiple issues:
	// - Displaced results
	// - Missing results
	// - Duplicates
	// - Orphans
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeToolResultMsg("orphan1", "orphan-id", "orphan before any call"), // orphan
		makeAssistantMsg("a1",
			makeToolCall("tc1", "tool1"),
			makeToolCall("tc2", "tool2"),
		),
		makeUserMsg("u2", "wait"),                       // displaces results
		makeToolResultMsg("tr1", "tc1", "result for 1"), // displaced
		makeToolResultMsg("tr2", "tc2", "result for 2"), // displaced
		makeToolResultMsg("tr3", "tc1", "duplicate"),    // duplicate
		makeAssistantMsg("a2", makeToolCall("tc3", "tool3")),
		// tc3 has no result - will need synthetic
		makeToolResultMsg("orphan2", "orphan-id2", "another orphan"), // orphan
		makeUserMsg("u3", "done"),
	}

	report := RepairTranscript(messages)

	if report.AddedSyntheticResults() != 1 {
		t.Errorf("expected 1 synthetic result (for tc3), got %d", report.AddedSyntheticResults())
	}
	if report.DroppedDuplicates() != 1 {
		t.Errorf("expected 1 dropped duplicate, got %d", report.DroppedDuplicates())
	}
	if report.DroppedOrphans() != 2 {
		t.Errorf("expected 2 dropped orphans, got %d", report.DroppedOrphans())
	}
	if !report.Moved {
		t.Error("expected Moved to be true")
	}

	// Verify message order is correct
	expectedOrder := []models.Role{
		models.RoleUser,      // u1
		models.RoleAssistant, // a1
		models.RoleTool,      // tc1 result
		models.RoleTool,      // tc2 result
		models.RoleUser,      // u2
		models.RoleAssistant, // a2
		models.RoleTool,      // synthetic tc3 result
		models.RoleUser,      // u3
	}

	if len(report.Messages) != len(expectedOrder) {
		t.Errorf("expected %d messages, got %d", len(expectedOrder), len(report.Messages))
		for i, msg := range report.Messages {
			t.Logf("  %d: role=%s id=%s", i, msg.Role, msg.ID)
		}
		return
	}

	for i, expected := range expectedOrder {
		if report.Messages[i].Role != expected {
			t.Errorf("message %d: expected role %s, got %s", i, expected, report.Messages[i].Role)
		}
	}
}

func TestRepairTranscript_EmptyMessages(t *testing.T) {
	report := RepairTranscript(nil)
	if len(report.Messages) != 0 {
		t.Errorf("expected 0 messages for nil input, got %d", len(report.Messages))
	}

	report = RepairTranscript([]*models.Message{})
	if len(report.Messages) != 0 {
		t.Errorf("expected 0 messages for empty input, got %d", len(report.Messages))
	}
}

func TestRepairTranscript_NilMessages(t *testing.T) {
	// Test that nil messages in input are handled gracefully
	// The implementation skips nil messages during processing
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		nil,
		makeAssistantMsg("a1"),
		nil,
	}

	report := RepairTranscript(messages)

	// The implementation returns the original slice if no changes were made
	// Nil messages are skipped during iteration but the original slice is returned
	// This is expected behavior - the repair focuses on tool call/result pairing
	if len(report.Messages) != 4 {
		t.Errorf("expected 4 messages (original slice returned when no repairs needed), got %d", len(report.Messages))
	}

	// Verify no panic occurred and non-nil messages are accessible
	nonNilCount := 0
	for _, m := range report.Messages {
		if m != nil {
			nonNilCount++
		}
	}
	if nonNilCount != 2 {
		t.Errorf("expected 2 non-nil messages, got %d", nonNilCount)
	}
}

func TestRepairTranscript_AssistantWithoutToolCalls(t *testing.T) {
	// Plain assistant messages without tool calls should pass through
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1"),
		makeUserMsg("u2", "goodbye"),
		makeAssistantMsg("a2"),
	}

	report := RepairTranscript(messages)

	if len(report.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(report.Messages))
	}
	if report.AddedSyntheticResults() != 0 {
		t.Errorf("expected 0 synthetic results, got %d", report.AddedSyntheticResults())
	}
}

func TestRepairTranscript_MultipleToolCallsPartialResults(t *testing.T) {
	// Assistant makes 3 tool calls, only 1 has result
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1",
			makeToolCall("tc1", "tool1"),
			makeToolCall("tc2", "tool2"),
			makeToolCall("tc3", "tool3"),
		),
		makeToolResultMsg("tr2", "tc2", "only result for tc2"),
		makeUserMsg("u2", "continue"),
	}

	report := RepairTranscript(messages)

	// Should add synthetic results for tc1 and tc3
	if report.AddedSyntheticResults() != 2 {
		t.Errorf("expected 2 synthetic results, got %d", report.AddedSyntheticResults())
	}

	// Verify all tool calls have results in order
	// user, assistant, tc1-synthetic, tc2-real, tc3-synthetic, user
	if len(report.Messages) != 6 {
		t.Errorf("expected 6 messages, got %d", len(report.Messages))
	}
}

func TestSanitizeToolUseResultPairing(t *testing.T) {
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1", makeToolCall("tc1", "read_file")),
		// Missing tool result
		makeUserMsg("u2", "continue"),
	}

	repaired := SanitizeToolUseResultPairing(messages)

	if len(repaired) != 4 {
		t.Errorf("expected 4 messages, got %d", len(repaired))
	}

	// Verify synthetic result was inserted
	if repaired[2].Role != models.RoleTool {
		t.Error("expected synthetic tool result at index 2")
	}
}

func TestSanitizeTranscript(t *testing.T) {
	// Verify SanitizeTranscript matches SanitizeToolUseResultPairing
	messages := []*models.Message{
		makeUserMsg("u1", "hello"),
		makeAssistantMsg("a1", makeToolCall("tc1", "read_file")),
		makeUserMsg("u2", "continue"),
	}

	result1 := SanitizeTranscript(messages)
	result2 := SanitizeToolUseResultPairing(messages)

	if len(result1) != len(result2) {
		t.Errorf("SanitizeTranscript and SanitizeToolUseResultPairing should produce same length: %d vs %d",
			len(result1), len(result2))
	}
}

func TestValidateToolCallPairing(t *testing.T) {
	tests := []struct {
		name        string
		messages    []*models.Message
		wantMissing int
	}{
		{
			name: "all_paired",
			messages: []*models.Message{
				makeAssistantMsg("a1", makeToolCall("tc1", "tool")),
				makeToolResultMsg("tr1", "tc1", "result"),
			},
			wantMissing: 0,
		},
		{
			name: "missing_one",
			messages: []*models.Message{
				makeAssistantMsg("a1", makeToolCall("tc1", "tool")),
				makeAssistantMsg("a2"),
			},
			wantMissing: 1,
		},
		{
			name: "missing_multiple",
			messages: []*models.Message{
				makeAssistantMsg("a1",
					makeToolCall("tc1", "tool1"),
					makeToolCall("tc2", "tool2"),
				),
			},
			wantMissing: 2,
		},
		{
			name: "partial_results",
			messages: []*models.Message{
				makeAssistantMsg("a1",
					makeToolCall("tc1", "tool1"),
					makeToolCall("tc2", "tool2"),
				),
				makeToolResultMsg("tr1", "tc1", "result"),
			},
			wantMissing: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing := ValidateToolCallPairing(tt.messages)
			if len(missing) != tt.wantMissing {
				t.Errorf("expected %d missing, got %d: %v", tt.wantMissing, len(missing), missing)
			}
		})
	}
}

func TestExtractToolCallIDs(t *testing.T) {
	tests := []struct {
		name string
		msg  *models.Message
		want []string
	}{
		{
			name: "nil_message",
			msg:  nil,
			want: nil,
		},
		{
			name: "user_message",
			msg:  makeUserMsg("u1", "hello"),
			want: nil,
		},
		{
			name: "assistant_no_tools",
			msg:  makeAssistantMsg("a1"),
			want: nil,
		},
		{
			name: "assistant_with_tools",
			msg: makeAssistantMsg("a1",
				makeToolCall("tc1", "tool1"),
				makeToolCall("tc2", "tool2"),
			),
			want: []string{"tc1", "tc2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractToolCallIDs(tt.msg)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
				return
			}
			for i, id := range tt.want {
				if got[i] != id {
					t.Errorf("at %d: expected %s, got %s", i, id, got[i])
				}
			}
		})
	}
}

func TestExtractToolResultID(t *testing.T) {
	tests := []struct {
		name string
		msg  *models.Message
		want string
	}{
		{
			name: "nil_message",
			msg:  nil,
			want: "",
		},
		{
			name: "user_message",
			msg:  makeUserMsg("u1", "hello"),
			want: "",
		},
		{
			name: "tool_result",
			msg:  makeToolResultMsg("tr1", "tc1", "result"),
			want: "tc1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractToolResultID(tt.msg)
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestToolCallGuard(t *testing.T) {
	t.Run("track_and_record", func(t *testing.T) {
		guard := NewToolCallGuard()

		msg := makeAssistantMsg("a1",
			makeToolCall("tc1", "tool1"),
			makeToolCall("tc2", "tool2"),
		)
		guard.TrackToolCalls(msg)

		if !guard.HasPending() {
			t.Error("expected pending tool calls")
		}

		pending := guard.GetPendingIDs()
		if len(pending) != 2 {
			t.Errorf("expected 2 pending, got %d", len(pending))
		}

		guard.RecordToolResult("tc1")
		pending = guard.GetPendingIDs()
		if len(pending) != 1 {
			t.Errorf("expected 1 pending after recording tc1, got %d", len(pending))
		}

		guard.RecordToolResult("tc2")
		if guard.HasPending() {
			t.Error("expected no pending after recording all")
		}
	})

	t.Run("flush_pending", func(t *testing.T) {
		guard := NewToolCallGuard()

		msg := makeAssistantMsg("a1",
			makeToolCall("tc1", "tool1"),
			makeToolCall("tc2", "tool2"),
		)
		guard.TrackToolCalls(msg)

		synthetics := guard.FlushPending()
		if len(synthetics) != 2 {
			t.Errorf("expected 2 synthetics, got %d", len(synthetics))
		}

		if guard.HasPending() {
			t.Error("expected no pending after flush")
		}

		// Verify synthetic messages
		for _, s := range synthetics {
			if s.Role != models.RoleTool {
				t.Errorf("synthetic should have role tool, got %s", s.Role)
			}
			if len(s.ToolResults) == 0 {
				t.Error("synthetic should have tool results")
			}
			if !s.ToolResults[0].IsError {
				t.Error("synthetic should be marked as error")
			}
		}
	})

	t.Run("track_nil_message", func(t *testing.T) {
		guard := NewToolCallGuard()
		guard.TrackToolCalls(nil)
		if guard.HasPending() {
			t.Error("tracking nil message should not add pending")
		}
	})

	t.Run("track_non_assistant", func(t *testing.T) {
		guard := NewToolCallGuard()
		guard.TrackToolCalls(makeUserMsg("u1", "hello"))
		if guard.HasPending() {
			t.Error("tracking user message should not add pending")
		}
	})
}

func TestMakeMissingToolResult(t *testing.T) {
	result := makeMissingToolResult("tc123", "read_file")

	if result.Role != models.RoleTool {
		t.Errorf("expected role tool, got %s", result.Role)
	}
	if len(result.ToolResults) != 1 {
		t.Fatal("expected 1 tool result")
	}
	if result.ToolResults[0].ToolCallID != "tc123" {
		t.Errorf("expected tool call ID tc123, got %s", result.ToolResults[0].ToolCallID)
	}
	if !result.ToolResults[0].IsError {
		t.Error("expected IsError to be true")
	}
	if result.Metadata["synthetic"] != true {
		t.Error("expected synthetic metadata to be true")
	}
	if result.Metadata["tool_name"] != "read_file" {
		t.Errorf("expected tool_name read_file, got %v", result.Metadata["tool_name"])
	}
}

func TestMakeMissingToolResult_EmptyToolName(t *testing.T) {
	result := makeMissingToolResult("tc123", "")

	if result.Metadata["tool_name"] != "unknown" {
		t.Errorf("expected tool_name 'unknown' for empty name, got %v", result.Metadata["tool_name"])
	}
}

func TestMarshalToolInput(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "{}",
		},
		{
			name:  "empty_object",
			input: json.RawMessage(`{}`),
			want:  "{}",
		},
		{
			name:  "with_data",
			input: json.RawMessage(`{"key":"value"}`),
			want:  `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarshalToolInput(tt.input)
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestTranscriptRepairReport_Methods(t *testing.T) {
	// Create a report with known values
	report := TranscriptRepairReport{
		Messages: []*models.Message{
			makeUserMsg("u1", "hello"),
		},
		Added: []*models.Message{
			makeMissingToolResult("tc1", "tool1"),
			makeMissingToolResult("tc2", "tool2"),
		},
		DroppedDuplicateCount: 3,
		DroppedOrphanCount:    4,
		Moved:                 true,
	}

	if report.AddedSyntheticResults() != 2 {
		t.Errorf("AddedSyntheticResults: expected 2, got %d", report.AddedSyntheticResults())
	}
	if report.DroppedDuplicates() != 3 {
		t.Errorf("DroppedDuplicates: expected 3, got %d", report.DroppedDuplicates())
	}
	if report.DroppedOrphans() != 4 {
		t.Errorf("DroppedOrphans: expected 4, got %d", report.DroppedOrphans())
	}
}

func TestRepairTranscript_ToolResultsFollowAssistant(t *testing.T) {
	// When an assistant makes multiple tool calls, all results should
	// follow directly after the assistant message (order preserved from input)
	messages := []*models.Message{
		makeAssistantMsg("a1",
			makeToolCall("tc1", "first"),
			makeToolCall("tc2", "second"),
			makeToolCall("tc3", "third"),
		),
		// Results provided in specific order
		makeToolResultMsg("tr3", "tc3", "third result"),
		makeToolResultMsg("tr1", "tc1", "first result"),
		makeToolResultMsg("tr2", "tc2", "second result"),
	}

	report := RepairTranscript(messages)

	// All 4 messages should be present
	if len(report.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(report.Messages))
	}

	// First should be assistant
	if report.Messages[0].Role != models.RoleAssistant {
		t.Error("message 0 should be assistant")
	}

	// All tool results should follow the assistant message
	toolResultCount := 0
	for i := 1; i < len(report.Messages); i++ {
		if report.Messages[i].Role != models.RoleTool {
			t.Errorf("message %d should be tool result, got %s", i, report.Messages[i].Role)
		}
		toolResultCount++
	}

	if toolResultCount != 3 {
		t.Errorf("expected 3 tool results, got %d", toolResultCount)
	}

	// Verify all tool call IDs are present
	foundIDs := make(map[string]bool)
	for i := 1; i < len(report.Messages); i++ {
		if len(report.Messages[i].ToolResults) > 0 {
			foundIDs[report.Messages[i].ToolResults[0].ToolCallID] = true
		}
	}
	for _, id := range []string{"tc1", "tc2", "tc3"} {
		if !foundIDs[id] {
			t.Errorf("missing tool result for %s", id)
		}
	}
}

func TestRepairTranscript_ConsecutiveAssistantMessages(t *testing.T) {
	// Two assistant messages with tool calls in a row (first missing results)
	messages := []*models.Message{
		makeAssistantMsg("a1", makeToolCall("tc1", "tool1")),
		makeAssistantMsg("a2", makeToolCall("tc2", "tool2")),
		makeToolResultMsg("tr2", "tc2", "result for tc2"),
	}

	report := RepairTranscript(messages)

	// Should insert synthetic for tc1 before a2
	if report.AddedSyntheticResults() != 1 {
		t.Errorf("expected 1 synthetic, got %d", report.AddedSyntheticResults())
	}

	// Expected order: a1, synthetic-tc1, a2, tr2
	if len(report.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(report.Messages))
	}

	if report.Messages[0].ID != "a1" {
		t.Error("message 0 should be a1")
	}
	if report.Messages[1].Role != models.RoleTool {
		t.Error("message 1 should be synthetic tool result")
	}
	if report.Messages[2].ID != "a2" {
		t.Error("message 2 should be a2")
	}
	if report.Messages[3].ID != "tr2" {
		t.Error("message 3 should be tr2")
	}
}
