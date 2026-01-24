package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
	"github.com/spf13/cobra"
)

func TestSplitCommandPath(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"", nil},
		{"cmd", []string{"cmd"}},
		{"parent.child", []string{"parent", "child"}},
		{"parent/child", []string{"parent", "child"}},
		{"a.b.c", []string{"a", "b", "c"}},
		{"a/b/c", []string{"a", "b", "c"}},
		{"mixed.path/here", []string{"mixed", "path", "here"}},
		{".leading", []string{"leading"}},
		{"trailing.", []string{"trailing"}},
		{"..double..", []string{"double"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := splitCommandPath(tt.path)
			if len(result) != len(tt.expected) {
				t.Errorf("splitCommandPath(%q) = %v, want %v", tt.path, result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("splitCommandPath(%q)[%d] = %q, want %q", tt.path, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestFindCommand(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child1 := &cobra.Command{Use: "child1"}
	child2 := &cobra.Command{Use: "child2"}
	grandchild := &cobra.Command{Use: "grandchild"}

	root.AddCommand(child1)
	root.AddCommand(child2)
	child1.AddCommand(grandchild)

	tests := []struct {
		path     string
		expected string
		found    bool
	}{
		{"", "root", true},
		{"child1", "child1", true},
		{"child2", "child2", true},
		{"child1.grandchild", "grandchild", true},
		{"nonexistent", "", false},
		{"child1.nonexistent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			cmd := findCommand(root, tt.path)
			if tt.found {
				if cmd == nil {
					t.Errorf("findCommand(root, %q) = nil, want %q", tt.path, tt.expected)
				} else if cmd.Name() != tt.expected {
					t.Errorf("findCommand(root, %q).Name() = %q, want %q", tt.path, cmd.Name(), tt.expected)
				}
			} else {
				if cmd != nil {
					t.Errorf("findCommand(root, %q) = %v, want nil", tt.path, cmd.Name())
				}
			}
		})
	}
}

func TestConvertCLICommand(t *testing.T) {
	t.Run("basic command", func(t *testing.T) {
		cmd := &pluginsdk.CLICommand{
			Use:     "test",
			Short:   "Test command",
			Long:    "A longer description",
			Example: "test --flag",
		}

		cobraCmd := convertCLICommand(cmd)

		if cobraCmd.Use != "test" {
			t.Errorf("Use = %q, want %q", cobraCmd.Use, "test")
		}
		if cobraCmd.Short != "Test command" {
			t.Errorf("Short = %q, want %q", cobraCmd.Short, "Test command")
		}
		if cobraCmd.Long != "A longer description" {
			t.Errorf("Long = %q", cobraCmd.Long)
		}
		if cobraCmd.Example != "test --flag" {
			t.Errorf("Example = %q", cobraCmd.Example)
		}
	})

	t.Run("command with run function", func(t *testing.T) {
		ran := false
		cmd := &pluginsdk.CLICommand{
			Use: "runnable",
			Run: func(cmd *cobra.Command, args []string) error {
				ran = true
				return nil
			},
		}

		cobraCmd := convertCLICommand(cmd)
		if cobraCmd.RunE == nil {
			t.Error("RunE should be set")
		}

		// Execute the command
		cobraCmd.RunE(cobraCmd, nil)
		if !ran {
			t.Error("Run function was not called")
		}
	})

	t.Run("command with subcommands", func(t *testing.T) {
		cmd := &pluginsdk.CLICommand{
			Use: "parent",
			Subcommands: []*pluginsdk.CLICommand{
				{Use: "sub1", Short: "Sub 1"},
				{Use: "sub2", Short: "Sub 2"},
			},
		}

		cobraCmd := convertCLICommand(cmd)
		if len(cobraCmd.Commands()) != 2 {
			t.Errorf("len(Commands()) = %d, want 2", len(cobraCmd.Commands()))
		}
	})

	t.Run("command with flags", func(t *testing.T) {
		cmd := &pluginsdk.CLICommand{
			Use: "withflags",
			Flags: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "A name flag")
			},
		}

		cobraCmd := convertCLICommand(cmd)
		flag := cobraCmd.Flags().Lookup("name")
		if flag == nil {
			t.Error("Flag 'name' should be set")
		}
	})
}

func TestRuntimeCLIRegistry_RegisterCommand(t *testing.T) {
	t.Run("nil root", func(t *testing.T) {
		reg := &runtimeCLIRegistry{rootCmd: nil}
		err := reg.RegisterCommand(&pluginsdk.CLICommand{Use: "test"})
		if err == nil {
			t.Error("expected error for nil root")
		}
	})

	t.Run("nil command", func(t *testing.T) {
		root := &cobra.Command{Use: "root"}
		reg := &runtimeCLIRegistry{rootCmd: root}
		err := reg.RegisterCommand(nil)
		if err == nil {
			t.Error("expected error for nil command")
		}
	})

	t.Run("successful registration", func(t *testing.T) {
		root := &cobra.Command{Use: "root"}
		reg := &runtimeCLIRegistry{rootCmd: root, pluginID: "test-plugin"}

		cmd := &pluginsdk.CLICommand{Use: "newcmd", Short: "New command"}
		err := reg.RegisterCommand(cmd)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(root.Commands()) != 1 {
			t.Errorf("expected 1 command, got %d", len(root.Commands()))
		}
	})
}

func TestRuntimeCLIRegistry_RegisterSubcommand(t *testing.T) {
	t.Run("nil root", func(t *testing.T) {
		reg := &runtimeCLIRegistry{rootCmd: nil}
		err := reg.RegisterSubcommand("parent", &pluginsdk.CLICommand{Use: "test"})
		if err == nil {
			t.Error("expected error for nil root")
		}
	})

	t.Run("nil command", func(t *testing.T) {
		root := &cobra.Command{Use: "root"}
		reg := &runtimeCLIRegistry{rootCmd: root}
		err := reg.RegisterSubcommand("parent", nil)
		if err == nil {
			t.Error("expected error for nil command")
		}
	})

	t.Run("parent not found", func(t *testing.T) {
		root := &cobra.Command{Use: "root"}
		reg := &runtimeCLIRegistry{rootCmd: root}
		err := reg.RegisterSubcommand("nonexistent", &pluginsdk.CLICommand{Use: "test"})
		if err == nil {
			t.Error("expected error for nonexistent parent")
		}
	})

	t.Run("successful registration", func(t *testing.T) {
		root := &cobra.Command{Use: "root"}
		parent := &cobra.Command{Use: "parent"}
		root.AddCommand(parent)

		reg := &runtimeCLIRegistry{rootCmd: root}
		cmd := &pluginsdk.CLICommand{Use: "child", Short: "Child command"}
		err := reg.RegisterSubcommand("parent", cmd)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(parent.Commands()) != 1 {
			t.Errorf("expected 1 subcommand, got %d", len(parent.Commands()))
		}
	})
}

func TestNewServiceManager(t *testing.T) {
	t.Run("with nil logger", func(t *testing.T) {
		mgr := NewServiceManager(nil)
		if mgr == nil {
			t.Fatal("NewServiceManager returned nil")
		}
		if mgr.logger == nil {
			t.Error("logger should be set to default")
		}
	})
}

func TestServiceManager_StartAll(t *testing.T) {
	mgr := NewServiceManager(nil)

	startCalled := false
	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "test-svc",
			Start: func(ctx context.Context) error {
				startCalled = true
				return nil
			},
			Stop: func(ctx context.Context) error {
				return nil
			},
		},
		pluginID: "test-plugin",
	})

	err := mgr.StartAll(context.Background())
	if err != nil {
		t.Errorf("StartAll() error = %v", err)
	}
	if !startCalled {
		t.Error("Start was not called")
	}
	if !mgr.services[0].running {
		t.Error("service should be marked as running")
	}
}

