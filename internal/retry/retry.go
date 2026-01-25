// Package retry provides utilities for retrying operations with configurable
// backoff strategies.
package retry

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

// Config configures retry behavior.
type Config struct {
	// MaxAttempts is the maximum number of attempts (including the first).
	MaxAttempts int
	// InitialDelay is the delay after the first failure.
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between attempts.
	MaxDelay time.Duration
	// Factor is the multiplier for exponential backoff.
	Factor float64
	// Jitter enables randomization of delays.
	Jitter bool
}

// DefaultConfig returns a default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Factor:       2.0,
		Jitter:       true,
	}
}

// Result contains the outcome of a retry operation.
type Result struct {
	// Attempts is the number of attempts made.
	Attempts int
	// Err is the last error (nil if successful).
	Err error
	// Duration is the total time spent retrying.
	Duration time.Duration
}

// Do executes the operation with retries.
func Do(ctx context.Context, config Config, op func() error) Result {
	start := time.Now()
	result := Result{}

	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = 100 * time.Millisecond
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 10 * time.Second
	}
	if config.Factor <= 0 {
		config.Factor = 2.0
	}

	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		result.Attempts = attempt

		// Check context before attempting
		if ctx.Err() != nil {
			result.Err = ctx.Err()
			result.Duration = time.Since(start)
			return result
		}

		// Execute operation
		err := op()
		if err == nil {
			result.Err = nil // Clear any error from previous attempts
			result.Duration = time.Since(start)
			return result
		}

		result.Err = err

		// Check if error is permanent (shouldn't retry)
		if IsPermanent(err) {
			result.Duration = time.Since(start)
			return result
		}

		// Don't sleep after the last attempt
		if attempt >= config.MaxAttempts {
			break
		}

		// Calculate sleep duration
		sleep := delay
		if config.Jitter {
			// Add jitter: delay * [0.5, 1.5]
			jitterFactor := 0.5 + rand.Float64() // #nosec G404 -- jitter does not require cryptographic randomness
			sleep = time.Duration(float64(delay) * jitterFactor)
		}

		// Sleep with context
		select {
		case <-ctx.Done():
			result.Err = ctx.Err()
			result.Duration = time.Since(start)
			return result
		case <-time.After(sleep):
		}

		// Increase delay for next attempt
		delay = time.Duration(float64(delay) * config.Factor)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	result.Duration = time.Since(start)
	return result
}

// DoWithValue executes an operation that returns a value with retries.
func DoWithValue[T any](ctx context.Context, config Config, op func() (T, error)) (T, Result) {
	var value T
	result := Do(ctx, config, func() error {
		var err error
		value, err = op()
		return err
	})
	return value, result
}

// PermanentError is an error that should not be retried.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// Permanent wraps an error to indicate it should not be retried.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent checks if an error is permanent (shouldn't retry).
func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

// Backoff calculates the backoff duration for a given attempt.
func Backoff(attempt int, initial, max time.Duration, factor float64) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	if initial <= 0 {
		initial = 100 * time.Millisecond
	}
	if max <= 0 {
		max = 10 * time.Second
	}
	if factor <= 0 {
		factor = 2.0
	}

	delay := float64(initial) * math.Pow(factor, float64(attempt-1))
	if delay > float64(max) {
		delay = float64(max)
	}
	return time.Duration(delay)
}

// BackoffWithJitter calculates the backoff with random jitter.
func BackoffWithJitter(attempt int, initial, max time.Duration, factor float64) time.Duration {
	base := Backoff(attempt, initial, max, factor)
	// Jitter: base * [0.5, 1.5]
	jitterFactor := 0.5 + rand.Float64() // #nosec G404 -- jitter does not require cryptographic randomness
	return time.Duration(float64(base) * jitterFactor)
}

// Linear creates a config for linear backoff.
func Linear(maxAttempts int, delay time.Duration) Config {
	return Config{
		MaxAttempts:  maxAttempts,
		InitialDelay: delay,
		MaxDelay:     delay,
		Factor:       1.0,
		Jitter:       false,
	}
}

// Exponential creates a config for exponential backoff.
func Exponential(maxAttempts int, initial, max time.Duration) Config {
	return Config{
		MaxAttempts:  maxAttempts,
		InitialDelay: initial,
		MaxDelay:     max,
		Factor:       2.0,
		Jitter:       true,
	}
}

// IsRetryable checks if an error is retryable (not permanent and not nil).
func IsRetryable(err error) bool {
	return err != nil && !IsPermanent(err)
}

// RetryableFunc is a function that can be retried.
type RetryableFunc func(attempt int) error

// WithAttemptNumber executes with attempt number available to the operation.
func WithAttemptNumber(ctx context.Context, config Config, op RetryableFunc) Result {
	attempt := 0
	return Do(ctx, config, func() error {
		attempt++
		return op(attempt)
	})
}
