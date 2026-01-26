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

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.AddCommand(&cobra.Command{Use: "parent"})

	builder := &PluginAPIBuilder{
		Channels:       channels.NewRegistry(),
		Tools:          runtime,
		RootCmd:        rootCmd,
		ServiceManager: NewServiceManager(nil),
		HookRegistry:   hooks.NewRegistry(nil),
		WorkspaceDir:   t.TempDir(),
	}

	manifest := &pluginsdk.Manifest{
		ID:           "test-plugin",
		ConfigSchema: json.RawMessage(`{"type":"object"}`),
		Tools:        []string{"allowed-tool"},
		Channels:     []string{string(models.ChannelTelegram)},
		Commands:     []string{"allowedcmd", "parent.child"},
		Services:     []string{"allowed-service"},
		Hooks:        []string{"session.created"},
	}

	api := builder.Build("test-plugin", map[string]any{}, manifest)

	if err := api.CLI.RegisterCommand(&pluginsdk.CLICommand{Use: "allowedcmd"}); err != nil {
		t.Fatalf("RegisterCommand(allowedcmd) error = %v", err)
	}

	if err := api.CLI.RegisterSubcommand("parent", &pluginsdk.CLICommand{Use: "child"}); err != nil {
		t.Fatalf("RegisterSubcommand(parent.child) error = %v", err)
	}

	err := api.CLI.RegisterCommand(&pluginsdk.CLICommand{Use: "forbidden"})
	if err == nil {
		t.Fatalf("RegisterCommand(forbidden) expected error")
	}
	if !strings.Contains(err.Error(), `plugin "test-plugin"`) {
		t.Fatalf("RegisterCommand(forbidden) error = %q; expected plugin id", err.Error())
	}

	err = api.CLI.RegisterSubcommand("parent", &pluginsdk.CLICommand{Use: "evil"})
	if err == nil {
		t.Fatalf("RegisterSubcommand(parent.evil) expected error")
	}
	if !strings.Contains(err.Error(), `plugin "test-plugin"`) {
		t.Fatalf("RegisterSubcommand(parent.evil) error = %q; expected plugin id", err.Error())
	}

	err = api.Tools.RegisterTool(pluginsdk.ToolDefinition{Name: "allowed-tool"}, func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
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

	if err := api.Services.RegisterService(&pluginsdk.Service{
		ID:    "allowed-service",
		Start: func(ctx context.Context) error { return nil },
		Stop:  func(ctx context.Context) error { return nil },
	}); err != nil {
		t.Fatalf("RegisterService(allowed-service) error = %v", err)
	}

	err = api.Services.RegisterService(&pluginsdk.Service{
		ID:    "forbidden-service",
		Start: func(ctx context.Context) error { return nil },
		Stop:  func(ctx context.Context) error { return nil },
	})
	if err == nil {
		t.Fatalf("RegisterService(forbidden-service) expected error")
	}
	if !strings.Contains(err.Error(), `plugin "test-plugin"`) {
		t.Fatalf("RegisterService(forbidden-service) error = %q; expected plugin id", err.Error())
	}

	if err := api.Hooks.RegisterHook(&pluginsdk.HookRegistration{
		EventType: "session.created",
		Handler:   func(ctx context.Context, event *pluginsdk.HookEvent) error { return nil },
	}); err != nil {
		t.Fatalf("RegisterHook(session.created) error = %v", err)
	}

	err = api.Hooks.RegisterHook(&pluginsdk.HookRegistration{
		EventType: "session.deleted",
		Handler:   func(ctx context.Context, event *pluginsdk.HookEvent) error { return nil },
	})
	if err == nil {
		t.Fatalf("RegisterHook(session.deleted) expected error")
	}
	if !strings.Contains(err.Error(), `plugin "test-plugin"`) {
		t.Fatalf("RegisterHook(session.deleted) error = %q; expected plugin id", err.Error())
	}
}
