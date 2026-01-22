package infra

import (
	"sync"
	"sync/atomic"
	"time"
)

// TTLCache is a thread-safe cache with per-entry expiration.
// It supports automatic cleanup of expired entries.
type TTLCache[K comparable, V any] struct {
	mu         sync.RWMutex
	entries    map[K]*cacheEntry[V]
	defaultTTL time.Duration
	maxSize    int
	cleanupMu  sync.Mutex
	stopCh     chan struct{}
	stopped    atomic.Bool

	// Statistics
	hits   atomic.Uint64
	misses atomic.Uint64
	evicts atomic.Uint64
}

type cacheEntry[V any] struct {
	value     V
	expiresAt time.Time
	createdAt time.Time
}

// CacheConfig configures a TTL cache.
type CacheConfig struct {
	// DefaultTTL is the default time-to-live for entries.
	DefaultTTL time.Duration
	// MaxSize limits the cache size (0 = unlimited).
	MaxSize int
	// CleanupInterval sets how often to scan for expired entries (0 = no automatic cleanup).
	CleanupInterval time.Duration
}

// NewTTLCache creates a new TTL cache with the given configuration.
func NewTTLCache[K comparable, V any](config CacheConfig) *TTLCache[K, V] {
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 5 * time.Minute
	}

	c := &TTLCache[K, V]{
		entries:    make(map[K]*cacheEntry[V]),
		defaultTTL: config.DefaultTTL,
		maxSize:    config.MaxSize,
		stopCh:     make(chan struct{}),
	}

	if config.CleanupInterval > 0 {
		go c.cleanupLoop(config.CleanupInterval)
	}

	return c
}

// Set stores a value with the default TTL.
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL stores a value with a custom TTL.
func (c *TTLCache[K, V]) SetWithTTL(key K, value V, ttl time.Duration) {
	now := time.Now()
	entry := &cacheEntry[V]{
		value:     value,
		expiresAt: now.Add(ttl),
		createdAt: now,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check max size
	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		// Evict oldest entry (simple strategy)
		c.evictOldest()
	}

	c.entries[key] = entry
}

// Get retrieves a value from the cache.
// Returns the value and true if found and not expired, zero value and false otherwise.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		var zero V
		return zero, false
	}

	if time.Now().After(entry.expiresAt) {
		c.misses.Add(1)
		// Entry expired - remove it
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		var zero V
		return zero, false
	}

	c.hits.Add(1)
	return entry.value, true
}

// GetOrSet returns an existing value or stores and returns a new one.
// The create function is only called if the key doesn't exist or is expired.
func (c *TTLCache[K, V]) GetOrSet(key K, create func() V) V {
	return c.GetOrSetWithTTL(key, create, c.defaultTTL)
}

// GetOrSetWithTTL returns an existing value or stores and returns a new one with custom TTL.
func (c *TTLCache[K, V]) GetOrSetWithTTL(key K, create func() V, ttl time.Duration) V {
	if value, ok := c.Get(key); ok {
		return value
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expiresAt) {
		c.hits.Add(1)
		return entry.value
	}

	// Create new value
	value := create()
	now := time.Now()
	entry := &cacheEntry[V]{
		value:     value,
		expiresAt: now.Add(ttl),
		createdAt: now,
	}

	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = entry
	return value
}

// Delete removes a key from the cache.
func (c *TTLCache[K, V]) Delete(key K) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes all entries from the cache.
func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	c.entries = make(map[K]*cacheEntry[V])
	c.mu.Unlock()
}

// Len returns the number of entries in the cache (including expired).
func (c *TTLCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Contains checks if a key exists and is not expired.
func (c *TTLCache[K, V]) Contains(key K) bool {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return false
	}

	return time.Now().Before(entry.expiresAt)
}

// TTL returns the remaining time-to-live for a key.
// Returns 0 if the key doesn't exist or is expired.
func (c *TTLCache[K, V]) TTL(key K) time.Duration {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return 0
	}

	remaining := time.Until(entry.expiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Refresh updates the expiration time for a key.
func (c *TTLCache[K, V]) Refresh(key K, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return false
	}

	entry.expiresAt = time.Now().Add(ttl)
	return true
}

// Keys returns all non-expired keys.
func (c *TTLCache[K, V]) Keys() []K {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	keys := make([]K, 0, len(c.entries))
	for k, entry := range c.entries {
		if now.Before(entry.expiresAt) {
			keys = append(keys, k)
		}
	}
	return keys
}

