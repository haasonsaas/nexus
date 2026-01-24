// Package models provides domain types for the Nexus agent system.
package models

import (
	"time"
)

// AgentEvent is the unified event model for streaming and hooks.
// It provides a single event stream that drives UI, logging, and plugins.
//
// Design principles:
//   - Versioned and forward-compatible (add fields, don't rename/remove)
//   - Single Type discriminator with optional payload pointers
//   - Monotonic Sequence for ordering guarantees across goroutines
type AgentEvent struct {
	// Version for forward compatibility. Current version: 1.
	Version int `json:"version"`

	// Type identifies the kind of event.
	Type AgentEventType `json:"type"`

	// Time is when the event occurred.
	Time time.Time `json:"time"`

	// Sequence is monotonic within a run for ordering guarantees.
	Sequence uint64 `json:"seq"`

	// RunID identifies the agent run (Process call).
	RunID string `json:"run_id,omitempty"`

	// TurnIndex is the 0-based turn number within the run.
	TurnIndex int `json:"turn_index,omitempty"`

	// IterIndex is the 0-based iteration (agentic loop iteration).
	IterIndex int `json:"iter_index,omitempty"`

	// Exactly one payload should be non-nil for a given Type.
	Text     *TextEventPayload     `json:"text,omitempty"`
	Tool     *ToolEventPayload     `json:"tool,omitempty"`
	Stream   *StreamEventPayload   `json:"stream,omitempty"`
	Error    *ErrorEventPayload    `json:"error,omitempty"`
	Stats    *StatsEventPayload    `json:"stats,omitempty"`
	Context  *ContextEventPayload  `json:"context,omitempty"`
	Steering *SteeringEventPayload `json:"steering,omitempty"`
}

// AgentEventType identifies the kind of agent event.
type AgentEventType string

const (
	// Run lifecycle
	AgentEventRunStarted   AgentEventType = "run.started"
	AgentEventRunFinished  AgentEventType = "run.finished"
	AgentEventRunError     AgentEventType = "run.error"
	AgentEventRunCancelled AgentEventType = "run.cancelled" // Explicit context cancellation
	AgentEventRunTimedOut  AgentEventType = "run.timed_out" // Run wall time exceeded

	// Turn/iteration lifecycle
	AgentEventTurnStarted  AgentEventType = "turn.started"
	AgentEventTurnFinished AgentEventType = "turn.finished"
	AgentEventIterStarted  AgentEventType = "iter.started"
	AgentEventIterFinished AgentEventType = "iter.finished"

	// Model streaming
	AgentEventModelDelta     AgentEventType = "model.delta"
	AgentEventModelCompleted AgentEventType = "model.completed"

	// Tool execution and streaming IO
	AgentEventToolStarted  AgentEventType = "tool.started"
	AgentEventToolStdout   AgentEventType = "tool.stdout"
	AgentEventToolStderr   AgentEventType = "tool.stderr"
	AgentEventToolFinished AgentEventType = "tool.finished"
	AgentEventToolTimedOut AgentEventType = "tool.timed_out" // Per-tool timeout exceeded

	// Context packing diagnostics
	AgentEventContextPacked AgentEventType = "context.packed"

	// Steering events
	AgentEventSteeringInjected AgentEventType = "steering.injected" // Steering message interrupted the run
	AgentEventToolsSkipped     AgentEventType = "tools.skipped"     // Tools were skipped due to steering
	AgentEventFollowUpQueued   AgentEventType = "followup.queued"   // Follow-up message queued for later
)

// TextEventPayload is generic human-readable text (logs, status messages).
type TextEventPayload struct {
	Text string `json:"text"`
}

