// Package commands provides the health command for system status checks.
package commands

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// HealthSummary contains the overall health status.
type HealthSummary struct {
	OK                bool                      `json:"ok"`
	Ts                int64                     `json:"ts"`
	DurationMs        int64                     `json:"duration_ms"`
	Channels          map[string]*ChannelHealth `json:"channels"`
	ChannelOrder      []string                  `json:"channel_order"`
	ChannelLabels     map[string]string         `json:"channel_labels"`
	DefaultAgentID    string                    `json:"default_agent_id"`
	Agents            []*AgentHealth            `json:"agents"`
	LinkUnderstanding *LinkUnderstandingHealth  `json:"link_understanding,omitempty"`
	Sessions          *SessionsHealth           `json:"sessions"`
}

// ChannelHealth contains health status for a channel.
type ChannelHealth struct {
	AccountID   string                    `json:"account_id"`
	Configured  bool                      `json:"configured"`
	Linked      bool                      `json:"linked"`
	AuthAgeMs   *int64                    `json:"auth_age_ms,omitempty"`
	Probe       *ChannelProbe             `json:"probe,omitempty"`
	LastProbeAt *int64                    `json:"last_probe_at,omitempty"`
	Accounts    map[string]*ChannelHealth `json:"accounts,omitempty"`
}

// ChannelProbe contains probe results for a channel.
type ChannelProbe struct {
	OK        bool     `json:"ok"`
	ElapsedMs int64    `json:"elapsed_ms,omitempty"`
	Status    int      `json:"status,omitempty"`
	Error     string   `json:"error,omitempty"`
	Bot       *BotInfo `json:"bot,omitempty"`
}

// BotInfo contains bot information from a probe.
type BotInfo struct {
	ID       string `json:"id,omitempty"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
}

// AgentHealth contains health status for an agent.
type AgentHealth struct {
	AgentID   string           `json:"agent_id"`
	Name      string           `json:"name,omitempty"`
	IsDefault bool             `json:"is_default"`
	Heartbeat *HeartbeatHealth `json:"heartbeat"`
	Sessions  *SessionsHealth  `json:"sessions,omitempty"`
}

// LinkUnderstandingHealth contains link understanding configuration status.
type LinkUnderstandingHealth struct {
	Enabled        bool   `json:"enabled"`
	MaxLinks       int    `json:"max_links"`
	MaxOutputChars int    `json:"max_output_chars"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	ModelCount     int    `json:"model_count"`
	ScopeMode      string `json:"scope_mode"`
	ScopeAllowlist int    `json:"scope_allowlist,omitempty"`
	ScopeDenylist  int    `json:"scope_denylist,omitempty"`
}

