package agent

import (
	"log/slog"
	"time"

	"github.com/haasonsaas/nexus/internal/jobs"
)

// RuntimeOptions configures tool execution and loop behavior.
type RuntimeOptions struct {
	// MaxIterations limits tool-use iterations per request.
	MaxIterations int

	// ToolParallelism caps concurrent tool execution.
	ToolParallelism int

	// ToolTimeout applies a default timeout to each tool call.
	ToolTimeout time.Duration

	// ToolMaxAttempts controls retry attempts for tool execution.
	ToolMaxAttempts int

	// ToolRetryBackoff waits between retry attempts.
	ToolRetryBackoff time.Duration

	// DisableToolEvents disables ToolEvent emission while processing.
	DisableToolEvents bool

	// MaxToolCalls limits total tool calls per request (0 = unlimited).
	MaxToolCalls int

	// RequireApproval lists tool names/patterns that require approval.
	RequireApproval []string

	// ApprovalChecker evaluates approval policy for tool calls when set.
	ApprovalChecker *ApprovalChecker

	// ElevatedTools lists tool patterns eligible for elevated full bypass.
	ElevatedTools []string

	// AsyncTools lists tool names to execute asynchronously as jobs.
	AsyncTools []string

	// JobStore receives async tool job updates.
	JobStore jobs.Store

	// ToolResultGuard redacts tool results before persistence.
	ToolResultGuard ToolResultGuard

	// Logger receives runtime diagnostics.
	Logger *slog.Logger
}

// DefaultRuntimeOptions returns the baseline runtime options.
func DefaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{
		MaxIterations:     5,
		ToolParallelism:   4,
		ToolTimeout:       30 * time.Second,
		ToolMaxAttempts:   1,
		ToolRetryBackoff:  0,
		DisableToolEvents: false,
		MaxToolCalls:      0,
		Logger:            slog.Default(),
	}
}

func mergeRuntimeOptions(base RuntimeOptions, override RuntimeOptions) RuntimeOptions {
	merged := base
	if override.MaxIterations > 0 {
		merged.MaxIterations = override.MaxIterations
	}
	if override.ToolParallelism > 0 {
		merged.ToolParallelism = override.ToolParallelism
	}
	if override.ToolTimeout > 0 {
		merged.ToolTimeout = override.ToolTimeout
	}
	if override.ToolMaxAttempts > 0 {
		merged.ToolMaxAttempts = override.ToolMaxAttempts
	}
	if override.ToolRetryBackoff > 0 {
		merged.ToolRetryBackoff = override.ToolRetryBackoff
	}
	if override.DisableToolEvents {
		merged.DisableToolEvents = true
	}
	if override.MaxToolCalls > 0 {
		merged.MaxToolCalls = override.MaxToolCalls
	}
	if len(override.RequireApproval) > 0 {
		merged.RequireApproval = override.RequireApproval
	}
	if override.ApprovalChecker != nil {
		merged.ApprovalChecker = override.ApprovalChecker
	}
	if len(override.ElevatedTools) > 0 {
		merged.ElevatedTools = override.ElevatedTools
	}
	if len(override.AsyncTools) > 0 {
		merged.AsyncTools = override.AsyncTools
	}
	if override.JobStore != nil {
		merged.JobStore = override.JobStore
	}
	if override.ToolResultGuard.active() {
		merged.ToolResultGuard = override.ToolResultGuard
	}
	if override.Logger != nil {
		merged.Logger = override.Logger
	}
	return merged
}
