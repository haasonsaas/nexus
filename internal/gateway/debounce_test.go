package gateway

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMessageDebouncer_SingleMessage(t *testing.T) {
	var flushed []*models.Message
	var mu sync.Mutex

	d := NewMessageDebouncer(50*time.Millisecond, 200*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			mu.Lock()
			flushed = append(flushed, messages...)
			mu.Unlock()
			return nil
		})

	msg := &models.Message{ID: "1", Content: "hello"}
	d.Enqueue(context.Background(), "session1", msg)

	// Wait for debounce
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(flushed) != 1 {
		t.Errorf("expected 1 flushed message, got %d", len(flushed))
	}
	mu.Unlock()
}

func TestMessageDebouncer_BatchMessages(t *testing.T) {
	var flushCount atomic.Int32
	var batchSize atomic.Int32

	d := NewMessageDebouncer(100*time.Millisecond, 500*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			flushCount.Add(1)
			batchSize.Store(int32(len(messages)))
			return nil
		})

	// Send 3 messages quickly
	for i := 0; i < 3; i++ {
		msg := &models.Message{ID: string(rune('1' + i)), Content: "msg"}
		d.Enqueue(context.Background(), "session1", msg)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(200 * time.Millisecond)

	if flushCount.Load() != 1 {
		t.Errorf("expected 1 flush, got %d", flushCount.Load())
	}
	if batchSize.Load() != 3 {
		t.Errorf("expected batch of 3, got %d", batchSize.Load())
	}
}

func TestMessageDebouncer_DifferentKeys(t *testing.T) {
	var flushCount atomic.Int32

	d := NewMessageDebouncer(50*time.Millisecond, 200*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			flushCount.Add(1)
			return nil
		})

	// Send to different sessions
	d.Enqueue(context.Background(), "session1", &models.Message{ID: "1"})
	d.Enqueue(context.Background(), "session2", &models.Message{ID: "2"})

	// Wait for debounce
	time.Sleep(100 * time.Millisecond)

	if flushCount.Load() != 2 {
		t.Errorf("expected 2 flushes (one per session), got %d", flushCount.Load())
	}
}

func TestMessageDebouncer_MaxWait(t *testing.T) {
	var flushCount atomic.Int32
	var batchSizes []int
	var mu sync.Mutex

	d := NewMessageDebouncer(100*time.Millisecond, 150*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			flushCount.Add(1)
			mu.Lock()
			batchSizes = append(batchSizes, len(messages))
			mu.Unlock()
			return nil
		})

	// Send messages that would exceed maxWait
	for i := 0; i < 5; i++ {
		d.Enqueue(context.Background(), "session1", &models.Message{ID: string(rune('1' + i))})
		time.Sleep(50 * time.Millisecond) // 50ms * 5 = 250ms > maxWait of 150ms
	}

	// Wait for all flushes
	time.Sleep(200 * time.Millisecond)

	if flushCount.Load() < 2 {
		t.Errorf("expected at least 2 flushes due to maxWait, got %d", flushCount.Load())
	}
}

func TestMessageDebouncer_FlushImmediate(t *testing.T) {
	var flushed []*models.Message
	var mu sync.Mutex

	d := NewMessageDebouncer(500*time.Millisecond, 2000*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			mu.Lock()
			flushed = append(flushed, messages...)
			mu.Unlock()
			return nil
		})

	// Enqueue a message
	d.Enqueue(context.Background(), "session1", &models.Message{ID: "1"})

	// Force flush
	d.Flush("session1")

	// Should be flushed immediately
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(flushed) != 1 {
		t.Errorf("expected 1 flushed message after Flush(), got %d", len(flushed))
	}
	mu.Unlock()
}

