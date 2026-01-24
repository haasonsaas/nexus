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
