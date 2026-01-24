// Package heartbeat provides agent health monitoring through periodic checks.
// Agents can emit heartbeats to signal they're running, and the system can
// check heartbeat status to detect stalled or crashed agents.
package heartbeat

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// Token is the marker that agents emit to signal a heartbeat response.
	Token = "HEARTBEAT_OK"
	// DefaultInterval is how often agents should heartbeat.
	DefaultInterval = 30 * time.Minute
	// DefaultPrompt is the default heartbeat prompt.
	DefaultPrompt = "Read HEARTBEAT.md if it exists (workspace context). Follow it strictly. Do not infer or repeat old tasks from prior chats. If nothing needs attention, reply HEARTBEAT_OK."
	// DefaultMaxAckChars is the max chars for a heartbeat acknowledgment.
	DefaultMaxAckChars = 300
)

// Status represents the heartbeat status of an agent.
type Status struct {
	AgentID      string    `json:"agent_id"`
	LastSeen     time.Time `json:"last_seen"`
	LastResponse string    `json:"last_response,omitempty"`
	Healthy      bool      `json:"healthy"`
	MissedCount  int       `json:"missed_count"`
}

// IsStale returns true if the status is older than the given threshold.
func (s *Status) IsStale(threshold time.Duration) bool {
	return time.Since(s.LastSeen) > threshold
}

// Config configures heartbeat behavior.
type Config struct {
	// Enabled controls whether heartbeats are active.
	Enabled bool `yaml:"enabled"`
	// Interval is how often to send heartbeats.
	Interval time.Duration `yaml:"interval"`
	// Prompt is the heartbeat prompt text.
	Prompt string `yaml:"prompt"`
	// MaxAckChars is the maximum characters for acknowledgment.
	MaxAckChars int `yaml:"max_ack_chars"`
	// MissedThreshold is how many missed heartbeats before unhealthy.
	MissedThreshold int `yaml:"missed_threshold"`
}

// DefaultConfig returns a config with default values.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Interval:        DefaultInterval,
		Prompt:          DefaultPrompt,
		MaxAckChars:     DefaultMaxAckChars,
		MissedThreshold: 3,
	}
}

// Monitor tracks heartbeat status for multiple agents.
type Monitor struct {
	mu       sync.RWMutex
	config   Config
	statuses map[string]*Status
}

// NewMonitor creates a new heartbeat monitor.
func NewMonitor(config Config) *Monitor {
	if config.Interval <= 0 {
		config.Interval = DefaultInterval
	}
	if config.MaxAckChars <= 0 {
		config.MaxAckChars = DefaultMaxAckChars
	}
	if config.MissedThreshold <= 0 {
		config.MissedThreshold = 3
	}

	return &Monitor{
		config:   config,
		statuses: make(map[string]*Status),
	}
}

// Record records a heartbeat from an agent.
func (m *Monitor) Record(agentID, response string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status, exists := m.statuses[agentID]
	if !exists {
		status = &Status{AgentID: agentID}
		m.statuses[agentID] = status
	}

	status.LastSeen = time.Now()
	status.LastResponse = response
	status.Healthy = true
	status.MissedCount = 0
}

// Check checks the health of an agent.
func (m *Monitor) Check(agentID string) *Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	status, exists := m.statuses[agentID]
	if !exists {
		return &Status{
			AgentID: agentID,
			Healthy: false,
		}
	}

	// Check if stale
	if status.IsStale(m.config.Interval * 2) {
		status.Healthy = false
		status.MissedCount++
	}

	return status
}

// MarkMissed marks an agent as having missed a heartbeat.
func (m *Monitor) MarkMissed(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status, exists := m.statuses[agentID]
	if !exists {
		status = &Status{AgentID: agentID}
		m.statuses[agentID] = status
	}

	status.MissedCount++
	if status.MissedCount >= m.config.MissedThreshold {
		status.Healthy = false
	}
}

// GetStatus returns the status for an agent.
func (m *Monitor) GetStatus(agentID string) *Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.statuses[agentID]
	if !exists {
		return nil
	}
	// Return a copy
	s := *status
	return &s
}

