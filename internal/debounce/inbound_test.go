package debounce

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testMessage is a simple struct for testing the debouncer.
type testMessage struct {
	ID      string
	Channel string
	Content string
}

func TestResolveDebounceMs_Override(t *testing.T) {
	config := DebounceConfig{
		DebounceMs: 100,
		ByChannel: map[string]int{
			"slack": 200,
		},
	}

	override := 50
	result := ResolveDebounceMs(config, "slack", &override)

	if result != 50*time.Millisecond {
		t.Errorf("expected 50ms override, got %v", result)
	}
}

func TestResolveDebounceMs_ByChannel(t *testing.T) {
	config := DebounceConfig{
		DebounceMs: 100,
		ByChannel: map[string]int{
			"slack": 200,
		},
	}

	result := ResolveDebounceMs(config, "slack", nil)

	if result != 200*time.Millisecond {
		t.Errorf("expected 200ms from channel config, got %v", result)
	}
}

func TestResolveDebounceMs_Base(t *testing.T) {
	config := DebounceConfig{
		DebounceMs: 100,
		ByChannel: map[string]int{
			"slack": 200,
		},
	}

	result := ResolveDebounceMs(config, "discord", nil)

	if result != 100*time.Millisecond {
		t.Errorf("expected 100ms from base config, got %v", result)
	}
}

func TestResolveDebounceMs_NoConfig(t *testing.T) {
	config := DebounceConfig{}

	result := ResolveDebounceMs(config, "any", nil)

	if result != 0 {
		t.Errorf("expected 0 with no config, got %v", result)
	}
}

func TestDebouncer_ItemsWithSameKeyAreBatched(t *testing.T) {
	var flushedItems []*testMessage
	var mu sync.Mutex
	flushCalled := make(chan struct{}, 1)

	d := NewDebouncer(
		WithDebounceMs[testMessage](50),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushedItems = append(flushedItems, items...)
			mu.Unlock()
			select {
			case flushCalled <- struct{}{}:
			default:
			}
			return nil
		}),
	)
	defer d.Stop()

	// Enqueue multiple items with the same key
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "hello"})
	d.Enqueue(&testMessage{ID: "2", Channel: "slack", Content: "world"})
	d.Enqueue(&testMessage{ID: "3", Channel: "slack", Content: "!"})

	// Wait for flush
	select {
	case <-flushCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("flush was not called within timeout")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(flushedItems) != 3 {
		t.Errorf("expected 3 batched items, got %d", len(flushedItems))
	}
}

func TestDebouncer_ItemsWithDifferentKeysAreSeparate(t *testing.T) {
	flushes := make(map[string][]*testMessage)
	var mu sync.Mutex
	flushCount := int32(0)

	d := NewDebouncer(
		WithDebounceMs[testMessage](50),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			if len(items) > 0 {
				key := items[0].Channel
				flushes[key] = append(flushes[key], items...)
			}
			mu.Unlock()
			atomic.AddInt32(&flushCount, 1)
			return nil
		}),
	)
	defer d.Stop()

	// Enqueue items with different keys
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "slack1"})
	d.Enqueue(&testMessage{ID: "2", Channel: "discord", Content: "discord1"})
	d.Enqueue(&testMessage{ID: "3", Channel: "slack", Content: "slack2"})

	// Wait for all flushes
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushes) != 2 {
		t.Errorf("expected 2 separate flushes (slack, discord), got %d", len(flushes))
	}

	if len(flushes["slack"]) != 2 {
		t.Errorf("expected 2 slack items, got %d", len(flushes["slack"]))
	}

	if len(flushes["discord"]) != 1 {
		t.Errorf("expected 1 discord item, got %d", len(flushes["discord"]))
	}
}

