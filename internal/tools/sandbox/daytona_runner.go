package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DaytonaRunnerOptions configures the Daytona command runner.
type DaytonaRunnerOptions struct {
	DefaultCPU      int
	DefaultMemoryMB int
	DefaultTimeout  time.Duration
	NetworkEnabled  bool
	WorkspaceAccess WorkspaceAccessMode
}

// DaytonaRunner executes arbitrary commands inside Daytona sandboxes with a local workspace upload.
type DaytonaRunner struct {
	executor *daytonaExecutor
	config   *Config
}

// NewDaytonaRunner creates a command runner using the Daytona backend.
func NewDaytonaRunner(cfg DaytonaConfig, opts DaytonaRunnerOptions) (*DaytonaRunner, error) {
	config := &Config{
		Backend:         BackendDaytona,
		PoolSize:        1,
		MaxPoolSize:     1,
		DefaultTimeout:  30 * time.Second,
		DefaultCPU:      1000,
		DefaultMemory:   512,
		NetworkEnabled:  opts.NetworkEnabled,
		WorkspaceAccess: WorkspaceReadOnly,
		Daytona:         &cfg,
	}

	if opts.DefaultTimeout > 0 {
		config.DefaultTimeout = opts.DefaultTimeout
	}
	if opts.DefaultCPU > 0 {
		config.DefaultCPU = opts.DefaultCPU
	}
	if opts.DefaultMemoryMB > 0 {
		config.DefaultMemory = opts.DefaultMemoryMB
	}
	if opts.WorkspaceAccess != "" {
		config.WorkspaceAccess = opts.WorkspaceAccess
	}

	resolved, err := resolveDaytonaConfig(config.Daytona)
	if err != nil {
		return nil, err
	}
	config.Daytona = resolved
	client, err := newDaytonaClient(resolved)
	if err != nil {
		return nil, err
	}
	config.daytonaClient = client

	executor, err := newDaytonaExecutor("bash", config)
	if err != nil {
		return nil, err
	}

	return &DaytonaRunner{
		executor: executor,
		config:   config,
	}, nil
}

// RunCommand executes a command inside a Daytona sandbox with the given workspace.
func (r *DaytonaRunner) RunCommand(ctx context.Context, workspace string, command string, params *ExecuteParams) (*ExecuteResult, error) {
	if r == nil || r.executor == nil {
		return nil, errors.New("daytona runner not initialized")
	}
	if params == nil {
		params = &ExecuteParams{
			Timeout:         int(r.config.DefaultTimeout.Seconds()),
			CPULimit:        r.config.DefaultCPU,
			MemLimit:        r.config.DefaultMemory,
			WorkspaceAccess: r.config.WorkspaceAccess,
		}
	}
	if params.Timeout <= 0 {
		params.Timeout = int(r.config.DefaultTimeout.Seconds())
	}
	if params.CPULimit <= 0 {
		params.CPULimit = r.config.DefaultCPU
	}
	if params.MemLimit <= 0 {
		params.MemLimit = r.config.DefaultMemory
	}
	if params.WorkspaceAccess == "" {
		params.WorkspaceAccess = r.config.WorkspaceAccess
	}
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("daytona runner command is empty")
	}
	return r.executor.runCommand(ctx, params, workspace, command)
}

// Close releases any retained Daytona sandboxes.
func (r *DaytonaRunner) Close() error {
	if r == nil || r.executor == nil {
		return nil
	}
	return r.executor.Close()
}
