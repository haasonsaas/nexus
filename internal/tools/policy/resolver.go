package policy

import (
	"strings"
)

// Resolver resolves tool access based on policies.
type Resolver struct {
	groups     map[string][]string
	mcpServers map[string][]string // serverID -> tool names
}

// NewResolver creates a new policy resolver.
func NewResolver() *Resolver {
	return &Resolver{
		groups:     DefaultGroups,
		mcpServers: make(map[string][]string),
	}
}

// AddGroup adds a custom tool group.
func (r *Resolver) AddGroup(name string, tools []string) {
	r.groups[name] = tools
}

// RegisterMCPServer registers tools from an MCP server.
func (r *Resolver) RegisterMCPServer(serverID string, tools []string) {
	r.mcpServers[serverID] = tools
	// Also add as a group
	r.groups["mcp:"+serverID] = tools
}

// ExpandGroups expands group references in a tool list.
func (r *Resolver) ExpandGroups(items []string) []string {
	var result []string
	seen := make(map[string]bool)

	for _, item := range items {
		normalized := NormalizeTool(item)

		// Check if it's a group reference
		if tools, ok := r.groups[normalized]; ok {
			for _, tool := range tools {
				if !seen[tool] {
					seen[tool] = true
					result = append(result, tool)
				}
			}
			continue
		}

		// Check for MCP wildcard (mcp:server.*)
		if strings.HasPrefix(normalized, "mcp:") && strings.HasSuffix(normalized, ".*") {
			serverID := strings.TrimSuffix(strings.TrimPrefix(normalized, "mcp:"), ".*")
			if tools, ok := r.mcpServers[serverID]; ok {
				for _, tool := range tools {
					fullName := "mcp:" + serverID + "." + tool
					if !seen[fullName] {
						seen[fullName] = true
						result = append(result, fullName)
					}
				}
			}
			continue
		}

		// Regular tool
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}

	return result
}

// IsAllowed checks if a tool is allowed by the given policy.
func (r *Resolver) IsAllowed(policy *Policy, toolName string) bool {
	normalized := NormalizeTool(toolName)

	// Build effective allow list
	var allowed []string

	// Start with profile defaults
	if policy.Profile != "" {
		if profilePolicy, ok := ProfileDefaults[policy.Profile]; ok && profilePolicy != nil {
			allowed = r.ExpandGroups(profilePolicy.Allow)
		}
	}

	// Add explicit allows
	if len(policy.Allow) > 0 {
		allowed = append(allowed, r.ExpandGroups(policy.Allow)...)
	}

	// Build deny list
	denied := r.ExpandGroups(policy.Deny)

	// Check denial first (deny always wins)
	for _, d := range denied {
		if d == normalized {
			return false
		}
		// Handle MCP tool denial
		if strings.HasPrefix(normalized, "mcp:") && matchMCPPattern(d, normalized) {
			return false
		}
	}

	// Full profile allows everything not denied
	if policy.Profile == ProfileFull {
		return true
	}

	// Check allow list
	for _, a := range allowed {
		if a == normalized {
			return true
		}
		// Handle MCP tool patterns
		if strings.HasPrefix(normalized, "mcp:") && matchMCPPattern(a, normalized) {
			return true
		}
	}

	return false
}

// matchMCPPattern checks if a pattern matches an MCP tool name.
// Pattern can be:
//   - "mcp:server.tool" - exact match
//   - "mcp:server.*" - all tools from server
//   - "mcp:*" - all MCP tools
func matchMCPPattern(pattern, toolName string) bool {
	if pattern == "mcp:*" {
		return strings.HasPrefix(toolName, "mcp:")
	}

	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(toolName, prefix)
	}

	return pattern == toolName
}

// FilterAllowed filters a list of tools to only those allowed by the policy.
func (r *Resolver) FilterAllowed(policy *Policy, tools []string) []string {
	var result []string
	for _, tool := range tools {
		if r.IsAllowed(policy, tool) {
			result = append(result, tool)
		}
	}
	return result
}

// GetDenied returns the list of explicitly denied tools.
func (r *Resolver) GetDenied(policy *Policy) []string {
	return r.ExpandGroups(policy.Deny)
}

// GetAllowed returns the list of explicitly allowed tools.
func (r *Resolver) GetAllowed(policy *Policy) []string {
	var allowed []string

	// Profile defaults
	if policy.Profile != "" {
		if profilePolicy, ok := ProfileDefaults[policy.Profile]; ok && profilePolicy != nil {
			allowed = r.ExpandGroups(profilePolicy.Allow)
		}
	}

	// Explicit allows
	if len(policy.Allow) > 0 {
		allowed = append(allowed, r.ExpandGroups(policy.Allow)...)
	}

	return allowed
}

// Merge merges multiple policies into one.
// Later policies override earlier ones.
func Merge(policies ...*Policy) *Policy {
	result := &Policy{}

	for _, p := range policies {
		if p == nil {
			continue
		}

		// Last profile wins
		if p.Profile != "" {
			result.Profile = p.Profile
		}

		// Accumulate allows
		result.Allow = append(result.Allow, p.Allow...)

		// Accumulate denies
		result.Deny = append(result.Deny, p.Deny...)
	}

	return result
}

// NewPolicy creates a policy with the given profile.
func NewPolicy(profile Profile) *Policy {
	return &Policy{Profile: profile}
}

// WithAllow adds tools to the allow list.
func (p *Policy) WithAllow(tools ...string) *Policy {
	p.Allow = append(p.Allow, tools...)
	return p
}

// WithDeny adds tools to the deny list.
func (p *Policy) WithDeny(tools ...string) *Policy {
	p.Deny = append(p.Deny, tools...)
	return p
}
