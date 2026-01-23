package tasks

import (
	"context"
	"errors"
	"testing"
)

func TestRoutingExecutor_RoutesByExecutionType(t *testing.T) {
	agentCalled := false
	messageCalled := false

	agentExec := &CallbackExecutor{
		Fn: func(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
			agentCalled = true
			return "agent response", nil
		},
	}

	messageExec := &CallbackExecutor{
		Fn: func(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
			messageCalled = true
			return "message response", nil
		},
	}

	router := NewRoutingExecutor(agentExec, messageExec, nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		execType     ExecutionType
		wantAgent    bool
		wantMessage  bool
		wantResponse string
	}{
		{
			name:         "default routes to agent",
			execType:     "",
			wantAgent:    true,
			wantMessage:  false,
			wantResponse: "agent response",
		},
		{
			name:         "agent type routes to agent",
			execType:     ExecutionTypeAgent,
			wantAgent:    true,
			wantMessage:  false,
			wantResponse: "agent response",
		},
		{
			name:         "message type routes to message",
			execType:     ExecutionTypeMessage,
			wantAgent:    false,
			wantMessage:  true,
			wantResponse: "message response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentCalled = false
			messageCalled = false

			task := &ScheduledTask{
				ID:   "test-task",
				Name: "Test Task",
				Config: TaskConfig{
					ExecutionType: tt.execType,
				},
			}
			exec := &TaskExecution{
				ID: "test-exec",
			}

			resp, err := router.Execute(ctx, task, exec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if agentCalled != tt.wantAgent {
				t.Errorf("agent called = %v, want %v", agentCalled, tt.wantAgent)
			}
			if messageCalled != tt.wantMessage {
				t.Errorf("message called = %v, want %v", messageCalled, tt.wantMessage)
			}
			if resp != tt.wantResponse {
				t.Errorf("response = %q, want %q", resp, tt.wantResponse)
			}
		})
	}
}

func TestRoutingExecutor_RequiresTask(t *testing.T) {
	router := NewRoutingExecutor(nil, nil, nil)

	_, err := router.Execute(context.Background(), nil, &TaskExecution{})
	if err == nil {
		t.Error("expected error for nil task")
	}
}

func TestRoutingExecutor_HandlesNilExecutors(t *testing.T) {
	router := NewRoutingExecutor(nil, nil, nil)
	ctx := context.Background()

	// Test agent route with nil agent executor
	task := &ScheduledTask{
		ID: "test",
		Config: TaskConfig{
			ExecutionType: ExecutionTypeAgent,
		},
	}
	exec := &TaskExecution{ID: "exec"}

	_, err := router.Execute(ctx, task, exec)
	if err == nil {
		t.Error("expected error for nil agent executor")
	}

	// Test message route with nil message executor
	task.Config.ExecutionType = ExecutionTypeMessage
	_, err = router.Execute(ctx, task, exec)
	if err == nil {
		t.Error("expected error for nil message executor")
	}
}

func TestRoutingExecutor_PropagatesErrors(t *testing.T) {
	expectedErr := errors.New("execution failed")

	agentExec := &CallbackExecutor{
		Fn: func(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
			return "", expectedErr
		},
	}

	router := NewRoutingExecutor(agentExec, nil, nil)
	task := &ScheduledTask{ID: "test"}
	exec := &TaskExecution{ID: "exec"}

	_, err := router.Execute(context.Background(), task, exec)
	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}
}
