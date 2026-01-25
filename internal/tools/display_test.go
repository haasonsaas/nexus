package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

func formatDetailKey(key string) string {
	// Check for override
	if override, ok := DetailLabelOverrides[key]; ok {
		return override
	}

	// Convert camelCase to words
	var result []rune
	for i, r := range key {
		if unicode.IsUpper(r) && i > 0 {
			result = append(result, ' ')
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, unicode.ToLower(r))
		}
	}

	// Replace underscores with spaces
	return strings.ReplaceAll(string(result), "_", " ")
}

func trimToMaxLength(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"read", "read"},
		{"READ", "read"},
		{"read_tool", "read"},
		{"mcp__server__read", "read"},
		{"server.read", "read"},
		{"mcp__files__write_tool", "write"},
		{"bash", "bash"},
		{"WEB_SEARCH", "web_search"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeToolName(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeToolName(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestDefaultTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"read", "Read"},
		{"web_search", "Web Search"},
		{"memory-search", "Memory Search"},
		{"sendMessage", "Sendmessage"}, // camelCase not handled here
		{"mcp__server__file_reader", "File Reader"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := defaultTitle(tc.input)
			if result != tc.expected {
				t.Errorf("defaultTitle(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestCoerceDisplayValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"empty string", "", ""},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int", 42, "42"},
		{"float64 whole", float64(42), "42"},
		{"float64 decimal", 3.14, "3.14"},
		{"array", []interface{}{"a", "b", "c"}, "a, b, c"},
		{"empty array", []interface{}{}, ""},
		{"map with name", map[string]interface{}{"name": "test"}, "test"},
		{"map with id", map[string]interface{}{"id": "123"}, "123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := coerceDisplayValue(tc.input)
			if result != tc.expected {
				t.Errorf("coerceDisplayValue(%v) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestLookupValueByPath(t *testing.T) {
	args := map[string]interface{}{
		"path":   "/home/user/file.txt",
		"nested": map[string]interface{}{"key": "value"},
		"deep": map[string]interface{}{
			"level1": map[string]interface{}{
				"level2": "deepvalue",
			},
		},
	}

	tests := []struct {
		path     string
		expected interface{}
	}{
		{"path", "/home/user/file.txt"},
		{"nested.key", "value"},
		{"deep.level1.level2", "deepvalue"},
		{"missing", nil},
		{"nested.missing", nil},
		{"", nil},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := lookupValueByPath(args, tc.path)
			if result != tc.expected {
				t.Errorf("lookupValueByPath(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestFormatDetailKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"agentId", "agent"},
		{"sessionKey", "session"},
		{"timeoutSeconds", "timeout"},
		{"filePath", "file path"},
		{"user_name", "user name"},
		{"simple", "simple"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := formatDetailKey(tc.input)
			if result != tc.expected {
				t.Errorf("formatDetailKey(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestShortenHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("could not determine home directory")
	}

	tests := []struct {
		input    string
		expected string
	}{
		{filepath.Join(home, "projects", "test.go"), "~/projects/test.go"},
		{"/tmp/other/file.txt", "/tmp/other/file.txt"},
		{"", ""},
		{"relative/path", "relative/path"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := shortenHomePath(tc.input)
			if result != tc.expected {
				t.Errorf("shortenHomePath(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestResolveToolDisplay(t *testing.T) {
	t.Run("read tool with path", func(t *testing.T) {
		args := map[string]interface{}{
			"path": "/tmp/test.txt",
		}
		display := ResolveToolDisplay("read", args, "")

		if display.Emoji != "📖" {
			t.Errorf("expected emoji '📖', got %q", display.Emoji)
		}
		if display.Title != "Read" {
			t.Errorf("expected title 'Read', got %q", display.Title)
		}
		if display.Label != "Reading" {
			t.Errorf("expected label 'Reading', got %q", display.Label)
		}
		if display.Detail != "/tmp/test.txt" {
			t.Errorf("expected detail '/tmp/test.txt', got %q", display.Detail)
		}
	})

	t.Run("read tool with offset and limit", func(t *testing.T) {
		args := map[string]interface{}{
			"path":   "/tmp/test.txt",
			"offset": float64(100),
			"limit":  float64(500),
		}
		display := ResolveToolDisplay("read", args, "")

		expected := "/tmp/test.txt (100-500)"
		if display.Detail != expected {
			t.Errorf("expected detail %q, got %q", expected, display.Detail)
		}
	})

	t.Run("bash tool", func(t *testing.T) {
		args := map[string]interface{}{
			"command": "ls -la",
		}
		display := ResolveToolDisplay("bash", args, "")

		if display.Emoji != "💻" {
			t.Errorf("expected emoji '💻', got %q", display.Emoji)
		}
		if display.Title != "Bash" {
			t.Errorf("expected title 'Bash', got %q", display.Title)
		}
		if display.Detail != "ls -la" {
			t.Errorf("expected detail 'ls -la', got %q", display.Detail)
		}
	})

	t.Run("grep tool with multiple detail keys", func(t *testing.T) {
		args := map[string]interface{}{
			"pattern": "TODO",
			"path":    "/project",
		}
		display := ResolveToolDisplay("grep", args, "")

		if display.Emoji != "🔍" {
			t.Errorf("expected emoji '🔍', got %q", display.Emoji)
		}
		// Detail should contain both pattern and path
		if display.Detail != "TODO · /project" {
			t.Errorf("expected detail 'TODO · /project', got %q", display.Detail)
		}
	})

	t.Run("unknown tool uses fallback", func(t *testing.T) {
		args := map[string]interface{}{}
		display := ResolveToolDisplay("custom_unknown_tool", args, "")

		if display.Emoji != "🧩" {
			t.Errorf("expected fallback emoji '🧩', got %q", display.Emoji)
		}
		// defaultTitle normalizes to "custom unknown" (removes _tool suffix)
		// then title-cases each word
		if display.Title != "Custom Unknown" {
			t.Errorf("expected title 'Custom Unknown', got %q", display.Title)
		}
	})

	t.Run("namespaced tool normalizes", func(t *testing.T) {
		args := map[string]interface{}{
			"path": "/tmp/file.txt",
		}
		display := ResolveToolDisplay("mcp__files__read", args, "")

		if display.Emoji != "📖" {
			t.Errorf("expected emoji '📖', got %q", display.Emoji)
		}
	})
}

func TestFormatToolSummary(t *testing.T) {
	tests := []struct {
		name     string
		display  *ToolDisplay
		expected string
	}{
		{
			name: "full display",
			display: &ToolDisplay{
				Emoji:  "📖",
				Label:  "Reading",
				Detail: "/tmp/test.txt",
			},
			expected: "📖 Reading: /tmp/test.txt",
		},
		{
			name: "no detail",
			display: &ToolDisplay{
				Emoji: "💻",
				Label: "Running",
			},
			expected: "💻 Running",
		},
		{
			name: "no label uses title",
			display: &ToolDisplay{
				Emoji:  "🔍",
				Title:  "Grep",
				Detail: "pattern",
			},
			expected: "🔍 Grep: pattern",
		},
		{
			name: "no emoji",
			display: &ToolDisplay{
				Label:  "Processing",
				Detail: "data",
			},
			expected: "Processing: data",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatToolSummary(tc.display)
			if result != tc.expected {
				t.Errorf("FormatToolSummary() = %q, want %q", result, tc.expected)
			}
		})
	}
}

func TestFormatToolDetail(t *testing.T) {
	tests := []struct {
		name     string
		display  *ToolDisplay
		expected string
	}{
		{
			name:     "with detail",
			display:  &ToolDisplay{Detail: "some detail"},
			expected: "some detail",
		},
		{
			name:     "empty detail",
			display:  &ToolDisplay{},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatToolDetail(tc.display)
			if result != tc.expected {
				t.Errorf("FormatToolDetail() = %q, want %q", result, tc.expected)
			}
		})
	}
}

func TestResolveDetailFromKeys(t *testing.T) {
	args := map[string]interface{}{
		"pattern": "test",
		"path":    "/project",
		"query":   "search term",
	}

	tests := []struct {
		name     string
		keys     []string
		expected string
	}{
		{"single key", []string{"pattern"}, "test"},
		{"multiple keys", []string{"pattern", "path"}, "test · /project"},
		{"missing key", []string{"missing"}, ""},
		{"mixed keys", []string{"pattern", "missing", "query"}, "test · search term"},
		{"empty keys", []string{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveDetailFromKeys(args, tc.keys)
			if result != tc.expected {
				t.Errorf("resolveDetailFromKeys(%v) = %q, want %q", tc.keys, result, tc.expected)
			}
		})
	}
}

func TestGetActionFromArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     interface{}
		expected string
	}{
		{
			name:     "action key",
			args:     map[string]interface{}{"action": "click"},
			expected: "click",
		},
		{
			name:     "type key",
			args:     map[string]interface{}{"type": "submit"},
			expected: "submit",
		},
		{
			name:     "method key",
			args:     map[string]interface{}{"method": "GET"},
			expected: "GET",
		},
		{
			name:     "operation key",
			args:     map[string]interface{}{"operation": "delete"},
			expected: "delete",
		},
		{
			name:     "no action key",
			args:     map[string]interface{}{"other": "value"},
			expected: "",
		},
		{
			name:     "nil args",
			args:     nil,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getActionFromArgs(tc.args)
			if result != tc.expected {
				t.Errorf("getActionFromArgs(%v) = %q, want %q", tc.args, result, tc.expected)
			}
		})
	}
}

func TestDefaultToolDisplayConfig(t *testing.T) {
	config := DefaultToolDisplayConfig()

	if config == nil {
		t.Fatal("DefaultToolDisplayConfig() returned nil")
	}

	if config.Version != 1 {
		t.Errorf("expected version 1, got %d", config.Version)
	}

	if config.Fallback == nil {
		t.Error("expected fallback to be set")
	}

	if config.Fallback.Emoji != "🧩" {
		t.Errorf("expected fallback emoji '🧩', got %q", config.Fallback.Emoji)
	}

	// Check some known tools
	expectedTools := []string{"read", "write", "edit", "bash", "grep", "glob", "browser"}
	for _, toolName := range expectedTools {
		if _, ok := config.Tools[toolName]; !ok {
			t.Errorf("expected tool %q to be in config", toolName)
		}
	}
}

func TestResolveWriteDetail(t *testing.T) {
	tests := []struct {
		name     string
		args     interface{}
		expected string
	}{
		{
			name:     "path key",
			args:     map[string]interface{}{"path": "/tmp/file.txt"},
			expected: "/tmp/file.txt",
		},
		{
			name:     "file_path key",
			args:     map[string]interface{}{"file_path": "/project/main.go"},
			expected: "/project/main.go",
		},
		{
			name:     "both keys prefers path",
			args:     map[string]interface{}{"path": "/a", "file_path": "/b"},
			expected: "/a",
		},
		{
			name:     "no path",
			args:     map[string]interface{}{"content": "data"},
			expected: "",
		},
		{
			name:     "nil args",
			args:     nil,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveWriteDetail(tc.args)
			if result != tc.expected {
				t.Errorf("resolveWriteDetail(%v) = %q, want %q", tc.args, result, tc.expected)
			}
		})
	}
}

func TestTrimToMaxLength(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"hello", 5, "hello"},
		{"hi", 2, "hi"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := trimToMaxLength(tc.input, tc.maxLen)
			if result != tc.expected {
				t.Errorf("trimToMaxLength(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expected)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[1;32mbold green\x1b[0m text", "bold green text"},
		{"no escape codes", "no escape codes"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := stripANSI(tc.input)
			if result != tc.expected {
				t.Errorf("stripANSI(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestMaxDetailEntries(t *testing.T) {
	// Create args with more than MaxDetailEntries
	args := map[string]interface{}{}
	keys := []string{}
	for i := 0; i < 15; i++ {
		key := string(rune('a' + i))
		args[key] = key
		keys = append(keys, key)
	}

	result := resolveDetailFromKeys(args, keys)

	// Count number of detail separators
	separatorCount := 0
	for i := 0; i < len(result)-2; i++ {
		if result[i:i+3] == " · " {
			separatorCount++
		}
	}

	// With MaxDetailEntries items, we should have MaxDetailEntries-1 separators
	if separatorCount >= MaxDetailEntries {
		t.Errorf("expected at most %d separators, got %d", MaxDetailEntries-1, separatorCount)
	}
}
