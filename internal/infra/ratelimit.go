package infra

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Rate limiter errors
var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// RateLimiter defines the rate limiter interface.
type RateLimiter interface {
	// Allow checks if a request is allowed under the rate limit.
	Allow() bool

	// AllowN checks if n requests are allowed.
	AllowN(n int) bool

	// Wait blocks until a request is allowed or context is cancelled.
	Wait(ctx context.Context) error

	// WaitN blocks until n requests are allowed or context is cancelled.
	WaitN(ctx context.Context, n int) error
}

// TokenBucket implements a token bucket rate limiter.
type TokenBucket struct {
	mu sync.Mutex

	// Configuration
	rate     float64   // tokens per second
	capacity int       // maximum tokens
	tokens   float64   // current tokens
	lastTime time.Time // last refill time
}

// NewTokenBucket creates a new token bucket rate limiter.
// rate is tokens per second, capacity is the maximum burst size.
func NewTokenBucket(rate float64, capacity int) *TokenBucket {
	if rate <= 0 {
		rate = 1
	}
	if capacity <= 0 {
		capacity = 1
	}

	return &TokenBucket{
		rate:     rate,
		capacity: capacity,
		tokens:   float64(capacity), // Start full
		lastTime: time.Now(),
	}
}

// Allow checks if one request is allowed.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	return false
}

// Wait blocks until one request is allowed.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	return tb.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (tb *TokenBucket) WaitN(ctx context.Context, n int) error {
	for {
		tb.mu.Lock()
		tb.refill()

		if tb.tokens >= float64(n) {
			tb.tokens -= float64(n)
			tb.mu.Unlock()
			return nil
		}

		// Calculate wait time for needed tokens
		needed := float64(n) - tb.tokens
		waitTime := time.Duration(needed/tb.rate*1000) * time.Millisecond
		tb.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Continue loop to recheck
		}
	}
}

// refill adds tokens based on elapsed time. Must be called with lock held.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	tb.tokens += elapsed * tb.rate
	if tb.tokens > float64(tb.capacity) {
		tb.tokens = float64(tb.capacity)
	}
}

// Available returns the current number of available tokens.
func (tb *TokenBucket) Available() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return int(tb.tokens)
}

// SlidingWindowLimiter implements a sliding window rate limiter.
type SlidingWindowLimiter struct {
	mu sync.Mutex

	// Configuration
	limit    int           // maximum requests per window
	window   time.Duration // window duration
	requests []time.Time   // request timestamps
}

// NewSlidingWindowLimiter creates a new sliding window rate limiter.
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Second
	}

	return &SlidingWindowLimiter{
		limit:    limit,
		window:   window,
		requests: make([]time.Time, 0, limit),
	}
}

// Allow checks if one request is allowed.
func (sw *SlidingWindowLimiter) Allow() bool {
	return sw.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (sw *SlidingWindowLimiter) AllowN(n int) bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.cleanup()

	if len(sw.requests)+n <= sw.limit {
		now := time.Now()
		for i := 0; i < n; i++ {
			sw.requests = append(sw.requests, now)
		}
		return true
	}
	return false
}

// Wait blocks until one request is allowed.
func (sw *SlidingWindowLimiter) Wait(ctx context.Context) error {
	return sw.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (sw *SlidingWindowLimiter) WaitN(ctx context.Context, n int) error {
	for {
		sw.mu.Lock()
		sw.cleanup()

		if len(sw.requests)+n <= sw.limit {
			now := time.Now()
			for i := 0; i < n; i++ {
				sw.requests = append(sw.requests, now)
			}
			sw.mu.Unlock()
			return nil
		}

		// Calculate wait time until oldest request expires
		var waitTime time.Duration
		if len(sw.requests) > 0 {
			oldest := sw.requests[0]
			waitTime = sw.window - time.Since(oldest)
			if waitTime < 0 {
				waitTime = time.Millisecond
			}
		}
		sw.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Continue loop
		}
	}
}

// cleanup removes expired requests. Must be called with lock held.
func (sw *SlidingWindowLimiter) cleanup() {
	cutoff := time.Now().Add(-sw.window)
	valid := sw.requests[:0]
	for _, t := range sw.requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	sw.requests = valid
}

