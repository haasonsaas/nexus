package infra

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	if cb.State() != CircuitClosed {
		t.Errorf("expected initial state to be closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_StaysClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
	})

	for i := 0; i < 10; i++ {
		err := cb.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected state to remain closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
	})

	testErr := errors.New("test error")

	for i := 0; i < 3; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.State() != CircuitOpen {
		t.Errorf("expected state to be open after %d failures, got %s", 3, cb.State())
	}
}

func TestCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          time.Hour, // Long timeout
	})

	// Trigger failure to open circuit
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("test error")
	})

	if cb.State() != CircuitOpen {
		t.Fatalf("expected circuit to be open")
	}

	// Should reject immediately
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          10 * time.Millisecond,
	})

	// Open the circuit
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("test error")
	})

	if cb.State() != CircuitOpen {
		t.Fatalf("expected circuit to be open")
	}

	// Wait for timeout
	time.Sleep(20 * time.Millisecond)

	// Next execution should be allowed (half-open)
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("expected execution to be allowed in half-open, got %v", err)
	}
}

func TestCircuitBreaker_ClosesAfterSuccessesInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          10 * time.Millisecond,
	})

	// Open the circuit
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("test error")
	})

	// Wait for timeout
	time.Sleep(20 * time.Millisecond)

	// Execute successfully twice
	for i := 0; i < 2; i++ {
		err := cb.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected circuit to close after successes, got %s", cb.State())
	}
}

func TestCircuitBreaker_ReopensOnFailureInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 3,
		Timeout:          10 * time.Millisecond,
	})

	// Open the circuit
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("test error")
	})

	// Wait for timeout
	time.Sleep(20 * time.Millisecond)

	// One success, then failure
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("another error")
	})

	if cb.State() != CircuitOpen {
		t.Errorf("expected circuit to reopen after failure in half-open, got %s", cb.State())
	}
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var transitions []string
	var mu sync.Mutex

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          10 * time.Millisecond,
		OnStateChange: func(from, to string) {
			mu.Lock()
			transitions = append(transitions, from+"->"+to)
			mu.Unlock()
		},
	})

	// Trigger open
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})

	// Wait for callback
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(transitions) != 1 || transitions[0] != "closed->open" {
		t.Errorf("expected transition closed->open, got %v", transitions)
	}
	mu.Unlock()
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          time.Hour,
	})

	// Open the circuit
	_ = cb.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})

	if cb.State() != CircuitOpen {
		t.Fatalf("expected circuit to be open")
	}

	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected circuit to be closed after reset, got %s", cb.State())
	}

	// Should allow execution
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error after reset: %v", err)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:             "test-circuit",
		FailureThreshold: 5,
	})

	// Record some failures
	for i := 0; i < 3; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return errors.New("error")
		})
	}

	stats := cb.Stats()

	if stats.Name != "test-circuit" {
		t.Errorf("expected name 'test-circuit', got %s", stats.Name)
	}
	if stats.State != CircuitClosed {
		t.Errorf("expected state closed, got %s", stats.State)
	}
	if stats.Failures != 3 {
		t.Errorf("expected 3 failures, got %d", stats.Failures)
	}
}

func TestExecuteWithResult(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
	})

	result, err := ExecuteWithResult(cb, context.Background(), func(ctx context.Context) (int, error) {
		return 42, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("expected result 42, got %d", result)
	}
}

func TestExecuteWithResult_ReturnsZeroWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          time.Hour,
	})

	// Open the circuit
	_, _ = ExecuteWithResult(cb, context.Background(), func(ctx context.Context) (int, error) {
		return 0, errors.New("error")
	})

	result, err := ExecuteWithResult(cb, context.Background(), func(ctx context.Context) (int, error) {
		return 42, nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
	if result != 0 {
		t.Errorf("expected zero value when open, got %d", result)
	}
}

func TestCircuitBreakerRegistry_Get(t *testing.T) {
	registry := NewCircuitBreakerRegistry(CircuitBreakerConfig{
		FailureThreshold: 10,
	})

	cb1 := registry.Get("service-a")
	cb2 := registry.Get("service-a")
	cb3 := registry.Get("service-b")

	if cb1 != cb2 {
		t.Error("expected same circuit breaker for same name")
	}
	if cb1 == cb3 {
		t.Error("expected different circuit breakers for different names")
	}
}

func TestCircuitBreakerRegistry_GetWithConfig(t *testing.T) {
	registry := NewCircuitBreakerRegistry(CircuitBreakerConfig{
		FailureThreshold: 10,
	})

	cb := registry.GetWithConfig("custom", CircuitBreakerConfig{
		FailureThreshold: 3,
	})

	// Trigger enough failures for custom threshold
	for i := 0; i < 3; i++ {
		_ = cb.Execute(context.Background(), func(ctx context.Context) error {
			return errors.New("error")
		})
	}

	if cb.State() != CircuitOpen {
		t.Error("expected circuit to open with custom threshold")
	}
}

func TestCircuitBreakerRegistry_Stats(t *testing.T) {
	registry := NewCircuitBreakerRegistry(CircuitBreakerConfig{})

	registry.Get("service-a")
	registry.Get("service-b")

	stats := registry.Stats()

	if len(stats) != 2 {
		t.Errorf("expected 2 stats entries, got %d", len(stats))
	}
}

func TestCircuitBreakerRegistry_OpenCircuits(t *testing.T) {
	registry := NewCircuitBreakerRegistry(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          time.Hour,
	})

	cb1 := registry.Get("healthy")
	cb2 := registry.Get("unhealthy")

	// Keep cb1 healthy
	_ = cb1.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	// Make cb2 unhealthy
	_ = cb2.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})

	open := registry.OpenCircuits()

	if len(open) != 1 {
		t.Fatalf("expected 1 open circuit, got %d", len(open))
	}
	if open[0] != "unhealthy" {
		t.Errorf("expected 'unhealthy' to be open, got %s", open[0])
	}
}

func TestCircuitBreakerRegistry_ResetAll(t *testing.T) {
	registry := NewCircuitBreakerRegistry(CircuitBreakerConfig{
		FailureThreshold: 1,
		Timeout:          time.Hour,
	})

	cb1 := registry.Get("service-a")
	cb2 := registry.Get("service-b")

	// Open both circuits
	_ = cb1.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})
	_ = cb2.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("error")
	})

	if len(registry.OpenCircuits()) != 2 {
		t.Fatalf("expected 2 open circuits")
	}

	registry.ResetAll()

	if len(registry.OpenCircuits()) != 0 {
		t.Error("expected no open circuits after reset")
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 100,
	})

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := cb.Execute(context.Background(), func(ctx context.Context) error {
				if n%2 == 0 {
					return errors.New("error")
				}
				return nil
			})
			if err != nil && !errors.Is(err, ErrCircuitOpen) {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Should complete without panic
	_ = cb.Stats()
}

func TestDefaultCircuitBreakerRegistry(t *testing.T) {
	// Reset for test
	DefaultCircuitBreakerRegistry = NewCircuitBreakerRegistry(CircuitBreakerConfig{})

	cb := GetCircuitBreaker("test-service")

	if cb == nil {
		t.Fatal("expected circuit breaker from default registry")
	}

	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
