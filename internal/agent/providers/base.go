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
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(b.retryDelay * time.Duration(attempt)):
			}
		}
	}
	return lastErr
}
