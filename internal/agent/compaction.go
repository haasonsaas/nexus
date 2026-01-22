package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentctx "github.com/haasonsaas/nexus/internal/agent/context"
	"github.com/haasonsaas/nexus/pkg/models"
)

// CompactionState tracks the current compaction status for a conversation session.
type CompactionState string

const (
	// CompactionIdle means no compaction is pending.
	CompactionIdle CompactionState = "idle"
	// CompactionPending means compaction is needed but awaiting flush.
	CompactionPending CompactionState = "pending"
	// CompactionAwaitingConfirm means flush was requested, waiting for confirmation.
	CompactionAwaitingConfirm CompactionState = "awaiting_confirm"
	// CompactionInProgress means compaction is running.
	CompactionInProgress CompactionState = "in_progress"
)

// CompactionConfig configures automatic context compaction behavior including
// thresholds, prompts, and timeout handling.
type CompactionConfig struct {
	// Enabled turns on automatic compaction monitoring.
	Enabled bool

	// ThresholdPercent is the context usage percentage (0-100) that triggers flush.
	// Default: 80.
	ThresholdPercent int

	// FlushPrompt is the message sent to prompt memory flush.
	FlushPrompt string

	// ConfirmationTimeout is how long to wait for flush confirmation.
	// Default: 5 minutes.
	ConfirmationTimeout time.Duration

	// AutoCompactOnTimeout compacts automatically if confirmation times out.
	// Default: true.
	AutoCompactOnTimeout bool
}

// DefaultCompactionConfig returns sensible defaults with 80% threshold and 5 minute timeout.
func DefaultCompactionConfig() *CompactionConfig {
	return &CompactionConfig{
		Enabled:              true,
		ThresholdPercent:     80,
		FlushPrompt:          "Session nearing compaction. If there are durable facts, store them in memory/YYYY-MM-DD.md or MEMORY.md. Reply NO_REPLY if nothing needs attention.",
		ConfirmationTimeout:  5 * time.Minute,
		AutoCompactOnTimeout: true,
	}
}

// CompactionManager monitors context usage and triggers compaction when thresholds are exceeded.
type CompactionManager struct {
	mu       sync.RWMutex
	config   *CompactionConfig
	packer   *agentctx.Packer
	sessions map[string]*sessionCompaction

	// Callback for when compaction is needed
	onFlushRequired func(ctx context.Context, sessionID string, prompt string) error
	// Callback for when compaction completes
	onCompactionComplete func(ctx context.Context, sessionID string, dropped int) error
}

type sessionCompaction struct {
	state        CompactionState
	lastCheck    time.Time
	flushSentAt  time.Time
	usagePercent int
}

// NewCompactionManager creates a new compaction manager with the given configuration and packer.
// If config is nil, DefaultCompactionConfig is used.
func NewCompactionManager(config *CompactionConfig, packer *agentctx.Packer) *CompactionManager {
	if config == nil {
		config = DefaultCompactionConfig()
	}
	return &CompactionManager{
		config:   config,
		packer:   packer,
		sessions: make(map[string]*sessionCompaction),
	}
}

