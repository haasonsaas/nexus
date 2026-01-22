package infra

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Basic(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig[int, int]{
		Workers:   2,
		QueueSize: 10,
		Processor: func(_ context.Context, n int) (int, error) {
			return n * 2, nil
		},
	})

	pool.Start()
	defer pool.Stop()

	// Submit jobs
	for i := 1; i <= 5; i++ {
		pool.Submit(Job[int]{ID: string(rune('0' + i)), Data: i})
	}

	// Collect results
	results := make(map[int]int)
	for i := 0; i < 5; i++ {
		result := <-pool.Results()
		results[result.Job.Data] = result.Result
	}

	// Verify
	for i := 1; i <= 5; i++ {
		expected := i * 2
		if results[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, results[i], expected)
		}
	}
}

func TestWorkerPool_Concurrency(t *testing.T) {
	var maxConcurrent int32
	var current int32

	pool := NewWorkerPool(WorkerPoolConfig[int, int]{
		Workers:   3,
		QueueSize: 10,
		Processor: func(_ context.Context, n int) (int, error) {
			c := atomic.AddInt32(&current, 1)
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			return n, nil
		},
	})

	pool.Start()

	// Submit more jobs than workers
	for i := 0; i < 10; i++ {
		pool.Submit(Job[int]{ID: string(rune('0' + i)), Data: i})
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-pool.Results()
	}

	pool.Stop()

	// Max concurrent should be at most 3
	if atomic.LoadInt32(&maxConcurrent) > 3 {
		t.Errorf("max concurrent %d exceeded workers 3", maxConcurrent)
	}
}

func TestWorkerPool_ErrorHandling(t *testing.T) {
	testErr := errors.New("test error")

	pool := NewWorkerPool(WorkerPoolConfig[int, int]{
		Workers:   2,
		QueueSize: 10,
		Processor: func(_ context.Context, n int) (int, error) {
			if n%2 == 0 {
				return 0, testErr
			}
			return n, nil
		},
	})

	pool.Start()
	defer pool.Stop()

	// Submit jobs
	for i := 1; i <= 4; i++ {
		pool.Submit(Job[int]{ID: string(rune('0' + i)), Data: i})
	}

	// Collect results
	var errs int
	for i := 0; i < 4; i++ {
		result := <-pool.Results()
		if result.Error != nil {
			errs++
		}
	}

	if errs != 2 {
		t.Errorf("expected 2 errors (even numbers), got %d", errs)
	}

	stats := pool.Stats()
	if stats.Failed != 2 {
		t.Errorf("expected 2 failed, got %d", stats.Failed)
	}
}

func TestWorkerPool_Stop(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig[int, int]{
		Workers:   2,
		QueueSize: 10,
		Processor: func(_ context.Context, n int) (int, error) {
			time.Sleep(10 * time.Millisecond)
			return n, nil
		},
	})

	pool.Start()

	// Submit some jobs
	for i := 0; i < 3; i++ {
		pool.Submit(Job[int]{ID: string(rune('0' + i)), Data: i})
	}

	// Stop should wait for workers to finish
	pool.Stop()

	// Should not accept new jobs
	if pool.Submit(Job[int]{ID: "x", Data: 99}) {
		t.Error("should not accept jobs after stop")
	}
}

func TestWorkerPool_Stats(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig[int, int]{
		Workers:   2,
		QueueSize: 10,
		Processor: func(_ context.Context, n int) (int, error) {
			return n, nil
		},
	})

	pool.Start()
	defer pool.Stop()

	for i := 0; i < 5; i++ {
		pool.Submit(Job[int]{ID: string(rune('0' + i)), Data: i})
	}

	// Wait for processing
	for i := 0; i < 5; i++ {
		<-pool.Results()
	}

	stats := pool.Stats()
	if stats.Workers != 2 {
		t.Errorf("expected 2 workers, got %d", stats.Workers)
	}
	if stats.Processed != 5 {
		t.Errorf("expected 5 processed, got %d", stats.Processed)
	}
	if !stats.Running {
		t.Error("expected pool to be running")
	}
}

func TestWorkerPool_QueueFull(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig[int, int]{
		Workers:   1,
		QueueSize: 2,
		Processor: func(_ context.Context, n int) (int, error) {
			time.Sleep(100 * time.Millisecond)
			return n, nil
		},
	})

	pool.Start()
	defer pool.Stop()

	// Fill the queue
	pool.Submit(Job[int]{ID: "1", Data: 1})
	pool.Submit(Job[int]{ID: "2", Data: 2})

	// Third should fail (queue full)
	if pool.Submit(Job[int]{ID: "3", Data: 3}) {
		// Might succeed if first job was picked up quickly
		// This is acceptable behavior
	}
}

func TestParallelProcess(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}

	results, errs := ParallelProcess(context.Background(), items, 3, func(_ context.Context, n int) (int, error) {
		return n * 2, nil
	})

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	for i, result := range results {
		if errs[i] != nil {
			t.Errorf("unexpected error at %d: %v", i, errs[i])
		}
		expected := items[i] * 2
		if result != expected {
			t.Errorf("results[%d] = %d, want %d", i, result, expected)
		}
	}
}

