package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewManager(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Servers: []*ServerConfig{
			{ID: "server1", Name: "Server 1", Transport: TransportStdio, Command: "echo"},
		},
	}

	mgr := NewManager(cfg, nil)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNewManagerNilConfig(t *testing.T) {
	mgr := NewManager(nil, nil)
	if mgr == nil {
		t.Fatal("expected non-nil manager even with nil config")
	}
}

func TestNewManagerNilLogger(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, nil)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManagerStartDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	mgr := NewManager(cfg, slog.Default())

	err := mgr.Start(context.Background())
	if err != nil {
		t.Errorf("Start() error = %v, expected nil for disabled manager", err)
	}
}

func TestManagerStop(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	err := mgr.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestManagerConnectServerNotFound(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Servers: []*ServerConfig{},
	}
	mgr := NewManager(cfg, slog.Default())

	err := mgr.Connect(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent server")
	}
}

func TestManagerDisconnectNotConnected(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	// Disconnecting a non-connected server should be a no-op
	err := mgr.Disconnect("server1")
	if err != nil {
		t.Errorf("Disconnect() error = %v, expected nil", err)
	}
}

func TestManagerClientNotFound(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	client, exists := mgr.Client("nonexistent")
	if exists {
		t.Error("expected exists to be false")
	}
	if client != nil {
		t.Error("expected client to be nil")
	}
}

func TestManagerClients(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	clients := mgr.Clients()
	if clients == nil {
		t.Error("expected non-nil clients map")
	}
	if len(clients) != 0 {
		t.Error("expected empty clients map")
	}
}

func TestManagerAllTools(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	tools := mgr.AllTools()
	if tools == nil {
		t.Error("expected non-nil tools map")
	}
	if len(tools) != 0 {
		t.Error("expected empty tools map")
	}
}

func TestManagerAllResources(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	resources := mgr.AllResources()
	if resources == nil {
		t.Error("expected non-nil resources map")
	}
	if len(resources) != 0 {
		t.Error("expected empty resources map")
	}
}

func TestManagerAllPrompts(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	prompts := mgr.AllPrompts()
	if prompts == nil {
		t.Error("expected non-nil prompts map")
	}
	if len(prompts) != 0 {
		t.Error("expected empty prompts map")
	}
}

func TestManagerCallToolServerNotConnected(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	_, err := mgr.CallTool(context.Background(), "server1", "tool1", nil)
	if err == nil {
		t.Error("expected error for not connected server")
	}
}

func TestManagerFindToolNotFound(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	serverID, tool := mgr.FindTool("nonexistent")
	if serverID != "" {
		t.Errorf("expected empty serverID, got %q", serverID)
	}
	if tool != nil {
		t.Error("expected nil tool")
	}
}

func TestManagerReadResourceServerNotConnected(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	_, err := mgr.ReadResource(context.Background(), "server1", "file://test.txt")
	if err == nil {
		t.Error("expected error for not connected server")
	}
}

func TestManagerGetPromptServerNotConnected(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	_, err := mgr.GetPrompt(context.Background(), "server1", "prompt1", nil)
	if err == nil {
		t.Error("expected error for not connected server")
	}
}

func TestManagerToolSchemas(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	schemas := mgr.ToolSchemas()
	// Empty list may be nil or empty slice, both are valid
	if len(schemas) != 0 {
		t.Error("expected empty schemas list")
	}
}

func TestManagerStatus(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Servers: []*ServerConfig{
			{ID: "server1", Name: "Server 1"},
			{ID: "server2", Name: "Server 2"},
		},
	}
	mgr := NewManager(cfg, slog.Default())

	statuses := mgr.Status()
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}

	for _, status := range statuses {
		if status.Connected {
			t.Error("expected all servers to be disconnected")
		}
	}
}

func TestManagerSetSamplingHandler(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	handler := func(ctx context.Context, req *SamplingRequest) (*SamplingResponse, error) {
		return &SamplingResponse{}, nil
	}

	// Should not panic
	mgr.SetSamplingHandler(handler)
}

func TestManagerSetSamplingHandlerNil(t *testing.T) {
	cfg := &Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	// Should not panic
	mgr.SetSamplingHandler(nil)
}

func TestToolSchemaJSON(t *testing.T) {
	schema := ToolSchema{
		ServerID:    "server1",
		Name:        "search",
		Description: "Search for files",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ToolSchema
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.ServerID != schema.ServerID {
		t.Errorf("expected ServerID %q, got %q", schema.ServerID, decoded.ServerID)
	}
	if decoded.Name != schema.Name {
		t.Errorf("expected Name %q, got %q", schema.Name, decoded.Name)
	}
}

func TestServerStatusJSON(t *testing.T) {
	status := ServerStatus{
		ID:        "server1",
		Name:      "Server 1",
		Connected: true,
		Server: ServerInfo{
			Name:    "MCP Server",
			Version: "1.0.0",
		},
		Tools:     5,
		Resources: 3,
		Prompts:   2,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded ServerStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.ID != status.ID {
		t.Errorf("expected ID %q, got %q", status.ID, decoded.ID)
	}
	if decoded.Connected != status.Connected {
		t.Errorf("expected Connected %v, got %v", status.Connected, decoded.Connected)
	}
	if decoded.Tools != status.Tools {
		t.Errorf("expected Tools %d, got %d", status.Tools, decoded.Tools)
	}
}
