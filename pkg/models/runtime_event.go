package models

// RuntimeEventType defines the types of runtime events.
type RuntimeEventType string

const (
	// EventThinkingStart indicates the LLM is processing.
	EventThinkingStart RuntimeEventType = "thinking_start"

	// EventThinkingEnd indicates the LLM has finished processing.
	EventThinkingEnd RuntimeEventType = "thinking_end"

	// EventToolQueued indicates a tool call is queued for execution.
	EventToolQueued RuntimeEventType = "tool_queued"

	// EventToolStarted indicates a tool has started executing.
	EventToolStarted RuntimeEventType = "tool_started"

	// EventToolCompleted indicates a tool has completed successfully.
	EventToolCompleted RuntimeEventType = "tool_completed"

	// EventToolFailed indicates a tool has failed.
	EventToolFailed RuntimeEventType = "tool_failed"

	// EventToolTimeout indicates a tool execution timed out.
	EventToolTimeout RuntimeEventType = "tool_timeout"

	// EventSummarizing indicates conversation summarization is in progress.
	EventSummarizing RuntimeEventType = "summarizing"

	// EventIterationStart indicates a new agentic loop iteration.
	EventIterationStart RuntimeEventType = "iteration_start"

	// EventIterationEnd indicates an agentic loop iteration has ended.
	EventIterationEnd RuntimeEventType = "iteration_end"
)

// RuntimeEvent represents a lifecycle event during agent processing.
// These events provide observability into the agent's execution flow.
type RuntimeEvent struct {
	// Type identifies the kind of event.
	Type RuntimeEventType `json:"type"`

	// Message is a human-readable description of the event.
	Message string `json:"message,omitempty"`

	// ToolName is the name of the tool (for tool events).
	ToolName string `json:"tool_name,omitempty"`

	// ToolCallID is the ID of the tool call (for tool events).
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Iteration is the current agentic loop iteration (0-indexed).
	Iteration int `json:"iteration,omitempty"`

	// Meta contains additional event-specific metadata.
	Meta map[string]any `json:"meta,omitempty"`
}

// NewToolEvent creates a new tool lifecycle event.
func NewToolEvent(eventType RuntimeEventType, toolName, toolCallID string) *RuntimeEvent {
	return &RuntimeEvent{
		Type:       eventType,
		ToolName:   toolName,
		ToolCallID: toolCallID,
	}
}

// WithMessage adds a message to the event.
func (e *RuntimeEvent) WithMessage(msg string) *RuntimeEvent {
	e.Message = msg
	return e
}

// WithIteration adds the iteration number to the event.
func (e *RuntimeEvent) WithIteration(iter int) *RuntimeEvent {
	e.Iteration = iter
	return e
}

// WithMeta adds metadata to the event.
func (e *RuntimeEvent) WithMeta(key string, value any) *RuntimeEvent {
	if e.Meta == nil {
		e.Meta = make(map[string]any)
	}
	e.Meta[key] = value
	return e
}
