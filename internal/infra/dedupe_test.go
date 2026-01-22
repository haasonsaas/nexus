package infra

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDedupeCache_IsDuplicate(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0, // Disable background cleanup
	})

	// First call should not be a duplicate
	if cache.IsDuplicate("key1", "value1") {
		t.Error("first call should not be a duplicate")
	}

	// Second call with same key should be a duplicate
	if !cache.IsDuplicate("key1", "value2") {
		t.Error("second call should be a duplicate")
	}

	// Different key should not be a duplicate
	if cache.IsDuplicate("key2", "value3") {
		t.Error("different key should not be a duplicate")
	}
}

func TestDedupeCache_TTLExpiry(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             50 * time.Millisecond,
		CleanupInterval: 0,
	})

	cache.IsDuplicate("key1", "value1")

	// Should be duplicate immediately
	if !cache.IsDuplicate("key1", "value2") {
		t.Error("should be duplicate before expiry")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Should not be duplicate after expiry
	if cache.IsDuplicate("key1", "value3") {
		t.Error("should not be duplicate after expiry")
	}
}

func TestDedupeCache_Check(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	// Check non-existent key
	if cache.Check("key1") {
		t.Error("non-existent key should return false")
	}

	// Add key
	cache.Add("key1", "value1")

	// Check existing key
	if !cache.Check("key1") {
		t.Error("existing key should return true")
	}

	// Check doesn't add the key
	if cache.Check("key2") {
		t.Error("Check should not add key")
	}
}

func TestDedupeCache_Get(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	// Get non-existent
	if _, ok := cache.Get("key1"); ok {
		t.Error("non-existent key should return false")
	}

	// Add and get
	cache.Add("key1", "value1")
	val, ok := cache.Get("key1")
	if !ok {
		t.Error("existing key should return true")
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got %v", val)
	}
}

func TestDedupeCache_Delete(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	cache.Add("key1", "value1")
	if !cache.Check("key1") {
		t.Error("key should exist after add")
	}

	cache.Delete("key1")
	if cache.Check("key1") {
		t.Error("key should not exist after delete")
	}
}

func TestDedupeCache_Size(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	if cache.Size() != 0 {
		t.Errorf("expected size 0, got %d", cache.Size())
	}

	cache.Add("key1", "value1")
	cache.Add("key2", "value2")

	if cache.Size() != 2 {
		t.Errorf("expected size 2, got %d", cache.Size())
	}
}

func TestDedupeCache_Clear(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	cache.Add("key1", "value1")
	cache.Add("key2", "value2")
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", cache.Size())
	}
}

func TestDedupeCache_Cleanup(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             50 * time.Millisecond,
		CleanupInterval: 0,
	})

	cache.Add("key1", "value1")
	cache.Add("key2", "value2")

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Add a fresh key
	cache.Add("key3", "value3")

	// Cleanup should remove expired entries
	removed := cache.Cleanup()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	if cache.Size() != 1 {
		t.Errorf("expected 1 remaining, got %d", cache.Size())
	}
}

func TestDedupeCache_MaxSize(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		MaxSize:         3,
		CleanupInterval: 0,
	})

	cache.Add("key1", "value1")
	time.Sleep(10 * time.Millisecond)
	cache.Add("key2", "value2")
	time.Sleep(10 * time.Millisecond)
	cache.Add("key3", "value3")

	if cache.Size() != 3 {
		t.Errorf("expected size 3, got %d", cache.Size())
	}

	// Adding 4th should evict oldest (key1)
	cache.Add("key4", "value4")

	if cache.Size() != 3 {
		t.Errorf("expected size 3 after eviction, got %d", cache.Size())
	}

	// key1 should be evicted
	if cache.Check("key1") {
		t.Error("key1 should have been evicted")
	}

	// key4 should exist
	if !cache.Check("key4") {
		t.Error("key4 should exist")
	}
}

func TestDedupeCache_OnEvict(t *testing.T) {
	var evicted []string
	var mu sync.Mutex

	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
		OnEvict: func(key string) {
			mu.Lock()
			evicted = append(evicted, key)
			mu.Unlock()
		},
	})

	cache.Add("key1", "value1")
	cache.Delete("key1")

	mu.Lock()
	if len(evicted) != 1 || evicted[0] != "key1" {
		t.Errorf("expected evicted ['key1'], got %v", evicted)
	}
	mu.Unlock()
}

func TestDedupeCache_Concurrent(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	var wg sync.WaitGroup
	var duplicates atomic.Int32

	// Spawn many goroutines trying to add same key
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cache.IsDuplicate("shared-key", "value") {
				duplicates.Add(1)
			}
		}()
	}

	wg.Wait()

	// 99 should be duplicates (first one adds, rest are duplicates)
	if duplicates.Load() != 99 {
		t.Errorf("expected 99 duplicates, got %d", duplicates.Load())
	}
}

func TestDedupeFunc(t *testing.T) {
	cache := NewDedupeCache(&DedupeCacheConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 0,
	})

	var calls atomic.Int32
	fn := DedupeFunc(cache, func(s string) string {
		return s // key is the input itself
	}, func(s string) (string, error) {
		calls.Add(1)
		return "result:" + s, nil
	})

	// First call should execute
	result, err := fn("input1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "result:input1" {
		t.Errorf("expected 'result:input1', got %q", result)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", calls.Load())
	}

	// Second call should be cached
	result, err = fn("input1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "result:input1" {
		t.Errorf("expected cached 'result:input1', got %q", result)
	}
	if calls.Load() != 1 {
		t.Errorf("expected still 1 call (cached), got %d", calls.Load())
	}

	// Different input should execute
	result, err = fn("input2")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "result:input2" {
		t.Errorf("expected 'result:input2', got %q", result)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", calls.Load())
	}
}

func TestMessageDeduper(t *testing.T) {
	deduper := NewMessageDeduper(100 * time.Millisecond)

	// First message should not be duplicate
	if deduper.IsDuplicate("msg-1") {
		t.Error("first message should not be duplicate")
	}

	// Same message should be duplicate
	if !deduper.IsDuplicate("msg-1") {
		t.Error("same message should be duplicate")
	}

	// Different message should not be duplicate
	if deduper.IsDuplicate("msg-2") {
		t.Error("different message should not be duplicate")
	}

	if deduper.Size() != 2 {
		t.Errorf("expected 2 messages tracked, got %d", deduper.Size())
	}
}

func TestMessageDeduper_Mark(t *testing.T) {
	deduper := NewMessageDeduper(100 * time.Millisecond)

	// Mark message as seen
	deduper.Mark("msg-1")

	// Should now be duplicate
	if !deduper.IsDuplicate("msg-1") {
		t.Error("marked message should be duplicate")
	}
}

func TestDefaultDedupeCacheConfig(t *testing.T) {
	cfg := DefaultDedupeCacheConfig()

	if cfg.TTL != 5*time.Minute {
		t.Errorf("expected TTL 5m, got %v", cfg.TTL)
	}
	if cfg.MaxSize != 10000 {
		t.Errorf("expected MaxSize 10000, got %d", cfg.MaxSize)
	}
	if cfg.CleanupInterval != time.Minute {
		t.Errorf("expected CleanupInterval 1m, got %v", cfg.CleanupInterval)
	}
}

func TestDedupeCache_NilConfig(t *testing.T) {
	// Should not panic with nil config
	cache := NewDedupeCache(nil)

	cache.Add("key1", "value1")
	if !cache.Check("key1") {
		t.Error("cache should work with nil config")
	}
}
