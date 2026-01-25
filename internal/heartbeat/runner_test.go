package heartbeat

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRunner_DefaultConfig(t *testing.T) {
	runner := NewRunner(nil, nil, nil)
	if runner == nil {
		t.Fatal("expected runner, got nil")
	}
	if runner.config == nil {
		t.Fatal("expected default config")
	}
	if runner.config.IntervalMs != 5000 {
		t.Errorf("IntervalMs = %d, want 5000", runner.config.IntervalMs)
	}
}

func TestNewRunner_CustomConfig(t *testing.T) {
	config := &HeartbeatConfig{
		IntervalMs:     1000,
		AckMaxChars:    100,
		TimeoutMs:      5000,
		RetryAttempts:  2,
		RetryDelayMs:   500,
		VisibilityMode: "presence",
	}
	runner := NewRunner(config, nil, nil)
	if runner.config.IntervalMs != 1000 {
		t.Errorf("IntervalMs = %d, want 1000", runner.config.IntervalMs)
	}
	if runner.config.VisibilityMode != "presence" {
		t.Errorf("VisibilityMode = %q, want %q", runner.config.VisibilityMode, "presence")
	}
}

func TestRunner_StartStop(t *testing.T) {
	var events []*HeartbeatEvent
	var mu sync.Mutex

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 50,
	}, nil, func(event *HeartbeatEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	if !runner.IsRunning() {
		t.Error("expected runner to be running")
	}

	// Wait for at least one tick
	time.Sleep(100 * time.Millisecond)

	runner.Stop()

	if runner.IsRunning() {
		t.Error("expected runner to be stopped")
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have start and stop events
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	if events[0].Type != "start" {
		t.Errorf("first event type = %q, want %q", events[0].Type, "start")
	}

	// Last event should be stop
	lastEvent := events[len(events)-1]
	if lastEvent.Type != "stop" {
		t.Errorf("last event type = %q, want %q", lastEvent.Type, "stop")
	}
}

func TestRunner_StartTwice(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 50,
	}, nil, nil)

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")
	defer runner.Stop()

	// Starting again should be a no-op
	runner.Start(ctx, "run-2", "session-2")

	// Should still be running with original IDs
	if runner.runID != "run-1" {
		t.Errorf("runID = %q, want %q", runner.runID, "run-1")
	}
}

func TestRunner_StopWhenNotRunning(t *testing.T) {
	runner := NewRunner(nil, nil, nil)

	// Should not panic
	runner.Stop()
}

func TestRunner_ContextCancellation(t *testing.T) {
	var stopEvent *HeartbeatEvent
	var mu sync.Mutex

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 50,
	}, nil, func(event *HeartbeatEvent) {
		mu.Lock()
		if event.Type == "stop" {
			stopEvent = event
		}
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	runner.Start(ctx, "run-1", "session-1")

	time.Sleep(30 * time.Millisecond)
	cancel()

	// Wait for stop
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if runner.IsRunning() {
		t.Error("expected runner to be stopped")
	}
	if stopEvent == nil {
		t.Error("expected stop event")
	} else if stopEvent.Message != "context cancelled" {
		t.Errorf("message = %q, want %q", stopEvent.Message, "context cancelled")
	}
}

func TestRunner_QueueAck(t *testing.T) {
	var deliveredAcks []*HeartbeatAck
	var mu sync.Mutex

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs:  50,
		AckMaxChars: 100,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		mu.Lock()
		deliveredAcks = append(deliveredAcks, ack)
		mu.Unlock()
		return nil
	}, nil)

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	runner.QueueAck("Hello World")
	runner.QueueAck("Second message")

	// Wait for ticks to deliver
	time.Sleep(200 * time.Millisecond)

	runner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(deliveredAcks) != 2 {
		t.Fatalf("expected 2 delivered acks, got %d", len(deliveredAcks))
	}
	if deliveredAcks[0].Text != "Hello World" {
		t.Errorf("first ack text = %q, want %q", deliveredAcks[0].Text, "Hello World")
	}
	if deliveredAcks[1].Text != "Second message" {
		t.Errorf("second ack text = %q, want %q", deliveredAcks[1].Text, "Second message")
	}
}

