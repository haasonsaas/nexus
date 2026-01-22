package infra

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := NewSemaphore(3)

	if sem.Available() != 3 {
		t.Errorf("expected 3 available, got %d", sem.Available())
	}

	// Acquire 2 permits
	err := sem.Acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sem.Available() != 1 {
		t.Errorf("expected 1 available, got %d", sem.Available())
	}
	if sem.InUse() != 2 {
		t.Errorf("expected 2 in use, got %d", sem.InUse())
	}

	// Acquire 1 more permit
	err = sem.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sem.Available() != 0 {
		t.Errorf("expected 0 available, got %d", sem.Available())
	}

	// Release 1 permit
	sem.Release(1)
	if sem.Available() != 1 {
		t.Errorf("expected 1 available after release, got %d", sem.Available())
	}

	// Release remaining
	sem.Release(2)
	if sem.Available() != 3 {
		t.Errorf("expected 3 available after all releases, got %d", sem.Available())
	}
}

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := NewSemaphore(2)

	// Should succeed
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire to succeed")
	}

	if !sem.TryAcquire(1) {
		t.Error("expected second TryAcquire to succeed")
	}

	// Should fail - no permits available
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire to fail when full")
	}

	// Release and try again
	sem.Release(1)
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire to succeed after release")
	}
}

func TestSemaphore_ContextCancellation(t *testing.T) {
	sem := NewSemaphore(1)

	// Acquire the only permit
	err := sem.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to acquire with a cancelled context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error)
	go func() {
		errCh <- sem.Acquire(ctx, 1)
	}()

	err = <-errCh
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestSemaphore_AcquireWithTimeout(t *testing.T) {
	sem := NewSemaphore(1)
	sem.Acquire(context.Background(), 1) // Take the permit

	start := time.Now()
	err := sem.AcquireWithTimeout(1, 50*time.Millisecond)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	if elapsed < 40*time.Millisecond {
		t.Errorf("expected to wait ~50ms, waited %v", elapsed)
	}
}

func TestSemaphore_ConcurrentAccess(t *testing.T) {
	sem := NewSemaphore(5)
	var maxConcurrent int32
	var current int32

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := sem.Acquire(context.Background(), 1)
			if err != nil {
				return
			}
			defer sem.Release(1)

			c := atomic.AddInt32(&current, 1)
			// Track max concurrent
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&current, -1)
		}()
	}

	wg.Wait()

	if maxConcurrent > 5 {
		t.Errorf("max concurrent exceeded limit: %d > 5", maxConcurrent)
	}
}

func TestSemaphore_WeightedAcquire(t *testing.T) {
	sem := NewSemaphore(10)

	// Acquire 7 permits
	err := sem.Acquire(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sem.Available() != 3 {
		t.Errorf("expected 3 available, got %d", sem.Available())
	}

	// Should fail to acquire 5 more (only 3 available)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err = sem.Acquire(ctx, 5)
	if err != context.DeadlineExceeded {
		t.Errorf("expected timeout trying to acquire 5, got %v", err)
	}

	// Should succeed acquiring 3
	err = sem.Acquire(context.Background(), 3)
	if err != nil {
		t.Fatalf("unexpected error acquiring 3: %v", err)
	}

	if sem.Available() != 0 {
		t.Errorf("expected 0 available, got %d", sem.Available())
	}
}

func TestSemaphore_ZeroAndNegative(t *testing.T) {
	sem := NewSemaphore(5)

	// Zero acquire should succeed immediately
	err := sem.Acquire(context.Background(), 0)
	if err != nil {
		t.Errorf("zero acquire should succeed: %v", err)
	}
	if sem.InUse() != 0 {
		t.Errorf("zero acquire should not use permits, got %d", sem.InUse())
	}

	// Negative acquire should succeed immediately
	err = sem.Acquire(context.Background(), -1)
	if err != nil {
		t.Errorf("negative acquire should succeed: %v", err)
	}

	// Zero release should be safe
	sem.Acquire(context.Background(), 3)
	sem.Release(0)
	if sem.InUse() != 3 {
		t.Errorf("zero release should not change in-use, got %d", sem.InUse())
	}

	// Negative release should be safe
	sem.Release(-1)
	if sem.InUse() != 3 {
		t.Errorf("negative release should not change in-use, got %d", sem.InUse())
	}
}

func TestSemaphore_Stats(t *testing.T) {
	sem := NewSemaphore(10)

	// Perform some operations
	sem.Acquire(context.Background(), 3)
	sem.Acquire(context.Background(), 2)
	sem.Release(1)

	stats := sem.Stats()

	if stats.Max != 10 {
		t.Errorf("expected max 10, got %d", stats.Max)
	}
	if stats.InUse != 4 { // 3 + 2 - 1
		t.Errorf("expected in-use 4, got %d", stats.InUse)
	}
	if stats.Available != 6 {
		t.Errorf("expected available 6, got %d", stats.Available)
	}
	if stats.Acquired != 2 {
		t.Errorf("expected acquired 2, got %d", stats.Acquired)
	}
	if stats.Released != 1 {
		t.Errorf("expected released 1, got %d", stats.Released)
	}
}

func TestSemaphore_WaitersCount(t *testing.T) {
	sem := NewSemaphore(1)
	sem.Acquire(context.Background(), 1) // Take the only permit

	var wg sync.WaitGroup
	started := make(chan struct{})

	// Start multiple waiters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started <- struct{}{}
			sem.Acquire(context.Background(), 1)
			sem.Release(1)
		}()
	}

	// Wait for goroutines to start waiting
	for i := 0; i < 3; i++ {
		<-started
	}
	time.Sleep(10 * time.Millisecond)

	waiters := sem.Waiters()
	if waiters != 3 {
		t.Errorf("expected 3 waiters, got %d", waiters)
	}

	// Release the permit - waiters should decrease
	sem.Release(1)
	wg.Wait()

	if sem.Waiters() != 0 {
		t.Errorf("expected 0 waiters after completion, got %d", sem.Waiters())
	}
}

