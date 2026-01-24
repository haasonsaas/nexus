// Package ratelimit provides rate limiting for API requests, channels, and users.
package ratelimit

import (
	"sync"
	"time"
)

// Config configures rate limiting behavior.
type Config struct {
	// RequestsPerSecond is the number of requests allowed per second.
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	// BurstSize is the maximum number of requests allowed in a burst.
	BurstSize int `yaml:"burst_size"`
	// Enabled controls whether rate limiting is active.
	Enabled bool `yaml:"enabled"`
}

// DefaultConfig returns the default rate limit configuration.
func DefaultConfig() Config {
	return Config{
		RequestsPerSecond: 10.0,
		BurstSize:         20,
		Enabled:           true,
	}
}

// Bucket implements token bucket rate limiting.
type Bucket struct {
	mu          sync.Mutex
	tokens      float64
	maxTokens   float64
	refillRate  float64 // tokens per second
	lastRefill  time.Time
}

// NewBucket creates a new token bucket.
func NewBucket(config Config) *Bucket {
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = 10.0
	}
	if config.BurstSize <= 0 {
		config.BurstSize = int(config.RequestsPerSecond * 2)
	}

	return &Bucket{
		tokens:     float64(config.BurstSize),
		maxTokens:  float64(config.BurstSize),
		refillRate: config.RequestsPerSecond,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request should be allowed and consumes a token if so.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// AllowN checks if n requests should be allowed.
func (b *Bucket) AllowN(n int) bool {
	if n <= 0 {
		return true
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()

	if b.tokens >= float64(n) {
		b.tokens -= float64(n)
		return true
	}
	return false
}

// refill adds tokens based on time elapsed (must be called with lock held).
func (b *Bucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.lastRefill = now

	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
}

// Tokens returns the current number of available tokens.
func (b *Bucket) Tokens() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}

// WaitTime returns how long to wait before a request would be allowed.
func (b *Bucket) WaitTime() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()

	if b.tokens >= 1 {
		return 0
	}

	needed := 1 - b.tokens
	seconds := needed / b.refillRate
	return time.Duration(seconds * float64(time.Second))
}

// Limiter manages rate limits for multiple keys (users, channels, etc).
type Limiter struct {
	mu      sync.RWMutex
	buckets map[string]*Bucket
	config  Config
	maxKeys int
}

// NewLimiter creates a new rate limiter.
func NewLimiter(config Config) *Limiter {
	return &Limiter{
		buckets: make(map[string]*Bucket),
		config:  config,
		maxKeys: 10000,
	}
}

// Allow checks if a request for the given key should be allowed.
func (l *Limiter) Allow(key string) bool {
	if !l.config.Enabled {
		return true
	}

	bucket := l.getBucket(key)
	return bucket.Allow()
}

// AllowN checks if n requests for the given key should be allowed.
func (l *Limiter) AllowN(key string, n int) bool {
	if !l.config.Enabled {
		return true
	}

	bucket := l.getBucket(key)
	return bucket.AllowN(n)
}

// getBucket returns or creates a bucket for the given key.
func (l *Limiter) getBucket(key string) *Bucket {
	l.mu.RLock()
	bucket, exists := l.buckets[key]
	l.mu.RUnlock()

	if exists {
		return bucket
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if bucket, exists = l.buckets[key]; exists {
		return bucket
	}

	// Prune if too many keys
	if len(l.buckets) >= l.maxKeys {
		l.prune()
	}

	bucket = NewBucket(l.config)
	l.buckets[key] = bucket
	return bucket
}

// prune removes buckets with full tokens (inactive keys).
func (l *Limiter) prune() {
	// Simple approach: remove entries with full tokens (likely inactive)
	for key, bucket := range l.buckets {
		if bucket.Tokens() >= bucket.maxTokens*0.9 {
			delete(l.buckets, key)
		}
	}
}

// WaitTime returns how long to wait before a request would be allowed.
func (l *Limiter) WaitTime(key string) time.Duration {
	if !l.config.Enabled {
		return 0
	}

	bucket := l.getBucket(key)
	return bucket.WaitTime()
}

// Reset resets the rate limit for a key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// Status returns rate limit status for a key.
type Status struct {
	Key            string        `json:"key"`
	AllowedNow     bool          `json:"allowed_now"`
	TokensRemaining float64      `json:"tokens_remaining"`
	WaitTime       time.Duration `json:"wait_time"`
}

// GetStatus returns the rate limit status for a key.
func (l *Limiter) GetStatus(key string) Status {
	if !l.config.Enabled {
		return Status{
			Key:             key,
			AllowedNow:      true,
			TokensRemaining: l.config.RequestsPerSecond,
			WaitTime:        0,
		}
	}

	bucket := l.getBucket(key)
	tokens := bucket.Tokens()

	return Status{
		Key:             key,
		AllowedNow:      tokens >= 1,
		TokensRemaining: tokens,
		WaitTime:        bucket.WaitTime(),
	}
}

// CompositeKey creates a rate limit key from multiple parts.
func CompositeKey(parts ...string) string {
	key := ""
	for i, part := range parts {
		if i > 0 {
			key += ":"
		}
		key += part
	}
	return key
}

// MultiLimiter applies multiple rate limiters.
type MultiLimiter struct {
	limiters []*Limiter
}

// NewMultiLimiter creates a limiter that checks multiple limits.
func NewMultiLimiter(limiters ...*Limiter) *MultiLimiter {
	return &MultiLimiter{limiters: limiters}
}

// Allow checks if all limiters allow the request.
func (m *MultiLimiter) Allow(key string) bool {
	for _, l := range m.limiters {
		if !l.Allow(key) {
			return false
		}
	}
	return true
}

// WaitTime returns the maximum wait time across all limiters.
func (m *MultiLimiter) WaitTime(key string) time.Duration {
	var maxWait time.Duration
	for _, l := range m.limiters {
		wait := l.WaitTime(key)
		if wait > maxWait {
			maxWait = wait
		}
	}
	return maxWait
}
