// Package multiagent provides subagent registry for tracking child agent runs.
package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SubagentRunStatus represents the current status of a subagent run.
type SubagentRunStatus string

const (
	// SubagentStatusPending means the subagent run has been registered but not started.
	SubagentStatusPending SubagentRunStatus = "pending"
	// SubagentStatusRunning means the subagent is currently executing.
	SubagentStatusRunning SubagentRunStatus = "running"
	// SubagentStatusCompleted means the subagent finished successfully.
	SubagentStatusCompleted SubagentRunStatus = "completed"
	// SubagentStatusError means the subagent terminated with an error.
	SubagentStatusError SubagentRunStatus = "error"
	// SubagentStatusTimeout means the subagent exceeded its time limit.
	SubagentStatusTimeout SubagentRunStatus = "timeout"
)

// SubagentOutcome describes the result of a subagent run.
type SubagentOutcome struct {
	Status  SubagentRunStatus `json:"status"`
	Error   string            `json:"error,omitempty"`
	Result  string            `json:"result,omitempty"`
	EndedAt time.Time         `json:"ended_at,omitempty"`
}

// SubagentRunRecord tracks the state of a subagent execution.
type SubagentRunRecord struct {
	// RunID uniquely identifies this subagent run.
	RunID string `json:"run_id"`

	// ChildSessionKey is the session key of the child agent.
	ChildSessionKey string `json:"child_session_key"`

	// RequesterSessionKey is the session key of the parent/requester.
	RequesterSessionKey string `json:"requester_session_key"`

	// RequesterDisplayKey is a human-readable identifier for the requester.
	RequesterDisplayKey string `json:"requester_display_key,omitempty"`

	// Task is a description of what the subagent is doing.
	Task string `json:"task"`

	// Label is an optional short label for the run.
	Label string `json:"label,omitempty"`

	// Cleanup specifies whether to delete the child session after completion.
	Cleanup string `json:"cleanup"` // "delete" or "keep"

	// CreatedAt is when the run was registered.
	CreatedAt time.Time `json:"created_at"`

	// StartedAt is when the run actually began executing.
	StartedAt time.Time `json:"started_at,omitempty"`

	// Outcome describes how the run ended.
	Outcome *SubagentOutcome `json:"outcome,omitempty"`

	// TimeoutMs is the maximum allowed runtime in milliseconds.
	TimeoutMs int64 `json:"timeout_ms,omitempty"`

	// ArchiveAtMs is when this record can be cleaned up.
	ArchiveAtMs int64 `json:"archive_at_ms,omitempty"`

	// CleanupCompleted tracks whether cleanup has been processed.
	CleanupCompleted bool `json:"cleanup_completed,omitempty"`
}

// IsComplete returns true if the subagent run has finished.
func (r *SubagentRunRecord) IsComplete() bool {
	if r.Outcome == nil {
		return false
	}
	switch r.Outcome.Status {
	case SubagentStatusCompleted, SubagentStatusError, SubagentStatusTimeout:
		return true
	}
	return false
}

// Duration returns the run duration if both started and ended.
func (r *SubagentRunRecord) Duration() time.Duration {
	if r.StartedAt.IsZero() || r.Outcome == nil || r.Outcome.EndedAt.IsZero() {
		return 0
	}
	return r.Outcome.EndedAt.Sub(r.StartedAt)
}

// SubagentRegistryConfig configures the subagent registry behavior.
type SubagentRegistryConfig struct {
	// PersistPath is where to store the registry file. If empty, no persistence.
	PersistPath string

	// DefaultTimeoutMs is the default timeout for subagent runs.
	DefaultTimeoutMs int64

	// ArchiveAfterMs is how long to keep completed runs before archiving.
	ArchiveAfterMs int64

	// SweepInterval is how often to check for archived runs.
	SweepInterval time.Duration

	// OnRunComplete is called when a subagent run completes.
	OnRunComplete func(ctx context.Context, record *SubagentRunRecord)

	// OnRunStart is called when a subagent run starts.
	OnRunStart func(ctx context.Context, record *SubagentRunRecord)
}