// Available returns the number of available request slots.
func (sw *SlidingWindowLimiter) Available() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.cleanup()
	return sw.limit - len(sw.requests)
}

// RateLimiterConfig configures a rate limiter.
type RateLimiterConfig struct {
	// Type is "token_bucket" or "sliding_window"
	Type string

	// For token bucket
	Rate     float64 // tokens per second
	Capacity int     // max burst

	// For sliding window
	Limit  int           // requests per window
	Window time.Duration // window duration
}

// NewRateLimiter creates a rate limiter from config.
func NewRateLimiter(config RateLimiterConfig) RateLimiter {
	switch config.Type {
	case "sliding_window":
		return NewSlidingWindowLimiter(config.Limit, config.Window)
	default: // token_bucket
		return NewTokenBucket(config.Rate, config.Capacity)
	}
}

// RateLimiterRegistry manages rate limiters by key.
type RateLimiterRegistry struct {
	mu       sync.RWMutex
	limiters map[string]RateLimiter
	factory  func(key string) RateLimiter
}

// NewRateLimiterRegistry creates a registry with a factory function.
func NewRateLimiterRegistry(factory func(key string) RateLimiter) *RateLimiterRegistry {
	return &RateLimiterRegistry{
		limiters: make(map[string]RateLimiter),
		factory:  factory,
	}
}

// Get returns or creates a rate limiter for the key.
func (r *RateLimiterRegistry) Get(key string) RateLimiter {
	r.mu.RLock()
	limiter, ok := r.limiters[key]
	r.mu.RUnlock()

	if ok {
		return limiter
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check
	if limiter, ok := r.limiters[key]; ok {
		return limiter
	}

	limiter = r.factory(key)
	r.limiters[key] = limiter
	return limiter
}

// Remove removes a rate limiter.
func (r *RateLimiterRegistry) Remove(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.limiters, key)
}

// Keys returns all registered keys.
func (r *RateLimiterRegistry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.limiters))
	for k := range r.limiters {
		keys = append(keys, k)
	}
	return keys
}

// CombinedLimiter applies multiple rate limiters.
type CombinedLimiter struct {
	limiters []RateLimiter
}

// NewCombinedLimiter creates a limiter that applies all given limiters.
func NewCombinedLimiter(limiters ...RateLimiter) *CombinedLimiter {
	return &CombinedLimiter{limiters: limiters}
}

// Allow checks if all limiters allow the request.
func (c *CombinedLimiter) Allow() bool {
	return c.AllowN(1)
}

// AllowN checks if all limiters allow n requests.
// Note: This is not atomic - if one limiter allows but another doesn't,
// tokens will be consumed from the first. Use Wait for safer behavior.
func (c *CombinedLimiter) AllowN(n int) bool {
	for _, limiter := range c.limiters {
		if !limiter.AllowN(n) {
			return false
		}
	}
	return true
}

// Wait blocks until all limiters allow.
func (c *CombinedLimiter) Wait(ctx context.Context) error {
	return c.WaitN(ctx, 1)
}

// WaitN blocks until all limiters allow n requests.
func (c *CombinedLimiter) WaitN(ctx context.Context, n int) error {
	for _, limiter := range c.limiters {
		if err := limiter.WaitN(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

// PerKeyLimiter applies rate limiting per key.
type PerKeyLimiter struct {
	registry *RateLimiterRegistry
}

// NewPerKeyLimiter creates a per-key rate limiter with the given factory.
func NewPerKeyLimiter(factory func(key string) RateLimiter) *PerKeyLimiter {
	return &PerKeyLimiter{
		registry: NewRateLimiterRegistry(factory),
	}
}

// Allow checks if a request for the key is allowed.
func (p *PerKeyLimiter) Allow(key string) bool {
	return p.registry.Get(key).Allow()
}

// AllowN checks if n requests for the key are allowed.
func (p *PerKeyLimiter) AllowN(key string, n int) bool {
	return p.registry.Get(key).AllowN(n)
}

// Wait blocks until a request for the key is allowed.
func (p *PerKeyLimiter) Wait(ctx context.Context, key string) error {
	return p.registry.Get(key).Wait(ctx)
}

// WaitN blocks until n requests for the key are allowed.
func (p *PerKeyLimiter) WaitN(ctx context.Context, key string, n int) error {
	return p.registry.Get(key).WaitN(ctx, n)
}