// Stats returns cache statistics.
func (c *TTLCache[K, V]) Stats() CacheStats {
	c.mu.RLock()
	size := len(c.entries)
	c.mu.RUnlock()

	hits := c.hits.Load()
	misses := c.misses.Load()

	var hitRate float64
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return CacheStats{
		Size:    size,
		MaxSize: c.maxSize,
		Hits:    hits,
		Misses:  misses,
		Evicts:  c.evicts.Load(),
		HitRate: hitRate,
	}
}

// CacheStats contains cache statistics.
type CacheStats struct {
	Size    int
	MaxSize int
	Hits    uint64
	Misses  uint64
	Evicts  uint64
	HitRate float64
}

// Stop stops the background cleanup goroutine.
func (c *TTLCache[K, V]) Stop() {
	if c.stopped.CompareAndSwap(false, true) {
		close(c.stopCh)
	}
}

// Cleanup removes expired entries.
func (c *TTLCache[K, V]) Cleanup() int {
	c.cleanupMu.Lock()
	defer c.cleanupMu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
			removed++
		}
	}

	return removed
}

// evictOldest removes the oldest entry. Must be called with mu held.
func (c *TTLCache[K, V]) evictOldest() {
	var oldestKey K
	var oldestTime time.Time
	first := true

	for key, entry := range c.entries {
		if first || entry.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.createdAt
			first = false
		}
	}

	if !first {
		delete(c.entries, oldestKey)
		c.evicts.Add(1)
	}
}

func (c *TTLCache[K, V]) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Cleanup()
		case <-c.stopCh:
			return
		}
	}
}

// AsyncTTLCache wraps TTLCache with asynchronous value loading.
// It prevents "thundering herd" by ensuring only one goroutine fetches a missing value.
type AsyncTTLCache[K comparable, V any] struct {
	cache    *TTLCache[K, V]
	loading  map[K]chan struct{}
	loadingM sync.Mutex
}

// NewAsyncTTLCache creates a new async TTL cache.
func NewAsyncTTLCache[K comparable, V any](config CacheConfig) *AsyncTTLCache[K, V] {
	return &AsyncTTLCache[K, V]{
		cache:   NewTTLCache[K, V](config),
		loading: make(map[K]chan struct{}),
	}
}

// Get retrieves a value, using the loader function if needed.
// Only one goroutine will call the loader for a given key at a time.
func (c *AsyncTTLCache[K, V]) Get(key K, loader func(K) (V, error)) (V, error) {
	return c.GetWithTTL(key, loader, c.cache.defaultTTL)
}

// GetWithTTL retrieves a value with custom TTL, using the loader function if needed.
func (c *AsyncTTLCache[K, V]) GetWithTTL(key K, loader func(K) (V, error), ttl time.Duration) (V, error) {
	// Fast path: check cache
	if value, ok := c.cache.Get(key); ok {
		return value, nil
	}

	// Slow path: need to load
	c.loadingM.Lock()

	// Check again under lock
	if value, ok := c.cache.Get(key); ok {
		c.loadingM.Unlock()
		return value, nil
	}

	// Check if another goroutine is already loading
	if ch, ok := c.loading[key]; ok {
		c.loadingM.Unlock()
		// Wait for other goroutine
		<-ch
		// Return cached value
		if value, ok := c.cache.Get(key); ok {
			return value, nil
		}
		// Other loader failed, we need to try
		return c.GetWithTTL(key, loader, ttl)
	}

	// We're the loader
	ch := make(chan struct{})
	c.loading[key] = ch
	c.loadingM.Unlock()

	// Load the value
	value, err := loader(key)

	// Clean up loading state
	c.loadingM.Lock()
	delete(c.loading, key)
	close(ch)
	c.loadingM.Unlock()

	if err != nil {
		var zero V
		return zero, err
	}

	c.cache.SetWithTTL(key, value, ttl)
	return value, nil
}

// Set stores a value directly.
func (c *AsyncTTLCache[K, V]) Set(key K, value V) {
	c.cache.Set(key, value)
}

// Delete removes a key.
func (c *AsyncTTLCache[K, V]) Delete(key K) {
	c.cache.Delete(key)
}

// Clear removes all entries.
func (c *AsyncTTLCache[K, V]) Clear() {
	c.cache.Clear()
}

// Stats returns cache statistics.
func (c *AsyncTTLCache[K, V]) Stats() CacheStats {
	return c.cache.Stats()
}

// Stop stops background cleanup.
func (c *AsyncTTLCache[K, V]) Stop() {
	c.cache.Stop()
}
