package agent

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestApprovalChecker_Allowlist(t *testing.T) {
	policy := DefaultApprovalPolicy()
	policy.Allowlist = []string{"read_file", "list_*"}
	checker := NewApprovalChecker(policy)

	tests := []struct {
		name     string
		tool     string
		expected ApprovalDecision
	}{
		{"exact match", "read_file", ApprovalAllowed},
		{"prefix match", "list_files", ApprovalAllowed},
		{"prefix match 2", "list_directory", ApprovalAllowed},
		{"no match", "write_file", ApprovalPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: tt.tool})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_Denylist(t *testing.T) {
	policy := DefaultApprovalPolicy()
	policy.Allowlist = []string{"*"} // allow everything except denylist
	policy.Denylist = []string{"rm", "delete_*"}
	checker := NewApprovalChecker(policy)

	tests := []struct {
		name     string
		tool     string
		expected ApprovalDecision
	}{
		{"exact deny", "rm", ApprovalDenied},
		{"prefix deny", "delete_file", ApprovalDenied},
		{"allowed", "read_file", ApprovalAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: tt.tool})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_SafeBins(t *testing.T) {
	policy := DefaultApprovalPolicy()
	policy.SafeBins = []string{"cat", "head", "tail"}
	checker := NewApprovalChecker(policy)

	tests := []struct {
		name     string
		tool     string
		expected ApprovalDecision
	}{
		{"cat allowed", "cat", ApprovalAllowed},
		{"head allowed", "head", ApprovalAllowed},
		{"rm pending", "rm", ApprovalPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: tt.tool})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_SkillTools(t *testing.T) {
	policy := DefaultApprovalPolicy()
	policy.SkillAllowlist = true
	checker := NewApprovalChecker(policy)
	checker.RegisterSkillTools([]string{"skill_tool_1", "skill_tool_2"})

	tests := []struct {
		name     string
		tool     string
		expected ApprovalDecision
	}{
		{"skill tool allowed", "skill_tool_1", ApprovalAllowed},
		{"skill tool 2 allowed", "skill_tool_2", ApprovalAllowed},
		{"non-skill pending", "other_tool", ApprovalPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: tt.tool})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_PerAgentPolicy(t *testing.T) {
	policy := DefaultApprovalPolicy()
	policy.DefaultDecision = ApprovalPending
	checker := NewApprovalChecker(policy)
	agent1 := DefaultApprovalPolicy()
	agent1.Allowlist = []string{"bash"}
	checker.SetAgentPolicy("agent-1", agent1)
	agent2 := DefaultApprovalPolicy()
	agent2.Denylist = []string{"bash"}
	checker.SetAgentPolicy("agent-2", agent2)

	// Agent 1 can use bash
	decision, _ := checker.Check(context.Background(), "agent-1", models.ToolCall{Name: "bash"})
	if decision != ApprovalAllowed {
		t.Errorf("agent-1 should be allowed bash, got %v", decision)
	}

	// Agent 2 cannot use bash
	decision, _ = checker.Check(context.Background(), "agent-2", models.ToolCall{Name: "bash"})
	if decision != ApprovalDenied {
		t.Errorf("agent-2 should be denied bash, got %v", decision)
	}

	// Unknown agent uses default policy
	decision, _ = checker.Check(context.Background(), "agent-3", models.ToolCall{Name: "bash"})
	if decision != ApprovalPending {
		t.Errorf("agent-3 should use default policy, got %v", decision)
	}
}

func TestApprovalChecker_MCPPattern(t *testing.T) {
	policy := DefaultApprovalPolicy()
	policy.Allowlist = []string{"mcp:*"}
	checker := NewApprovalChecker(policy)

	tests := []struct {
		name     string
		tool     string
		expected ApprovalDecision
	}{
		{"mcp tool allowed", "mcp:github.search", ApprovalAllowed},
		{"mcp tool 2", "mcp:slack.send", ApprovalAllowed},
		{"non-mcp pending", "other_tool", ApprovalPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: tt.tool})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_CreateAndDecide(t *testing.T) {
	store := NewMemoryApprovalStore()
	policy := DefaultApprovalPolicy()
	policy.RequireApproval = []string{"dangerous_tool"}
	checker := NewApprovalChecker(policy)
	checker.SetStore(store)

	ctx := context.Background()
	toolCall := models.ToolCall{
		ID:   "call-1",
		Name: "dangerous_tool",
	}

	// Check should return pending
	decision, _ := checker.Check(ctx, "agent-1", toolCall)
	if decision != ApprovalPending {
		t.Fatalf("expected pending, got %v", decision)
	}

	// Create approval request
	req, err := checker.CreateApprovalRequest(ctx, "agent-1", "session-1", toolCall, "requires approval")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.Decision != ApprovalPending {
		t.Fatalf("expected pending request, got %v", req.Decision)
	}

	// Get pending requests
	pending, _ := checker.GetPendingRequests(ctx, "agent-1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	// Approve the request
	if err := checker.Approve(ctx, req.ID, "user-1"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Request should be approved
	approved, _ := store.Get(ctx, req.ID)
	if approved.Decision != ApprovalAllowed {
		t.Fatalf("expected allowed, got %v", approved.Decision)
	}

	// No more pending requests
	pending, _ = checker.GetPendingRequests(ctx, "agent-1")
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending after approval, got %d", len(pending))
	}
}

func TestApprovalChecker_DeniesWhenUIUnavailableAndAskFallbackOff(t *testing.T) {
	checker := NewApprovalChecker(&ApprovalPolicy{
		RequireApproval: []string{"dangerous_tool"},
		AskFallback:     false,
	})

	decision, reason := checker.Check(context.Background(), "agent-1", models.ToolCall{Name: "dangerous_tool"})
	if decision != ApprovalDenied {
		t.Fatalf("expected denied when UI unavailable, got %v (%s)", decision, reason)
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		patterns []string
		tool     string
		expected bool
	}{
		{[]string{"foo"}, "foo", true},
		{[]string{"foo"}, "bar", false},
		{[]string{"foo*"}, "foobar", true},
		{[]string{"foo*"}, "foo", true},
		{[]string{"foo*"}, "barfoo", false},
		{[]string{"*bar"}, "foobar", true},
		{[]string{"*bar"}, "bar", true},
		{[]string{"*bar"}, "barfoo", false},
		{[]string{"mcp:*"}, "mcp:github.search", true},
		{[]string{"mcp:*"}, "other_tool", false},
		{[]string{""}, "anything", false},
	}

	for _, tt := range tests {
		result := matchesPattern(tt.patterns, tt.tool)
		if result != tt.expected {
			t.Errorf("matchesPattern(%v, %q) = %v, want %v", tt.patterns, tt.tool, result, tt.expected)
		}
	}
}

func TestApprovalChecker_DefaultDecision(t *testing.T) {
	tests := []struct {
		name            string
		defaultDecision ApprovalDecision
		expected        ApprovalDecision
	}{
		{"empty default", "", ApprovalPending},
		{"allowed default", ApprovalAllowed, ApprovalAllowed},
		{"denied default", ApprovalDenied, ApprovalDenied},
		{"pending default", ApprovalPending, ApprovalPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewApprovalChecker(&ApprovalPolicy{
				DefaultDecision: tt.defaultDecision,
			})

			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: "unknown_tool"})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_SuffixPattern(t *testing.T) {
	checker := NewApprovalChecker(&ApprovalPolicy{
		Allowlist: []string{"*_read", "*_list"},
	})

	tests := []struct {
		name     string
		tool     string
		expected ApprovalDecision
	}{
		{"suffix match _read", "file_read", ApprovalAllowed},
		{"suffix match _list", "dir_list", ApprovalAllowed},
		{"no match", "file_write", ApprovalPending},
		{"exact suffix", "_read", ApprovalAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: tt.tool})
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestApprovalChecker_WildcardAll(t *testing.T) {
	checker := NewApprovalChecker(&ApprovalPolicy{
		Allowlist: []string{"*"},
		Denylist:  []string{"dangerous"},
	})

	// Everything allowed except denylist
	decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: "any_tool"})
	if decision != ApprovalAllowed {
		t.Errorf("expected allowed for any_tool, got %v", decision)
	}

	// Denylist takes precedence
	decision, _ = checker.Check(context.Background(), "", models.ToolCall{Name: "dangerous"})
	if decision != ApprovalDenied {
		t.Errorf("expected denied for dangerous, got %v", decision)
	}
}

func TestApprovalChecker_Deny(t *testing.T) {
	store := NewMemoryApprovalStore()
	checker := NewApprovalChecker(&ApprovalPolicy{
		RequireApproval: []string{"risky_tool"},
	})
	checker.SetStore(store)

	ctx := context.Background()
	toolCall := models.ToolCall{
		ID:   "call-1",
		Name: "risky_tool",
	}

	// Create request
	req, err := checker.CreateApprovalRequest(ctx, "agent-1", "session-1", toolCall, "needs approval")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	// Deny the request
	if err := checker.Deny(ctx, req.ID, "admin"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	// Verify denied
	denied, _ := store.Get(ctx, req.ID)
	if denied.Decision != ApprovalDenied {
		t.Fatalf("expected denied, got %v", denied.Decision)
	}
	if denied.DecidedBy != "admin" {
		t.Errorf("DecidedBy = %q, want %q", denied.DecidedBy, "admin")
	}
}

func TestApprovalChecker_IsUIAvailable(t *testing.T) {
	checker := NewApprovalChecker(nil)

	// No callback set
	if checker.IsUIAvailable() {
		t.Error("expected false when no callback set")
	}

	// Callback returns false
	checker.SetUIAvailableCheck(func() bool { return false })
	if checker.IsUIAvailable() {
		t.Error("expected false when callback returns false")
	}

	// Callback returns true
	checker.SetUIAvailableCheck(func() bool { return true })
	if !checker.IsUIAvailable() {
		t.Error("expected true when callback returns true")
	}
}

func TestApprovalChecker_NilStore(t *testing.T) {
	checker := NewApprovalChecker(&ApprovalPolicy{
		RequireApproval: []string{"tool"},
	})
	// No store set

	ctx := context.Background()
	toolCall := models.ToolCall{ID: "call-1", Name: "tool"}

	// Should not error, just no persistence
	req, err := checker.CreateApprovalRequest(ctx, "agent", "session", toolCall, "reason")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req == nil {
		t.Fatal("expected request even without store")
	}

	// Approve/Deny should not error
	if err := checker.Approve(ctx, req.ID, "user"); err != nil {
		t.Fatalf("approve without store: %v", err)
	}
	if err := checker.Deny(ctx, req.ID, "user"); err != nil {
		t.Fatalf("deny without store: %v", err)
	}

	// GetPendingRequests should return nil without error
	pending, err := checker.GetPendingRequests(ctx, "agent")
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if pending != nil {
		t.Error("expected nil pending without store")
	}
}

func TestMemoryApprovalStore_Prune(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()

	// Create old request
	oldReq := &ApprovalRequest{
		ID:        "old-req",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Decision:  ApprovalPending,
	}
	store.Create(ctx, oldReq)

	// Create recent request
	newReq := &ApprovalRequest{
		ID:        "new-req",
		CreatedAt: time.Now().Add(-10 * time.Minute),
		Decision:  ApprovalPending,
	}
	store.Create(ctx, newReq)

	// Prune requests older than 1 hour
	pruned, err := store.Prune(ctx, time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	// Old request should be gone
	old, _ := store.Get(ctx, "old-req")
	if old != nil {
		t.Error("old request should be pruned")
	}

	// New request should remain
	new, _ := store.Get(ctx, "new-req")
	if new == nil {
		t.Error("new request should remain")
	}
}

func TestMemoryApprovalStore_ListPending_Filters(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()

	// Create requests with different states and agents
	store.Create(ctx, &ApprovalRequest{
		ID:        "pending-agent1",
		AgentID:   "agent-1",
		Decision:  ApprovalPending,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	store.Create(ctx, &ApprovalRequest{
		ID:        "allowed-agent1",
		AgentID:   "agent-1",
		Decision:  ApprovalAllowed,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	store.Create(ctx, &ApprovalRequest{
		ID:        "pending-agent2",
		AgentID:   "agent-2",
		Decision:  ApprovalPending,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	store.Create(ctx, &ApprovalRequest{
		ID:        "expired",
		AgentID:   "agent-1",
		Decision:  ApprovalPending,
		ExpiresAt: time.Now().Add(-time.Hour), // Expired
	})

	// List pending for agent-1
	pending, _ := store.ListPending(ctx, "agent-1")
	if len(pending) != 1 {
		t.Errorf("agent-1 pending = %d, want 1", len(pending))
	}

	// List pending for agent-2
	pending, _ = store.ListPending(ctx, "agent-2")
	if len(pending) != 1 {
		t.Errorf("agent-2 pending = %d, want 1", len(pending))
	}

	// List all pending (empty agentID)
	pending, _ = store.ListPending(ctx, "")
	if len(pending) != 2 {
		t.Errorf("all pending = %d, want 2", len(pending))
	}
}

func TestMemoryApprovalStore_NilRequest(t *testing.T) {
	store := NewMemoryApprovalStore()
	ctx := context.Background()

	// Create nil should not panic
	err := store.Create(ctx, nil)
	if err != nil {
		t.Fatalf("create nil: %v", err)
	}

	// Update nil should not panic
	err = store.Update(ctx, nil)
	if err != nil {
		t.Fatalf("update nil: %v", err)
	}
}

func TestApprovalRequest_Fields(t *testing.T) {
	now := time.Now()
	req := &ApprovalRequest{
		ID:         "req-123",
		ToolCallID: "call-456",
		ToolName:   "dangerous_tool",
		Input:      []byte(`{"action":"delete"}`),
		AgentID:    "agent-1",
		SessionID:  "session-1",
		Reason:     "requires confirmation",
		CreatedAt:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
		Decision:   ApprovalPending,
	}

	if req.ID != "req-123" {
		t.Errorf("ID = %q, want %q", req.ID, "req-123")
	}
	if req.ToolName != "dangerous_tool" {
		t.Errorf("ToolName = %q, want %q", req.ToolName, "dangerous_tool")
	}
	if req.Decision != ApprovalPending {
		t.Errorf("Decision = %v, want %v", req.Decision, ApprovalPending)
	}
}

func TestApprovalPolicy_DefaultApprovalPolicy(t *testing.T) {
	policy := DefaultApprovalPolicy()

	if len(policy.Allowlist) != 0 {
		t.Errorf("Allowlist should be empty by default")
	}
	if len(policy.Denylist) != 0 {
		t.Errorf("Denylist should be empty by default")
	}
	if len(policy.SafeBins) == 0 {
		t.Error("SafeBins should have defaults")
	}
	if !policy.SkillAllowlist {
		t.Error("SkillAllowlist should be true by default")
	}
	if !policy.AskFallback {
		t.Error("AskFallback should be true by default")
	}
	if policy.DefaultDecision != ApprovalPending {
		t.Errorf("DefaultDecision = %v, want %v", policy.DefaultDecision, ApprovalPending)
	}
	if policy.RequestTTL != 5*time.Minute {
		t.Errorf("RequestTTL = %v, want 5m", policy.RequestTTL)
	}
}

func TestApprovalChecker_PriorityOrder(t *testing.T) {
	// Verify that denylist > allowlist > skill > safebins > require_approval > default
	checker := NewApprovalChecker(&ApprovalPolicy{
		Denylist:        []string{"tool_x"},
		Allowlist:       []string{"tool_x", "tool_y"},
		SafeBins:        []string{"tool_x", "tool_y", "tool_z"},
		RequireApproval: []string{"tool_w"},
		SkillAllowlist:  true,
		DefaultDecision: ApprovalAllowed,
	})
	checker.RegisterSkillTools([]string{"tool_x", "tool_y", "tool_z", "tool_w"})

	// tool_x: in denylist (highest priority) -> denied
	decision, _ := checker.Check(context.Background(), "", models.ToolCall{Name: "tool_x"})
	if decision != ApprovalDenied {
		t.Errorf("tool_x: expected denied (denylist), got %v", decision)
	}

	// tool_y: not in denylist, in allowlist -> allowed
	decision, _ = checker.Check(context.Background(), "", models.ToolCall{Name: "tool_y"})
	if decision != ApprovalAllowed {
		t.Errorf("tool_y: expected allowed (allowlist), got %v", decision)
	}
}

func TestApprovalChecker_RequestTTL(t *testing.T) {
	store := NewMemoryApprovalStore()
	checker := NewApprovalChecker(&ApprovalPolicy{
		RequestTTL: 10 * time.Minute,
	})
	checker.SetStore(store)

	ctx := context.Background()
	toolCall := models.ToolCall{ID: "call-1", Name: "tool"}

	req, _ := checker.CreateApprovalRequest(ctx, "agent", "session", toolCall, "reason")

	// Verify expiration is set correctly
	expectedExpiry := time.Now().Add(10 * time.Minute)
	if req.ExpiresAt.Before(expectedExpiry.Add(-time.Second)) || req.ExpiresAt.After(expectedExpiry.Add(time.Second)) {
		t.Errorf("ExpiresAt not within expected range: %v", req.ExpiresAt)
	}
}

func TestApprovalChecker_ZeroTTL(t *testing.T) {
	store := NewMemoryApprovalStore()
	checker := NewApprovalChecker(&ApprovalPolicy{
		RequestTTL: 0, // Zero TTL should use default
	})
	checker.SetStore(store)

	ctx := context.Background()
	toolCall := models.ToolCall{ID: "call-1", Name: "tool"}

	req, _ := checker.CreateApprovalRequest(ctx, "agent", "session", toolCall, "reason")

	// Should use default 5 minute TTL
	expectedExpiry := time.Now().Add(5 * time.Minute)
	if req.ExpiresAt.Before(expectedExpiry.Add(-time.Second)) || req.ExpiresAt.After(expectedExpiry.Add(time.Second)) {
		t.Errorf("ExpiresAt not within expected range for zero TTL: %v", req.ExpiresAt)
	}
}
