// Package heartbeat provides heartbeat runner with visibility control.
package heartbeat

import (
	"context"
	"sync"
	"time"
)

// RunResult represents the outcome of a heartbeat run.
type RunResult struct {
	Status     RunStatus     `json:"status"`
	Reason     string        `json:"reason,omitempty"`
	DurationMs int64         `json:"duration_ms,omitempty"`
	Preview    string        `json:"preview,omitempty"`
	Channel    string        `json:"channel,omitempty"`
	HasMedia   bool          `json:"has_media,omitempty"`
	Indicator  IndicatorType `json:"indicator,omitempty"`
}

// RunStatus describes the heartbeat run outcome.
type RunStatus string

const (
	RunStatusRan     RunStatus = "ran"
	RunStatusSkipped RunStatus = "skipped"
	RunStatusFailed  RunStatus = "failed"
)

// IndicatorType describes the indicator to show for heartbeat.
type IndicatorType string

const (
	IndicatorOkEmpty IndicatorType = "ok-empty"
	IndicatorOkToken IndicatorType = "ok-token"
	IndicatorSent    IndicatorType = "sent"
	IndicatorFailed  IndicatorType = "failed"
)

// Visibility controls what heartbeat outputs are shown.
type Visibility struct {
	// ShowOk shows HEARTBEAT_OK acknowledgments.
	ShowOk bool `json:"show_ok" yaml:"show_ok"`
	// ShowAlerts shows content messages from heartbeat.
	ShowAlerts bool `json:"show_alerts" yaml:"show_alerts"`
	// UseIndicator emits indicator events for observability.
	UseIndicator bool `json:"use_indicator" yaml:"use_indicator"`
}

// DefaultVisibility returns the default visibility settings.
func DefaultVisibility() Visibility {
	return Visibility{
		ShowOk:       false, // Silent by default
		ShowAlerts:   true,  // Show content messages
		UseIndicator: true,  // Emit indicator events
	}
}

// RunnerConfig configures the heartbeat runner.
type RunnerConfig struct {
	// Enabled turns on the heartbeat runner.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// IntervalMs is the time between heartbeats in milliseconds.
	IntervalMs int64 `json:"interval_ms" yaml:"interval_ms"`

	// ActiveHours restricts when heartbeats can run.
	ActiveHours *ActiveHoursConfig `json:"active_hours" yaml:"active_hours"`

	// Visibility controls heartbeat output visibility.
	Visibility *Visibility `json:"visibility" yaml:"visibility"`

	// Prompt is the heartbeat prompt text.
	Prompt string `json:"prompt" yaml:"prompt"`

	// Target is the delivery target (e.g., "last", channel ID).
	Target string `json:"target" yaml:"target"`

	// Model overrides the default model for heartbeat.
	Model string `json:"model" yaml:"model"`

	// AckMaxChars is the max chars for acknowledgment text.
	AckMaxChars int `json:"ack_max_chars" yaml:"ack_max_chars"`
}

// DefaultRunnerConfig returns the default runner configuration.
func DefaultRunnerConfig() *RunnerConfig {
	return &RunnerConfig{
		Enabled:     false,
		IntervalMs:  5 * 60 * 1000, // 5 minutes
		ActiveHours: DefaultActiveHoursConfig(),
		Visibility:  &Visibility{ShowOk: false, ShowAlerts: true, UseIndicator: true},
		Target:      "last",
		AckMaxChars: 200,
	}
}

// AgentHeartbeatState tracks heartbeat state for an agent.
type AgentHeartbeatState struct {
	AgentID    string
	Config     *RunnerConfig
	IntervalMs int64
	LastRunMs  int64
	NextDueMs  int64
}

// Runner manages heartbeat execution across agents.
type Runner struct {
	mu           sync.RWMutex
	agents       map[string]*AgentHeartbeatState
	timer        *time.Timer
	stopped      bool
	config       *RunnerConfig
	userTimezone string

	// Callbacks
	onRun   func(ctx context.Context, agentID string, config *RunnerConfig) (*RunResult, error)
	onEvent func(event *HeartbeatEvent)
}

