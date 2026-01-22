package policy

import (
	"strings"
	"sync"
)

// Resolver resolves tool access based on policies by evaluating profiles,
// groups, allow lists, and deny lists. It supports MCP server tool registration,
// edge daemon tool registration, and custom tool aliases.
type Resolver struct {
	mu          sync.RWMutex
	groups      map[string][]string
	mcpServers  map[string][]string // serverID -> tool names
	edgeServers map[string][]string // edgeID -> tool names
	aliases     map[string]string   // alias -> canonical tool name
}

// Decision explains why a tool was allowed or denied, providing
// the reason string for debugging and audit purposes.
type Decision struct {
	Allowed bool
	Tool    string
	Reason  string
}

// NewResolver creates a new policy resolver with default groups initialized.
func NewResolver() *Resolver {
	return &Resolver{
		groups:      DefaultGroups,
		mcpServers:  make(map[string][]string),
		edgeServers: make(map[string][]string),
		aliases:     make(map[string]string),
	}
}

// AddGroup adds a custom tool group that can be referenced in policies.
func (r *Resolver) AddGroup(name string, tools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groups[name] = tools
}

// RegisterMCPServer registers tools from an MCP server, making them available
// for policy rules and creating a group "mcp:serverID" for convenience.
func (r *Resolver) RegisterMCPServer(serverID string, tools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpServers[serverID] = tools
	// Also add as a group
	r.groups["mcp:"+serverID] = tools
}

// RegisterEdgeServer registers tools from an edge daemon, making them available
// for policy rules and creating a group "edge:edgeID" for convenience.
func (r *Resolver) RegisterEdgeServer(edgeID string, tools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edgeServers[edgeID] = tools
	// Also add as a group
	r.groups["edge:"+edgeID] = tools
}

// UnregisterEdgeServer removes tools from an edge daemon.
func (r *Resolver) UnregisterEdgeServer(edgeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.edgeServers, edgeID)
	delete(r.groups, "edge:"+edgeID)
}

// RegisterAlias registers an alias that resolves to a canonical tool name,
// allowing alternative names like "bash" for "exec".
func (r *Resolver) RegisterAlias(alias string, canonical string) {
	alias = NormalizeTool(alias)
	canonical = NormalizeTool(canonical)
	if alias == "" || canonical == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[alias] = canonical
}

// CanonicalName resolves a tool name to its canonical form via registered aliases.
func (r *Resolver) CanonicalName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.canonicalNameLocked(name)
}

// canonicalNameLocked is the internal version that assumes lock is held.
func (r *Resolver) canonicalNameLocked(name string) string {
	normalized := NormalizeTool(name)
	if canonical, ok := r.aliases[normalized]; ok {
		return canonical
	}
	return normalized
}

