package managers

import (
	"context"
	"errors"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

// mockManager is a test double for the Manager interface.
type mockManager struct {
	startCalled bool
	stopCalled  bool
	startErr    error
	stopErr     error
	startOrder  *[]string
	stopOrder   *[]string
	name        string
}

func (m *mockManager) Start(ctx context.Context) error {
	m.startCalled = true
	if m.startOrder != nil {
		*m.startOrder = append(*m.startOrder, m.name)
	}
	return m.startErr
}

func (m *mockManager) Stop(ctx context.Context) error {
	m.stopCalled = true
	if m.stopOrder != nil {
		*m.stopOrder = append(*m.stopOrder, m.name)
	}
	return m.stopErr
}

// testableManagers is a version of Managers that uses the Manager interface
// for easier testing without requiring full RuntimeManager setup.
type testableManagers struct {
	managers []Manager
}

func (t *testableManagers) StartAll(ctx context.Context) error {
	var started []Manager
	for _, mgr := range t.managers {
		if mgr == nil {
			continue
		}
		if err := mgr.Start(ctx); err != nil {
			// Stop already-started managers in reverse order
			for i := len(started) - 1; i >= 0; i-- {
				_ = started[i].Stop(ctx)
			}
			return err
		}
		started = append(started, mgr)
	}
	return nil
}

func (t *testableManagers) StopAll(ctx context.Context) error {
	var firstErr error
	for i := len(t.managers) - 1; i >= 0; i-- {
		mgr := t.managers[i]
		if mgr == nil {
			continue
		}
		if err := mgr.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func TestStartAllLogic(t *testing.T) {
	t.Run("starts all managers in order", func(t *testing.T) {
		var order []string

		mgrs := &testableManagers{
			managers: []Manager{
				&mockManager{name: "runtime", startOrder: &order},
				&mockManager{name: "tooling", startOrder: &order},
				&mockManager{name: "channel", startOrder: &order},
				&mockManager{name: "scheduler", startOrder: &order},
			},
		}

		err := mgrs.StartAll(context.Background())
		if err != nil {
			t.Errorf("StartAll error: %v", err)
		}

		expected := []string{"runtime", "tooling", "channel", "scheduler"}
		if len(order) != len(expected) {
			t.Errorf("start order length = %d, want %d", len(order), len(expected))
		}
		for i, name := range expected {
			if i < len(order) && order[i] != name {
				t.Errorf("order[%d] = %q, want %q", i, order[i], name)
			}
		}
	})

	t.Run("handles nil managers", func(t *testing.T) {
		mgrs := &testableManagers{
			managers: []Manager{nil, nil, nil},
		}

		err := mgrs.StartAll(context.Background())
		if err != nil {
			t.Errorf("StartAll with nil managers should not error: %v", err)
		}
	})

	t.Run("stops started managers on error", func(t *testing.T) {
		var startOrder []string
		var stopOrder []string

		mgrs := &testableManagers{
			managers: []Manager{
				&mockManager{name: "runtime", startOrder: &startOrder, stopOrder: &stopOrder},
				&mockManager{name: "tooling", startOrder: &startOrder, stopOrder: &stopOrder},
				&mockManager{
					name:       "channel",
					startOrder: &startOrder,
					stopOrder:  &stopOrder,
					startErr:   errors.New("channel start failed"),
				},
				&mockManager{name: "scheduler", startOrder: &startOrder, stopOrder: &stopOrder},
			},
		}

		err := mgrs.StartAll(context.Background())
		if err == nil {
			t.Error("expected error from StartAll")
		}
		if err.Error() != "channel start failed" {
			t.Errorf("error = %q, want %q", err.Error(), "channel start failed")
		}

		// Verify runtime and tooling were started before channel
		if len(startOrder) != 3 {
			t.Errorf("expected 3 start attempts, got %d: %v", len(startOrder), startOrder)
		}

		// Verify rollback - tooling and runtime should be stopped in reverse order
		if len(stopOrder) != 2 {
			t.Errorf("expected 2 stops for rollback, got %d: %v", len(stopOrder), stopOrder)
		}
		if len(stopOrder) >= 2 {
			if stopOrder[0] != "tooling" || stopOrder[1] != "runtime" {
				t.Errorf("stop order = %v, want [tooling, runtime]", stopOrder)
			}
		}
	})

	t.Run("returns first manager error", func(t *testing.T) {
		mgrs := &testableManagers{
			managers: []Manager{
				&mockManager{name: "runtime", startErr: errors.New("runtime failed")},
			},
		}

		err := mgrs.StartAll(context.Background())
		if err == nil || err.Error() != "runtime failed" {
			t.Errorf("error = %v, want 'runtime failed'", err)
		}
	})
}

func TestStopAllLogic(t *testing.T) {
	t.Run("stops all managers in reverse order", func(t *testing.T) {
		var order []string

		mgrs := &testableManagers{
			managers: []Manager{
				&mockManager{name: "runtime", stopOrder: &order},
				&mockManager{name: "tooling", stopOrder: &order},
				&mockManager{name: "channel", stopOrder: &order},
				&mockManager{name: "scheduler", stopOrder: &order},
			},
		}

		err := mgrs.StopAll(context.Background())
		if err != nil {
			t.Errorf("StopAll error: %v", err)
		}

		// Verify reverse order
		expected := []string{"scheduler", "channel", "tooling", "runtime"}
		if len(order) != len(expected) {
			t.Errorf("stop order length = %d, want %d", len(order), len(expected))
		}
		for i, name := range expected {
			if i < len(order) && order[i] != name {
				t.Errorf("order[%d] = %q, want %q", i, order[i], name)
			}
		}
	})

	t.Run("handles nil managers", func(t *testing.T) {
		mgrs := &testableManagers{managers: nil}

		err := mgrs.StopAll(context.Background())
		if err != nil {
			t.Errorf("StopAll with nil managers should not error: %v", err)
		}
	})

	t.Run("continues on error and returns first", func(t *testing.T) {
		var stopOrder []string

		mgrs := &testableManagers{
			managers: []Manager{
				&mockManager{name: "runtime", stopOrder: &stopOrder},
				&mockManager{name: "tooling", stopOrder: &stopOrder},
				&mockManager{
					name:      "channel",
					stopOrder: &stopOrder,
					stopErr:   errors.New("channel stop failed"),
				},
				&mockManager{
					name:      "scheduler",
					stopOrder: &stopOrder,
					stopErr:   errors.New("scheduler stop failed"),
				},
			},
		}

		err := mgrs.StopAll(context.Background())

		// Should return first error (scheduler, since we stop in reverse)
		if err == nil || err.Error() != "scheduler stop failed" {
			t.Errorf("error = %v, want 'scheduler stop failed'", err)
		}

		// Should still stop all managers
		if len(stopOrder) != 4 {
			t.Errorf("expected 4 stops, got %d: %v", len(stopOrder), stopOrder)
		}
	})
}

func TestManagerConfig(t *testing.T) {
	t.Run("can hold logger", func(t *testing.T) {
		cfg := ManagerConfig{
			Logger: nil,
		}
		if cfg.Logger != nil {
			t.Error("expected nil logger")
		}
	})
}

func TestManagers_Struct(t *testing.T) {
	t.Run("Managers struct fields exist", func(t *testing.T) {
		// Verify the Managers struct has the expected fields
		m := &Managers{}
		_ = m.Runtime
		_ = m.Channel
		_ = m.Scheduler
		_ = m.Tooling
	})
}

func TestNewRuntimeManager(t *testing.T) {
	t.Run("creates manager with nil logger", func(t *testing.T) {
		m := NewRuntimeManager(RuntimeManagerConfig{})
		if m == nil {
			t.Fatal("expected non-nil manager")
		}
		if m.logger == nil {
			t.Error("logger should default to slog.Default()")
		}
	})

	t.Run("uses provided logger", func(t *testing.T) {
		m := NewRuntimeManager(RuntimeManagerConfig{
			Logger: nil, // Will still use default
		})
		if m == nil {
			t.Fatal("expected non-nil manager")
		}
	})
}

func TestNewChannelManager(t *testing.T) {
	t.Run("creates manager with nil logger", func(t *testing.T) {
		m := NewChannelManager(ChannelManagerConfig{})
		if m == nil {
			t.Fatal("expected non-nil manager")
		}
		if m.logger == nil {
			t.Error("logger should default to slog.Default()")
		}
		if m.registry == nil {
			t.Error("registry should be initialized")
		}
	})

	t.Run("accessors return nil before start", func(t *testing.T) {
		m := NewChannelManager(ChannelManagerConfig{})
		if m.MediaProcessor() != nil {
			t.Error("MediaProcessor should be nil when not provided")
		}
		if m.MediaAggregator() != nil {
			t.Error("MediaAggregator should be nil when not provided")
		}
	})
}

func TestNewToolingManager(t *testing.T) {
	t.Run("creates manager with nil logger", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{})
		if m == nil {
			t.Fatal("expected non-nil manager")
		}
		if m.logger == nil {
			t.Error("logger should default to slog.Default()")
		}
	})

	t.Run("creates default policy resolver", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{})
		if m.PolicyResolver() == nil {
			t.Error("PolicyResolver should be created by default")
		}
	})

	t.Run("creates default hooks registry", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{})
		if m.HooksRegistry() == nil {
			t.Error("HooksRegistry should be created by default")
		}
	})

	t.Run("accessors return nil before initialization", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{})
		if m.BrowserPool() != nil {
			t.Error("BrowserPool should be nil before initialization")
		}
		if m.FirecrackerBackend() != nil {
			t.Error("FirecrackerBackend should be nil before initialization")
		}
		if m.MCPManager() != nil {
			t.Error("MCPManager should be nil when not provided")
		}
	})
}

