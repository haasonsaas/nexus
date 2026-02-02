package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ToolDisplay contains formatted display info for a tool
type ToolDisplay struct {
	Name   string
	Emoji  string
	Title  string
	Label  string
	Verb   string
	Detail string
}

// ToolDisplaySpec defines display configuration for a tool
type ToolDisplaySpec struct {
	Emoji      string                       `json:"emoji,omitempty"`
	Title      string                       `json:"title,omitempty"`
	Label      string                       `json:"label,omitempty"`
	DetailKeys []string                     `json:"detailKeys,omitempty"`
	Actions    map[string]ToolDisplayAction `json:"actions,omitempty"`
}

// ToolDisplayAction defines action-specific display overrides
type ToolDisplayAction struct {
	Label      string   `json:"label,omitempty"`
	DetailKeys []string `json:"detailKeys,omitempty"`
}

// ToolDisplayConfig contains the full display configuration
type ToolDisplayConfig struct {
	Version  int                        `json:"version,omitempty"`
	Fallback *ToolDisplaySpec           `json:"fallback,omitempty"`
	Tools    map[string]ToolDisplaySpec `json:"tools,omitempty"`
}

// Detail label overrides for common keys
var DetailLabelOverrides = map[string]string{
	"agentId":           "agent",
	"sessionKey":        "session",
	"targetId":          "target",
	"targetUrl":         "url",
	"nodeId":            "node",
	"requestId":         "request",
	"messageId":         "message",
	"threadId":          "thread",
	"channelId":         "channel",
	"userId":            "user",
	"runTimeoutSeconds": "timeout",
	"timeoutSeconds":    "timeout",
	"maxChars":          "max chars",
}

// MaxDetailEntries limits the number of detail items shown
const MaxDetailEntries = 8

// defaultToolEmojis maps tool names to their default emojis
var defaultToolEmojis = map[string]string{
	"read":                 "📖",
	"write":                "✏️",
	"edit":                 "✏️",
	"bash":                 "💻",
	"search":               "🔍",
	"grep":                 "🔍",
	"glob":                 "📁",
	"ls":                   "📂",
	"browser":              "🌐",
	"web_search":           "🔎",
	"memory_search":        "🧠",
	"vector_memory_search": "🧠",
	"vector_memory_write":  "🧠",
	"message":              "💬",
	"send_message":         "📤",
	"spawn_agent":          "🤖",
	"subagent":             "🤖",
	"canvas":               "🎨",
	"image":                "🖼️",
	"camera":               "📷",
	"schedule":             "📅",
	"reminder":             "⏰",
	"tool":                 "🧩", // fallback
}

// DefaultToolDisplayConfig returns the default configuration
func DefaultToolDisplayConfig() *ToolDisplayConfig {
	return &ToolDisplayConfig{
		Version: 1,
		Fallback: &ToolDisplaySpec{
			Emoji:      "🧩",
			DetailKeys: []string{},
		},
		Tools: map[string]ToolDisplaySpec{
			"read": {
				Emoji:      "📖",
				Title:      "Read",
				Label:      "Reading",
				DetailKeys: []string{"path"},
			},
			"write": {
				Emoji:      "✏️",
				Title:      "Write",
				Label:      "Writing",
				DetailKeys: []string{"file_path", "path"},
			},
			"edit": {
				Emoji:      "✏️",
				Title:      "Edit",
				Label:      "Editing",
				DetailKeys: []string{"file_path", "path"},
			},
			"bash": {
				Emoji:      "💻",
				Title:      "Bash",
				Label:      "Running",
				DetailKeys: []string{"command"},
			},
			"grep": {
				Emoji:      "🔍",
				Title:      "Grep",
				Label:      "Searching",
				DetailKeys: []string{"pattern", "path"},
			},
			"glob": {
				Emoji:      "📁",
				Title:      "Glob",
				Label:      "Finding",
				DetailKeys: []string{"pattern"},
			},
			"browser": {
				Emoji:      "🌐",
				Title:      "Browser",
				Label:      "Browsing",
				DetailKeys: []string{"url", "action"},
			},
			"web_search": {
				Emoji:      "🔎",
				Title:      "Web Search",
				Label:      "Searching",
				DetailKeys: []string{"query"},
			},
			"memory_search": {
				Emoji:      "🧠",
				Title:      "Memory Search",
				Label:      "Searching memory",
				DetailKeys: []string{"query"},
			},
			"vector_memory_search": {
				Emoji:      "🧠",
				Title:      "Vector Memory Search",
				Label:      "Searching memory",
				DetailKeys: []string{"query", "scope"},
			},
			"vector_memory_write": {
				Emoji:      "🧠",
				Title:      "Vector Memory Write",
				Label:      "Writing memory",
				DetailKeys: []string{"scope", "tags"},
			},
			"send_message": {
				Emoji:      "📤",
				Title:      "Send Message",
				Label:      "Sending",
				DetailKeys: []string{"channelId", "threadId"},
			},
			"spawn_agent": {
				Emoji:      "🤖",
				Title:      "Spawn Agent",
				Label:      "Spawning",
				DetailKeys: []string{"agentId", "task"},
			},
			"canvas": {
				Emoji:      "🎨",
				Title:      "Canvas",
				Label:      "Drawing",
				DetailKeys: []string{"action"},
			},
			"reminder": {
				Emoji:      "⏰",
				Title:      "Reminder",
				Label:      "Setting reminder",
				DetailKeys: []string{"message", "time"},
			},
		},
	}
}

