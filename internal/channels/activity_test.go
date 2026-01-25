package channels

import (
	"sync"
	"testing"
	"time"
)

func TestActivityTracker_RecordInbound(t *testing.T) {
	tracker := NewActivityTracker()

	now := time.Now()
	tracker.RecordAt("telegram", "user123", DirectionInbound, now)

	entry := tracker.Get("telegram", "user123")
	if entry.InboundAt == nil {
		t.Fatal("expected InboundAt to be set")
	}
	if !entry.InboundAt.Equal(now) {
		t.Errorf("InboundAt = %v, want %v", entry.InboundAt, now)
	}
	if entry.OutboundAt != nil {
		t.Error("expected OutboundAt to be nil")
	}
}

func TestActivityTracker_RecordOutbound(t *testing.T) {
	tracker := NewActivityTracker()

	now := time.Now()
	tracker.RecordAt("discord", "user456", DirectionOutbound, now)

	entry := tracker.Get("discord", "user456")
	if entry.OutboundAt == nil {
		t.Fatal("expected OutboundAt to be set")
	}
	if !entry.OutboundAt.Equal(now) {
		t.Errorf("OutboundAt = %v, want %v", entry.OutboundAt, now)
	}
	if entry.InboundAt != nil {
		t.Error("expected InboundAt to be nil")
	}
}

func TestActivityTracker_RecordBothDirections(t *testing.T) {
	tracker := NewActivityTracker()

	inboundTime := time.Now().Add(-time.Minute)
	outboundTime := time.Now()

	tracker.RecordAt("slack", "user789", DirectionInbound, inboundTime)
	tracker.RecordAt("slack", "user789", DirectionOutbound, outboundTime)

	entry := tracker.Get("slack", "user789")
	if entry.InboundAt == nil || entry.OutboundAt == nil {
		t.Fatal("expected both timestamps to be set")
	}
	if !entry.InboundAt.Equal(inboundTime) {
		t.Errorf("InboundAt = %v, want %v", entry.InboundAt, inboundTime)
	}
	if !entry.OutboundAt.Equal(outboundTime) {
		t.Errorf("OutboundAt = %v, want %v", entry.OutboundAt, outboundTime)
	}
}

func TestActivityTracker_DifferentChannelsIsolated(t *testing.T) {
	tracker := NewActivityTracker()

	telegramTime := time.Now().Add(-time.Hour)
	discordTime := time.Now()

	tracker.RecordAt("telegram", "user1", DirectionInbound, telegramTime)
	tracker.RecordAt("discord", "user1", DirectionInbound, discordTime)

	telegramEntry := tracker.Get("telegram", "user1")
	discordEntry := tracker.Get("discord", "user1")

	if telegramEntry.InboundAt == nil || !telegramEntry.InboundAt.Equal(telegramTime) {
		t.Errorf("telegram entry incorrect: got %v, want %v", telegramEntry.InboundAt, telegramTime)
	}
	if discordEntry.InboundAt == nil || !discordEntry.InboundAt.Equal(discordTime) {
		t.Errorf("discord entry incorrect: got %v, want %v", discordEntry.InboundAt, discordTime)
	}
}

func TestActivityTracker_DifferentAccountsIsolated(t *testing.T) {
	tracker := NewActivityTracker()

	user1Time := time.Now().Add(-time.Hour)
	user2Time := time.Now()

	tracker.RecordAt("telegram", "user1", DirectionInbound, user1Time)
	tracker.RecordAt("telegram", "user2", DirectionInbound, user2Time)

	user1Entry := tracker.Get("telegram", "user1")
	user2Entry := tracker.Get("telegram", "user2")

	if user1Entry.InboundAt == nil || !user1Entry.InboundAt.Equal(user1Time) {
		t.Errorf("user1 entry incorrect: got %v, want %v", user1Entry.InboundAt, user1Time)
	}
	if user2Entry.InboundAt == nil || !user2Entry.InboundAt.Equal(user2Time) {
		t.Errorf("user2 entry incorrect: got %v, want %v", user2Entry.InboundAt, user2Time)
	}
}