func TestNewSchedulerManager(t *testing.T) {
	t.Run("creates manager with nil config", func(t *testing.T) {
		// Needs at minimum a config, even if empty
		cfg := &config.Config{}
		m, err := NewSchedulerManager(SchedulerManagerConfig{
			Config: cfg,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m == nil {
			t.Fatal("expected non-nil manager")
		}
		if m.logger == nil {
			t.Error("logger should default to slog.Default()")
		}
	})

	t.Run("accessors return nil before start", func(t *testing.T) {
		cfg := &config.Config{}
		m, err := NewSchedulerManager(SchedulerManagerConfig{
			Config: cfg,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.CronScheduler() != nil {
			t.Error("CronScheduler should be nil when not enabled")
		}
		if m.TaskScheduler() != nil {
			t.Error("TaskScheduler should be nil before start")
		}
		if m.TaskStore() != nil {
			t.Error("TaskStore should be nil when not provided")
		}
		if m.JobStore() != nil {
			t.Error("JobStore should be nil when not provided")
		}
	})

	t.Run("SetRuntime and SetSessions work", func(t *testing.T) {
		cfg := &config.Config{}
		m, err := NewSchedulerManager(SchedulerManagerConfig{
			Config: cfg,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// These should not panic
		m.SetRuntime(nil)
		m.SetSessions(nil)
	})
}

func TestChannelManager_Registry(t *testing.T) {
	m := NewChannelManager(ChannelManagerConfig{})
	registry := m.Registry()
	if registry == nil {
		t.Error("Registry() should return non-nil registry")
	}
}

func TestChannelManager_StartStop(t *testing.T) {
	t.Run("double start is idempotent", func(t *testing.T) {
		m := NewChannelManager(ChannelManagerConfig{})
		ctx := context.Background()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("first Start() error: %v", err)
		}
		if err := m.Start(ctx); err != nil {
			t.Errorf("second Start() should be idempotent, got: %v", err)
		}
	})

	t.Run("double stop is idempotent", func(t *testing.T) {
		m := NewChannelManager(ChannelManagerConfig{})
		ctx := context.Background()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("Start() error: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Fatalf("first Stop() error: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Errorf("second Stop() should be idempotent, got: %v", err)
		}
	})

	t.Run("stop without start is safe", func(t *testing.T) {
		m := NewChannelManager(ChannelManagerConfig{})
		ctx := context.Background()

		if err := m.Stop(ctx); err != nil {
			t.Errorf("Stop() without Start() should be safe, got: %v", err)
		}
	})
}

func TestToolingManager_StartStop(t *testing.T) {
	t.Run("double start is idempotent", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{
			Config: &config.Config{},
		})
		ctx := context.Background()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("first Start() error: %v", err)
		}
		if err := m.Start(ctx); err != nil {
			t.Errorf("second Start() should be idempotent, got: %v", err)
		}
	})

	t.Run("double stop is idempotent", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{
			Config: &config.Config{},
		})
		ctx := context.Background()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("Start() error: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Fatalf("first Stop() error: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Errorf("second Stop() should be idempotent, got: %v", err)
		}
	})

	t.Run("stop without start is safe", func(t *testing.T) {
		m := NewToolingManager(ToolingManagerConfig{
			Config: &config.Config{},
		})
		ctx := context.Background()

		if err := m.Stop(ctx); err != nil {
			t.Errorf("Stop() without Start() should be safe, got: %v", err)
		}
	})
}

