package reminders

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/tasks"
)

func TestParseWhen_RelativeTime(t *testing.T) {
	tests := []struct {
		input    string
		minDelta time.Duration
		maxDelta time.Duration
	}{
		{"in 5 minutes", 4 * time.Minute, 6 * time.Minute},
		{"in 1 hour", 59 * time.Minute, 61 * time.Minute},
		{"in 30 seconds", 25 * time.Second, 35 * time.Second},
		{"in 2 hours", 119 * time.Minute, 121 * time.Minute},
		{"in 1 day", 23 * time.Hour, 25 * time.Hour},
		{"in 10 mins", 9 * time.Minute, 11 * time.Minute},
		{"in 2 hrs", 119 * time.Minute, 121 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseWhen(tt.input)
			if err != nil {
				t.Fatalf("parseWhen(%q) failed: %v", tt.input, err)
			}

			delta := time.Until(result)
			if delta < tt.minDelta || delta > tt.maxDelta {
				t.Errorf("parseWhen(%q) = %v from now, want between %v and %v", tt.input, delta, tt.minDelta, tt.maxDelta)
			}
		})
	}
}

func TestParseWhen_InvalidInput(t *testing.T) {
	tests := []string{
		"",
		"now",
		"yesterday",
		"in",
		"in 5",
		"in minutes",
		"5 minutes",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseWhen(input)
			if err == nil {
				t.Errorf("parseWhen(%q) should have failed", input)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Second, "30 seconds"},
		{1 * time.Minute, "1 minute"},
		{5 * time.Minute, "5 minutes"},
		{1 * time.Hour, "1 hour"},
		{2 * time.Hour, "2.0 hours"},
		{24 * time.Hour, "1 day"},
		{48 * time.Hour, "2.0 days"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.input)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatReminderName(t *testing.T) {
	tests := []struct {
		title    string
		message  string
		expected string
	}{
		{"", "Short message", "Reminder: Short message"},
		{"Custom Title", "Any message", "Reminder: Custom Title"},
		{"", "This is a very long message that exceeds fifty characters and should be truncated", "Reminder: This is a very long message that exceeds fifty ..."},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatReminderName(tt.title, tt.message)
			if result != tt.expected {
				t.Errorf("formatReminderName(%q, %q) = %q, want %q", tt.title, tt.message, result, tt.expected)
			}
		})
	}
}

func TestListTool_Name(t *testing.T) {
	tool := NewListTool(nil)
	if name := tool.Name(); name != "reminder_list" {
		t.Errorf("Name() = %q, want %q", name, "reminder_list")
	}
}

func TestListTool_Description(t *testing.T) {
	tool := NewListTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestListTool_Schema(t *testing.T) {
	tool := NewListTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema should not be empty")
	}

	// Validate it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema is not valid JSON: %v", err)
	}
}

func TestListTool_Execute_NilStore(t *testing.T) {
	tool := NewListTool(nil)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for nil store")
	}
	if !strings.Contains(result.Content, "unavailable") {
		t.Errorf("Content = %q, want to contain 'unavailable'", result.Content)
	}
}

func TestListInput_Struct(t *testing.T) {
	input := ListInput{
		IncludeCompleted: true,
		Limit:            50,
	}
	if !input.IncludeCompleted {
		t.Error("IncludeCompleted should be true")
	}
	if input.Limit != 50 {
		t.Errorf("Limit = %d, want 50", input.Limit)
	}
}

func TestCancelTool_Name(t *testing.T) {
	tool := NewCancelTool(nil)
	if name := tool.Name(); name != "reminder_cancel" {
		t.Errorf("Name() = %q, want %q", name, "reminder_cancel")
	}
}

func TestCancelTool_Description(t *testing.T) {
	tool := NewCancelTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestCancelTool_Schema(t *testing.T) {
	tool := NewCancelTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema should not be empty")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema is not valid JSON: %v", err)
	}

	// Check required field
	required, ok := parsed["required"].([]any)
	if !ok {
		t.Fatal("schema required field not found")
	}
	found := false
	for _, r := range required {
		if r == "reminder_id" {
			found = true
			break
		}
	}
	if !found {
		t.Error("reminder_id should be required")
	}
}

