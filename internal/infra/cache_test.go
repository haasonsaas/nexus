package infra

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTTLCache_SetGet(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key1", 100)
	cache.Set("key2", 200)

	val, ok := cache.Get("key1")
	if !ok || val != 100 {
		t.Errorf("expected 100, got %d (ok=%v)", val, ok)
	}

	val, ok = cache.Get("key2")
	if !ok || val != 200 {
		t.Errorf("expected 200, got %d (ok=%v)", val, ok)
	}

	_, ok = cache.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent key to return false")
	}
}

func TestTTLCache_Expiration(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: 50 * time.Millisecond,
	})
	defer cache.Stop()

	cache.Set("key", 42)

	// Should exist immediately
	val, ok := cache.Get("key")
	if !ok || val != 42 {
		t.Errorf("expected 42, got %d (ok=%v)", val, ok)
	}

	// Wait for expiration
	time.Sleep(70 * time.Millisecond)

	_, ok = cache.Get("key")
	if ok {
		t.Error("expected key to be expired")
	}
}

func TestTTLCache_SetWithTTL(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.SetWithTTL("short", 1, 30*time.Millisecond)
	cache.SetWithTTL("long", 2, 200*time.Millisecond)

	time.Sleep(50 * time.Millisecond)

	// Short should be expired
	_, ok := cache.Get("short")
	if ok {
		t.Error("expected short key to be expired")
	}

	// Long should still exist
	val, ok := cache.Get("long")
	if !ok || val != 2 {
		t.Errorf("expected 2, got %d (ok=%v)", val, ok)
	}
}

func TestTTLCache_GetOrSet(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	callCount := 0
	create := func() int {
		callCount++
		return 42
	}

	// First call should invoke create
	val := cache.GetOrSet("key", create)
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
	if callCount != 1 {
		t.Errorf("expected create called once, called %d times", callCount)
	}

	// Second call should return cached value
	val = cache.GetOrSet("key", create)
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
	if callCount != 1 {
		t.Errorf("expected create still called once, called %d times", callCount)
	}
}

func TestTTLCache_Delete(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key", 100)
	cache.Delete("key")

	_, ok := cache.Get("key")
	if ok {
		t.Error("expected key to be deleted")
	}
}

func TestTTLCache_Clear(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key1", 1)
	cache.Set("key2", 2)
	cache.Set("key3", 3)

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected 0 entries, got %d", cache.Len())
	}
}

func TestTTLCache_Contains(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: 50 * time.Millisecond,
	})
	defer cache.Stop()

	cache.Set("key", 100)

	if !cache.Contains("key") {
		t.Error("expected Contains to return true")
	}

	if cache.Contains("nonexistent") {
		t.Error("expected Contains to return false for nonexistent key")
	}

	time.Sleep(70 * time.Millisecond)

	if cache.Contains("key") {
		t.Error("expected Contains to return false for expired key")
	}
}

func TestTTLCache_TTL(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: 100 * time.Millisecond,
	})
	defer cache.Stop()

	cache.Set("key", 100)

	ttl := cache.TTL("key")
	if ttl <= 0 || ttl > 100*time.Millisecond {
		t.Errorf("unexpected TTL: %v", ttl)
	}

	// Nonexistent key
	if cache.TTL("nonexistent") != 0 {
		t.Error("expected 0 TTL for nonexistent key")
	}

	// Expired key
	time.Sleep(120 * time.Millisecond)
	if cache.TTL("key") != 0 {
		t.Error("expected 0 TTL for expired key")
	}
}

func TestTTLCache_Refresh(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: 50 * time.Millisecond,
	})
	defer cache.Stop()

	cache.Set("key", 100)
	time.Sleep(30 * time.Millisecond)

	// Refresh should extend TTL
	if !cache.Refresh("key", 100*time.Millisecond) {
		t.Error("expected Refresh to succeed")
	}

	// Should still exist after original TTL
	time.Sleep(40 * time.Millisecond)
	if !cache.Contains("key") {
		t.Error("expected key to still exist after refresh")
	}

	// Refresh nonexistent key should fail
	if cache.Refresh("nonexistent", time.Minute) {
		t.Error("expected Refresh to fail for nonexistent key")
	}
}

func TestTTLCache_Keys(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key1", 1)
	cache.Set("key2", 2)
	cache.SetWithTTL("key3", 3, 10*time.Millisecond)

	keys := cache.Keys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}

	// Wait for key3 to expire
	time.Sleep(20 * time.Millisecond)

	keys = cache.Keys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys after expiration, got %d", len(keys))
	}
}

func TestTTLCache_Stats(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key", 100)

	// Hit
	cache.Get("key")
	cache.Get("key")

	// Miss
	cache.Get("nonexistent")

	stats := cache.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Size != 1 {
		t.Errorf("expected size 1, got %d", stats.Size)
	}
}

