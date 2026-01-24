package tasks

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestRoutingExecutor_UnknownExecutionType(t *testing.T) {
	router := NewRoutingExecutor(&CallbackExecutor{
		Fn: func(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
			return "ok", nil
		},
	}, nil, nil)

	task := &ScheduledTask{
		ID: "test",
		Config: TaskConfig{
			ExecutionType: ExecutionType("unknown_type"),
		},
	}
	exec := &TaskExecution{ID: "exec"}

	_, err := router.Execute(context.Background(), task, exec)
	if err == nil {
		t.Error("expected error for unknown execution type")
	}
	if !errors.Is(err, nil) && err.Error() != "unknown execution type: unknown_type" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNoOpExecutor(t *testing.T) {
	ctx := context.Background()

	t.Run("returns configured response", func(t *testing.T) {
		exec := &NoOpExecutor{
			Response: "test response",
		}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		resp, err := exec.Execute(ctx, task, execution)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "test response" {
			t.Errorf("response = %q, want %q", resp, "test response")
		}
	})

	t.Run("returns configured error", func(t *testing.T) {
		expectedErr := errors.New("configured error")
		exec := &NoOpExecutor{
			Error: expectedErr,
		}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		_, err := exec.Execute(ctx, task, execution)
		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("respects context cancellation during delay", func(t *testing.T) {
		exec := &NoOpExecutor{
			Response: "test",
			Delay:    1 * time.Second,
		}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		ctx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		_, err := exec.Execute(ctx, task, execution)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})

	t.Run("completes after delay", func(t *testing.T) {
		exec := &NoOpExecutor{
			Response: "delayed response",
			Delay:    10 * time.Millisecond,
		}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		start := time.Now()
		resp, err := exec.Execute(ctx, task, execution)
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "delayed response" {
			t.Errorf("response = %q, want %q", resp, "delayed response")
		}
		if duration < 10*time.Millisecond {
			t.Errorf("expected at least 10ms delay, got %v", duration)
		}
	})
}

func TestCallbackExecutor(t *testing.T) {
	ctx := context.Background()

	t.Run("calls provided function", func(t *testing.T) {
		called := false
		exec := &CallbackExecutor{
			Fn: func(ctx context.Context, task *ScheduledTask, e *TaskExecution) (string, error) {
				called = true
				return "callback response", nil
			},
		}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		resp, err := exec.Execute(ctx, task, execution)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("callback function was not called")
		}
		if resp != "callback response" {
			t.Errorf("response = %q, want %q", resp, "callback response")
		}
	})

	t.Run("returns error for nil function", func(t *testing.T) {
		exec := &CallbackExecutor{Fn: nil}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		_, err := exec.Execute(ctx, task, execution)
		if err == nil {
			t.Error("expected error for nil function")
		}
	})

	t.Run("propagates errors from callback", func(t *testing.T) {
		expectedErr := errors.New("callback error")
		exec := &CallbackExecutor{
			Fn: func(ctx context.Context, task *ScheduledTask, e *TaskExecution) (string, error) {
				return "", expectedErr
			},
		}
		task := &ScheduledTask{ID: "test"}
		execution := &TaskExecution{ID: "exec"}

		_, err := exec.Execute(ctx, task, execution)
		if !errors.Is(err, expectedErr) {
			t.Errorf("error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("receives correct arguments", func(t *testing.T) {
		var receivedTask *ScheduledTask
		var receivedExec *TaskExecution

		exec := &CallbackExecutor{
			Fn: func(ctx context.Context, task *ScheduledTask, e *TaskExecution) (string, error) {
				receivedTask = task
				receivedExec = e
				return "", nil
			},
		}
		task := &ScheduledTask{ID: "task-123", Name: "Test Task"}
		execution := &TaskExecution{ID: "exec-456", TaskID: "task-123"}

		_, err := exec.Execute(ctx, task, execution)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedTask.ID != "task-123" {
			t.Errorf("task ID = %q, want %q", receivedTask.ID, "task-123")
		}
		if receivedExec.ID != "exec-456" {
			t.Errorf("execution ID = %q, want %q", receivedExec.ID, "exec-456")
		}
	})
}

func TestAgentExecutorConfig_Struct(t *testing.T) {
	cfg := AgentExecutorConfig{
		Logger: nil,
	}
	if cfg.Logger != nil {
		t.Error("Logger should be nil")
	}
}

func TestNewAgentExecutor(t *testing.T) {
	t.Run("creates executor with nil logger", func(t *testing.T) {
		exec := NewAgentExecutor(nil, nil, AgentExecutorConfig{})
		if exec == nil {
			t.Fatal("expected non-nil executor")
		}
		if exec.logger == nil {
			t.Error("logger should default to slog.Default()")
		}
	})

	t.Run("stores runtime and sessions", func(t *testing.T) {
		exec := NewAgentExecutor(nil, nil, AgentExecutorConfig{})
		if exec.runtime != nil {
			t.Error("runtime should be nil when not provided")
		}
		if exec.sessions != nil {
			t.Error("sessions should be nil when not provided")
		}
	})
}

func TestAgentExecutor_Execute_NilTask(t *testing.T) {
	exec := NewAgentExecutor(nil, nil, AgentExecutorConfig{})
	ctx := context.Background()

	_, err := exec.Execute(ctx, nil, &TaskExecution{})
	if err == nil {
		t.Error("expected error for nil task")
	}
	if err.Error() != "task is required" {
		t.Errorf("error = %q, want %q", err.Error(), "task is required")
	}
}

func TestAgentExecutor_Execute_NilExecution(t *testing.T) {
	exec := NewAgentExecutor(nil, nil, AgentExecutorConfig{})
	ctx := context.Background()

	_, err := exec.Execute(ctx, &ScheduledTask{}, nil)
	if err == nil {
		t.Error("expected error for nil execution")
	}
	if err.Error() != "execution is required" {
		t.Errorf("error = %q, want %q", err.Error(), "execution is required")
	}
}

func TestNoOpExecutor_Struct(t *testing.T) {
	exec := &NoOpExecutor{
		Response: "test",
		Error:    errors.New("test error"),
		Delay:    100 * time.Millisecond,
	}

	if exec.Response != "test" {
		t.Errorf("Response = %q, want %q", exec.Response, "test")
	}
	if exec.Delay != 100*time.Millisecond {
		t.Errorf("Delay = %v, want %v", exec.Delay, 100*time.Millisecond)
	}
}

func TestCallbackExecutor_Struct(t *testing.T) {
	called := false
	exec := &CallbackExecutor{
		Fn: func(ctx context.Context, task *ScheduledTask, e *TaskExecution) (string, error) {
			called = true
			return "ok", nil
		},
	}

	if exec.Fn == nil {
		t.Error("Fn should not be nil")
	}

	// Call to verify struct works
	_, _ = exec.Fn(context.Background(), nil, nil)
	if !called {
		t.Error("Fn should have been called")
	}
}

func TestRoutingExecutor_Struct(t *testing.T) {
	router := NewRoutingExecutor(nil, nil, nil)
	if router == nil {
		t.Fatal("expected non-nil router")
	}
	if router.logger == nil {
		t.Error("logger should default to slog.Default()")
	}
}

func TestNewRoutingExecutor_WithLogger(t *testing.T) {
	agentExec := &NoOpExecutor{Response: "agent"}
	msgExec := &NoOpExecutor{Response: "message"}

	router := NewRoutingExecutor(agentExec, msgExec, nil)
	if router == nil {
		t.Fatal("expected non-nil router")
	}
	if router.agentExecutor == nil {
		t.Error("agentExecutor should be set")
	}
	if router.messageExecutor == nil {
		t.Error("messageExecutor should be set")
	}
}