// HeartbeatEvent is emitted for heartbeat observability.
type HeartbeatEvent struct {
	Status     RunStatus     `json:"status"`
	Reason     string        `json:"reason,omitempty"`
	AgentID    string        `json:"agent_id,omitempty"`
	Channel    string        `json:"channel,omitempty"`
	Preview    string        `json:"preview,omitempty"`
	DurationMs int64         `json:"duration_ms,omitempty"`
	HasMedia   bool          `json:"has_media,omitempty"`
	Indicator  IndicatorType `json:"indicator,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// RunnerOption configures the runner.
type RunnerOption func(*Runner)

// WithOnRun sets the heartbeat execution callback.
func WithOnRun(fn func(ctx context.Context, agentID string, config *RunnerConfig) (*RunResult, error)) RunnerOption {
	return func(r *Runner) {
		r.onRun = fn
	}
}

// WithOnEvent sets the event emission callback.
func WithOnEvent(fn func(event *HeartbeatEvent)) RunnerOption {
	return func(r *Runner) {
		r.onEvent = fn
	}
}

// WithUserTimezone sets the user timezone for active hours.
func WithUserTimezone(tz string) RunnerOption {
	return func(r *Runner) {
		r.userTimezone = tz
	}
}

// NewRunner creates a new heartbeat runner.
func NewRunner(config *RunnerConfig, opts ...RunnerOption) *Runner {
	if config == nil {
		config = DefaultRunnerConfig()
	}

	r := &Runner{
		agents: make(map[string]*AgentHeartbeatState),
		config: config,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// RegisterAgent adds an agent to the heartbeat runner.
func (r *Runner) RegisterAgent(agentID string, config *RunnerConfig) {
	if config == nil {
		config = r.config
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped {
		return
	}

	now := time.Now().UnixMilli()
	intervalMs := config.IntervalMs
	if intervalMs <= 0 {
		intervalMs = r.config.IntervalMs
	}

	prev := r.agents[agentID]
	var nextDue int64
	if prev != nil && prev.LastRunMs > 0 {
		nextDue = prev.LastRunMs + intervalMs
	} else if prev != nil && prev.NextDueMs > now {
		nextDue = prev.NextDueMs
	} else {
		nextDue = now + intervalMs
	}

	r.agents[agentID] = &AgentHeartbeatState{
		AgentID:    agentID,
		Config:     config,
		IntervalMs: intervalMs,
		LastRunMs:  0,
		NextDueMs:  nextDue,
	}

	r.scheduleNextLocked()
}

// UnregisterAgent removes an agent from the heartbeat runner.
func (r *Runner) UnregisterAgent(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.agents, agentID)
	r.scheduleNextLocked()
}

// Start begins the heartbeat runner.
func (r *Runner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped {
		return
	}

	r.scheduleNextLocked()
}

// Stop halts the heartbeat runner.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopped = true
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
}

// TriggerNow triggers an immediate heartbeat run.
func (r *Runner) TriggerNow(ctx context.Context, reason string) (*RunResult, error) {
	return r.runNow(ctx, reason)
}

// scheduleNextLocked schedules the next heartbeat check.
// Must be called with r.mu held.
func (r *Runner) scheduleNextLocked() {
	if r.stopped || len(r.agents) == 0 {
		return
	}

	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}

	now := time.Now().UnixMilli()
	var nextDue int64 = -1

	for _, agent := range r.agents {
		if !agent.Config.Enabled {
			continue
		}
		if nextDue < 0 || agent.NextDueMs < nextDue {
			nextDue = agent.NextDueMs
		}
	}

	if nextDue < 0 {
		return
	}

	delay := nextDue - now
	if delay < 0 {
		delay = 0
	}

	r.timer = time.AfterFunc(time.Duration(delay)*time.Millisecond, func() {
		_, _ = r.runNow(context.Background(), "interval")
	})
}

// runNow executes heartbeats for all due agents.
func (r *Runner) runNow(ctx context.Context, reason string) (*RunResult, error) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return &RunResult{Status: RunStatusSkipped, Reason: "stopped"}, nil
	}

	now := time.Now().UnixMilli()
	isInterval := reason == "interval"

	var toRun []*AgentHeartbeatState
	for _, agent := range r.agents {
		if !agent.Config.Enabled {
			continue
		}
		if isInterval && now < agent.NextDueMs {
			continue
		}
		toRun = append(toRun, agent)
	}
	r.mu.Unlock()

	if len(toRun) == 0 {
		r.mu.Lock()
		r.scheduleNextLocked()
		r.mu.Unlock()
		return &RunResult{Status: RunStatusSkipped, Reason: "not-due"}, nil
	}

	startedAt := time.Now()
	var lastResult *RunResult

	for _, agent := range toRun {
		// Check active hours
		if agent.Config.ActiveHours != nil && agent.Config.ActiveHours.Enabled {
			active, err := agent.Config.ActiveHours.IsActiveNow(r.userTimezone)
			if err != nil || !active {
				r.emitEvent(&HeartbeatEvent{
					Status:    RunStatusSkipped,
					Reason:    "quiet-hours",
					AgentID:   agent.AgentID,
					Timestamp: time.Now(),
				})
				continue
			}
		}

		var result *RunResult
		if r.onRun != nil {
			var err error
			result, err = r.onRun(ctx, agent.AgentID, agent.Config)
			if err != nil {
				result = &RunResult{
					Status: RunStatusFailed,
					Reason: err.Error(),
				}
			}
		} else {
			result = &RunResult{
				Status: RunStatusSkipped,
				Reason: "no-handler",
			}
		}

		// Update state
		r.mu.Lock()
		if state, ok := r.agents[agent.AgentID]; ok {
			state.LastRunMs = now
			state.NextDueMs = now + state.IntervalMs
		}
		r.mu.Unlock()

		// Emit event
		indicator := resolveIndicator(result.Status, result.Reason)
		visibility := agent.Config.Visibility
		if visibility == nil {
			visibility = &Visibility{ShowOk: false, ShowAlerts: true, UseIndicator: true}
		}

		if visibility.UseIndicator || result.Status == RunStatusFailed {
			r.emitEvent(&HeartbeatEvent{
				Status:     result.Status,
				Reason:     result.Reason,
				AgentID:    agent.AgentID,
				Channel:    result.Channel,
				Preview:    result.Preview,
				DurationMs: result.DurationMs,
				HasMedia:   result.HasMedia,
				Indicator:  indicator,
				Timestamp:  time.Now(),
			})
		}

		lastResult = result
	}

	r.mu.Lock()
	r.scheduleNextLocked()
	r.mu.Unlock()

	if lastResult == nil {
		return &RunResult{
			Status:     RunStatusRan,
			DurationMs: time.Since(startedAt).Milliseconds(),
		}, nil
	}

	lastResult.DurationMs = time.Since(startedAt).Milliseconds()
	return lastResult, nil
}

// emitEvent sends a heartbeat event to the callback.
func (r *Runner) emitEvent(event *HeartbeatEvent) {
	if r.onEvent != nil {
		r.onEvent(event)
	}
}

// resolveIndicator determines the indicator type from status and reason.
func resolveIndicator(status RunStatus, reason string) IndicatorType {
	switch status {
	case RunStatusFailed:
		return IndicatorFailed
	case RunStatusRan:
		switch reason {
		case "ok-empty":
			return IndicatorOkEmpty
		case "ok-token":
			return IndicatorOkToken
		default:
			return IndicatorSent
		}
	default:
		return ""
	}
}

// ListAgents returns all registered agent IDs.
func (r *Runner) ListAgents() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]string, 0, len(r.agents))
	for id := range r.agents {
		agents = append(agents, id)
	}
	return agents
}

// GetAgentState returns the state for an agent.
func (r *Runner) GetAgentState(agentID string) *AgentHeartbeatState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if state, ok := r.agents[agentID]; ok {
		// Return a copy
		copied := *state
		return &copied
	}
	return nil
}

// Stats returns heartbeat runner statistics.
func (r *Runner) Stats() *RunnerStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := &RunnerStats{
		TotalAgents:   len(r.agents),
		EnabledAgents: 0,
		Stopped:       r.stopped,
		AgentStats:    make(map[string]*AgentStats),
	}

	now := time.Now().UnixMilli()
	for id, agent := range r.agents {
		if agent.Config.Enabled {
			stats.EnabledAgents++
		}

		var nextDueIn int64
		if agent.NextDueMs > now {
			nextDueIn = agent.NextDueMs - now
		}

		stats.AgentStats[id] = &AgentStats{
			AgentID:    id,
			Enabled:    agent.Config.Enabled,
			IntervalMs: agent.IntervalMs,
			LastRunMs:  agent.LastRunMs,
			NextDueMs:  agent.NextDueMs,
			NextDueIn:  nextDueIn,
		}
	}

	return stats
}

// RunnerStats contains heartbeat runner statistics.
type RunnerStats struct {
	TotalAgents   int                   `json:"total_agents"`
	EnabledAgents int                   `json:"enabled_agents"`
	Stopped       bool                  `json:"stopped"`
	AgentStats    map[string]*AgentStats `json:"agent_stats"`
}

// AgentStats contains per-agent heartbeat statistics.
type AgentStats struct {
	AgentID    string `json:"agent_id"`
	Enabled    bool   `json:"enabled"`
	IntervalMs int64  `json:"interval_ms"`
	LastRunMs  int64  `json:"last_run_ms"`
	NextDueMs  int64  `json:"next_due_ms"`
	NextDueIn  int64  `json:"next_due_in_ms"`
}

// WakeRequest triggers an immediate heartbeat wake.
type WakeRequest struct {
	Reason     string `json:"reason"`
	AgentID    string `json:"agent_id,omitempty"`
	CoalesceMs int64  `json:"coalesce_ms,omitempty"`
}

// WakeHandler handles heartbeat wake requests.
type WakeHandler func(ctx context.Context, req *WakeRequest) (*RunResult, error)

var (
	globalWakeHandler WakeHandler
	wakeHandlerMu     sync.RWMutex
)

// SetWakeHandler sets the global wake handler.
func SetWakeHandler(handler WakeHandler) {
	wakeHandlerMu.Lock()
	defer wakeHandlerMu.Unlock()
	globalWakeHandler = handler
}

// RequestWakeNow triggers an immediate heartbeat.
func RequestWakeNow(ctx context.Context, req *WakeRequest) (*RunResult, error) {
	wakeHandlerMu.RLock()
	handler := globalWakeHandler
	wakeHandlerMu.RUnlock()

	if handler == nil {
		return &RunResult{Status: RunStatusSkipped, Reason: "no-handler"}, nil
	}

	return handler(ctx, req)
}
