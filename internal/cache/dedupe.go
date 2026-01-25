package cache

import (
	"sync"
	"time"
)

// DedupeCache provides time-limited deduplication
type DedupeCache struct {
	mu      sync.Mutex
	cache   map[string]int64 // key -> timestamp
	ttl     time.Duration
	maxSize int
}

// DedupeCacheOptions configures the cache
type DedupeCacheOptions struct {
	TTL     time.Duration
	MaxSize int
}

// NewDedupeCache creates a new deduplication cache
func NewDedupeCache(opts DedupeCacheOptions) *DedupeCache {
	ttl := opts.TTL
	if ttl < 0 {
		ttl = 0
	}
	maxSize := opts.MaxSize
	if maxSize < 0 {
		maxSize = 0
	}

	return &DedupeCache{
		cache:   make(map[string]int64),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// Check returns true if the key was seen within TTL (duplicate)
// Also adds/updates the key with current timestamp
func (c *DedupeCache) Check(key string) bool {
	return c.CheckAt(key, time.Now())
}

// CheckAt checks for duplicate with explicit timestamp (for testing)
func (c *DedupeCache) CheckAt(key string, now time.Time) bool {
	if key == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	nowUnix := now.UnixMilli()

	// Check if key exists and is still valid
	if existing, ok := c.cache[key]; ok {
		if c.ttl <= 0 || nowUnix-existing < c.ttl.Milliseconds() {
			// Key exists and is within TTL - duplicate
			c.touch(key, nowUnix)
			return true
		}
	}

	// Not a duplicate, add/update the key
	c.touch(key, nowUnix)
	c.prune(nowUnix)
	return false
}

// touch updates the key timestamp (moves to end for LRU)
func (c *DedupeCache) touch(key string, timestamp int64) {
	// Delete and re-add to maintain insertion order for LRU
	delete(c.cache, key)
	c.cache[key] = timestamp
}

// prune removes expired and excess entries
func (c *DedupeCache) prune(nowUnix int64) {
	// Remove expired entries
	if c.ttl > 0 {
		cutoff := nowUnix - c.ttl.Milliseconds()
		for key, ts := range c.cache {
			if ts < cutoff {
				delete(c.cache, key)
			}
		}
	}

	// Enforce max size (remove oldest)
	if c.maxSize <= 0 {
		c.cache = make(map[string]int64)
		return
	}

	for len(c.cache) > c.maxSize {
		// Find oldest key (maps aren't ordered, so we need to find min)
		var oldestKey string
		var oldestTs int64 = int64(^uint64(0) >> 1) // max int64
		for k, ts := range c.cache {
			if ts < oldestTs {
				oldestTs = ts
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(c.cache, oldestKey)
		} else {
			break
		}
	}
}

// Clear removes all entries
func (c *DedupeCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]int64)
}

// Size returns current number of entries
func (c *DedupeCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cache)
}

// Contains checks if key exists without updating timestamp
func (c *DedupeCache) Contains(key string) bool {
	return c.ContainsAt(key, time.Now())
}

// ContainsAt checks if key exists with explicit timestamp
func (c *DedupeCache) ContainsAt(key string, now time.Time) bool {
	if key == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.cache[key]
	if !ok {
		return false
	}

	if c.ttl <= 0 {
		return true
	}

	return now.UnixMilli()-existing < c.ttl.Milliseconds()
}

// Remove removes a specific key
func (c *DedupeCache) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, key)
}

// Keys returns all current keys (for debugging)
func (c *DedupeCache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, 0, len(c.cache))
	for k := range c.cache {
		keys = append(keys, k)
	}
	return keys
}

// MessageDedupeKey generates a deduplication key for a message
func MessageDedupeKey(channel, messageID string) string {
	if messageID == "" {
		return ""
	}
	if channel == "" {
		return messageID
	}
	return channel + ":" + messageID
}
