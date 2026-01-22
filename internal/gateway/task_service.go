// Package gateway provides the main Nexus gateway server.
//
// task_service.go implements the TaskService gRPC handlers for scheduled tasks.
package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/internal/tasks"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// cronParser supports both standard (5-field) and extended (6-field with seconds) cron expressions.
var cronParser = cron.NewParser(
	cron.SecondOptional |
		cron.Minute |
		cron.Hour |
		cron.Dom |
		cron.Month |
		cron.Dow |
		cron.Descriptor,
)

// taskService implements the proto.TaskServiceServer interface.
type taskService struct {
	proto.UnimplementedTaskServiceServer
	server *Server
}

// newTaskService creates a new task service handler.
func newTaskService(s *Server) *taskService {
	return &taskService{server: s}
}

// CreateTask creates a new scheduled task.
func (s *taskService) CreateTask(ctx context.Context, req *proto.CreateTaskRequest) (*proto.CreateTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	// Validate schedule
	_, err := cronParser.Parse(req.Schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule: %w", err)
	}

	now := time.Now()

	// Calculate next run time
	loc := time.UTC
	if req.Timezone != "" {
		loc, _ = time.LoadLocation(req.Timezone)
		if loc == nil {
			loc = time.UTC
		}
	}
	sched, _ := cronParser.Parse(req.Schedule)
	nextRun := sched.Next(now.In(loc))

	task := &tasks.ScheduledTask{
		ID:          uuid.NewString(),
		Name:        req.Name,
		Description: req.Description,
		AgentID:     req.AgentId,
		Schedule:    req.Schedule,
		Timezone:    req.Timezone,
		Prompt:      req.Prompt,
		Status:      tasks.TaskStatusActive,
		NextRunAt:   nextRun,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Apply config
	if req.Config != nil {
		task.Config = tasks.TaskConfig{
			Timeout:      time.Duration(req.Config.TimeoutSeconds) * time.Second,
			MaxRetries:   int(req.Config.MaxRetries),
			RetryDelay:   time.Duration(req.Config.RetryDelaySeconds) * time.Second,
			AllowOverlap: req.Config.AllowOverlap,
			Channel:      req.Config.Channel,
			ChannelID:    req.Config.ChannelId,
			SessionID:    req.Config.SessionId,
			SystemPrompt: req.Config.SystemPrompt,
			Model:        req.Config.Model,
		}
	}

	// Apply metadata
	if len(req.Metadata) > 0 {
		task.Metadata = make(map[string]any)
		for k, v := range req.Metadata {
			task.Metadata[k] = v
		}
	}

	if err := s.server.taskStore.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	return &proto.CreateTaskResponse{
		Task: taskToProto(task),
	}, nil
}

// GetTask retrieves a task by ID.
func (s *taskService) GetTask(ctx context.Context, req *proto.GetTaskRequest) (*proto.GetTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	task, err := s.server.taskStore.GetTask(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", req.Id)
	}

	return &proto.GetTaskResponse{
		Task: taskToProto(task),
	}, nil
}

// ListTasks lists tasks with optional filtering.
func (s *taskService) ListTasks(ctx context.Context, req *proto.ListTasksRequest) (*proto.ListTasksResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	opts := tasks.ListTasksOptions{
		AgentID: req.AgentId,
	}
	if req.Status != proto.TaskStatus_TASK_STATUS_UNSPECIFIED {
		var status tasks.TaskStatus
		switch req.Status {
		case proto.TaskStatus_TASK_STATUS_ACTIVE:
			status = tasks.TaskStatusActive
		case proto.TaskStatus_TASK_STATUS_PAUSED:
			status = tasks.TaskStatusPaused
		case proto.TaskStatus_TASK_STATUS_DISABLED:
			status = tasks.TaskStatusDisabled
		}
		opts.Status = &status
	}

	limit := int(req.PageSize)
	if limit <= 0 {
		limit = 50
	}
	opts.Limit = limit

	taskList, err := s.server.taskStore.ListTasks(ctx, opts)
	if err != nil {
		return nil, err
	}

	protoTasks := make([]*proto.ScheduledTask, 0, len(taskList))
	for _, t := range taskList {
		protoTasks = append(protoTasks, taskToProto(t))
	}

	return &proto.ListTasksResponse{
		Tasks:      protoTasks,
		TotalCount: int32(len(taskList)),
	}, nil
}

// UpdateTask updates an existing task.
func (s *taskService) UpdateTask(ctx context.Context, req *proto.UpdateTaskRequest) (*proto.UpdateTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	task, err := s.server.taskStore.GetTask(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", req.Id)
	}

	// Update fields
	if req.Name != "" {
		task.Name = req.Name
	}
	if req.Description != "" {
		task.Description = req.Description
	}
	if req.Schedule != "" {
		if _, err := cronParser.Parse(req.Schedule); err != nil {
			return nil, fmt.Errorf("invalid cron schedule: %w", err)
		}
		task.Schedule = req.Schedule

		// Recalculate next run
		loc := time.UTC
		tz := req.Timezone
		if tz == "" {
			tz = task.Timezone
		}
		if tz != "" {
			loc, _ = time.LoadLocation(tz)
			if loc == nil {
				loc = time.UTC
			}
		}
		sched, _ := cronParser.Parse(req.Schedule)
		task.NextRunAt = sched.Next(time.Now().In(loc))
	}
	if req.Timezone != "" {
		task.Timezone = req.Timezone
	}
	if req.Prompt != "" {
		task.Prompt = req.Prompt
	}
	if req.Config != nil {
		task.Config = tasks.TaskConfig{
			Timeout:      time.Duration(req.Config.TimeoutSeconds) * time.Second,
			MaxRetries:   int(req.Config.MaxRetries),
			RetryDelay:   time.Duration(req.Config.RetryDelaySeconds) * time.Second,
			AllowOverlap: req.Config.AllowOverlap,
			Channel:      req.Config.Channel,
			ChannelID:    req.Config.ChannelId,
			SessionID:    req.Config.SessionId,
			SystemPrompt: req.Config.SystemPrompt,
			Model:        req.Config.Model,
		}
	}
	if len(req.Metadata) > 0 {
		task.Metadata = make(map[string]any)
		for k, v := range req.Metadata {
			task.Metadata[k] = v
		}
	}

	task.UpdatedAt = time.Now()

	if err := s.server.taskStore.UpdateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}

	return &proto.UpdateTaskResponse{
		Task: taskToProto(task),
	}, nil
}

