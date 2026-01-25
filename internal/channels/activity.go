// Package channels provides channel activity tracking.
package channels

import (
	"sync"
	"time"
)

// Direction represents message direction.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// ActivityEntry tracks activity timestamps for a channel/account.
type ActivityEntry struct {
	InboundAt  *time.Time `json:"inbound_at,omitempty"`
	OutboundAt *time.Time `json:"outbound_at,omitempty"`
}

// ActivityTracker tracks channel activity across accounts.
type ActivityTracker struct {
	mu       sync.RWMutex
	activity map[string]*ActivityEntry
}

// NewActivityTracker creates a new activity tracker.
func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		activity: make(map[string]*ActivityEntry),
	}
}

// key generates a map key for channel/account.
func key(channel, accountID string) string {
	if accountID == "" {
		accountID = "default"
	}
	return channel + ":" + accountID
}

// Record records channel activity.
func (t *ActivityTracker) Record(channel, accountID string, direction Direction) {
	t.RecordAt(channel, accountID, direction, time.Now())
}

// RecordAt records channel activity at a specific time.
func (t *ActivityTracker) RecordAt(channel, accountID string, direction Direction, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := key(channel, accountID)
	entry := t.activity[k]
	if entry == nil {
		entry = &ActivityEntry{}
		t.activity[k] = entry
	}

	switch direction {
	case DirectionInbound:
		entry.InboundAt = &at
	case DirectionOutbound:
		entry.OutboundAt = &at
	}
}

// Get returns activity for a channel/account.
func (t *ActivityTracker) Get(channel, accountID string) ActivityEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	k := key(channel, accountID)
	if entry := t.activity[k]; entry != nil {
		return *entry
	}
	return ActivityEntry{}
}

// GetAll returns all activity entries.
func (t *ActivityTracker) GetAll() map[string]ActivityEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]ActivityEntry, len(t.activity))
	for k, v := range t.activity {
		if v != nil {
			result[k] = *v
		}
	}
	return result
}

// LastActivity returns the most recent activity timestamp for a channel/account.
func (t *ActivityTracker) LastActivity(channel, accountID string) *time.Time {
	entry := t.Get(channel, accountID)

	if entry.InboundAt == nil && entry.OutboundAt == nil {
		return nil
	}
	if entry.InboundAt == nil {
		return entry.OutboundAt
	}
	if entry.OutboundAt == nil {
		return entry.InboundAt
	}
	if entry.InboundAt.After(*entry.OutboundAt) {
		return entry.InboundAt
	}
	return entry.OutboundAt
}

// TimeSinceLastActivity returns the duration since last activity.
func (t *ActivityTracker) TimeSinceLastActivity(channel, accountID string) *time.Duration {
	last := t.LastActivity(channel, accountID)
	if last == nil {
		return nil
	}
	d := time.Since(*last)
	return &d
}

// IsActive returns true if activity occurred within the given duration.
func (t *ActivityTracker) IsActive(channel, accountID string, within time.Duration) bool {
	d := t.TimeSinceLastActivity(channel, accountID)
	if d == nil {
		return false
	}
	return *d <= within
}

// Clear removes all activity data.
func (t *ActivityTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.activity = make(map[string]*ActivityEntry)
}

// ClearChannel removes activity for a specific channel.
func (t *ActivityTracker) ClearChannel(channel string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for k := range t.activity {
		if len(k) > len(channel) && k[:len(channel)+1] == channel+":" {
			delete(t.activity, k)
		}
	}
}

// Stats returns activity statistics.
func (t *ActivityTracker) Stats() ActivityStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := ActivityStats{
		TotalChannels: len(t.activity),
		ByChannel:     make(map[string]int),
	}

	now := time.Now()
	for k, entry := range t.activity {
		// Extract channel from key
		for i := 0; i < len(k); i++ {
			if k[i] == ':' {
				channel := k[:i]
				stats.ByChannel[channel]++
				break
			}
		}

		if entry.InboundAt != nil {
			stats.TotalInbound++
			if now.Sub(*entry.InboundAt) < time.Hour {
				stats.RecentInbound++
			}
		}
		if entry.OutboundAt != nil {
			stats.TotalOutbound++
			if now.Sub(*entry.OutboundAt) < time.Hour {
				stats.RecentOutbound++
			}
		}
	}

	return stats
}

// ActivityStats contains activity statistics.
type ActivityStats struct {
	TotalChannels  int            `json:"total_channels"`
	TotalInbound   int            `json:"total_inbound"`
	TotalOutbound  int            `json:"total_outbound"`
	RecentInbound  int            `json:"recent_inbound"`  // Last hour
	RecentOutbound int            `json:"recent_outbound"` // Last hour
	ByChannel      map[string]int `json:"by_channel"`
}

// Global activity tracker
var globalActivity = NewActivityTracker()

// RecordActivity records activity on the global tracker.
func RecordActivity(channel, accountID string, direction Direction) {
	globalActivity.Record(channel, accountID, direction)
}

// GetActivity returns activity from the global tracker.
func GetActivity(channel, accountID string) ActivityEntry {
	return globalActivity.Get(channel, accountID)
}

// GetActivityStats returns stats from the global tracker.
func GetActivityStats() ActivityStats {
	return globalActivity.Stats()
}

// ResetActivityForTest resets the global tracker for testing.
func ResetActivityForTest() {
	globalActivity.Clear()
}
