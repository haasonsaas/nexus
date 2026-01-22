package policy

import (
	"slices"
	"testing"
)

func TestExpandGroups(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		contains []string // tools that should be present
		excludes []string // tools that should NOT be present
	}{
		{
			name:     "expand single group",
			input:    []string{"group:fs"},
			contains: []string{"read", "write", "edit", "apply_patch"},
		},
		{
			name:     "expand runtime group",
			input:    []string{"group:runtime"},
			contains: []string{"exec", "bash", "sandbox"},
		},
		{
			name:     "expand multiple groups",
			input:    []string{"group:fs", "group:web"},
			contains: []string{"read", "write", "edit", "websearch", "webfetch"},
		},
		{
			name:     "pass through direct tool names",
			input:    []string{"custom_tool", "another_tool"},
			contains: []string{"custom_tool", "another_tool"},
		},
		{
			name:     "mix of groups and tools",
			input:    []string{"group:messaging", "custom_tool"},
			contains: []string{"message", "send_message", "custom_tool"},
		},
		{
			name:     "deduplicate results",
			input:    []string{"group:fs", "read", "write"},
			contains: []string{"read", "write", "edit", "apply_patch"},
		},
		{
			name:     "empty input",
			input:    []string{},
			contains: []string{},
		},
		{
			name:     "unknown group passed through",
			input:    []string{"group:unknown"},
			contains: []string{"group:unknown"},
		},
		{
			name:     "readonly group",
			input:    []string{"group:readonly"},
			contains: []string{"read", "websearch", "memory_search", "job_status"},
			excludes: []string{"write", "edit", "exec"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandGroups(tt.input)

			for _, expected := range tt.contains {
				if !slices.Contains(result, expected) {
					t.Errorf("expected %q to be in result %v", expected, result)
				}
			}

			for _, excluded := range tt.excludes {
				if slices.Contains(result, excluded) {
					t.Errorf("expected %q to NOT be in result %v", excluded, result)
				}
			}
		})
	}
}

func TestExpandGroupsDeduplication(t *testing.T) {
	// Test that duplicate tools are removed
	input := []string{"group:fs", "read", "group:fs"}
	result := ExpandGroups(input)

	// Count occurrences of "read"
	count := 0
	for _, tool := range result {
		if tool == "read" {
			count++
		}
	}

	if count != 1 {
		t.Errorf("expected 'read' to appear exactly once, got %d times in %v", count, result)
	}
}

func TestGetProfilePolicy(t *testing.T) {
	tests := []struct {
		name        string
		profile     string
		expectNil   bool
		expectAllow []string
	}{
		{
			name:        "coding profile",
			profile:     "coding",
			expectNil:   false,
			expectAllow: []string{"group:fs", "group:runtime"},
		},
		{
			name:        "messaging profile",
			profile:     "messaging",
			expectNil:   false,
			expectAllow: []string{"group:messaging"},
		},
		{
			name:        "readonly profile",
			profile:     "readonly",
			expectNil:   false,
			expectAllow: []string{"group:readonly"},
		},
		{
			name:        "full profile",
			profile:     "full",
			expectNil:   false,
			expectAllow: nil, // full profile has no explicit allows
		},
		{
			name:      "unknown profile",
			profile:   "nonexistent",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := GetProfilePolicy(tt.profile)

			if tt.expectNil {
				if policy != nil {
					t.Errorf("expected nil policy for profile %q", tt.profile)
				}
				return
			}

			if policy == nil {
				t.Fatalf("expected non-nil policy for profile %q", tt.profile)
			}

			for _, expected := range tt.expectAllow {
				if !slices.Contains(policy.Allow, expected) {
					t.Errorf("expected %q in allow list for profile %q, got %v", expected, tt.profile, policy.Allow)
				}
			}
		})
	}
}

func TestIsGroup(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid fs group", "group:fs", true},
		{"valid runtime group", "group:runtime", true},
		{"valid memory group", "group:memory", true},
		{"valid messaging group", "group:messaging", true},
		{"valid readonly group", "group:readonly", true},
		{"invalid group", "group:unknown", false},
		{"regular tool name", "read", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsGroup(tt.input)
			if result != tt.expected {
				t.Errorf("IsGroup(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetGroupTools(t *testing.T) {
	tests := []struct {
		name       string
		group      string
		expectNil  bool
		expectLen  int
		expectTool string
	}{
		{
			name:       "get fs tools",
			group:      "group:fs",
			expectNil:  false,
			expectLen:  4,
			expectTool: "read",
		},
		{
			name:       "get messaging tools",
			group:      "group:messaging",
			expectNil:  false,
			expectLen:  2,
			expectTool: "message",
		},
		{
			name:      "unknown group",
			group:     "group:nonexistent",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetGroupTools(tt.group)

			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil for group %q", tt.group)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected non-nil result for group %q", tt.group)
			}

			if len(result) != tt.expectLen {
				t.Errorf("expected %d tools, got %d: %v", tt.expectLen, len(result), result)
			}

			if !slices.Contains(result, tt.expectTool) {
				t.Errorf("expected tool %q in result %v", tt.expectTool, result)
			}
		})
	}
}

