package policy

// ToolGroups defines named groups of tools for easier policy configuration.
// Group names use the "group:" prefix to distinguish them from tool names.
// Based on Clawdbot patterns for consistent tool categorization.
var ToolGroups = map[string][]string{
	// Runtime/execution tools - commands that run code or processes
	"group:runtime": {"exec", "bash", "process", "sandbox", "execute_code"},

	// Filesystem tools - read/write/modify files
	"group:fs": {"read", "write", "edit", "apply_patch"},

	// Session management tools
	"group:sessions": {
		"sessions_list",
		"sessions_history",
		"sessions_send",
		"sessions_spawn",
		"session_status",
	},

	// Memory/knowledge retrieval tools
	"group:memory": {"memory_search", "memory_get"},

	// UI/browser automation tools
	"group:ui": {"browser", "canvas"},

	// Automation/scheduling tools
	"group:automation": {"cron", "gateway", "job_status"},

	// Messaging tools - send messages to users/channels
	"group:messaging": {"message", "send_message"},

	// Web tools - search and fetch from the web
	"group:web": {"websearch", "webfetch", "web_search", "web_fetch"},

	// All built-in Nexus tools
	"group:nexus": {
		// Runtime
		"exec", "bash", "process", "sandbox", "execute_code",
		// Filesystem
		"read", "write", "edit", "apply_patch",
		// Web
		"websearch", "webfetch", "web_search", "web_fetch",
		// Memory
		"memory_search", "memory_get",
		// Browser
		"browser", "canvas",
		// Messaging
		"message", "send_message",
		// Jobs
		"job_status",
		// Sessions
		"sessions_list", "sessions_history", "sessions_send", "sessions_spawn", "session_status",
	},

	// Read-only tools - safe tools that don't modify state
	"group:readonly": {
		"read",
		"websearch", "webfetch", "web_search", "web_fetch",
		"memory_search", "memory_get",
		"sessions_list", "sessions_history", "session_status",
		"job_status",
	},
}

// ToolProfiles defines pre-configured tool sets for common use cases.
// These map profile names to policies with their allowed tool groups.
var ToolProfiles = map[string]*Policy{
	// Coding profile - full development capabilities
	// Allows filesystem, runtime, web research, and memory tools
	"coding": {
		Profile: ProfileCoding,
		Allow: []string{
			"group:fs",
			"group:runtime",
			"group:web",
			"group:memory",
			"group:sessions",
			"group:automation",
		},
	},

	// Messaging profile - only messaging tools
	// For agents that should only send messages without other capabilities
	"messaging": {
		Profile: ProfileMessaging,
		Allow: []string{
			"group:messaging",
			"status",
		},
	},

	// Readonly profile - observation only, no modifications
	// For agents that need to read and analyze but not change anything
	"readonly": {
		Allow: []string{
			"group:readonly",
		},
	},

	// Full profile - everything allowed (except explicit denies)
	"full": {
		Profile: ProfileFull,
	},

	// Minimal profile - just status checks
	"minimal": {
		Profile: ProfileMinimal,
		Allow:   []string{"status"},
	},
}

// ExpandGroups expands group references in a tool list to their constituent tools.
// It handles:
//   - Group references (e.g., "group:fs" -> ["read", "write", "edit", "apply_patch"])
//   - Direct tool names (passed through unchanged)
//   - Deduplication of results
//
// Example:
//
//	ExpandGroups([]string{"group:fs", "websearch"})
//	// Returns: ["read", "write", "edit", "apply_patch", "websearch"]
func ExpandGroups(items []string) []string {
	var result []string
	seen := make(map[string]bool)

	for _, item := range items {
		// Check if it's a group reference
		if tools, ok := ToolGroups[item]; ok {
			for _, tool := range tools {
				if !seen[tool] {
					seen[tool] = true
					result = append(result, tool)
				}
			}
			continue
		}

		// Regular tool name
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// GetProfilePolicy returns the policy for a named profile.
// Returns nil if the profile doesn't exist.
func GetProfilePolicy(name string) *Policy {
	return ToolProfiles[name]
}

// ListGroups returns all available group names.
func ListGroups() []string {
	groups := make([]string, 0, len(ToolGroups))
	for name := range ToolGroups {
		groups = append(groups, name)
	}
	return groups
}

// ListProfiles returns all available profile names.
func ListProfiles() []string {
	profiles := make([]string, 0, len(ToolProfiles))
	for name := range ToolProfiles {
		profiles = append(profiles, name)
	}
	return profiles
}

// IsGroup returns true if the name is a valid group reference.
func IsGroup(name string) bool {
	_, ok := ToolGroups[name]
	return ok
}

// GetGroupTools returns the tools in a group, or nil if the group doesn't exist.
func GetGroupTools(name string) []string {
	tools, ok := ToolGroups[name]
	if !ok {
		return nil
	}
	// Return a copy to prevent modification
	result := make([]string, len(tools))
	copy(result, tools)
	return result
}

// init ensures ToolGroups is synchronized with DefaultGroups
func init() {
	// Copy ToolGroups to DefaultGroups for backwards compatibility
	for name, tools := range ToolGroups {
		DefaultGroups[name] = tools
	}
}
