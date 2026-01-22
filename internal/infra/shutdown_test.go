package infra

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShutdownCoordinator_PhaseOrder(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	var order []string
	var mu sync.Mutex

	record := func(name string) ShutdownFunc {
		return func(ctx context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}

	// Register in reverse order to ensure phase order is respected
	coord.RegisterFunc("cleanup1", PhaseCleanup, record("cleanup1"))
	coord.RegisterFunc("connection1", PhaseConnections, record("connection1"))
	coord.RegisterFunc("service1", PhaseServices, record("service1"))
	coord.RegisterFunc("preshutdown1", PhasePreShutdown, record("preshutdown1"))

	ctx := context.Background()
	coord.Shutdown(ctx)

	expected := []string{"preshutdown1", "service1", "connection1", "cleanup1"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d handlers, got %d: %v", len(expected), len(order), order)
	}

	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("order[%d] = %s, want %s", i, order[i], exp)
		}
	}
}

func TestShutdownCoordinator_ConcurrentWithinPhase(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	var maxConcurrent int32
	var current int32

	handler := func(_ string) ShutdownFunc {
		return func(ctx context.Context) error {
			c := atomic.AddInt32(&current, 1)
			// Track max concurrent
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			return nil
		}
	}

	// Register 3 handlers in the same phase
	for i := 0; i < 3; i++ {
		coord.RegisterService("service"+string(rune('A'+i)), handler("service"))
	}

	start := time.Now()
	coord.Shutdown(context.Background())
	elapsed := time.Since(start)

	// Should run concurrently, so total time should be ~30ms, not 90ms
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected concurrent execution, took %v", elapsed)
	}

	if maxConcurrent < 2 {
		t.Errorf("expected concurrent execution, max concurrent was %d", maxConcurrent)
	}
}

func TestShutdownCoordinator_HandlerError(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)
	testErr := errors.New("handler error")

	var handlersCalled int32

	coord.RegisterFunc("failing", PhaseServices, func(ctx context.Context) error {
		atomic.AddInt32(&handlersCalled, 1)
		return testErr
	})

	coord.RegisterFunc("succeeding", PhaseServices, func(ctx context.Context) error {
		atomic.AddInt32(&handlersCalled, 1)
		return nil
	})

	results := coord.Shutdown(context.Background())

	// Both handlers should still be called
	if atomic.LoadInt32(&handlersCalled) != 2 {
		t.Errorf("expected 2 handlers called, got %d", handlersCalled)
	}

	// Check results contain the error
	var foundError bool
	for _, r := range results {
		if r.Name == "failing" && errors.Is(r.Error, testErr) {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected to find handler error in results")
	}
}

