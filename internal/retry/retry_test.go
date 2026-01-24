package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_Success(t *testing.T) {
	config := DefaultConfig()
	config.MaxAttempts = 3

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		return nil
	})

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if result.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", result.Attempts)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_RetryThenSuccess(t *testing.T) {
	config := Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Factor:       2.0,
		Jitter:       false,
	}

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		if calls < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if result.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", result.Attempts)
	}
}

func TestDo_MaxAttempts(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Factor:       2.0,
	}

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		return errors.New("always fails")
	})

	if result.Err == nil {
		t.Error("expected error")
	}
	if result.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", result.Attempts)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_PermanentError(t *testing.T) {
	config := Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Millisecond,
	}

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		return Permanent(errors.New("permanent error"))
	})

	if result.Err == nil {
		t.Error("expected error")
	}
	if result.Attempts != 1 {
		t.Errorf("expected 1 attempt (no retry for permanent), got %d", result.Attempts)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_ContextCanceled(t *testing.T) {
	config := Config{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := Do(ctx, config, func() error {
		calls++
		return errors.New("retry")
	})

	if !errors.Is(result.Err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", result.Err)
	}
}

func TestDoWithValue(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
	}

	calls := 0
	value, result := DoWithValue(context.Background(), config, func() (int, error) {
		calls++
		if calls < 2 {
			return 0, errors.New("retry")
		}
		return 42, nil
	})

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if value != 42 {
		t.Errorf("expected 42, got %d", value)
	}
	if result.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", result.Attempts)
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		initial time.Duration
		max     time.Duration
		factor  float64
		want    time.Duration
	}{
		{1, 100 * time.Millisecond, 10 * time.Second, 2.0, 100 * time.Millisecond},
		{2, 100 * time.Millisecond, 10 * time.Second, 2.0, 200 * time.Millisecond},
		{3, 100 * time.Millisecond, 10 * time.Second, 2.0, 400 * time.Millisecond},
		{10, 100 * time.Millisecond, 1 * time.Second, 2.0, 1 * time.Second}, // Capped at max
	}

	for _, tt := range tests {
		got := Backoff(tt.attempt, tt.initial, tt.max, tt.factor)
		if got != tt.want {
			t.Errorf("Backoff(%d, %v, %v, %v) = %v, want %v",
				tt.attempt, tt.initial, tt.max, tt.factor, got, tt.want)
		}
	}
}

func TestLinear(t *testing.T) {
	config := Linear(5, 100*time.Millisecond)

	if config.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", config.MaxAttempts)
	}
	if config.Factor != 1.0 {
		t.Errorf("Factor = %f, want 1.0", config.Factor)
	}
	if config.Jitter {
		t.Error("Linear should not have jitter")
	}
}

func TestExponential(t *testing.T) {
	config := Exponential(5, 100*time.Millisecond, 10*time.Second)

	if config.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", config.MaxAttempts)
	}
	if config.Factor != 2.0 {
		t.Errorf("Factor = %f, want 2.0", config.Factor)
	}
	if !config.Jitter {
		t.Error("Exponential should have jitter")
	}
}

func TestPermanent(t *testing.T) {
	err := errors.New("original")
	perm := Permanent(err)

	if !IsPermanent(perm) {
		t.Error("should be permanent")
	}
	if !errors.Is(perm, err) {
		t.Error("should unwrap to original")
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("nil should not be retryable")
	}
	if IsRetryable(Permanent(errors.New("perm"))) {
		t.Error("permanent error should not be retryable")
	}
	if !IsRetryable(errors.New("temp")) {
		t.Error("regular error should be retryable")
	}
}

func TestWithAttemptNumber(t *testing.T) {
	config := Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Millisecond,
	}

	attempts := make([]int, 0)
	result := WithAttemptNumber(context.Background(), config, func(attempt int) error {
		attempts = append(attempts, attempt)
		if attempt < 3 {
			return errors.New("retry")
		}
		return nil
	})

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if len(attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", len(attempts))
	}
	if attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 3 {
		t.Errorf("unexpected attempt numbers: %v", attempts)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxAttempts != 3 {
		t.Error("wrong default MaxAttempts")
	}
	if config.Factor != 2.0 {
		t.Error("wrong default Factor")
	}
	if !config.Jitter {
		t.Error("default should have jitter")
	}
}

