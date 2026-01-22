package infra

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommandQueue_SerialExecution(t *testing.T) {
	q := NewCommandQueue()

	var order []int
	var mu sync.Mutex

	// Enqueue 3 tasks that should run serially
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				order = append(order, i)
				mu.Unlock()
				return i, nil
			}, nil)
		}()
		// Small delay to ensure ordering
		time.Sleep(5 * time.Millisecond)
	}

	wg.Wait()

	if len(order) != 3 {
		t.Fatalf("expected 3 results, got %d", len(order))
	}

	// Tasks should complete in order due to serial execution
	for i, v := range order {
		if v != i+1 {
			t.Errorf("expected order[%d] = %d, got %d", i, i+1, v)
		}
	}
}

func TestCommandQueue_ParallelLanes(t *testing.T) {
	q := NewCommandQueue()
	q.SetLaneConcurrency("parallel", 3)

	var count int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = q.EnqueueInLane(context.Background(), "parallel", func(ctx context.Context) (any, error) {
				current := atomic.AddInt32(&count, 1)

				// Track max concurrent
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if current <= max {
						break
					}
					if atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
						break
					}
				}

				time.Sleep(50 * time.Millisecond)
				atomic.AddInt32(&count, -1)
				return nil, nil
			}, nil)
		}()
	}

	wg.Wait()

	max := atomic.LoadInt32(&maxConcurrent)
	if max != 3 {
		t.Errorf("expected max concurrent 3, got %d", max)
	}
}

func TestCommandQueue_DifferentLanesRunConcurrently(t *testing.T) {
	q := NewCommandQueue()

	var mainStarted, cronStarted atomic.Bool
	var bothRunning atomic.Bool

	var wg sync.WaitGroup
	wg.Add(2)

	// Main lane task
	go func() {
		defer wg.Done()
		_, _ = q.EnqueueInLane(context.Background(), "main", func(ctx context.Context) (any, error) {
			mainStarted.Store(true)
			// Wait a bit and check if cron is also running
			time.Sleep(30 * time.Millisecond)
			if cronStarted.Load() {
				bothRunning.Store(true)
			}
			time.Sleep(30 * time.Millisecond)
			return nil, nil
		}, nil)
	}()

	// Cron lane task
	go func() {
		defer wg.Done()
		_, _ = q.EnqueueInLane(context.Background(), "cron", func(ctx context.Context) (any, error) {
			cronStarted.Store(true)
			// Wait a bit and check if main is also running
			time.Sleep(30 * time.Millisecond)
			if mainStarted.Load() {
				bothRunning.Store(true)
			}
			time.Sleep(30 * time.Millisecond)
			return nil, nil
		}, nil)
	}()

	wg.Wait()

	if !bothRunning.Load() {
		t.Error("expected main and cron lanes to run concurrently")
	}
}

func TestCommandQueue_ReturnValues(t *testing.T) {
	q := NewCommandQueue()

	result, err := q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
		return "hello", nil
	}, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}
}

func TestCommandQueue_ErrorHandling(t *testing.T) {
	q := NewCommandQueue()
	testErr := errors.New("test error")

	result, err := q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
		return nil, testErr
	}, nil)

	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestCommandQueue_ContextCancellation(t *testing.T) {
	q := NewCommandQueue()

	// Start a long-running task
	go func() {
		_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
			time.Sleep(500 * time.Millisecond)
			return nil, nil
		}, nil)
	}()

	// Wait for first task to start
	time.Sleep(10 * time.Millisecond)

	// Try to enqueue with a context that will be cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := q.Enqueue(ctx, func(ctx context.Context) (any, error) {
		return "should not run", nil
	}, nil)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestCommandQueue_QueueSize(t *testing.T) {
	q := NewCommandQueue()

	// Block the main lane
	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
			close(started)
			<-done
			return nil, nil
		}, nil)
	}()

	<-started

	// Enqueue more tasks
	for i := 0; i < 3; i++ {
		go func() {
			_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
				return nil, nil
			}, nil)
		}()
	}

	// Wait for tasks to be queued
	time.Sleep(20 * time.Millisecond)

	size := q.QueueSize("main")
	if size != 4 { // 1 active + 3 pending
		t.Errorf("expected queue size 4, got %d", size)
	}

	total := q.TotalQueueSize()
	if total != 4 {
		t.Errorf("expected total queue size 4, got %d", total)
	}

	close(done)
}

func TestCommandQueue_ClearLane(t *testing.T) {
	q := NewCommandQueue()

	// Block the main lane
	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
			close(started)
			<-done
			return nil, nil
		}, nil)
	}()

	<-started

	// Enqueue more tasks
	for i := 0; i < 3; i++ {
		go func() {
			_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
				return nil, nil
			}, nil)
		}()
	}

	time.Sleep(20 * time.Millisecond)

	removed := q.ClearLane("main")
	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}

	// Only the active task should remain
	if q.QueueSize("main") != 1 {
		t.Errorf("expected queue size 1 after clear, got %d", q.QueueSize("main"))
	}

	close(done)
}

func TestCommandQueue_WaitCallback(t *testing.T) {
	q := NewCommandQueue()

	// Block the main lane
	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
			close(started)
			<-done
			return nil, nil
		}, nil)
	}()

	<-started

	var callbackCalled atomic.Bool
	var waitedDuration time.Duration

	go func() {
		_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
			return nil, nil
		}, &QueueOptions{
			WarnAfter: 10 * time.Millisecond,
			OnWait: func(waited time.Duration, queueLen int) {
				callbackCalled.Store(true)
				waitedDuration = waited
			},
		})
	}()

	// Wait longer than WarnAfter
	time.Sleep(50 * time.Millisecond)
	close(done)

	// Give time for task to complete
	time.Sleep(20 * time.Millisecond)

	if !callbackCalled.Load() {
		t.Error("expected wait callback to be called")
	}
	if waitedDuration < 10*time.Millisecond {
		t.Errorf("expected waited duration >= 10ms, got %v", waitedDuration)
	}
}

func TestCommandQueue_Stats(t *testing.T) {
	q := NewCommandQueue()
	q.SetLaneConcurrency("cron", 2)

	stats := q.Stats()
	if len(stats) != 1 {
		t.Errorf("expected 1 lane in stats (only cron set), got %d", len(stats))
	}

	// Find the cron lane
	var cronStats *LaneStats
	for i := range stats {
		if stats[i].Name == "cron" {
			cronStats = &stats[i]
			break
		}
	}

	if cronStats == nil {
		t.Fatal("cron lane not found in stats")
	}

	if cronStats.MaxConcurrent != 2 {
		t.Errorf("expected cron max concurrent 2, got %d", cronStats.MaxConcurrent)
	}

	// Now use main lane
	_, _ = q.Enqueue(context.Background(), func(ctx context.Context) (any, error) {
		return nil, nil
	}, nil)

	stats = q.Stats()
	if len(stats) != 2 {
		t.Errorf("expected 2 lanes after using main, got %d", len(stats))
	}
}

func TestCommandQueue_EnqueueVoid(t *testing.T) {
	q := NewCommandQueue()

	var called atomic.Bool
	err := q.EnqueueVoid(context.Background(), func(ctx context.Context) error {
		called.Store(true)
		return nil
	}, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called.Load() {
		t.Error("expected task to be called")
	}
}

func TestDefaultQueue(t *testing.T) {
	result, err := Enqueue(context.Background(), func(ctx context.Context) (any, error) {
		return 42, nil
	}, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %v", result)
	}
}
