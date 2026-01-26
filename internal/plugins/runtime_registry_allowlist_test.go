package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
	"github.com/spf13/cobra"
)

type stubChannelAdapter struct {
	channel models.ChannelType
}

func (a stubChannelAdapter) Type() models.ChannelType { return a.channel }

func TestPluginAPIBuilderBuild_EnforcesManifestAllowlists(t *testing.T) {
	runtime := agent.NewRuntime(stubProvider{}, stubStore{})

	builder := &PluginAPIBuilder{
		Channels:       channels.NewRegistry(),
		Tools:          runtime,
		RootCmd:        &cobra.Command{Use: "root"},
		ServiceManager: NewServiceManager(nil),
		HookRegistry:   hooks.NewRegistry(nil),
		WorkspaceDir:   t.TempDir(),
	}

	manifest := &pluginsdk.Manifest{
		ID:           "test-plugin",
		ConfigSchema: json.RawMessage(`{"type":"object"}`),
		Tools:        []string{"allowed-tool"},
		Channels:     []string{string(models.ChannelTelegram)},
	}

	api := builder.Build("test-plugin", map[string]any{}, manifest)

	err := api.Tools.RegisterTool(pluginsdk.ToolDefinition{Name: "allowed-tool"}, func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
		return &pluginsdk.ToolResult{Content: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("RegisterTool(allowed-tool) error = %v", err)
	}

	err = api.Tools.RegisterTool(pluginsdk.ToolDefinition{Name: "forbidden-tool"}, func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
		return &pluginsdk.ToolResult{Content: "ok"}, nil
	})
	if err == nil {
		t.Fatalf("RegisterTool(forbidden-tool) expected error")
	}
	if !strings.Contains(err.Error(), `plugin "test-plugin"`) {
		t.Fatalf("RegisterTool(forbidden-tool) error = %q; expected plugin id", err.Error())
	}

	if err := api.Channels.RegisterChannel(stubChannelAdapter{channel: models.ChannelTelegram}); err != nil {
		t.Fatalf("RegisterChannel(telegram) error = %v", err)
	}

	err = api.Channels.RegisterChannel(stubChannelAdapter{channel: models.ChannelSlack})
	if err == nil {
		t.Fatalf("RegisterChannel(slack) expected error")
	}
	if !strings.Contains(err.Error(), `plugin "test-plugin"`) {
		t.Fatalf("RegisterChannel(slack) error = %q; expected plugin id", err.Error())
	}
}