func TestActivityTracker_GetUnknownChannelAccount(t *testing.T) {
	tracker := NewActivityTracker()

	entry := tracker.Get("unknown", "unknown")

	if entry.InboundAt != nil {
		t.Error("expected InboundAt to be nil for unknown channel/account")
	}
	if entry.OutboundAt != nil {
		t.Error("expected OutboundAt to be nil for unknown channel/account")
	}
}

func TestActivityTracker_Reset(t *testing.T) {
	tracker := NewActivityTracker()

	tracker.RecordAt("telegram", "user1", DirectionInbound, time.Now())
	tracker.RecordAt("discord", "user2", DirectionOutbound, time.Now())

	// Verify data exists
	all := tracker.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries before clear, got %d", len(all))
	}

	// Clear all data
	tracker.Clear()

	// Verify data is gone
	all = tracker.GetAll()
	if len(all) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(all))
	}

	// Verify individual lookups return empty
	entry := tracker.Get("telegram", "user1")
	if entry.InboundAt != nil || entry.OutboundAt != nil {
		t.Error("expected empty entry after clear")
	}
}

func TestActivityTracker_ClearChannel(t *testing.T) {
	tracker := NewActivityTracker()

	tracker.RecordAt("telegram", "user1", DirectionInbound, time.Now())
	tracker.RecordAt("telegram", "user2", DirectionInbound, time.Now())
	tracker.RecordAt("discord", "user1", DirectionInbound, time.Now())

	// Clear only telegram
	tracker.ClearChannel("telegram")

	// Discord should still exist
	discordEntry := tracker.Get("discord", "user1")
	if discordEntry.InboundAt == nil {
		t.Error("expected discord entry to remain after clearing telegram")
	}

	// Telegram entries should be gone
	telegramEntry1 := tracker.Get("telegram", "user1")
	telegramEntry2 := tracker.Get("telegram", "user2")
	if telegramEntry1.InboundAt != nil || telegramEntry2.InboundAt != nil {
		t.Error("expected telegram entries to be cleared")
	}
}

func TestActivityTracker_LastActivity(t *testing.T) {
	tracker := NewActivityTracker()

	// Test with no activity
	last := tracker.LastActivity("telegram", "user1")
	if last != nil {
		t.Error("expected nil for unknown channel/account")
	}

	// Test with only inbound
	inboundTime := time.Now().Add(-time.Minute)
	tracker.RecordAt("telegram", "user1", DirectionInbound, inboundTime)
	last = tracker.LastActivity("telegram", "user1")
	if last == nil || !last.Equal(inboundTime) {
		t.Errorf("expected %v, got %v", inboundTime, last)
	}

	// Test with outbound after inbound
	outboundTime := time.Now()
	tracker.RecordAt("telegram", "user1", DirectionOutbound, outboundTime)
	last = tracker.LastActivity("telegram", "user1")
	if last == nil || !last.Equal(outboundTime) {
		t.Errorf("expected %v (outbound is later), got %v", outboundTime, last)
	}

	// Test with inbound after outbound (new entry)
	laterInbound := time.Now().Add(time.Minute)
	tracker.RecordAt("telegram", "user1", DirectionInbound, laterInbound)
	last = tracker.LastActivity("telegram", "user1")
	if last == nil || !last.Equal(laterInbound) {
		t.Errorf("expected %v (later inbound), got %v", laterInbound, last)
	}
}

func TestActivityTracker_GetAll(t *testing.T) {
	tracker := NewActivityTracker()

	t1 := time.Now()
	t2 := time.Now().Add(time.Second)

	tracker.RecordAt("telegram", "user1", DirectionInbound, t1)
	tracker.RecordAt("discord", "user2", DirectionOutbound, t2)

	all := tracker.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	// Check entries exist (map iteration order is not guaranteed)
	foundTelegram := false
	foundDiscord := false
	for key, entry := range all {
		switch key {
		case "telegram:user1":
			foundTelegram = true
			if entry.InboundAt == nil || !entry.InboundAt.Equal(t1) {
				t.Errorf("telegram entry incorrect")
			}
		case "discord:user2":
			foundDiscord = true
			if entry.OutboundAt == nil || !entry.OutboundAt.Equal(t2) {
				t.Errorf("discord entry incorrect")
			}
		}
	}
	if !foundTelegram {
		t.Error("telegram entry not found")
	}
	if !foundDiscord {
		t.Error("discord entry not found")
	}
}

