package models

import (
	"encoding/json"
	"time"
)

// ToolEventStage describes the lifecycle stage of a tool invocation for observability.
type ToolEventStage string

const (
	ToolEventRequested        ToolEventStage = "requested"
	ToolEventStarted          ToolEventStage = "started"
	ToolEventSucceeded        ToolEventStage = "succeeded"
	ToolEventFailed           ToolEventStage = "failed"
	ToolEventDenied           ToolEventStage = "denied"
	ToolEventRetrying         ToolEventStage = "retrying"
	ToolEventApprovalRequired ToolEventStage = "approval_required"
)

// ToolEvent represents a lifecycle event for a tool call including timing and results.
type ToolEvent struct {
	ToolCallID   string          `json:"tool_call_id"`
	ToolName     string          `json:"tool_name"`
	Stage        ToolEventStage  `json:"stage"`
	Attempt      int             `json:"attempt,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       string          `json:"output,omitempty"`
	Error        string          `json:"error,omitempty"`
	PolicyReason string          `json:"policy_reason,omitempty"`
	StartedAt    time.Time       `json:"started_at,omitempty"`
	FinishedAt   time.Time       `json:"finished_at,omitempty"`
}

// ToolSummary describes a tool available for use in the agent system.
type ToolSummary struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Source      string          `json:"source"`
	Namespace   string          `json:"namespace,omitempty"`
	Canonical   string          `json:"canonical,omitempty"`
}
