package channels

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter to prevent API throttling.
// It allows a burst of operations up to the bucket capacity, then refills at a steady rate.
type RateLimiter struct {
	// rate is the number of tokens added per second
	rate float64

	// capacity is the maximum number of tokens the bucket can hold
	capacity int

	// tokens is the current number of available tokens
	tokens float64

	// lastRefill is the timestamp of the last token refill
	lastRefill time.Time

	mu sync.Mutex
}

// NewRateLimiter creates a new rate limiter with the specified rate and capacity.
// rate: tokens per second (e.g., 10 = 10 operations per second)
// capacity: maximum burst size (e.g., 20 = allow up to 20 operations at once)
func NewRateLimiter(rate float64, capacity int) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		capacity:   capacity,
		tokens:     float64(capacity),
		lastRefill: time.Now(),
	}
}

// Wait blocks until a token is available or the context is cancelled.
// It returns an error if the context is cancelled before a token becomes available.
func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		if r.Allow() {
			return nil
		}

		// Calculate wait time until next token
		waitTime := r.waitDuration()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Try again after waiting
			continue
		}
	}
}

// Allow returns true if a token is available, consuming it in the process.
// It returns false if no tokens are available.
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	if r.tokens >= 1 {
		r.tokens--
		return true
	}

	return false
}

// AllowN returns true if n tokens are available, consuming them in the process.
// It returns false if fewer than n tokens are available.
func (r *RateLimiter) AllowN(n int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	if r.tokens >= float64(n) {
		r.tokens -= float64(n)
		return true
	}

	return false
}

// Reserve reserves a token for future use and returns the wait duration.
// Unlike Allow(), this always succeeds but may return a positive duration.
func (r *RateLimiter) Reserve() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	if r.tokens >= 1 {
		r.tokens--
		return 0
	}

	// Calculate how long until we have a token
	tokensNeeded := 1 - r.tokens
	return time.Duration(tokensNeeded / r.rate * float64(time.Second))
}

// refill adds tokens based on elapsed time since last refill.
// Must be called with lock held.
func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill)

	// Calculate tokens to add based on elapsed time
	tokensToAdd := elapsed.Seconds() * r.rate

	r.tokens += tokensToAdd
	if r.tokens > float64(r.capacity) {
		r.tokens = float64(r.capacity)
	}

	r.lastRefill = now
}

// waitDuration calculates how long to wait for the next token.
func (r *RateLimiter) waitDuration() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	if r.tokens >= 1 {
		return 0
	}

	// Calculate time until next token
	tokensNeeded := 1 - r.tokens
	return time.Duration(tokensNeeded / r.rate * float64(time.Second))
}

// Tokens returns the current number of available tokens.
func (r *RateLimiter) Tokens() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()
	return r.tokens
}

// MultiRateLimiter manages multiple rate limiters for different operation types.
// This allows fine-grained rate limiting (e.g., separate limits for sends vs. receives).
type MultiRateLimiter struct {
	limiters map[string]*RateLimiter
	mu       sync.RWMutex
}

// NewMultiRateLimiter creates a new multi-rate limiter.
func NewMultiRateLimiter() *MultiRateLimiter {
	return &MultiRateLimiter{
		limiters: make(map[string]*RateLimiter),
	}
}

// Add registers a rate limiter for a specific operation type.
func (m *MultiRateLimiter) Add(name string, rate float64, capacity int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.limiters[name] = NewRateLimiter(rate, capacity)
}

// Wait blocks until a token is available for the specified operation type.
func (m *MultiRateLimiter) Wait(ctx context.Context, name string) error {
	limiter := m.get(name)
	if limiter == nil {
		return ErrConfig(fmt.Sprintf("rate limiter %q not found", name), nil)
	}

	return limiter.Wait(ctx)
}

// Allow returns true if a token is available for the specified operation type.
func (m *MultiRateLimiter) Allow(name string) bool {
	limiter := m.get(name)
	if limiter == nil {
		// If no limiter is configured, allow the operation
		return true
	}

	return limiter.Allow()
}

// get retrieves a rate limiter by name.
func (m *MultiRateLimiter) get(name string) *RateLimiter {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.limiters[name]
}

// Reset resets all rate limiters to full capacity.
func (m *MultiRateLimiter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, limiter := range m.limiters {
		limiter.mu.Lock()
		limiter.tokens = float64(limiter.capacity)
		limiter.lastRefill = time.Now()
		limiter.mu.Unlock()
	}
}
