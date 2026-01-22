package infra

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_GetPut(t *testing.T) {
	var created int32
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return int(atomic.AddInt32(&created, 1)), nil
		},
	})
	defer pool.Close()

	// Get a resource
	res, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Value != 1 {
		t.Errorf("expected 1, got %d", res.Value)
	}

	// Return it
	pool.Put(res)

	// Get again - should reuse
	res2, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Value != 1 {
		t.Errorf("expected reused resource (1), got %d", res2.Value)
	}

	// Only 1 should have been created
	if atomic.LoadInt32(&created) != 1 {
		t.Errorf("expected 1 created, got %d", created)
	}
}

func TestPool_MaxSize(t *testing.T) {
	var created int32
	pool := NewPool(PoolConfig[int]{
		MaxSize: 2,
		Factory: func(_ context.Context) (int, error) {
			return int(atomic.AddInt32(&created, 1)), nil
		},
	})
	defer pool.Close()

	// Get 2 resources (max)
	res1, _ := pool.Get(context.Background())
	res2, _ := pool.Get(context.Background())

	// Try to get a 3rd with timeout - should fail
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pool.Get(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected timeout, got %v", err)
	}

	// Return one and try again
	pool.Put(res1)

	res3, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res3.Value != 1 {
		t.Errorf("expected reused resource (1), got %d", res3.Value)
	}

	pool.Put(res2)
	pool.Put(res3)
}

func TestPool_ConcurrentAccess(t *testing.T) {
	var created int32
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return int(atomic.AddInt32(&created, 1)), nil
		},
	})
	defer pool.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := pool.Get(context.Background())
			if err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			pool.Put(res)
		}()
	}

	wg.Wait()

	// Should have created at most MaxSize resources
	if c := atomic.LoadInt32(&created); c > 5 {
		t.Errorf("expected at most 5 created, got %d", c)
	}

	stats := pool.Stats()
	if stats.InUse != 0 {
		t.Errorf("expected 0 in use, got %d", stats.InUse)
	}
}

func TestPool_Discard(t *testing.T) {
	var created int32
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return int(atomic.AddInt32(&created, 1)), nil
		},
	})
	defer pool.Close()

	res, _ := pool.Get(context.Background())
	pool.Discard(res)

	// Get another - should create new
	res2, _ := pool.Get(context.Background())
	if res2.Value != 2 {
		t.Errorf("expected new resource (2), got %d", res2.Value)
	}

	stats := pool.Stats()
	if stats.Destroyed != 1 {
		t.Errorf("expected 1 destroyed, got %d", stats.Destroyed)
	}
}

func TestPool_Validate(t *testing.T) {
	var created int32
	validUntil := int32(2) // First 2 resources are valid

	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return int(atomic.AddInt32(&created, 1)), nil
		},
		Validate: func(v int) bool {
			return v <= int(atomic.LoadInt32(&validUntil))
		},
	})
	defer pool.Close()

	// Get and return 3 resources
	res1, _ := pool.Get(context.Background())
	res2, _ := pool.Get(context.Background())
	res3, _ := pool.Get(context.Background())
	pool.Put(res1)
	pool.Put(res2)
	pool.Put(res3)

	// Now make only res3 invalid
	atomic.StoreInt32(&validUntil, 2)

	// Get should skip invalid res3 and return res2
	res, _ := pool.Get(context.Background())
	if res.Value != 2 {
		t.Errorf("expected 2 (valid), got %d", res.Value)
	}
}

func TestPool_MaxIdleTime(t *testing.T) {
	pool := NewPool(PoolConfig[int]{
		MaxSize:     5,
		MaxIdleTime: 50 * time.Millisecond,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
	})
	defer pool.Close()

	res, _ := pool.Get(context.Background())
	pool.Put(res)

	// Wait for idle timeout
	time.Sleep(70 * time.Millisecond)

	// Get should create new resource (old one expired)
	res2, _ := pool.Get(context.Background())

	stats := pool.Stats()
	if stats.Created != 2 {
		t.Errorf("expected 2 created (one expired), got %d", stats.Created)
	}

	pool.Put(res2)
}

func TestPool_MaxLifetime(t *testing.T) {
	pool := NewPool(PoolConfig[int]{
		MaxSize:     5,
		MaxLifetime: 50 * time.Millisecond,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
	})
	defer pool.Close()

	res, _ := pool.Get(context.Background())

	// Wait for lifetime to expire
	time.Sleep(70 * time.Millisecond)

	// Return - should be discarded due to lifetime
	pool.Put(res)

	// Get should create new resource
	res2, _ := pool.Get(context.Background())

	stats := pool.Stats()
	if stats.Created != 2 {
		t.Errorf("expected 2 created (one exceeded lifetime), got %d", stats.Created)
	}

	pool.Put(res2)
}