func TestShutdownCoordinator_HandlerTimeout(t *testing.T) {
	coord := NewShutdownCoordinator(50*time.Millisecond, nil)

	coord.Register(ShutdownHandler{
		Name:    "slow",
		Phase:   PhaseServices,
		Timeout: 30 * time.Millisecond,
		Func: func(ctx context.Context) error {
			select {
			case <-time.After(100 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	start := time.Now()
	results := coord.Shutdown(context.Background())
	elapsed := time.Since(start)

	// Should timeout after ~30ms, not wait 100ms
	if elapsed > 80*time.Millisecond {
		t.Errorf("expected handler to timeout, took %v", elapsed)
	}

	// Result should have deadline exceeded error
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !errors.Is(results[0].Error, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", results[0].Error)
	}
}

func TestShutdownCoordinator_OnlyOnce(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	var callCount int32
	coord.RegisterFunc("counter", PhaseServices, func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	// Call shutdown multiple times
	coord.Shutdown(context.Background())
	coord.Shutdown(context.Background())
	coord.Shutdown(context.Background())

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected handler to be called once, called %d times", callCount)
	}
}

func TestShutdownCoordinator_IsShuttingDown(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	if coord.IsShuttingDown() {
		t.Error("should not be shutting down initially")
	}

	coord.Shutdown(context.Background())

	if !coord.IsShuttingDown() {
		t.Error("should be shutting down after Shutdown()")
	}
}

func TestShutdownCoordinator_Done(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	done := coord.Done()

	select {
	case <-done:
		t.Error("done channel should not be closed initially")
	default:
		// Expected
	}

	coord.Shutdown(context.Background())

	select {
	case <-done:
		// Expected
	default:
		t.Error("done channel should be closed after shutdown")
	}
}

func TestShutdownCoordinator_RegisterConvenience(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	var phases []ShutdownPhase

	coord.RegisterService("svc", func(ctx context.Context) error {
		return nil
	})

	coord.RegisterConnection("conn", func(ctx context.Context) error {
		return nil
	})

	results := coord.Shutdown(context.Background())

	for _, r := range results {
		phases = append(phases, r.Phase)
	}

	// Check phases
	if len(phases) != 2 {
		t.Fatalf("expected 2 results, got %d", len(phases))
	}

	foundServices := false
	foundConnections := false
	for _, p := range phases {
		if p == PhaseServices {
			foundServices = true
		}
		if p == PhaseConnections {
			foundConnections = true
		}
	}

	if !foundServices || !foundConnections {
		t.Error("expected both services and connections phases")
	}
}

func TestShutdownCoordinator_ContextCancellation(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	var phasesRun []ShutdownPhase
	var mu sync.Mutex

	record := func(phase ShutdownPhase) ShutdownFunc {
		return func(ctx context.Context) error {
			mu.Lock()
			phasesRun = append(phasesRun, phase)
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return nil
		}
	}

	coord.RegisterFunc("pre", PhasePreShutdown, record(PhasePreShutdown))
	coord.RegisterFunc("svc", PhaseServices, record(PhaseServices))
	coord.RegisterFunc("conn", PhaseConnections, record(PhaseConnections))

	// Cancel context after first phase
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	coord.Shutdown(ctx)

	// Should have run at least 1 phase, but may not complete all
	mu.Lock()
	count := len(phasesRun)
	mu.Unlock()

	if count == 0 {
		t.Error("expected at least one phase to run")
	}
}

func TestShutdownPhase_String(t *testing.T) {
	tests := []struct {
		phase    ShutdownPhase
		expected string
	}{
		{PhasePreShutdown, "pre-shutdown"},
		{PhaseServices, "services"},
		{PhaseConnections, "connections"},
		{PhaseCleanup, "cleanup"},
		{ShutdownPhase(99), "phase-99"},
	}

	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.expected {
			t.Errorf("%d.String() = %q, want %q", tt.phase, got, tt.expected)
		}
	}
}

func TestShutdownCoordinator_Results(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	coord.RegisterFunc("handler1", PhaseServices, func(ctx context.Context) error {
		return nil
	})
	coord.RegisterFunc("handler2", PhaseServices, func(ctx context.Context) error {
		return errors.New("failed")
	})

	coord.Shutdown(context.Background())

	results := coord.Results()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Results should be accessible after shutdown
	for _, r := range results {
		if r.Name == "" {
			t.Error("result should have a name")
		}
		if r.Duration == 0 {
			t.Error("result should have a duration")
		}
	}
}

func TestShutdownCoordinator_InvalidPhase(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	var called bool
	coord.Register(ShutdownHandler{
		Name:  "invalid-phase",
		Phase: ShutdownPhase(100), // Invalid phase
		Func: func(ctx context.Context) error {
			called = true
			return nil
		},
	})

	coord.Shutdown(context.Background())

	// Should be assigned to PhaseCleanup and called
	if !called {
		t.Error("handler with invalid phase should still be called")
	}
}

func TestShutdownCoordinator_EmptyPhases(t *testing.T) {
	coord := NewShutdownCoordinator(5*time.Second, nil)

	// Only register in one phase
	coord.RegisterFunc("cleanup", PhaseCleanup, func(ctx context.Context) error {
		return nil
	})

	// Should not panic with empty phases
	results := coord.Shutdown(context.Background())

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