func TestDebouncer_FlushAfterTimeout(t *testing.T) {
	flushTime := time.Time{}
	enqueueTime := time.Time{}
	var mu sync.Mutex
	flushCalled := make(chan struct{})

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushTime = time.Now()
			mu.Unlock()
			close(flushCalled)
			return nil
		}),
	)
	defer d.Stop()

	enqueueTime = time.Now()
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test"})

	select {
	case <-flushCalled:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("flush was not called within timeout")
	}

	mu.Lock()
	elapsed := flushTime.Sub(enqueueTime)
	mu.Unlock()

	// Should flush after approximately 100ms (allow some tolerance)
	if elapsed < 80*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("expected flush after ~100ms, got %v", elapsed)
	}
}

func TestDebouncer_ImmediateFlushWhenDebounceDisabled(t *testing.T) {
	var flushCount int32
	var mu sync.Mutex
	var flushedItems []*testMessage

	d := NewDebouncer(
		WithDebounceMs[testMessage](0), // Debounce disabled
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			atomic.AddInt32(&flushCount, 1)
			mu.Lock()
			flushedItems = append(flushedItems, items...)
			mu.Unlock()
			return nil
		}),
	)
	defer d.Stop()

	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test1"})
	d.Enqueue(&testMessage{ID: "2", Channel: "slack", Content: "test2"})

	// With debounce disabled, items should flush immediately
	time.Sleep(20 * time.Millisecond)

	count := atomic.LoadInt32(&flushCount)
	if count != 2 {
		t.Errorf("expected 2 immediate flushes, got %d", count)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flushedItems) != 2 {
		t.Errorf("expected 2 items flushed, got %d", len(flushedItems))
	}
}

func TestDebouncer_ImmediateFlushWhenShouldDebounceFalse(t *testing.T) {
	var flushCount int32

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithShouldDebounce(func(m *testMessage) bool {
			return m.Content != "urgent"
		}),
		WithOnFlush(func(items []*testMessage) error {
			atomic.AddInt32(&flushCount, 1)
			return nil
		}),
	)
	defer d.Stop()

	// This should flush immediately because shouldDebounce returns false
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "urgent"})

	// Give a moment for synchronous flush
	time.Sleep(20 * time.Millisecond)

	count := atomic.LoadInt32(&flushCount)
	if count != 1 {
		t.Errorf("expected immediate flush for urgent message, got %d flushes", count)
	}
}

func TestDebouncer_ManualFlushWithFlushKey(t *testing.T) {
	var flushedItems []*testMessage
	var mu sync.Mutex
	flushCalled := make(chan struct{}, 1)

	d := NewDebouncer(
		WithDebounceMs[testMessage](1000), // Long timeout
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushedItems = append(flushedItems, items...)
			mu.Unlock()
			select {
			case flushCalled <- struct{}{}:
			default:
			}
			return nil
		}),
	)
	defer d.Stop()

	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test1"})
	d.Enqueue(&testMessage{ID: "2", Channel: "slack", Content: "test2"})

	// Items should be pending
	if d.PendingItems() != 2 {
		t.Errorf("expected 2 pending items, got %d", d.PendingItems())
	}

	// Manually flush
	d.FlushKey("slack")

	select {
	case <-flushCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("flush was not called after FlushKey")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(flushedItems) != 2 {
		t.Errorf("expected 2 items after manual flush, got %d", len(flushedItems))
	}

	if d.PendingItems() != 0 {
		t.Errorf("expected 0 pending items after flush, got %d", d.PendingItems())
	}
}

func TestDebouncer_ErrorHandlingInOnFlush(t *testing.T) {
	testErr := errors.New("flush error")
	var capturedErr error
	var capturedItems []*testMessage
	var mu sync.Mutex
	errorCalled := make(chan struct{})

	d := NewDebouncer(
		WithDebounceMs[testMessage](50),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			return testErr
		}),
		WithOnError(func(err error, items []*testMessage) {
			mu.Lock()
			capturedErr = err
			capturedItems = items
			mu.Unlock()
			close(errorCalled)
		}),
	)
	defer d.Stop()

	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test"})

	select {
	case <-errorCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("onError was not called within timeout")
	}

	mu.Lock()
	defer mu.Unlock()

	if !errors.Is(capturedErr, testErr) {
		t.Errorf("expected error %v, got %v", testErr, capturedErr)
	}

	if len(capturedItems) != 1 {
		t.Errorf("expected 1 item in error callback, got %d", len(capturedItems))
	}
}