// Additional edge case tests

func TestDo_ZeroMaxAttempts(t *testing.T) {
	config := Config{
		MaxAttempts:  0, // Should be treated as 1
		InitialDelay: 1 * time.Millisecond,
	}

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		return errors.New("fail")
	})

	// Zero attempts should be normalized to 1
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if result.Err == nil {
		t.Error("expected error")
	}
}

func TestDo_NegativeMaxAttempts(t *testing.T) {
	config := Config{
		MaxAttempts:  -5, // Should be treated as 1
		InitialDelay: 1 * time.Millisecond,
	}

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		return nil
	})

	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
}

func TestDo_ZeroDelay(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 0, // Should use default
		MaxDelay:     0, // Should use default
		Factor:       0, // Should use default
	}

	calls := 0
	result := Do(context.Background(), config, func() error {
		calls++
		if calls < 2 {
			return errors.New("retry")
		}
		return nil
	})

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestDo_ContextCanceledBeforeFirstAttempt(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling Do

	calls := 0
	result := Do(ctx, config, func() error {
		calls++
		return nil
	})

	if calls != 0 {
		t.Errorf("expected 0 calls, got %d", calls)
	}
	if !errors.Is(result.Err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", result.Err)
	}
}

func TestDo_ContextDeadlineExceeded(t *testing.T) {
	config := Config{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	calls := 0
	result := Do(ctx, config, func() error {
		calls++
		return errors.New("retry")
	})

	if !errors.Is(result.Err, context.DeadlineExceeded) && !errors.Is(result.Err, context.Canceled) {
		t.Errorf("expected context deadline/canceled, got %v", result.Err)
	}
}

func TestDoWithValue_Failure(t *testing.T) {
	config := Config{
		MaxAttempts:  2,
		InitialDelay: 1 * time.Millisecond,
	}

	value, result := DoWithValue(context.Background(), config, func() (string, error) {
		return "", errors.New("always fails")
	})

	if result.Err == nil {
		t.Error("expected error")
	}
	if value != "" {
		t.Errorf("expected empty string on failure, got %q", value)
	}
	if result.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", result.Attempts)
	}
}

func TestDoWithValue_PermanentError(t *testing.T) {
	config := Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Millisecond,
	}

	calls := 0
	value, result := DoWithValue(context.Background(), config, func() (int, error) {
		calls++
		return -1, Permanent(errors.New("permanent"))
	})

	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if !IsPermanent(result.Err) {
		t.Error("expected permanent error")
	}
	if value != -1 {
		t.Errorf("expected -1, got %d", value)
	}
}