func TestActivityTracker_GetInactiveChannels(t *testing.T) {
	tracker := NewActivityTracker()

	now := time.Now()
	oldTime := now.Add(-24 * time.Hour)
	recentTime := now.Add(-time.Minute)

	// Old activity - should be inactive
	tracker.RecordAt("telegram", "old_user", DirectionInbound, oldTime)

	// Recent activity - should be active
	tracker.RecordAt("discord", "active_user", DirectionInbound, recentTime)

	// Activity with old inbound but recent outbound - should be active
	tracker.RecordAt("slack", "mixed_user", DirectionInbound, oldTime)
	tracker.RecordAt("slack", "mixed_user", DirectionOutbound, recentTime)

	// Check inactive since 1 hour ago
	since := now.Add(-time.Hour)
	inactive := tracker.GetInactiveChannels(since)

	if len(inactive) != 1 {
		t.Fatalf("expected 1 inactive entry, got %d", len(inactive))
	}

	if inactive[0].Channel != "telegram" || inactive[0].AccountID != "old_user" {
		t.Errorf("expected telegram:old_user, got %s:%s", inactive[0].Channel, inactive[0].AccountID)
	}
	if inactive[0].LastActivity == nil || !inactive[0].LastActivity.Equal(oldTime) {
		t.Errorf("expected LastActivity = %v, got %v", oldTime, inactive[0].LastActivity)
	}
}

func TestActivityTracker_GetInactiveChannels_Empty(t *testing.T) {
	tracker := NewActivityTracker()

	since := time.Now()
	inactive := tracker.GetInactiveChannels(since)

	if len(inactive) != 0 {
		t.Errorf("expected 0 inactive entries for empty tracker, got %d", len(inactive))
	}
}

func TestActivityTracker_DefaultAccountID(t *testing.T) {
	tracker := NewActivityTracker()

	now := time.Now()

	// Empty account ID should use "default"
	tracker.RecordAt("telegram", "", DirectionInbound, now)

	// Both ways of accessing should work
	entry1 := tracker.Get("telegram", "")
	entry2 := tracker.Get("telegram", "default")

	if entry1.InboundAt == nil {
		t.Error("expected InboundAt to be set with empty account")
	}
	if entry2.InboundAt == nil {
		t.Error("expected InboundAt to be set with 'default' account")
	}
}

func TestActivityTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewActivityTracker()
	var wg sync.WaitGroup

	// Run concurrent operations
	for i := 0; i < 100; i++ {
		wg.Add(4)

		// Concurrent writes
		go func(n int) {
			defer wg.Done()
			tracker.RecordAt("telegram", "user", DirectionInbound, time.Now())
		}(i)

		go func(n int) {
			defer wg.Done()
			tracker.RecordAt("discord", "user", DirectionOutbound, time.Now())
		}(i)

		// Concurrent reads
		go func(n int) {
			defer wg.Done()
			_ = tracker.Get("telegram", "user")
		}(i)

		go func(n int) {
			defer wg.Done()
			_ = tracker.GetAll()
		}(i)
	}

	wg.Wait()

	// If we get here without deadlock or panic, the test passes
	entry := tracker.Get("telegram", "user")
	if entry.InboundAt == nil {
		t.Error("expected InboundAt to be set after concurrent writes")
	}
}

func TestActivityTracker_TimeSinceLastActivity(t *testing.T) {
	tracker := NewActivityTracker()

	// Test unknown channel returns nil
	d := tracker.TimeSinceLastActivity("unknown", "unknown")
	if d != nil {
		t.Error("expected nil for unknown channel/account")
	}

	// Test with known activity
	past := time.Now().Add(-time.Hour)
	tracker.RecordAt("telegram", "user", DirectionInbound, past)

	d = tracker.TimeSinceLastActivity("telegram", "user")
	if d == nil {
		t.Fatal("expected non-nil duration")
	}

	// Should be approximately 1 hour (allow some tolerance)
	if *d < 59*time.Minute || *d > 61*time.Minute {
		t.Errorf("expected ~1 hour, got %v", *d)
	}
}

