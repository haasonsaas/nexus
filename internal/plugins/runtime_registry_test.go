package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

type stubRuntimePlugin struct {
	id            string
	channelsCalls int
	toolsCalls    int
	manifest      *pluginsdk.Manifest
}

func (p *stubRuntimePlugin) Manifest() *pluginsdk.Manifest {
	if p.manifest != nil {
		return p.manifest
	}
	return &pluginsdk.Manifest{
		ID:           p.id,
		ConfigSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (p *stubRuntimePlugin) RegisterChannels(registry pluginsdk.ChannelRegistry, cfg map[string]any) error {
	p.channelsCalls++
	return registry.RegisterChannel(stubPluginAdapter{channel: models.ChannelTelegram})
}

func (p *stubRuntimePlugin) RegisterTools(registry pluginsdk.ToolRegistry, cfg map[string]any) error {
	p.toolsCalls++
	return registry.RegisterTool(pluginsdk.ToolDefinition{
		Name:        "stub",
		Description: "stub tool",
		Schema:      json.RawMessage(`{"type":"object"}`),
	}, func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
		return &pluginsdk.ToolResult{Content: "ok"}, nil
	})
}

type stubPluginAdapter struct {
	channel models.ChannelType
}

func (a stubPluginAdapter) Type() models.ChannelType { return a.channel }

type stubProvider struct{}

func (stubProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk)
	close(ch)
	return ch, nil
}

func (stubProvider) Name() string          { return "stub" }
func (stubProvider) Models() []agent.Model { return nil }
func (stubProvider) SupportsTools() bool   { return false }

type stubStore struct{}

func (stubStore) Create(ctx context.Context, session *models.Session) error { return nil }
func (stubStore) Get(ctx context.Context, id string) (*models.Session, error) {
	return nil, nil
}
func (stubStore) Update(ctx context.Context, session *models.Session) error { return nil }
func (stubStore) Delete(ctx context.Context, id string) error               { return nil }
func (stubStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	return nil, nil
}
func (stubStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	return nil, nil
}
func (stubStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	return nil, nil
}
func (stubStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	return nil
}
func (stubStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	return nil, nil
}

func TestRuntimeRegistryLoadsChannelsAndToolsOnce(t *testing.T) {
	registry := NewRuntimeRegistry()
	plugin := &stubRuntimePlugin{id: "stub-plugin"}
	if err := registry.Register(plugin); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Entries: map[string]config.PluginEntryConfig{
				"stub-plugin": {Enabled: true, Config: map[string]any{}},
			},
		},
	}

	channelRegistry := channels.NewRegistry()
	if err := registry.LoadChannels(cfg, channelRegistry); err != nil {
		t.Fatalf("LoadChannels() error = %v", err)
	}
	if err := registry.LoadChannels(cfg, channelRegistry); err != nil {
		t.Fatalf("LoadChannels() error = %v", err)
	}

	if plugin.channelsCalls != 1 {
		t.Fatalf("expected channels to register once, got %d", plugin.channelsCalls)
	}

	runtime := agent.NewRuntime(stubProvider{}, stubStore{})
	if err := registry.LoadTools(cfg, runtime); err != nil {
		t.Fatalf("LoadTools() error = %v", err)
	}
	if err := registry.LoadTools(cfg, runtime); err != nil {
		t.Fatalf("LoadTools() error = %v", err)
	}
	if plugin.toolsCalls != 1 {
		t.Fatalf("expected tools to register once, got %d", plugin.toolsCalls)
	}
}

func TestRuntimeRegistrySkipsDisabled(t *testing.T) {
	registry := NewRuntimeRegistry()
	plugin := &stubRuntimePlugin{id: "stub-plugin"}
	if err := registry.Register(plugin); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Entries: map[string]config.PluginEntryConfig{
				"stub-plugin": {Enabled: false, Config: map[string]any{}},
			},
		},
	}

	if err := registry.LoadChannels(cfg, channels.NewRegistry()); err != nil {
		t.Fatalf("LoadChannels() error = %v", err)
	}
	if plugin.channelsCalls != 0 {
		t.Fatalf("expected no channels registration, got %d", plugin.channelsCalls)
	}
}

func TestRuntimeRegistryCapabilitiesAllowed(t *testing.T) {
	registry := NewRuntimeRegistry()
	manifest := &pluginsdk.Manifest{
		ID:           "stub-plugin",
		ConfigSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Capabilities: &pluginsdk.Capabilities{
			Required: []string{"channel:telegram", "tool:stub"},
		},
	}
	plugin := &stubRuntimePlugin{id: "stub-plugin", manifest: manifest}
	if err := registry.Register(plugin); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Entries: map[string]config.PluginEntryConfig{
				"stub-plugin": {Enabled: true, Config: map[string]any{}},
			},
		},
	}

	if err := registry.LoadChannels(cfg, channels.NewRegistry()); err != nil {
		t.Fatalf("LoadChannels() error = %v", err)
	}
	runtime := agent.NewRuntime(stubProvider{}, stubStore{})
	if err := registry.LoadTools(cfg, runtime); err != nil {
		t.Fatalf("LoadTools() error = %v", err)
	}
}

func TestRuntimeRegistryCapabilitiesDenied(t *testing.T) {
	registry := NewRuntimeRegistry()
	manifest := &pluginsdk.Manifest{
		ID:           "stub-plugin",
		ConfigSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Capabilities: &pluginsdk.Capabilities{
			Required: []string{"tool:stub"},
		},
	}
	plugin := &stubRuntimePlugin{id: "stub-plugin", manifest: manifest}
	if err := registry.Register(plugin); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Entries: map[string]config.PluginEntryConfig{
				"stub-plugin": {Enabled: true, Config: map[string]any{}},
			},
		},
	}

	err := registry.LoadChannels(cfg, channels.NewRegistry())
	if err == nil {
		t.Fatal("expected LoadChannels() to return an error")
	}
	if !strings.Contains(err.Error(), "capability") {
		t.Fatalf("expected capability error, got %v", err)
	}
}

func TestRuntimeRegistryAllowsIsolationEnabled(t *testing.T) {
	registry := NewRuntimeRegistry()
	plugin := &stubRuntimePlugin{id: "stub-plugin"}
	if err := registry.Register(plugin); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Isolation: config.PluginIsolationConfig{
				Enabled: true,
			},
			Entries: map[string]config.PluginEntryConfig{
				"stub-plugin": {Enabled: true, Config: map[string]any{}},
			},
		},
	}

	runtime := agent.NewRuntime(stubProvider{}, stubStore{})
	if err := registry.LoadTools(cfg, runtime); err != nil {
		t.Fatalf("LoadTools() error = %v", err)
	}
	if plugin.toolsCalls != 1 {
		t.Fatalf("expected tools to register once, got %d", plugin.toolsCalls)
	}
}
