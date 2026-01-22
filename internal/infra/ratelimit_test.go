package infra

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket_AllowInitial(t *testing.T) {
	tb := NewTokenBucket(10, 5) // 10/s, capacity 5

	// Should allow up to capacity
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("expected Allow() to return true for request %d", i+1)
		}
	}

	// Next should be denied
	if tb.Allow() {
		t.Error("expected Allow() to return false when empty")
	}
}

func TestTokenBucket_AllowN(t *testing.T) {
	tb := NewTokenBucket(10, 10)

	if !tb.AllowN(5) {
		t.Error("expected AllowN(5) to succeed")
	}

	if !tb.AllowN(5) {
		t.Error("expected AllowN(5) to succeed again")
	}

	if tb.AllowN(1) {
		t.Error("expected AllowN(1) to fail when empty")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := NewTokenBucket(100, 10) // 100/s, capacity 10

	// Drain the bucket
	for i := 0; i < 10; i++ {
		tb.Allow()
	}

	if tb.Allow() {
		t.Error("expected bucket to be empty")
	}

	// Wait for refill (100/s = 1 token per 10ms)
	time.Sleep(50 * time.Millisecond)

	// Should have refilled some tokens
	if !tb.Allow() {
		t.Error("expected token to be refilled")
	}
}

func TestTokenBucket_Wait(t *testing.T) {
	tb := NewTokenBucket(100, 1) // 100/s, capacity 1

	// Use the initial token
	tb.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := tb.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have waited ~10ms for 1 token at 100/s
	if elapsed < 5*time.Millisecond {
		t.Error("expected Wait to block until token available")
	}
}

func TestTokenBucket_WaitTimeout(t *testing.T) {
	tb := NewTokenBucket(0.1, 1) // Very slow: 0.1/s

	// Use the initial token
	tb.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := tb.Wait(ctx)

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestTokenBucket_Available(t *testing.T) {
	tb := NewTokenBucket(10, 10)

	if tb.Available() != 10 {
		t.Errorf("expected 10 available, got %d", tb.Available())
	}

	tb.Allow()
	tb.Allow()

	if tb.Available() != 8 {
		t.Errorf("expected 8 available, got %d", tb.Available())
	}
}

func TestSlidingWindowLimiter_Allow(t *testing.T) {
	sw := NewSlidingWindowLimiter(5, 100*time.Millisecond)

	// Should allow up to limit
	for i := 0; i < 5; i++ {
		if !sw.Allow() {
			t.Errorf("expected Allow() to return true for request %d", i+1)
		}
	}

	// Next should be denied
	if sw.Allow() {
		t.Error("expected Allow() to return false at limit")
	}
}

func TestSlidingWindowLimiter_WindowExpiry(t *testing.T) {
	sw := NewSlidingWindowLimiter(2, 50*time.Millisecond)

	// Use all slots
	sw.Allow()
	sw.Allow()

	if sw.Allow() {
		t.Error("expected denial at limit")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should allow again
	if !sw.Allow() {
		t.Error("expected Allow() after window expiry")
	}
}

func TestSlidingWindowLimiter_Wait(t *testing.T) {
	sw := NewSlidingWindowLimiter(1, 50*time.Millisecond)

	// Use the slot
	sw.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sw.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have waited for window expiry
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected Wait to block until window expired, elapsed %v", elapsed)
	}
}

func TestSlidingWindowLimiter_Available(t *testing.T) {
	sw := NewSlidingWindowLimiter(5, time.Second)

	if sw.Available() != 5 {
		t.Errorf("expected 5 available, got %d", sw.Available())
	}

	sw.Allow()
	sw.Allow()

	if sw.Available() != 3 {
		t.Errorf("expected 3 available, got %d", sw.Available())
	}
}

func TestNewRateLimiter_TokenBucket(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterConfig{
		Type:     "token_bucket",
		Rate:     10,
		Capacity: 5,
	})

	_, ok := limiter.(*TokenBucket)
	if !ok {
		t.Error("expected TokenBucket type")
	}
}

func TestNewRateLimiter_SlidingWindow(t *testing.T) {
	limiter := NewRateLimiter(RateLimiterConfig{
		Type:   "sliding_window",
		Limit:  10,
		Window: time.Second,
	})

	_, ok := limiter.(*SlidingWindowLimiter)
	if !ok {
		t.Error("expected SlidingWindowLimiter type")
	}
}

func TestRateLimiterRegistry_Get(t *testing.T) {
	factory := func(key string) RateLimiter {
		return NewTokenBucket(10, 5)
	}

	registry := NewRateLimiterRegistry(factory)

	l1 := registry.Get("user-1")
	l2 := registry.Get("user-1")
	l3 := registry.Get("user-2")

	if l1 != l2 {
		t.Error("expected same limiter for same key")
	}
	if l1 == l3 {
		t.Error("expected different limiters for different keys")
	}
}

func TestRateLimiterRegistry_Remove(t *testing.T) {
	factory := func(key string) RateLimiter {
		return NewTokenBucket(10, 5)
	}

	registry := NewRateLimiterRegistry(factory)

	l1 := registry.Get("user-1")
	registry.Remove("user-1")
	l2 := registry.Get("user-1")

	if l1 == l2 {
		t.Error("expected new limiter after remove")
	}
}

func TestRateLimiterRegistry_Keys(t *testing.T) {
	factory := func(key string) RateLimiter {
		return NewTokenBucket(10, 5)
	}

	registry := NewRateLimiterRegistry(factory)

	registry.Get("a")
	registry.Get("b")
	registry.Get("c")

	keys := registry.Keys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

func TestCombinedLimiter_AllowBoth(t *testing.T) {
	l1 := NewTokenBucket(10, 10)
	l2 := NewTokenBucket(10, 10)

	combined := NewCombinedLimiter(l1, l2)

	if !combined.Allow() {
		t.Error("expected combined Allow() to succeed when both allow")
	}
}

func TestCombinedLimiter_DenyIfOneDenies(t *testing.T) {
	l1 := NewTokenBucket(10, 10)
	l2 := NewTokenBucket(10, 1)

	// Drain l2
	l2.Allow()

	combined := NewCombinedLimiter(l1, l2)

	if combined.Allow() {
		t.Error("expected combined Allow() to fail when one denies")
	}
}

func TestCombinedLimiter_Wait(t *testing.T) {
	l1 := NewTokenBucket(100, 1)
	l2 := NewTokenBucket(100, 1)

	// Drain both
	l1.Allow()
	l2.Allow()

	combined := NewCombinedLimiter(l1, l2)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := combined.Wait(ctx)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPerKeyLimiter_SeparateKeys(t *testing.T) {
	factory := func(key string) RateLimiter {
		return NewTokenBucket(10, 2)
	}

	pkl := NewPerKeyLimiter(factory)

	// User A uses their limit
	pkl.Allow("user-a")
	pkl.Allow("user-a")

	// User A should be denied
	if pkl.Allow("user-a") {
		t.Error("expected user-a to be denied")
	}

	// User B should still be allowed
	if !pkl.Allow("user-b") {
		t.Error("expected user-b to be allowed")
	}
}

func TestPerKeyLimiter_Wait(t *testing.T) {
	factory := func(key string) RateLimiter {
		return NewTokenBucket(100, 1)
	}

	pkl := NewPerKeyLimiter(factory)

	// Drain user-a
	pkl.Allow("user-a")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := pkl.Wait(ctx, "user-a")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	tb := NewTokenBucket(1000, 100)

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Should have allowed approximately capacity (100)
	if allowed > 110 || allowed < 90 {
		t.Errorf("expected ~100 allowed, got %d", allowed)
	}
}

func TestSlidingWindowLimiter_Concurrent(t *testing.T) {
	sw := NewSlidingWindowLimiter(100, time.Second)

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sw.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if allowed != 100 {
		t.Errorf("expected exactly 100 allowed, got %d", allowed)
	}
}

func TestTokenBucket_ZeroConfig(t *testing.T) {
	// Should not panic with zero config
	tb := NewTokenBucket(0, 0)

	// Should use defaults
	if !tb.Allow() {
		t.Error("expected default config to allow at least one request")
	}
}

func TestSlidingWindowLimiter_ZeroConfig(t *testing.T) {
	// Should not panic with zero config
	sw := NewSlidingWindowLimiter(0, 0)

	// Should use defaults
	if !sw.Allow() {
		t.Error("expected default config to allow at least one request")
	}
}