func TestTTLCache_MaxSize(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
		MaxSize:    3,
	})
	defer cache.Stop()

	cache.Set("key1", 1)
	time.Sleep(time.Millisecond) // Ensure different timestamps
	cache.Set("key2", 2)
	time.Sleep(time.Millisecond)
	cache.Set("key3", 3)
	time.Sleep(time.Millisecond)

	// Adding 4th should evict oldest
	cache.Set("key4", 4)

	if cache.Len() != 3 {
		t.Errorf("expected 3 entries, got %d", cache.Len())
	}

	// key1 should be evicted (oldest)
	if cache.Contains("key1") {
		t.Error("expected key1 to be evicted")
	}

	// Others should exist
	if !cache.Contains("key2") || !cache.Contains("key3") || !cache.Contains("key4") {
		t.Error("expected key2, key3, key4 to exist")
	}
}

func TestTTLCache_Cleanup(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL: 30 * time.Millisecond,
	})
	defer cache.Stop()

	cache.Set("key1", 1)
	cache.Set("key2", 2)
	cache.SetWithTTL("key3", 3, time.Minute) // Long-lived

	time.Sleep(50 * time.Millisecond)

	removed := cache.Cleanup()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	if cache.Len() != 1 {
		t.Errorf("expected 1 entry remaining, got %d", cache.Len())
	}
}

func TestTTLCache_AutoCleanup(t *testing.T) {
	cache := NewTTLCache[string, int](CacheConfig{
		DefaultTTL:      30 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
	})
	defer cache.Stop()

	cache.Set("key1", 1)
	cache.Set("key2", 2)

	// Wait for entries to expire and cleanup to run
	time.Sleep(120 * time.Millisecond)

	// Entries should be cleaned up
	if cache.Len() != 0 {
		t.Errorf("expected 0 entries after auto cleanup, got %d", cache.Len())
	}
}

func TestTTLCache_ConcurrentAccess(t *testing.T) {
	cache := NewTTLCache[int, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache.Set(i, i*2)
			cache.Get(i)
			cache.Delete(i % 50)
		}(i)
	}
	wg.Wait()
}

func TestAsyncTTLCache_Basic(t *testing.T) {
	cache := NewAsyncTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	var loadCount int32
	loader := func(key string) (int, error) {
		atomic.AddInt32(&loadCount, 1)
		return 42, nil
	}

	// First call should invoke loader
	val, err := cache.Get("key", loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
	if atomic.LoadInt32(&loadCount) != 1 {
		t.Errorf("expected loader called once, called %d times", loadCount)
	}

	// Second call should return cached value
	val, err = cache.Get("key", loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
	if atomic.LoadInt32(&loadCount) != 1 {
		t.Errorf("expected loader still called once, called %d times", loadCount)
	}
}

func TestAsyncTTLCache_LoaderError(t *testing.T) {
	cache := NewAsyncTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	testErr := errors.New("load failed")
	loader := func(_ string) (int, error) {
		return 0, testErr
	}

	_, err := cache.Get("key", loader)
	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}

	// Should not cache failed loads
	if cache.Stats().Size != 0 {
		t.Error("expected no cached entry after failed load")
	}
}

func TestAsyncTTLCache_PreventThunderingHerd(t *testing.T) {
	cache := NewAsyncTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	var loadCount int32
	loader := func(_ string) (int, error) {
		atomic.AddInt32(&loadCount, 1)
		time.Sleep(50 * time.Millisecond) // Simulate slow load
		return 42, nil
	}

	// Launch multiple concurrent requests with a barrier to ensure
	// all goroutines start at approximately the same time
	const numRequests = 10
	var wg sync.WaitGroup
	var readyWg sync.WaitGroup
	start := make(chan struct{})
	results := make([]int, numRequests)
	errs := make([]error, numRequests)

	readyWg.Add(numRequests)
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			readyWg.Done() // Signal that this goroutine is ready
			<-start        // Wait for the start signal
			results[idx], errs[idx] = cache.Get("key", loader)
		}(i)
	}

	readyWg.Wait() // Wait for all goroutines to be ready
	close(start)   // Start all goroutines at once
	wg.Wait()

	// All should succeed with same value
	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d failed: %v", i, err)
		}
		if results[i] != 42 {
			t.Errorf("request %d got wrong value: %d", i, results[i])
		}
	}

	// Loader should only be called once (thundering herd prevented)
	if count := atomic.LoadInt32(&loadCount); count != 1 {
		t.Errorf("expected loader called once, called %d times", count)
	}
}

func TestAsyncTTLCache_SetDelete(t *testing.T) {
	cache := NewAsyncTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key", 100)

	loader := func(_ string) (int, error) {
		return 999, nil // Should not be called
	}

	val, err := cache.Get("key", loader)
	if err != nil || val != 100 {
		t.Errorf("expected 100, got %d (err=%v)", val, err)
	}

	cache.Delete("key")

	val, err = cache.Get("key", loader)
	if err != nil || val != 999 {
		t.Errorf("expected 999 after delete, got %d (err=%v)", val, err)
	}
}

func TestAsyncTTLCache_Clear(t *testing.T) {
	cache := NewAsyncTTLCache[string, int](CacheConfig{
		DefaultTTL: time.Minute,
	})
	defer cache.Stop()

	cache.Set("key1", 1)
	cache.Set("key2", 2)

	cache.Clear()

	if cache.Stats().Size != 0 {
		t.Errorf("expected 0 entries after clear, got %d", cache.Stats().Size)
	}
}
