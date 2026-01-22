package naming

import (
	"testing"
)

func TestCoreTool(t *testing.T) {
	tool := CoreTool("browser")

	if tool.Source != SourceCore {
		t.Errorf("expected source core, got %s", tool.Source)
	}
	if tool.Name != "browser" {
		t.Errorf("expected name browser, got %s", tool.Name)
	}
	if tool.SafeName != "browser" {
		t.Errorf("expected safe name browser, got %s", tool.SafeName)
	}
	if tool.CanonicalName != "core.browser" {
		t.Errorf("expected canonical core.browser, got %s", tool.CanonicalName)
	}
}

func TestMCPTool(t *testing.T) {
	tool := MCPTool("filesystem", "read_file")

	if tool.Source != SourceMCP {
		t.Errorf("expected source mcp, got %s", tool.Source)
	}
	if tool.Namespace != "filesystem" {
		t.Errorf("expected namespace filesystem, got %s", tool.Namespace)
	}
	if tool.Name != "read_file" {
		t.Errorf("expected name read_file, got %s", tool.Name)
	}
	if tool.CanonicalName != "mcp:filesystem.read_file" {
		t.Errorf("expected canonical mcp:filesystem.read_file, got %s", tool.CanonicalName)
	}
}

func TestEdgeTool(t *testing.T) {
	tool := EdgeTool("macbook", "camera_snap")

	if tool.Source != SourceEdge {
		t.Errorf("expected source edge, got %s", tool.Source)
	}
	if tool.Namespace != "macbook" {
		t.Errorf("expected namespace macbook, got %s", tool.Namespace)
	}
	if tool.Name != "camera_snap" {
		t.Errorf("expected name camera_snap, got %s", tool.Name)
	}
	if tool.CanonicalName != "edge:macbook.camera_snap" {
		t.Errorf("expected canonical edge:macbook.camera_snap, got %s", tool.CanonicalName)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		canonical     string
		expectSource  ToolSource
		expectNS      string
		expectName    string
		expectErr     bool
	}{
		{"core.browser", SourceCore, "", "browser", false},
		{"mcp:server.tool", SourceMCP, "server", "tool", false},
		{"edge:id.tool", SourceEdge, "id", "tool", false},
		{"legacy_tool", SourceCore, "", "legacy_tool", false}, // Legacy format
		{"mcp:", "", "", "", true},                            // Invalid
		{"mcp:server", "", "", "", true},                      // Missing tool
		{"edge:.", "", "", "", true},                          // Empty parts
	}

	for _, tt := range tests {
		t.Run(tt.canonical, func(t *testing.T) {
			identity, err := Parse(tt.canonical)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if identity.Source != tt.expectSource {
				t.Errorf("expected source %s, got %s", tt.expectSource, identity.Source)
			}
			if identity.Namespace != tt.expectNS {
				t.Errorf("expected namespace %s, got %s", tt.expectNS, identity.Namespace)
			}
			if identity.Name != tt.expectName {
				t.Errorf("expected name %s, got %s", tt.expectName, identity.Name)
			}
		})
	}
}

func TestToolIdentity_Matches(t *testing.T) {
	tests := []struct {
		canonical string
		pattern   string
		expected  bool
	}{
		// Exact match
		{"core.browser", "core.browser", true},
		{"mcp:server.tool", "mcp:server.tool", true},
		{"edge:id.tool", "edge:id.tool", true},

		// Source wildcard
		{"core.browser", "core.*", true},
		{"core.execute_code", "core.*", true},
		{"mcp:server.tool", "core.*", false},
		{"mcp:server.tool", "mcp:*", true},
		{"edge:id.tool", "edge:*", true},

		// Namespace wildcard
		{"mcp:server.tool1", "mcp:server.*", true},
		{"mcp:server.tool2", "mcp:server.*", true},
		{"mcp:other.tool", "mcp:server.*", false},

		// Full wildcard
		{"core.browser", "*", true},
		{"mcp:server.tool", "*", true},
		{"edge:id.tool", "*", true},

		// No match
		{"core.browser", "core.sandbox", false},
		{"mcp:a.b", "mcp:c.d", false},
	}

	for _, tt := range tests {
		t.Run(tt.canonical+"_"+tt.pattern, func(t *testing.T) {
			identity, _ := Parse(tt.canonical)
			if got := identity.Matches(tt.pattern); got != tt.expected {
				t.Errorf("Matches(%s) = %v, want %v", tt.pattern, got, tt.expected)
			}
		})
	}
}