func TestMessageDebouncer_EnqueueImmediate(t *testing.T) {
	var flushCount atomic.Int32
	var allBatches [][]*models.Message
	var mu sync.Mutex

	d := NewMessageDebouncer(500*time.Millisecond, 2000*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			flushCount.Add(1)
			mu.Lock()
			allBatches = append(allBatches, messages)
			mu.Unlock()
			return nil
		})

	// Enqueue pending message
	d.Enqueue(context.Background(), "session1", &models.Message{ID: "1"})

	// Immediate message should flush pending and process new
	err := d.EnqueueImmediate(context.Background(), "session1", &models.Message{ID: "2"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Should have 2 flushes: pending batch + immediate
	if flushCount.Load() != 2 {
		t.Errorf("expected 2 flushes, got %d", flushCount.Load())
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify both messages were processed (order may vary due to async)
	var foundPending, foundImmediate bool
	for _, batch := range allBatches {
		for _, msg := range batch {
			if msg.ID == "1" {
				foundPending = true
			}
			if msg.ID == "2" {
				foundImmediate = true
			}
		}
	}

	if !foundPending {
		t.Error("pending message not flushed")
	}
	if !foundImmediate {
		t.Error("immediate message not processed")
	}
}

func TestMessageDebouncer_Close(t *testing.T) {
	var flushCount atomic.Int32

	d := NewMessageDebouncer(500*time.Millisecond, 2000*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			flushCount.Add(1)
			return nil
		})

	// Enqueue messages
	d.Enqueue(context.Background(), "session1", &models.Message{ID: "1"})
	d.Enqueue(context.Background(), "session2", &models.Message{ID: "2"})

	// Close should flush all
	d.Close()

	time.Sleep(50 * time.Millisecond)

	if flushCount.Load() != 2 {
		t.Errorf("expected 2 flushes on Close(), got %d", flushCount.Load())
	}

	// New messages should be ignored
	d.Enqueue(context.Background(), "session1", &models.Message{ID: "3"})
	time.Sleep(50 * time.Millisecond)

	if flushCount.Load() != 2 {
		t.Errorf("expected no new flushes after Close(), got %d", flushCount.Load())
	}
}

func TestMessageDebouncer_PendingCount(t *testing.T) {
	d := NewMessageDebouncer(500*time.Millisecond, 2000*time.Millisecond,
		func(ctx context.Context, messages []*models.Message) error {
			return nil
		})

	if d.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", d.PendingCount())
	}

	d.Enqueue(context.Background(), "session1", &models.Message{ID: "1"})
	d.Enqueue(context.Background(), "session2", &models.Message{ID: "2"})

	if d.PendingCount() != 2 {
		t.Errorf("expected 2 pending sessions, got %d", d.PendingCount())
	}

	if d.PendingMessages("session1") != 1 {
		t.Errorf("expected 1 pending message for session1, got %d", d.PendingMessages("session1"))
	}
}

func TestShouldDebounce(t *testing.T) {
	tests := []struct {
		name     string
		msg      *models.Message
		expected bool
	}{
		{"nil message", nil, false},
		{"normal message", &models.Message{Content: "hello"}, true},
		{"command message", &models.Message{Content: "/help"}, false},
		{"exclaim command", &models.Message{Content: "!stop"}, false},
		{"empty message", &models.Message{Content: ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldDebounce(tt.msg); got != tt.expected {
				t.Errorf("ShouldDebounce() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildDebounceKey(t *testing.T) {
	tests := []struct {
		name     string
		msg      *models.Message
		expected string
	}{
		{"nil message", nil, ""},
		{"normal message", &models.Message{ChannelID: "ch1", SessionID: "session1"}, "ch1:session1"},
		{"different session", &models.Message{ChannelID: "ch1", SessionID: "session2"}, "ch1:session2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildDebounceKey(tt.msg); got != tt.expected {
				t.Errorf("BuildDebounceKey() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultDebounceConfig(t *testing.T) {
	cfg := DefaultDebounceConfig()

	if cfg.DebounceMs != 500 {
		t.Errorf("expected DebounceMs 500, got %d", cfg.DebounceMs)
	}
	if cfg.MaxWaitMs != 2000 {
		t.Errorf("expected MaxWaitMs 2000, got %d", cfg.MaxWaitMs)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
}