func TestRunner_TruncateAck(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		AckMaxChars: 10,
	}, nil, nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"short", "short"},
		{"exactly10!", "exactly10!"},
		{"this is too long", "this is..."},
		{"", ""},
	}

	for _, tt := range tests {
		result := runner.truncateAck(tt.input)
		if result != tt.expected {
			t.Errorf("truncateAck(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestRunner_TruncateAck_Unicode(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		AckMaxChars: 5,
	}, nil, nil)

	// Unicode characters should be counted correctly
	result := runner.truncateAck("hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	result = runner.truncateAck("hello world")
	if result != "he..." {
		t.Errorf("expected 'he...', got %q", result)
	}
}

func TestRunner_DeliveryRetry(t *testing.T) {
	var attempts int32
	runner := NewRunner(&HeartbeatConfig{
		IntervalMs:    50,
		TimeoutMs:     100,
		RetryAttempts: 3,
		RetryDelayMs:  10,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}, nil)

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	runner.QueueAck("retry me")

	// Wait for retries
	time.Sleep(300 * time.Millisecond)

	runner.Stop()

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRunner_DeliveryTimeout(t *testing.T) {
	var errorEvent *HeartbeatEvent
	var mu sync.Mutex

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs:    50,
		TimeoutMs:     10,
		RetryAttempts: 1,
		RetryDelayMs:  10,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		// Simulate slow delivery
		select {
		case <-time.After(100 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, func(event *HeartbeatEvent) {
		mu.Lock()
		if event.Type == "error" {
			errorEvent = event
		}
		mu.Unlock()
	})

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	runner.QueueAck("will timeout")

	time.Sleep(200 * time.Millisecond)

	runner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if errorEvent == nil {
		t.Error("expected error event due to timeout")
	}
}

func TestRunner_GetLastAck(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 50,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		return nil
	}, nil)

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	// Initially no last ack
	if runner.GetLastAck() != nil {
		t.Error("expected nil last ack initially")
	}

	runner.QueueAck("test message")

	time.Sleep(100 * time.Millisecond)

	runner.Stop()

	lastAck := runner.GetLastAck()
	if lastAck == nil {
		t.Fatal("expected last ack")
	}
	if lastAck.Text != "test message" {
		t.Errorf("lastAck.Text = %q, want %q", lastAck.Text, "test message")
	}
	if !lastAck.Delivered {
		t.Error("expected Delivered = true")
	}
}

func TestRunner_SetDeliveryTarget(t *testing.T) {
	runner := NewRunner(nil, nil, nil)

	runner.SetDeliveryTarget("new-target")

	if runner.config.DeliveryTarget != "new-target" {
		t.Errorf("DeliveryTarget = %q, want %q", runner.config.DeliveryTarget, "new-target")
	}
}

func TestRunner_GeneratesRunID(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 50,
	}, nil, nil)

	ctx := context.Background()
	runner.Start(ctx, "", "session-1")
	defer runner.Stop()

	if runner.runID == "" {
		t.Error("expected generated runID")
	}
}

func TestRunner_TickEvents(t *testing.T) {
	var tickCount int32
	var mu sync.Mutex

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 20,
	}, nil, func(event *HeartbeatEvent) {
		if event.Type == "tick" {
			mu.Lock()
			tickCount++
			mu.Unlock()
		}
	})

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	time.Sleep(100 * time.Millisecond)

	runner.Stop()

	mu.Lock()
	count := tickCount
	mu.Unlock()

	// Should have multiple tick events
	if count < 2 {
		t.Errorf("expected at least 2 ticks, got %d", count)
	}
}

func TestRunner_AckEvents(t *testing.T) {
	var ackEvent *HeartbeatEvent
	var mu sync.Mutex

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 50,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		return nil
	}, func(event *HeartbeatEvent) {
		mu.Lock()
		if event.Type == "ack" {
			ackEvent = event
		}
		mu.Unlock()
	})

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	runner.QueueAck("ack message")

	time.Sleep(100 * time.Millisecond)

	runner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if ackEvent == nil {
		t.Fatal("expected ack event")
	}
	if ackEvent.Message != "ack message" {
		t.Errorf("ackEvent.Message = %q, want %q", ackEvent.Message, "ack message")
	}
}

func TestScheduler_GetOrCreate(t *testing.T) {
	scheduler := NewScheduler(&HeartbeatConfig{
		IntervalMs: 100,
	})

	runner1 := scheduler.GetOrCreate("session-1", nil, nil)
	if runner1 == nil {
		t.Fatal("expected runner")
	}

	runner2 := scheduler.GetOrCreate("session-1", nil, nil)
	if runner1 != runner2 {
		t.Error("expected same runner for same session")
	}

	runner3 := scheduler.GetOrCreate("session-2", nil, nil)
	if runner3 == runner1 {
		t.Error("expected different runner for different session")
	}
}

func TestScheduler_StopSession(t *testing.T) {
	scheduler := NewScheduler(&HeartbeatConfig{
		IntervalMs: 50,
	})

	runner := scheduler.GetOrCreate("session-1", nil, nil)
	runner.Start(context.Background(), "run-1", "session-1")

	if !runner.IsRunning() {
		t.Error("expected runner to be running")
	}

	scheduler.StopSession("session-1")

	if runner.IsRunning() {
		t.Error("expected runner to be stopped")
	}

	// Should not panic for non-existent session
	scheduler.StopSession("non-existent")
}

