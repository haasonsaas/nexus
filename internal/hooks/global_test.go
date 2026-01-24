package hooks

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGlobal(t *testing.T) {
	// Reset global state for test
	resetGlobalForTest()

	reg := Global()
	if reg == nil {
		t.Error("expected non-nil registry")
	}

	// Calling Global() again should return the same instance
	reg2 := Global()
	if reg != reg2 {
		t.Error("expected same registry instance")
	}
}

func TestSetGlobalRegistry(t *testing.T) {
	// Reset global state
	resetGlobalForTest()

	// Create a new registry
	newReg := NewRegistry(nil)
	SetGlobalRegistry(newReg)

	// Global should now return the new registry
	if Global() != newReg {
		t.Error("expected SetGlobalRegistry to replace the global registry")
	}
}

func TestSetGlobalLogger(t *testing.T) {
	resetGlobalForTest()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	SetGlobalLogger(logger)

	// The logger is internal, so we can only verify it doesn't panic
	// and the registry still works
	id := Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		return nil
	})
	if id == "" {
		t.Error("expected registration to work after setting logger")
	}
}

func TestGlobal_Register(t *testing.T) {
	resetGlobalForTest()

	var called bool
	id := Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		called = true
		return nil
	})

	if id == "" {
		t.Error("expected non-empty registration ID")
	}

	event := NewEvent(EventMessageReceived, "")
	if err := Trigger(context.Background(), event); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !called {
		t.Error("handler was not called")
	}
}

func TestGlobal_Unregister(t *testing.T) {
	resetGlobalForTest()

	id := Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		return nil
	})

	if !Unregister(id) {
		t.Error("expected Unregister to return true")
	}

	if Unregister(id) {
		t.Error("expected Unregister to return false for already-removed handler")
	}
}

func TestGlobal_On(t *testing.T) {
	resetGlobalForTest()

	var called bool
	id := On(EventSessionCreated, func(ctx context.Context, e *Event) error {
		called = true
		return nil
	})

	if id == "" {
		t.Error("expected non-empty registration ID")
	}

	event := NewEvent(EventSessionCreated, "")
	if err := Trigger(context.Background(), event); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !called {
		t.Error("handler was not called")
	}
}

func TestGlobal_OnAction(t *testing.T) {
	resetGlobalForTest()

	var generalCalled, specificCalled bool

	On(EventCommandDetected, func(ctx context.Context, e *Event) error {
		generalCalled = true
		return nil
	})

	OnAction(EventCommandDetected, "help", func(ctx context.Context, e *Event) error {
		specificCalled = true
		return nil
	})

	// Trigger with "help" action
	event := NewEvent(EventCommandDetected, "help")
	if err := Trigger(context.Background(), event); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !generalCalled {
		t.Error("general handler should have been called")
	}
	if !specificCalled {
		t.Error("specific handler should have been called")
	}

	// Reset and trigger with different action
	generalCalled = false
	specificCalled = false

	event = NewEvent(EventCommandDetected, "other")
	Trigger(context.Background(), event)

	if !generalCalled {
		t.Error("general handler should have been called for other action")
	}
	if specificCalled {
		t.Error("specific handler should NOT have been called for other action")
	}
}

func TestGlobal_Emit(t *testing.T) {
	resetGlobalForTest()

	var receivedEvent *Event
	On(EventAgentStarted, func(ctx context.Context, e *Event) error {
		receivedEvent = e
		return nil
	})

	if err := Emit(context.Background(), EventAgentStarted, "test_action"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if receivedEvent == nil {
		t.Fatal("expected event to be received")
	}
	if receivedEvent.Type != EventAgentStarted {
		t.Errorf("expected type %s, got %s", EventAgentStarted, receivedEvent.Type)
	}
	if receivedEvent.Action != "test_action" {
		t.Errorf("expected action test_action, got %s", receivedEvent.Action)
	}
}

func TestGlobal_EmitAsync(t *testing.T) {
	resetGlobalForTest()

	var called atomic.Bool
	On(EventAgentCompleted, func(ctx context.Context, e *Event) error {
		time.Sleep(10 * time.Millisecond)
		called.Store(true)
		return nil
	})

	EmitAsync(context.Background(), EventAgentCompleted, "done")

	// Should return immediately
	if called.Load() {
		t.Error("handler should not have completed yet")
	}

	// Wait for async completion
	time.Sleep(50 * time.Millisecond)

	if !called.Load() {
		t.Error("handler should have been called")
	}
}

func TestGlobal_TriggerAsync(t *testing.T) {
	resetGlobalForTest()

	var called atomic.Bool
	On(EventGatewayStartup, func(ctx context.Context, e *Event) error {
		time.Sleep(10 * time.Millisecond)
		called.Store(true)
		return nil
	})

	event := NewEvent(EventGatewayStartup, "")
	TriggerAsync(context.Background(), event)

	// Should return immediately
	if called.Load() {
		t.Error("handler should not have completed yet")
	}

	// Wait for async completion
	time.Sleep(50 * time.Millisecond)

	if !called.Load() {
		t.Error("handler should have been called")
	}
}

func TestGlobal_WithOptions(t *testing.T) {
	resetGlobalForTest()

	var order []int

	Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		order = append(order, 2)
		return nil
	}, WithPriority(PriorityNormal), WithName("handler2"))

	Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		order = append(order, 1)
		return nil
	}, WithPriority(PriorityHigh), WithName("handler1"), WithSource("test-source"))

	event := NewEvent(EventMessageReceived, "")
	Trigger(context.Background(), event)

	if len(order) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(order))
	}
	if order[0] != 1 || order[1] != 2 {
		t.Errorf("expected order [1,2], got %v", order)
	}
}

func TestGlobal_ConcurrentAccess(t *testing.T) {
	resetGlobalForTest()

	var wg sync.WaitGroup
	var counter atomic.Int32

	// Register handler
	On(EventMessageReceived, func(ctx context.Context, e *Event) error {
		counter.Add(1)
		return nil
	})

	// Trigger concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Trigger(context.Background(), NewEvent(EventMessageReceived, ""))
		}()
	}

	wg.Wait()

	if counter.Load() != 100 {
		t.Errorf("expected 100 calls, got %d", counter.Load())
	}
}

// resetGlobalForTest resets the global registry state for testing.
// This ensures tests are isolated.
func resetGlobalForTest() {
	globalMu.Lock()
	globalRegistry = NewRegistry(nil)
	// Reset the Once by creating a new instance and immediately marking it as done
	// by calling Do with a no-op since we already set globalRegistry above
	globalOnce = sync.Once{}
	globalOnce.Do(func() {}) // Mark as done so Global() won't reinitialize
	globalMu.Unlock()
}
