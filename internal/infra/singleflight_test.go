package infra

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroup_Do(t *testing.T) {
	var g Group[string, int]

	val, err, shared := g.Do("key", func() (int, error) {
		return 42, nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
	if shared {
		t.Error("expected shared=false for single call")
	}
}

func TestGroup_DoError(t *testing.T) {
	var g Group[string, int]
	testErr := errors.New("test error")

	val, err, _ := g.Do("key", func() (int, error) {
		return 0, testErr
	})

	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}
	if val != 0 {
		t.Errorf("expected 0, got %d", val)
	}
}

func TestGroup_DoDuplicates(t *testing.T) {
	var g Group[string, int]
	var callCount int32

	var wg sync.WaitGroup
	results := make([]int, 10)
	shared := make([]bool, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			val, _, sh := g.Do("key", func() (int, error) {
				atomic.AddInt32(&callCount, 1)
				time.Sleep(50 * time.Millisecond)
				return 42, nil
			})
			results[idx] = val
			shared[idx] = sh
		}(i)
	}

	wg.Wait()

	// Function should only be called once
	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Errorf("expected 1 call, got %d", count)
	}

	// All should get the same result
	for i, val := range results {
		if val != 42 {
			t.Errorf("results[%d] = %d, want 42", i, val)
		}
	}

	// At least some should be shared
	sharedCount := 0
	for _, sh := range shared {
		if sh {
			sharedCount++
		}
	}
	if sharedCount < 9 {
		t.Errorf("expected at least 9 shared, got %d", sharedCount)
	}
}

func TestGroup_DoDifferentKeys(t *testing.T) {
	var g Group[string, int]
	var callCount int32

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i))
			g.Do(key, func() (int, error) {
				atomic.AddInt32(&callCount, 1)
				time.Sleep(30 * time.Millisecond)
				return i, nil
			})
		}(i)
	}

	wg.Wait()

	// Each key should have its own call
	if count := atomic.LoadInt32(&callCount); count != 3 {
		t.Errorf("expected 3 calls for different keys, got %d", count)
	}
}

func TestGroup_DoChan(t *testing.T) {
	var g Group[string, int]
	var callCount int32
	started := make(chan struct{})

	ch1 := g.DoChan("key", func() (int, error) {
		atomic.AddInt32(&callCount, 1)
		close(started) // Signal that we've started
		time.Sleep(50 * time.Millisecond)
		return 42, nil
	})

	// Wait for first call to start before launching second
	<-started

	ch2 := g.DoChan("key", func() (int, error) {
		atomic.AddInt32(&callCount, 1)
		return 99, nil
	})

	r1 := <-ch1
	r2 := <-ch2

	if r1.Val != 42 || r2.Val != 42 {
		t.Errorf("expected both to get 42, got %d and %d", r1.Val, r2.Val)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second one should be shared
	if !r2.Shared {
		t.Error("expected second result to be shared")
	}
}

func TestGroup_Forget(t *testing.T) {
	var g Group[string, int]
	var callCount int32

	// First call
	g.Do("key", func() (int, error) {
		atomic.AddInt32(&callCount, 1)
		return 1, nil
	})

	// Forget the key
	g.Forget("key")

	// Second call should execute again
	g.Do("key", func() (int, error) {
		atomic.AddInt32(&callCount, 1)
		return 2, nil
	})

	if count := atomic.LoadInt32(&callCount); count != 2 {
		t.Errorf("expected 2 calls after Forget, got %d", count)
	}
}

func TestGroup_Stats(t *testing.T) {
	var g Group[string, int]

	// First call - miss
	g.Do("key", func() (int, error) {
		time.Sleep(30 * time.Millisecond)
		return 42, nil
	})

	// Concurrent calls - should share
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Do("key2", func() (int, error) {
				time.Sleep(30 * time.Millisecond)
				return 42, nil
			})
		}()
	}
	wg.Wait()

	stats := g.Stats()

	// 2 misses (key and key2)
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}

	// 4 hits (4 of the 5 key2 calls shared)
	if stats.Hits != 4 {
		t.Errorf("expected 4 hits, got %d", stats.Hits)
	}
}

func TestGroupStats_HitRate(t *testing.T) {
	tests := []struct {
		hits, misses uint64
		expected     float64
	}{
		{0, 0, 0.0},
		{5, 5, 0.5},
		{10, 0, 1.0},
		{0, 10, 0.0},
		{3, 7, 0.3},
	}

	for _, tt := range tests {
		stats := GroupStats{Hits: tt.hits, Misses: tt.misses}
		got := stats.HitRate()
		if got != tt.expected {
			t.Errorf("HitRate(%d, %d) = %f, want %f", tt.hits, tt.misses, got, tt.expected)
		}
	}
}

func TestCoalescer_Get(t *testing.T) {
	var callCount int32

	c := NewCoalescer(func(key string) (int, error) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(30 * time.Millisecond)
		return len(key), nil
	})

	var wg sync.WaitGroup
	results := make([]int, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			val, err := c.Get("hello")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			results[idx] = val
		}(i)
	}

	wg.Wait()

	// Function should only be called once
	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Errorf("expected 1 call, got %d", count)
	}

	// All should get same result
	for i, val := range results {
		if val != 5 {
			t.Errorf("results[%d] = %d, want 5", i, val)
		}
	}
}

func TestCoalescer_Stats(t *testing.T) {
	c := NewCoalescer(func(key string) (int, error) {
		time.Sleep(30 * time.Millisecond)
		return len(key), nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get("key")
		}()
	}
	wg.Wait()

	stats := c.Stats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Hits != 4 {
		t.Errorf("expected 4 hits, got %d", stats.Hits)
	}
}

func TestGroup_ConcurrentSafety(t *testing.T) {
	var g Group[int, int]

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := i % 10
			g.Do(key, func() (int, error) {
				time.Sleep(time.Millisecond)
				return key * 2, nil
			})
		}(i)
	}

	wg.Wait()
}
