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
