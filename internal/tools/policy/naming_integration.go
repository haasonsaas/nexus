// Package policy provides tool authorization and access control.
// This file integrates with the naming package for unified tool identity.
package policy

import (
	"strings"
	"sync"

	"github.com/haasonsaas/nexus/internal/tools/naming"
)

// ToolRegistry provides a unified registry that bridges tool naming with policy.
// It wraps the naming.ToolRegistry and adds policy-specific functionality.
type ToolRegistry struct {
	naming   *naming.ToolRegistry
	resolver *Resolver

	mu             sync.RWMutex
	edgeServers    map[string][]string // edgeID -> tool names
	edgeTrustLevel map[string]TrustLevel
}

// TrustLevel defines the trust level for an edge device.
type TrustLevel string

const (
	// TrustUntrusted means tools require explicit approval for each use.
	TrustUntrusted TrustLevel = "untrusted"

	// TrustTOFU means trust-on-first-use; approved after first successful auth.
	TrustTOFU TrustLevel = "tofu"

	// TrustTrusted means tools are trusted and can be used without approval.
	TrustTrusted TrustLevel = "trusted"
)

// NewToolRegistry creates a new unified tool registry.
func NewToolRegistry(resolver *Resolver) *ToolRegistry {
	reg := &ToolRegistry{
		naming:         naming.NewToolRegistry(),
		resolver:       resolver,
		edgeServers:    make(map[string][]string),
		edgeTrustLevel: make(map[string]TrustLevel),
	}

	// Register default core aliases
	for alias, canonical := range naming.DefaultCoreAliases() {
		_ = reg.naming.RegisterAlias(alias, canonical) //nolint:errcheck // default aliases shouldn't fail
	}

	return reg
}

// RegisterCoreTool registers a core (built-in) tool.
func (r *ToolRegistry) RegisterCoreTool(name string) error {
	identity := naming.CoreTool(name)
	return r.naming.Register(identity)
}

// RegisterMCPTool registers an MCP tool and updates the policy resolver.
func (r *ToolRegistry) RegisterMCPTool(serverID, toolName string) error {
	identity := naming.MCPTool(serverID, toolName)
	if err := r.naming.Register(identity); err != nil {
		return err
	}

	// Also register with the compatibility resolver for backwards compatibility
	if r.resolver != nil {
		r.resolver.RegisterMCPServer(serverID, []string{toolName})
	}

	return nil
}

// RegisterMCPServer registers all tools from an MCP server.
func (r *ToolRegistry) RegisterMCPServer(serverID string, tools []string) error {
	for _, tool := range tools {
		identity := naming.MCPTool(serverID, tool)
		if err := r.naming.Register(identity); err != nil {
			// Continue on collision - server may be re-registering
			if _, ok := err.(naming.CollisionError); !ok {
				return err
			}
		}
	}

	// Register with compatibility resolver
	if r.resolver != nil {
		r.resolver.RegisterMCPServer(serverID, tools)
	}

	return nil
}

// RegisterEdgeTool registers a tool from an edge daemon.
func (r *ToolRegistry) RegisterEdgeTool(edgeID, toolName string) error {
	identity := naming.EdgeTool(edgeID, toolName)
	if err := r.naming.Register(identity); err != nil {
		return err
	}

	r.mu.Lock()
	r.edgeServers[edgeID] = append(r.edgeServers[edgeID], toolName)
	r.mu.Unlock()

	// Also add edge group to resolver
	if r.resolver != nil {
		r.resolver.AddGroup("edge:"+edgeID, r.edgeServers[edgeID])
	}

	return nil
}

// RegisterEdgeServer registers all tools from an edge daemon with a trust level.
func (r *ToolRegistry) RegisterEdgeServer(edgeID string, tools []string, trust TrustLevel) error {
	for _, tool := range tools {
		identity := naming.EdgeTool(edgeID, tool)
		if err := r.naming.Register(identity); err != nil {
			// Continue on collision
			if _, ok := err.(naming.CollisionError); !ok {
				return err
			}
		}
	}

	r.mu.Lock()
	r.edgeServers[edgeID] = tools
	r.edgeTrustLevel[edgeID] = trust
	r.mu.Unlock()

	// Add edge group to resolver
	if r.resolver != nil {
		r.resolver.AddGroup("edge:"+edgeID, tools)
	}

	return nil
}

// UnregisterEdgeServer removes all tools from an edge daemon.
func (r *ToolRegistry) UnregisterEdgeServer(edgeID string) {
	r.mu.Lock()
	tools := r.edgeServers[edgeID]
	delete(r.edgeServers, edgeID)
	delete(r.edgeTrustLevel, edgeID)
	r.mu.Unlock()

	for _, tool := range tools {
		identity := naming.EdgeTool(edgeID, tool)
		r.naming.Unregister(identity.CanonicalName)
	}
}

// Resolve resolves a tool name to its identity.
func (r *ToolRegistry) Resolve(name string) (naming.ToolIdentity, bool) {
	return r.naming.Resolve(name)
}

// ResolveCanonical resolves a tool name to its canonical form.
func (r *ToolRegistry) ResolveCanonical(name string) string {
	return r.naming.ResolveCanonical(name)
}

// GetEdgeTrustLevel returns the trust level for an edge device.
func (r *ToolRegistry) GetEdgeTrustLevel(edgeID string) TrustLevel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	level, ok := r.edgeTrustLevel[edgeID]
	if !ok {
		return TrustUntrusted
	}
	return level
}

// SetEdgeTrustLevel sets the trust level for an edge device.
func (r *ToolRegistry) SetEdgeTrustLevel(edgeID string, level TrustLevel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edgeTrustLevel[edgeID] = level
}

// All returns all registered tool identities.
func (r *ToolRegistry) All() []naming.ToolIdentity {
	return r.naming.All()
}

// BySource returns tools filtered by source.
func (r *ToolRegistry) BySource(source naming.ToolSource) []naming.ToolIdentity {
	return r.naming.BySource(source)
}

// Matching returns tools matching a pattern.
func (r *ToolRegistry) Matching(pattern string) []naming.ToolIdentity {
	return r.naming.Matching(pattern)
}

// IsEdgeTool returns true if the tool name refers to an edge tool.
func IsEdgeTool(toolName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	return strings.HasPrefix(normalized, "edge:")
}

// ParseEdgeToolName extracts the edge ID and tool name from an edge tool reference.
func ParseEdgeToolName(toolName string) (edgeID, tool string) {
	normalized := strings.ToLower(strings.TrimSpace(toolName))

	if !strings.HasPrefix(normalized, "edge:") {
		return "", ""
	}

	trimmed := strings.TrimPrefix(normalized, "edge:")
	parts := strings.SplitN(trimmed, ".", 2)
	if len(parts) < 2 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// IdentifyTool returns the source type for a tool name.
func IdentifyTool(toolName string) naming.ToolSource {
	normalized := strings.ToLower(strings.TrimSpace(toolName))

	if strings.HasPrefix(normalized, "mcp:") || strings.HasPrefix(normalized, "mcp.") {
		return naming.SourceMCP
	}
	if strings.HasPrefix(normalized, "edge:") {
		return naming.SourceEdge
	}
	if strings.HasPrefix(normalized, "core.") {
		return naming.SourceCore
	}

	// Default to core for unqualified names
	return naming.SourceCore
}