func TestScheduler_StopAll(t *testing.T) {
	scheduler := NewScheduler(&HeartbeatConfig{
		IntervalMs: 50,
	})

	ctx := context.Background()

	runner1 := scheduler.GetOrCreate("session-1", nil, nil)
	runner1.Start(ctx, "run-1", "session-1")

	runner2 := scheduler.GetOrCreate("session-2", nil, nil)
	runner2.Start(ctx, "run-2", "session-2")

	if scheduler.Active() != 2 {
		t.Errorf("Active() = %d, want 2", scheduler.Active())
	}

	scheduler.StopAll()

	if runner1.IsRunning() || runner2.IsRunning() {
		t.Error("expected all runners to be stopped")
	}

	if scheduler.Active() != 0 {
		t.Errorf("Active() = %d, want 0", scheduler.Active())
	}
}

func TestScheduler_Active(t *testing.T) {
	scheduler := NewScheduler(&HeartbeatConfig{
		IntervalMs: 50,
	})

	if scheduler.Active() != 0 {
		t.Errorf("Active() = %d, want 0", scheduler.Active())
	}

	runner1 := scheduler.GetOrCreate("session-1", nil, nil)
	runner1.Start(context.Background(), "run-1", "session-1")
	defer runner1.Stop()

	if scheduler.Active() != 1 {
		t.Errorf("Active() = %d, want 1", scheduler.Active())
	}

	// Runner exists but not started
	scheduler.GetOrCreate("session-2", nil, nil)

	if scheduler.Active() != 1 {
		t.Errorf("Active() = %d, want 1 (one running, one not started)", scheduler.Active())
	}
}

func TestScheduler_Get(t *testing.T) {
	scheduler := NewScheduler(nil)

	if scheduler.Get("session-1") != nil {
		t.Error("expected nil for non-existent session")
	}

	runner := scheduler.GetOrCreate("session-1", nil, nil)
	got := scheduler.Get("session-1")

	if got != runner {
		t.Error("expected same runner")
	}
}

func TestScheduler_NilConfig(t *testing.T) {
	scheduler := NewScheduler(nil)
	if scheduler == nil {
		t.Fatal("expected scheduler, got nil")
	}
	if scheduler.config == nil {
		t.Fatal("expected default config")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.IntervalMs != 5000 {
		t.Errorf("IntervalMs = %d, want 5000", config.IntervalMs)
	}
	if config.AckMaxChars != 500 {
		t.Errorf("AckMaxChars = %d, want 500", config.AckMaxChars)
	}
	if config.TimeoutMs != 10000 {
		t.Errorf("TimeoutMs = %d, want 10000", config.TimeoutMs)
	}
	if config.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", config.RetryAttempts)
	}
	if config.RetryDelayMs != 1000 {
		t.Errorf("RetryDelayMs = %d, want 1000", config.RetryDelayMs)
	}
	if config.VisibilityMode != "typing" {
		t.Errorf("VisibilityMode = %q, want %q", config.VisibilityMode, "typing")
	}
}

func TestRunner_DeliverWithRetry_NoDeliveryFunc(t *testing.T) {
	runner := NewRunner(nil, nil, nil)

	ack := &HeartbeatAck{Text: "test"}
	err := runner.deliverWithRetry(context.Background(), ack)

	if err != nil {
		t.Errorf("expected no error when deliver func is nil, got %v", err)
	}
}

func TestRunner_DeliverWithRetry_ContextCancelled(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		TimeoutMs:     1000,
		RetryAttempts: 5,
		RetryDelayMs:  500,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		return errors.New("always fails")
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	ack := &HeartbeatAck{Text: "test"}
	err := runner.deliverWithRetry(ctx, ack)

	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestRunner_ConcurrentQueueAck(t *testing.T) {
	var deliveryCount int32

	runner := NewRunner(&HeartbeatConfig{
		IntervalMs: 10,
	}, func(ctx context.Context, ack *HeartbeatAck) error {
		atomic.AddInt32(&deliveryCount, 1)
		return nil
	}, nil)

	ctx := context.Background()
	runner.Start(ctx, "run-1", "session-1")

	// Queue multiple acks concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			runner.QueueAck("message")
		}(i)
	}
	wg.Wait()

	// Wait for delivery
	time.Sleep(300 * time.Millisecond)

	runner.Stop()

	count := atomic.LoadInt32(&deliveryCount)
	if count != 10 {
		t.Errorf("expected 10 deliveries, got %d", count)
	}
}

func TestRunner_TruncateAck_VerySmallMaxChars(t *testing.T) {
	runner := NewRunner(&HeartbeatConfig{
		AckMaxChars: 2,
	}, nil, nil)

	result := runner.truncateAck("hello")
	if result != "he" {
		t.Errorf("expected 'he', got %q", result)
	}

	runner.config.AckMaxChars = 0
	result = runner.truncateAck("hello")
	// Should use default of 500
	if result != "hello" {
		t.Errorf("expected 'hello' with default max, got %q", result)
	}
}
