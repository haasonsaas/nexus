package gateway

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	policyPkg "github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestToolPolicyForAgentMergesConfigAndTools(t *testing.T) {
	agent := &models.Agent{
		ID:    "agent-1",
		Tools: []string{"read"},
		Config: map[string]any{
			"tool_policy": map[string]any{
				"allow": []any{"websearch"},
				"deny":  []any{"exec"},
			},
		},
	}

	toolPolicy := toolPolicyFromAgent(agent)
	if toolPolicy == nil {
		t.Fatal("expected tool policy")
	}
	resolver := policyPkg.NewResolver()
	if !resolver.IsAllowed(toolPolicy, "read") {
		t.Fatal("expected agent tools to be allowed")
	}
	if !resolver.IsAllowed(toolPolicy, "websearch") {
		t.Fatal("expected config allow to be allowed")
	}
	if resolver.IsAllowed(toolPolicy, "exec") {
		t.Fatal("expected config deny to be enforced")
	}
}

func TestToolPolicyForAgentProviderOverrides(t *testing.T) {
	agent := &models.Agent{
		ID: "agent-1",
		Config: map[string]any{
			"tool_policy": map[string]any{
				"by_provider": map[string]any{
					"mcp:github": map[string]any{
						"allow": []any{"mcp:github.search"},
					},
				},
			},
		},
	}

	toolPolicy := toolPolicyFromAgent(agent)
	if toolPolicy == nil {
		t.Fatal("expected tool policy")
	}

	resolver := policyPkg.NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})
	if !resolver.IsAllowed(toolPolicy, "mcp:github.search") {
		t.Fatal("expected provider-specific allow to be honored")
	}
	if resolver.IsAllowed(toolPolicy, "mcp:github.other") {
		t.Fatal("expected non-allowed provider tool to be denied")
	}
}

func TestToolPolicyFromConfigChannelRules(t *testing.T) {
	cfg := config.ToolPoliciesConfig{
		Default: "allow",
		Rules: []config.ToolPolicyRule{
			{Tool: "browser", Action: "deny", Channels: []string{"telegram"}},
		},
	}

	msg := &models.Message{Channel: models.ChannelTelegram}
	policy := toolPolicyFromConfig(cfg, msg, "")
	if policy == nil {
		t.Fatal("expected policy for matching channel")
	}
	resolver := policyPkg.NewResolver()
	if resolver.IsAllowed(policy, "browser") {
		t.Fatal("expected browser to be denied for telegram")
	}

	other := &models.Message{Channel: models.ChannelSlack}
	if toolPolicyFromConfig(cfg, other, "") != nil {
		t.Fatal("expected no policy for non-matching channel")
	}
}

func TestToolPolicyFromConfigDefaultDeny(t *testing.T) {
	cfg := config.ToolPoliciesConfig{
		Default: "deny",
		Rules: []config.ToolPolicyRule{
			{Tool: "read", Action: "allow"},
		},
	}

	msg := &models.Message{Channel: models.ChannelAPI}
	policy := toolPolicyFromConfig(cfg, msg, "")
	if policy == nil {
		t.Fatal("expected policy for default deny")
	}
	resolver := policyPkg.NewResolver()
	if !resolver.IsAllowed(policy, "read") {
		t.Fatal("expected read to be allowed")
	}
	if resolver.IsAllowed(policy, "write") {
		t.Fatal("expected write to be denied")
	}
}