// ResolveToolDisplay resolves display info for a tool call
func ResolveToolDisplay(name string, args interface{}, meta string) *ToolDisplay {
	config := DefaultToolDisplayConfig()
	normalizedName := normalizeToolName(name)

	display := &ToolDisplay{
		Name:  name,
		Title: defaultTitle(name),
		Verb:  "Using",
	}

	// Look up tool spec
	spec, found := config.Tools[normalizedName]
	if !found {
		// Try original name
		spec, found = config.Tools[name]
	}

	if !found && config.Fallback != nil {
		spec = *config.Fallback
	}

	// Apply spec
	if spec.Emoji != "" {
		display.Emoji = spec.Emoji
	} else if emoji, ok := defaultToolEmojis[normalizedName]; ok {
		display.Emoji = emoji
	} else {
		display.Emoji = defaultToolEmojis["tool"]
	}

	if spec.Title != "" {
		display.Title = spec.Title
	}
	if spec.Label != "" {
		display.Label = spec.Label
	}

	// Check for action-specific overrides
	if spec.Actions != nil && args != nil {
		action := getActionFromArgs(args)
		if action != "" {
			if actionSpec, ok := spec.Actions[action]; ok {
				if actionSpec.Label != "" {
					display.Label = actionSpec.Label
				}
				if len(actionSpec.DetailKeys) > 0 {
					spec.DetailKeys = actionSpec.DetailKeys
				}
			}
		}
	}

	// Resolve detail
	display.Detail = resolveDetail(name, args, spec.DetailKeys)

	return display
}

// FormatToolDetail formats the detail portion of tool display
func FormatToolDetail(display *ToolDisplay) string {
	if display.Detail == "" {
		return ""
	}
	return display.Detail
}

// FormatToolSummary formats a complete tool summary line
func FormatToolSummary(display *ToolDisplay) string {
	parts := []string{}

	if display.Emoji != "" {
		parts = append(parts, display.Emoji)
	}

	label := display.Label
	if label == "" {
		label = display.Title
	}
	if label != "" {
		parts = append(parts, label)
	}

	summary := strings.Join(parts, " ")

	if display.Detail != "" {
		summary += ": " + display.Detail
	}

	return summary
}

// normalizeToolName cleans up tool name
func normalizeToolName(name string) string {
	// Remove common prefixes/suffixes
	normalized := strings.ToLower(name)

	// Handle namespaced tools like "mcp__server__tool"
	if strings.Contains(normalized, "__") {
		parts := strings.Split(normalized, "__")
		normalized = parts[len(parts)-1]
	}

	// Handle dotted namespaces like "server.tool"
	if strings.Contains(normalized, ".") {
		parts := strings.Split(normalized, ".")
		normalized = parts[len(parts)-1]
	}

	// Remove _tool suffix
	normalized = strings.TrimSuffix(normalized, "_tool")

	return normalized
}

