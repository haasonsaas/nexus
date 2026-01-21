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
