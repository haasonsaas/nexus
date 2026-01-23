package gateway

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
)

func TestParseApprovalDecision(t *testing.T) {
	tests := []struct {
		value            string
		expectedDecision agent.ApprovalDecision
		expectedOK       bool
	}{
		{"", "", false},
		{"allow", agent.ApprovalAllowed, true},
		{"Allow", agent.ApprovalAllowed, true},
		{"ALLOW", agent.ApprovalAllowed, true},
		{"allowed", agent.ApprovalAllowed, true},
		{"  allow  ", agent.ApprovalAllowed, true},
		{"deny", agent.ApprovalDenied, true},
		{"Deny", agent.ApprovalDenied, true},
		{"denied", agent.ApprovalDenied, true},
		{"pending", agent.ApprovalPending, true},
		{"ask", agent.ApprovalPending, true},
		{"invalid", "", false},
		{"reject", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			decision, ok := parseApprovalDecision(tt.value)
			if ok != tt.expectedOK {
				t.Errorf("parseApprovalDecision(%q) ok = %v, want %v", tt.value, ok, tt.expectedOK)
			}
			if decision != tt.expectedDecision {
				t.Errorf("parseApprovalDecision(%q) = %v, want %v", tt.value, decision, tt.expectedDecision)
			}
		})
	}
}

func TestCloneApprovalPolicy(t *testing.T) {
	t.Run("nil policy returns nil", func(t *testing.T) {
		result := cloneApprovalPolicy(nil)
		if result != nil {
			t.Error("cloneApprovalPolicy(nil) should return nil")
		}
	})

	t.Run("clones all slices", func(t *testing.T) {
		original := &agent.ApprovalPolicy{
			DefaultDecision: agent.ApprovalPending,
			Allowlist:       []string{"tool1", "tool2"},
			Denylist:        []string{"dangerous"},
			RequireApproval: []string{"bash"},
			SafeBins:        []string{"ls", "cat"},
			SkillAllowlist:  true,
			AskFallback:     false,
		}

		cloned := cloneApprovalPolicy(original)

		// Verify values are equal
		if cloned.DefaultDecision != original.DefaultDecision {
			t.Errorf("DefaultDecision = %v, want %v", cloned.DefaultDecision, original.DefaultDecision)
		}
		if cloned.SkillAllowlist != original.SkillAllowlist {
			t.Errorf("SkillAllowlist = %v, want %v", cloned.SkillAllowlist, original.SkillAllowlist)
		}
		if cloned.AskFallback != original.AskFallback {
			t.Errorf("AskFallback = %v, want %v", cloned.AskFallback, original.AskFallback)
		}

		// Verify slices are copied (not same reference)
		if len(cloned.Allowlist) != len(original.Allowlist) {
			t.Errorf("Allowlist length = %d, want %d", len(cloned.Allowlist), len(original.Allowlist))
		}
		if len(cloned.Denylist) != len(original.Denylist) {
			t.Errorf("Denylist length = %d, want %d", len(cloned.Denylist), len(original.Denylist))
		}
		if len(cloned.RequireApproval) != len(original.RequireApproval) {
			t.Errorf("RequireApproval length = %d, want %d", len(cloned.RequireApproval), len(original.RequireApproval))
		}
		if len(cloned.SafeBins) != len(original.SafeBins) {
			t.Errorf("SafeBins length = %d, want %d", len(cloned.SafeBins), len(original.SafeBins))
		}

		// Modify original and verify clone is unchanged
		original.Allowlist[0] = "modified"
		if cloned.Allowlist[0] == "modified" {
			t.Error("cloned.Allowlist should be independent of original")
		}
	})

	t.Run("handles empty slices", func(t *testing.T) {
		original := &agent.ApprovalPolicy{
			DefaultDecision: agent.ApprovalAllowed,
			Allowlist:       []string{},
			Denylist:        nil,
		}

		cloned := cloneApprovalPolicy(original)

		if cloned.DefaultDecision != original.DefaultDecision {
			t.Errorf("DefaultDecision = %v, want %v", cloned.DefaultDecision, original.DefaultDecision)
		}
		// Empty slice appends result in nil, which is acceptable
		if len(cloned.Allowlist) != 0 {
			t.Errorf("Allowlist should be empty, got %v", cloned.Allowlist)
		}
	})
}

func TestExpandApprovalPatterns(t *testing.T) {
	t.Run("empty items returns nil", func(t *testing.T) {
		result := expandApprovalPatterns(nil, nil)
		if result != nil {
			t.Errorf("expandApprovalPatterns(nil, nil) = %v, want nil", result)
		}

		result = expandApprovalPatterns([]string{}, nil)
		if result != nil {
			t.Errorf("expandApprovalPatterns(empty, nil) = %v, want nil", result)
		}
	})

	t.Run("without resolver uses default expansion", func(t *testing.T) {
		items := []string{"tool1", "tool2"}
		result := expandApprovalPatterns(items, nil)
		// Without a resolver, should use policy.ExpandGroups
		if len(result) == 0 {
			t.Error("expandApprovalPatterns should return expanded items")
		}
	})
}