// DeleteTask deletes a task.
func (s *taskService) DeleteTask(ctx context.Context, req *proto.DeleteTaskRequest) (*proto.DeleteTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	if err := s.server.taskStore.DeleteTask(ctx, req.Id); err != nil {
		return nil, err
	}

	return &proto.DeleteTaskResponse{Success: true}, nil
}

// PauseTask pauses a task's schedule.
func (s *taskService) PauseTask(ctx context.Context, req *proto.PauseTaskRequest) (*proto.PauseTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	task, err := s.server.taskStore.GetTask(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", req.Id)
	}

	task.Status = tasks.TaskStatusPaused
	task.UpdatedAt = time.Now()

	if err := s.server.taskStore.UpdateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("pause task: %w", err)
	}

	return &proto.PauseTaskResponse{
		Task: taskToProto(task),
	}, nil
}

// ResumeTask resumes a paused task.
func (s *taskService) ResumeTask(ctx context.Context, req *proto.ResumeTaskRequest) (*proto.ResumeTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	task, err := s.server.taskStore.GetTask(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", req.Id)
	}

	task.Status = tasks.TaskStatusActive
	task.UpdatedAt = time.Now()

	// Recalculate next run time
	loc := time.UTC
	if task.Timezone != "" {
		loc, _ = time.LoadLocation(task.Timezone)
		if loc == nil {
			loc = time.UTC
		}
	}
	sched, _ := cronParser.Parse(task.Schedule)
	task.NextRunAt = sched.Next(time.Now().In(loc))

	if err := s.server.taskStore.UpdateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("resume task: %w", err)
	}

	return &proto.ResumeTaskResponse{
		Task: taskToProto(task),
	}, nil
}

// TriggerTask manually triggers a task to run immediately.
func (s *taskService) TriggerTask(ctx context.Context, req *proto.TriggerTaskRequest) (*proto.TriggerTaskResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	task, err := s.server.taskStore.GetTask(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", req.Id)
	}

	// Create execution for immediate run
	now := time.Now()
	exec := &tasks.TaskExecution{
		ID:            uuid.NewString(),
		TaskID:        task.ID,
		Status:        tasks.ExecutionStatusPending,
		ScheduledAt:   now,
		Prompt:        task.Prompt,
		AttemptNumber: 1,
	}

	if err := s.server.taskStore.CreateExecution(ctx, exec); err != nil {
		return nil, fmt.Errorf("create execution: %w", err)
	}

	return &proto.TriggerTaskResponse{
		Execution: executionToProto(exec),
	}, nil
}

