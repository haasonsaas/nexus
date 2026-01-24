package hooks

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNewToolHookManager(t *testing.T) {
	t.Run("creates with nil registry", func(t *testing.T) {
		mgr := NewToolHookManager(nil, nil)
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}
		if mgr.registry == nil {
			t.Error("registry should default to global")
		}
		if mgr.logger == nil {
			t.Error("logger should default")
		}
	})

	t.Run("creates with provided registry", func(t *testing.T) {
		reg := NewRegistry(nil)
		mgr := NewToolHookManager(reg, nil)
		if mgr.registry != reg {
			t.Error("should use provided registry")
		}
	})
}

func TestToolHookManager_RegisterPreHook(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	id := mgr.RegisterPreHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		return nil
	})

	if id == "" {
		t.Error("expected non-empty hook ID")
	}

	// Verify hook is registered
	mgr.mu.RLock()
	if len(mgr.preHooks) != 1 {
		t.Errorf("expected 1 pre-hook, got %d", len(mgr.preHooks))
	}
	mgr.mu.RUnlock()
}

func TestToolHookManager_RegisterPostHook(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	id := mgr.RegisterPostHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		return nil
	})

	if id == "" {
		t.Error("expected non-empty hook ID")
	}

	// Verify hook is registered
	mgr.mu.RLock()
	if len(mgr.postHooks) != 1 {
		t.Errorf("expected 1 post-hook, got %d", len(mgr.postHooks))
	}
	mgr.mu.RUnlock()
}

func TestToolHookManager_Unregister(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	id := mgr.RegisterPreHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		return nil
	})

	// Unregister
	result := mgr.Unregister(id)
	if !result {
		t.Error("expected successful unregister")
	}

	mgr.mu.RLock()
	if len(mgr.preHooks) != 0 {
		t.Errorf("expected 0 pre-hooks after unregister, got %d", len(mgr.preHooks))
	}
	mgr.mu.RUnlock()
}

func TestToolHookManager_TriggerPreExecution(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	called := false
	mgr.RegisterPreHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		called = true
		return nil
	})

	hookCtx := &ToolHookContext{
		ToolName:   "test-tool",
		ToolCallID: "call-1",
		SessionKey: "session-1",
	}

	err := mgr.TriggerPreExecution(context.Background(), hookCtx)
	if err != nil {
		t.Errorf("TriggerPreExecution error: %v", err)
	}
	if !called {
		t.Error("pre-hook was not called")
	}
}

func TestToolHookManager_TriggerPostExecution(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	called := false
	mgr.RegisterPostHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		called = true
		return nil
	})

	hookCtx := &ToolHookContext{
		ToolName:   "test-tool",
		ToolCallID: "call-1",
		Duration:   100 * time.Millisecond,
	}

	err := mgr.TriggerPostExecution(context.Background(), hookCtx)
	if err != nil {
		t.Errorf("TriggerPostExecution error: %v", err)
	}
	if !called {
		t.Error("post-hook was not called")
	}
}

func TestToolHookManager_TriggerPostExecution_WithError(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	mgr.RegisterPostHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		return nil
	})

	hookCtx := &ToolHookContext{
		ToolName:   "test-tool",
		ToolCallID: "call-1",
		Error:      context.DeadlineExceeded,
	}

	err := mgr.TriggerPostExecution(context.Background(), hookCtx)
	if err != nil {
		t.Errorf("TriggerPostExecution error: %v", err)
	}
}

func TestForTools(t *testing.T) {
	opt := ForTools("tool-a", "tool-b")
	cfg := &toolHookConfig{}
	opt(cfg)

	if len(cfg.tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(cfg.tools))
	}
}

func TestWithHookPriority(t *testing.T) {
	opt := WithHookPriority(PriorityHigh)
	cfg := &toolHookConfig{}
	opt(cfg)

	if cfg.priority != PriorityHigh {
		t.Errorf("priority = %d, want %d", cfg.priority, PriorityHigh)
	}
}

func TestNewApprovalWorkflow(t *testing.T) {
	t.Run("creates with defaults", func(t *testing.T) {
		w := NewApprovalWorkflow(nil, nil)
		if w == nil {
			t.Fatal("expected non-nil workflow")
		}
		if w.registry == nil {
			t.Error("registry should default to global")
		}
		if w.logger == nil {
			t.Error("logger should default")
		}
		if w.defaultTimeout != 5*time.Minute {
			t.Errorf("defaultTimeout = %v, want 5m", w.defaultTimeout)
		}
	})
}

