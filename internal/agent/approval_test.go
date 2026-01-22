package agent

import (
	"context"
	"testing"

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