// ExpandGroups expands group references (e.g., "group:fs") and wildcards
// (e.g., "mcp:server.*", "edge:device.*") in a tool list to their constituent tools.
func (r *Resolver) ExpandGroups(items []string) []string {
	var result []string
	seen := make(map[string]bool)

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, item := range items {
		normalized := r.canonicalNameLocked(item)

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

		// Check for edge wildcard (edge:device.*)
		if strings.HasPrefix(normalized, "edge:") && strings.HasSuffix(normalized, ".*") {
			edgeID := strings.TrimSuffix(strings.TrimPrefix(normalized, "edge:"), ".*")
			if tools, ok := r.edgeServers[edgeID]; ok {
				for _, tool := range tools {
					fullName := "edge:" + edgeID + "." + tool
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

// IsAllowed checks if a tool is allowed by the given policy and returns a boolean.
func (r *Resolver) IsAllowed(policy *Policy, toolName string) bool {
	return r.Decide(policy, toolName).Allowed
}

// Decide returns an allow/deny decision with a detailed reason string
// explaining which rule caused the decision.
func (r *Resolver) Decide(policy *Policy, toolName string) Decision {
	normalized := r.CanonicalName(toolName)
	decision := Decision{Allowed: false, Tool: normalized, Reason: "no matching allow rule"}

	if policy == nil {
		decision.Reason = "no policy configured"
		return decision
	}

	policy = r.effectivePolicyForTool(policy, normalized)
	if policy == nil {
		decision.Reason = "no policy configured"
		return decision
	}

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
			decision.Reason = "denied by rule: " + d
			return decision
		}
		if matchToolPattern(d, normalized) {
			decision.Reason = "denied by rule: " + d
			return decision
		}
	}

	// Full profile allows everything not denied
	if policy.Profile == ProfileFull {
		decision.Allowed = true
		decision.Reason = "allowed by profile full"
		return decision
	}

	// Check allow list
	for _, a := range allowed {
		if a == normalized {
			decision.Allowed = true
			decision.Reason = "allowed by rule: " + a
			return decision
		}
		if matchToolPattern(a, normalized) {
			decision.Allowed = true
			decision.Reason = "allowed by rule: " + a
			return decision
		}
	}

	return decision
}

func (r *Resolver) effectivePolicyForTool(policy *Policy, toolName string) *Policy {
	if policy == nil {
		return nil
	}
	if len(policy.ByProvider) == 0 {
		return policy
	}
	providerKey := toolProviderKey(toolName)
	if providerKey == "" {
		return policy
	}
	providerPolicy, ok := policy.ByProvider[providerKey]
	if !ok || providerPolicy == nil {
		return policy
	}

	base := *policy
	base.ByProvider = nil
	override := *providerPolicy
	override.ByProvider = nil
	return Merge(&base, &override)
}

func toolProviderKey(toolName string) string {
	normalized := NormalizeTool(toolName)
	if strings.HasPrefix(normalized, "mcp:") {
		trimmed := strings.TrimPrefix(normalized, "mcp:")
		if trimmed == "" {
			return "mcp"
		}
		parts := strings.SplitN(trimmed, ".", 2)
		if len(parts) > 0 && parts[0] != "" {
			return "mcp:" + parts[0]
		}
		return "mcp"
	}
	if strings.HasPrefix(normalized, "edge:") {
		trimmed := strings.TrimPrefix(normalized, "edge:")
		if trimmed == "" {
			return "edge"
		}
		parts := strings.SplitN(trimmed, ".", 2)
		if len(parts) > 0 && parts[0] != "" {
			return "edge:" + parts[0]
		}
		return "edge"
	}
	return "nexus"
}

// matchToolPattern checks if a pattern matches a tool name.
// Supports patterns for MCP, edge, and core tools:
//   - "mcp:*" or "edge:*" or "core.*" - all tools from source
//   - "mcp:server.*" or "edge:device.*" - all tools from server/device
//   - "mcp:server.tool" or "edge:device.tool" - exact match
//   - "*" - matches any tool
func matchToolPattern(pattern, toolName string) bool {
	// Universal wildcard
	if pattern == "*" {
		return true
	}

	// Source wildcards
	if pattern == "mcp:*" {
		return strings.HasPrefix(toolName, "mcp:")
	}
	if pattern == "edge:*" {
		return strings.HasPrefix(toolName, "edge:")
	}
	if pattern == "core.*" {
		return strings.HasPrefix(toolName, "core.") || !strings.Contains(toolName, ":")
	}

	// Namespace wildcards (e.g., "mcp:server.*", "edge:device.*")
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(toolName, prefix)
	}

	// Exact match
	return pattern == toolName
}

// matchMCPPattern is kept for backwards compatibility.
// Deprecated: Use matchToolPattern instead.
func matchMCPPattern(pattern, toolName string) bool {
	return matchToolPattern(pattern, toolName)
}

// FilterAllowed filters a list of tools to only those allowed by the policy,
// useful for presenting available tools to an agent.
func (r *Resolver) FilterAllowed(policy *Policy, tools []string) []string {
	var result []string
	for _, tool := range tools {
		if r.IsAllowed(policy, tool) {
			result = append(result, tool)
		}
	}
	return result
}

// GetDenied returns the list of explicitly denied tools with groups expanded.
func (r *Resolver) GetDenied(policy *Policy) []string {
	return r.ExpandGroups(policy.Deny)
}

// GetAllowed returns the list of explicitly allowed tools including
// profile defaults with groups expanded.
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

// Merge merges multiple policies into one combined policy.
// Later policies override earlier ones for profile, and allow/deny lists are accumulated.
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

		// Merge provider-specific policies (later wins).
		if len(p.ByProvider) > 0 {
			if result.ByProvider == nil {
				result.ByProvider = make(map[string]*Policy)
			}
			for key, policy := range p.ByProvider {
				result.ByProvider[key] = policy
			}
		}
	}

	return result
}

// NewPolicy creates a new policy with the given profile as a base.
func NewPolicy(profile Profile) *Policy {
	return &Policy{Profile: profile}
}

// WithAllow adds tools to the allow list and returns the policy for chaining.
func (p *Policy) WithAllow(tools ...string) *Policy {
	p.Allow = append(p.Allow, tools...)
	return p
}

// WithDeny adds tools to the deny list and returns the policy for chaining.
func (p *Policy) WithDeny(tools ...string) *Policy {
	p.Deny = append(p.Deny, tools...)
	return p
}
