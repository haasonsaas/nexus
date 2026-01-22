// Package audit provides structured audit logging for agent actions, tool invocations,
// and permission decisions. Inspired by Clawdbot patterns for comprehensive event tracking.
package audit

import (
	"encoding/json"
	"time"
)

// EventType categorizes audit events.
type EventType string

const (
	// Tool events
	EventToolInvocation EventType = "tool.invocation"
	EventToolCompletion EventType = "tool.completion"
	EventToolDenied     EventType = "tool.denied"
	EventToolRetry      EventType = "tool.retry"

	// Agent events
	EventAgentAction   EventType = "agent.action"
	EventAgentHandoff  EventType = "agent.handoff"
	EventAgentError    EventType = "agent.error"
	EventAgentStartup  EventType = "agent.startup"
	EventAgentShutdown EventType = "agent.shutdown"

	// Permission events
	EventPermissionGranted EventType = "permission.granted"
	EventPermissionDenied  EventType = "permission.denied"
	EventPermissionRequest EventType = "permission.request"

	// Session events
	EventSessionCreate  EventType = "session.create"
	EventSessionUpdate  EventType = "session.update"
	EventSessionDelete  EventType = "session.delete"
	EventSessionCompact EventType = "session.compact"

	// Message events
	EventMessageReceived  EventType = "message.received"
	EventMessageProcessed EventType = "message.processed"
	EventMessageSent      EventType = "message.sent"

	// Gateway events
	EventGatewayStartup  EventType = "gateway.startup"
	EventGatewayShutdown EventType = "gateway.shutdown"
	EventGatewayError    EventType = "gateway.error"
)

// Level represents audit log severity.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event represents a single audit log entry.
type Event struct {
	// ID is a unique identifier for this audit event.
	ID string `json:"id"`

	// Type categorizes the event.
	Type EventType `json:"type"`

	// Level is the severity level.
	Level Level `json:"level"`

	// Timestamp when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// SessionID identifies the session context.
	SessionID string `json:"session_id,omitempty"`

	// SessionKey is the hierarchical session key (agent:agentId:mainKey).
	SessionKey string `json:"session_key,omitempty"`

	// AgentID identifies the agent involved.
	AgentID string `json:"agent_id,omitempty"`

	// ToolName identifies the tool for tool-related events.
	ToolName string `json:"tool_name,omitempty"`

	// ToolCallID links to a specific tool call.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Action describes what happened.
	Action string `json:"action"`

	// Details contains event-specific structured data.
	Details map[string]any `json:"details,omitempty"`

	// Duration is the time taken for timed operations.
	Duration time.Duration `json:"duration,omitempty"`

	// Error contains error information if applicable.
	Error string `json:"error,omitempty"`

	// UserID identifies the user if authenticated.
	UserID string `json:"user_id,omitempty"`

	// Channel identifies the messaging channel.
	Channel string `json:"channel,omitempty"`

	// TraceID for distributed tracing correlation.
	TraceID string `json:"trace_id,omitempty"`

	// SpanID for distributed tracing correlation.
	SpanID string `json:"span_id,omitempty"`

	// ParentEventID links to a parent audit event.
	ParentEventID string `json:"parent_event_id,omitempty"`
}

// ToolInvocationDetails contains details for tool invocation events.
type ToolInvocationDetails struct {
	ToolName    string          `json:"tool_name"`
	ToolCallID  string          `json:"tool_call_id"`
	Input       json.RawMessage `json:"input,omitempty"`
	InputHash   string          `json:"input_hash,omitempty"` // For privacy, hash sensitive inputs
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"max_attempts,omitempty"`
}

// ToolCompletionDetails contains details for tool completion events.
type ToolCompletionDetails struct {
	ToolName   string `json:"tool_name"`
	ToolCallID string `json:"tool_call_id"`
	Success    bool   `json:"success"`
	OutputSize int    `json:"output_size,omitempty"`
	Duration   int64  `json:"duration_ms"`
}

// PermissionDetails contains details for permission-related events.
type PermissionDetails struct {
	Permission    string   `json:"permission"`
	Resource      string   `json:"resource,omitempty"`
	Action        string   `json:"action"`
	GrantedBy     string   `json:"granted_by,omitempty"`
	DeniedReason  string   `json:"denied_reason,omitempty"`
	PolicyMatched string   `json:"policy_matched,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

// AgentHandoffDetails contains details for agent handoff events.
type AgentHandoffDetails struct {
	FromAgentID  string `json:"from_agent_id"`
	ToAgentID    string `json:"to_agent_id"`
	Reason       string `json:"reason"`
	ContextMode  string `json:"context_mode,omitempty"`
	HandoffDepth int    `json:"handoff_depth,omitempty"`
}

// SessionCompactDetails contains details for session compaction events.
type SessionCompactDetails struct {
	MessagesBeforeCompact int    `json:"messages_before_compact"`
	MessagesAfterCompact  int    `json:"messages_after_compact"`
	TokensSaved           int    `json:"tokens_saved,omitempty"`
	CompactionStrategy    string `json:"compaction_strategy,omitempty"`
}

// OutputFormat specifies the audit log output format.
type OutputFormat string

const (
	FormatJSON   OutputFormat = "json"
	FormatLogfmt OutputFormat = "logfmt"
	FormatText   OutputFormat = "text"
)

// Config configures the audit logger.
type Config struct {
	// Enabled determines if audit logging is active.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Level is the minimum level to log.
	Level Level `json:"level" yaml:"level"`

	// Format specifies the output format.
	Format OutputFormat `json:"format" yaml:"format"`

	// Output specifies where to write logs.
	// Supported: "stdout", "stderr", "file:/path/to/file.log"
	Output string `json:"output" yaml:"output"`

	// IncludeToolInput determines if tool inputs are logged.
	// Set to false for privacy-sensitive environments.
	IncludeToolInput bool `json:"include_tool_input" yaml:"include_tool_input"`

	// IncludeToolOutput determines if tool outputs are logged.
	IncludeToolOutput bool `json:"include_tool_output" yaml:"include_tool_output"`

	// IncludeMessageContent determines if message content is logged.
	IncludeMessageContent bool `json:"include_message_content" yaml:"include_message_content"`

	// MaxFieldSize limits the size of logged fields.
	MaxFieldSize int `json:"max_field_size" yaml:"max_field_size"`

	// EventTypes filters which event types to log (empty = all).
	EventTypes []EventType `json:"event_types" yaml:"event_types"`

	// SampleRate controls what fraction of events are logged (0.0 to 1.0).
	// 1.0 = all events, 0.1 = 10% of events.
	SampleRate float64 `json:"sample_rate" yaml:"sample_rate"`

	// BufferSize is the size of the async write buffer.
	BufferSize int `json:"buffer_size" yaml:"buffer_size"`

	// FlushInterval is how often to flush the buffer.
	FlushInterval time.Duration `json:"flush_interval" yaml:"flush_interval"`
}

// DefaultConfig returns a default audit configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:               false,
		Level:                 LevelInfo,
		Format:                FormatJSON,
		Output:                "stdout",
		IncludeToolInput:      false,
		IncludeToolOutput:     false,
		IncludeMessageContent: false,
		MaxFieldSize:          1024,
		SampleRate:            1.0,
		BufferSize:            1000,
		FlushInterval:         5 * time.Second,
	}
}
