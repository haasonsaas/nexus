// Package policy provides tool authorization and access control.
// It defines profiles, policies, and groups for managing which tools
// agents are allowed to use.
package policy

import (
	"strings"
)

// Profile defines a pre-configured tool access profile that provides
// sensible defaults for common use cases like coding, messaging, or full access.
type Profile string

const (
	// ProfileMinimal allows only status tools.
	ProfileMinimal Profile = "minimal"

	// ProfileCoding allows filesystem, runtime, and web tools.
	ProfileCoding Profile = "coding"

	// ProfileMessaging allows messaging tools.
	ProfileMessaging Profile = "messaging"

	// ProfileFull allows all tools (except explicitly denied).
	ProfileFull Profile = "full"
)

// Policy defines tool access rules for an agent combining profiles with
// explicit allow and deny lists. Deny rules always take precedence over allow rules.
type Policy struct {
	// Profile is a pre-configured access level.
	Profile Profile `json:"profile,omitempty" yaml:"profile"`

	// Allow explicitly allows these tools (in addition to profile).
	Allow []string `json:"allow,omitempty" yaml:"allow"`

	// Deny explicitly denies these tools (overrides allow).
	Deny []string `json:"deny,omitempty" yaml:"deny"`

	// ByProvider applies additional policy rules scoped to a tool provider.
	// For MCP tools, the provider key is "mcp:<server>".
	// For built-in tools, the provider key is "nexus".
	ByProvider map[string]*Policy `json:"by_provider,omitempty" yaml:"by_provider,omitempty"`
}

// ToolGroup defines a named group of tools for convenient bulk permissions.
type ToolGroup struct {
	Name  string
	Tools []string
}

// DefaultGroups are the built-in tool groups.
// Groups can be referenced in policies using their key (e.g., "group:fs").
var DefaultGroups = map[string][]string{
	// Filesystem tools
	"group:fs": {"read", "write", "edit", "exec"},

	// Web tools
	"group:web": {"websearch", "webfetch"},

	// Runtime/execution tools
	"group:runtime": {"sandbox"},

	// Memory tools
	"group:memory": {"memory_search"},

	// Browser automation
	"group:browser": {"browser"},

	// Messaging
	"group:messaging": {"send_message"},

	// Jobs
	"group:jobs": {"job_status"},

	// All built-in nexus tools
	"group:nexus": {
		"read", "write", "edit", "exec",
		"websearch", "webfetch",
		"sandbox",
		"memory_search",
		"browser",
		"send_message",
		"job_status",
	},

	// MCP tools (dynamically populated via RegisterMCPServer)
	// Use "mcp:*" in policies to allow all MCP tools
	// Use "mcp:serverID.*" to allow all tools from a specific server
	// Use "mcp:serverID.toolName" for specific tools
	"group:mcp": {},

	// All tools (native + MCP)
	// Note: This is a marker group; actual resolution uses ProfileFull
	"group:all": {},
}

// ProfileDefaults defines the default allow lists for each profile.
var ProfileDefaults = map[Profile]*Policy{
	ProfileMinimal: {
		Allow: []string{"status"},
	},
	ProfileCoding: {
		Allow: []string{"group:fs", "group:runtime", "group:web", "group:memory"},
	},
	ProfileMessaging: {
		Allow: []string{"group:messaging", "status"},
	},
	ProfileFull: {
		// Full profile allows everything not explicitly denied
	},
}

// ToolAliases maps alternative names to canonical tool names.
var ToolAliases = map[string]string{
	"bash":        "exec",
	"shell":       "exec",
	"apply-patch": "edit",
	"apply_patch": "edit",
	"sandbox":     "execute_code",
	"websearch":   "web_search",
	"webfetch":    "web_fetch",
}

// NormalizeTool normalizes a tool name to its canonical form by converting
// to lowercase and resolving known aliases.
func NormalizeTool(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := ToolAliases[normalized]; ok {
		return alias
	}
	return normalized
}