func TestDebouncer_ConcurrentAccess(t *testing.T) {
	var totalItems int32
	var mu sync.Mutex

	d := NewDebouncer(
		WithDebounceMs[testMessage](20),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			atomic.AddInt32(&totalItems, int32(len(items)))
			mu.Unlock()
			return nil
		}),
	)
	defer d.Stop()

	const numGoroutines = 10
	const itemsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				channel := "channel" // Same channel to test contention
				if j%2 == 0 {
					channel = "channel2"
				}
				d.Enqueue(&testMessage{
					ID:      "id",
					Channel: channel,
					Content: "test",
				})
			}
		}(i)
	}

	wg.Wait()

	// Wait for all pending items to flush
	time.Sleep(100 * time.Millisecond)

	total := atomic.LoadInt32(&totalItems)
	expected := int32(numGoroutines * itemsPerGoroutine)

	if total != expected {
		t.Errorf("expected %d total items flushed, got %d", expected, total)
	}
}

func TestDebouncer_StopCleansUpTimers(t *testing.T) {
	flushCalled := int32(0)

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			atomic.AddInt32(&flushCalled, 1)
			return nil
		}),
	)

	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test1"})
	d.Enqueue(&testMessage{ID: "2", Channel: "discord", Content: "test2"})

	// Verify items are pending
	if d.PendingCount() != 2 {
		t.Errorf("expected 2 pending keys, got %d", d.PendingCount())
	}

	// Stop the debouncer
	d.Stop()

	// Verify buffers are cleared
	if d.PendingCount() != 0 {
		t.Errorf("expected 0 pending keys after stop, got %d", d.PendingCount())
	}

	// Wait to ensure timers don't fire after stop
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&flushCalled) != 0 {
		t.Error("flush should not be called after Stop")
	}
}

func TestDebouncer_EnqueueAfterStop(t *testing.T) {
	flushCalled := int32(0)

	d := NewDebouncer(
		WithDebounceMs[testMessage](50),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			atomic.AddInt32(&flushCalled, 1)
			return nil
		}),
	)

	d.Stop()

	// Enqueue after stop should be a no-op
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test"})

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&flushCalled) != 0 {
		t.Error("flush should not be called after Stop")
	}
}

func TestDebouncer_EmptyKeyFlushesImmediately(t *testing.T) {
	var flushCount int32

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			if m.Channel == "" {
				return ""
			}
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			atomic.AddInt32(&flushCount, 1)
			return nil
		}),
	)
	defer d.Stop()

	// Empty key should flush immediately
	d.Enqueue(&testMessage{ID: "1", Channel: "", Content: "test"})

	time.Sleep(20 * time.Millisecond)

	count := atomic.LoadInt32(&flushCount)
	if count != 1 {
		t.Errorf("expected immediate flush for empty key, got %d flushes", count)
	}
}

func TestDebouncer_TimerResetsOnNewItem(t *testing.T) {
	flushTime := time.Time{}
	firstEnqueueTime := time.Time{}
	var mu sync.Mutex
	flushCalled := make(chan struct{})

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushTime = time.Now()
			mu.Unlock()
			close(flushCalled)
			return nil
		}),
	)
	defer d.Stop()

	firstEnqueueTime = time.Now()
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test1"})

	// Wait 50ms then add another item (should reset timer)
	time.Sleep(50 * time.Millisecond)
	d.Enqueue(&testMessage{ID: "2", Channel: "slack", Content: "test2"})

	select {
	case <-flushCalled:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("flush was not called within timeout")
	}

	mu.Lock()
	elapsed := flushTime.Sub(firstEnqueueTime)
	mu.Unlock()

	// Should flush ~150ms after first enqueue (50ms delay + 100ms debounce)
	if elapsed < 120*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Errorf("expected flush after ~150ms (timer reset), got %v", elapsed)
	}
}