func TestCancelTool_Execute_NilStore(t *testing.T) {
	tool := NewCancelTool(nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"reminder_id": "test-123"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for nil store")
	}
}

func TestCancelTool_Execute_EmptyReminderID(t *testing.T) {
	tool := NewCancelTool(&mockStore{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"reminder_id": ""}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for empty reminder_id")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("Content = %q, want to contain 'required'", result.Content)
	}
}

func TestCancelInput_Struct(t *testing.T) {
	input := CancelInput{
		ReminderID: "reminder-123",
	}
	if input.ReminderID != "reminder-123" {
		t.Errorf("ReminderID = %q, want %q", input.ReminderID, "reminder-123")
	}
}

func TestSetTool_Name(t *testing.T) {
	tool := NewSetTool(nil)
	if name := tool.Name(); name != "reminder_set" {
		t.Errorf("Name() = %q, want %q", name, "reminder_set")
	}
}

func TestSetTool_Description(t *testing.T) {
	tool := NewSetTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestSetTool_Schema(t *testing.T) {
	tool := NewSetTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("Schema should not be empty")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema is not valid JSON: %v", err)
	}
}

func TestSetTool_Execute_NilStore(t *testing.T) {
	tool := NewSetTool(nil)
	params := json.RawMessage(`{"message": "test", "when": "in 5 minutes"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for nil store")
	}
}

func TestSetTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewSetTool(&mockStore{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{invalid json}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSetTool_Execute_MissingMessage(t *testing.T) {
	tool := NewSetTool(&mockStore{})
	params := json.RawMessage(`{"when": "in 5 minutes"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing message")
	}
}

func TestSetTool_Execute_MissingWhen(t *testing.T) {
	tool := NewSetTool(&mockStore{})
	params := json.RawMessage(`{"message": "test"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing when")
	}
}

func TestSetInput_Struct(t *testing.T) {
	input := SetInput{
		Message: "Don't forget!",
		When:    "in 5 minutes",
		Title:   "My Reminder",
	}
	if input.Message != "Don't forget!" {
		t.Errorf("Message = %q", input.Message)
	}
	if input.When != "in 5 minutes" {
		t.Errorf("When = %q", input.When)
	}
	if input.Title != "My Reminder" {
		t.Errorf("Title = %q", input.Title)
	}
}

// mockStore is a minimal mock for testing
type mockStore struct{}

func (m *mockStore) CreateTask(ctx context.Context, task *tasks.ScheduledTask) error {
	return nil
}

func (m *mockStore) GetTask(ctx context.Context, id string) (*tasks.ScheduledTask, error) {
	return nil, nil
}

func (m *mockStore) UpdateTask(ctx context.Context, task *tasks.ScheduledTask) error {
	return nil
}

func (m *mockStore) DeleteTask(ctx context.Context, id string) error {
	return nil
}

func (m *mockStore) ListTasks(ctx context.Context, opts tasks.ListTasksOptions) ([]*tasks.ScheduledTask, error) {
	return nil, nil
}

func (m *mockStore) CreateExecution(ctx context.Context, exec *tasks.TaskExecution) error {
	return nil
}

func (m *mockStore) GetExecution(ctx context.Context, id string) (*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *mockStore) UpdateExecution(ctx context.Context, exec *tasks.TaskExecution) error {
	return nil
}

func (m *mockStore) ListExecutions(ctx context.Context, taskID string, opts tasks.ListExecutionsOptions) ([]*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *mockStore) GetDueTasks(ctx context.Context, now time.Time, limit int) ([]*tasks.ScheduledTask, error) {
	return nil, nil
}

func (m *mockStore) AcquireExecution(ctx context.Context, workerID string, lockDuration time.Duration) (*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *mockStore) ReleaseExecution(ctx context.Context, executionID string) error {
	return nil
}

func (m *mockStore) CompleteExecution(ctx context.Context, executionID string, status tasks.ExecutionStatus, response string, errStr string) error {
	return nil
}

func (m *mockStore) GetRunningExecutions(ctx context.Context, taskID string) ([]*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *mockStore) CleanupStaleExecutions(ctx context.Context, timeout time.Duration) (int, error) {
	return 0, nil
}
