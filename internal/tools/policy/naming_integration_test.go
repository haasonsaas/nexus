package policy

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/tools/naming"
)

func TestToolRegistry_RegisterCoreTool(t *testing.T) {
	reg := NewToolRegistry(nil)

	err := reg.RegisterCoreTool("browser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	identity, ok := reg.Resolve("core.browser")
	if !ok {
		t.Error("expected to find tool by canonical name")
	}
	if identity.Source != naming.SourceCore {
		t.Errorf("expected source core, got %s", identity.Source)
	}
	if identity.Name != "browser" {
		t.Errorf("expected name browser, got %s", identity.Name)
	}
}

func TestToolRegistry_RegisterMCPTool(t *testing.T) {
	resolver := NewResolver()
	reg := NewToolRegistry(resolver)

	err := reg.RegisterMCPTool("filesystem", "read_file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	identity, ok := reg.Resolve("mcp:filesystem.read_file")
	if !ok {
		t.Error("expected to find tool by canonical name")
	}
	if identity.Source != naming.SourceMCP {
		t.Errorf("expected source mcp, got %s", identity.Source)
	}
	if identity.Namespace != "filesystem" {
		t.Errorf("expected namespace filesystem, got %s", identity.Namespace)
	}
}

func TestToolRegistry_RegisterEdgeTool(t *testing.T) {
	resolver := NewResolver()
	reg := NewToolRegistry(resolver)

	err := reg.RegisterEdgeTool("macbook", "camera_snap")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	identity, ok := reg.Resolve("edge:macbook.camera_snap")
	if !ok {
		t.Error("expected to find tool by canonical name")
	}
	if identity.Source != naming.SourceEdge {
		t.Errorf("expected source edge, got %s", identity.Source)
	}
	if identity.Namespace != "macbook" {
		t.Errorf("expected namespace macbook, got %s", identity.Namespace)
	}
}

func TestToolRegistry_RegisterEdgeServer(t *testing.T) {
	resolver := NewResolver()
	reg := NewToolRegistry(resolver)

	err := reg.RegisterEdgeServer("phone", []string{"camera", "location", "contacts"}, TrustTOFU)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tools are registered
	for _, tool := range []string{"camera", "location", "contacts"} {
		canonical := "edge:phone." + tool
		if _, ok := reg.Resolve(canonical); !ok {
			t.Errorf("expected to find tool %s", canonical)
		}
	}

	// Verify trust level
	if level := reg.GetEdgeTrustLevel("phone"); level != TrustTOFU {
		t.Errorf("expected TOFU trust level, got %s", level)
	}

	// Verify group was created
	if _, ok := resolver.groups["edge:phone"]; !ok {
		t.Error("expected edge group to be created")
	}
}

func TestToolRegistry_UnregisterEdgeServer(t *testing.T) {
	resolver := NewResolver()
	reg := NewToolRegistry(resolver)

	// Register first
	err := reg.RegisterEdgeServer("phone", []string{"camera", "location"}, TrustTrusted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify registered
	if _, ok := reg.Resolve("edge:phone.camera"); !ok {
		t.Error("expected tool to be registered")
	}

	// Unregister
	reg.UnregisterEdgeServer("phone")

	// Verify unregistered
	if _, ok := reg.Resolve("edge:phone.camera"); ok {
		t.Error("expected tool to be unregistered")
	}
	if reg.GetEdgeTrustLevel("phone") != TrustUntrusted {
		t.Error("expected trust level to default to untrusted after unregister")
	}
}

func TestToolRegistry_BySource(t *testing.T) {
	reg := NewToolRegistry(nil)

	reg.RegisterCoreTool("browser")
	reg.RegisterCoreTool("sandbox")
	reg.RegisterMCPTool("fs", "read")
	reg.RegisterEdgeTool("edge1", "camera")

	core := reg.BySource(naming.SourceCore)
	if len(core) != 2 {
		t.Errorf("expected 2 core tools, got %d", len(core))
	}

	mcp := reg.BySource(naming.SourceMCP)
	if len(mcp) != 1 {
		t.Errorf("expected 1 MCP tool, got %d", len(mcp))
	}

	edge := reg.BySource(naming.SourceEdge)
	if len(edge) != 1 {
		t.Errorf("expected 1 edge tool, got %d", len(edge))
	}
}

func TestToolRegistry_Matching(t *testing.T) {
	reg := NewToolRegistry(nil)

	reg.RegisterCoreTool("browser")
	reg.RegisterCoreTool("sandbox")
	reg.RegisterMCPTool("fs", "read")
	reg.RegisterMCPTool("fs", "write")
	reg.RegisterEdgeTool("phone", "camera")

	// Match all core
	core := reg.Matching("core.*")
	if len(core) != 2 {
		t.Errorf("expected 2 core tools, got %d", len(core))
	}

	// Match specific MCP server
	fs := reg.Matching("mcp:fs.*")
	if len(fs) != 2 {
		t.Errorf("expected 2 fs tools, got %d", len(fs))
	}

	// Match all edge
	edge := reg.Matching("edge:*")
	if len(edge) != 1 {
		t.Errorf("expected 1 edge tool, got %d", len(edge))
	}

	// Match all
	all := reg.Matching("*")
	if len(all) != 5 {
		t.Errorf("expected 5 tools, got %d", len(all))
	}
}

func TestIsEdgeTool(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"edge:phone.camera", true},
		{"edge:macbook.screenshot", true},
		{"mcp:fs.read", false},
		{"core.browser", false},
		{"browser", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEdgeTool(tt.name); got != tt.expected {
				t.Errorf("IsEdgeTool(%s) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestParseEdgeToolName(t *testing.T) {
	tests := []struct {
		name       string
		wantEdgeID string
		wantTool   string
	}{
		{"edge:phone.camera", "phone", "camera"},
		{"edge:macbook.screenshot", "macbook", "screenshot"},
		{"edge:device", "device", ""},
		{"mcp:fs.read", "", ""},
		{"browser", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edgeID, tool := ParseEdgeToolName(tt.name)
			if edgeID != tt.wantEdgeID {
				t.Errorf("ParseEdgeToolName(%s) edgeID = %s, want %s", tt.name, edgeID, tt.wantEdgeID)
			}
			if tool != tt.wantTool {
				t.Errorf("ParseEdgeToolName(%s) tool = %s, want %s", tt.name, tool, tt.wantTool)
			}
		})
	}
}

func TestIdentifyTool(t *testing.T) {
	tests := []struct {
		name     string
		expected naming.ToolSource
	}{
		{"mcp:fs.read", naming.SourceMCP},
		{"mcp.fs.read", naming.SourceMCP},
		{"edge:phone.camera", naming.SourceEdge},
		{"core.browser", naming.SourceCore},
		{"browser", naming.SourceCore},
		{"sandbox", naming.SourceCore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IdentifyTool(tt.name); got != tt.expected {
				t.Errorf("IdentifyTool(%s) = %s, want %s", tt.name, got, tt.expected)
			}
		})
	}
}
