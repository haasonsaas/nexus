package backoff

import (
	"context"
	"errors"
)

// ErrMaxAttemptsExhausted is returned when all retry attempts have been exhausted.
var ErrMaxAttemptsExhausted = errors.New("max retry attempts exhausted")

// RetryResult holds the result of a retry operation.
type RetryResult[T any] struct {
	// Value is the successful result value.
	Value T
	// Attempts is the number of attempts made (1-indexed).
	Attempts int
	// LastError is the last error encountered, if any.
	LastError error
}

// RetryWithBackoff executes the provided function with exponential backoff retry logic.
// It will retry up to maxAttempts times, sleeping between attempts according to the policy.
// Returns the result on success, or an error after all attempts are exhausted or context is cancelled.
//
// The fn function receives the current attempt number (1-indexed) and should return:
//   - (value, nil) on success
//   - (zero, error) on failure (will trigger retry if attempts remain)
//
// Context cancellation is checked between attempts, allowing graceful shutdown.
func RetryWithBackoff[T any](
	ctx context.Context,
	policy BackoffPolicy,
	maxAttempts int,
	fn func(attempt int) (T, error),
) (RetryResult[T], error) {
	var result RetryResult[T]
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.Attempts = attempt

		// Check context before each attempt
		if err := ctx.Err(); err != nil {
			result.LastError = lastErr
			return result, err
		}

		// Execute the function
		value, err := fn(attempt)
		if err == nil {
			result.Value = value
			return result, nil
		}

		lastErr = err
		result.LastError = err

		// Don't sleep after the last attempt
		if attempt < maxAttempts {
			if err := SleepWithBackoff(ctx, policy, attempt); err != nil {
				return result, err
			}
		}
	}

	return result, ErrMaxAttemptsExhausted
}

// RetryFunc is a convenience wrapper for RetryWithBackoff that uses the default policy.
// It executes the provided function with exponential backoff retry logic.
func RetryFunc[T any](
	ctx context.Context,
	maxAttempts int,
	fn func(attempt int) (T, error),
) (T, error) {
	result, err := RetryWithBackoff(ctx, DefaultPolicy(), maxAttempts, fn)
	return result.Value, err
}

// RetrySimple is a convenience wrapper for simple retry cases without return values.
// It uses the default policy and retries the function up to maxAttempts times.
func RetrySimple(
	ctx context.Context,
	maxAttempts int,
	fn func() error,
) error {
	_, err := RetryWithBackoff(ctx, DefaultPolicy(), maxAttempts, func(_ int) (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}
