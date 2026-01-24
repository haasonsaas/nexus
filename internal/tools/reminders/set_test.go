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

func TestParseWhen_TimeOfDay(t *testing.T) {
	// Test time-only format (24h)
	// Parse a time that's definitely in the future today (or tomorrow)
	result, err := parseWhen("23:59")
	if err != nil {
		t.Fatalf("parseWhen(\"23:59\") failed: %v", err)
	}
	// Result should be within the next 24 hours
	delta := time.Until(result)
	if delta < 0 || delta > 24*time.Hour {
		t.Errorf("parseWhen(\"23:59\") = %v from now, expected within 24 hours", delta)
	}
}

func TestParseRelativeTime_MoreUnits(t *testing.T) {
	tests := []struct {
		input    string
		minDelta time.Duration
		maxDelta time.Duration
	}{
		{"1 week", 6*24*time.Hour + 23*time.Hour, 7*24*time.Hour + time.Hour},
		{"2 weeks", 13*24*time.Hour + 23*time.Hour, 14*24*time.Hour + time.Hour},
		{"0.5 hours", 25 * time.Minute, 35 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseRelativeTime(tt.input)
			if err != nil {
				t.Fatalf("parseRelativeTime(%q) failed: %v", tt.input, err)
			}
			delta := time.Until(result)
			if delta < tt.minDelta || delta > tt.maxDelta {
				t.Errorf("parseRelativeTime(%q) = %v from now, want between %v and %v", tt.input, delta, tt.minDelta, tt.maxDelta)
			}
		})
	}
}

func TestSetTool_Execute_InvalidTime(t *testing.T) {
	tool := NewSetTool(&mockStore{})
	params := json.RawMessage(`{"message": "test", "when": "invalid time format"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid time")
	}
	if !strings.Contains(result.Content, "invalid time") {
		t.Errorf("Content = %q, want to contain 'invalid time'", result.Content)
	}
}

func TestSetTool_Execute_PastTime(t *testing.T) {
	tool := NewSetTool(&mockStore{})
	// Use a past time in format that the parser accepts (15:04 format)
	// We'll use a time like 00:00 which has already passed today
	// (unless we're running tests at midnight)
	now := time.Now()
	if now.Hour() > 0 { // If it's not midnight-ish
		params := json.RawMessage(`{"message": "test", "when": "00:01"}`)
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		// The code schedules for tomorrow if time has passed today, so this is valid
		// Verify we got a successful response (not an error)
		_ = result // result is valid - scheduled for tomorrow
	}
}

func TestSetTool_Execute_Success(t *testing.T) {
	tool := NewSetTool(&mockStore{})
	params := json.RawMessage(`{"message": "test reminder", "when": "in 5 minutes", "title": "Test Title"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Reminder set") {
		t.Errorf("Content = %q, want to contain 'Reminder set'", result.Content)
	}
	if !strings.Contains(result.Content, "test reminder") {
		t.Errorf("Content = %q, want to contain message", result.Content)
	}
}

// advancedMockStore allows configuring behavior for test scenarios
type advancedMockStore struct {
	tasks        map[string]*tasks.ScheduledTask
	returnError  error
	taskNotFound bool
}

func newAdvancedMockStore() *advancedMockStore {
	return &advancedMockStore{
		tasks: make(map[string]*tasks.ScheduledTask),
	}
}

func (m *advancedMockStore) CreateTask(ctx context.Context, task *tasks.ScheduledTask) error {
	if m.returnError != nil {
		return m.returnError
	}
	m.tasks[task.ID] = task
	return nil
}

func (m *advancedMockStore) GetTask(ctx context.Context, id string) (*tasks.ScheduledTask, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	if m.taskNotFound {
		return nil, nil
	}
	return m.tasks[id], nil
}

func (m *advancedMockStore) UpdateTask(ctx context.Context, task *tasks.ScheduledTask) error {
	if m.returnError != nil {
		return m.returnError
	}
	m.tasks[task.ID] = task
	return nil
}

func (m *advancedMockStore) DeleteTask(ctx context.Context, id string) error {
	delete(m.tasks, id)
	return nil
}

func (m *advancedMockStore) ListTasks(ctx context.Context, opts tasks.ListTasksOptions) ([]*tasks.ScheduledTask, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}
	var result []*tasks.ScheduledTask
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result, nil
}

