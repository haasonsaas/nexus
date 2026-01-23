package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/tasks"
	"github.com/haasonsaas/nexus/pkg/models"
)

// mockAdapter implements both channels.Adapter and channels.OutboundAdapter for testing
type mockAdapter struct {
	channelType models.ChannelType
	sendFunc    func(ctx context.Context, msg *models.Message) error
	messages    []*models.Message
}

func (m *mockAdapter) Type() models.ChannelType {
	return m.channelType
}

func (m *mockAdapter) Send(ctx context.Context, msg *models.Message) error {
	m.messages = append(m.messages, msg)
	if m.sendFunc != nil {
		return m.sendFunc(ctx, msg)
	}
	return nil
}

func TestMessageExecutor_Execute(t *testing.T) {
	tests := []struct {
		name        string
		task        *tasks.ScheduledTask
		exec        *tasks.TaskExecution
		setupMock   func(*mockAdapter)
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil task returns error",
			task:        nil,
			exec:        &tasks.TaskExecution{ID: "exec-1"},
			wantErr:     true,
			errContains: "task is required",
		},
		{
			name:        "nil execution returns error",
			task:        &tasks.ScheduledTask{ID: "task-1"},
			exec:        nil,
			wantErr:     true,
			errContains: "execution is required",
		},
		{
			name: "missing channel returns error",
			task: &tasks.ScheduledTask{
				ID:     "task-1",
				Config: tasks.TaskConfig{ChannelID: "user-1"},
			},
			exec:        &tasks.TaskExecution{ID: "exec-1"},
			wantErr:     true,
			errContains: "channel is required",
		},
		{
			name: "missing channel_id returns error",
			task: &tasks.ScheduledTask{
				ID:     "task-1",
				Config: tasks.TaskConfig{Channel: "test"},
			},
			exec:        &tasks.TaskExecution{ID: "exec-1"},
			wantErr:     true,
			errContains: "channel_id (peer) is required",
		},
		{
			name: "successful message send",
			task: &tasks.ScheduledTask{
				ID:      "task-1",
				Name:    "Test Reminder",
				Prompt:  "Don't forget to stretch!",
				AgentID: "agent-1",
				Config: tasks.TaskConfig{
					Channel:   "test",
					ChannelID: "user-123",
				},
			},
			exec:    &tasks.TaskExecution{ID: "exec-1"},
			wantErr: false,
		},
		{
			name: "send failure returns error",
			task: &tasks.ScheduledTask{
				ID:     "task-1",
				Prompt: "Test message",
				Config: tasks.TaskConfig{
					Channel:   "test",
					ChannelID: "user-123",
				},
			},
			exec: &tasks.TaskExecution{ID: "exec-1"},
			setupMock: func(m *mockAdapter) {
				m.sendFunc = func(ctx context.Context, msg *models.Message) error {
					return errors.New("network error")
				}
			},
			wantErr:     true,
			errContains: "send message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock channel registry
			mock := &mockAdapter{channelType: "test"}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			registry := channels.NewRegistry()
			registry.Register(mock)

			executor := NewMessageExecutor(registry, MessageExecutorConfig{})

			result, err := executor.Execute(context.Background(), tt.task, tt.exec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestMessageExecutor_MessageContent(t *testing.T) {
	mock := &mockAdapter{channelType: "test"}
	registry := channels.NewRegistry()
	registry.Register(mock)

	executor := NewMessageExecutor(registry, MessageExecutorConfig{})

	task := &tasks.ScheduledTask{
		ID:     "task-1",
		Name:   "Stretch Reminder",
		Prompt: "Time to stretch!",
		Config: tasks.TaskConfig{
			Channel:   "test",
			ChannelID: "user-123",
		},
	}
	exec := &tasks.TaskExecution{ID: "exec-1"}

	_, err := executor.Execute(context.Background(), task, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mock.messages))
	}

	msg := mock.messages[0]
	if msg.Content != "Reminder: Time to stretch!" {
		t.Errorf("expected content %q, got %q", "Reminder: Time to stretch!", msg.Content)
	}
	if msg.Channel != "test" {
		t.Errorf("expected channel %q, got %q", "test", msg.Channel)
	}
	if msg.ChannelID != "user-123" {
		t.Errorf("expected channelID %q, got %q", "user-123", msg.ChannelID)
	}
	if msg.Direction != models.DirectionOutbound {
		t.Errorf("expected direction outbound, got %v", msg.Direction)
	}
	if msg.Role != models.RoleAssistant {
		t.Errorf("expected role assistant, got %v", msg.Role)
	}

	// Check metadata
	if msg.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if msg.Metadata["task_id"] != "task-1" {
		t.Errorf("expected task_id %q, got %v", "task-1", msg.Metadata["task_id"])
	}
	if msg.Metadata["type"] != "reminder" {
		t.Errorf("expected type %q, got %v", "reminder", msg.Metadata["type"])
	}
}

func TestFormatReminderMessage(t *testing.T) {
	task := &tasks.ScheduledTask{
		Prompt: "Check the oven",
	}
	exec := &tasks.TaskExecution{ID: "exec-1"}

	result := formatReminderMessage(task, exec)
	expected := "Reminder: Check the oven"

	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