func TestApprovalWorkflow_GetPending(t *testing.T) {
	w := NewApprovalWorkflow(NewRegistry(nil), nil)

	// Initially empty
	pending := w.GetPending()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}

	// Add a pending request manually
	w.pendingMu.Lock()
	w.pending["req-1"] = &ApprovalRequest{
		ID:       "req-1",
		ToolName: "bash",
	}
	w.pendingMu.Unlock()

	pending = w.GetPending()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
}

func TestApprovalWorkflow_GetPendingBySession(t *testing.T) {
	w := NewApprovalWorkflow(NewRegistry(nil), nil)

	// Add requests for different sessions
	w.pendingMu.Lock()
	w.pending["req-1"] = &ApprovalRequest{
		ID:         "req-1",
		SessionKey: "session-a",
	}
	w.pending["req-2"] = &ApprovalRequest{
		ID:         "req-2",
		SessionKey: "session-b",
	}
	w.pending["req-3"] = &ApprovalRequest{
		ID:         "req-3",
		SessionKey: "session-a",
	}
	w.pendingMu.Unlock()

	bySession := w.GetPendingBySession("session-a")
	if len(bySession) != 2 {
		t.Errorf("expected 2 pending for session-a, got %d", len(bySession))
	}

	bySession = w.GetPendingBySession("session-c")
	if len(bySession) != 0 {
		t.Errorf("expected 0 pending for session-c, got %d", len(bySession))
	}
}

func TestApprovalWorkflow_Cancel(t *testing.T) {
	w := NewApprovalWorkflow(NewRegistry(nil), nil)

	// Add a pending request
	w.pendingMu.Lock()
	w.pending["req-1"] = &ApprovalRequest{ID: "req-1"}
	w.responseChans["req-1"] = make(chan *ApprovalResponse, 1)
	w.pendingMu.Unlock()

	// Cancel it
	result := w.Cancel("req-1")
	if !result {
		t.Error("expected successful cancel")
	}

	// Verify it's removed
	w.pendingMu.RLock()
	_, exists := w.pending["req-1"]
	w.pendingMu.RUnlock()
	if exists {
		t.Error("request should be removed after cancel")
	}

	// Cancel non-existent
	result = w.Cancel("nonexistent")
	if result {
		t.Error("expected false for non-existent request")
	}
}

func TestApprovalWorkflow_SetDefaultTimeout(t *testing.T) {
	w := NewApprovalWorkflow(nil, nil)

	w.SetDefaultTimeout(10 * time.Minute)
	if w.defaultTimeout != 10*time.Minute {
		t.Errorf("defaultTimeout = %v, want 10m", w.defaultTimeout)
	}
}

func TestApprovalWorkflow_Respond_NotFound(t *testing.T) {
	w := NewApprovalWorkflow(NewRegistry(nil), nil)

	err := w.Respond(context.Background(), &ApprovalResponse{
		RequestID: "nonexistent",
	})
	if err == nil {
		t.Error("expected error for non-existent request")
	}
}

func TestApprovalWorkflow_Respond(t *testing.T) {
	reg := NewRegistry(nil)
	w := NewApprovalWorkflow(reg, nil)

	// Set up a pending request with response channel
	responseChan := make(chan *ApprovalResponse, 1)
	w.pendingMu.Lock()
	w.pending["req-1"] = &ApprovalRequest{
		ID:         "req-1",
		SessionKey: "session-1",
	}
	w.responseChans["req-1"] = responseChan
	w.pendingMu.Unlock()

	// Send response
	err := w.Respond(context.Background(), &ApprovalResponse{
		RequestID:  "req-1",
		Approved:   true,
		ApprovedBy: "user-1",
	})
	if err != nil {
		t.Errorf("Respond error: %v", err)
	}

	// Check response was sent to channel
	select {
	case resp := <-responseChan:
		if !resp.Approved {
			t.Error("expected approved response")
		}
	default:
		t.Error("expected response on channel")
	}
}

func TestToolHookContext_Struct(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"key": "value"})
	ctx := ToolHookContext{
		ToolName:     "bash",
		ToolCallID:   "call-1",
		Input:        input,
		Output:       "result",
		Duration:     100 * time.Millisecond,
		Attempt:      1,
		MaxAttempts:  3,
		SessionKey:   "session-1",
		AgentID:      "agent-1",
		Canceled:     false,
		CancelReason: "",
		Modified:     true,
		Metadata:     map[string]any{"key": "value"},
	}

	if ctx.ToolName != "bash" {
		t.Errorf("ToolName = %q", ctx.ToolName)
	}
	if ctx.Attempt != 1 {
		t.Errorf("Attempt = %d", ctx.Attempt)
	}
}