func TestSemaphorePool_Basic(t *testing.T) {
	pool := NewSemaphorePool(5)

	// Get creates a new semaphore
	sem1 := pool.Get("resource1")
	if sem1.Available() != 5 {
		t.Errorf("expected 5 available, got %d", sem1.Available())
	}

	// Get returns the same semaphore
	sem2 := pool.Get("resource1")
	if sem1 != sem2 {
		t.Error("expected same semaphore instance")
	}

	// Different name creates new semaphore
	sem3 := pool.Get("resource2")
	if sem1 == sem3 {
		t.Error("expected different semaphore instance for different name")
	}
}

func TestSemaphorePool_GetOrCreate(t *testing.T) {
	pool := NewSemaphorePool(5)

	// Create with custom max
	sem1 := pool.GetOrCreate("custom", 20)
	if sem1.Available() != 20 {
		t.Errorf("expected 20 available, got %d", sem1.Available())
	}

	// GetOrCreate returns existing semaphore (doesn't change max)
	sem2 := pool.GetOrCreate("custom", 100)
	if sem2.Available() != 20 {
		t.Errorf("expected still 20 available, got %d", sem2.Available())
	}
}

func TestSemaphorePool_AcquireRelease(t *testing.T) {
	pool := NewSemaphorePool(3)

	err := pool.Acquire(context.Background(), "api", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := pool.Stats()
	if apiStats, ok := stats["api"]; !ok {
		t.Error("expected api semaphore in stats")
	} else if apiStats.InUse != 2 {
		t.Errorf("expected 2 in use, got %d", apiStats.InUse)
	}

	pool.Release("api", 2)

	stats = pool.Stats()
	if apiStats := stats["api"]; apiStats.InUse != 0 {
		t.Errorf("expected 0 in use after release, got %d", apiStats.InUse)
	}
}

func TestSemaphorePool_ReleaseMissing(t *testing.T) {
	pool := NewSemaphorePool(5)

	// Release on non-existent semaphore should not panic
	pool.Release("nonexistent", 1)
}

func TestSemaphorePool_ConcurrentAccess(t *testing.T) {
	pool := NewSemaphorePool(3)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "resource"
			if i%2 == 0 {
				name = "other"
			}

			err := pool.Acquire(context.Background(), name, 1)
			if err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
			pool.Release(name, 1)
		}(i)
	}

	wg.Wait()
}

func TestNewSemaphore_InvalidMax(t *testing.T) {
	// Zero max should default to 1
	sem := NewSemaphore(0)
	if sem.Available() != 1 {
		t.Errorf("expected 1 available for zero max, got %d", sem.Available())
	}

	// Negative max should default to 1
	sem = NewSemaphore(-5)
	if sem.Available() != 1 {
		t.Errorf("expected 1 available for negative max, got %d", sem.Available())
	}
}

func TestSemaphore_AcquireMoreThanMax(t *testing.T) {
	sem := NewSemaphore(5)

	// Acquiring more than max should be capped
	err := sem.Acquire(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have used max (5) permits, not 10
	if sem.InUse() != 5 {
		t.Errorf("expected 5 in use (capped), got %d", sem.InUse())
	}
}

func TestSemaphore_ReleaseMoreThanAcquired(t *testing.T) {
	sem := NewSemaphore(5)
	sem.Acquire(context.Background(), 2)

	// Release more than acquired should not go negative
	sem.Release(10)
	if sem.InUse() != 0 {
		t.Errorf("expected 0 in use, got %d", sem.InUse())
	}
	if sem.Available() != 5 {
		t.Errorf("expected 5 available (max), got %d", sem.Available())
	}
}
