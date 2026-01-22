package channels

import (
	"testing"
	"time"
)

func TestLatencyHistogramRingBuffer(t *testing.T) {
	hist := NewLatencyHistogram()

	for i := 1; i <= 1005; i++ {
		hist.Record(time.Duration(i))
	}

	snap := hist.Snapshot()
	if snap.Count != 1000 {
		t.Fatalf("expected count 1000, got %d", snap.Count)
	}
	if snap.Min != 6 {
		t.Fatalf("expected min 6, got %v", snap.Min)
	}
	if snap.Max != 1005 {
		t.Fatalf("expected max 1005, got %v", snap.Max)
	}
	if snap.P50 != 506 {
		t.Fatalf("expected p50 506, got %v", snap.P50)
	}
	if snap.P95 != 956 {
		t.Fatalf("expected p95 956, got %v", snap.P95)
	}
	if snap.P99 != 996 {
		t.Fatalf("expected p99 996, got %v", snap.P99)
	}
}

func TestLatencyHistogramEmpty(t *testing.T) {
	hist := NewLatencyHistogram()
	snap := hist.Snapshot()
	if snap.Count != 0 {
		t.Fatalf("expected count 0, got %d", snap.Count)
	}
	if snap.Min != 0 || snap.Max != 0 || snap.Mean != 0 {
		t.Fatalf("expected zeroed snapshot, got %+v", snap)
	}
}

func TestMetrics_ActionTracking(t *testing.T) {
	m := NewMetrics("test")

	// Record some successful actions
	m.RecordActionExecuted(ActionSend, 100*time.Millisecond)
	m.RecordActionExecuted(ActionSend, 150*time.Millisecond)
	m.RecordActionExecuted(ActionEdit, 50*time.Millisecond)

	// Record some failed actions
	m.RecordActionFailed(ActionDelete)
	m.RecordActionFailed(ActionDelete)

	// Get snapshot
	snap := m.Snapshot()

	// Verify action counts
	if snap.ActionsByType[ActionSend] != 2 {
		t.Errorf("expected 2 send actions, got %d", snap.ActionsByType[ActionSend])
	}
	if snap.ActionsByType[ActionEdit] != 1 {
		t.Errorf("expected 1 edit action, got %d", snap.ActionsByType[ActionEdit])
	}

	// Verify failed action counts
	if snap.ActionsFailed[ActionDelete] != 2 {
		t.Errorf("expected 2 failed delete actions, got %d", snap.ActionsFailed[ActionDelete])
	}

	// Verify latency was recorded for send actions
	sendLatency := snap.ActionsLatency[ActionSend]
	if sendLatency.Count != 2 {
		t.Errorf("expected 2 latency samples for send, got %d", sendLatency.Count)
	}
	if sendLatency.Min != 100*time.Millisecond {
		t.Errorf("expected min latency 100ms, got %v", sendLatency.Min)
	}
	if sendLatency.Max != 150*time.Millisecond {
		t.Errorf("expected max latency 150ms, got %v", sendLatency.Max)
	}
}

func TestMetrics_ActionTrackingConcurrency(t *testing.T) {
	m := NewMetrics("test")
	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				m.RecordActionExecuted(ActionSend, time.Millisecond)
				m.RecordActionFailed(ActionEdit)
			}
			done <- true
		}()
	}

	// Wait for all writers
	for i := 0; i < 10; i++ {
		<-done
	}

	snap := m.Snapshot()
	if snap.ActionsByType[ActionSend] != 1000 {
		t.Errorf("expected 1000 send actions, got %d", snap.ActionsByType[ActionSend])
	}
	if snap.ActionsFailed[ActionEdit] != 1000 {
		t.Errorf("expected 1000 failed edit actions, got %d", snap.ActionsFailed[ActionEdit])
	}
}
