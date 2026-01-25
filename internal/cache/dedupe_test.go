package cache

import (
	"sync"
	"testing"
	"time"
)

func TestNewDedupeCache(t *testing.T) {
	t.Run("creates cache with valid options", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		if cache == nil {
			t.Fatal("expected cache to be created")
		}
		if cache.ttl != time.Minute {
			t.Errorf("expected TTL %v, got %v", time.Minute, cache.ttl)
		}
		if cache.maxSize != 100 {
			t.Errorf("expected maxSize 100, got %d", cache.maxSize)
		}
	})

	t.Run("normalizes negative TTL to zero", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     -time.Minute,
			MaxSize: 100,
		})
		if cache.ttl != 0 {
			t.Errorf("expected TTL 0, got %v", cache.ttl)
		}
	})

	t.Run("normalizes negative maxSize to zero", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: -10,
		})
		if cache.maxSize != 0 {
			t.Errorf("expected maxSize 0, got %d", cache.maxSize)
		}
	})
}

func TestDedupeCache_Check(t *testing.T) {
	t.Run("returns false for first occurrence", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		if cache.Check("key1") {
			t.Error("expected false for first occurrence")
		}
	})

	t.Run("returns true for duplicate within TTL", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		cache.Check("key1")
		if !cache.Check("key1") {
			t.Error("expected true for duplicate")
		}
	})

	t.Run("returns false for empty key", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		if cache.Check("") {
			t.Error("expected false for empty key")
		}
		// Empty key should not be stored
		if cache.Size() != 0 {
			t.Error("expected cache to be empty")
		}
	})

	t.Run("returns false after TTL expires", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     100 * time.Millisecond,
			MaxSize: 100,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)

		// Still within TTL
		if !cache.CheckAt("key1", baseTime.Add(50*time.Millisecond)) {
			t.Error("expected true within TTL")
		}

		// After TTL expires
		if cache.CheckAt("key1", baseTime.Add(150*time.Millisecond)) {
			t.Error("expected false after TTL expires")
		}
	})

	t.Run("touch updates timestamp", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     100 * time.Millisecond,
			MaxSize: 100,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)

		// Touch at 50ms
		cache.CheckAt("key1", baseTime.Add(50*time.Millisecond))

		// Should still be valid at 120ms (since touched at 50ms)
		if !cache.CheckAt("key1", baseTime.Add(120*time.Millisecond)) {
			t.Error("expected true after touch extended TTL")
		}
	})
}

func TestDedupeCache_CheckAt(t *testing.T) {
	t.Run("uses explicit timestamp", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})

		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		if cache.CheckAt("key1", baseTime) {
			t.Error("expected false for first occurrence")
		}

		// Same timestamp should be duplicate
		if !cache.CheckAt("key1", baseTime) {
			t.Error("expected true for duplicate at same time")
		}
	})
}

func TestDedupeCache_MaxSize(t *testing.T) {
	t.Run("enforces max size limit", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Hour,
			MaxSize: 3,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)
		cache.CheckAt("key2", baseTime.Add(time.Millisecond))
		cache.CheckAt("key3", baseTime.Add(2*time.Millisecond))
		cache.CheckAt("key4", baseTime.Add(3*time.Millisecond))

		if cache.Size() > 3 {
			t.Errorf("expected size <= 3, got %d", cache.Size())
		}
	})

	t.Run("removes oldest entries on overflow", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Hour,
			MaxSize: 2,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)
		cache.CheckAt("key2", baseTime.Add(time.Millisecond))
		cache.CheckAt("key3", baseTime.Add(2*time.Millisecond))

		// key1 should be evicted (oldest)
		if cache.ContainsAt("key1", baseTime.Add(3*time.Millisecond)) {
			t.Error("expected key1 to be evicted")
		}
		if !cache.ContainsAt("key2", baseTime.Add(3*time.Millisecond)) {
			t.Error("expected key2 to still exist")
		}
		if !cache.ContainsAt("key3", baseTime.Add(3*time.Millisecond)) {
			t.Error("expected key3 to still exist")
		}
	})

	t.Run("zero maxSize clears cache on prune", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Hour,
			MaxSize: 0,
		})

		cache.Check("key1")
		// After Check, prune should clear everything
		if cache.Size() != 0 {
			t.Errorf("expected empty cache with maxSize 0, got %d", cache.Size())
		}
	})
}

func TestDedupeCache_Contains(t *testing.T) {
	t.Run("returns false for non-existent key", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		if cache.Contains("nonexistent") {
			t.Error("expected false for non-existent key")
		}
	})

	t.Run("returns true for existing key", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		cache.Check("key1")
		if !cache.Contains("key1") {
			t.Error("expected true for existing key")
		}
	})

	t.Run("returns false for empty key", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     time.Minute,
			MaxSize: 100,
		})
		if cache.Contains("") {
			t.Error("expected false for empty key")
		}
	})

	t.Run("returns false for expired key", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     100 * time.Millisecond,
			MaxSize: 100,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)

		if !cache.ContainsAt("key1", baseTime.Add(50*time.Millisecond)) {
			t.Error("expected true within TTL")
		}

		if cache.ContainsAt("key1", baseTime.Add(150*time.Millisecond)) {
			t.Error("expected false after TTL expires")
		}
	})

	t.Run("does not update timestamp", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     100 * time.Millisecond,
			MaxSize: 100,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)

		// Contains at 50ms should not update timestamp
		cache.ContainsAt("key1", baseTime.Add(50*time.Millisecond))

		// Should still expire at 100ms (not extended by Contains)
		if cache.ContainsAt("key1", baseTime.Add(110*time.Millisecond)) {
			t.Error("expected false - Contains should not extend TTL")
		}
	})
}