func TestApprovalRequest_Struct(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"cmd": "ls"})
	req := ApprovalRequest{
		ID:          "req-1",
		ToolName:    "bash",
		ToolCallID:  "call-1",
		Input:       input,
		SessionKey:  "session-1",
		AgentID:     "agent-1",
		Reason:      "dangerous command",
		RequestedAt: time.Now(),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		Metadata:    map[string]any{"priority": "high"},
	}

	if req.ToolName != "bash" {
		t.Errorf("ToolName = %q", req.ToolName)
	}
}

func TestApprovalResponse_Struct(t *testing.T) {
	resp := ApprovalResponse{
		RequestID:     "req-1",
		Approved:      true,
		ApprovedBy:    "admin",
		Reason:        "approved for testing",
		RespondedAt:   time.Now(),
		ModifiedInput: json.RawMessage(`{"cmd": "ls -la"}`),
	}

	if !resp.Approved {
		t.Error("Approved should be true")
	}
	if resp.ApprovedBy != "admin" {
		t.Errorf("ApprovedBy = %q", resp.ApprovedBy)
	}
}

func TestToolEventConstants(t *testing.T) {
	tests := []struct {
		event    EventType
		expected string
	}{
		{EventToolPreExecution, "tool.pre_execution"},
		{EventToolPostExecution, "tool.post_execution"},
		{EventToolApprovalRequired, "tool.approval_required"},
		{EventToolApprovalGranted, "tool.approval_granted"},
		{EventToolApprovalDenied, "tool.approval_denied"},
		{EventToolApprovalTimeout, "tool.approval_timeout"},
		{EventToolRetry, "tool.retry"},
		{EventToolRateLimited, "tool.rate_limited"},
	}

	for _, tt := range tests {
		if string(tt.event) != tt.expected {
			t.Errorf("EventType = %q, want %q", tt.event, tt.expected)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		slice    []string
		value    string
		expected bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{nil, "a", false},
	}

	for _, tt := range tests {
		result := contains(tt.slice, tt.value)
		if result != tt.expected {
			t.Errorf("contains(%v, %q) = %v, want %v", tt.slice, tt.value, result, tt.expected)
		}
	}
}

func TestNewToolEvent(t *testing.T) {
	event := NewToolEvent(EventToolCalled, "bash", "call-123")

	if event.Type != EventToolCalled {
		t.Errorf("Type = %q, want %q", event.Type, EventToolCalled)
	}
	if event.Context["tool_name"] != "bash" {
		t.Errorf("tool_name = %v", event.Context["tool_name"])
	}
	if event.Context["tool_call_id"] != "call-123" {
		t.Errorf("tool_call_id = %v", event.Context["tool_call_id"])
	}
}

func TestToolHookManager_HookWithToolFilter(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	called := false
	mgr.RegisterPreHook("filtered-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		called = true
		return nil
	}, ForTools("specific-tool"))

	// Trigger for different tool - should not call
	hookCtx := &ToolHookContext{
		ToolName:   "other-tool",
		ToolCallID: "call-1",
	}

	_ = mgr.TriggerPreExecution(context.Background(), hookCtx)
	if called {
		t.Error("hook should not be called for filtered tool")
	}

	// Trigger for matching tool
	hookCtx.ToolName = "specific-tool"
	_ = mgr.TriggerPreExecution(context.Background(), hookCtx)
	if !called {
		t.Error("hook should be called for matching tool")
	}
}

func TestToolHookManager_UnregisterPostHook(t *testing.T) {
	reg := NewRegistry(nil)
	mgr := NewToolHookManager(reg, nil)

	id := mgr.RegisterPostHook("test-hook", func(ctx context.Context, hookCtx *ToolHookContext) error {
		return nil
	}, ForTools("bash"))

	// Unregister
	result := mgr.Unregister(id)
	if !result {
		t.Error("expected successful unregister")
	}

	mgr.mu.RLock()
	if len(mgr.postHooks) != 0 {
		t.Errorf("expected 0 post-hooks after unregister, got %d", len(mgr.postHooks))
	}
	if _, exists := mgr.toolFilters[id]; exists {
		t.Error("tool filter should be removed")
	}
	mgr.mu.RUnlock()
}
