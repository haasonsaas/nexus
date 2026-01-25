package backoff

import (
	"context"
	"time"
)

// SleepWithContext sleeps for the specified duration, respecting context cancellation.
// Returns nil if the sleep completed, or ctx.Err() if the context was cancelled.
func SleepWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SleepWithBackoff calculates the backoff duration for the given attempt and sleeps.
// It combines ComputeBackoff and SleepWithContext for convenience.
// Returns nil if the sleep completed, or ctx.Err() if the context was cancelled.
func SleepWithBackoff(ctx context.Context, policy BackoffPolicy, attempt int) error {
	duration := ComputeBackoff(policy, attempt)
	return SleepWithContext(ctx, duration)
}
