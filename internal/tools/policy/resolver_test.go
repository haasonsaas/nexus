package policy

import "testing"

func TestResolverAllowsMCPAlias(t *testing.T) {
	resolver := NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})
	resolver.RegisterAlias("mcp_github_search", "mcp:github.search")

	policy := &Policy{Allow: []string{"mcp:github.search"}}
	if !resolver.IsAllowed(policy, "mcp_github_search") {
		t.Fatal("expected alias tool to be allowed")
	}
}

func TestResolverResetMCP(t *testing.T) {
	resolver := NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})
	resolver.RegisterAlias("mcp.github.search", "mcp:github.search")

	expanded := resolver.ExpandGroups([]string{"mcp:github.*"})
	if len(expanded) != 1 || expanded[0] != "mcp:github.search" {
		t.Fatalf("expected MCP tools before reset, got %v", expanded)
	}
	if got := resolver.CanonicalName("mcp.github.search"); got != "mcp:github.search" {
		t.Fatalf("expected alias to resolve, got %q", got)
	}

	resolver.ResetMCP()

	expanded = resolver.ExpandGroups([]string{"mcp:github.*"})
	if len(expanded) != 0 {
		t.Fatalf("expected MCP tools cleared after reset, got %v", expanded)
	}
	if got := resolver.CanonicalName("mcp.github.search"); got != "mcp.github.search" {
		t.Fatalf("expected alias to be cleared, got %q", got)
	}
}

func TestResolverAllowsMCPAliasViaWildcard(t *testing.T) {
	resolver := NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})
	resolver.RegisterAlias("mcp_github_search", "mcp:github.search")

	policy := &Policy{Allow: []string{"mcp:github.*"}}
	if !resolver.IsAllowed(policy, "mcp_github_search") {
		t.Fatal("expected alias tool to be allowed via wildcard")
	}
}

func TestResolverProviderOverrides(t *testing.T) {
	resolver := NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})

	policy := &Policy{
		Allow: []string{"read"},
		ByProvider: map[string]*Policy{
			"mcp:github": {
				Allow: []string{"mcp:github.search"},
			},
		},
	}

	if !resolver.IsAllowed(policy, "mcp:github.search") {
		t.Fatal("expected provider-specific allow to grant access")
	}
	if resolver.IsAllowed(policy, "mcp:github.other") {
		t.Fatal("expected non-allowed provider tool to be denied")
	}
	if resolver.IsAllowed(policy, "exec") {
		t.Fatal("expected base policy to deny exec by default")
	}
}

func TestResolverProviderDenyWins(t *testing.T) {
	resolver := NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})

	policy := &Policy{
		Allow: []string{"mcp:github.search"},
		ByProvider: map[string]*Policy{
			"mcp:github": {
				Deny: []string{"mcp:github.search"},
			},
		},
	}

	if resolver.IsAllowed(policy, "mcp:github.search") {
		t.Fatal("expected provider-specific deny to override allow")
	}
}

func TestResolverWildcardAllowAll(t *testing.T) {
	resolver := NewResolver()
	policy := &Policy{Allow: []string{"*"}}

	if !resolver.IsAllowed(policy, "web_search") {
		t.Fatal("expected wildcard to allow web_search")
	}
	if !resolver.IsAllowed(policy, "exec") {
		t.Fatal("expected wildcard to allow exec")
	}
}

func TestResolverWildcardDenyOverridesAllow(t *testing.T) {
	resolver := NewResolver()
	policy := &Policy{
		Allow: []string{"*"},
		Deny:  []string{"exec"},
	}

	if resolver.IsAllowed(policy, "exec") {
		t.Fatal("expected deny to override wildcard allow")
	}
}

func TestResolverWildcardPrefix(t *testing.T) {
	resolver := NewResolver()
	policy := &Policy{Allow: []string{"web_*"}}

	if !resolver.IsAllowed(policy, "web_search") {
		t.Fatal("expected wildcard prefix to allow web_search")
	}
	if !resolver.IsAllowed(policy, "web_fetch") {
		t.Fatal("expected wildcard prefix to allow web_fetch")
	}
	if resolver.IsAllowed(policy, "memory_search") {
		t.Fatal("expected wildcard prefix not to allow memory_search")
	}
}

func TestResolverWildcardPrefixWithAlias(t *testing.T) {
	resolver := NewResolver()
	policy := &Policy{Allow: []string{"web_*"}}

	if !resolver.IsAllowed(policy, "websearch") {
		t.Fatal("expected wildcard prefix to allow websearch alias")
	}
}
