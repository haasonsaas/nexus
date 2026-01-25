package backoff

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

var errTemporary = errors.New("temporary error")

func TestRetryWithBackoff_SucceedsFirstAttempt(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 10,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	result, err := RetryWithBackoff(ctx, policy, 3, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "success", nil
	})

	if err != nil {
		t.Errorf("RetryWithBackoff() error = %v, want nil", err)
	}
	if result.Value != "success" {
		t.Errorf("RetryWithBackoff() value = %v, want success", result.Value)
	}
	if result.Attempts != 1 {
		t.Errorf("RetryWithBackoff() attempts = %v, want 1", result.Attempts)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("Function called %v times, want 1", attempts)
	}
}

func TestRetryWithBackoff_SucceedsAfterRetries(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 5,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	result, err := RetryWithBackoff(ctx, policy, 5, func(attempt int) (int, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return 0, errTemporary
		}
		return int(n), nil
	})

	if err != nil {
		t.Errorf("RetryWithBackoff() error = %v, want nil", err)
	}
	if result.Value != 3 {
		t.Errorf("RetryWithBackoff() value = %v, want 3", result.Value)
	}
	if result.Attempts != 3 {
		t.Errorf("RetryWithBackoff() attempts = %v, want 3", result.Attempts)
	}
}

func TestRetryWithBackoff_AllAttemptsFail(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 5,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	result, err := RetryWithBackoff(ctx, policy, 3, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", errTemporary
	})

	if !errors.Is(err, ErrMaxAttemptsExhausted) {
		t.Errorf("RetryWithBackoff() error = %v, want ErrMaxAttemptsExhausted", err)
	}
	if result.LastError != errTemporary {
		t.Errorf("RetryWithBackoff() LastError = %v, want errTemporary", result.LastError)
	}
	if result.Attempts != 3 {
		t.Errorf("RetryWithBackoff() attempts = %v, want 3", result.Attempts)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Function called %v times, want 3", attempts)
	}
}

func TestRetryWithBackoff_ContextCancelledBetweenAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	policy := BackoffPolicy{
		InitialMs: 100, // Long enough to cancel during sleep
		MaxMs:     1000,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	// Cancel after first attempt fails
	go func() {
		for atomic.LoadInt32(&attempts) < 1 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result, err := RetryWithBackoff(ctx, policy, 5, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", errTemporary
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("RetryWithBackoff() error = %v, want context.Canceled", err)
	}
	if result.Attempts < 1 {
		t.Errorf("RetryWithBackoff() attempts = %v, want >= 1", result.Attempts)
	}
	// Should complete quickly due to cancellation
	if elapsed > 200*time.Millisecond {
		t.Errorf("RetryWithBackoff() took too long: %v", elapsed)
	}
}

func TestRetryWithBackoff_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	policy := BackoffPolicy{
		InitialMs: 100,
		MaxMs:     1000,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	result, err := RetryWithBackoff(ctx, policy, 5, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "success", nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("RetryWithBackoff() error = %v, want context.Canceled", err)
	}
	if atomic.LoadInt32(&attempts) != 0 {
		t.Errorf("Function called %v times, want 0", attempts)
	}
	if result.Attempts != 1 {
		t.Errorf("RetryWithBackoff() attempts = %v, want 1 (checked before first attempt)", result.Attempts)
	}
}

func TestRetryWithBackoff_AttemptNumberPassedCorrectly(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 1,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	var receivedAttempts []int
	_, _ = RetryWithBackoff(ctx, policy, 3, func(attempt int) (struct{}, error) {
		receivedAttempts = append(receivedAttempts, attempt)
		return struct{}{}, errTemporary
	})

	expected := []int{1, 2, 3}
	if len(receivedAttempts) != len(expected) {
		t.Fatalf("Got %v attempts, want %v", len(receivedAttempts), len(expected))
	}
	for i, v := range expected {
		if receivedAttempts[i] != v {
			t.Errorf("Attempt %d: got %v, want %v", i, receivedAttempts[i], v)
		}
	}
}

func TestRetryWithBackoff_SingleAttempt(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 10,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	_, err := RetryWithBackoff(ctx, policy, 1, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", errTemporary
	})

	if !errors.Is(err, ErrMaxAttemptsExhausted) {
		t.Errorf("RetryWithBackoff() error = %v, want ErrMaxAttemptsExhausted", err)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("Function called %v times, want 1", attempts)
	}
}