func TestServiceManager_StartAll_AlreadyRunning(t *testing.T) {
	mgr := NewServiceManager(nil)

	callCount := 0
	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "test-svc",
			Start: func(ctx context.Context) error {
				callCount++
				return nil
			},
			Stop: func(ctx context.Context) error {
				return nil
			},
		},
		pluginID: "test-plugin",
		running:  true,
	})

	mgr.StartAll(context.Background())
	if callCount != 0 {
		t.Error("Start should not be called for already running service")
	}
}

func TestServiceManager_StartAll_Error(t *testing.T) {
	mgr := NewServiceManager(nil)

	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "failing-svc",
			Start: func(ctx context.Context) error {
				return errors.New("start failed")
			},
			Stop: func(ctx context.Context) error {
				return nil
			},
		},
		pluginID: "test-plugin",
	})

	// Should not return error, just log it
	err := mgr.StartAll(context.Background())
	if err != nil {
		t.Errorf("StartAll() error = %v", err)
	}
	if mgr.services[0].running {
		t.Error("service should not be marked as running on error")
	}
}

func TestServiceManager_StopAll(t *testing.T) {
	mgr := NewServiceManager(nil)

	stopCalled := false
	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "test-svc",
			Start: func(ctx context.Context) error {
				return nil
			},
			Stop: func(ctx context.Context) error {
				stopCalled = true
				return nil
			},
		},
		pluginID: "test-plugin",
		running:  true,
	})

	err := mgr.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll() error = %v", err)
	}
	if !stopCalled {
		t.Error("Stop was not called")
	}
	if mgr.services[0].running {
		t.Error("service should not be marked as running")
	}
}

