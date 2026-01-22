// Package managers provides modular subsystem managers for the gateway server.
//
// Each manager encapsulates a cohesive set of functionality with standardized
// lifecycle management via Start(ctx)/Stop(ctx) methods.
package managers

import (
	"context"
	"log/slog"
)

// Manager defines the standard lifecycle interface for all gateway subsystems.
// All managers must implement Start and Stop for consistent lifecycle management.
type Manager interface {
	// Start initializes and starts the manager.
	// The context should be used for cancellation of long-running operations.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the manager.
	// The context provides a deadline for graceful shutdown.
	Stop(ctx context.Context) error
}

// ManagerConfig provides common configuration for all managers.
type ManagerConfig struct {
	Logger *slog.Logger
}

// Managers holds all gateway subsystem managers for coordinated lifecycle management.
type Managers struct {
	Runtime   *RuntimeManager
	Channel   *ChannelManager
	Scheduler *SchedulerManager
	Tooling   *ToolingManager
}

// StartAll starts all managers in the correct order.
// Returns on first error, stopping already-started managers.
func (m *Managers) StartAll(ctx context.Context) error {
	// Start in dependency order
	managers := []Manager{
		m.Runtime,   // Runtime first - other managers may need it
		m.Tooling,   // Tooling second - needs runtime for tool registration
		m.Channel,   // Channels third - needs runtime for message processing
		m.Scheduler, // Scheduler last - needs everything else
	}

	var started []Manager
	for _, mgr := range managers {
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

// StopAll stops all managers in reverse order.
// Continues stopping even if errors occur, returning the first error.
func (m *Managers) StopAll(ctx context.Context) error {
	// Stop in reverse dependency order
	managers := []Manager{
		m.Scheduler,
		m.Channel,
		m.Tooling,
		m.Runtime,
	}

	var firstErr error
	for _, mgr := range managers {
		if mgr == nil {
			continue
		}
		if err := mgr.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