func TestPool_Close(t *testing.T) {
	var closed int32
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
		Close: func(_ int) error {
			atomic.AddInt32(&closed, 1)
			return nil
		},
	})

	// Get and return some resources
	res1, _ := pool.Get(context.Background())
	res2, _ := pool.Get(context.Background())
	pool.Put(res1)
	pool.Put(res2)

	// Close the pool
	pool.Close()

	// Should have closed idle resources
	if c := atomic.LoadInt32(&closed); c != 2 {
		t.Errorf("expected 2 closed, got %d", c)
	}

	// Get should fail
	_, err := pool.Get(context.Background())
	if !errors.Is(err, ErrPoolClosed) {
		t.Errorf("expected ErrPoolClosed, got %v", err)
	}
}

func TestPool_Stats(t *testing.T) {
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
	})
	defer pool.Close()

	// Get 3 resources
	res1, _ := pool.Get(context.Background())
	res2, _ := pool.Get(context.Background())
	res3, _ := pool.Get(context.Background())

	stats := pool.Stats()
	if stats.InUse != 3 {
		t.Errorf("expected 3 in use, got %d", stats.InUse)
	}
	if stats.Created != 3 {
		t.Errorf("expected 3 created, got %d", stats.Created)
	}

	// Return 2
	pool.Put(res1)
	pool.Put(res2)

	stats = pool.Stats()
	if stats.InUse != 1 {
		t.Errorf("expected 1 in use, got %d", stats.InUse)
	}
	if stats.Idle != 2 {
		t.Errorf("expected 2 idle, got %d", stats.Idle)
	}

	// Get one (should reuse)
	res4, _ := pool.Get(context.Background())
	_ = res4

	stats = pool.Stats()
	if stats.Reused != 1 {
		t.Errorf("expected 1 reused, got %d", stats.Reused)
	}

	pool.Put(res3)
	pool.Put(res4)
}

func TestPoolStats_ReuseRate(t *testing.T) {
	tests := []struct {
		created, reused uint64
		expected        float64
	}{
		{0, 0, 0.0},
		{5, 5, 0.5},
		{10, 0, 0.0},
		{0, 10, 1.0},
		{3, 7, 0.7},
	}

	for _, tt := range tests {
		stats := PoolStats{Created: tt.created, Reused: tt.reused}
		got := stats.ReuseRate()
		if got != tt.expected {
			t.Errorf("ReuseRate(%d, %d) = %f, want %f", tt.created, tt.reused, got, tt.expected)
		}
	}
}

func TestPool_FactoryError(t *testing.T) {
	testErr := errors.New("factory error")
	var callCount int32

	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			if atomic.AddInt32(&callCount, 1) <= 2 {
				return 0, testErr
			}
			return 42, nil
		},
	})
	defer pool.Close()

	// First two calls should fail
	_, err := pool.Get(context.Background())
	if !errors.Is(err, testErr) {
		t.Errorf("expected factory error, got %v", err)
	}

	_, err = pool.Get(context.Background())
	if !errors.Is(err, testErr) {
		t.Errorf("expected factory error, got %v", err)
	}

	// Third should succeed
	res, err := pool.Get(context.Background())
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	if res.Value != 42 {
		t.Errorf("expected 42, got %d", res.Value)
	}

	pool.Put(res)
}

func TestPool_CleanupIdle(t *testing.T) {
	pool := NewPool(PoolConfig[int]{
		MaxSize:     5,
		MinIdle:     1,
		MaxIdleTime: 30 * time.Millisecond,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
	})
	defer pool.Close()

	// Get and return 3 resources
	res1, _ := pool.Get(context.Background())
	res2, _ := pool.Get(context.Background())
	res3, _ := pool.Get(context.Background())
	pool.Put(res1)
	pool.Put(res2)
	pool.Put(res3)

	// Wait for idle timeout
	time.Sleep(50 * time.Millisecond)

	// Cleanup should remove resources exceeding MinIdle
	removed := pool.CleanupIdle()
	if removed != 2 {
		t.Errorf("expected 2 removed (keep MinIdle=1), got %d", removed)
	}

	stats := pool.Stats()
	if stats.Idle != 1 {
		t.Errorf("expected 1 idle after cleanup, got %d", stats.Idle)
	}
}

func TestPool_PutNil(t *testing.T) {
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
	})
	defer pool.Close()

	// Should not panic
	pool.Put(nil)
}

func TestPool_DiscardNil(t *testing.T) {
	pool := NewPool(PoolConfig[int]{
		MaxSize: 5,
		Factory: func(_ context.Context) (int, error) {
			return 42, nil
		},
	})
	defer pool.Close()

	// Should not panic
	pool.Discard(nil)
}