func TestToolRegistry_Register(t *testing.T) {
	r := NewToolRegistry()

	// Register first tool
	err := r.Register(CoreTool("browser"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resolve by canonical
	identity, ok := r.Resolve("core.browser")
	if !ok {
		t.Error("expected to find tool by canonical name")
	}
	if identity.Name != "browser" {
		t.Errorf("expected name browser, got %s", identity.Name)
	}

	// Resolve by safe name
	identity, ok = r.Resolve("browser")
	if !ok {
		t.Error("expected to find tool by safe name")
	}
}

func TestToolRegistry_Collision(t *testing.T) {
	r := NewToolRegistry()

	// Register first tool
	err := r.Register(CoreTool("browser"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to register same canonical name
	err = r.Register(CoreTool("browser"))
	if err == nil {
		t.Error("expected collision error")
	}
	collErr, ok := err.(CollisionError)
	if !ok {
		t.Errorf("expected CollisionError, got %T", err)
	}
	if collErr.Field != "canonical" {
		t.Errorf("expected canonical collision, got %s", collErr.Field)
	}
}

func TestToolRegistry_Alias(t *testing.T) {
	r := NewToolRegistry()

	// Register tool
	err := r.Register(CoreTool("execute_code"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Register alias
	err = r.RegisterAlias("sandbox", "core.execute_code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resolve by alias
	identity, ok := r.Resolve("sandbox")
	if !ok {
		t.Error("expected to find tool by alias")
	}
	if identity.CanonicalName != "core.execute_code" {
		t.Errorf("expected core.execute_code, got %s", identity.CanonicalName)
	}

	// ResolveCanonical should work
	canonical := r.ResolveCanonical("sandbox")
	if canonical != "core.execute_code" {
		t.Errorf("expected core.execute_code, got %s", canonical)
	}
}

func TestToolRegistry_BySource(t *testing.T) {
	r := NewToolRegistry()

	r.Register(CoreTool("browser"))
	r.Register(CoreTool("sandbox"))
	r.Register(MCPTool("server", "tool1"))
	r.Register(EdgeTool("edge1", "camera"))

	core := r.BySource(SourceCore)
	if len(core) != 2 {
		t.Errorf("expected 2 core tools, got %d", len(core))
	}

	mcp := r.BySource(SourceMCP)
	if len(mcp) != 1 {
		t.Errorf("expected 1 MCP tool, got %d", len(mcp))
	}

	edge := r.BySource(SourceEdge)
	if len(edge) != 1 {
		t.Errorf("expected 1 edge tool, got %d", len(edge))
	}
}

func TestToolRegistry_Matching(t *testing.T) {
	r := NewToolRegistry()

	r.Register(CoreTool("browser"))
	r.Register(CoreTool("sandbox"))
	r.Register(MCPTool("fs", "read"))
	r.Register(MCPTool("fs", "write"))
	r.Register(MCPTool("git", "commit"))

	// Match all core
	core := r.Matching("core.*")
	if len(core) != 2 {
		t.Errorf("expected 2 core tools, got %d", len(core))
	}

	// Match specific MCP server
	fs := r.Matching("mcp:fs.*")
	if len(fs) != 2 {
		t.Errorf("expected 2 fs tools, got %d", len(fs))
	}

	// Match all
	all := r.Matching("*")
	if len(all) != 5 {
		t.Errorf("expected 5 tools, got %d", len(all))
	}
}

func TestToolRegistry_Unregister(t *testing.T) {
	r := NewToolRegistry()

	r.Register(CoreTool("browser"))
	r.RegisterAlias("web", "core.browser")

	// Verify registered
	if _, ok := r.Resolve("core.browser"); !ok {
		t.Error("expected tool to be registered")
	}
	if _, ok := r.Resolve("web"); !ok {
		t.Error("expected alias to work")
	}

	// Unregister
	r.Unregister("core.browser")

	// Verify gone
	if _, ok := r.Resolve("core.browser"); ok {
		t.Error("expected tool to be unregistered")
	}
	if _, ok := r.Resolve("web"); ok {
		t.Error("expected alias to be removed")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"browser", "browser"},
		{"web_search", "web_search"},
		{"UPPERCASE", "uppercase"},
		{"with-dashes", "with_dashes"},
		{"with.dots", "with_dots"},
		{"multiple__underscores", "multiple_underscores"},
		{"__leading_trailing__", "leading_trailing"},
		{"special!@#chars", "special_chars"},
		{"", "tool"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := sanitizeName(tt.input); got != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSafeNameWithNamespace_LongName(t *testing.T) {
	// Create a very long namespace and name
	longNS := "very_long_namespace_that_goes_on_and_on"
	longName := "extremely_long_tool_name_that_exceeds_limits"

	safeName := safeNameWithNamespace("mcp", longNS, longName)

	if len(safeName) > MaxSafeNameLength {
		t.Errorf("safe name too long: %d > %d", len(safeName), MaxSafeNameLength)
	}

	// Should end with hash suffix
	if !contains(safeName, "_") {
		t.Error("expected safe name to contain underscores")
	}
}

func TestDefaultCoreAliases(t *testing.T) {
	aliases := DefaultCoreAliases()

	// Check key aliases exist
	expected := map[string]string{
		"browser":      "core.browser",
		"sandbox":      "core.execute_code",
		"execute_code": "core.execute_code",
	}

	for alias, canonical := range expected {
		if aliases[alias] != canonical {
			t.Errorf("expected alias %s -> %s, got %s", alias, canonical, aliases[alias])
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
