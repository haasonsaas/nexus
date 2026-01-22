package sessions

import (
	"strings"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ResetMode constants for session expiry.
const (
	ResetModeNever     = "never"
	ResetModeDaily     = "daily"
	ResetModeIdle      = "idle"
	ResetModeDailyIdle = "daily+idle"
)

// ConversationType constants for reset configuration.
const (
	ConvTypeDM     = "dm"
	ConvTypeGroup  = "group"
	ConvTypeThread = "thread"
)

// SessionExpiry checks whether sessions should be reset based on configuration.
type SessionExpiry struct {
	cfg      ScopeConfig
	nowFunc  func() time.Time // For testing
	location *time.Location   // Timezone for daily resets
}

// NewSessionExpiry creates a new SessionExpiry checker.
func NewSessionExpiry(cfg ScopeConfig) *SessionExpiry {
	return &SessionExpiry{
		cfg:      cfg,
		nowFunc:  time.Now,
		location: time.Local,
	}
}

// NewSessionExpiryWithLocation creates a SessionExpiry with a specific timezone.
func NewSessionExpiryWithLocation(cfg ScopeConfig, loc *time.Location) *SessionExpiry {
	if loc == nil {
		loc = time.Local
	}
	return &SessionExpiry{
		cfg:      cfg,
		nowFunc:  time.Now,
		location: loc,
	}
}

// SetNowFunc sets a custom time function for testing.
func (e *SessionExpiry) SetNowFunc(fn func() time.Time) {
	e.nowFunc = fn
}

// CheckExpiry returns true if the session should be reset based on the configuration.
// Parameters:
//   - session: the session to check
//   - channel: the channel type for channel-specific reset rules
//   - convType: the conversation type (dm, group, thread) for type-specific rules
//
// The function checks reset rules in order of specificity:
// 1. Channel-specific rules (ResetByChannel)
// 2. Conversation type rules (ResetByType)
// 3. Default reset configuration
func (e *SessionExpiry) CheckExpiry(session *models.Session, channel models.ChannelType, convType string) bool {
	if session == nil {
		return false
	}

	// Find the most specific reset config
	resetCfg := e.getResetConfig(channel, convType)

	return e.checkResetConfig(session, resetCfg)
}

// CheckExpiryWithConfig checks expiry using a specific reset configuration.
func (e *SessionExpiry) CheckExpiryWithConfig(session *models.Session, resetCfg ResetConfig) bool {
	if session == nil {
		return false
	}
	return e.checkResetConfig(session, resetCfg)
}

// getResetConfig returns the most specific reset configuration.
func (e *SessionExpiry) getResetConfig(channel models.ChannelType, convType string) ResetConfig {
	// Check channel-specific config first
	if e.cfg.ResetByChannel != nil {
		if cfg, ok := e.cfg.ResetByChannel[string(channel)]; ok {
			return cfg
		}
	}

	// Check conversation type config
	if e.cfg.ResetByType != nil {
		if cfg, ok := e.cfg.ResetByType[convType]; ok {
			return cfg
		}
	}

	// Fall back to default
	return e.cfg.Reset
}

// checkResetConfig checks if a session should be reset based on a specific config.
func (e *SessionExpiry) checkResetConfig(session *models.Session, cfg ResetConfig) bool {
	now := e.nowFunc()
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))

	switch mode {
	case ResetModeNever, "":
		return false

	case ResetModeDaily:
		return e.checkDailyReset(session, cfg.AtHour, now)

	case ResetModeIdle:
		return e.checkIdleReset(session, cfg.IdleMinutes, now)

	case ResetModeDailyIdle:
		// Reset if EITHER condition is met
		return e.checkDailyReset(session, cfg.AtHour, now) ||
			e.checkIdleReset(session, cfg.IdleMinutes, now)

	default:
		return false
	}
}

// checkDailyReset returns true if the session should be reset based on daily schedule.
// A session should be reset if:
// - The configured reset hour has passed since the session was last updated
// - The session was last updated before today's reset hour
func (e *SessionExpiry) checkDailyReset(session *models.Session, atHour int, now time.Time) bool {
	if atHour < 0 || atHour > 23 {
		atHour = 0
	}

	// Get the last activity time (use UpdatedAt as proxy for last activity)
	lastActivity := session.UpdatedAt
	if lastActivity.IsZero() {
		lastActivity = session.CreatedAt
	}
	if lastActivity.IsZero() {
		return false
	}

	// Convert times to the configured timezone
	nowInLoc := now.In(e.location)
	lastActivityInLoc := lastActivity.In(e.location)

	// Find today's reset time
	todayReset := time.Date(
		nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(),
		atHour, 0, 0, 0,
		e.location,
	)

	// If the reset hour hasn't occurred today yet, check against yesterday's reset
	if nowInLoc.Hour() < atHour {
		todayReset = todayReset.AddDate(0, 0, -1)
	}

	// Session should be reset if last activity was before the reset time
	return lastActivityInLoc.Before(todayReset)
}

// checkIdleReset returns true if the session should be reset due to inactivity.
func (e *SessionExpiry) checkIdleReset(session *models.Session, idleMinutes int, now time.Time) bool {
	if idleMinutes <= 0 {
		return false
	}

	lastActivity := session.UpdatedAt
	if lastActivity.IsZero() {
		lastActivity = session.CreatedAt
	}
	if lastActivity.IsZero() {
		return false
	}

	idleDuration := time.Duration(idleMinutes) * time.Minute
	return now.Sub(lastActivity) >= idleDuration
}

// GetNextResetTime returns the next scheduled reset time, if any.
// Returns zero time if no reset is scheduled (e.g., mode is "never" or "idle" only).
func (e *SessionExpiry) GetNextResetTime(channel models.ChannelType, convType string) time.Time {
	resetCfg := e.getResetConfig(channel, convType)
	mode := strings.ToLower(strings.TrimSpace(resetCfg.Mode))

	if mode != ResetModeDaily && mode != ResetModeDailyIdle {
		return time.Time{}
	}

	now := e.nowFunc().In(e.location)
	atHour := resetCfg.AtHour
	if atHour < 0 || atHour > 23 {
		atHour = 0
	}

	// Calculate next reset time
	nextReset := time.Date(
		now.Year(), now.Month(), now.Day(),
		atHour, 0, 0, 0,
		e.location,
	)

	// If we've passed today's reset time, move to tomorrow
	if now.Hour() >= atHour {
		nextReset = nextReset.AddDate(0, 0, 1)
	}

	return nextReset
}

// ShouldResetSession is a convenience function that checks if a session should be reset.
// It uses the default reset configuration from ScopeConfig.
func ShouldResetSession(session *models.Session, cfg ScopeConfig) bool {
	expiry := NewSessionExpiry(cfg)
	// Default to DM type if not specified
	return expiry.CheckExpiry(session, session.Channel, ConvTypeDM)
}

// ShouldResetSessionWithType checks if a session should be reset with explicit type.
func ShouldResetSessionWithType(session *models.Session, cfg ScopeConfig, convType string) bool {
	expiry := NewSessionExpiry(cfg)
	return expiry.CheckExpiry(session, session.Channel, convType)
}