func TestDedupeCache_Clear(t *testing.T) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 100,
	})

	cache.Check("key1")
	cache.Check("key2")
	cache.Check("key3")

	if cache.Size() != 3 {
		t.Fatalf("expected size 3, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", cache.Size())
	}

	// Cleared keys should not be duplicates
	if cache.Check("key1") {
		t.Error("expected false for key after clear")
	}
}

func TestDedupeCache_Size(t *testing.T) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 100,
	})

	if cache.Size() != 0 {
		t.Errorf("expected initial size 0, got %d", cache.Size())
	}

	cache.Check("key1")
	if cache.Size() != 1 {
		t.Errorf("expected size 1, got %d", cache.Size())
	}

	cache.Check("key2")
	if cache.Size() != 2 {
		t.Errorf("expected size 2, got %d", cache.Size())
	}

	// Duplicate should not increase size
	cache.Check("key1")
	if cache.Size() != 2 {
		t.Errorf("expected size 2 after duplicate, got %d", cache.Size())
	}
}

func TestDedupeCache_Remove(t *testing.T) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 100,
	})

	cache.Check("key1")
	cache.Check("key2")

	cache.Remove("key1")

	if cache.Contains("key1") {
		t.Error("expected key1 to be removed")
	}
	if !cache.Contains("key2") {
		t.Error("expected key2 to still exist")
	}

	// Removed key should not be duplicate
	if cache.Check("key1") {
		t.Error("expected false for removed key")
	}
}

func TestDedupeCache_Keys(t *testing.T) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 100,
	})

	cache.Check("key1")
	cache.Check("key2")
	cache.Check("key3")

	keys := cache.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}

	if !keySet["key1"] || !keySet["key2"] || !keySet["key3"] {
		t.Error("expected all keys to be present")
	}
}

func TestDedupeCache_ZeroTTL(t *testing.T) {
	t.Run("zero TTL means infinite", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     0,
			MaxSize: 100,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)

		// Should still be valid after long time
		if !cache.CheckAt("key1", baseTime.Add(24*time.Hour)) {
			t.Error("expected true with zero TTL (infinite)")
		}

		if !cache.ContainsAt("key1", baseTime.Add(24*time.Hour)) {
			t.Error("expected Contains true with zero TTL")
		}
	})
}

func TestDedupeCache_Concurrency(t *testing.T) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 1000,
	})

	var wg sync.WaitGroup
	numGoroutines := 100
	numOps := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := "key" + string(rune(id%26+'a'))
				cache.Check(key)
				cache.Contains(key)
				cache.Size()
			}
		}(i)
	}

	wg.Wait()

	// Should not panic and should have some entries
	if cache.Size() == 0 {
		t.Error("expected some entries after concurrent operations")
	}
}

func TestMessageDedupeKey(t *testing.T) {
	tests := []struct {
		name      string
		channel   string
		messageID string
		expected  string
	}{
		{
			name:      "both channel and messageID",
			channel:   "slack",
			messageID: "msg123",
			expected:  "slack:msg123",
		},
		{
			name:      "empty channel",
			channel:   "",
			messageID: "msg123",
			expected:  "msg123",
		},
		{
			name:      "empty messageID",
			channel:   "slack",
			messageID: "",
			expected:  "",
		},
		{
			name:      "both empty",
			channel:   "",
			messageID: "",
			expected:  "",
		},
		{
			name:      "complex channel name",
			channel:   "discord#general",
			messageID: "12345",
			expected:  "discord#general:12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MessageDedupeKey(tt.channel, tt.messageID)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDedupeCache_TTLExpiration(t *testing.T) {
	t.Run("prune removes expired entries", func(t *testing.T) {
		cache := NewDedupeCache(DedupeCacheOptions{
			TTL:     100 * time.Millisecond,
			MaxSize: 100,
		})

		baseTime := time.Now()
		cache.CheckAt("key1", baseTime)
		cache.CheckAt("key2", baseTime.Add(50*time.Millisecond))

		// At 120ms, key1 should be expired but key2 should not
		cache.CheckAt("key3", baseTime.Add(120*time.Millisecond))

		// key1 should be pruned
		if cache.ContainsAt("key1", baseTime.Add(120*time.Millisecond)) {
			t.Error("expected key1 to be pruned")
		}
		// key2 should still exist
		if !cache.ContainsAt("key2", baseTime.Add(120*time.Millisecond)) {
			t.Error("expected key2 to still exist")
		}
	})
}

func BenchmarkDedupeCache_Check(b *testing.B) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 10000,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Check("key" + string(rune(i%1000)))
	}
}

func BenchmarkDedupeCache_CheckParallel(b *testing.B) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 10000,
	})

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Check("key" + string(rune(i%1000)))
			i++
		}
	})
}

func BenchmarkDedupeCache_Contains(b *testing.B) {
	cache := NewDedupeCache(DedupeCacheOptions{
		TTL:     time.Minute,
		MaxSize: 10000,
	})

	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Check("key" + string(rune(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Contains("key" + string(rune(i%1000)))
	}
}