func TestBackoff_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		initial time.Duration
		max     time.Duration
		factor  float64
		want    time.Duration
	}{
		{"zero attempt", 0, 100 * time.Millisecond, 10 * time.Second, 2.0, 100 * time.Millisecond},
		{"negative attempt", -1, 100 * time.Millisecond, 10 * time.Second, 2.0, 100 * time.Millisecond},
		{"zero initial", 1, 0, 10 * time.Second, 2.0, 100 * time.Millisecond}, // Default
		{"zero max", 1, 100 * time.Millisecond, 0, 2.0, 100 * time.Millisecond},
		{"zero factor", 1, 100 * time.Millisecond, 10 * time.Second, 0, 100 * time.Millisecond}, // Default
		{"factor of 1", 5, 100 * time.Millisecond, 10 * time.Second, 1.0, 100 * time.Millisecond},
		{"factor of 3", 3, 100 * time.Millisecond, 10 * time.Second, 3.0, 900 * time.Millisecond},
		{"negative initial", 1, -100 * time.Millisecond, 10 * time.Second, 2.0, 100 * time.Millisecond},
		{"negative max", 1, 100 * time.Millisecond, -10 * time.Second, 2.0, 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Backoff(tt.attempt, tt.initial, tt.max, tt.factor)
			if got != tt.want {
				t.Errorf("Backoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBackoffWithJitter(t *testing.T) {
	// Run multiple times to verify jitter produces different results
	results := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		got := BackoffWithJitter(2, 100*time.Millisecond, 10*time.Second, 2.0)
		results[got] = true

		// Verify result is within expected range [100ms, 300ms] for attempt 2
		// Base is 200ms, jitter is [0.5, 1.5], so range is [100ms, 300ms]
		if got < 50*time.Millisecond || got > 350*time.Millisecond {
			t.Errorf("BackoffWithJitter() = %v, outside expected range", got)
		}
	}

	// With 100 iterations, we should see some variation
	if len(results) < 5 {
		t.Errorf("expected jitter to produce variation, got only %d unique values", len(results))
	}
}

func TestPermanent_Nil(t *testing.T) {
	result := Permanent(nil)
	if result != nil {
		t.Error("Permanent(nil) should return nil")
	}
}

func TestPermanentError_Error(t *testing.T) {
	original := errors.New("original message")
	perm := Permanent(original)

	if perm.Error() != "original message" {
		t.Errorf("Error() = %q, want %q", perm.Error(), "original message")
	}
}

func TestPermanentError_Unwrap(t *testing.T) {
	original := errors.New("wrapped error")
	perm := Permanent(original)

	unwrapped := errors.Unwrap(perm)
	if unwrapped != original {
		t.Error("Unwrap should return original error")
	}
}

func TestIsPermanent_NestedError(t *testing.T) {
	original := errors.New("base error")
	perm := Permanent(original)

	// Wrap the permanent error
	wrapped := errors.Join(errors.New("wrapper"), perm)

	if !IsPermanent(wrapped) {
		t.Error("IsPermanent should detect wrapped permanent error")
	}
}

func TestLinear_AllFields(t *testing.T) {
	config := Linear(10, 500*time.Millisecond)

	if config.MaxAttempts != 10 {
		t.Errorf("MaxAttempts = %d, want 10", config.MaxAttempts)
	}
	if config.InitialDelay != 500*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 500ms", config.InitialDelay)
	}
	if config.MaxDelay != 500*time.Millisecond {
		t.Errorf("MaxDelay = %v, want 500ms", config.MaxDelay)
	}
	if config.Factor != 1.0 {
		t.Errorf("Factor = %f, want 1.0", config.Factor)
	}
	if config.Jitter {
		t.Error("Linear should not have jitter")
	}
}

func TestExponential_AllFields(t *testing.T) {
	config := Exponential(7, 50*time.Millisecond, 5*time.Second)

	if config.MaxAttempts != 7 {
		t.Errorf("MaxAttempts = %d, want 7", config.MaxAttempts)
	}
	if config.InitialDelay != 50*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 50ms", config.InitialDelay)
	}
	if config.MaxDelay != 5*time.Second {
		t.Errorf("MaxDelay = %v, want 5s", config.MaxDelay)
	}
	if config.Factor != 2.0 {
		t.Errorf("Factor = %f, want 2.0", config.Factor)
	}
	if !config.Jitter {
		t.Error("Exponential should have jitter")
	}
}

func TestResult_Duration(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		Jitter:       false,
	}

	result := Do(context.Background(), config, func() error {
		time.Sleep(5 * time.Millisecond)
		return errors.New("fail")
	})

	// Duration should be at least (3 calls * 5ms) + (2 delays * 10ms) = 35ms
	// But allow some slack for timing variations
	if result.Duration < 15*time.Millisecond {
		t.Errorf("Duration = %v, expected at least 15ms", result.Duration)
	}
}

func TestWithAttemptNumber_AllFail(t *testing.T) {
	config := Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
	}

	attempts := make([]int, 0)
	result := WithAttemptNumber(context.Background(), config, func(attempt int) error {
		attempts = append(attempts, attempt)
		return errors.New("always fail")
	})

	if result.Err == nil {
		t.Error("expected error")
	}
	if len(attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", len(attempts))
	}
	if attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 3 {
		t.Errorf("unexpected attempt numbers: %v", attempts)
	}
}