func TestDebouncer_FlushKeyNonExistent(t *testing.T) {
	flushCalled := int32(0)

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			atomic.AddInt32(&flushCalled, 1)
			return nil
		}),
	)
	defer d.Stop()

	// FlushKey on non-existent key should be a no-op
	d.FlushKey("nonexistent")

	time.Sleep(20 * time.Millisecond)

	if atomic.LoadInt32(&flushCalled) != 0 {
		t.Error("flush should not be called for non-existent key")
	}
}

func TestDebouncer_WithDebounceDuration(t *testing.T) {
	flushTime := time.Time{}
	enqueueTime := time.Time{}
	var mu sync.Mutex
	flushCalled := make(chan struct{})

	d := NewDebouncer(
		WithDebounceDuration[testMessage](75*time.Millisecond),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushTime = time.Now()
			mu.Unlock()
			close(flushCalled)
			return nil
		}),
	)
	defer d.Stop()

	enqueueTime = time.Now()
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test"})

	select {
	case <-flushCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("flush was not called within timeout")
	}

	mu.Lock()
	elapsed := flushTime.Sub(enqueueTime)
	mu.Unlock()

	if elapsed < 60*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Errorf("expected flush after ~75ms, got %v", elapsed)
	}
}

func TestDebouncer_DefaultBuildKey(t *testing.T) {
	var flushedItems []*testMessage
	var mu sync.Mutex
	flushCalled := make(chan struct{}, 1)

	// No buildKey provided, should use default
	d := NewDebouncer(
		WithDebounceMs[testMessage](50),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushedItems = append(flushedItems, items...)
			mu.Unlock()
			select {
			case flushCalled <- struct{}{}:
			default:
			}
			return nil
		}),
	)
	defer d.Stop()

	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "test1"})
	d.Enqueue(&testMessage{ID: "2", Channel: "discord", Content: "test2"})

	select {
	case <-flushCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("flush was not called within timeout")
	}

	mu.Lock()
	defer mu.Unlock()

	// Both items should be batched under the default key
	if len(flushedItems) != 2 {
		t.Errorf("expected 2 items batched with default key, got %d", len(flushedItems))
	}
}

func TestDebouncer_FlushExistingBufferBeforeImmediate(t *testing.T) {
	var flushCounts []int
	var mu sync.Mutex

	d := NewDebouncer(
		WithDebounceMs[testMessage](100),
		WithBuildKey(func(m *testMessage) string {
			return m.Channel
		}),
		WithShouldDebounce(func(m *testMessage) bool {
			return m.Content != "urgent"
		}),
		WithOnFlush(func(items []*testMessage) error {
			mu.Lock()
			flushCounts = append(flushCounts, len(items))
			mu.Unlock()
			return nil
		}),
	)
	defer d.Stop()

	// Add items that should be debounced
	d.Enqueue(&testMessage{ID: "1", Channel: "slack", Content: "normal1"})
	d.Enqueue(&testMessage{ID: "2", Channel: "slack", Content: "normal2"})

	// Add an urgent item that should flush immediately
	// This should first flush the existing buffer, then flush the urgent item
	d.Enqueue(&testMessage{ID: "3", Channel: "slack", Content: "urgent"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have flushed the buffer (2 items) then the urgent item (1 item)
	if len(flushCounts) != 2 {
		t.Errorf("expected 2 flushes, got %d", len(flushCounts))
	}

	if len(flushCounts) >= 2 {
		if flushCounts[0] != 2 {
			t.Errorf("expected first flush to have 2 items, got %d", flushCounts[0])
		}
		if flushCounts[1] != 1 {
			t.Errorf("expected second flush to have 1 item, got %d", flushCounts[1])
		}
	}
}
