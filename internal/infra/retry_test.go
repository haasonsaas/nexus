package infra

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetry_Success(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
	}

	var attempts int32
	result, info := Retry(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "success", nil
	})

	if result != "success" {
		t.Errorf("expected 'success', got %q", result)
	}
	if info.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", info.Attempts)
	}
	if info.LastError != nil {
		t.Errorf("expected no error, got %v", info.LastError)
	}
	if attempts != 1 {
		t.Errorf("expected function called once, called %d times", attempts)
	}
}

func TestRetry_EventualSuccess(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
	}

	var attempts int32
	result, info := Retry(context.Background(), cfg, func(ctx context.Context) (string, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return "", errors.New("temporary error")
		}
		return "success", nil
	})

	if result != "success" {
		t.Errorf("expected 'success', got %q", result)
	}
	if info.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", info.Attempts)
	}
}

func TestRetry_ExhaustedRetries(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  2,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
	}

	testErr := errors.New("persistent error")
	var attempts int32
	result, info := Retry(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", testErr
	})

	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	if info.Attempts != 3 { // Initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", info.Attempts)
	}
	if !errors.Is(info.LastError, testErr) {
		t.Errorf("expected test error, got %v", info.LastError)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
	}

	// Use a pre-canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	var attempts int32
	_, info := Retry(ctx, cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", errors.New("error")
	})

	// First attempt runs, then the delay check sees context is canceled
	if !errors.Is(info.LastError, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", info.LastError)
	}

	// Should have made at most 1 attempt since context is canceled
	if atomic.LoadInt32(&attempts) > 1 {
		t.Errorf("expected at most 1 attempt with canceled context, got %d", attempts)
	}
}

func TestRetry_NoRetryOnContextErrors(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
	}

	var attempts int32
	_, info := Retry(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", context.DeadlineExceeded
	})

	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retries on context errors), got %d", attempts)
	}
	if !errors.Is(info.LastError, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", info.LastError)
	}
}

func TestRetry_CustomRetryPredicate(t *testing.T) {
	retryableErr := errors.New("retryable")
	permanentErr := errors.New("permanent")

	cfg := &RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
		RetryIf: func(err error) bool {
			return errors.Is(err, retryableErr)
		},
	}

	// Test with retryable error
	var attempts int32
	_, info := Retry(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", retryableErr
	})

	if info.Attempts != 6 { // Initial + 5 retries
		t.Errorf("expected 6 attempts for retryable error, got %d", info.Attempts)
	}

	// Test with permanent error
	attempts = 0
	_, info = Retry(context.Background(), cfg, func(ctx context.Context) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", permanentErr
	})

	if attempts != 1 {
		t.Errorf("expected 1 attempt for permanent error, got %d", attempts)
	}
}

func TestRetry_BackoffStrategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy BackoffStrategy
		delays   []time.Duration // Expected delays for attempts 0, 1, 2
	}{
		{
			name:     "constant",
			strategy: BackoffConstant,
			delays:   []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond},
		},
		{
			name:     "linear",
			strategy: BackoffLinear,
			delays:   []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond},
		},
		{
			name:     "exponential",
			strategy: BackoffExponential,
			delays:   []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &RetryConfig{
				InitialDelay:   100 * time.Millisecond,
				MaxDelay:       10 * time.Second,
				Strategy:       tt.strategy,
				JitterFraction: 0, // No jitter for predictable testing
			}

			for i, expected := range tt.delays {
				delay := calculateDelay(cfg, i)
				if delay != expected {
					t.Errorf("attempt %d: expected %v, got %v", i, expected, delay)
				}
			}
		})
	}
}

func TestRetry_MaxDelayCapped(t *testing.T) {
	cfg := &RetryConfig{
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       500 * time.Millisecond,
		Strategy:       BackoffExponential,
		JitterFraction: 0,
	}

	// At attempt 10, exponential would be 100ms * 2^10 = 102.4s
	// But it should be capped at 500ms
	delay := calculateDelay(cfg, 10)
	if delay != 500*time.Millisecond {
		t.Errorf("expected delay capped at 500ms, got %v", delay)
	}
}

func TestRetry_Jitter(t *testing.T) {
	cfg := &RetryConfig{
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       10 * time.Second,
		Strategy:       BackoffConstant,
		JitterFraction: 0.2, // ±20%
	}

	// Calculate multiple delays and verify they're within range
	minAllowed := 80 * time.Millisecond  // 100ms - 20%
	maxAllowed := 120 * time.Millisecond // 100ms + 20%

	sawVariation := false
	var lastDelay time.Duration

	for i := 0; i < 100; i++ {
		delay := calculateDelay(cfg, 0)
		if delay < minAllowed || delay > maxAllowed {
			t.Errorf("delay %v outside allowed range [%v, %v]", delay, minAllowed, maxAllowed)
		}
		if lastDelay != 0 && delay != lastDelay {
			sawVariation = true
		}
		lastDelay = delay
	}

	if !sawVariation {
		t.Error("jitter should cause variation in delays")
	}
}

func TestRetryVoid(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:  2,
		InitialDelay: 10 * time.Millisecond,
		Strategy:     BackoffConstant,
	}

	var attempts int32
	result := RetryVoid(context.Background(), cfg, func(ctx context.Context) error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			return errors.New("error")
		}
		return nil
	})

	if result.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", result.Attempts)
	}
	if result.LastError != nil {
		t.Errorf("expected success, got error: %v", result.LastError)
	}
}

func TestRetryableError(t *testing.T) {
	originalErr := errors.New("original")
	retryable := AsRetryable(originalErr)

	if !IsRetryable(retryable) {
		t.Error("expected IsRetryable to return true")
	}

	if !errors.Is(retryable, originalErr) {
		t.Error("expected errors.Is to find original error")
	}

	if IsRetryable(originalErr) {
		t.Error("original error should not be retryable")
	}

	if AsRetryable(nil) != nil {
		t.Error("AsRetryable(nil) should return nil")
	}
}

func TestPermanentError(t *testing.T) {
	originalErr := errors.New("original")
	permanent := AsPermanent(originalErr)

	if !IsPermanent(permanent) {
		t.Error("expected IsPermanent to return true")
	}

	if !errors.Is(permanent, originalErr) {
		t.Error("expected errors.Is to find original error")
	}

	if IsPermanent(originalErr) {
		t.Error("original error should not be permanent")
	}

	if AsPermanent(nil) != nil {
		t.Error("AsPermanent(nil) should return nil")
	}
}

func TestRetryIfNotPermanent(t *testing.T) {
	regular := errors.New("regular error")
	permanent := AsPermanent(errors.New("permanent"))

	if !RetryIfNotPermanent(regular) {
		t.Error("regular error should be retryable")
	}

	if RetryIfNotPermanent(permanent) {
		t.Error("permanent error should not be retryable")
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts 3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("expected InitialDelay 100ms, got %v", cfg.InitialDelay)
	}
	if cfg.Strategy != BackoffExponential {
		t.Errorf("expected BackoffExponential, got %v", cfg.Strategy)
	}
}

func TestRetry_NilConfig(t *testing.T) {
	var attempts int32
	result, info := Retry(context.Background(), nil, func(ctx context.Context) (string, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			return "", errors.New("error")
		}
		return "success", nil
	})

	if result != "success" {
		t.Errorf("expected success with nil config, got %q", result)
	}
	if info.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", info.Attempts)
	}
}