// DefaultSubagentRegistryConfig returns sensible defaults.
func DefaultSubagentRegistryConfig() *SubagentRegistryConfig {
	return &SubagentRegistryConfig{
		DefaultTimeoutMs: 10 * 60 * 1000, // 10 minutes
		ArchiveAfterMs:   60 * 60 * 1000, // 1 hour
		SweepInterval:    60 * time.Second,
	}
}

// SubagentRegistry manages subagent runs with persistence and lifecycle tracking.
type SubagentRegistry struct {
	mu       sync.RWMutex
	config   *SubagentRegistryConfig
	runs     map[string]*SubagentRunRecord
	sweeper  *time.Ticker
	stopCh   chan struct{}
	stopped  bool
	restored bool
}

// NewSubagentRegistry creates a new registry with the given configuration.
func NewSubagentRegistry(config *SubagentRegistryConfig) *SubagentRegistry {
	if config == nil {
		config = DefaultSubagentRegistryConfig()
	}
	r := &SubagentRegistry{
		config: config,
		runs:   make(map[string]*SubagentRunRecord),
		stopCh: make(chan struct{}),
	}

	// Restore from disk
	r.restore()

	// Start sweeper
	if config.SweepInterval > 0 {
		r.sweeper = time.NewTicker(config.SweepInterval)
		go r.sweepLoop()
	}

	return r
}

// Register creates a new subagent run record.
func (r *SubagentRegistry) Register(params RegisterSubagentParams) *SubagentRunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	timeoutMs := params.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = r.config.DefaultTimeoutMs
	}

	archiveAt := int64(0)
	if r.config.ArchiveAfterMs > 0 {
		archiveAt = now.UnixMilli() + r.config.ArchiveAfterMs
	}

	record := &SubagentRunRecord{
		RunID:               params.RunID,
		ChildSessionKey:     params.ChildSessionKey,
		RequesterSessionKey: params.RequesterSessionKey,
		RequesterDisplayKey: params.RequesterDisplayKey,
		Task:                params.Task,
		Label:               params.Label,
		Cleanup:             params.Cleanup,
		CreatedAt:           now,
		TimeoutMs:           timeoutMs,
		ArchiveAtMs:         archiveAt,
	}

	r.runs[params.RunID] = record
	r.persist()

	return record
}

// RegisterSubagentParams contains parameters for registering a subagent run.
type RegisterSubagentParams struct {
	RunID               string
	ChildSessionKey     string
	RequesterSessionKey string
	RequesterDisplayKey string
	Task                string
	Label               string
	Cleanup             string // "delete" or "keep"
	TimeoutMs           int64
}

// Start marks a subagent run as started.
func (r *SubagentRegistry) Start(runID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.runs[runID]
	if record == nil {
		return errors.New("run not found")
	}

	record.StartedAt = time.Now()
	r.persist()

	// Notify callback
	if r.config.OnRunStart != nil {
		go r.config.OnRunStart(context.Background(), record)
	}

	return nil
}

// Complete marks a subagent run as completed.
func (r *SubagentRegistry) Complete(runID string, outcome *SubagentOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.runs[runID]
	if record == nil {
		return errors.New("run not found")
	}

	if outcome.EndedAt.IsZero() {
		outcome.EndedAt = time.Now()
	}
	record.Outcome = outcome
	r.persist()

	// Notify callback
	if r.config.OnRunComplete != nil {
		go r.config.OnRunComplete(context.Background(), record)
	}

	return nil
}

// Get retrieves a subagent run record.
func (r *SubagentRegistry) Get(runID string) *SubagentRunRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	record := r.runs[runID]
	if record == nil {
		return nil
	}
	// Return a copy
	copied := *record
	return &copied
}

// ListForRequester returns all runs for a given requester session.
func (r *SubagentRegistry) ListForRequester(requesterSessionKey string) []*SubagentRunRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*SubagentRunRecord
	for _, record := range r.runs {
		if record.RequesterSessionKey == requesterSessionKey {
			copied := *record
			result = append(result, &copied)
		}
	}
	return result
}