func TestToolingManager_TriggerHook(t *testing.T) {
	t.Run("TriggerHook with nil registry is safe", func(t *testing.T) {
		m := &ToolingManager{} // No hooks registry
		ctx := context.Background()

		err := m.TriggerHook(ctx, nil)
		if err != nil {
			t.Errorf("TriggerHook with nil registry should be safe, got: %v", err)
		}
	})

	t.Run("TriggerHookAsync with nil registry is safe", func(t *testing.T) {
		m := &ToolingManager{} // No hooks registry
		ctx := context.Background()

		// Should not panic
		m.TriggerHookAsync(ctx, nil)
	})
}

func TestManagers_Struct_Fields(t *testing.T) {
	// Verify the struct has all expected fields
	m := &Managers{}

	// These nil checks ensure the struct fields exist and are the expected types
	if m.Runtime != nil {
		t.Error("Runtime should be nil for new struct")
	}
	if m.Channel != nil {
		t.Error("Channel should be nil for new struct")
	}
	if m.Scheduler != nil {
		t.Error("Scheduler should be nil for new struct")
	}
	if m.Tooling != nil {
		t.Error("Tooling should be nil for new struct")
	}
}

func TestRuntimeManager_Accessors(t *testing.T) {
	m := NewRuntimeManager(RuntimeManagerConfig{})

	// All accessors should return nil before start
	if m.Sessions() != nil {
		t.Error("Sessions should be nil before start")
	}
	if m.BranchStore() != nil {
		t.Error("BranchStore should be nil before start")
	}
	if m.VectorMemory() != nil {
		t.Error("VectorMemory should be nil when not provided")
	}
	if m.SkillsManager() != nil {
		t.Error("SkillsManager should be nil when not provided")
	}
	if m.ApprovalChecker() != nil {
		t.Error("ApprovalChecker should be nil before start")
	}
	if m.LLMProvider() != nil {
		t.Error("LLMProvider should be nil before start")
	}
	if m.DefaultModel() != "" {
		t.Errorf("DefaultModel should be empty, got %q", m.DefaultModel())
	}
}