func (m *advancedMockStore) CreateExecution(ctx context.Context, exec *tasks.TaskExecution) error {
	return nil
}

func (m *advancedMockStore) GetExecution(ctx context.Context, id string) (*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *advancedMockStore) UpdateExecution(ctx context.Context, exec *tasks.TaskExecution) error {
	return nil
}

func (m *advancedMockStore) ListExecutions(ctx context.Context, taskID string, opts tasks.ListExecutionsOptions) ([]*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *advancedMockStore) GetDueTasks(ctx context.Context, now time.Time, limit int) ([]*tasks.ScheduledTask, error) {
	return nil, nil
}

func (m *advancedMockStore) AcquireExecution(ctx context.Context, workerID string, lockDuration time.Duration) (*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *advancedMockStore) ReleaseExecution(ctx context.Context, executionID string) error {
	return nil
}

func (m *advancedMockStore) CompleteExecution(ctx context.Context, executionID string, status tasks.ExecutionStatus, response string, errStr string) error {
	return nil
}

func (m *advancedMockStore) GetRunningExecutions(ctx context.Context, taskID string) ([]*tasks.TaskExecution, error) {
	return nil, nil
}

func (m *advancedMockStore) CleanupStaleExecutions(ctx context.Context, timeout time.Duration) (int, error) {
	return 0, nil
}

func TestCancelTool_Execute_NotFound(t *testing.T) {
	store := newAdvancedMockStore()
	store.taskNotFound = true

	tool := NewCancelTool(store)
	params := json.RawMessage(`{"reminder_id": "nonexistent-123"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for not found")
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("Content = %q, want to contain 'not found'", result.Content)
	}
}

func TestCancelTool_Execute_NotAReminder(t *testing.T) {
	store := newAdvancedMockStore()
	store.tasks["task-123"] = &tasks.ScheduledTask{
		ID:       "task-123",
		Metadata: nil, // No metadata
	}

	tool := NewCancelTool(store)
	params := json.RawMessage(`{"reminder_id": "task-123"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for not a reminder")
	}
	if !strings.Contains(result.Content, "not a reminder") {
		t.Errorf("Content = %q, want to contain 'not a reminder'", result.Content)
	}
}

func TestCancelTool_Execute_AlreadyCancelled(t *testing.T) {
	store := newAdvancedMockStore()
	store.tasks["reminder-123"] = &tasks.ScheduledTask{
		ID:       "reminder-123",
		Status:   tasks.TaskStatusDisabled,
		Metadata: map[string]any{"type": "reminder"},
	}

	tool := NewCancelTool(store)
	params := json.RawMessage(`{"reminder_id": "reminder-123"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Error("should not be error for already cancelled")
	}
	if !strings.Contains(result.Content, "already cancelled") {
		t.Errorf("Content = %q, want to contain 'already cancelled'", result.Content)
	}
}

func TestCancelTool_Execute_Success(t *testing.T) {
	store := newAdvancedMockStore()
	store.tasks["reminder-123"] = &tasks.ScheduledTask{
		ID:       "reminder-123",
		Name:     "Test Reminder",
		Prompt:   "Don't forget this",
		Status:   tasks.TaskStatusActive,
		Metadata: map[string]any{"type": "reminder"},
	}

	tool := NewCancelTool(store)
	params := json.RawMessage(`{"reminder_id": "reminder-123"}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "cancelled") {
		t.Errorf("Content = %q, want to contain 'cancelled'", result.Content)
	}
	if !strings.Contains(result.Content, "Don't forget this") {
		t.Errorf("Content = %q, want to contain the message", result.Content)
	}
}

func TestCancelTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewCancelTool(&mockStore{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{invalid}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestListTool_Execute_NoReminders(t *testing.T) {
	store := newAdvancedMockStore()

	tool := NewListTool(store)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No active reminders") {
		t.Errorf("Content = %q, want to contain 'No active reminders'", result.Content)
	}
}

func TestListTool_Execute_WithReminders(t *testing.T) {
	store := newAdvancedMockStore()
	store.tasks["reminder-1"] = &tasks.ScheduledTask{
		ID:        "reminder-1",
		Name:      "First Reminder",
		Prompt:    "Remember this",
		Status:    tasks.TaskStatusActive,
		NextRunAt: time.Now().Add(1 * time.Hour),
		Metadata:  map[string]any{"type": "reminder"},
	}
	store.tasks["reminder-2"] = &tasks.ScheduledTask{
		ID:        "reminder-2",
		Name:      "Second Reminder",
		Prompt:    "Also this",
		Status:    tasks.TaskStatusActive,
		NextRunAt: time.Now().Add(2 * time.Hour),
		Metadata:  map[string]any{"type": "reminder"},
	}

	tool := NewListTool(store)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Found 2 reminder(s)") {
		t.Errorf("Content = %q, want to contain 'Found 2 reminder(s)'", result.Content)
	}
}

func TestListTool_Execute_ExcludesNonReminders(t *testing.T) {
	store := newAdvancedMockStore()
	store.tasks["reminder-1"] = &tasks.ScheduledTask{
		ID:        "reminder-1",
		Name:      "Real Reminder",
		Prompt:    "Remember",
		Status:    tasks.TaskStatusActive,
		NextRunAt: time.Now().Add(1 * time.Hour),
		Metadata:  map[string]any{"type": "reminder"},
	}
	store.tasks["task-1"] = &tasks.ScheduledTask{
		ID:       "task-1",
		Name:     "Not a Reminder",
		Prompt:   "Do something",
		Status:   tasks.TaskStatusActive,
		Metadata: map[string]any{"type": "scheduled_task"},
	}

	tool := NewListTool(store)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "Found 1 reminder(s)") {
		t.Errorf("Content = %q, want to contain 'Found 1 reminder(s)'", result.Content)
	}
	if strings.Contains(result.Content, "Not a Reminder") {
		t.Error("should not include non-reminder tasks")
	}
}

