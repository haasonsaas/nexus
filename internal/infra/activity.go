package infra

import (
	"sync"
	"time"
)

// ActivityDirection indicates whether activity was inbound or outbound.
type ActivityDirection string

const (
	// ActivityInbound represents incoming messages/events.
	ActivityInbound ActivityDirection = "inbound"
	// ActivityOutbound represents outgoing messages/events.
	ActivityOutbound ActivityDirection = "outbound"
)

// ActivityEntry tracks activity timestamps for a channel.
type ActivityEntry struct {
	// InboundAt is when the last inbound activity occurred.
	InboundAt *time.Time `json:"inbound_at,omitempty"`

	// OutboundAt is when the last outbound activity occurred.
	OutboundAt *time.Time `json:"outbound_at,omitempty"`

	// InboundCount is the total inbound activity count.
	InboundCount int64 `json:"inbound_count"`

	// OutboundCount is the total outbound activity count.
	OutboundCount int64 `json:"outbound_count"`
}

// ActivityTracker tracks channel activity for health monitoring.
type ActivityTracker struct {
	mu       sync.RWMutex
	entries  map[string]*ActivityEntry
	onChange func(channel, accountID string, direction ActivityDirection)
}

// NewActivityTracker creates a new activity tracker.
func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		entries: make(map[string]*ActivityEntry),
	}
}

// OnChange sets a callback to be invoked when activity is recorded.
func (t *ActivityTracker) OnChange(fn func(channel, accountID string, direction ActivityDirection)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onChange = fn
}

func (t *ActivityTracker) key(channel, accountID string) string {
	if accountID == "" {
		accountID = "default"
	}
	return channel + ":" + accountID
}

// Record records activity for a channel.
func (t *ActivityTracker) Record(channel, accountID string, direction ActivityDirection) {
	t.RecordAt(channel, accountID, direction, time.Now())
}

// RecordAt records activity for a channel at a specific time.
func (t *ActivityTracker) RecordAt(channel, accountID string, direction ActivityDirection, at time.Time) {
	t.mu.Lock()

	key := t.key(channel, accountID)
	entry := t.entries[key]
	if entry == nil {
		entry = &ActivityEntry{}
		t.entries[key] = entry
	}

	switch direction {
	case ActivityInbound:
		entry.InboundAt = &at
		entry.InboundCount++
	case ActivityOutbound:
		entry.OutboundAt = &at
		entry.OutboundCount++
	}

	callback := t.onChange
	t.mu.Unlock()

	// Call callback outside lock
	if callback != nil {
		callback(channel, accountID, direction)
	}
}

// Get returns the activity entry for a channel.
func (t *ActivityTracker) Get(channel, accountID string) ActivityEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := t.key(channel, accountID)
	entry := t.entries[key]
	if entry == nil {
		return ActivityEntry{}
	}
	return *entry
}

// GetAll returns all activity entries.
func (t *ActivityTracker) GetAll() map[string]ActivityEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]ActivityEntry, len(t.entries))
	for k, v := range t.entries {
		result[k] = *v
	}
	return result
}

// LastActivity returns the most recent activity timestamp across all channels.
func (t *ActivityTracker) LastActivity() *time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var latest *time.Time
	for _, entry := range t.entries {
		if entry.InboundAt != nil && (latest == nil || entry.InboundAt.After(*latest)) {
			latest = entry.InboundAt
		}
		if entry.OutboundAt != nil && (latest == nil || entry.OutboundAt.After(*latest)) {
			latest = entry.OutboundAt
		}
	}
	return latest
}

// IdleDuration returns how long since the last activity.
func (t *ActivityTracker) IdleDuration() time.Duration {
	last := t.LastActivity()
	if last == nil {
		return 0
	}
	return time.Since(*last)
}

// IsIdle returns true if no activity has occurred within the given duration.
func (t *ActivityTracker) IsIdle(threshold time.Duration) bool {
	last := t.LastActivity()
	if last == nil {
		return true
	}
	return time.Since(*last) > threshold
}

// Reset clears all tracked activity.
func (t *ActivityTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = make(map[string]*ActivityEntry)
}

// HealthStatus summarizes the health of tracked channels.
type HealthStatus struct {
	// TotalChannels is the number of channels being tracked.
	TotalChannels int `json:"total_channels"`

	// ActiveChannels is the number of channels with recent activity.
	ActiveChannels int `json:"active_channels"`

	// IdleChannels is the number of channels without recent activity.
	IdleChannels int `json:"idle_channels"`

	// LastActivityAt is when the most recent activity occurred.
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`

	// IdleDuration is how long since the last activity.
	IdleDuration time.Duration `json:"idle_duration"`
}

// Health returns the health status based on the given idle threshold.
func (t *ActivityTracker) Health(idleThreshold time.Duration) HealthStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := HealthStatus{
		TotalChannels: len(t.entries),
	}

	now := time.Now()
	var latest *time.Time

	for _, entry := range t.entries {
		// Find latest activity for this entry
		var entryLatest *time.Time
		if entry.InboundAt != nil {
			entryLatest = entry.InboundAt
		}
		if entry.OutboundAt != nil && (entryLatest == nil || entry.OutboundAt.After(*entryLatest)) {
			entryLatest = entry.OutboundAt
		}

		// Update global latest
		if entryLatest != nil && (latest == nil || entryLatest.After(*latest)) {
			latest = entryLatest
		}

		// Check if active or idle
		if entryLatest != nil && now.Sub(*entryLatest) <= idleThreshold {
			status.ActiveChannels++
		} else {
			status.IdleChannels++
		}
	}

	status.LastActivityAt = latest
	if latest != nil {
		status.IdleDuration = now.Sub(*latest)
	}

	return status
}

// Global default activity tracker
var defaultActivityTracker = NewActivityTracker()

// RecordActivity records activity using the default tracker.
func RecordActivity(channel, accountID string, direction ActivityDirection) {
	defaultActivityTracker.Record(channel, accountID, direction)
}

// GetActivity returns activity from the default tracker.
func GetActivity(channel, accountID string) ActivityEntry {
	return defaultActivityTracker.Get(channel, accountID)
}

// GetActivityHealth returns health status from the default tracker.
func GetActivityHealth(idleThreshold time.Duration) HealthStatus {
	return defaultActivityTracker.Health(idleThreshold)
}