// SetFlushCallback sets the function called when a memory flush is required before compaction.
func (m *CompactionManager) SetFlushCallback(fn func(ctx context.Context, sessionID string, prompt string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFlushRequired = fn
}

// SetCompactionCallback sets the function called when compaction completes successfully.
func (m *CompactionManager) SetCompactionCallback(fn func(ctx context.Context, sessionID string, dropped int) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCompactionComplete = fn
}

// Check evaluates context usage and triggers a flush if the threshold is exceeded.
// Returns true if compaction was triggered, false otherwise.
func (m *CompactionManager) Check(ctx context.Context, sessionID string, history []*models.Message, incoming *models.Message, summary *models.Message) (bool, error) {
	if !m.config.Enabled || m.packer == nil {
		return false, nil
	}

	// Pack to get diagnostics
	result := m.packer.PackWithDiagnostics(history, incoming, summary)
	if result.Diagnostics == nil {
		return false, nil
	}

	// Calculate usage percentage
	usagePercent := 0
	if result.Diagnostics.BudgetChars > 0 {
		usagePercent = (result.Diagnostics.UsedChars * 100) / result.Diagnostics.BudgetChars
	}

	m.mu.Lock()
	session := m.sessions[sessionID]
	if session == nil {
		session = &sessionCompaction{state: CompactionIdle}
		m.sessions[sessionID] = session
	}
	session.lastCheck = time.Now()
	session.usagePercent = usagePercent

	// Check if we need to trigger flush
	if usagePercent >= m.config.ThresholdPercent && session.state == CompactionIdle {
		session.state = CompactionPending
		session.flushSentAt = time.Now()
		flushCallback := m.onFlushRequired
		prompt := m.config.FlushPrompt
		m.mu.Unlock()

		// Trigger flush callback
		if flushCallback != nil {
			if err := flushCallback(ctx, sessionID, prompt); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	// Check for confirmation timeout
	if session.state == CompactionAwaitingConfirm {
		if time.Since(session.flushSentAt) > m.config.ConfirmationTimeout {
			if m.config.AutoCompactOnTimeout {
				session.state = CompactionInProgress
				m.mu.Unlock()
				return m.performCompaction(ctx, sessionID, result.Diagnostics.Dropped)
			}
			// Reset to idle if not auto-compacting
			session.state = CompactionIdle
		}
	}
	m.mu.Unlock()

	return false, nil
}

// ConfirmFlush confirms that memory flush is complete and proceeds with compaction.
func (m *CompactionManager) ConfirmFlush(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	session := m.sessions[sessionID]
	if session == nil {
		m.mu.Unlock()
		return nil
	}

	if session.state == CompactionPending || session.state == CompactionAwaitingConfirm {
		session.state = CompactionInProgress
		m.mu.Unlock()

		// Perform compaction
		_, err := m.performCompaction(ctx, sessionID, 0)
		return err
	}
	m.mu.Unlock()
	return nil
}

// RejectFlush rejects the flush request and proceeds with compaction without saving.
func (m *CompactionManager) RejectFlush(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	session := m.sessions[sessionID]
	if session != nil && (session.state == CompactionPending || session.state == CompactionAwaitingConfirm) {
		session.state = CompactionInProgress
		m.mu.Unlock()

		// Proceed with compaction anyway
		_, err := m.performCompaction(ctx, sessionID, 0)
		return err
	}
	m.mu.Unlock()
	return nil
}

// GetState returns the current compaction state for a session.
func (m *CompactionManager) GetState(sessionID string) CompactionState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session := m.sessions[sessionID]
	if session == nil {
		return CompactionIdle
	}
	return session.state
}

// GetUsage returns the last known context usage percentage (0-100) for a session.
func (m *CompactionManager) GetUsage(sessionID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session := m.sessions[sessionID]
	if session == nil {
		return 0
	}
	return session.usagePercent
}

// performCompaction executes the compaction and notifies via callback.
func (m *CompactionManager) performCompaction(ctx context.Context, sessionID string, dropped int) (bool, error) {
	m.mu.Lock()
	callback := m.onCompactionComplete
	session := m.sessions[sessionID]
	if session != nil {
		session.state = CompactionIdle
	}
	m.mu.Unlock()

	if callback != nil {
		if err := callback(ctx, sessionID, dropped); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Reset clears all compaction state for a session, returning it to idle.
func (m *CompactionManager) Reset(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// CompactionInfo contains diagnostic information about a session's compaction state.
type CompactionInfo struct {
	SessionID    string          `json:"session_id"`
	State        CompactionState `json:"state"`
	UsagePercent int             `json:"usage_percent"`
	LastCheck    time.Time       `json:"last_check"`
	FlushSentAt  time.Time       `json:"flush_sent_at,omitempty"`
	Threshold    int             `json:"threshold"`
}

// GetInfo returns detailed diagnostic information about a session's compaction state.
func (m *CompactionManager) GetInfo(sessionID string) *CompactionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session := m.sessions[sessionID]
	if session == nil {
		return &CompactionInfo{
			SessionID: sessionID,
			State:     CompactionIdle,
			Threshold: m.config.ThresholdPercent,
		}
	}
	return &CompactionInfo{
		SessionID:    sessionID,
		State:        session.state,
		UsagePercent: session.usagePercent,
		LastCheck:    session.lastCheck,
		FlushSentAt:  session.flushSentAt,
		Threshold:    m.config.ThresholdPercent,
	}
}

// IsFlushResponse checks if a message content contains patterns indicating a flush response.
func IsFlushResponse(content string) bool {
	// Check for common acknowledgment patterns
	lowerContent := content
	if len(lowerContent) > 50 {
		lowerContent = lowerContent[:50]
	}
	patterns := []string{
		"no_reply",
		"NO_REPLY",
		"nothing to save",
		"nothing needs attention",
		"saved to memory",
		"stored in memory",
		"memory updated",
	}
	for _, p := range patterns {
		if containsFlushPattern(lowerContent, p) {
			return true
		}
	}
	return false
}

func containsFlushPattern(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// CompactionTool provides a Tool interface for querying compaction status from within the agent.
type CompactionTool struct {
	manager *CompactionManager
}

// NewCompactionTool creates a tool for querying compaction status.
func NewCompactionTool(manager *CompactionManager) *CompactionTool {
	return &CompactionTool{manager: manager}
}

// Name returns the tool name for LLM function calling.
func (t *CompactionTool) Name() string {
	return "compaction_status"
}

// Description returns a natural language description of the tool for LLM guidance.
func (t *CompactionTool) Description() string {
	return "Check context compaction status and usage. Use to monitor when memory flush may be needed."
}

// Schema returns the JSON Schema for tool parameters (empty for this tool).
func (t *CompactionTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

// Execute returns the current compaction status for the session in context.
func (t *CompactionTool) Execute(ctx context.Context, input []byte) (string, error) {
	session := SessionFromContext(ctx)
	if session == nil {
		return "no session context", nil
	}

	info := t.manager.GetInfo(session.ID)
	return fmt.Sprintf("Session: %s\nState: %s\nUsage: %d%%\nThreshold: %d%%",
		info.SessionID, info.State, info.UsagePercent, info.Threshold), nil
}
