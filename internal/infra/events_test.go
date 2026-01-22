package infra

import (
	"testing"
)

func TestSystemEventsQueue_BasicEnqueueDrain(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Event 1", "")
	q.Enqueue("session1", "Event 2", "")
	q.Enqueue("session1", "Event 3", "")

	events := q.DrainText("session1")
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	if events[0] != "Event 1" || events[1] != "Event 2" || events[2] != "Event 3" {
		t.Errorf("unexpected events: %v", events)
	}

	// Second drain should be empty
	events = q.DrainText("session1")
	if events != nil {
		t.Errorf("expected nil after drain, got %v", events)
	}
}

func TestSystemEventsQueue_ConsecutiveDuplicateSuppression(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Event A", "")
	q.Enqueue("session1", "Event A", "") // duplicate - should be suppressed
	q.Enqueue("session1", "Event A", "") // duplicate - should be suppressed
	q.Enqueue("session1", "Event B", "")
	q.Enqueue("session1", "Event A", "") // not consecutive duplicate - should be added

	events := q.DrainText("session1")
	if len(events) != 3 {
		t.Fatalf("expected 3 events (duplicates suppressed), got %d", len(events))
	}

	if events[0] != "Event A" || events[1] != "Event B" || events[2] != "Event A" {
		t.Errorf("unexpected events: %v", events)
	}
}

func TestSystemEventsQueue_MaxEventsLimit(t *testing.T) {
	q := NewSystemEventsQueue()
	q.MaxEvents = 5

	for i := 1; i <= 10; i++ {
		q.Enqueue("session1", "Event", "") // Use different text each time
		// Workaround for consecutive duplicate suppression
		if i < 10 {
			q.Enqueue("session1", "spacer", "")
		}
	}

	// Should only have the last 5 events
	if q.Count("session1") != 5 {
		t.Errorf("expected 5 events (max limit), got %d", q.Count("session1"))
	}
}

func TestSystemEventsQueue_SessionIsolation(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Session 1 Event", "")
	q.Enqueue("session2", "Session 2 Event", "")

	events1 := q.DrainText("session1")
	events2 := q.DrainText("session2")

	if len(events1) != 1 || events1[0] != "Session 1 Event" {
		t.Errorf("unexpected session1 events: %v", events1)
	}

	if len(events2) != 1 || events2[0] != "Session 2 Event" {
		t.Errorf("unexpected session2 events: %v", events2)
	}
}

func TestSystemEventsQueue_EmptyInputs(t *testing.T) {
	q := NewSystemEventsQueue()

	// Empty session key should be ignored
	q.Enqueue("", "Event", "")
	if q.SessionCount() != 0 {
		t.Error("expected no sessions for empty session key")
	}

	// Empty text should be ignored
	q.Enqueue("session1", "", "")
	if q.HasEvents("session1") {
		t.Error("expected no events for empty text")
	}

	// Whitespace-only inputs
	q.Enqueue("  ", "Event", "")
	q.Enqueue("session1", "   ", "")
	if q.SessionCount() != 0 {
		t.Error("expected no sessions for whitespace inputs")
	}
}

func TestSystemEventsQueue_ContextKeyTracking(t *testing.T) {
	q := NewSystemEventsQueue()

	// No session yet - any non-empty context is "changed"
	if !q.IsContextChanged("session1", "context-a") {
		t.Error("expected context change for new session")
	}

	// Empty context on new session is not changed
	if q.IsContextChanged("session1", "") {
		t.Error("expected no context change for empty context on new session")
	}

	// Set context
	q.Enqueue("session1", "Event", "context-a")

	// Same context
	if q.IsContextChanged("session1", "context-a") {
		t.Error("expected no context change for same context")
	}

	// Case insensitive
	if q.IsContextChanged("session1", "CONTEXT-A") {
		t.Error("expected no context change for same context (case insensitive)")
	}

	// Different context
	if !q.IsContextChanged("session1", "context-b") {
		t.Error("expected context change for different context")
	}
}

func TestSystemEventsQueue_Peek(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Event 1", "")
	q.Enqueue("session1", "Event 2", "")

	// Peek should return events without removing
	peeked := q.PeekText("session1")
	if len(peeked) != 2 {
		t.Fatalf("expected 2 events from peek, got %d", len(peeked))
	}

	// Should still have events
	if !q.HasEvents("session1") {
		t.Error("expected events to still exist after peek")
	}

	// Peek again should return same events
	peeked2 := q.PeekText("session1")
	if len(peeked2) != 2 {
		t.Error("expected same events from second peek")
	}
}

func TestSystemEventsQueue_HasEvents(t *testing.T) {
	q := NewSystemEventsQueue()

	if q.HasEvents("session1") {
		t.Error("expected no events for new session")
	}

	q.Enqueue("session1", "Event", "")

	if !q.HasEvents("session1") {
		t.Error("expected events after enqueue")
	}

	q.Drain("session1")

	if q.HasEvents("session1") {
		t.Error("expected no events after drain")
	}
}

func TestSystemEventsQueue_Count(t *testing.T) {
	q := NewSystemEventsQueue()

	if q.Count("session1") != 0 {
		t.Error("expected count 0 for new session")
	}

	q.Enqueue("session1", "Event 1", "")
	q.Enqueue("session1", "Event 2", "")

	if q.Count("session1") != 2 {
		t.Errorf("expected count 2, got %d", q.Count("session1"))
	}
}

func TestSystemEventsQueue_Clear(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Event", "")
	q.Enqueue("session2", "Event", "")

	q.Clear("session1")

	if q.HasEvents("session1") {
		t.Error("expected no events after clear")
	}

	if !q.HasEvents("session2") {
		t.Error("expected session2 to be unaffected by clear")
	}
}

func TestSystemEventsQueue_Reset(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Event", "")
	q.Enqueue("session2", "Event", "")
	q.Enqueue("session3", "Event", "")

	q.Reset()

	if q.SessionCount() != 0 {
		t.Error("expected no sessions after reset")
	}
}

func TestSystemEventsQueue_SessionCount(t *testing.T) {
	q := NewSystemEventsQueue()

	if q.SessionCount() != 0 {
		t.Error("expected 0 sessions initially")
	}

	q.Enqueue("session1", "Event", "")
	q.Enqueue("session2", "Event", "")
	q.Enqueue("session3", "Event", "")

	if q.SessionCount() != 3 {
		t.Errorf("expected 3 sessions, got %d", q.SessionCount())
	}
}

func TestSystemEventsQueue_DrainWithTimestamps(t *testing.T) {
	q := NewSystemEventsQueue()

	q.Enqueue("session1", "Event 1", "")
	q.Enqueue("session1", "Event 2", "")

	events := q.Drain("session1")
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	for i, e := range events {
		if e.Timestamp.IsZero() {
			t.Errorf("event %d has zero timestamp", i)
		}
		if e.Text == "" {
			t.Errorf("event %d has empty text", i)
		}
	}
}

func TestDefaultSystemEvents(t *testing.T) {
	// Reset default queue
	DefaultSystemEvents.Reset()

	EnqueueSystemEvent("test-session", "Test event", "")

	if !HasSystemEvents("test-session") {
		t.Error("expected default queue to have events")
	}

	events := DrainSystemEvents("test-session")
	if len(events) != 1 || events[0] != "Test event" {
		t.Errorf("unexpected events from default queue: %v", events)
	}
}
