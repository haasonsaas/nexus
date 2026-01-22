package infra

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBaseComponent_StateTransitions(t *testing.T) {
	c := NewBaseComponent("test", nil)

	if c.State() != ComponentStateNew {
		t.Errorf("expected state New, got %s", c.State())
	}

	if !c.TransitionTo(ComponentStateNew, ComponentStateStarting) {
		t.Error("expected transition to Starting to succeed")
	}

	if c.State() != ComponentStateStarting {
		t.Errorf("expected state Starting, got %s", c.State())
	}

	// Invalid transition should fail
	if c.TransitionTo(ComponentStateNew, ComponentStateRunning) {
		t.Error("expected invalid transition to fail")
	}

	c.MarkStarted()
	if !c.IsRunning() {
		t.Error("expected component to be running")
	}

	if c.Uptime() <= 0 {
		t.Error("expected positive uptime")
	}

	c.MarkStopped()
	if c.State() != ComponentStateStopped {
		t.Errorf("expected state Stopped, got %s", c.State())
	}
}

func TestBaseComponent_Name(t *testing.T) {
	c := NewBaseComponent("my-component", nil)
	if c.Name() != "my-component" {
		t.Errorf("expected name 'my-component', got %s", c.Name())
	}
}

func TestBaseComponent_StartTime(t *testing.T) {
	c := NewBaseComponent("test", nil)

	if !c.StartTime().IsZero() {
		t.Error("expected zero start time before start")
	}

	c.MarkStarted()

	if c.StartTime().IsZero() {
		t.Error("expected non-zero start time after start")
	}

	if time.Since(c.StartTime()) > time.Second {
		t.Error("start time should be recent")
	}
}

func TestSimpleComponent_Lifecycle(t *testing.T) {
	var startCalled, stopCalled bool

	c := NewSimpleComponent(
		"simple",
		nil,
		func(_ context.Context) error {
			startCalled = true
			return nil
		},
		func(_ context.Context) error {
			stopCalled = true
			return nil
		},
	)

	// Start
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !startCalled {
		t.Error("start function not called")
	}

	if !c.IsRunning() {
		t.Error("expected component to be running")
	}

	// Start again should be no-op
	startCalled = false
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Second Start failed: %v", err)
	}

	if startCalled {
		t.Error("start function should not be called on idempotent start")
	}

	// Stop
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if !stopCalled {
		t.Error("stop function not called")
	}

	if c.State() != ComponentStateStopped {
		t.Errorf("expected Stopped state, got %s", c.State())
	}

	// Stop again should be no-op
	stopCalled = false
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Second Stop failed: %v", err)
	}

	if stopCalled {
		t.Error("stop function should not be called on idempotent stop")
	}
}

