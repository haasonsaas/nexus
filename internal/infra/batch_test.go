package infra

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchProcessor_Submit(t *testing.T) {
	processed := make([]int, 0)
	var mu sync.Mutex

	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 3, MaxWait: time.Second},
		func(ctx context.Context, items []int) ([]int, error) {
			mu.Lock()
			processed = append(processed, items...)
			mu.Unlock()

			// Return doubled values
			results := make([]int, len(items))
			for i, v := range items {
				results[i] = v * 2
			}
			return results, nil
		},
	)

	var wg sync.WaitGroup
	results := make([]int, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			result, err := bp.Submit(context.Background(), n+1)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			results[n] = result
		}(i)
	}

	wg.Wait()

	// All results should be doubled
	for i, r := range results {
		expected := (i + 1) * 2
		if r != expected {
			t.Errorf("expected result[%d] = %d, got %d", i, expected, r)
		}
	}

	// Should have processed 3 items
	mu.Lock()
	if len(processed) != 3 {
		t.Errorf("expected 3 processed items, got %d", len(processed))
	}
	mu.Unlock()
}

func TestBatchProcessor_MaxWait(t *testing.T) {
	var batchSize int
	var mu sync.Mutex

	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 100, MaxWait: 50 * time.Millisecond},
		func(ctx context.Context, items []int) ([]int, error) {
			mu.Lock()
			batchSize = len(items)
			mu.Unlock()
			return items, nil
		},
	)

	// Submit one item
	go func() {
		_, _ = bp.Submit(context.Background(), 1)
	}()

	// Wait for max wait to trigger
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if batchSize != 1 {
		t.Errorf("expected batch size 1 after timeout, got %d", batchSize)
	}
	mu.Unlock()
}

func TestBatchProcessor_MaxSize(t *testing.T) {
	var callCount int32

	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 5, MaxWait: time.Hour},
		func(ctx context.Context, items []int) ([]int, error) {
			atomic.AddInt32(&callCount, 1)
			return items, nil
		},
	)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = bp.Submit(context.Background(), n)
		}(i)
	}

	wg.Wait()

	// Should have called process function at least twice (2 batches of 5)
	count := atomic.LoadInt32(&callCount)
	if count < 2 {
		t.Errorf("expected at least 2 batch calls, got %d", count)
	}
}

func TestBatchProcessor_ProcessError(t *testing.T) {
	expectedErr := errors.New("batch error")

	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 2, MaxWait: time.Second},
		func(ctx context.Context, items []int) ([]int, error) {
			return nil, expectedErr
		},
	)

	var wg sync.WaitGroup
	var errors []error
	var mu sync.Mutex

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := bp.Submit(context.Background(), n)
			mu.Lock()
			errors = append(errors, err)
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Both should get the error
	for i, err := range errors {
		if err != expectedErr {
			t.Errorf("expected error %v for item %d, got %v", expectedErr, i, err)
		}
	}
}

func TestBatchProcessor_ContextCancellation(t *testing.T) {
	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 10, MaxWait: time.Hour},
		func(ctx context.Context, items []int) ([]int, error) {
			// Never called because context cancelled first
			return items, nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Submit but immediately cancel
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := bp.Submit(ctx, 1)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestBatchProcessor_Flush(t *testing.T) {
	var processed bool
	var mu sync.Mutex

	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 100, MaxWait: time.Hour},
		func(ctx context.Context, items []int) ([]int, error) {
			mu.Lock()
			processed = true
			mu.Unlock()
			return items, nil
		},
	)

	// Submit but don't fill batch
	go func() {
		_, _ = bp.Submit(context.Background(), 1)
	}()

	time.Sleep(10 * time.Millisecond)

	// Force flush
	bp.Flush()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !processed {
		t.Error("expected batch to be processed after Flush()")
	}
	mu.Unlock()
}

func TestBatchProcessor_Pending(t *testing.T) {
	bp := NewBatchProcessor(
		BatchConfig{MaxSize: 100, MaxWait: time.Hour},
		func(ctx context.Context, items []int) ([]int, error) {
			time.Sleep(100 * time.Millisecond)
			return items, nil
		},
	)

	// Add items async
	for i := 0; i < 5; i++ {
		go func(n int) {
			_, _ = bp.Submit(context.Background(), n)
		}(i)
	}

	time.Sleep(10 * time.Millisecond)

	pending := bp.Pending()
	if pending != 5 {
		t.Errorf("expected 5 pending, got %d", pending)
	}
}

func TestSimpleBatchProcessor_Add(t *testing.T) {
	var processed []int
	var mu sync.Mutex

	bp := NewSimpleBatchProcessor(
		BatchConfig{MaxSize: 3, MaxWait: time.Second},
		func(ctx context.Context, items []int) error {
			mu.Lock()
			processed = append(processed, items...)
			mu.Unlock()
			return nil
		},
	)

	bp.Add(1)
	bp.Add(2)
	bp.Add(3)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(processed) != 3 {
		t.Errorf("expected 3 processed items, got %d", len(processed))
	}
	mu.Unlock()
}

func TestSimpleBatchProcessor_AddMany(t *testing.T) {
	var processed []int
	var mu sync.Mutex

	bp := NewSimpleBatchProcessor(
		BatchConfig{MaxSize: 100, MaxWait: 50 * time.Millisecond},
		func(ctx context.Context, items []int) error {
			mu.Lock()
			processed = append(processed, items...)
			mu.Unlock()
			return nil
		},
	)

	bp.AddMany([]int{1, 2, 3, 4, 5})

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(processed) != 5 {
		t.Errorf("expected 5 processed items, got %d", len(processed))
	}
	mu.Unlock()
}

