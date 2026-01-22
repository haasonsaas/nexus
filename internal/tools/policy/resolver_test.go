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
