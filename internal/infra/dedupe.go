package infra

import (
	"sync"
	"time"
)

// DedupeCache provides a thread-safe cache for deduplicating operations.
// Entries automatically expire after a configurable TTL.
type DedupeCache struct {
	mu      sync.RWMutex
	entries map[string]*dedupeEntry
	ttl     time.Duration
	maxSize int
	onEvict func(key string)
}

type dedupeEntry struct {
	value     any
	expiresAt time.Time
}

// DedupeCacheConfig configures a DedupeCache.
type DedupeCacheConfig struct {
	// TTL is how long entries remain valid. Default: 5 minutes.
	TTL time.Duration

	// MaxSize is the maximum number of entries. 0 = unlimited.
	MaxSize int

	// OnEvict is called when an entry is evicted (optional).
	OnEvict func(key string)

	// CleanupInterval is how often to run cleanup. 0 = no background cleanup.
	CleanupInterval time.Duration
}

// DefaultDedupeCacheConfig returns sensible defaults.
func DefaultDedupeCacheConfig() *DedupeCacheConfig {
	return &DedupeCacheConfig{
		TTL:             5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: time.Minute,
	}
}

// NewDedupeCache creates a new deduplication cache.
func NewDedupeCache(cfg *DedupeCacheConfig) *DedupeCache {
	if cfg == nil {
		cfg = DefaultDedupeCacheConfig()
	}

	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Minute
	}

	c := &DedupeCache{
		entries: make(map[string]*dedupeEntry),
		ttl:     cfg.TTL,
		maxSize: cfg.MaxSize,
		onEvict: cfg.OnEvict,
	}

	// Start background cleanup if configured
	if cfg.CleanupInterval > 0 {
		go c.cleanupLoop(cfg.CleanupInterval)
	}

	return c
}

// IsDuplicate checks if a key exists and is not expired.
// If not a duplicate, it adds the key with the given value and returns false.
// This is an atomic check-and-set operation.
func (c *DedupeCache) IsDuplicate(key string, value any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if exists and not expired
	if entry, ok := c.entries[key]; ok {
		if time.Now().Before(entry.expiresAt) {
			return true
		}
		// Entry expired, will be replaced
	}

	// Evict oldest if at capacity
	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	// Add new entry
	c.entries[key] = &dedupeEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}

	return false
}

// Check returns true if the key exists and is not expired (without adding it).
func (c *DedupeCache) Check(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, ok := c.entries[key]; ok {
		return time.Now().Before(entry.expiresAt)
	}
	return false
}

// Add adds or updates a key in the cache.
func (c *DedupeCache) Add(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity and this is a new key
	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		if _, exists := c.entries[key]; !exists {
			c.evictOldest()
		}
	}

	c.entries[key] = &dedupeEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Get retrieves a value from the cache if it exists and is not expired.
func (c *DedupeCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, ok := c.entries[key]; ok {
		if time.Now().Before(entry.expiresAt) {
			return entry.value, true
		}
	}
	return nil, false
}

// Delete removes a key from the cache.
func (c *DedupeCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.onEvict != nil {
		if _, ok := c.entries[key]; ok {
			c.onEvict(key)
		}
	}
	delete(c.entries, key)
}

// Size returns the current number of entries (including expired ones).
func (c *DedupeCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all entries from the cache.
func (c *DedupeCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.onEvict != nil {
		for key := range c.entries {
			c.onEvict(key)
		}
	}
	c.entries = make(map[string]*dedupeEntry)
}

// Cleanup removes expired entries.
func (c *DedupeCache) Cleanup() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.cleanupExpired()
}

// cleanupExpired removes expired entries. Must be called with lock held.
func (c *DedupeCache) cleanupExpired() int {
	now := time.Now()
	removed := 0

	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			if c.onEvict != nil {
				c.onEvict(key)
			}
			delete(c.entries, key)
			removed++
		}
	}

	return removed
}

// evictOldest removes the oldest entry. Must be called with lock held.
func (c *DedupeCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range c.entries {
		if first || entry.expiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.expiresAt
			first = false
		}
	}

	if oldestKey != "" {
		if c.onEvict != nil {
			c.onEvict(oldestKey)
		}
		delete(c.entries, oldestKey)
	}
}

// cleanupLoop runs periodic cleanup.
func (c *DedupeCache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.Cleanup()
	}
}

// DedupeFunc wraps a function to deduplicate calls based on a key function.
// If a call with the same key is made within TTL, the cached result is returned.
func DedupeFunc[K comparable, V any](cache *DedupeCache, keyFn func(K) string, fn func(K) (V, error)) func(K) (V, error) {
	return func(input K) (V, error) {
		key := keyFn(input)

		// Check cache
		if cached, ok := cache.Get(key); ok {
			if result, ok := cached.(*funcResult[V]); ok {
				return result.value, result.err
			}
		}

		// Execute function
		value, err := fn(input)

		// Cache result
		cache.Add(key, &funcResult[V]{value: value, err: err})

		return value, err
	}
}

type funcResult[V any] struct {
	value V
	err   error
}

// MessageDeduper provides specialized deduplication for messages.
type MessageDeduper struct {
	cache *DedupeCache
}

// NewMessageDeduper creates a deduper for message IDs.
func NewMessageDeduper(ttl time.Duration) *MessageDeduper {
	return &MessageDeduper{
		cache: NewDedupeCache(&DedupeCacheConfig{
			TTL:             ttl,
			MaxSize:         50000,
			CleanupInterval: ttl / 2,
		}),
	}
}

// IsDuplicate checks if a message ID has been seen recently.
func (d *MessageDeduper) IsDuplicate(messageID string) bool {
	return d.cache.IsDuplicate(messageID, struct{}{})
}

// Mark marks a message ID as seen.
func (d *MessageDeduper) Mark(messageID string) {
	d.cache.Add(messageID, struct{}{})
}

// Size returns the number of tracked message IDs.
func (d *MessageDeduper) Size() int {
	return d.cache.Size()
}
