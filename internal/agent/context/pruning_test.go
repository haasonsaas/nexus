package context

import (
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestPruneContextMessages_SoftTrimOnly(t *testing.T) {
	settings := DefaultContextPruningSettings()
	settings.KeepLastAssistants = 1
	settings.SoftTrimRatio = 0.01
	settings.HardClearRatio = 0.9
	settings.MinPrunableToolChars = 1
	settings.SoftTrim.MaxChars = 50
	settings.SoftTrim.HeadChars = 10
	settings.SoftTrim.TailChars = 10
	settings.HardClear.Enabled = true

	history := []*models.Message{
		newMessage(models.RoleUser, "hello"),
		assistantToolCall("tc-1", "fetch"),
		toolResult("tc-1", strings.Repeat("a", 200)),
		newMessage(models.RoleAssistant, "done"),
	}

	out := PruneContextMessages(history, settings, 1000)
	got := out[2].ToolResults[0].Content
	if got == strings.Repeat("a", 200) {
		t.Fatalf("expected tool result to be trimmed")
	}
	if !strings.Contains(got, "Tool result trimmed") {
		t.Fatalf("expected trim note, got %q", got)
	}
	if got == settings.HardClear.Placeholder {
		t.Fatalf("unexpected hard clear placeholder")
	}
}

func TestPruneContextMessages_HardClear(t *testing.T) {
	settings := DefaultContextPruningSettings()
	settings.KeepLastAssistants = 1
	settings.SoftTrimRatio = 0.01
	settings.HardClearRatio = 0.2
	settings.MinPrunableToolChars = 1
	settings.SoftTrim.MaxChars = 50
	settings.SoftTrim.HeadChars = 10
	settings.SoftTrim.TailChars = 10
	settings.HardClear.Enabled = true

	history := []*models.Message{
		newMessage(models.RoleUser, "hello"),
		assistantToolCall("tc-1", "fetch"),
		toolResult("tc-1", strings.Repeat("b", 200)),
		newMessage(models.RoleAssistant, "done"),
	}

	out := PruneContextMessages(history, settings, 100)
	got := out[2].ToolResults[0].Content
	if got != settings.HardClear.Placeholder {
		t.Fatalf("expected hard clear placeholder, got %q", got)
	}
}

func TestPruneContextMessages_AllowDeny(t *testing.T) {
	settings := DefaultContextPruningSettings()
	settings.KeepLastAssistants = 1
	settings.SoftTrimRatio = 0.01
	settings.HardClear.Enabled = false
	settings.SoftTrim.MaxChars = 10
	settings.SoftTrim.HeadChars = 4
	settings.SoftTrim.TailChars = 4
	settings.Tools.Allow = []string{"fetch*"}
	settings.Tools.Deny = []string{"fetch_secret"}

	history := []*models.Message{
		newMessage(models.RoleUser, "hello"),
		assistantToolCall("tc-1", "fetch_public", "tc-2", "fetch_secret"),
		toolResults(
			[]models.ToolResult{
				{ToolCallID: "tc-1", Content: strings.Repeat("p", 40)},
				{ToolCallID: "tc-2", Content: strings.Repeat("s", 40)},
			},
		),
		newMessage(models.RoleAssistant, "done"),
	}

	out := PruneContextMessages(history, settings, 1000)
	publicResult := out[2].ToolResults[0].Content
	secretResult := out[2].ToolResults[1].Content

	if publicResult == strings.Repeat("p", 40) {
		t.Fatalf("expected public tool result to be trimmed")
	}
	if !strings.Contains(publicResult, "Tool result trimmed") {
		t.Fatalf("expected trim note for public tool result")
	}
	if secretResult != strings.Repeat("s", 40) {
		t.Fatalf("expected secret tool result to remain unchanged")
	}
}

func TestPruneContextMessages_UnknownToolNameDefaultAllowed(t *testing.T) {
	settings := DefaultContextPruningSettings()
	settings.KeepLastAssistants = 1
	settings.SoftTrimRatio = 0.01
	settings.HardClear.Enabled = false
	settings.SoftTrim.MaxChars = 10
	settings.SoftTrim.HeadChars = 4
	settings.SoftTrim.TailChars = 4

	history := []*models.Message{
		newMessage(models.RoleUser, "hello"),
		{Role: models.RoleTool, ToolResults: []models.ToolResult{{ToolCallID: "missing", Content: strings.Repeat("x", 40)}}},
		newMessage(models.RoleAssistant, "done"),
	}

	out := PruneContextMessages(history, settings, 1000)
	got := out[1].ToolResults[0].Content
	if got == strings.Repeat("x", 40) {
		t.Fatalf("expected tool result to be trimmed even without tool name")
	}
}

func newMessage(role models.Role, content string) *models.Message {
	return &models.Message{
		Role:    role,
		Content: content,
	}
}

func assistantToolCall(id, name string, rest ...string) *models.Message {
	calls := []models.ToolCall{{ID: id, Name: name}}
	for i := 0; i+1 < len(rest); i += 2 {
		calls = append(calls, models.ToolCall{ID: rest[i], Name: rest[i+1]})
	}
	return &models.Message{
		Role:      models.RoleAssistant,
		ToolCalls: calls,
	}
}

func toolResult(id, content string) *models.Message {
	return &models.Message{
		Role:        models.RoleTool,
		ToolResults: []models.ToolResult{{ToolCallID: id, Content: content}},
	}
}

func toolResults(results []models.ToolResult) *models.Message {
	return &models.Message{
		Role:        models.RoleTool,
		ToolResults: results,
	}
}
