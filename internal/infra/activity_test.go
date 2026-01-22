package infra

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestActivityTracker_Record(t *testing.T) {
	tracker := NewActivityTracker()

	tracker.Record("telegram", "account1", ActivityInbound)
	tracker.Record("telegram", "account1", ActivityOutbound)

	entry := tracker.Get("telegram", "account1")

	if entry.InboundAt == nil {
		t.Error("expected InboundAt to be set")
	}
	if entry.OutboundAt == nil {
		t.Error("expected OutboundAt to be set")
	}
	if entry.InboundCount != 1 {
		t.Errorf("expected InboundCount 1, got %d", entry.InboundCount)
	}
	if entry.OutboundCount != 1 {
		t.Errorf("expected OutboundCount 1, got %d", entry.OutboundCount)
	}
}

func TestActivityTracker_DefaultAccountID(t *testing.T) {
	tracker := NewActivityTracker()

	// Empty account ID should default to "default"
	tracker.Record("discord", "", ActivityInbound)

	entry1 := tracker.Get("discord", "")
	entry2 := tracker.Get("discord", "default")

	if entry1.InboundCount != entry2.InboundCount {
		t.Error("empty and 'default' account IDs should be equivalent")
	}
}

func TestActivityTracker_RecordAt(t *testing.T) {
	tracker := NewActivityTracker()

	past := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	tracker.RecordAt("slack", "account1", ActivityInbound, past)

	entry := tracker.Get("slack", "account1")

	if entry.InboundAt == nil {
		t.Fatal("expected InboundAt to be set")
	}
	if !entry.InboundAt.Equal(past) {
		t.Errorf("expected InboundAt %v, got %v", past, entry.InboundAt)
	}
}

func TestActivityTracker_GetNonexistent(t *testing.T) {
	tracker := NewActivityTracker()

	entry := tracker.Get("nonexistent", "account1")

	if entry.InboundAt != nil || entry.OutboundAt != nil {
		t.Error("expected nil timestamps for nonexistent entry")
	}
	if entry.InboundCount != 0 || entry.OutboundCount != 0 {
		t.Error("expected zero counts for nonexistent entry")
	}
}

func TestActivityTracker_GetAll(t *testing.T) {
	tracker := NewActivityTracker()

	tracker.Record("telegram", "a1", ActivityInbound)
	tracker.Record("discord", "a2", ActivityOutbound)
	tracker.Record("slack", "a3", ActivityInbound)

	all := tracker.GetAll()

	if len(all) != 3 {
		t.Errorf("expected 3 entries, got %d", len(all))
	}
}

func TestActivityTracker_LastActivity(t *testing.T) {
	tracker := NewActivityTracker()

	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)

	tracker.RecordAt("a", "", ActivityInbound, t1)
	tracker.RecordAt("b", "", ActivityOutbound, t3)
	tracker.RecordAt("c", "", ActivityInbound, t2)

	last := tracker.LastActivity()

	if last == nil {
		t.Fatal("expected last activity to be set")
	}
	if !last.Equal(t3) {
		t.Errorf("expected last activity %v, got %v", t3, last)
	}
}

func TestActivityTracker_LastActivity_Empty(t *testing.T) {
	tracker := NewActivityTracker()

	last := tracker.LastActivity()

	if last != nil {
		t.Error("expected nil for empty tracker")
	}
}

func TestActivityTracker_IsIdle(t *testing.T) {
	tracker := NewActivityTracker()

	// Empty tracker is idle
	if !tracker.IsIdle(time.Second) {
		t.Error("empty tracker should be idle")
	}

	// Record recent activity
	tracker.Record("test", "", ActivityInbound)

	// Should not be idle immediately
	if tracker.IsIdle(time.Second) {
		t.Error("tracker should not be idle immediately after activity")
	}
}