func TestServiceManager_StopAll_NotRunning(t *testing.T) {
	mgr := NewServiceManager(nil)

	callCount := 0
	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "test-svc",
			Start: func(ctx context.Context) error {
				return nil
			},
			Stop: func(ctx context.Context) error {
				callCount++
				return nil
			},
		},
		pluginID: "test-plugin",
		running:  false,
	})

	mgr.StopAll(context.Background())
	if callCount != 0 {
		t.Error("Stop should not be called for non-running service")
	}
}

func TestServiceManager_HealthCheck(t *testing.T) {
	mgr := NewServiceManager(nil)

	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "healthy-svc",
			Start: func(ctx context.Context) error {
				return nil
			},
			Stop: func(ctx context.Context) error {
				return nil
			},
			HealthCheck: func(ctx context.Context) error {
				return nil
			},
		},
		pluginID: "test-plugin",
		running:  true,
	})

	mgr.services = append(mgr.services, &pluginService{
		def: &pluginsdk.Service{
			ID: "unhealthy-svc",
			Start: func(ctx context.Context) error {
				return nil
			},
			Stop: func(ctx context.Context) error {
				return nil
			},
			HealthCheck: func(ctx context.Context) error {
				return errors.New("unhealthy")
			},
		},
		pluginID: "test-plugin",
		running:  true,
	})

	results := mgr.HealthCheck(context.Background())

	if results["healthy-svc"] != nil {
		t.Errorf("healthy-svc should have nil error, got %v", results["healthy-svc"])
	}
	if results["unhealthy-svc"] == nil {
		t.Error("unhealthy-svc should have error")
	}
}

func TestServiceManager_Services(t *testing.T) {
	mgr := NewServiceManager(nil)

	svc1 := &pluginsdk.Service{ID: "svc1"}
	svc2 := &pluginsdk.Service{ID: "svc2"}

	mgr.services = append(mgr.services, &pluginService{def: svc1})
	mgr.services = append(mgr.services, &pluginService{def: svc2})

	services := mgr.Services()
	if len(services) != 2 {
		t.Errorf("len(Services()) = %d, want 2", len(services))
	}
}

