package tasks

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskStatus_Constants(t *testing.T) {
	if TaskStatusActive != "active" {
		t.Errorf("TaskStatusActive = %q, want %q", TaskStatusActive, "active")
	}
	if TaskStatusPaused != "paused" {
		t.Errorf("TaskStatusPaused = %q, want %q", TaskStatusPaused, "paused")
	}
	if TaskStatusDisabled != "disabled" {
		t.Errorf("TaskStatusDisabled = %q, want %q", TaskStatusDisabled, "disabled")
	}
}

func TestExecutionStatus_Constants(t *testing.T) {
	if ExecutionStatusPending != "pending" {
		t.Errorf("ExecutionStatusPending = %q, want %q", ExecutionStatusPending, "pending")
	}
	if ExecutionStatusRunning != "running" {
		t.Errorf("ExecutionStatusRunning = %q, want %q", ExecutionStatusRunning, "running")
	}
	if ExecutionStatusSucceeded != "succeeded" {
		t.Errorf("ExecutionStatusSucceeded = %q, want %q", ExecutionStatusSucceeded, "succeeded")
	}
	if ExecutionStatusFailed != "failed" {
		t.Errorf("ExecutionStatusFailed = %q, want %q", ExecutionStatusFailed, "failed")
	}
	if ExecutionStatusTimedOut != "timed_out" {
		t.Errorf("ExecutionStatusTimedOut = %q, want %q", ExecutionStatusTimedOut, "timed_out")
	}
	if ExecutionStatusCancelled != "cancelled" {
		t.Errorf("ExecutionStatusCancelled = %q, want %q", ExecutionStatusCancelled, "cancelled")
	}
}

func TestExecutionType_Constants(t *testing.T) {
	if ExecutionTypeAgent != "agent" {
		t.Errorf("ExecutionTypeAgent = %q, want %q", ExecutionTypeAgent, "agent")
	}
	if ExecutionTypeMessage != "message" {
		t.Errorf("ExecutionTypeMessage = %q, want %q", ExecutionTypeMessage, "message")
	}
}

func TestTaskExecution_IsTerminal(t *testing.T) {
	tests := []struct {
		status   ExecutionStatus
		terminal bool
	}{
		{ExecutionStatusPending, false},
		{ExecutionStatusRunning, false},
		{ExecutionStatusSucceeded, true},
		{ExecutionStatusFailed, true},
		{ExecutionStatusTimedOut, true},
		{ExecutionStatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			exec := &TaskExecution{Status: tt.status}
			if exec.IsTerminal() != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", exec.IsTerminal(), tt.terminal)
			}
		})
	}
}