func TestRuntimeManager_StopBeforeStart(t *testing.T) {
	m := NewRuntimeManager(RuntimeManagerConfig{})
	ctx := context.Background()

	// Stop without start should be safe
	err := m.Stop(ctx)
	if err != nil {
		t.Errorf("Stop without Start should be safe, got: %v", err)
	}
}

func TestRuntimeManagerConfig_Struct(t *testing.T) {
	cfg := RuntimeManagerConfig{
		Config:        &config.Config{},
		Logger:        nil,
		SkillsManager: nil,
		VectorMemory:  nil,
	}

	if cfg.Config == nil {
		t.Error("Config should not be nil")
	}
}

func TestChannelManagerConfig_Struct(t *testing.T) {
	cfg := ChannelManagerConfig{
		Config: &config.Config{},
		Logger: nil,
	}

	if cfg.Config == nil {
		t.Error("Config should not be nil")
	}
}

func TestToolingManagerConfig_Struct(t *testing.T) {
	cfg := ToolingManagerConfig{
		Config: &config.Config{},
		Logger: nil,
	}

	if cfg.Config == nil {
		t.Error("Config should not be nil")
	}
}

func TestSchedulerManagerConfig_Struct(t *testing.T) {
	cfg := SchedulerManagerConfig{
		Config: &config.Config{},
		Logger: nil,
	}

	if cfg.Config == nil {
		t.Error("Config should not be nil")
	}
}