func TestRuntimeServiceRegistry_RegisterService(t *testing.T) {
	t.Run("nil manager", func(t *testing.T) {
		reg := &runtimeServiceRegistry{manager: nil}
		err := reg.RegisterService(&pluginsdk.Service{ID: "test"})
		if err == nil {
			t.Error("expected error for nil manager")
		}
	})

	t.Run("nil service", func(t *testing.T) {
		reg := &runtimeServiceRegistry{manager: NewServiceManager(nil)}
		err := reg.RegisterService(nil)
		if err == nil {
			t.Error("expected error for nil service")
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		reg := &runtimeServiceRegistry{manager: NewServiceManager(nil)}
		err := reg.RegisterService(&pluginsdk.Service{
			ID:    "",
			Start: func(ctx context.Context) error { return nil },
			Stop:  func(ctx context.Context) error { return nil },
		})
		if err == nil {
			t.Error("expected error for empty ID")
		}
	})

	t.Run("nil Start", func(t *testing.T) {
		reg := &runtimeServiceRegistry{manager: NewServiceManager(nil)}
		err := reg.RegisterService(&pluginsdk.Service{
			ID:    "test",
			Start: nil,
			Stop:  func(ctx context.Context) error { return nil },
		})
		if err == nil {
			t.Error("expected error for nil Start")
		}
	})

	t.Run("nil Stop", func(t *testing.T) {
		reg := &runtimeServiceRegistry{manager: NewServiceManager(nil)}
		err := reg.RegisterService(&pluginsdk.Service{
			ID:    "test",
			Start: func(ctx context.Context) error { return nil },
			Stop:  nil,
		})
		if err == nil {
			t.Error("expected error for nil Stop")
		}
	})

	t.Run("successful registration", func(t *testing.T) {
		mgr := NewServiceManager(nil)
		reg := &runtimeServiceRegistry{manager: mgr, pluginID: "test-plugin"}
		err := reg.RegisterService(&pluginsdk.Service{
			ID:    "test-svc",
			Start: func(ctx context.Context) error { return nil },
			Stop:  func(ctx context.Context) error { return nil },
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(mgr.services) != 1 {
			t.Errorf("expected 1 service, got %d", len(mgr.services))
		}
	})
}

func TestPluginLoggerAdapter(t *testing.T) {
	// Just verify it can be instantiated
	_ = &pluginLoggerAdapter{logger: nil}
	// In production, logger would never be nil
}

func TestToChannelStatus(t *testing.T) {
	status := pluginsdk.Status{
		Connected: true,
		Error:     "some error",
		LastPing:  12345,
	}

	result := toChannelStatus(status)

	if result.Connected != true {
		t.Error("Connected should be true")
	}
	if result.Error != "some error" {
		t.Errorf("Error = %q", result.Error)
	}
	if result.LastPing != 12345 {
		t.Errorf("LastPing = %d", result.LastPing)
	}
}

func TestToChannelHealth(t *testing.T) {
	now := time.Now()
	status := pluginsdk.HealthStatus{
		Healthy:   true,
		Latency:   100 * time.Millisecond,
		Message:   "OK",
		LastCheck: now,
		Degraded:  false,
	}

	result := toChannelHealth(status)

	if result.Healthy != true {
		t.Error("Healthy should be true")
	}
	if result.Latency != 100*time.Millisecond {
		t.Errorf("Latency = %v", result.Latency)
	}
	if result.Message != "OK" {
		t.Errorf("Message = %q", result.Message)
	}
	if !result.LastCheck.Equal(now) {
		t.Errorf("LastCheck = %v", result.LastCheck)
	}
	if result.Degraded != false {
		t.Error("Degraded should be false")
	}
}

func TestRuntimeChannelRegistry_RegisterChannel(t *testing.T) {
	t.Run("nil registry", func(t *testing.T) {
		reg := &runtimeChannelRegistry{registry: nil}
		err := reg.RegisterChannel(&mockChannelAdapter{})
		if err == nil {
			t.Error("expected error for nil registry")
		}
	})

	t.Run("nil adapter", func(t *testing.T) {
		// Can't really test with non-nil registry without more setup
		reg := &runtimeChannelRegistry{registry: nil}
		err := reg.RegisterChannel(nil)
		if err == nil {
			t.Error("expected error for nil adapter")
		}
	})
}

func TestRuntimeToolRegistry_RegisterTool(t *testing.T) {
	t.Run("nil runtime", func(t *testing.T) {
		reg := &runtimeToolRegistry{runtime: nil}
		err := reg.RegisterTool(pluginsdk.ToolDefinition{Name: "test"}, nil)
		if err == nil {
			t.Error("expected error for nil runtime")
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		// Can't really test with non-nil runtime without more setup
		reg := &runtimeToolRegistry{runtime: nil}
		err := reg.RegisterTool(pluginsdk.ToolDefinition{Name: "test"}, nil)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		reg := &runtimeToolRegistry{runtime: nil}
		err := reg.RegisterTool(pluginsdk.ToolDefinition{Name: ""}, func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
			return nil, nil
		})
		if err == nil {
			t.Error("expected error for empty name")
		}
	})
}

func TestPluginTool(t *testing.T) {
	def := pluginsdk.ToolDefinition{
		Name:        "test-tool",
		Description: "A test tool",
		Schema:      json.RawMessage(`{"type": "object"}`),
	}

	tool := &pluginTool{
		definition: def,
		handler: func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
			return &pluginsdk.ToolResult{Content: "result", IsError: false}, nil
		},
	}

	if tool.Name() != "test-tool" {
		t.Errorf("Name() = %q", tool.Name())
	}
	if tool.Description() != "A test tool" {
		t.Errorf("Description() = %q", tool.Description())
	}
	if tool.Schema() == nil {
		t.Error("Schema() should not be nil")
	}

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}
	if result.Content != "result" {
		t.Errorf("result.Content = %q", result.Content)
	}
}

func TestPluginTool_NilResult(t *testing.T) {
	tool := &pluginTool{
		definition: pluginsdk.ToolDefinition{Name: "test"},
		handler: func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
			return nil, nil
		},
	}

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}
	if result.Content != "" {
		t.Errorf("result.Content = %q, want empty", result.Content)
	}
}

func TestPluginTool_Error(t *testing.T) {
	tool := &pluginTool{
		definition: pluginsdk.ToolDefinition{Name: "test"},
		handler: func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
			return nil, errors.New("handler error")
		},
	}

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error")
	}
}

func TestPluginServiceStruct(t *testing.T) {
	svc := pluginService{
		def:      &pluginsdk.Service{ID: "test"},
		pluginID: "plugin-1",
		running:  true,
	}

	if svc.def.ID != "test" {
		t.Errorf("def.ID = %q", svc.def.ID)
	}
	if svc.pluginID != "plugin-1" {
		t.Errorf("pluginID = %q", svc.pluginID)
	}
	if !svc.running {
		t.Error("running should be true")
	}
}

// mockChannelAdapter is a minimal implementation for testing.
type mockChannelAdapter struct{}

func (m *mockChannelAdapter) Type() models.ChannelType {
	return "mock"
}
