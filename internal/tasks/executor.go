package tasks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// AgentExecutor executes scheduled tasks using the agent runtime.
type AgentExecutor struct {
	runtime  *agent.Runtime
	sessions sessions.Store
	logger   *slog.Logger
}

// AgentExecutorConfig configures the agent executor.
type AgentExecutorConfig struct {
	// Logger for executor events.
	Logger *slog.Logger
}

// NewAgentExecutor creates a new executor that uses the agent runtime.
func NewAgentExecutor(runtime *agent.Runtime, sessions sessions.Store, config AgentExecutorConfig) *AgentExecutor {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default().With("component", "task-executor")
	}

	return &AgentExecutor{
		runtime:  runtime,
		sessions: sessions,
		logger:   logger,
	}
}

// Execute runs a scheduled task using the agent runtime.
func (e *AgentExecutor) Execute(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is required")
	}
	if exec == nil {
		return "", fmt.Errorf("execution is required")
	}

	e.logger.Info("executing scheduled task",
		"task_id", task.ID,
		"task_name", task.Name,
		"execution_id", exec.ID,
		"agent_id", task.AgentID,
	)

	// Get or create session for this execution
	session, err := e.getOrCreateSession(ctx, task, exec)
	if err != nil {
		return "", fmt.Errorf("get or create session: %w", err)
	}

	// Update execution with session ID
	exec.SessionID = session.ID

	// Build the message to send to the agent
	msg := &models.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Channel:   session.Channel,
		ChannelID: session.ChannelID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   exec.Prompt,
		CreatedAt: time.Now(),
		Metadata: map[string]any{
			"scheduled_task_id":   task.ID,
			"scheduled_task_name": task.Name,
			"execution_id":        exec.ID,
			"attempt_number":      exec.AttemptNumber,
		},
	}

	// Apply task-specific configuration
	execCtx := ctx
	if task.Config.SystemPrompt != "" {
		execCtx = agent.WithSystemPrompt(execCtx, task.Config.SystemPrompt)
	}

	// Process the message through the agent runtime
	chunks, err := e.runtime.Process(execCtx, session, msg)
	if err != nil {
		return "", fmt.Errorf("process message: %w", err)
	}

	// Collect the response
	var response strings.Builder
	var lastError error

	for chunk := range chunks {
		if chunk == nil {
			continue
		}
		if chunk.Error != nil {
			lastError = chunk.Error
			e.logger.Error("chunk error during task execution",
				"task_id", task.ID,
				"execution_id", exec.ID,
				"error", chunk.Error,
			)
			continue
		}
		if chunk.Text != "" {
			response.WriteString(chunk.Text)
		}
	}

	if lastError != nil && response.Len() == 0 {
		return "", lastError
	}

	e.logger.Info("task execution completed",
		"task_id", task.ID,
		"execution_id", exec.ID,
		"response_length", response.Len(),
	)

	return response.String(), nil
}

// getOrCreateSession gets or creates a session for task execution.
func (e *AgentExecutor) getOrCreateSession(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (*models.Session, error) {
	// Use fixed session if configured
	if task.Config.SessionID != "" {
		session, err := e.sessions.Get(ctx, task.Config.SessionID)
		if err != nil {
			return nil, fmt.Errorf("get fixed session: %w", err)
		}
		if session != nil {
			return session, nil
		}
		// Session doesn't exist, fall through to create new one
	}

	// Determine channel context
	channel := models.ChannelType(task.Config.Channel)
	if channel == "" {
		channel = "scheduled_task"
	}

	channelID := task.Config.ChannelID
	if channelID == "" {
		channelID = task.ID
	}

	// Build session key
	key := sessions.SessionKey(task.AgentID, channel, channelID)

	// Get or create the session
	session, err := e.sessions.GetOrCreate(ctx, key, task.AgentID, channel, channelID)
	if err != nil {
		return nil, fmt.Errorf("get or create session: %w", err)
	}

	// Update session metadata with task info
	if session.Metadata == nil {
		session.Metadata = make(map[string]any)
	}
	session.Metadata["scheduled_task_id"] = task.ID
	session.Metadata["scheduled_task_name"] = task.Name
	session.Metadata["last_execution_id"] = exec.ID

	if err := e.sessions.Update(ctx, session); err != nil {
		e.logger.Warn("failed to update session metadata",
			"session_id", session.ID,
			"error", err,
		)
	}

	return session, nil
}

// NoOpExecutor is a no-operation executor for testing.
type NoOpExecutor struct {
	Response string
	Error    error
	Delay    time.Duration
}

// Execute returns a configured response after an optional delay.
func (e *NoOpExecutor) Execute(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
	if e.Delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(e.Delay):
		}
	}
	return e.Response, e.Error
}

// CallbackExecutor wraps a function as an Executor.
type CallbackExecutor struct {
	Fn func(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error)
}

// Execute calls the wrapped function.
func (e *CallbackExecutor) Execute(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
	if e.Fn == nil {
		return "", fmt.Errorf("callback function is nil")
	}
	return e.Fn(ctx, task, exec)
}

// RoutingExecutor routes task execution based on ExecutionType.
// This allows reminders to send direct messages while other tasks go through the agent.
type RoutingExecutor struct {
	agentExecutor   Executor
	messageExecutor Executor
	logger          *slog.Logger
}

// NewRoutingExecutor creates an executor that routes based on task configuration.
func NewRoutingExecutor(agentExecutor, messageExecutor Executor, logger *slog.Logger) *RoutingExecutor {
	if logger == nil {
		logger = slog.Default().With("component", "routing-executor")
	}
	return &RoutingExecutor{
		agentExecutor:   agentExecutor,
		messageExecutor: messageExecutor,
		logger:          logger,
	}
}

// Execute routes to the appropriate executor based on the task's ExecutionType.
func (e *RoutingExecutor) Execute(ctx context.Context, task *ScheduledTask, exec *TaskExecution) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is required")
	}
	taskLabel := fmt.Sprintf("task %q", task.ID)
	if name := strings.TrimSpace(task.Name); name != "" {
		taskLabel = fmt.Sprintf("%s (%s)", taskLabel, name)
	}

	switch task.Config.ExecutionType {
	case ExecutionTypeMessage:
		if e.messageExecutor == nil {
			return "", fmt.Errorf("%s message executor not configured", taskLabel)
		}
		e.logger.Info("routing task to message executor",
			"task_id", task.ID,
			"task_name", task.Name,
		)
		return e.messageExecutor.Execute(ctx, task, exec)

	case ExecutionTypeAgent, "":
		// Default to agent executor
		if e.agentExecutor == nil {
			return "", fmt.Errorf("%s agent executor not configured", taskLabel)
		}
		e.logger.Info("routing task to agent executor",
			"task_id", task.ID,
			"task_name", task.Name,
		)
		return e.agentExecutor.Execute(ctx, task, exec)

	default:
		return "", fmt.Errorf("%s unknown execution type: %s", taskLabel, task.Config.ExecutionType)
	}
}
