package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestPluginRegistry_Use(t *testing.T) {
	registry := NewPluginRegistry()

	if registry.Count() != 0 {
		t.Errorf("new registry should have 0 plugins, got %d", registry.Count())
	}

	// Add a plugin
	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {}))

	if registry.Count() != 1 {
		t.Errorf("expected 1 plugin, got %d", registry.Count())
	}

	// Add another
	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {}))

	if registry.Count() != 2 {
		t.Errorf("expected 2 plugins, got %d", registry.Count())
	}
}

func TestPluginRegistry_Use_Nil(t *testing.T) {
	registry := NewPluginRegistry()
	registry.Use(nil)

	if registry.Count() != 0 {
		t.Errorf("nil plugin should not be added, got %d plugins", registry.Count())
	}
}

func TestPluginRegistry_Emit(t *testing.T) {
	registry := NewPluginRegistry()

	var received []models.AgentEvent
	var mu sync.Mutex

	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	}))

	event := models.AgentEvent{
		Type:  models.AgentEventRunStarted,
		RunID: "test-run",
	}

	registry.Emit(context.Background(), event)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].RunID != "test-run" {
		t.Errorf("RunID = %q, want %q", received[0].RunID, "test-run")
	}
}

func TestPluginRegistry_Emit_MultiplePlugins(t *testing.T) {
	registry := NewPluginRegistry()

	var order []int
	var mu sync.Mutex

	for i := 0; i < 3; i++ {
		idx := i
		registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
		}))
	}

	registry.Emit(context.Background(), models.AgentEvent{})

	mu.Lock()
	defer mu.Unlock()

	// Plugins should be called in registration order
	if len(order) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(order))
	}
	for i, v := range order {
		if v != i {
			t.Errorf("order[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestPluginRegistry_Emit_PanicRecovery(t *testing.T) {
	registry := NewPluginRegistry()

	var called bool
	var mu sync.Mutex

	// First plugin panics
	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {
		panic("test panic")
	}))

	// Second plugin should still be called
	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {
		mu.Lock()
		called = true
		mu.Unlock()
	}))

	// Should not panic
	registry.Emit(context.Background(), models.AgentEvent{})

	mu.Lock()
	defer mu.Unlock()

	if !called {
		t.Error("second plugin should be called even after first panics")
	}
}

func TestPluginRegistry_Clear(t *testing.T) {
	registry := NewPluginRegistry()

	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {}))
	registry.Use(PluginFunc(func(ctx context.Context, e models.AgentEvent) {}))

	if registry.Count() != 2 {
		t.Fatalf("expected 2 plugins before clear")
	}

	registry.Clear()

	if registry.Count() != 0 {
		t.Errorf("expected 0 plugins after clear, got %d", registry.Count())
	}
}

func TestPluginFunc(t *testing.T) {
	var called bool

	fn := PluginFunc(func(ctx context.Context, e models.AgentEvent) {
		called = true
	})

	fn.OnEvent(context.Background(), models.AgentEvent{})

	if !called {
		t.Error("PluginFunc should call the wrapped function")
	}
}