func TestActivityTracker_Reset(t *testing.T) {
	tracker := NewActivityTracker()

	tracker.Record("a", "", ActivityInbound)
	tracker.Record("b", "", ActivityOutbound)

	all := tracker.GetAll()
	if len(all) == 0 {
		t.Fatal("expected entries before reset")
	}

	tracker.Reset()

	all = tracker.GetAll()
	if len(all) != 0 {
		t.Errorf("expected empty after reset, got %d", len(all))
	}
}

func TestActivityTracker_OnChange(t *testing.T) {
	tracker := NewActivityTracker()

	var callCount atomic.Int32
	var lastChannel, lastAccount string
	var lastDirection ActivityDirection
	var mu sync.Mutex

	tracker.OnChange(func(channel, accountID string, direction ActivityDirection) {
		callCount.Add(1)
		mu.Lock()
		lastChannel = channel
		lastAccount = accountID
		lastDirection = direction
		mu.Unlock()
	})

	tracker.Record("telegram", "acc1", ActivityInbound)

	if callCount.Load() != 1 {
		t.Errorf("expected callback called once, got %d", callCount.Load())
	}

	mu.Lock()
	if lastChannel != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", lastChannel)
	}
	if lastAccount != "acc1" {
		t.Errorf("expected account 'acc1', got %q", lastAccount)
	}
	if lastDirection != ActivityInbound {
		t.Errorf("expected direction inbound, got %v", lastDirection)
	}
	mu.Unlock()
}

func TestActivityTracker_Health(t *testing.T) {
	tracker := NewActivityTracker()

	now := time.Now()
	recent := now.Add(-30 * time.Second)
	old := now.Add(-2 * time.Hour)

	tracker.RecordAt("active1", "", ActivityInbound, recent)
	tracker.RecordAt("active2", "", ActivityOutbound, now)
	tracker.RecordAt("idle", "", ActivityInbound, old)

	health := tracker.Health(time.Minute)

	if health.TotalChannels != 3 {
		t.Errorf("expected 3 total channels, got %d", health.TotalChannels)
	}
	if health.ActiveChannels != 2 {
		t.Errorf("expected 2 active channels, got %d", health.ActiveChannels)
	}
	if health.IdleChannels != 1 {
		t.Errorf("expected 1 idle channel, got %d", health.IdleChannels)
	}
	if health.LastActivityAt == nil {
		t.Error("expected LastActivityAt to be set")
	}
}

func TestActivityTracker_Concurrent(t *testing.T) {
	tracker := NewActivityTracker()

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if j%2 == 0 {
					tracker.Record("test", "", ActivityInbound)
				} else {
					tracker.Record("test", "", ActivityOutbound)
				}
			}
		}(i)
	}

	wg.Wait()

	entry := tracker.Get("test", "")
	total := entry.InboundCount + entry.OutboundCount

	expected := int64(goroutines * 100)
	if total != expected {
		t.Errorf("expected %d total activities, got %d", expected, total)
	}
}

func TestGlobalActivityFunctions(t *testing.T) {
	// Reset the default tracker first
	defaultActivityTracker.Reset()

	RecordActivity("global-test", "acc1", ActivityInbound)

	entry := GetActivity("global-test", "acc1")
	if entry.InboundCount != 1 {
		t.Errorf("expected 1 inbound, got %d", entry.InboundCount)
	}

	health := GetActivityHealth(time.Minute)
	if health.TotalChannels != 1 {
		t.Errorf("expected 1 channel, got %d", health.TotalChannels)
	}
}

func TestActivityTracker_MultipleCounts(t *testing.T) {
	tracker := NewActivityTracker()

	for i := 0; i < 10; i++ {
		tracker.Record("test", "", ActivityInbound)
	}
	for i := 0; i < 5; i++ {
		tracker.Record("test", "", ActivityOutbound)
	}

	entry := tracker.Get("test", "")

	if entry.InboundCount != 10 {
		t.Errorf("expected 10 inbound, got %d", entry.InboundCount)
	}
	if entry.OutboundCount != 5 {
		t.Errorf("expected 5 outbound, got %d", entry.OutboundCount)
	}
}