// StreamEventPayload represents model streaming deltas and completion metadata.
type StreamEventPayload struct {
	// Delta is the incremental text (token-by-token or chunked).
	Delta string `json:"delta,omitempty"`

	// Final is optional final text on completion events.
	Final string `json:"final,omitempty"`

	// Provider/Model for debugging (optional).
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`

	// Token counts (optional; not all providers supply them).
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// ToolEventPayload describes tool calls and their streamed outputs.
// Args/Result are opaque []byte to avoid coupling to tool schemas.
type ToolEventPayload struct {
	// CallID identifies this specific tool invocation.
	CallID string `json:"call_id,omitempty"`

	// Name is the tool name.
	Name string `json:"name,omitempty"`

	// ArgsJSON is the raw JSON arguments (for started events).
	ArgsJSON []byte `json:"args_json,omitempty"`

	// Chunk is stdout/stderr content (for stdout/stderr events).
	Chunk string `json:"chunk,omitempty"`

	// For finished events:
	Success    bool          `json:"success,omitempty"`
	ResultJSON []byte        `json:"result_json,omitempty"`
	Elapsed    time.Duration `json:"elapsed,omitempty"`
}

// ErrorEventPayload standardizes errors for streaming and plugins.
type ErrorEventPayload struct {
	// Message is the error description (required).
	Message string `json:"message"`

	// Code is an optional error code for programmatic handling.
	Code string `json:"code,omitempty"`

	// Retriable indicates if the operation can be retried.
	Retriable bool `json:"retriable,omitempty"`

	// Err is the original error (runtime only, not serialized).
	// Used to preserve error types for errors.Is/errors.As.
	Err error `json:"-"`
}

// StatsEventPayload carries run statistics as an event.
type StatsEventPayload struct {
	Run *RunStats `json:"run,omitempty"`
}

// RunStats is an aggregated summary of an agent run.
// Derived from the event stream for observability.
type RunStats struct {
	// RunID identifies this run.
	RunID string `json:"run_id,omitempty"`

	// Timing
	StartedAt  time.Time     `json:"started_at,omitempty"`
	FinishedAt time.Time     `json:"finished_at,omitempty"`
	WallTime   time.Duration `json:"wall_time,omitempty"`

	// Counts
	Turns int `json:"turns,omitempty"`
	Iters int `json:"iters,omitempty"`

	// Tool metrics
	ToolCalls    int           `json:"tool_calls,omitempty"`
	ToolWallTime time.Duration `json:"tool_wall_time,omitempty"`
	ToolTimeouts int           `json:"tool_timeouts,omitempty"`

	// Model metrics
	ModelWallTime time.Duration `json:"model_wall_time,omitempty"`
	InputTokens   int           `json:"input_tokens,omitempty"`
	OutputTokens  int           `json:"output_tokens,omitempty"`

	// Context packing metrics
	ContextPacks int `json:"context_packs,omitempty"`
	DroppedItems int `json:"dropped_items,omitempty"`

	// Reliability signals
	Cancelled     bool `json:"cancelled,omitempty"`      // Run was explicitly cancelled
	TimedOut      bool `json:"timed_out,omitempty"`      // Run hit wall time limit
	DroppedEvents int  `json:"dropped_events,omitempty"` // Events dropped due to backpressure

	// Error count
	Errors int `json:"errors,omitempty"`
}

// SteeringEventPayload describes steering and follow-up message events.
type SteeringEventPayload struct {
	// Content is the text content of the steering/follow-up message.
	Content string `json:"content,omitempty"`

	// Count is the number of messages (for multi-message events).
	Count int `json:"count,omitempty"`

	// SkippedTools lists tool call IDs that were skipped due to steering.
	SkippedTools []string `json:"skipped_tools,omitempty"`

	// Priority indicates steering message priority (higher = first).
	Priority int `json:"priority,omitempty"`
}

// ContextEventPayload contains context packing diagnostics.
// It explains why certain messages were included or dropped during packing.
type ContextEventPayload struct {
	// Budget configuration
	BudgetChars    int `json:"budget_chars"`    // Max character budget
	BudgetMessages int `json:"budget_messages"` // Max message count
	UsedChars      int `json:"used_chars"`      // Characters used
	UsedMessages   int `json:"used_messages"`   // Messages included

	// Message counts by category
	Candidates int `json:"candidates"` // Total messages before packing
	Included   int `json:"included"`   // Messages included
	Dropped    int `json:"dropped"`    // Messages dropped

	// Summary info
	SummaryUsed  bool `json:"summary_used,omitempty"`  // Whether summary was included
	SummaryChars int  `json:"summary_chars,omitempty"` // Characters in summary

	// Per-item diagnostics (optional, only when verbose)
	Items []ContextPackItem `json:"items,omitempty"`
}

// ContextPackItem describes a single item in the context packing decision.
type ContextPackItem struct {
	// ID is a hash or identifier for the message (not the content itself).
	ID string `json:"id,omitempty"`

	// Kind categorizes the message type.
	Kind ContextItemKind `json:"kind"`

	// Chars is the character count.
	Chars int `json:"chars"`

	// Included indicates whether this item was included.
	Included bool `json:"included"`

	// Reason explains why the item was included or dropped.
	Reason ContextPackReason `json:"reason,omitempty"`
}

// ContextItemKind categorizes context items.
type ContextItemKind string

const (
	ContextItemSystem   ContextItemKind = "system"
	ContextItemHistory  ContextItemKind = "history"
	ContextItemTool     ContextItemKind = "tool"
	ContextItemSummary  ContextItemKind = "summary"
	ContextItemIncoming ContextItemKind = "incoming"
)

// ContextPackReason explains a packing decision.
type ContextPackReason string

const (
	// Inclusion reasons
	ContextReasonIncluded ContextPackReason = "included"
	ContextReasonReserved ContextPackReason = "reserved" // incoming/summary

	// Exclusion reasons
	ContextReasonOverBudget ContextPackReason = "over_budget"
	ContextReasonTooOld     ContextPackReason = "too_old"
	ContextReasonFiltered   ContextPackReason = "filtered"
)