// defaultTitle creates a default title from tool name
func defaultTitle(name string) string {
	// Get normalized name and convert to title case
	normalized := normalizeToolName(name)

	// Replace underscores and hyphens with spaces
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.ReplaceAll(normalized, "-", " ")

	// Title case each word
	words := strings.Fields(normalized)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// coerceDisplayValue converts a value to a display string
func coerceDisplayValue(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case int, int64, int32:
		return fmt.Sprintf("%d", v)
	case []interface{}:
		if len(v) == 0 {
			return ""
		}
		items := make([]string, 0, len(v))
		for _, item := range v {
			s := coerceDisplayValue(item)
			if s != "" {
				items = append(items, s)
			}
		}
		return strings.Join(items, ", ")
	case map[string]interface{}:
		// Try common name keys
		for _, key := range []string{"name", "id", "path", "value"} {
			if val, ok := v[key]; ok {
				return coerceDisplayValue(val)
			}
		}
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// lookupValueByPath gets a value from args using dot notation path
func lookupValueByPath(args interface{}, path string) interface{} {
	if args == nil || path == "" {
		return nil
	}

	parts := strings.Split(path, ".")

	current := args
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return nil
			}
			current = val
		default:
			return nil
		}
	}

	return current
}

// resolveDetailFromKeys extracts details from args using specified keys
func resolveDetailFromKeys(args interface{}, keys []string) string {
	if args == nil || len(keys) == 0 {
		return ""
	}

	details := []string{}
	count := 0

	for _, key := range keys {
		if count >= MaxDetailEntries {
			break
		}

		value := lookupValueByPath(args, key)
		if value == nil {
			continue
		}

		strValue := coerceDisplayValue(value)
		if strValue == "" {
			continue
		}

		// Shorten paths
		strValue = shortenHomePath(strValue)

		details = append(details, strValue)
		count++
	}

	return strings.Join(details, " · ")
}

// resolveReadDetail extracts detail for read tool (path:offset-limit)
func resolveReadDetail(args interface{}) string {
	argsMap, ok := args.(map[string]interface{})
	if !ok {
		return ""
	}

	path := ""
	if p, ok := argsMap["path"].(string); ok {
		path = shortenHomePath(p)
	} else if p, ok := argsMap["file_path"].(string); ok {
		path = shortenHomePath(p)
	}

	if path == "" {
		return ""
	}

	detail := path

	// Add offset:limit if present
	offset, hasOffset := argsMap["offset"]
	limit, hasLimit := argsMap["limit"]

	if hasOffset || hasLimit {
		offsetVal := coerceDisplayValue(offset)
		limitVal := coerceDisplayValue(limit)

		if offsetVal != "" || limitVal != "" {
			detail += " ("
			if offsetVal != "" {
				detail += offsetVal
			}
			if limitVal != "" {
				if offsetVal != "" {
					detail += "-"
				}
				detail += limitVal
			}
			detail += ")"
		}
	}

	return detail
}

// resolveWriteDetail extracts detail for write/edit tools
func resolveWriteDetail(args interface{}) string {
	argsMap, ok := args.(map[string]interface{})
	if !ok {
		return ""
	}

	path := ""
	if p, ok := argsMap["path"].(string); ok {
		path = shortenHomePath(p)
	} else if p, ok := argsMap["file_path"].(string); ok {
		path = shortenHomePath(p)
	}

	return path
}

// shortenHomePath replaces home directory with ~
func shortenHomePath(path string) string {
	if path == "" {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}

	// Clean path for comparison
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(home)

	if strings.HasPrefix(cleanPath, cleanHome) {
		return "~" + cleanPath[len(cleanHome):]
	}

	return path
}

// getActionFromArgs extracts the action parameter from args
func getActionFromArgs(args interface{}) string {
	argsMap, ok := args.(map[string]interface{})
	if !ok {
		return ""
	}

	// Try common action key names
	for _, key := range []string{"action", "type", "method", "operation"} {
		if val, ok := argsMap[key].(string); ok {
			return val
		}
	}

	return ""
}

// resolveDetail determines the detail string based on tool type and args
func resolveDetail(name string, args interface{}, detailKeys []string) string {
	normalizedName := normalizeToolName(name)

	// Special handling for certain tools
	switch normalizedName {
	case "read":
		return resolveReadDetail(args)
	case "write", "edit":
		return resolveWriteDetail(args)
	}

	// Use detail keys from config
	if len(detailKeys) > 0 {
		return resolveDetailFromKeys(args, detailKeys)
	}

	return ""
}