func TestSimpleBatchProcessor_MaxWait(t *testing.T) {
	var processed []int
	var mu sync.Mutex

	bp := NewSimpleBatchProcessor(
		BatchConfig{MaxSize: 100, MaxWait: 50 * time.Millisecond},
		func(ctx context.Context, items []int) error {
			mu.Lock()
			processed = append(processed, items...)
			mu.Unlock()
			return nil
		},
	)

	bp.Add(1)

	// Wait for max wait
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(processed) != 1 {
		t.Errorf("expected 1 processed item after timeout, got %d", len(processed))
	}
	mu.Unlock()
}

func TestSimpleBatchProcessor_Pending(t *testing.T) {
	bp := NewSimpleBatchProcessor(
		BatchConfig{MaxSize: 100, MaxWait: time.Hour},
		func(ctx context.Context, items []int) error {
			return nil
		},
	)

	bp.Add(1)
	bp.Add(2)
	bp.Add(3)

	if bp.Pending() != 3 {
		t.Errorf("expected 3 pending, got %d", bp.Pending())
	}
}

func TestBatchAggregator_Add(t *testing.T) {
	var flushed map[string]int
	var mu sync.Mutex

	ba := NewBatchAggregator(
		50*time.Millisecond,
		func(existing, new int) int {
			return existing + new
		},
		func(ctx context.Context, data map[string]int) {
			mu.Lock()
			flushed = data
			mu.Unlock()
		},
	)

	ba.Add("a", 1)
	ba.Add("b", 2)
	ba.Add("a", 3) // Should merge: a = 1 + 3 = 4

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if flushed == nil {
		t.Fatal("expected data to be flushed")
	}

	if flushed["a"] != 4 {
		t.Errorf("expected a = 4, got %d", flushed["a"])
	}

	if flushed["b"] != 2 {
		t.Errorf("expected b = 2, got %d", flushed["b"])
	}
}

func TestBatchAggregator_ManualFlush(t *testing.T) {
	var flushed map[string]int
	var mu sync.Mutex

	ba := NewBatchAggregator(
		time.Hour,
		func(existing, new int) int {
			return existing + new
		},
		func(ctx context.Context, data map[string]int) {
			mu.Lock()
			flushed = data
			mu.Unlock()
		},
	)

	ba.Add("x", 10)

	// Manual flush
	ba.Flush()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if flushed == nil || flushed["x"] != 10 {
		t.Error("expected manual flush to work")
	}
}

func TestBatchAggregator_Stop(t *testing.T) {
	var flushed map[string]int
	var mu sync.Mutex

	ba := NewBatchAggregator(
		time.Hour,
		func(existing, new int) int {
			return existing + new
		},
		func(ctx context.Context, data map[string]int) {
			mu.Lock()
			flushed = data
			mu.Unlock()
		},
	)

	ba.Add("x", 10)
	ba.Stop()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if flushed == nil || flushed["x"] != 10 {
		t.Error("expected Stop to flush pending data")
	}
	mu.Unlock()

	// Adding after stop should be ignored
	ba.Add("y", 20)

	if ba.Count() != 0 {
		t.Error("expected no items after stop")
	}
}

func TestBatchAggregator_Count(t *testing.T) {
	ba := NewBatchAggregator(
		time.Hour,
		func(existing, new int) int {
			return existing + new
		},
		func(ctx context.Context, data map[string]int) {},
	)

	ba.Add("a", 1)
	ba.Add("b", 2)
	ba.Add("a", 3)

	if ba.Count() != 2 {
		t.Errorf("expected count 2 (unique keys), got %d", ba.Count())
	}
}

func TestBatchAggregator_Concurrent(t *testing.T) {
	var total int64
	var mu sync.Mutex

	ba := NewBatchAggregator(
		50*time.Millisecond,
		func(existing, new int) int {
			return existing + new
		},
		func(ctx context.Context, data map[string]int) {
			mu.Lock()
			for _, v := range data {
				total += int64(v)
			}
			mu.Unlock()
		},
	)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ba.Add("counter", 1)
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if total != 100 {
		t.Errorf("expected total 100, got %d", total)
	}
	mu.Unlock()
}

func TestBatchProcessor_ZeroConfig(t *testing.T) {
	// Should not panic with zero config
	bp := NewBatchProcessor(
		BatchConfig{},
		func(ctx context.Context, items []int) ([]int, error) {
			return items, nil
		},
	)

	result, err := bp.Submit(context.Background(), 1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != 1 {
		t.Errorf("expected result 1, got %d", result)
	}
}

func TestSimpleBatchProcessor_ZeroConfig(t *testing.T) {
	// Should not panic with zero config
	bp := NewSimpleBatchProcessor(
		BatchConfig{},
		func(ctx context.Context, items []int) error {
			return nil
		},
	)

	bp.Add(1)
	bp.Flush()
	// Should complete without error
}

func TestBatchAggregator_ZeroInterval(t *testing.T) {
	// Should use default interval
	ba := NewBatchAggregator(
		0,
		func(existing, new int) int { return existing + new },
		func(ctx context.Context, data map[string]int) {},
	)

	// Should not panic
	ba.Add("x", 1)
	ba.Stop()
}