// ListExecutions lists executions for a task.
func (s *taskService) ListExecutions(ctx context.Context, req *proto.ListExecutionsRequest) (*proto.ListExecutionsResponse, error) {
	if s.server.taskStore == nil {
		return nil, fmt.Errorf("task scheduler not enabled")
	}

	opts := tasks.ListExecutionsOptions{}
	if req.Status != proto.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED {
		var status tasks.ExecutionStatus
		switch req.Status {
		case proto.ExecutionStatus_EXECUTION_STATUS_PENDING:
			status = tasks.ExecutionStatusPending
		case proto.ExecutionStatus_EXECUTION_STATUS_RUNNING:
			status = tasks.ExecutionStatusRunning
		case proto.ExecutionStatus_EXECUTION_STATUS_SUCCEEDED:
			status = tasks.ExecutionStatusSucceeded
		case proto.ExecutionStatus_EXECUTION_STATUS_FAILED:
			status = tasks.ExecutionStatusFailed
		case proto.ExecutionStatus_EXECUTION_STATUS_TIMED_OUT:
			status = tasks.ExecutionStatusTimedOut
		case proto.ExecutionStatus_EXECUTION_STATUS_CANCELLED:
			status = tasks.ExecutionStatusCancelled
		}
		opts.Status = &status
	}

	limit := int(req.PageSize)
	if limit <= 0 {
		limit = 50
	}
	opts.Limit = limit

	executions, err := s.server.taskStore.ListExecutions(ctx, req.TaskId, opts)
	if err != nil {
		return nil, err
	}

	protoExecs := make([]*proto.TaskExecution, 0, len(executions))
	for _, e := range executions {
		protoExecs = append(protoExecs, executionToProto(e))
	}

	return &proto.ListExecutionsResponse{
		Executions: protoExecs,
		TotalCount: int32(len(executions)),
	}, nil
}

// taskToProto converts a tasks.ScheduledTask to a proto.ScheduledTask.
func taskToProto(t *tasks.ScheduledTask) *proto.ScheduledTask {
	pt := &proto.ScheduledTask{
		Id:              t.ID,
		Name:            t.Name,
		Description:     t.Description,
		AgentId:         t.AgentID,
		Schedule:        t.Schedule,
		Timezone:        t.Timezone,
		Prompt:          t.Prompt,
		LastExecutionId: t.LastExecutionID,
		CreatedAt:       timestamppb.New(t.CreatedAt),
		UpdatedAt:       timestamppb.New(t.UpdatedAt),
	}

	if !t.NextRunAt.IsZero() {
		pt.NextRunAt = timestamppb.New(t.NextRunAt)
	}
	if t.LastRunAt != nil && !t.LastRunAt.IsZero() {
		pt.LastRunAt = timestamppb.New(*t.LastRunAt)
	}

	switch t.Status {
	case tasks.TaskStatusActive:
		pt.Status = proto.TaskStatus_TASK_STATUS_ACTIVE
	case tasks.TaskStatusPaused:
		pt.Status = proto.TaskStatus_TASK_STATUS_PAUSED
	case tasks.TaskStatusDisabled:
		pt.Status = proto.TaskStatus_TASK_STATUS_DISABLED
	}

	pt.Config = &proto.TaskConfig{
		TimeoutSeconds:    int32(t.Config.Timeout.Seconds()),
		MaxRetries:        int32(t.Config.MaxRetries),
		RetryDelaySeconds: int32(t.Config.RetryDelay.Seconds()),
		AllowOverlap:      t.Config.AllowOverlap,
		Channel:           t.Config.Channel,
		ChannelId:         t.Config.ChannelID,
		SessionId:         t.Config.SessionID,
		SystemPrompt:      t.Config.SystemPrompt,
		Model:             t.Config.Model,
	}

	if len(t.Metadata) > 0 {
		pt.Metadata = make(map[string]string)
		for k, v := range t.Metadata {
			if s, ok := v.(string); ok {
				pt.Metadata[k] = s
			}
		}
	}

	return pt
}

// executionToProto converts a tasks.TaskExecution to a proto.TaskExecution.
func executionToProto(e *tasks.TaskExecution) *proto.TaskExecution {
	pe := &proto.TaskExecution{
		Id:            e.ID,
		TaskId:        e.TaskID,
		SessionId:     e.SessionID,
		Prompt:        e.Prompt,
		Response:      e.Response,
		Error:         e.Error,
		AttemptNumber: int32(e.AttemptNumber),
		DurationMs:    e.Duration.Milliseconds(),
		ScheduledAt:   timestamppb.New(e.ScheduledAt),
	}

	if e.StartedAt != nil {
		pe.StartedAt = timestamppb.New(*e.StartedAt)
	}
	if e.FinishedAt != nil {
		pe.FinishedAt = timestamppb.New(*e.FinishedAt)
	}

	switch e.Status {
	case tasks.ExecutionStatusPending:
		pe.Status = proto.ExecutionStatus_EXECUTION_STATUS_PENDING
	case tasks.ExecutionStatusRunning:
		pe.Status = proto.ExecutionStatus_EXECUTION_STATUS_RUNNING
	case tasks.ExecutionStatusSucceeded:
		pe.Status = proto.ExecutionStatus_EXECUTION_STATUS_SUCCEEDED
	case tasks.ExecutionStatusFailed:
		pe.Status = proto.ExecutionStatus_EXECUTION_STATUS_FAILED
	case tasks.ExecutionStatusTimedOut:
		pe.Status = proto.ExecutionStatus_EXECUTION_STATUS_TIMED_OUT
	case tasks.ExecutionStatusCancelled:
		pe.Status = proto.ExecutionStatus_EXECUTION_STATUS_CANCELLED
	}

	return pe
}