// HeartbeatHealth contains heartbeat configuration status.
type HeartbeatHealth struct {
	Enabled     bool   `json:"enabled"`
	Every       string `json:"every"`
	EveryMs     *int64 `json:"every_ms,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Target      string `json:"target,omitempty"`
	Model       string `json:"model,omitempty"`
	AckMaxChars int    `json:"ack_max_chars,omitempty"`
}

// SessionsHealth contains session store status.
type SessionsHealth struct {
	Path   string           `json:"path"`
	Count  int              `json:"count"`
	Recent []*RecentSession `json:"recent"`
}

// RecentSession contains information about a recent session.
type RecentSession struct {
	Key       string `json:"key"`
	UpdatedAt *int64 `json:"updated_at,omitempty"`
	AgeMs     *int64 `json:"age_ms,omitempty"`
}

// HealthChecker performs health checks on the system.
type HealthChecker struct {
	mu      sync.RWMutex
	probers map[string]ChannelProber
	config  *HealthCheckerConfig
}

// HealthCheckerConfig configures the health checker.
type HealthCheckerConfig struct {
	TimeoutMs       int64
	ProbeChannels   bool
	IncludeAgents   bool
	IncludeSessions bool
}

// DefaultHealthCheckerConfig returns sensible defaults.
func DefaultHealthCheckerConfig() *HealthCheckerConfig {
	return &HealthCheckerConfig{
		TimeoutMs:       10000,
		ProbeChannels:   true,
		IncludeAgents:   true,
		IncludeSessions: true,
	}
}

// ChannelProber probes a channel for health status.
type ChannelProber interface {
	Probe(ctx context.Context, accountID string) (*ChannelProbe, error)
	Label() string
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(config *HealthCheckerConfig) *HealthChecker {
	if config == nil {
		config = DefaultHealthCheckerConfig()
	}
	return &HealthChecker{
		probers: make(map[string]ChannelProber),
		config:  config,
	}
}

// RegisterProber registers a channel prober.
func (h *HealthChecker) RegisterProber(channel string, prober ChannelProber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.probers[channel] = prober
}

// Check performs a health check.
func (h *HealthChecker) Check(ctx context.Context, opts *HealthCheckOptions) (*HealthSummary, error) {
	startedAt := time.Now()

	if opts == nil {
		opts = &HealthCheckOptions{}
	}

	timeout := time.Duration(h.config.TimeoutMs) * time.Millisecond
	if opts.TimeoutMs > 0 {
		timeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	summary := &HealthSummary{
		OK:            true,
		Ts:            startedAt.UnixMilli(),
		Channels:      make(map[string]*ChannelHealth),
		ChannelOrder:  make([]string, 0),
		ChannelLabels: make(map[string]string),
		Agents:        make([]*AgentHealth, 0),
	}

	// Check channels
	if h.config.ProbeChannels && opts.ProbeChannels != nil && *opts.ProbeChannels {
		h.mu.RLock()
		probers := make(map[string]ChannelProber, len(h.probers))
		for k, v := range h.probers {
			probers[k] = v
		}
		h.mu.RUnlock()

		var wg sync.WaitGroup
		var mu sync.Mutex
		probeFailed := false

		for channel, prober := range probers {
			summary.ChannelOrder = append(summary.ChannelOrder, channel)
			summary.ChannelLabels[channel] = prober.Label()

			wg.Add(1)
			go func(ch string, p ChannelProber) {
				defer wg.Done()

				probe, err := p.Probe(ctx, "")

				mu.Lock()
				defer mu.Unlock()

				health := &ChannelHealth{
					AccountID:  "default",
					Configured: true,
				}

				if err != nil {
					health.Probe = &ChannelProbe{
						OK:    false,
						Error: err.Error(),
					}
					probeFailed = true
				} else if probe != nil {
					health.Probe = probe
					health.Linked = probe.OK
					now := time.Now().UnixMilli()
					health.LastProbeAt = &now
					if !probe.OK {
						probeFailed = true
					}
				}

				summary.Channels[ch] = health
			}(channel, prober)
		}

		wg.Wait()
		if probeFailed {
			summary.OK = false
		}
	}

	// Add agent health from options
	if h.config.IncludeAgents && opts.Agents != nil {
		summary.Agents = opts.Agents
	}
	if opts.DefaultAgentID != "" {
		summary.DefaultAgentID = opts.DefaultAgentID
	}

	// Add sessions health from options
	if h.config.IncludeSessions && opts.Sessions != nil {
		summary.Sessions = opts.Sessions
	}
	if opts.LinkUnderstanding != nil {
		summary.LinkUnderstanding = opts.LinkUnderstanding
	}

	summary.DurationMs = time.Since(startedAt).Milliseconds()
	return summary, nil
}

// HealthCheckOptions configures a health check.
type HealthCheckOptions struct {
	TimeoutMs         int64
	ProbeChannels     *bool
	DefaultAgentID    string
	Agents            []*AgentHealth
	Sessions          *SessionsHealth
	LinkUnderstanding *LinkUnderstandingHealth
}

// FormatHealthSummary formats a health summary for display.
func FormatHealthSummary(summary *HealthSummary) string {
	if summary == nil {
		return "No health data"
	}

	result := fmt.Sprintf("Health Check (took %dms)\n", summary.DurationMs)
	result += fmt.Sprintf("Status: %s\n", formatOK(summary.OK))

	if len(summary.Channels) > 0 {
		result += "\nChannels:\n"
		for _, ch := range summary.ChannelOrder {
			health := summary.Channels[ch]
			if health == nil {
				continue
			}
			label := summary.ChannelLabels[ch]
			if label == "" {
				label = ch
			}
			result += fmt.Sprintf("  %s: %s\n", label, formatChannelHealth(health))
		}
	}

	if len(summary.Agents) > 0 {
		result += "\nAgents:\n"
		for _, agent := range summary.Agents {
			defaultStr := ""
			if agent.IsDefault {
				defaultStr = " (default)"
			}
			result += fmt.Sprintf("  %s%s\n", agent.AgentID, defaultStr)
			if agent.Heartbeat != nil {
				if agent.Heartbeat.Enabled {
					result += fmt.Sprintf("    heartbeat: every %s\n", agent.Heartbeat.Every)
				} else {
					result += "    heartbeat: disabled\n"
				}
			}
		}
	}

	if summary.Sessions != nil {
		result += fmt.Sprintf("\nSessions: %d total\n", summary.Sessions.Count)
		if len(summary.Sessions.Recent) > 0 {
			result += "  Recent:\n"
			for _, s := range summary.Sessions.Recent {
				age := "unknown"
				if s.AgeMs != nil {
					age = formatDuration(time.Duration(*s.AgeMs) * time.Millisecond)
				}
				result += fmt.Sprintf("    %s (%s ago)\n", s.Key, age)
			}
		}
	}

	if summary.LinkUnderstanding != nil {
		lu := summary.LinkUnderstanding
		status := "disabled"
		if lu.Enabled {
			status = "enabled"
		}
		details := make([]string, 0, 6)
		if lu.MaxLinks > 0 {
			details = append(details, fmt.Sprintf("max_links=%d", lu.MaxLinks))
		}
		if lu.MaxOutputChars > 0 {
			details = append(details, fmt.Sprintf("max_output_chars=%d", lu.MaxOutputChars))
		}
		if lu.TimeoutSeconds > 0 {
			details = append(details, fmt.Sprintf("timeout=%ds", lu.TimeoutSeconds))
		}
		details = append(details, fmt.Sprintf("models=%d", lu.ModelCount))
		if lu.ScopeMode != "" {
			details = append(details, fmt.Sprintf("scope=%s", lu.ScopeMode))
		}
		if lu.ScopeMode == "allowlist" {
			details = append(details, fmt.Sprintf("allowlist=%d", lu.ScopeAllowlist))
		}
		if lu.ScopeMode == "denylist" {
			details = append(details, fmt.Sprintf("denylist=%d", lu.ScopeDenylist))
		}

		if len(details) > 0 {
			result += fmt.Sprintf("\nLink Understanding: %s (%s)\n", status, strings.Join(details, ", "))
		} else {
			result += fmt.Sprintf("\nLink Understanding: %s\n", status)
		}
	}

	return result
}

func formatOK(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAILED"
}

func formatChannelHealth(health *ChannelHealth) string {
	if health.Probe == nil {
		if health.Configured {
			return "configured (not probed)"
		}
		return "not configured"
	}

	if health.Probe.OK {
		result := "ok"
		if health.Probe.Bot != nil && health.Probe.Bot.Username != "" {
			result += fmt.Sprintf(" (@%s)", health.Probe.Bot.Username)
		}
		if health.Probe.ElapsedMs > 0 {
			result += fmt.Sprintf(" (%dms)", health.Probe.ElapsedMs)
		}
		return result
	}

	result := "failed"
	if health.Probe.Status > 0 {
		result += fmt.Sprintf(" (%d)", health.Probe.Status)
	}
	if health.Probe.Error != "" {
		result += fmt.Sprintf(" - %s", health.Probe.Error)
	}
	return result
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

// HealthServer provides health check endpoints.
type HealthServer struct {
	checker *HealthChecker
}

// NewHealthServer creates a new health server.
func NewHealthServer(checker *HealthChecker) *HealthServer {
	return &HealthServer{checker: checker}
}

// HandleHealth handles health check requests.
func (s *HealthServer) HandleHealth(ctx context.Context, opts *HealthCheckOptions) (*HealthSummary, error) {
	return s.checker.Check(ctx, opts)
}

// QuickHealth returns a quick health status without probing.
func (s *HealthServer) QuickHealth() *HealthSummary {
	return &HealthSummary{
		OK: true,
		Ts: time.Now().UnixMilli(),
	}
}
