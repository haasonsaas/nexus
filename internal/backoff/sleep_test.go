package backoff

import (
	"context"
	"testing"
	"time"
)

func TestSleepWithContext_Completes(t *testing.T) {
	ctx := context.Background()
	start := time.Now()

	err := SleepWithContext(ctx, 50*time.Millisecond)

	elapsed := time.Since(start)
	if err != nil {
		t.Errorf("SleepWithContext() error = %v, want nil", err)
	}
	if elapsed < 45*time.Millisecond {
		t.Errorf("SleepWithContext() completed too quickly: %v", elapsed)
	}
}

func TestSleepWithContext_ZeroDuration(t *testing.T) {
	ctx := context.Background()
	start := time.Now()

	err := SleepWithContext(ctx, 0)

	elapsed := time.Since(start)
	if err != nil {
		t.Errorf("SleepWithContext() error = %v, want nil", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("SleepWithContext() with zero duration took too long: %v", elapsed)
	}
}

func TestSleepWithContext_NegativeDuration(t *testing.T) {
	ctx := context.Background()
	start := time.Now()

	err := SleepWithContext(ctx, -100*time.Millisecond)

	elapsed := time.Since(start)
	if err != nil {
		t.Errorf("SleepWithContext() error = %v, want nil", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("SleepWithContext() with negative duration took too long: %v", elapsed)
	}
}

func TestSleepWithContext_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()

	// Cancel after 20ms
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := SleepWithContext(ctx, 500*time.Millisecond)

	elapsed := time.Since(start)
	if err != context.Canceled {
		t.Errorf("SleepWithContext() error = %v, want context.Canceled", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("SleepWithContext() did not cancel quickly: %v", elapsed)
	}
}

func TestSleepWithContext_AlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	start := time.Now()
	err := SleepWithContext(ctx, 500*time.Millisecond)
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Errorf("SleepWithContext() error = %v, want context.Canceled", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("SleepWithContext() with cancelled context took too long: %v", elapsed)
	}
}

func TestSleepWithContext_DeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := SleepWithContext(ctx, 500*time.Millisecond)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("SleepWithContext() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("SleepWithContext() did not respect deadline: %v", elapsed)
	}
}

func TestSleepWithBackoff(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 10,
		MaxMs:     1000,
		Factor:    2,
		Jitter:    0,
	}

	start := time.Now()
	err := SleepWithBackoff(ctx, policy, 1)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("SleepWithBackoff() error = %v, want nil", err)
	}
	// Should sleep approximately 10ms (no jitter)
	if elapsed < 8*time.Millisecond || elapsed > 50*time.Millisecond {
		t.Errorf("SleepWithBackoff() elapsed = %v, want ~10ms", elapsed)
	}
}

func TestSleepWithBackoff_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	policy := BackoffPolicy{
		InitialMs: 500,
		MaxMs:     1000,
		Factor:    2,
		Jitter:    0,
	}

	// Cancel after 20ms
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := SleepWithBackoff(ctx, policy, 1)
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Errorf("SleepWithBackoff() error = %v, want context.Canceled", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("SleepWithBackoff() did not cancel quickly: %v", elapsed)
	}
}

func TestSleepWithBackoff_IncreasesWithAttempt(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 5,
		MaxMs:     1000,
		Factor:    2,
		Jitter:    0,
	}

	// Measure sleep durations for first 3 attempts
	var durations []time.Duration
	for attempt := 1; attempt <= 3; attempt++ {
		start := time.Now()
		_ = SleepWithBackoff(ctx, policy, attempt)
		durations = append(durations, time.Since(start))
	}

	// Each duration should be approximately double the previous
	// Allow for timer imprecision
	for i := 1; i < len(durations); i++ {
		ratio := float64(durations[i]) / float64(durations[i-1])
		// Expect ratio to be roughly 2 (allowing for timer imprecision)
		if ratio < 1.2 || ratio > 3.5 {
			t.Errorf("Duration ratio %d/%d = %v, expected ~2", i+1, i, ratio)
		}
	}
}