func TestActivityTracker_IsActive(t *testing.T) {
	tracker := NewActivityTracker()

	// Unknown channel is not active
	if tracker.IsActive("unknown", "unknown", time.Hour) {
		t.Error("expected unknown channel to not be active")
	}

	// Recent activity is active
	tracker.RecordAt("telegram", "user", DirectionInbound, time.Now())
	if !tracker.IsActive("telegram", "user", time.Hour) {
		t.Error("expected recent activity to be active")
	}

	// Old activity is not active
	tracker.RecordAt("discord", "user", DirectionInbound, time.Now().Add(-2*time.Hour))
	if tracker.IsActive("discord", "user", time.Hour) {
		t.Error("expected old activity to not be active")
	}
}

func TestActivityTracker_Stats(t *testing.T) {
	tracker := NewActivityTracker()

	now := time.Now()
	recentTime := now.Add(-30 * time.Minute)
	oldTime := now.Add(-2 * time.Hour)

	// Add some activity
	tracker.RecordAt("telegram", "user1", DirectionInbound, recentTime)
	tracker.RecordAt("telegram", "user2", DirectionOutbound, recentTime)
	tracker.RecordAt("discord", "user1", DirectionInbound, oldTime)

	stats := tracker.Stats()

	if stats.TotalChannels != 3 {
		t.Errorf("TotalChannels = %d, want 3", stats.TotalChannels)
	}
	if stats.TotalInbound != 2 {
		t.Errorf("TotalInbound = %d, want 2", stats.TotalInbound)
	}
	if stats.TotalOutbound != 1 {
		t.Errorf("TotalOutbound = %d, want 1", stats.TotalOutbound)
	}
	if stats.RecentInbound != 1 {
		t.Errorf("RecentInbound = %d, want 1 (only the one within last hour)", stats.RecentInbound)
	}
	if stats.RecentOutbound != 1 {
		t.Errorf("RecentOutbound = %d, want 1", stats.RecentOutbound)
	}
	if stats.ByChannel["telegram"] != 2 {
		t.Errorf("ByChannel[telegram] = %d, want 2", stats.ByChannel["telegram"])
	}
	if stats.ByChannel["discord"] != 1 {
		t.Errorf("ByChannel[discord] = %d, want 1", stats.ByChannel["discord"])
	}
}

func TestActivityTracker_Record_UsesCurrentTime(t *testing.T) {
	tracker := NewActivityTracker()

	before := time.Now()
	tracker.Record("telegram", "user", DirectionInbound)
	after := time.Now()

	entry := tracker.Get("telegram", "user")
	if entry.InboundAt == nil {
		t.Fatal("expected InboundAt to be set")
	}

	if entry.InboundAt.Before(before) || entry.InboundAt.After(after) {
		t.Errorf("Record() should use current time, got %v (before=%v, after=%v)",
			entry.InboundAt, before, after)
	}
}

func TestGlobalActivityFunctions(t *testing.T) {
	// Reset global state first
	ResetActivityForTest()

	// Test RecordActivity
	RecordActivity("telegram", "user", DirectionInbound)

	// Test GetActivity
	entry := GetActivity("telegram", "user")
	if entry.InboundAt == nil {
		t.Error("expected InboundAt to be set via global function")
	}

	// Test GetActivityStats
	stats := GetActivityStats()
	if stats.TotalChannels != 1 {
		t.Errorf("expected 1 channel, got %d", stats.TotalChannels)
	}

	// Test ResetActivityForTest
	ResetActivityForTest()
	entry = GetActivity("telegram", "user")
	if entry.InboundAt != nil {
		t.Error("expected nil after reset")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		key       string
		channel   string
		accountID string
	}{
		{"telegram:user123", "telegram", "user123"},
		{"discord:default", "discord", "default"},
		{"slack:user:with:colons", "slack", "user:with:colons"},
		{"nocolon", "nocolon", ""},
		{":", "", ""},
	}

	for _, tt := range tests {
		channel, accountID := parseKey(tt.key)
		if channel != tt.channel || accountID != tt.accountID {
			t.Errorf("parseKey(%q) = (%q, %q), want (%q, %q)",
				tt.key, channel, accountID, tt.channel, tt.accountID)
		}
	}
}
