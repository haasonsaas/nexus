package skills

import (
	"testing"
)

// mockToolPolicyChecker implements ToolPolicyChecker for testing.
type mockToolPolicyChecker struct {
	allowedGroups map[string]bool
	edgeConnected bool
}

func (m *mockToolPolicyChecker) IsGroupAllowed(group string) bool {
	return m.allowedGroups[group]
}

func (m *mockToolPolicyChecker) HasEdgeConnected() bool {
	return m.edgeConnected
}

func TestSkillEntry_ExecutionLocation(t *testing.T) {
	tests := []struct {
		name     string
		skill    SkillEntry
		expected ExecutionLocation
	}{
		{
			name:     "default is any",
			skill:    SkillEntry{},
			expected: ExecAny,
		},
		{
			name: "explicit core",
			skill: SkillEntry{
				Metadata: &SkillMetadata{Execution: ExecCore},
			},
			expected: ExecCore,
		},
		{
			name: "explicit edge",
			skill: SkillEntry{
				Metadata: &SkillMetadata{Execution: ExecEdge},
			},
			expected: ExecEdge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.skill.ExecutionLocation()
			if got != tt.expected {
				t.Errorf("ExecutionLocation() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSkillEntry_RequiresEdge(t *testing.T) {
	tests := []struct {
		name     string
		skill    SkillEntry
		expected bool
	}{
		{
			name:     "no metadata",
			skill:    SkillEntry{},
			expected: false,
		},
		{
			name: "core execution",
			skill: SkillEntry{
				Metadata: &SkillMetadata{Execution: ExecCore},
			},
			expected: false,
		},
		{
			name: "edge execution",
			skill: SkillEntry{
				Metadata: &SkillMetadata{Execution: ExecEdge},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.skill.RequiresEdge()
			if got != tt.expected {
				t.Errorf("RequiresEdge() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSkillEntry_RequiredToolGroups(t *testing.T) {
	tests := []struct {
		name     string
		skill    SkillEntry
		expected []string
	}{
		{
			name:     "no metadata",
			skill:    SkillEntry{},
			expected: nil,
		},
		{
			name: "with tool groups",
			skill: SkillEntry{
				Metadata: &SkillMetadata{
					ToolGroups: []string{"group:web", "group:fs"},
				},
			},
			expected: []string{"group:web", "group:fs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.skill.RequiredToolGroups()
			if len(got) != len(tt.expected) {
				t.Errorf("RequiredToolGroups() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCheckEligibility_ToolGroups(t *testing.T) {
	skill := &SkillEntry{
		Name: "test-skill",
		Metadata: &SkillMetadata{
			ToolGroups: []string{"group:web"},
		},
	}

	tests := []struct {
		name           string
		allowedGroups  map[string]bool
		wantEligible   bool
		wantReasonPart string
	}{
		{
			name:          "allowed",
			allowedGroups: map[string]bool{"group:web": true},
			wantEligible:  true,
		},
		{
			name:           "not allowed",
			allowedGroups:  map[string]bool{"group:fs": true},
			wantEligible:   false,
			wantReasonPart: "tool group not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewGatingContext(nil, nil)
			ctx.ToolPolicy = &mockToolPolicyChecker{
				allowedGroups: tt.allowedGroups,
				edgeConnected: true,
			}

			result := skill.CheckEligibility(ctx)
			if result.Eligible != tt.wantEligible {
				t.Errorf("CheckEligibility().Eligible = %v, want %v", result.Eligible, tt.wantEligible)
			}
			if !tt.wantEligible && tt.wantReasonPart != "" {
				if !contains(result.Reason, tt.wantReasonPart) {
					t.Errorf("CheckEligibility().Reason = %q, want to contain %q", result.Reason, tt.wantReasonPart)
				}
			}
		})
	}
}

func TestCheckEligibility_EdgeExecution(t *testing.T) {
	skill := &SkillEntry{
		Name: "edge-skill",
		Metadata: &SkillMetadata{
			Execution: ExecEdge,
		},
	}

	tests := []struct {
		name           string
		edgeConnected  bool
		wantEligible   bool
		wantReasonPart string
	}{
		{
			name:          "edge connected",
			edgeConnected: true,
			wantEligible:  true,
		},
		{
			name:           "edge not connected",
			edgeConnected:  false,
			wantEligible:   false,
			wantReasonPart: "requires edge daemon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewGatingContext(nil, nil)
			ctx.ToolPolicy = &mockToolPolicyChecker{
				allowedGroups: map[string]bool{},
				edgeConnected: tt.edgeConnected,
			}

			result := skill.CheckEligibility(ctx)
			if result.Eligible != tt.wantEligible {
				t.Errorf("CheckEligibility().Eligible = %v, want %v", result.Eligible, tt.wantEligible)
			}
			if !tt.wantEligible && tt.wantReasonPart != "" {
				if !contains(result.Reason, tt.wantReasonPart) {
					t.Errorf("CheckEligibility().Reason = %q, want to contain %q", result.Reason, tt.wantReasonPart)
				}
			}
		})
	}
}

func TestCheckEligibility_NoToolPolicyChecker(t *testing.T) {
	// Skill with tool group requirements should be eligible
	// when no tool policy checker is available (backward compatibility)
	skill := &SkillEntry{
		Name: "test-skill",
		Metadata: &SkillMetadata{
			ToolGroups: []string{"group:web"},
			Execution:  ExecEdge,
		},
	}

	ctx := NewGatingContext(nil, nil)
	// No ToolPolicy set

	result := skill.CheckEligibility(ctx)
	if !result.Eligible {
		t.Errorf("CheckEligibility() should be eligible without ToolPolicy checker, got reason: %s", result.Reason)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