func TestSchedulerManager_StartStop(t *testing.T) {
	t.Run("double start is idempotent", func(t *testing.T) {
		m, err := NewSchedulerManager(SchedulerManagerConfig{
			Config: &config.Config{},
		})
		if err != nil {
			t.Fatalf("NewSchedulerManager error: %v", err)
		}
		ctx := context.Background()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("first Start() error: %v", err)
		}
		if err := m.Start(ctx); err != nil {
			t.Errorf("second Start() should be idempotent, got: %v", err)
		}
	})

	t.Run("double stop is idempotent", func(t *testing.T) {
		m, err := NewSchedulerManager(SchedulerManagerConfig{
			Config: &config.Config{},
		})
		if err != nil {
			t.Fatalf("NewSchedulerManager error: %v", err)
		}
		ctx := context.Background()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("Start() error: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Fatalf("first Stop() error: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Errorf("second Stop() should be idempotent, got: %v", err)
		}
	})

	t.Run("stop without start is safe", func(t *testing.T) {
		m, err := NewSchedulerManager(SchedulerManagerConfig{
			Config: &config.Config{},
		})
		if err != nil {
			t.Fatalf("NewSchedulerManager error: %v", err)
		}
		ctx := context.Background()

		if err := m.Stop(ctx); err != nil {
			t.Errorf("Stop() without Start() should be safe, got: %v", err)
		}
	})
}

func TestRuntimeManager_StartStop(t *testing.T) {
	t.Run("double stop is idempotent", func(t *testing.T) {
		m := NewRuntimeManager(RuntimeManagerConfig{})
		ctx := context.Background()

		// Stop twice without start should be safe
		if err := m.Stop(ctx); err != nil {
			t.Errorf("first Stop() should be safe, got: %v", err)
		}
		if err := m.Stop(ctx); err != nil {
			t.Errorf("second Stop() should be safe, got: %v", err)
		}
	})
}

func TestToolingManager_Accessors(t *testing.T) {
	m := NewToolingManager(ToolingManagerConfig{})

	// PolicyResolver should be created by default
	if m.PolicyResolver() == nil {
		t.Error("PolicyResolver should not be nil")
	}

	// HooksRegistry should be created by default
	if m.HooksRegistry() == nil {
		t.Error("HooksRegistry should not be nil")
	}

	// BrowserPool should be nil before start
	if m.BrowserPool() != nil {
		t.Error("BrowserPool should be nil before start")
	}

	// FirecrackerBackend should be nil when not configured
	if m.FirecrackerBackend() != nil {
		t.Error("FirecrackerBackend should be nil when not configured")
	}

	// MCPManager should be nil when not provided
	if m.MCPManager() != nil {
		t.Error("MCPManager should be nil when not provided")
	}
}

func TestChannelManager_Accessors(t *testing.T) {
	m := NewChannelManager(ChannelManagerConfig{})

	// Registry should be initialized
	if m.Registry() == nil {
		t.Error("Registry should not be nil")
	}

	// MediaProcessor should be nil when not provided
	if m.MediaProcessor() != nil {
		t.Error("MediaProcessor should be nil when not provided")
	}

	// MediaAggregator should be nil when not provided
	if m.MediaAggregator() != nil {
		t.Error("MediaAggregator should be nil when not provided")
	}
}

func TestSchedulerManager_Accessors(t *testing.T) {
	cfg := &config.Config{}
	m, err := NewSchedulerManager(SchedulerManagerConfig{
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("NewSchedulerManager error: %v", err)
	}

	// All should be nil before start
	if m.CronScheduler() != nil {
		t.Error("CronScheduler should be nil before start")
	}
	if m.TaskScheduler() != nil {
		t.Error("TaskScheduler should be nil before start")
	}
	if m.TaskStore() != nil {
		t.Error("TaskStore should be nil before start")
	}
	if m.JobStore() != nil {
		t.Error("JobStore should be nil before start")
	}
}

func TestManagerInterface(t *testing.T) {
	// Verify all managers implement the Manager interface
	var _ Manager = (*RuntimeManager)(nil)
	var _ Manager = (*ChannelManager)(nil)
	var _ Manager = (*ToolingManager)(nil)
	var _ Manager = (*SchedulerManager)(nil)
}
