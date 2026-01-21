package gateway

import (
	"log/slog"
	"testing"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubChannelAdapter struct {
	channel models.ChannelType
}

func (a stubChannelAdapter) Type() models.ChannelType {
	return a.channel
}

type stubChannelPlugin struct {
	id      models.ChannelType
	enabled bool
	builds  int
}

func (p *stubChannelPlugin) Manifest() ChannelPluginManifest {
	return ChannelPluginManifest{ID: p.id, Name: "stub"}
}

func (p *stubChannelPlugin) Enabled(cfg *config.Config) bool {
	return p.enabled
}

func (p *stubChannelPlugin) Build(cfg *config.Config, logger *slog.Logger) (channels.Adapter, error) {
	p.builds++
	return stubChannelAdapter{channel: p.id}, nil
}

func TestChannelPluginRegistryLazyLoad(t *testing.T) {
	registry := newChannelPluginRegistry()
	plugin := &stubChannelPlugin{id: models.ChannelTelegram, enabled: true}
	registry.Register(plugin)

	channelRegistry := channels.NewRegistry()
	cfg := &config.Config{}

	if err := registry.LoadEnabled(cfg, channelRegistry, slog.Default()); err != nil {
		t.Fatalf("LoadEnabled() error = %v", err)
	}
	if err := registry.LoadEnabled(cfg, channelRegistry, slog.Default()); err != nil {
		t.Fatalf("LoadEnabled() error = %v", err)
	}
	if plugin.builds != 1 {
		t.Fatalf("expected plugin to build once, got %d", plugin.builds)
	}
}

func TestChannelPluginRegistrySkipsDisabled(t *testing.T) {
	registry := newChannelPluginRegistry()
	plugin := &stubChannelPlugin{id: models.ChannelDiscord, enabled: false}
	registry.Register(plugin)

	channelRegistry := channels.NewRegistry()
	cfg := &config.Config{}

	if err := registry.LoadEnabled(cfg, channelRegistry, slog.Default()); err != nil {
		t.Fatalf("LoadEnabled() error = %v", err)
	}
	if plugin.builds != 0 {
		t.Fatalf("expected plugin not to build, got %d", plugin.builds)
	}
	if _, ok := channelRegistry.Get(plugin.id); ok {
		t.Fatalf("expected no adapter registered for disabled plugin")
	}
}