func TestRetryWithBackoff_ZeroAttempts(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 10,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	var attempts int32
	_, err := RetryWithBackoff(ctx, policy, 0, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "success", nil
	})

	if !errors.Is(err, ErrMaxAttemptsExhausted) {
		t.Errorf("RetryWithBackoff() error = %v, want ErrMaxAttemptsExhausted", err)
	}
	if atomic.LoadInt32(&attempts) != 0 {
		t.Errorf("Function called %v times, want 0", attempts)
	}
}

func TestRetryFunc(t *testing.T) {
	ctx := context.Background()

	var attempts int32
	result, err := RetryFunc(ctx, 3, func(attempt int) (string, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			return "", errTemporary
		}
		return "done", nil
	})

	if err != nil {
		t.Errorf("RetryFunc() error = %v, want nil", err)
	}
	if result != "done" {
		t.Errorf("RetryFunc() result = %v, want done", result)
	}
}

func TestRetryFunc_Failure(t *testing.T) {
	ctx := context.Background()

	_, err := RetryFunc(ctx, 2, func(attempt int) (string, error) {
		return "", errTemporary
	})

	if !errors.Is(err, ErrMaxAttemptsExhausted) {
		t.Errorf("RetryFunc() error = %v, want ErrMaxAttemptsExhausted", err)
	}
}

func TestRetrySimple_Success(t *testing.T) {
	ctx := context.Background()

	var attempts int32
	err := RetrySimple(ctx, 3, func() error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			return errTemporary
		}
		return nil
	})

	if err != nil {
		t.Errorf("RetrySimple() error = %v, want nil", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("Function called %v times, want 2", attempts)
	}
}

func TestRetrySimple_Failure(t *testing.T) {
	ctx := context.Background()

	var attempts int32
	err := RetrySimple(ctx, 2, func() error {
		atomic.AddInt32(&attempts, 1)
		return errTemporary
	})

	if !errors.Is(err, ErrMaxAttemptsExhausted) {
		t.Errorf("RetrySimple() error = %v, want ErrMaxAttemptsExhausted", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("Function called %v times, want 2", attempts)
	}
}

func TestRetryWithBackoff_BackoffActuallyApplied(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 20,
		MaxMs:     1000,
		Factor:    2,
		Jitter:    0,
	}

	start := time.Now()
	var attempts int32
	_, _ = RetryWithBackoff(ctx, policy, 3, func(attempt int) (string, error) {
		atomic.AddInt32(&attempts, 1)
		return "", errTemporary
	})
	elapsed := time.Since(start)

	// With 3 attempts and backoff after attempts 1 and 2:
	// Sleep 1: 20ms (after attempt 1)
	// Sleep 2: 40ms (after attempt 2)
	// Total minimum sleep: 60ms
	// Adding some tolerance for timing
	if elapsed < 50*time.Millisecond {
		t.Errorf("RetryWithBackoff() completed too quickly: %v, expected >= 50ms of backoff", elapsed)
	}
}

func TestRetryWithBackoff_GenericTypes(t *testing.T) {
	ctx := context.Background()
	policy := BackoffPolicy{
		InitialMs: 1,
		MaxMs:     100,
		Factor:    2,
		Jitter:    0,
	}

	// Test with struct type
	type Result struct {
		Value int
		Name  string
	}

	result, err := RetryWithBackoff(ctx, policy, 1, func(attempt int) (Result, error) {
		return Result{Value: 42, Name: "test"}, nil
	})

	if err != nil {
		t.Errorf("RetryWithBackoff() error = %v, want nil", err)
	}
	if result.Value.Value != 42 || result.Value.Name != "test" {
		t.Errorf("RetryWithBackoff() value = %+v, want {Value:42 Name:test}", result.Value)
	}
}