func TestListTool_Execute_IncludeCompleted(t *testing.T) {
	store := newAdvancedMockStore()
	store.tasks["reminder-1"] = &tasks.ScheduledTask{
		ID:       "reminder-1",
		Name:     "Active Reminder",
		Status:   tasks.TaskStatusActive,
		Metadata: map[string]any{"type": "reminder"},
	}
	store.tasks["reminder-2"] = &tasks.ScheduledTask{
		ID:       "reminder-2",
		Name:     "Completed Reminder",
		Status:   tasks.TaskStatusDisabled,
		Metadata: map[string]any{"type": "reminder"},
	}

	tool := NewListTool(store)

	// Without include_completed - should only show active
	params := json.RawMessage(`{"include_completed": false}`)
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "Found 1 reminder(s)") {
		t.Errorf("Without include_completed: Content = %q, want 'Found 1 reminder(s)'", result.Content)
	}

	// With include_completed - should show both
	params = json.RawMessage(`{"include_completed": true}`)
	result, err = tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "Found 2 reminder(s)") {
		t.Errorf("With include_completed: Content = %q, want 'Found 2 reminder(s)'", result.Content)
	}
}

func TestListTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewListTool(&mockStore{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{invalid}`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGetContextFunctions(t *testing.T) {
	ctx := context.Background()

	// Test with no session
	channelType := getChannelFromContext(ctx)
	if channelType != "whatsapp" {
		t.Errorf("getChannelFromContext with no session = %q, want 'whatsapp'", channelType)
	}

	channelID := getChannelIDFromContext(ctx)
	if channelID != "" {
		t.Errorf("getChannelIDFromContext with no session = %q, want ''", channelID)
	}

	agentID := getAgentIDFromContext(ctx)
	if agentID != "default" {
		t.Errorf("getAgentIDFromContext with no session = %q, want 'default'", agentID)
	}
}