// ListActive returns all currently running subagents.
func (r *SubagentRegistry) ListActive() []*SubagentRunRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*SubagentRunRecord
	for _, record := range r.runs {
		if !record.IsComplete() {
			copied := *record
			result = append(result, &copied)
		}
	}
	return result
}

// Delete removes a run from the registry.
func (r *SubagentRegistry) Delete(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.runs, runID)
	r.persist()
}

// MarkCleanupComplete marks that cleanup has been handled for a run.
func (r *SubagentRegistry) MarkCleanupComplete(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.runs[runID]
	if record != nil {
		record.CleanupCompleted = true
		r.persist()
	}
}

// Stop shuts down the registry sweeper.
func (r *SubagentRegistry) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	r.mu.Unlock()

	close(r.stopCh)
	if r.sweeper != nil {
		r.sweeper.Stop()
	}
}

// sweepLoop periodically removes archived runs.
func (r *SubagentRegistry) sweepLoop() {
	for {
		select {
		case <-r.stopCh:
			return
		case <-r.sweeper.C:
			r.sweep()
		}
	}
}

// sweep removes runs that are past their archive time.
func (r *SubagentRegistry) sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()
	mutated := false

	for runID, record := range r.runs {
		if record.ArchiveAtMs > 0 && record.ArchiveAtMs <= now && record.IsComplete() {
			delete(r.runs, runID)
			mutated = true
		}
	}

	if mutated {
		r.persist()
	}
}

// CheckTimeouts marks any overdue runs as timed out.
func (r *SubagentRegistry) CheckTimeouts() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	mutated := false

	for _, record := range r.runs {
		if record.IsComplete() {
			continue
		}
		if record.StartedAt.IsZero() {
			continue
		}
		if record.TimeoutMs <= 0 {
			continue
		}

		deadline := record.StartedAt.Add(time.Duration(record.TimeoutMs) * time.Millisecond)
		if now.After(deadline) {
			record.Outcome = &SubagentOutcome{
				Status:  SubagentStatusTimeout,
				Error:   "subagent exceeded timeout",
				EndedAt: now,
			}
			mutated = true

			// Notify callback
			if r.config.OnRunComplete != nil {
				go r.config.OnRunComplete(context.Background(), record)
			}
		}
	}

	if mutated {
		r.persist()
	}
}

// persist saves the registry to disk.
func (r *SubagentRegistry) persist() {
	if r.config.PersistPath == "" {
		return
	}

	data, err := json.MarshalIndent(r.runs, "", "  ")
	if err != nil {
		return
	}

	dir := filepath.Dir(r.config.PersistPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	// Write atomically
	tmpPath := r.config.PersistPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, r.config.PersistPath)
}

// restore loads the registry from disk.
func (r *SubagentRegistry) restore() {
	if r.restored || r.config.PersistPath == "" {
		return
	}
	r.restored = true

	data, err := os.ReadFile(r.config.PersistPath)
	if err != nil {
		return
	}

	var runs map[string]*SubagentRunRecord
	if err := json.Unmarshal(data, &runs); err != nil {
		return
	}

	for runID, record := range runs {
		if r.runs[runID] == nil {
			r.runs[runID] = record
		}
	}
}

// Stats returns statistics about the registry.
func (r *SubagentRegistry) Stats() SubagentRegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := SubagentRegistryStats{
		TotalRuns:     len(r.runs),
		ByStatus:      make(map[SubagentRunStatus]int),
		ActiveByAgent: make(map[string]int),
	}

	for _, record := range r.runs {
		if record.IsComplete() {
			stats.ByStatus[record.Outcome.Status]++
			stats.CompletedRuns++
		} else {
			stats.ActiveRuns++
			stats.ByStatus[SubagentStatusRunning]++
			stats.ActiveByAgent[record.RequesterSessionKey]++
		}
	}

	return stats
}

// SubagentRegistryStats contains registry statistics.
type SubagentRegistryStats struct {
	TotalRuns     int                       `json:"total_runs"`
	ActiveRuns    int                       `json:"active_runs"`
	CompletedRuns int                       `json:"completed_runs"`
	ByStatus      map[SubagentRunStatus]int `json:"by_status"`
	ActiveByAgent map[string]int            `json:"active_by_agent"`
}
