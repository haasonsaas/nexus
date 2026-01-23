package testharness_test

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/testharness"
	"github.com/haasonsaas/nexus/internal/tools/policy"
)

// TestToolPolicy_DenialMessage_ExplicitDeny verifies denial messages for explicitly denied tools.
func TestToolPolicy_DenialMessage_ExplicitDeny(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileCoding,
		Deny:    []string{"exec"},
	}

	result := resolver.Decide(pol, "exec")

	if result.Allowed {
		t.Fatal("expected tool to be denied")
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/policy")
	g.AssertNamed("denial", result.Reason)
}

// TestToolPolicy_DenialMessage_NotInProfile verifies denial messages for tools not in profile.
func TestToolPolicy_DenialMessage_NotInProfile(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileMinimal, // Only allows "status"
	}

	result := resolver.Decide(pol, "exec")

	if result.Allowed {
		t.Fatal("expected tool to be denied")
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/policy")
	g.AssertNamed("not_in_profile", result.Reason)
}

// TestToolPolicy_DenialMessage_MCPDenied verifies denial messages for MCP tools.
func TestToolPolicy_DenialMessage_MCPDenied(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileCoding,
		Deny:    []string{"mcp:github.*"},
	}

	result := resolver.Decide(pol, "mcp:github.create_issue")

	if result.Allowed {
		t.Fatal("expected MCP tool to be denied")
	}

	g := testharness.NewGoldenAt(t, "testdata/golden/policy")
	g.AssertNamed("mcp_denied", result.Reason)
}

// TestToolPolicy_ProfileMinimal verifies minimal profile behavior.
func TestToolPolicy_ProfileMinimal(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileMinimal,
	}

	tests := []struct {
		tool    string
		allowed bool
	}{
		{"status", true},
		{"exec", false},
		{"read", false},
		{"write", false},
		{"web_search", false},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := resolver.Decide(pol, tt.tool)
			if result.Allowed != tt.allowed {
				t.Errorf("Decide(%q) = %v, want %v", tt.tool, result.Allowed, tt.allowed)
			}
		})
	}
}

// TestToolPolicy_ProfileCoding verifies coding profile behavior.
func TestToolPolicy_ProfileCoding(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileCoding,
	}

	tests := []struct {
		tool    string
		allowed bool
	}{
		{"read", true},
		{"write", true},
		{"edit", true},
		{"exec", true},
		{"web_search", true},
		{"web_fetch", true},
		{"memory_search", true},
		{"send_message", false}, // messaging tool not in coding profile
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := resolver.Decide(pol, tt.tool)
			if result.Allowed != tt.allowed {
				t.Errorf("Decide(%q) = %v, want %v (reason: %s)", tt.tool, result.Allowed, tt.allowed, result.Reason)
			}
		})
	}
}

// TestToolPolicy_ProfileMessaging verifies messaging profile behavior.
func TestToolPolicy_ProfileMessaging(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileMessaging,
	}

	tests := []struct {
		tool    string
		allowed bool
	}{
		{"send_message", true},
		{"status", true},
		{"exec", false},
		{"read", false},
		{"write", false},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := resolver.Decide(pol, tt.tool)
			if result.Allowed != tt.allowed {
				t.Errorf("Decide(%q) = %v, want %v (reason: %s)", tt.tool, result.Allowed, tt.allowed, result.Reason)
			}
		})
	}
}

// TestToolPolicy_ProfileFull verifies full access profile.
func TestToolPolicy_ProfileFull(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileFull,
	}

	tools := []string{"read", "write", "edit", "exec", "web_search", "send_message", "status"}

	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			result := resolver.Decide(pol, tool)
			if !result.Allowed {
				t.Errorf("Decide(%q) should be allowed in full profile, got denied: %s", tool, result.Reason)
			}
		})
	}
}

// TestToolPolicy_ExplicitAllowOverridesProfile verifies allow list extends profile.
func TestToolPolicy_ExplicitAllowOverridesProfile(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileMinimal,
		Allow:   []string{"exec"}, // Explicitly allow exec
	}

	result := resolver.Decide(pol, "exec")

	if !result.Allowed {
		t.Errorf("expected exec to be allowed via explicit allow list, got denied: %s", result.Reason)
	}
}

// TestToolPolicy_DenyOverridesAllow verifies deny takes precedence.
func TestToolPolicy_DenyOverridesAllow(t *testing.T) {
	resolver := policy.NewResolver()

	pol := &policy.Policy{
		Profile: policy.ProfileFull,
		Deny:    []string{"exec"}, // Explicitly deny exec even in full profile
	}

	result := resolver.Decide(pol, "exec")

	if result.Allowed {
		t.Error("expected exec to be denied despite full profile")
	}
}

// TestToolPolicy_ToolAliases verifies tool alias resolution.
func TestToolPolicy_ToolAliases(t *testing.T) {
	tests := []struct {
		alias     string
		canonical string
	}{
		{"bash", "exec"},
		{"shell", "exec"},
		{"apply-patch", "edit"},
		{"apply_patch", "edit"},
		{"websearch", "web_search"},
		{"webfetch", "web_fetch"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			normalized := policy.NormalizeTool(tt.alias)
			if normalized != tt.canonical {
				t.Errorf("NormalizeTool(%q) = %q, want %q", tt.alias, normalized, tt.canonical)
			}
		})
	}
}

// TestToolPolicy_MCPToolParsing verifies MCP tool name parsing.
func TestToolPolicy_MCPToolParsing(t *testing.T) {
	tests := []struct {
		toolName string
		serverID string
		tool     string
	}{
		{"mcp:github.create_issue", "github", "create_issue"},
		{"mcp:slack.post_message", "slack", "post_message"},
		{"mcp.filesystem.read_file", "filesystem", "read_file"},
		{"not_mcp_tool", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			serverID, tool := policy.ParseMCPToolName(tt.toolName)
			if serverID != tt.serverID || tool != tt.tool {
				t.Errorf("ParseMCPToolName(%q) = (%q, %q), want (%q, %q)",
					tt.toolName, serverID, tool, tt.serverID, tt.tool)
			}
		})
	}
}

// TestToolPolicy_UnifiedPolicyBuilder verifies the fluent policy builder.
func TestToolPolicy_UnifiedPolicyBuilder(t *testing.T) {
	pol := policy.NewUnifiedPolicy().
		WithProfile(policy.ProfileCoding).
		AllowMCPServer("github").
		DenyMCPTool("github", "delete_repo").
		AllowNative("send_message").
		DenyNative("exec").
		Build()

	resolver := policy.NewResolver()
	// Register the github MCP server with its tools so the resolver can expand wildcards
	resolver.RegisterMCPServer("github", []string{"create_issue", "list_repos", "delete_repo"})

	tests := []struct {
		tool    string
		allowed bool
	}{
		{"read", true},                       // from coding profile
		{"mcp:github.create_issue", true},    // allowed MCP server
		{"mcp:github.delete_repo", false},    // explicitly denied
		{"send_message", true},               // explicitly allowed
		{"exec", false},                      // explicitly denied
		{"mcp:unknown.tool", false},          // not in allowed list
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := resolver.Decide(pol, tt.tool)
			if result.Allowed != tt.allowed {
				t.Errorf("Decide(%q) = %v, want %v (reason: %s)",
					tt.tool, result.Allowed, tt.allowed, result.Reason)
			}
		})
	}
}
