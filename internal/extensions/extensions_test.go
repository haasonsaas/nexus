package extensions

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/mcp"
)

func TestList_NilInputs(t *testing.T) {
	result := List(nil, nil)
	if len(result) != 0 {
		t.Fatalf("expected empty list for nil inputs, got %d", len(result))
	}
}

func TestList_PluginsOnly(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.Entries = map[string]config.PluginEntryConfig{
		"plugin-b": {Enabled: true, Path: "/path/b"},
		"plugin-a": {Enabled: false, Path: "/path/a"},
	}

	result := List(cfg, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(result))
	}

	// Should be sorted by ID.
	if result[0].ID != "plugin-a" {
		t.Fatalf("expected first plugin 'plugin-a', got %q", result[0].ID)
	}
	if result[0].Status != "disabled" {
		t.Fatalf("expected 'disabled' status, got %q", result[0].Status)
	}
	if result[1].ID != "plugin-b" {
		t.Fatalf("expected second plugin 'plugin-b', got %q", result[1].ID)
	}
	if result[1].Status != "enabled" {
		t.Fatalf("expected 'enabled' status, got %q", result[1].Status)
	}
	if result[0].Kind != KindPlugin {
		t.Fatalf("expected kind %q, got %q", KindPlugin, result[0].Kind)
	}
}

func TestList_MCPOnly(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP = mcp.Config{
		Enabled: true,
		Servers: []*mcp.ServerConfig{
			{ID: "server-1", Name: "Server One", Transport: mcp.TransportStdio, AutoStart: true},
			{ID: "server-2", Name: "", Transport: mcp.TransportHTTP, AutoStart: false},
			nil, // nil entries should be skipped
		},
	}

	result := List(cfg, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(result))
	}

	// Sorted by ID.
	if result[0].ID != "server-1" {
		t.Fatalf("expected 'server-1', got %q", result[0].ID)
	}
	if result[0].Name != "Server One" {
		t.Fatalf("expected name 'Server One', got %q", result[0].Name)
	}
	if result[0].Status != "auto_start" {
		t.Fatalf("expected 'auto_start', got %q", result[0].Status)
	}

	// server-2 has empty name, should fallback to ID.
	if result[1].Name != "server-2" {
		t.Fatalf("expected name fallback to ID 'server-2', got %q", result[1].Name)
	}
	if result[1].Status != "configured" {
		t.Fatalf("expected 'configured', got %q", result[1].Status)
	}
	if result[1].Source != string(mcp.TransportHTTP) {
		t.Fatalf("expected source %q, got %q", mcp.TransportHTTP, result[1].Source)
	}
}

func TestList_MCPDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP = mcp.Config{
		Enabled: false,
		Servers: []*mcp.ServerConfig{
			{ID: "server-1", Name: "S1", Transport: mcp.TransportStdio, AutoStart: true},
		},
	}

	result := List(cfg, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 extensions when MCP disabled, got %d", len(result))
	}
}

func TestList_SortedByKindThenID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.Entries = map[string]config.PluginEntryConfig{
		"zeta-plugin": {Enabled: true},
	}
	cfg.MCP = mcp.Config{
		Enabled: true,
		Servers: []*mcp.ServerConfig{
			{ID: "alpha-server", Name: "Alpha", Transport: mcp.TransportStdio},
		},
	}

	result := List(cfg, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(result))
	}
	// MCP kind ("mcp") < plugin kind ("plugin") alphabetically.
	if result[0].Kind != KindMCP {
		t.Fatalf("expected MCP first (sorted by kind), got %q", result[0].Kind)
	}
	if result[1].Kind != KindPlugin {
		t.Fatalf("expected plugin second, got %q", result[1].Kind)
	}
}
