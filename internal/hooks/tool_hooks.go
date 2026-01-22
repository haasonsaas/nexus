package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Tool execution hook event types following Clawdbot patterns.
const (
	// EventToolPreExecution fires before a tool is executed.
	// Handlers can modify input or cancel execution.
	EventToolPreExecution EventType = "tool.pre_execution"

	// EventToolPostExecution fires after a tool completes.
	// Handlers can modify output or perform cleanup.
	EventToolPostExecution EventType = "tool.post_execution"

	// EventToolApprovalRequired fires when a tool needs approval.
	EventToolApprovalRequired EventType = "tool.approval_required"

	// EventToolApprovalGranted fires when approval is granted.
	EventToolApprovalGranted EventType = "tool.approval_granted"

	// EventToolApprovalDenied fires when approval is denied.
	EventToolApprovalDenied EventType = "tool.approval_denied"

	// EventToolApprovalTimeout fires when approval times out.
	EventToolApprovalTimeout EventType = "tool.approval_timeout"

	// EventToolRetry fires when a tool execution is retried.
	EventToolRetry EventType = "tool.retry"

	// EventToolRateLimited fires when a tool is rate limited.
	EventToolRateLimited EventType = "tool.rate_limited"
)

// ToolHookContext provides context for tool execution hooks.
type ToolHookContext struct {
	// ToolName is the name of the tool being executed.
	ToolName string `json:"tool_name"`

	// ToolCallID is the unique identifier for this tool call.
	ToolCallID string `json:"tool_call_id"`

	// Input is the tool input (may be modified by pre-hooks).
	Input json.RawMessage `json:"input"`

	// Output is the tool output (available in post-hooks).
	Output string `json:"output,omitempty"`

	// Error is set if the tool execution failed.
	Error    error  `json:"-"`
	ErrorMsg string `json:"error,omitempty"`

	// Duration is the execution time (available in post-hooks).
	Duration time.Duration `json:"duration,omitempty"`

	// Attempt is the current retry attempt number.
	Attempt int `json:"attempt"`

	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int `json:"max_attempts"`

	// SessionKey is the session this tool call belongs to.
	SessionKey string `json:"session_key,omitempty"`

	// AgentID is the agent making the tool call.
	AgentID string `json:"agent_id,omitempty"`

	// Canceled indicates if execution should be skipped.
	Canceled bool `json:"canceled"`

	// CancelReason explains why execution was canceled.
	CancelReason string `json:"cancel_reason,omitempty"`

	// Modified indicates if the input/output was modified by a hook.
	Modified bool `json:"modified"`

	// Metadata stores additional hook-specific data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToolPreHook is a specialized handler for pre-execution hooks.
// It can modify the input or cancel execution.
type ToolPreHook func(ctx context.Context, hookCtx *ToolHookContext) error

// ToolPostHook is a specialized handler for post-execution hooks.
// It can modify the output or perform cleanup.
type ToolPostHook func(ctx context.Context, hookCtx *ToolHookContext) error

// ToolHookManager manages tool execution hooks.
type ToolHookManager struct {
	registry *Registry
	logger   *slog.Logger

	// preHooks are handlers that run before tool execution.
	preHooks []toolHookEntry

	// postHooks are handlers that run after tool execution.
	postHooks []toolHookEntry

	// toolFilters allow hooks to be registered for specific tools.
	toolFilters map[string][]string // hookID -> []toolNames

	mu sync.RWMutex
}

type toolHookEntry struct {
	ID       string
	Name     string
	Priority Priority
	Handler  Handler
	Tools    []string // Empty means all tools
}

// NewToolHookManager creates a new tool hook manager.
func NewToolHookManager(registry *Registry, logger *slog.Logger) *ToolHookManager {
	if logger == nil {
		logger = slog.Default()
	}
	if registry == nil {
		registry = Global()
	}

	return &ToolHookManager{
		registry:    registry,
		logger:      logger.With("component", "tool-hooks"),
		toolFilters: make(map[string][]string),
	}
}

// RegisterPreHook registers a pre-execution hook.
func (m *ToolHookManager) RegisterPreHook(name string, handler ToolPreHook, opts ...ToolHookOption) string {
	cfg := &toolHookConfig{priority: PriorityNormal}
	for _, opt := range opts {
		opt(cfg)
	}

	// Wrap the specialized handler
	wrappedHandler := func(ctx context.Context, event *Event) error {
		hookCtx, ok := event.Context["tool_hook_context"].(*ToolHookContext)
		if !ok {
			return nil
		}

		// Check tool filter
		if len(cfg.tools) > 0 && !contains(cfg.tools, hookCtx.ToolName) {
			return nil
		}

		return handler(ctx, hookCtx)
	}

	id := m.registry.Register(string(EventToolPreExecution), wrappedHandler,
		WithName(name),
		WithPriority(cfg.priority),
	)

	m.mu.Lock()
	m.preHooks = append(m.preHooks, toolHookEntry{
		ID:       id,
		Name:     name,
		Priority: cfg.priority,
		Handler:  wrappedHandler,
		Tools:    cfg.tools,
	})
	if len(cfg.tools) > 0 {
		m.toolFilters[id] = cfg.tools
	}
	m.mu.Unlock()

	m.logger.Debug("registered pre-execution hook", "id", id, "name", name, "tools", cfg.tools)
	return id
}

// RegisterPostHook registers a post-execution hook.
func (m *ToolHookManager) RegisterPostHook(name string, handler ToolPostHook, opts ...ToolHookOption) string {
	cfg := &toolHookConfig{priority: PriorityNormal}
	for _, opt := range opts {
		opt(cfg)
	}

	// Wrap the specialized handler
	wrappedHandler := func(ctx context.Context, event *Event) error {
		hookCtx, ok := event.Context["tool_hook_context"].(*ToolHookContext)
		if !ok {
			return nil
		}

		// Check tool filter
		if len(cfg.tools) > 0 && !contains(cfg.tools, hookCtx.ToolName) {
			return nil
		}

		return handler(ctx, hookCtx)
	}

	id := m.registry.Register(string(EventToolPostExecution), wrappedHandler,
		WithName(name),
		WithPriority(cfg.priority),
	)

	m.mu.Lock()
	m.postHooks = append(m.postHooks, toolHookEntry{
		ID:       id,
		Name:     name,
		Priority: cfg.priority,
		Handler:  wrappedHandler,
		Tools:    cfg.tools,
	})
	if len(cfg.tools) > 0 {
		m.toolFilters[id] = cfg.tools
	}
	m.mu.Unlock()

	m.logger.Debug("registered post-execution hook", "id", id, "name", name, "tools", cfg.tools)
	return id
}

// Unregister removes a hook by ID.
func (m *ToolHookManager) Unregister(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from pre-hooks
	for i, h := range m.preHooks {
		if h.ID == id {
			m.preHooks = append(m.preHooks[:i], m.preHooks[i+1:]...)
			break
		}
	}

	// Remove from post-hooks
	for i, h := range m.postHooks {
		if h.ID == id {
			m.postHooks = append(m.postHooks[:i], m.postHooks[i+1:]...)
			break
		}
	}

	delete(m.toolFilters, id)
	return m.registry.Unregister(id)
}

// TriggerPreExecution triggers pre-execution hooks.
func (m *ToolHookManager) TriggerPreExecution(ctx context.Context, hookCtx *ToolHookContext) error {
	event := NewEvent(EventToolPreExecution, "pre_execution").
		WithSession(hookCtx.SessionKey).
		WithContext("tool_hook_context", hookCtx).
		WithContext("tool_name", hookCtx.ToolName).
		WithContext("tool_call_id", hookCtx.ToolCallID)

	return m.registry.Trigger(ctx, event)
}

// TriggerPostExecution triggers post-execution hooks.
func (m *ToolHookManager) TriggerPostExecution(ctx context.Context, hookCtx *ToolHookContext) error {
	event := NewEvent(EventToolPostExecution, "post_execution").
		WithSession(hookCtx.SessionKey).
		WithContext("tool_hook_context", hookCtx).
		WithContext("tool_name", hookCtx.ToolName).
		WithContext("tool_call_id", hookCtx.ToolCallID).
		WithContext("duration_ms", hookCtx.Duration.Milliseconds())

	if hookCtx.Error != nil {
		event = event.WithError(hookCtx.Error)
	}

	return m.registry.Trigger(ctx, event)
}

// toolHookConfig configures tool hook registration.
type toolHookConfig struct {
	priority Priority
	tools    []string
}

// ToolHookOption configures tool hook registration.
type ToolHookOption func(*toolHookConfig)

// ForTools limits the hook to specific tools.
func ForTools(tools ...string) ToolHookOption {
	return func(c *toolHookConfig) {
		c.tools = tools
	}
}

// WithHookPriority sets the hook priority.
func WithHookPriority(p Priority) ToolHookOption {
	return func(c *toolHookConfig) {
		c.priority = p
	}
}

// ApprovalRequest represents a request for tool execution approval.
type ApprovalRequest struct {
	// ID is a unique identifier for this approval request.
	ID string `json:"id"`

	// ToolName is the tool requesting approval.
	ToolName string `json:"tool_name"`

	// ToolCallID is the tool call identifier.
	ToolCallID string `json:"tool_call_id"`

	// Input is the tool input.
	Input json.RawMessage `json:"input"`

	// SessionKey is the session making the request.
	SessionKey string `json:"session_key"`

	// AgentID is the agent making the request.
	AgentID string `json:"agent_id"`

	// Reason explains why approval is needed.
	Reason string `json:"reason"`

	// RequestedAt is when approval was requested.
	RequestedAt time.Time `json:"requested_at"`

	// ExpiresAt is when the request expires.
	ExpiresAt time.Time `json:"expires_at"`

	// Metadata contains additional context.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ApprovalResponse represents a response to an approval request.
type ApprovalResponse struct {
	// RequestID links to the original request.
	RequestID string `json:"request_id"`

	// Approved indicates if the request was approved.
	Approved bool `json:"approved"`

	// ApprovedBy identifies who approved/denied.
	ApprovedBy string `json:"approved_by,omitempty"`

	// Reason explains the decision.
	Reason string `json:"reason,omitempty"`

	// RespondedAt is when the response was given.
	RespondedAt time.Time `json:"responded_at"`

	// ModifiedInput is optionally modified input (if approved with changes).
	ModifiedInput json.RawMessage `json:"modified_input,omitempty"`
}

// ApprovalWorkflow manages tool approval workflows following Clawdbot patterns.
type ApprovalWorkflow struct {
	registry       *Registry
	logger         *slog.Logger
	pendingMu      sync.RWMutex
	pending        map[string]*ApprovalRequest
	responseChans  map[string]chan *ApprovalResponse
	defaultTimeout time.Duration
}

// NewApprovalWorkflow creates a new approval workflow manager.
func NewApprovalWorkflow(registry *Registry, logger *slog.Logger) *ApprovalWorkflow {
	if logger == nil {
		logger = slog.Default()
	}
	if registry == nil {
		registry = Global()
	}

	return &ApprovalWorkflow{
		registry:       registry,
		logger:         logger.With("component", "approval-workflow"),
		pending:        make(map[string]*ApprovalRequest),
		responseChans:  make(map[string]chan *ApprovalResponse),
		defaultTimeout: 5 * time.Minute,
	}
}

// RequestApproval initiates an approval request and waits for a response.
func (w *ApprovalWorkflow) RequestApproval(ctx context.Context, req *ApprovalRequest) (*ApprovalResponse, error) {
	if req.ID == "" {
		req.ID = fmt.Sprintf("approval-%s-%d", req.ToolCallID, time.Now().UnixNano())
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now()
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = req.RequestedAt.Add(w.defaultTimeout)
	}

	// Create response channel
	responseChan := make(chan *ApprovalResponse, 1)

	w.pendingMu.Lock()
	w.pending[req.ID] = req
	w.responseChans[req.ID] = responseChan
	w.pendingMu.Unlock()

	// Trigger approval required event
	event := NewEvent(EventToolApprovalRequired, "approval_required").
		WithSession(req.SessionKey).
		WithContext("approval_request", req).
		WithContext("tool_name", req.ToolName).
		WithContext("tool_call_id", req.ToolCallID)

	if err := w.registry.Trigger(ctx, event); err != nil {
		w.logger.Warn("failed to trigger approval required event", "error", err)
	}

	w.logger.Info("approval requested",
		"request_id", req.ID,
		"tool_name", req.ToolName,
		"expires_at", req.ExpiresAt,
	)

	// Wait for response or timeout
	timeout := time.Until(req.ExpiresAt)
	if timeout < 0 {
		timeout = 0 // Clamp to prevent negative duration
	}
	select {
	case response := <-responseChan:
		w.cleanup(req.ID)
		return response, nil
	case <-time.After(timeout):
		w.cleanup(req.ID)
		// Trigger timeout event
		timeoutEvent := NewEvent(EventToolApprovalTimeout, "approval_timeout").
			WithSession(req.SessionKey).
			WithContext("approval_request", req)
		w.registry.TriggerAsync(ctx, timeoutEvent)
		return nil, fmt.Errorf("approval request timed out after %v", timeout)
	case <-ctx.Done():
		w.cleanup(req.ID)
		return nil, ctx.Err()
	}
}

// Respond processes an approval response.
func (w *ApprovalWorkflow) Respond(ctx context.Context, response *ApprovalResponse) error {
	w.pendingMu.RLock()
	req, exists := w.pending[response.RequestID]
	responseChan, hasChan := w.responseChans[response.RequestID]
	w.pendingMu.RUnlock()

	if !exists {
		return fmt.Errorf("no pending approval request with ID: %s", response.RequestID)
	}

	if response.RespondedAt.IsZero() {
		response.RespondedAt = time.Now()
	}

	// Trigger appropriate event
	var eventType EventType
	if response.Approved {
		eventType = EventToolApprovalGranted
	} else {
		eventType = EventToolApprovalDenied
	}

	event := NewEvent(eventType, "approval_response").
		WithSession(req.SessionKey).
		WithContext("approval_request", req).
		WithContext("approval_response", response)

	if err := w.registry.Trigger(ctx, event); err != nil {
		w.logger.Warn("failed to trigger approval response event", "error", err)
	}

	w.logger.Info("approval response received",
		"request_id", response.RequestID,
		"approved", response.Approved,
		"approved_by", response.ApprovedBy,
	)

	// Send response to waiting goroutine
	if hasChan {
		select {
		case responseChan <- response:
		default:
		}
	}

	return nil
}

// GetPending returns all pending approval requests.
func (w *ApprovalWorkflow) GetPending() []*ApprovalRequest {
	w.pendingMu.RLock()
	defer w.pendingMu.RUnlock()

	result := make([]*ApprovalRequest, 0, len(w.pending))
	for _, req := range w.pending {
		result = append(result, req)
	}
	return result
}

// GetPendingBySession returns pending requests for a session.
func (w *ApprovalWorkflow) GetPendingBySession(sessionKey string) []*ApprovalRequest {
	w.pendingMu.RLock()
	defer w.pendingMu.RUnlock()

	var result []*ApprovalRequest
	for _, req := range w.pending {
		if req.SessionKey == sessionKey {
			result = append(result, req)
		}
	}
	return result
}

// Cancel cancels a pending approval request.
func (w *ApprovalWorkflow) Cancel(requestID string) bool {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	if _, exists := w.pending[requestID]; !exists {
		return false
	}

	if ch, ok := w.responseChans[requestID]; ok {
		close(ch)
		delete(w.responseChans, requestID)
	}
	delete(w.pending, requestID)
	return true
}

// SetDefaultTimeout sets the default approval timeout.
func (w *ApprovalWorkflow) SetDefaultTimeout(d time.Duration) {
	w.defaultTimeout = d
}

// cleanup removes a completed request.
func (w *ApprovalWorkflow) cleanup(requestID string) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	delete(w.pending, requestID)
	// Don't close the channel - just remove from map
	// This prevents panic if Respond() tries to send to a closed channel
	// The channel will be garbage collected when no longer referenced
	delete(w.responseChans, requestID)
}

// contains checks if a slice contains a value.
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// NewToolEvent creates a tool-related event.
func NewToolEvent(eventType EventType, toolName, toolCallID string) *Event {
	return NewEvent(eventType, toolName).
		WithContext("tool_name", toolName).
		WithContext("tool_call_id", toolCallID)
}

// TriggerToolEvent is a convenience function to trigger a tool event.
func TriggerToolEvent(ctx context.Context, eventType EventType, toolName, toolCallID string, details map[string]any) error {
	event := NewToolEvent(eventType, toolName, toolCallID)
	for k, v := range details {
		event = event.WithContext(k, v)
	}
	return Global().Trigger(ctx, event)
}

// EmitToolEvent is a convenience function to emit a tool event asynchronously.
func EmitToolEvent(ctx context.Context, eventType EventType, toolName, toolCallID string, details map[string]any) {
	event := NewToolEvent(eventType, toolName, toolCallID)
	for k, v := range details {
		event = event.WithContext(k, v)
	}
	Global().TriggerAsync(ctx, event)
}

// ToolEventFromModel creates an Event from a models.ToolEvent.
func ToolEventFromModel(te *models.ToolEvent) *Event {
	var eventType EventType
	switch te.Stage {
	case models.ToolEventRequested:
		eventType = EventToolCalled
	case models.ToolEventStarted:
		eventType = EventToolPreExecution
	case models.ToolEventSucceeded:
		eventType = EventToolCompleted
	case models.ToolEventFailed:
		eventType = EventToolCompleted
	case models.ToolEventDenied:
		eventType = EventToolApprovalDenied
	case models.ToolEventRetrying:
		eventType = EventToolRetry
	case models.ToolEventApprovalRequired:
		eventType = EventToolApprovalRequired
	default:
		eventType = EventToolCalled
	}

	event := NewEvent(eventType, string(te.Stage)).
		WithContext("tool_name", te.ToolName).
		WithContext("tool_call_id", te.ToolCallID).
		WithContext("attempt", te.Attempt)

	if te.Error != "" {
		event.ErrorMsg = te.Error
	}
	if te.PolicyReason != "" {
		event = event.WithContext("policy_reason", te.PolicyReason)
	}

	return event
}