// NormalizeTools normalizes a list of tool names to their canonical forms.
func NormalizeTools(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		normalized := NormalizeTool(name)
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

// UnifiedPolicyBuilder provides a fluent interface for building policies
// that work consistently across native and MCP tools.
type UnifiedPolicyBuilder struct {
	policy *Policy
}

// NewUnifiedPolicy creates a new unified policy builder.
func NewUnifiedPolicy() *UnifiedPolicyBuilder {
	return &UnifiedPolicyBuilder{
		policy: &Policy{},
	}
}

// WithProfile sets the base profile.
func (b *UnifiedPolicyBuilder) WithProfile(profile Profile) *UnifiedPolicyBuilder {
	b.policy.Profile = profile
	return b
}

// AllowNative allows native (built-in) tools.
func (b *UnifiedPolicyBuilder) AllowNative(tools ...string) *UnifiedPolicyBuilder {
	for _, t := range tools {
		b.policy.Allow = append(b.policy.Allow, NormalizeTool(t))
	}
	return b
}

// AllowNativeGroup allows a native tool group (e.g., "fs", "web").
func (b *UnifiedPolicyBuilder) AllowNativeGroup(groups ...string) *UnifiedPolicyBuilder {
	for _, g := range groups {
		if !strings.HasPrefix(g, "group:") {
			g = "group:" + g
		}
		b.policy.Allow = append(b.policy.Allow, g)
	}
	return b
}

// AllowMCPServer allows all tools from an MCP server.
func (b *UnifiedPolicyBuilder) AllowMCPServer(serverIDs ...string) *UnifiedPolicyBuilder {
	for _, id := range serverIDs {
		b.policy.Allow = append(b.policy.Allow, "mcp:"+id+".*")
	}
	return b
}

// AllowMCPTool allows a specific MCP tool.
func (b *UnifiedPolicyBuilder) AllowMCPTool(serverID, toolName string) *UnifiedPolicyBuilder {
	b.policy.Allow = append(b.policy.Allow, "mcp:"+serverID+"."+toolName)
	return b
}

// AllowAllMCP allows all MCP tools.
func (b *UnifiedPolicyBuilder) AllowAllMCP() *UnifiedPolicyBuilder {
	b.policy.Allow = append(b.policy.Allow, "mcp:*")
	return b
}

// DenyNative denies native (built-in) tools.
func (b *UnifiedPolicyBuilder) DenyNative(tools ...string) *UnifiedPolicyBuilder {
	for _, t := range tools {
		b.policy.Deny = append(b.policy.Deny, NormalizeTool(t))
	}
	return b
}

// DenyMCPServer denies all tools from an MCP server.
func (b *UnifiedPolicyBuilder) DenyMCPServer(serverIDs ...string) *UnifiedPolicyBuilder {
	for _, id := range serverIDs {
		b.policy.Deny = append(b.policy.Deny, "mcp:"+id+".*")
	}
	return b
}

// DenyMCPTool denies a specific MCP tool.
func (b *UnifiedPolicyBuilder) DenyMCPTool(serverID, toolName string) *UnifiedPolicyBuilder {
	b.policy.Deny = append(b.policy.Deny, "mcp:"+serverID+"."+toolName)
	return b
}

// WithMCPServerPolicy sets provider-specific policy for an MCP server.
func (b *UnifiedPolicyBuilder) WithMCPServerPolicy(serverID string, policy *Policy) *UnifiedPolicyBuilder {
	if b.policy.ByProvider == nil {
		b.policy.ByProvider = make(map[string]*Policy)
	}
	b.policy.ByProvider["mcp:"+serverID] = policy
	return b
}

// WithNativePolicy sets provider-specific policy for native tools.
func (b *UnifiedPolicyBuilder) WithNativePolicy(policy *Policy) *UnifiedPolicyBuilder {
	if b.policy.ByProvider == nil {
		b.policy.ByProvider = make(map[string]*Policy)
	}
	b.policy.ByProvider["nexus"] = policy
	return b
}

// Build returns the constructed policy.
func (b *UnifiedPolicyBuilder) Build() *Policy {
	return b.policy
}

// IsMCPTool returns true if the tool name refers to an MCP tool.
func IsMCPTool(toolName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	return strings.HasPrefix(normalized, "mcp:") || strings.HasPrefix(normalized, "mcp.")
}

// ParseMCPToolName extracts the server ID and tool name from an MCP tool reference.
// Returns empty strings if the tool name is not an MCP tool.
func ParseMCPToolName(toolName string) (serverID, tool string) {
	normalized := strings.ToLower(strings.TrimSpace(toolName))

	// Handle both mcp:server.tool and mcp.server.tool formats
	var trimmed string
	if strings.HasPrefix(normalized, "mcp:") {
		trimmed = strings.TrimPrefix(normalized, "mcp:")
	} else if strings.HasPrefix(normalized, "mcp.") {
		trimmed = strings.TrimPrefix(normalized, "mcp.")
	} else {
		return "", ""
	}

	parts := strings.SplitN(trimmed, ".", 2)
	if len(parts) < 2 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
