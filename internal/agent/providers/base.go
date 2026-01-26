package providers

import (
	"context"
	"time"
)

// BaseProvider holds shared retry configuration for LLM providers.
type BaseProvider struct {
	name       string
	maxRetries int
	retryDelay time.Duration
}

// NewBaseProvider creates a base provider with sane defaults.
func NewBaseProvider(name string, maxRetries int, retryDelay time.Duration) BaseProvider {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if retryDelay <= 0 {
		retryDelay = time.Second
	}
	return BaseProvider{
		name:       name,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

// Retry executes op with linear backoff if isRetryable returns true.
func (b *BaseProvider) Retry(ctx context.Context, isRetryable func(error) bool, op func() error) error {
	return b.RetryWithBackoff(ctx, isRetryable, op, nil)
}

// RetryWithBackoff executes op with custom backoff per attempt.
// If backoff is nil, a linear backoff of retryDelay * attempt is used.
func (b *BaseProvider) RetryWithBackoff(ctx context.Context, isRetryable func(error) bool, op func() error, backoff func(int) time.Duration) error {
	if op == nil {
		return nil
	}
	var lastErr error
	for attempt := 1; attempt <= b.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := op(); err == nil {
			return nil
		} else {
			lastErr = err
			if isRetryable == nil || !isRetryable(err) {
				return err
			}
			if attempt >= b.maxRetries {
				break
			}
			delay := b.retryDelay * time.Duration(attempt)
			if backoff != nil {
				delay = backoff(attempt)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return lastErr
}
