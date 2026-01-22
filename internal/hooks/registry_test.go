package hooks

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry(nil)

	called := false
	id := r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		called = true
		return nil
	})

	if id == "" {
		t.Error("expected non-empty registration ID")
	}

	if r.HandlerCount(string(EventMessageReceived)) != 1 {
		t.Errorf("expected 1 handler, got %d", r.HandlerCount(string(EventMessageReceived)))
	}

	event := NewEvent(EventMessageReceived, "")
	if err := r.Trigger(context.Background(), event); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !called {
		t.Error("handler was not called")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry(nil)

	id := r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		return nil
	})

	if !r.Unregister(id) {
		t.Error("expected Unregister to return true")
	}

	if r.HandlerCount(string(EventMessageReceived)) != 0 {
		t.Errorf("expected 0 handlers after unregister, got %d", r.HandlerCount(string(EventMessageReceived)))
	}

	if r.Unregister(id) {
		t.Error("expected Unregister to return false for already-removed handler")
	}
}

func TestRegistry_Priority(t *testing.T) {
	r := NewRegistry(nil)

	var order []int

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		order = append(order, 2)
		return nil
	}, WithPriority(PriorityNormal))

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		order = append(order, 1)
		return nil
	}, WithPriority(PriorityHigh))

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		order = append(order, 3)
		return nil
	}, WithPriority(PriorityLow))

	event := NewEvent(EventMessageReceived, "")
	r.Trigger(context.Background(), event)

	if len(order) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(order))
	}

	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("expected order [1,2,3], got %v", order)
	}
}

func TestRegistry_SpecificAction(t *testing.T) {
	r := NewRegistry(nil)

	var generalCalled, specificCalled bool

	r.Register(string(EventCommandDetected), func(ctx context.Context, e *Event) error {
		generalCalled = true
		return nil
	})

	r.Register(string(EventCommandDetected)+":help", func(ctx context.Context, e *Event) error {
		specificCalled = true
		return nil
	})

	// Trigger with action "help"
	event := NewEvent(EventCommandDetected, "help")
	r.Trigger(context.Background(), event)

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
	r.Trigger(context.Background(), event)

	if !generalCalled {
		t.Error("general handler should have been called for other action")
	}
	if specificCalled {
		t.Error("specific handler should NOT have been called for other action")
	}
}

func TestRegistry_ErrorHandling(t *testing.T) {
	r := NewRegistry(nil)

	expectedErr := errors.New("test error")
	var secondCalled bool

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		return expectedErr
	}, WithPriority(PriorityHigh))

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		secondCalled = true
		return nil
	}, WithPriority(PriorityLow))

	event := NewEvent(EventMessageReceived, "")
	err := r.Trigger(context.Background(), event)

	// First error should be returned
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	// Second handler should still be called
	if !secondCalled {
		t.Error("second handler should have been called despite first error")
	}
}

func TestRegistry_PanicRecovery(t *testing.T) {
	r := NewRegistry(nil)

	var secondCalled bool

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		panic("test panic")
	}, WithPriority(PriorityHigh))

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		secondCalled = true
		return nil
	}, WithPriority(PriorityLow))

	event := NewEvent(EventMessageReceived, "")
	err := r.Trigger(context.Background(), event)

	if err == nil {
		t.Error("expected error from panic")
	}

	if !secondCalled {
		t.Error("second handler should have been called despite panic")
	}
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		return nil
	})
	r.Register(string(EventMessageSent), func(ctx context.Context, e *Event) error {
		return nil
	})

	r.Clear()

	if len(r.RegisteredEvents()) != 0 {
		t.Errorf("expected 0 registered events after clear, got %d", len(r.RegisteredEvents()))
	}
}

func TestRegistry_TriggerAsync(t *testing.T) {
	r := NewRegistry(nil)

	var called atomic.Bool

	r.Register(string(EventMessageReceived), func(ctx context.Context, e *Event) error {
		time.Sleep(10 * time.Millisecond)
		called.Store(true)
		return nil
	})

	event := NewEvent(EventMessageReceived, "")
	r.TriggerAsync(context.Background(), event)

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

func TestFilter_Matches(t *testing.T) {
	tests := []struct {
		name   string
		filter *Filter
		event  *Event
		want   bool
	}{
		{
			name:   "nil filter matches all",
			filter: nil,
			event:  NewEvent(EventMessageReceived, ""),
			want:   true,
		},
		{
			name:   "empty filter matches all",
			filter: &Filter{},
			event:  NewEvent(EventMessageReceived, ""),
			want:   true,
		},
		{
			name: "event type filter matches",
			filter: &Filter{
				EventTypes: []EventType{EventMessageReceived, EventMessageSent},
			},
			event: NewEvent(EventMessageReceived, ""),
			want:  true,
		},
		{
			name: "event type filter does not match",
			filter: &Filter{
				EventTypes: []EventType{EventMessageSent},
			},
			event: NewEvent(EventMessageReceived, ""),
			want:  false,
		},
		{
			name: "session key filter matches",
			filter: &Filter{
				SessionKeys: []string{"session-1", "session-2"},
			},
			event: NewEvent(EventMessageReceived, "").WithSession("session-1"),
			want:  true,
		},
		{
			name: "session key filter does not match",
			filter: &Filter{
				SessionKeys: []string{"session-1"},
			},
			event: NewEvent(EventMessageReceived, "").WithSession("session-2"),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Matches(tt.event); got != tt.want {
				t.Errorf("Filter.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvent_Builder(t *testing.T) {
	err := errors.New("test error")
	event := NewEvent(EventAgentError, "provider_failure").
		WithSession("session-123").
		WithChannel("channel-456", "discord").
		WithContext("model", "claude-3").
		WithError(err)

	if event.Type != EventAgentError {
		t.Errorf("expected type %s, got %s", EventAgentError, event.Type)
	}
	if event.Action != "provider_failure" {
		t.Errorf("expected action provider_failure, got %s", event.Action)
	}
	if event.SessionKey != "session-123" {
		t.Errorf("expected session session-123, got %s", event.SessionKey)
	}
	if event.ChannelID != "channel-456" {
		t.Errorf("expected channel channel-456, got %s", event.ChannelID)
	}
	if event.Context["model"] != "claude-3" {
		t.Errorf("expected context model claude-3, got %v", event.Context["model"])
	}
	if event.Error != err {
		t.Errorf("expected error %v, got %v", err, event.Error)
	}
	if event.ErrorMsg != "test error" {
		t.Errorf("expected error msg 'test error', got %s", event.ErrorMsg)
	}
}