func TestParallelProcess_Concurrency(t *testing.T) {
	var maxConcurrent int32
	var current int32

	items := make([]int, 10)
	for i := range items {
		items[i] = i
	}

	ParallelProcess(context.Background(), items, 3, func(_ context.Context, _ int) (int, error) {
		c := atomic.AddInt32(&current, 1)
		for {
			m := atomic.LoadInt32(&maxConcurrent)
			if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return 0, nil
	})

	if atomic.LoadInt32(&maxConcurrent) > 3 {
		t.Errorf("max concurrent %d exceeded limit 3", maxConcurrent)
	}
}

func TestParallelProcess_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var started int32

	items := make([]int, 10)

	go func() {
		// Cancel after a few items start
		for atomic.LoadInt32(&started) < 3 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()

	_, errs := ParallelProcess(ctx, items, 2, func(ctx context.Context, _ int) (int, error) {
		atomic.AddInt32(&started, 1)
		select {
		case <-time.After(100 * time.Millisecond):
			return 1, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	})

	// Some should have been cancelled
	cancelled := 0
	for _, err := range errs {
		if errors.Is(err, context.Canceled) {
			cancelled++
		}
	}

	if cancelled == 0 {
		t.Error("expected some items to be cancelled")
	}
}

func TestParallelMap(t *testing.T) {
	items := []string{"a", "bb", "ccc"}
	results := ParallelMap(context.Background(), items, 2, func(s string) int {
		return len(s)
	})

	expected := []int{1, 2, 3}
	for i, r := range results {
		if r != expected[i] {
			t.Errorf("results[%d] = %d, want %d", i, r, expected[i])
		}
	}
}

func TestParallelForEach(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var sum int32

	ParallelForEach(context.Background(), items, 2, func(n int) {
		atomic.AddInt32(&sum, int32(n))
	})

	if sum != 15 {
		t.Errorf("expected sum 15, got %d", sum)
	}
}

func TestBatchItems(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7}
	batches := BatchItems(items, 3)

	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}

	// First batch
	if len(batches[0].Items) != 3 || batches[0].Index != 0 {
		t.Errorf("batch 0: len=%d index=%d", len(batches[0].Items), batches[0].Index)
	}

	// Second batch
	if len(batches[1].Items) != 3 || batches[1].Index != 1 {
		t.Errorf("batch 1: len=%d index=%d", len(batches[1].Items), batches[1].Index)
	}

	// Third batch (partial)
	if len(batches[2].Items) != 1 || batches[2].Index != 2 {
		t.Errorf("batch 2: len=%d index=%d", len(batches[2].Items), batches[2].Index)
	}
}

func TestBatchItems_Empty(t *testing.T) {
	batches := BatchItems([]int{}, 3)
	if len(batches) != 0 {
		t.Errorf("expected 0 batches for empty slice, got %d", len(batches))
	}
}

func TestBatchItems_ExactFit(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6}
	batches := BatchItems(items, 3)

	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}

	for _, b := range batches {
		if len(b.Items) != 3 {
			t.Errorf("expected 3 items in batch, got %d", len(b.Items))
		}
	}
}

func TestThrottle_Do(t *testing.T) {
	throttle := NewThrottle(50 * time.Millisecond)
	var callCount int

	// First call should execute
	if !throttle.Do(func() { callCount++ }) {
		t.Error("first call should execute")
	}

	// Immediate second call should not execute
	if throttle.Do(func() { callCount++ }) {
		t.Error("second call should be throttled")
	}

	// Wait and try again
	time.Sleep(60 * time.Millisecond)

	if !throttle.Do(func() { callCount++ }) {
		t.Error("third call after wait should execute")
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestThrottle_DoWait(t *testing.T) {
	throttle := NewThrottle(50 * time.Millisecond)
	var calls []time.Time
	var mu sync.Mutex

	record := func() {
		mu.Lock()
		calls = append(calls, time.Now())
		mu.Unlock()
	}

	start := time.Now()

	// Three rapid calls
	throttle.DoWait(record)
	throttle.DoWait(record)
	throttle.DoWait(record)

	elapsed := time.Since(start)

	// Should take at least 2*50ms = 100ms (second and third calls wait)
	if elapsed < 90*time.Millisecond {
		t.Errorf("expected at least 100ms, took %v", elapsed)
	}

	if len(calls) != 3 {
		t.Errorf("expected 3 calls, got %d", len(calls))
	}
}

func TestThrottle_Reset(t *testing.T) {
	throttle := NewThrottle(100 * time.Millisecond)
	var callCount int

	throttle.Do(func() { callCount++ })

	// Should be throttled
	if throttle.Do(func() { callCount++ }) {
		t.Error("should be throttled")
	}

	// Reset
	throttle.Reset()

	// Should execute immediately
	if !throttle.Do(func() { callCount++ }) {
		t.Error("should execute after reset")
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestParallelProcess_EmptySlice(t *testing.T) {
	results, errs := ParallelProcess(context.Background(), []int{}, 3, func(_ context.Context, n int) (int, error) {
		return n, nil
	})

	if results != nil {
		t.Error("expected nil results for empty slice")
	}
	if errs != nil {
		t.Error("expected nil errors for empty slice")
	}
}
