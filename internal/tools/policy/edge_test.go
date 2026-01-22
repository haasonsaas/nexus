package policy

import (
	"testing"
)

func TestResolverEdgePattern(t *testing.T) {
	r := NewResolver()

	// Register edge tools
	r.RegisterEdgeServer("phone", []string{"camera", "location", "contacts"})

	tests := []struct {
		name    string
		policy  *Policy
		tool    string
		allowed bool
		reason  string
	}{
		{
			name:    "edge tool allowed by wildcard",
			policy:  NewPolicy(ProfileMinimal).WithAllow("edge:phone.*"),
			tool:    "edge:phone.camera",
			allowed: true,
			reason:  "allowed by rule: edge:phone.camera", // Expanded from wildcard
		},
		{
			name:    "edge tool allowed by exact match",
			policy:  NewPolicy(ProfileMinimal).WithAllow("edge:phone.camera"),
			tool:    "edge:phone.camera",
			allowed: true,
			reason:  "allowed by rule: edge:phone.camera",
		},
		{
			name:    "edge tool denied by wildcard",
			policy:  NewPolicy(ProfileFull).WithDeny("edge:*"),
			tool:    "edge:phone.camera",
			allowed: false,
			reason:  "denied by rule: edge:*",
		},
		{
			name:    "edge tool denied by server wildcard",
			policy:  NewPolicy(ProfileFull).WithDeny("edge:phone.*"),
			tool:    "edge:phone.location",
			allowed: false,
			reason:  "denied by rule: edge:phone.location", // Expanded from wildcard
		},
		{
			name:    "edge tool not allowed when not in allow list",
			policy:  NewPolicy(ProfileMinimal),
			tool:    "edge:phone.camera",
			allowed: false,
			reason:  "no matching allow rule",
		},
		{
			name:    "edge tool allowed by full profile",
			policy:  NewPolicy(ProfileFull),
			tool:    "edge:phone.camera",
			allowed: true,
			reason:  "allowed by profile full",
		},
		{
			name:    "all edge tools allowed",
			policy:  NewPolicy(ProfileMinimal).WithAllow("edge:*"),
			tool:    "edge:phone.contacts",
			allowed: true,
			reason:  "allowed by rule: edge:*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := r.Decide(tt.policy, tt.tool)
			if decision.Allowed != tt.allowed {
				t.Errorf("expected allowed=%v, got %v (reason: %s)", tt.allowed, decision.Allowed, decision.Reason)
			}
			if decision.Reason != tt.reason {
				t.Errorf("expected reason %q, got %q", tt.reason, decision.Reason)
			}
		})
	}
}

func TestResolverExpandEdgeGroups(t *testing.T) {
	r := NewResolver()

	// Register edge server
	r.RegisterEdgeServer("laptop", []string{"screen_capture", "clipboard", "keylogger"})

	// Test wildcard expansion
	expanded := r.ExpandGroups([]string{"edge:laptop.*"})
	if len(expanded) != 3 {
		t.Errorf("expected 3 tools, got %d: %v", len(expanded), expanded)
	}

	// Verify canonical names
	expected := map[string]bool{
		"edge:laptop.screen_capture": true,
		"edge:laptop.clipboard":      true,
		"edge:laptop.keylogger":      true,
	}
	for _, tool := range expanded {
		if !expected[tool] {
			t.Errorf("unexpected tool in expansion: %s", tool)
		}
	}
}

func TestResolverEdgeProviderKey(t *testing.T) {
	tests := []struct {
		tool     string
		expected string
	}{
		{"edge:phone.camera", "edge:phone"},
		{"edge:laptop.clipboard", "edge:laptop"},
		{"edge:", "edge"},
		{"mcp:fs.read", "mcp:fs"},
		{"browser", "nexus"},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			got := toolProviderKey(tt.tool)
			if got != tt.expected {
				t.Errorf("toolProviderKey(%s) = %s, want %s", tt.tool, got, tt.expected)
			}
		})
	}
}

func TestMatchToolPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		tool     string
		expected bool
	}{
		// Universal wildcard
		{"*", "anything", true},
		{"*", "mcp:fs.read", true},
		{"*", "edge:phone.camera", true},

		// Source wildcards
		{"mcp:*", "mcp:fs.read", true},
		{"mcp:*", "edge:phone.camera", false},
		{"edge:*", "edge:phone.camera", true},
		{"edge:*", "mcp:fs.read", false},
		{"core.*", "core.browser", true},
		{"core.*", "browser", true}, // Unqualified = core
		{"core.*", "mcp:fs.read", false},

		// Namespace wildcards
		{"mcp:fs.*", "mcp:fs.read", true},
		{"mcp:fs.*", "mcp:fs.write", true},
		{"mcp:fs.*", "mcp:git.commit", false},
		{"edge:phone.*", "edge:phone.camera", true},
		{"edge:phone.*", "edge:laptop.camera", false},

		// Exact matches
		{"mcp:fs.read", "mcp:fs.read", true},
		{"mcp:fs.read", "mcp:fs.write", false},
		{"edge:phone.camera", "edge:phone.camera", true},
		{"edge:phone.camera", "edge:phone.location", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.tool, func(t *testing.T) {
			if got := matchToolPattern(tt.pattern, tt.tool); got != tt.expected {
				t.Errorf("matchToolPattern(%s, %s) = %v, want %v", tt.pattern, tt.tool, got, tt.expected)
			}
		})
	}
}

func TestPolicyBuilderEdge(t *testing.T) {
	// Test that policy can be used with edge tools
	policy := NewPolicy(ProfileMinimal).
		WithAllow("mcp:filesystem.*", "browser", "edge:phone.*")

	r := NewResolver()
	r.RegisterEdgeServer("phone", []string{"camera"})

	if !r.IsAllowed(policy, "edge:phone.camera") {
		t.Error("expected edge tool to be allowed")
	}
}

func TestResolverUnregisterEdge(t *testing.T) {
	r := NewResolver()

	// Register
	r.RegisterEdgeServer("device", []string{"tool1", "tool2"})

	// Verify group exists
	if _, ok := r.groups["edge:device"]; !ok {
		t.Error("expected edge group to exist")
	}

	// Unregister
	r.UnregisterEdgeServer("device")

	// Verify group is gone
	if _, ok := r.groups["edge:device"]; ok {
		t.Error("expected edge group to be removed")
	}
}
