// Package infra provides infrastructure patterns and utilities.
//
// lifecycle.go defines standardized lifecycle interfaces for subsystem management.
package infra

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Lifecycle defines the standard lifecycle interface for subsystems.
// All managed components should implement this interface to enable
// consistent startup, shutdown, and health monitoring.
type Lifecycle interface {
	// Start initializes and starts the component.
	// It should be idempotent - calling Start on an already-started component is a no-op.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the component.
	// It should be idempotent - calling Stop on an already-stopped component is a no-op.
	Stop(ctx context.Context) error
}

// ComponentHealthChecker defines health check capabilities for lifecycle components.
// This is distinct from HealthChecker (which is a function type for health checks).
type ComponentHealthChecker interface {
	// Health returns the current health status of the component.
	Health(ctx context.Context) ComponentHealth
}

// ComponentHealth represents the health state of a lifecycle component.
type ComponentHealth struct {
	State   ServiceHealth     `json:"state"`
	Message string            `json:"message,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

// Named provides a name for a component (used in logging and monitoring).
type Named interface {
	Name() string
}

// FullLifecycleComponent combines all lifecycle-related interfaces for components.
type FullLifecycleComponent interface {
	Lifecycle
	ComponentHealthChecker
	Named
}

// ComponentState tracks the state of a lifecycle component.
type ComponentState int32

const (
	ComponentStateNew ComponentState = iota
	ComponentStateStarting
	ComponentStateRunning
	ComponentStateStopping
	ComponentStateStopped
	ComponentStateFailed
)

func (s ComponentState) String() string {
	switch s {
	case ComponentStateNew:
		return "new"
	case ComponentStateStarting:
		return "starting"
	case ComponentStateRunning:
		return "running"
	case ComponentStateStopping:
		return "stopping"
	case ComponentStateStopped:
		return "stopped"
	case ComponentStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// BaseComponent provides common lifecycle management functionality.
// Embed this in components to get standard state tracking and idempotency.
type BaseComponent struct {
	name      string
	state     atomic.Int32
	startTime time.Time
	mu        sync.Mutex
	logger    *slog.Logger
}

// NewBaseComponent creates a new base component with the given name.
func NewBaseComponent(name string, logger *slog.Logger) *BaseComponent {
	if logger == nil {
		logger = slog.Default()
	}
	return &BaseComponent{
		name:   name,
		logger: logger,
	}
}

// Name returns the component name.
func (c *BaseComponent) Name() string {
	return c.name
}

// State returns the current component state.
func (c *BaseComponent) State() ComponentState {
	return ComponentState(c.state.Load())
}

// IsRunning returns true if the component is in the running state.
func (c *BaseComponent) IsRunning() bool {
	return c.State() == ComponentStateRunning
}

// StartTime returns when the component started (zero if not started).
func (c *BaseComponent) StartTime() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startTime
}

// Uptime returns how long the component has been running.
func (c *BaseComponent) Uptime() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.startTime.IsZero() {
		return 0
	}
	return time.Since(c.startTime)
}

// Logger returns the component's logger.
func (c *BaseComponent) Logger() *slog.Logger {
	return c.logger
}

// TransitionTo attempts to transition to a new state.
// Returns true if the transition was successful.
func (c *BaseComponent) TransitionTo(from, to ComponentState) bool {
	if c.state.CompareAndSwap(int32(from), int32(to)) {
		c.logger.Debug("component state transition",
			"component", c.name,
			"from", from.String(),
			"to", to.String(),
		)
		return true
	}
	return false
}

// SetState forcibly sets the component state (use with caution).
func (c *BaseComponent) SetState(state ComponentState) {
	c.state.Store(int32(state))
}

// MarkStarted marks the component as started and records the start time.
func (c *BaseComponent) MarkStarted() {
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()
	c.SetState(ComponentStateRunning)
}

// MarkStopped marks the component as stopped.
func (c *BaseComponent) MarkStopped() {
	c.SetState(ComponentStateStopped)
}

// MarkFailed marks the component as failed.
func (c *BaseComponent) MarkFailed() {
	c.SetState(ComponentStateFailed)
}

// ComponentManager manages multiple lifecycle components.
type ComponentManager struct {
	mu         sync.RWMutex
	components []FullLifecycleComponent
	logger     *slog.Logger
	started    atomic.Bool
}

// NewComponentManager creates a new component manager.
func NewComponentManager(logger *slog.Logger) *ComponentManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ComponentManager{
		components: make([]FullLifecycleComponent, 0),
		logger:     logger,
	}
}

// Register adds a component to be managed.
// Components are started in registration order and stopped in reverse order.
func (m *ComponentManager) Register(c FullLifecycleComponent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.components = append(m.components, c)
}

// Start starts all registered components in order.
// If a component fails to start, previously started components are stopped.
func (m *ComponentManager) Start(ctx context.Context) error {
	if !m.started.CompareAndSwap(false, true) {
		return nil // Already started
	}

	m.mu.RLock()
	components := make([]FullLifecycleComponent, len(m.components))
	copy(components, m.components)
	m.mu.RUnlock()

	started := make([]FullLifecycleComponent, 0, len(components))

	for _, c := range components {
		m.logger.Info("starting component", "component", c.Name())

		if err := c.Start(ctx); err != nil {
			m.logger.Error("component failed to start",
				"component", c.Name(),
				"error", err,
			)

			// Stop already-started components in reverse order
			for i := len(started) - 1; i >= 0; i-- {
				if stopErr := started[i].Stop(ctx); stopErr != nil {
					m.logger.Error("error stopping component during rollback",
						"component", started[i].Name(),
						"error", stopErr,
					)
				}
			}

			m.started.Store(false)
			return fmt.Errorf("component %s failed to start: %w", c.Name(), err)
		}

		started = append(started, c)
	}

	m.logger.Info("all components started", "count", len(started))
	return nil
}

// Stop stops all registered components in reverse order.
func (m *ComponentManager) Stop(ctx context.Context) error {
	if !m.started.CompareAndSwap(true, false) {
		return nil // Already stopped
	}

	m.mu.RLock()
	components := make([]FullLifecycleComponent, len(m.components))
	copy(components, m.components)
	m.mu.RUnlock()

	var errs []error

	// Stop in reverse order
	for i := len(components) - 1; i >= 0; i-- {
		c := components[i]
		m.logger.Info("stopping component", "component", c.Name())

		if err := c.Stop(ctx); err != nil {
			m.logger.Error("error stopping component",
				"component", c.Name(),
				"error", err,
			)
			errs = append(errs, fmt.Errorf("component %s: %w", c.Name(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping components: %v", errs)
	}

	m.logger.Info("all components stopped")
	return nil
}

// Health returns aggregated health status of all components.
func (m *ComponentManager) Health(ctx context.Context) map[string]ComponentHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]ComponentHealth, len(m.components))
	for _, c := range m.components {
		health[c.Name()] = c.Health(ctx)
	}
	return health
}

// Components returns the list of managed components.
func (m *ComponentManager) Components() []FullLifecycleComponent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	components := make([]FullLifecycleComponent, len(m.components))
	copy(components, m.components)
	return components
}

// SimpleComponent wraps start/stop functions into a FullLifecycleComponent implementation.
type SimpleComponent struct {
	*BaseComponent
	startFn func(ctx context.Context) error
	stopFn  func(ctx context.Context) error
}

// NewSimpleComponent creates a component from start/stop functions.
func NewSimpleComponent(
	name string,
	logger *slog.Logger,
	startFn func(ctx context.Context) error,
	stopFn func(ctx context.Context) error,
) *SimpleComponent {
	return &SimpleComponent{
		BaseComponent: NewBaseComponent(name, logger),
		startFn:       startFn,
		stopFn:        stopFn,
	}
}

// Start implements Lifecycle.
func (c *SimpleComponent) Start(ctx context.Context) error {
	if !c.TransitionTo(ComponentStateNew, ComponentStateStarting) {
		if c.IsRunning() {
			return nil // Already running
		}
		return fmt.Errorf("component %s cannot start from state %s", c.Name(), c.State())
	}

	if c.startFn != nil {
		if err := c.startFn(ctx); err != nil {
			c.MarkFailed()
			return err
		}
	}

	c.MarkStarted()
	return nil
}

// Stop implements Lifecycle.
func (c *SimpleComponent) Stop(ctx context.Context) error {
	if !c.TransitionTo(ComponentStateRunning, ComponentStateStopping) {
		if c.State() == ComponentStateStopped {
			return nil // Already stopped
		}
		// Allow stopping from failed state
		if c.State() != ComponentStateFailed {
			return nil
		}
	}

	if c.stopFn != nil {
		if err := c.stopFn(ctx); err != nil {
			c.MarkFailed()
			return err
		}
	}

	c.MarkStopped()
	return nil
}

// Health implements ComponentHealthChecker.
func (c *SimpleComponent) Health(_ context.Context) ComponentHealth {
	switch c.State() {
	case ComponentStateRunning:
		return ComponentHealth{
			State:   ServiceHealthHealthy,
			Message: "running",
			Details: map[string]string{
				"uptime": c.Uptime().String(),
			},
		}
	case ComponentStateStopped:
		return ComponentHealth{
			State:   ServiceHealthUnhealthy,
			Message: "stopped",
		}
	case ComponentStateFailed:
		return ComponentHealth{
			State:   ServiceHealthUnhealthy,
			Message: "failed",
		}
	default:
		return ComponentHealth{
			State:   ServiceHealthUnknown,
			Message: c.State().String(),
		}
	}
}

// Ensure SimpleComponent implements FullLifecycleComponent.
var _ FullLifecycleComponent = (*SimpleComponent)(nil)