func TestTaskConfig_MarshalConfig(t *testing.T) {
	cfg := TaskConfig{
		Timeout:       10 * time.Minute,
		MaxRetries:    3,
		RetryDelay:    1 * time.Minute,
		AllowOverlap:  true,
		ExecutionType: ExecutionTypeAgent,
		Channel:       "slack",
		ChannelID:     "channel-123",
		SessionID:     "session-456",
		SystemPrompt:  "You are a helpful assistant",
		Model:         "gpt-4",
	}

	data, err := cfg.MarshalConfig()
	if err != nil {
		t.Fatalf("MarshalConfig error: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check some values
	if parsed["max_retries"].(float64) != 3 {
		t.Errorf("max_retries = %v, want 3", parsed["max_retries"])
	}
	if parsed["allow_overlap"] != true {
		t.Errorf("allow_overlap = %v, want true", parsed["allow_overlap"])
	}
}

func TestUnmarshalConfig(t *testing.T) {
	t.Run("empty data returns empty config", func(t *testing.T) {
		cfg, err := UnmarshalConfig(nil)
		if err != nil {
			t.Fatalf("UnmarshalConfig error: %v", err)
		}
		if cfg.MaxRetries != 0 {
			t.Errorf("MaxRetries = %d, want 0", cfg.MaxRetries)
		}
	})

	t.Run("empty byte slice returns empty config", func(t *testing.T) {
		cfg, err := UnmarshalConfig([]byte{})
		if err != nil {
			t.Fatalf("UnmarshalConfig error: %v", err)
		}
		if cfg.MaxRetries != 0 {
			t.Errorf("MaxRetries = %d, want 0", cfg.MaxRetries)
		}
	})

	t.Run("valid JSON parses correctly", func(t *testing.T) {
		data := []byte(`{"max_retries": 5, "allow_overlap": true, "channel": "telegram"}`)
		cfg, err := UnmarshalConfig(data)
		if err != nil {
			t.Fatalf("UnmarshalConfig error: %v", err)
		}
		if cfg.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
		}
		if !cfg.AllowOverlap {
			t.Error("AllowOverlap should be true")
		}
		if cfg.Channel != "telegram" {
			t.Errorf("Channel = %q, want %q", cfg.Channel, "telegram")
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, err := UnmarshalConfig([]byte(`{invalid}`))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestDefaultTaskConfig(t *testing.T) {
	cfg := DefaultTaskConfig()

	if cfg.Timeout != 5*time.Minute {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 5*time.Minute)
	}
	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries = %d, want 0", cfg.MaxRetries)
	}
	if cfg.RetryDelay != 30*time.Second {
		t.Errorf("RetryDelay = %v, want %v", cfg.RetryDelay, 30*time.Second)
	}
	if cfg.AllowOverlap {
		t.Error("AllowOverlap should default to false")
	}
}

func TestScheduledTask_Struct(t *testing.T) {
	now := time.Now()
	lastRun := now.Add(-1 * time.Hour)

	task := ScheduledTask{
		ID:              "task-123",
		Name:            "Daily Report",
		Description:     "Generate daily report",
		AgentID:         "agent-456",
		Schedule:        "0 9 * * *",
		Timezone:        "America/New_York",
		Prompt:          "Generate the daily report",
		Config:          DefaultTaskConfig(),
		Status:          TaskStatusActive,
		NextRunAt:       now.Add(24 * time.Hour),
		LastRunAt:       &lastRun,
		LastExecutionID: "exec-789",
		CreatedAt:       now,
		UpdatedAt:       now,
		Metadata:        map[string]any{"priority": "high"},
	}

	if task.ID != "task-123" {
		t.Errorf("ID = %q, want %q", task.ID, "task-123")
	}
	if task.Name != "Daily Report" {
		t.Errorf("Name = %q, want %q", task.Name, "Daily Report")
	}
	if task.Status != TaskStatusActive {
		t.Errorf("Status = %v, want %v", task.Status, TaskStatusActive)
	}
}

func TestTaskExecution_Struct(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	finished := now

	exec := TaskExecution{
		ID:            "exec-123",
		TaskID:        "task-456",
		Status:        ExecutionStatusSucceeded,
		ScheduledAt:   now.Add(-6 * time.Minute),
		StartedAt:     &started,
		FinishedAt:    &finished,
		SessionID:     "session-789",
		Prompt:        "Run the task",
		Response:      "Task completed successfully",
		Error:         "",
		AttemptNumber: 1,
		WorkerID:      "worker-001",
		Duration:      5 * time.Minute,
		Metadata:      map[string]any{"retries": 0},
	}

	if exec.ID != "exec-123" {
		t.Errorf("ID = %q, want %q", exec.ID, "exec-123")
	}
	if exec.Status != ExecutionStatusSucceeded {
		t.Errorf("Status = %v, want %v", exec.Status, ExecutionStatusSucceeded)
	}
	if exec.AttemptNumber != 1 {
		t.Errorf("AttemptNumber = %d, want 1", exec.AttemptNumber)
	}
}

func TestScheduledTask_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second) // Truncate for JSON comparison
	lastRun := now.Add(-1 * time.Hour)

	original := ScheduledTask{
		ID:        "task-123",
		Name:      "Test Task",
		AgentID:   "agent-456",
		Schedule:  "*/5 * * * *",
		Prompt:    "Run test",
		Status:    TaskStatusActive,
		NextRunAt: now.Add(5 * time.Minute),
		LastRunAt: &lastRun,
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded ScheduledTask
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %v, want %v", decoded.Status, original.Status)
	}
}

func TestTaskExecution_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	started := now.Add(-5 * time.Minute)

	original := TaskExecution{
		ID:            "exec-123",
		TaskID:        "task-456",
		Status:        ExecutionStatusRunning,
		ScheduledAt:   now.Add(-6 * time.Minute),
		StartedAt:     &started,
		Prompt:        "Execute",
		AttemptNumber: 2,
		WorkerID:      "worker-001",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded TaskExecution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %v, want %v", decoded.Status, original.Status)
	}
	if decoded.AttemptNumber != original.AttemptNumber {
		t.Errorf("AttemptNumber = %d, want %d", decoded.AttemptNumber, original.AttemptNumber)
	}
}