func TestGetGroupToolsReturnsCopy(t *testing.T) {
	// Verify that modifying the returned slice doesn't affect the original
	original := GetGroupTools("group:fs")
	if original == nil {
		t.Fatal("expected non-nil result for group:fs")
	}

	// Modify the copy
	original[0] = "modified"

	// Get again and verify original is unchanged
	fresh := GetGroupTools("group:fs")
	if fresh[0] == "modified" {
		t.Error("GetGroupTools should return a copy, not the original slice")
	}
}

func TestListGroups(t *testing.T) {
	groups := ListGroups()

	// Should have at least the core groups
	expectedGroups := []string{
		"group:fs",
		"group:runtime",
		"group:memory",
		"group:messaging",
		"group:web",
		"group:readonly",
	}

	for _, expected := range expectedGroups {
		if !slices.Contains(groups, expected) {
			t.Errorf("expected %q in group list %v", expected, groups)
		}
	}
}

func TestListProfiles(t *testing.T) {
	profiles := ListProfiles()

	expectedProfiles := []string{
		"coding",
		"messaging",
		"readonly",
		"full",
		"minimal",
	}

	for _, expected := range expectedProfiles {
		if !slices.Contains(profiles, expected) {
			t.Errorf("expected %q in profile list %v", expected, profiles)
		}
	}
}

func TestResolverWithGroups(t *testing.T) {
	resolver := NewResolver()

	// Test that resolver's ExpandGroups works with our tool groups
	policy := &Policy{
		Allow: []string{"group:fs", "websearch"},
	}

	// These should be allowed
	allowedTools := []string{"read", "write", "edit", "apply_patch", "websearch"}
	for _, tool := range allowedTools {
		if !resolver.IsAllowed(policy, tool) {
			t.Errorf("expected %q to be allowed", tool)
		}
	}

	// These should NOT be allowed
	deniedTools := []string{"exec", "sandbox", "browser", "message"}
	for _, tool := range deniedTools {
		if resolver.IsAllowed(policy, tool) {
			t.Errorf("expected %q to be denied", tool)
		}
	}
}

func TestResolverWithProfile(t *testing.T) {
	resolver := NewResolver()

	// Test coding profile
	policy := &Policy{
		Profile: ProfileCoding,
	}

	// Coding profile should allow fs and runtime tools
	allowedTools := []string{"read", "write", "exec", "sandbox"}
	for _, tool := range allowedTools {
		if !resolver.IsAllowed(policy, tool) {
			t.Errorf("coding profile: expected %q to be allowed", tool)
		}
	}
}

func TestResolverWithProfileAndDeny(t *testing.T) {
	resolver := NewResolver()

	// Test that deny overrides profile
	policy := &Policy{
		Profile: ProfileFull,
		Deny:    []string{"exec"},
	}

	if resolver.IsAllowed(policy, "exec") {
		t.Error("expected exec to be denied even with full profile")
	}

	// Other tools should still work
	if !resolver.IsAllowed(policy, "read") {
		t.Error("expected read to be allowed with full profile")
	}
}

func TestResolverWithGroupDeny(t *testing.T) {
	resolver := NewResolver()

	// Test denying an entire group
	policy := &Policy{
		Profile: ProfileFull,
		Deny:    []string{"group:runtime"},
	}

	// Runtime tools should be denied
	deniedTools := []string{"exec", "bash", "sandbox"}
	for _, tool := range deniedTools {
		if resolver.IsAllowed(policy, tool) {
			t.Errorf("expected %q to be denied by group:runtime deny", tool)
		}
	}

	// Non-runtime tools should still work
	if !resolver.IsAllowed(policy, "read") {
		t.Error("expected read to be allowed")
	}
}

func TestToolGroupsConsistency(t *testing.T) {
	// Verify that group:nexus contains all tools from other groups
	nexusTools := GetGroupTools("group:nexus")
	if nexusTools == nil {
		t.Fatal("group:nexus should exist")
	}

	groupsToCheck := []string{"group:fs", "group:runtime", "group:web", "group:messaging"}

	for _, group := range groupsToCheck {
		tools := GetGroupTools(group)
		for _, tool := range tools {
			if !slices.Contains(nexusTools, tool) {
				t.Errorf("group:nexus should contain %q from %s", tool, group)
			}
		}
	}
}

func TestReadonlyGroupNoModifyTools(t *testing.T) {
	// Verify readonly group doesn't include modification tools
	readonlyTools := GetGroupTools("group:readonly")
	if readonlyTools == nil {
		t.Fatal("group:readonly should exist")
	}

	modifyTools := []string{"write", "edit", "exec", "bash", "sandbox", "apply_patch", "message", "send_message"}

	for _, tool := range modifyTools {
		if slices.Contains(readonlyTools, tool) {
			t.Errorf("group:readonly should NOT contain modification tool %q", tool)
		}
	}

	// Should include read tools
	readTools := []string{"read", "websearch", "memory_search"}
	for _, tool := range readTools {
		if !slices.Contains(readonlyTools, tool) {
			t.Errorf("group:readonly should contain read tool %q", tool)
		}
	}
}
