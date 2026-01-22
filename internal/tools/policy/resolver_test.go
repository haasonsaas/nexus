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