func TestSimpleComponent_StartError(t *testing.T) {
	expectedErr := errors.New("start failed")

	c := NewSimpleComponent(
		"failing",
		nil,
		func(_ context.Context) error {
			return expectedErr
		},
		nil,
	)

	err := c.Start(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	if c.State() != ComponentStateFailed {
		t.Errorf("expected Failed state, got %s", c.State())
	}
}

func TestSimpleComponent_Health(t *testing.T) {
	c := NewSimpleComponent("health-test", nil, nil, nil)

	// New state
	health := c.Health(context.Background())
	if health.State != ServiceHealthUnknown {
		t.Errorf("expected Unknown health, got %s", health.State)
	}

	// Running state
	c.SetState(ComponentStateRunning)
	c.BaseComponent.mu.Lock()
	c.BaseComponent.startTime = time.Now()
	c.BaseComponent.mu.Unlock()

	health = c.Health(context.Background())
	if health.State != ServiceHealthHealthy {
		t.Errorf("expected Healthy health, got %s", health.State)
	}

	// Failed state
	c.MarkFailed()
	health = c.Health(context.Background())
	if health.State != ServiceHealthUnhealthy {
		t.Errorf("expected Unhealthy health, got %s", health.State)
	}
}

func TestComponentManager_StartStop(t *testing.T) {
	m := NewComponentManager(nil)

	var order []string

	c1 := NewSimpleComponent("first", nil,
		func(_ context.Context) error {
			order = append(order, "start-first")
			return nil
		},
		func(_ context.Context) error {
			order = append(order, "stop-first")
			return nil
		},
	)

	c2 := NewSimpleComponent("second", nil,
		func(_ context.Context) error {
			order = append(order, "start-second")
			return nil
		},
		func(_ context.Context) error {
			order = append(order, "stop-second")
			return nil
		},
	)

	m.Register(c1)
	m.Register(c2)

	// Start
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if len(order) != 2 || order[0] != "start-first" || order[1] != "start-second" {
		t.Errorf("unexpected start order: %v", order)
	}

	// Stop
	order = nil
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Should stop in reverse order
	if len(order) != 2 || order[0] != "stop-second" || order[1] != "stop-first" {
		t.Errorf("unexpected stop order: %v", order)
	}
}

func TestComponentManager_StartRollback(t *testing.T) {
	m := NewComponentManager(nil)

	var c1Started, c1Stopped bool

	c1 := NewSimpleComponent("first", nil,
		func(_ context.Context) error {
			c1Started = true
			return nil
		},
		func(_ context.Context) error {
			c1Stopped = true
			return nil
		},
	)

	c2 := NewSimpleComponent("second", nil,
		func(_ context.Context) error {
			return errors.New("second failed")
		},
		nil,
	)

	m.Register(c1)
	m.Register(c2)

	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from Start")
	}

	if !c1Started {
		t.Error("first component should have started")
	}

	if !c1Stopped {
		t.Error("first component should have been stopped during rollback")
	}
}

func TestComponentManager_Idempotent(t *testing.T) {
	m := NewComponentManager(nil)

	var startCount int32
	c := NewSimpleComponent("test", nil,
		func(_ context.Context) error {
			atomic.AddInt32(&startCount, 1)
			return nil
		},
		nil,
	)

	m.Register(c)

	// Start multiple times
	for i := 0; i < 3; i++ {
		if err := m.Start(context.Background()); err != nil {
			t.Fatalf("Start %d failed: %v", i, err)
		}
	}

	if atomic.LoadInt32(&startCount) != 1 {
		t.Errorf("expected 1 start call, got %d", startCount)
	}
}

func TestComponentManager_Health(t *testing.T) {
	m := NewComponentManager(nil)

	c1 := NewSimpleComponent("healthy", nil, nil, nil)
	c1.MarkStarted()

	c2 := NewSimpleComponent("unhealthy", nil, nil, nil)
	c2.MarkFailed()

	m.Register(c1)
	m.Register(c2)

	health := m.Health(context.Background())

	if len(health) != 2 {
		t.Fatalf("expected 2 health entries, got %d", len(health))
	}

	if health["healthy"].State != ServiceHealthHealthy {
		t.Errorf("expected healthy state for c1, got %s", health["healthy"].State)
	}

	if health["unhealthy"].State != ServiceHealthUnhealthy {
		t.Errorf("expected unhealthy state for c2, got %s", health["unhealthy"].State)
	}
}

func TestComponentManager_Components(t *testing.T) {
	m := NewComponentManager(nil)

	c1 := NewSimpleComponent("first", nil, nil, nil)
	c2 := NewSimpleComponent("second", nil, nil, nil)

	m.Register(c1)
	m.Register(c2)

	components := m.Components()
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(components))
	}

	if components[0].Name() != "first" || components[1].Name() != "second" {
		t.Errorf("unexpected component names: %s, %s", components[0].Name(), components[1].Name())
	}
}

func TestComponentState_String(t *testing.T) {
	tests := []struct {
		state    ComponentState
		expected string
	}{
		{ComponentStateNew, "new"},
		{ComponentStateStarting, "starting"},
		{ComponentStateRunning, "running"},
		{ComponentStateStopping, "stopping"},
		{ComponentStateStopped, "stopped"},
		{ComponentStateFailed, "failed"},
		{ComponentState(99), "unknown"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.state.String())
		}
	}
}
