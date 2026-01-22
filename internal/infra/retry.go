package infra

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

// BackoffStrategy defines how retry delays are calculated.
type BackoffStrategy string

const (
	// BackoffConstant uses a fixed delay between retries.
	BackoffConstant BackoffStrategy = "constant"

	// BackoffLinear increases delay linearly (delay * attempt).
	BackoffLinear BackoffStrategy = "linear"

	// BackoffExponential doubles the delay after each retry.
	BackoffExponential BackoffStrategy = "exponential"
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts (0 = no retries, just initial attempt).
	MaxAttempts int

	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration

	// MaxDelay caps the delay between retries.
	MaxDelay time.Duration

	// Strategy determines how delays increase between retries.
	Strategy BackoffStrategy

	// JitterFraction adds randomness to delays (0.0-1.0). 0.1 means ±10% jitter.
	JitterFraction float64

	// RetryIf is called to determine if an error should be retried.
	// If nil, all errors are retried.
	RetryIf func(error) bool
}

// DefaultRetryConfig returns sensible defaults for retry configuration.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:    3,
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       10 * time.Second,
		Strategy:       BackoffExponential,
		JitterFraction: 0.1,
	}
}

// RetryResult contains information about a retry operation.
type RetryResult struct {
	// Attempts is the total number of attempts made.
	Attempts int

	// TotalDuration is the total time spent including delays.
	TotalDuration time.Duration

	// LastError is the last error encountered (nil on success).
	LastError error
}

// Retry executes fn with retries according to cfg.
// Returns the result of fn or the last error after all retries are exhausted.
func Retry[T any](ctx context.Context, cfg *RetryConfig, fn func(ctx context.Context) (T, error)) (T, *RetryResult) {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}

	var zero T
	result := &RetryResult{}
	start := time.Now()

	for attempt := 0; attempt <= cfg.MaxAttempts; attempt++ {
		result.Attempts = attempt + 1

		// Check context before each attempt
		if ctx.Err() != nil {
			result.LastError = ctx.Err()
			result.TotalDuration = time.Since(start)
			return zero, result
		}

		// Execute the function
		val, err := fn(ctx)
		if err == nil {
			result.LastError = nil // Clear any previous error on success
			result.TotalDuration = time.Since(start)
			return val, result
		}

		result.LastError = err

		// Check if we should retry
		if !shouldRetry(cfg, err) {
			result.TotalDuration = time.Since(start)
			return zero, result
		}

		// Check if this was the last attempt
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Calculate delay
		delay := calculateDelay(cfg, attempt)

		// Wait with context cancellation support
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			result.LastError = ctx.Err()
			result.TotalDuration = time.Since(start)
			return zero, result
		}
	}

	result.TotalDuration = time.Since(start)
	return zero, result
}

// RetryVoid executes fn with retries for functions that don't return a value.
func RetryVoid(ctx context.Context, cfg *RetryConfig, fn func(ctx context.Context) error) *RetryResult {
	_, result := Retry(ctx, cfg, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return result
}

// shouldRetry determines if an error should trigger a retry.
func shouldRetry(cfg *RetryConfig, err error) bool {
	if err == nil {
		return false
	}

	// Don't retry context errors
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Use custom retry function if provided
	if cfg.RetryIf != nil {
		return cfg.RetryIf(err)
	}

	return true
}

// calculateDelay computes the delay for a given attempt.
func calculateDelay(cfg *RetryConfig, attempt int) time.Duration {
	var delay time.Duration

	switch cfg.Strategy {
	case BackoffConstant:
		delay = cfg.InitialDelay

	case BackoffLinear:
		delay = cfg.InitialDelay * time.Duration(attempt+1)

	case BackoffExponential:
		// 2^attempt * initial delay
		multiplier := math.Pow(2, float64(attempt))
		delay = time.Duration(float64(cfg.InitialDelay) * multiplier)

	default:
		delay = cfg.InitialDelay
	}

	// Apply max delay cap
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}

	// Apply jitter
	if cfg.JitterFraction > 0 {
		delay = addJitter(delay, cfg.JitterFraction)
	}

	return delay
}

// addJitter adds random variance to a duration.
func addJitter(d time.Duration, fraction float64) time.Duration {
	if fraction <= 0 {
		return d
	}

	// Calculate jitter range
	jitter := float64(d) * fraction
	// Random value between -jitter and +jitter
	delta := (rand.Float64()*2 - 1) * jitter

	result := time.Duration(float64(d) + delta)
	if result < 0 {
		result = 0
	}

	return result
}

// RetryableError wraps an error and indicates it should be retried.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// AsRetryable wraps an error to indicate it should be retried.
func AsRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err}
}

// IsRetryable checks if an error was marked as retryable.
func IsRetryable(err error) bool {
	var retryable *RetryableError
	return errors.As(err, &retryable)
}

// PermanentError wraps an error and indicates it should NOT be retried.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// AsPermanent wraps an error to indicate it should NOT be retried.
func AsPermanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent checks if an error was marked as permanent (non-retryable).
func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

// RetryIfNotPermanent is a retry predicate that retries all errors except permanent ones.
func RetryIfNotPermanent(err error) bool {
	return !IsPermanent(err)
}