// GetAllStatuses returns all agent statuses.
func (m *Monitor) GetAllStatuses() []*Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Status, 0, len(m.statuses))
	for _, status := range m.statuses {
		s := *status
		result = append(result, &s)
	}
	return result
}

// Remove removes an agent from monitoring.
func (m *Monitor) Remove(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.statuses, agentID)
}

// GetHealthyCount returns the number of healthy agents.
func (m *Monitor) GetHealthyCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, status := range m.statuses {
		if status.Healthy {
			count++
		}
	}
	return count
}

// StripResult contains the result of stripping heartbeat tokens.
type StripResult struct {
	// ShouldSkip indicates the message should be suppressed.
	ShouldSkip bool
	// Text is the remaining text after stripping.
	Text string
	// DidStrip indicates whether any tokens were removed.
	DidStrip bool
}

// stripMarkup removes HTML tags and markdown wrappers.
func stripMarkup(text string) string {
	// Remove HTML tags
	htmlRegex := regexp.MustCompile(`<[^>]*>`)
	text = htmlRegex.ReplaceAllString(text, " ")
	// Remove &nbsp;
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	// Remove markdown wrappers at edges
	text = strings.TrimLeft(text, "*`~_")
	text = strings.TrimRight(text, "*`~_")
	return text
}

// stripTokenAtEdges removes the heartbeat token from the start and end of text.
func stripTokenAtEdges(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", false
	}

	if !strings.Contains(text, Token) {
		return text, false
	}

	didStrip := false
	changed := true
	for changed {
		changed = false
		text = strings.TrimSpace(text)

		if strings.HasPrefix(text, Token) {
			text = strings.TrimSpace(text[len(Token):])
			didStrip = true
			changed = true
			continue
		}
		if strings.HasSuffix(text, Token) {
			text = strings.TrimSpace(text[:len(text)-len(Token)])
			didStrip = true
			changed = true
		}
	}

	// Collapse whitespace
	wsRegex := regexp.MustCompile(`\s+`)
	text = wsRegex.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	return text, didStrip
}

// StripToken removes heartbeat tokens from a response.
// Used to clean up agent responses that include the HEARTBEAT_OK marker.
func StripToken(raw string, maxAckChars int) StripResult {
	if raw == "" {
		return StripResult{ShouldSkip: true, Text: "", DidStrip: false}
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return StripResult{ShouldSkip: true, Text: "", DidStrip: false}
	}

	if maxAckChars <= 0 {
		maxAckChars = DefaultMaxAckChars
	}

	// Check both original and normalized versions
	normalized := stripMarkup(trimmed)
	hasToken := strings.Contains(trimmed, Token) || strings.Contains(normalized, Token)

	if !hasToken {
		return StripResult{ShouldSkip: false, Text: trimmed, DidStrip: false}
	}

	// Try stripping from original first, then normalized
	strippedOrig, didStripOrig := stripTokenAtEdges(trimmed)
	strippedNorm, didStripNorm := stripTokenAtEdges(normalized)

	var text string
	var didStrip bool
	if didStripOrig && strippedOrig != "" {
		text = strippedOrig
		didStrip = true
	} else if didStripNorm {
		text = strippedNorm
		didStrip = true
	} else {
		return StripResult{ShouldSkip: false, Text: trimmed, DidStrip: false}
	}

	if text == "" {
		return StripResult{ShouldSkip: true, Text: "", DidStrip: true}
	}

	// If remaining text is short enough, consider it an ack and skip
	if len(text) <= maxAckChars {
		return StripResult{ShouldSkip: true, Text: "", DidStrip: true}
	}

	return StripResult{ShouldSkip: false, Text: text, DidStrip: didStrip}
}

// ResolvePrompt returns the heartbeat prompt, using default if empty.
func ResolvePrompt(custom string) string {
	trimmed := strings.TrimSpace(custom)
	if trimmed == "" {
		return DefaultPrompt
	}
	return trimmed
}
