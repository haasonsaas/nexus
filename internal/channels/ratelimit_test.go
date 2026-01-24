package channels

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10.0, 5)

	if rl.rate != 10.0 {
		t.Errorf("expected rate 10.0, got %f", rl.rate)
	}
	if rl.capacity != 5 {
		t.Errorf("expected capacity 5, got %d", rl.capacity)
	}
	if rl.tokens != 5.0 {
		t.Errorf("expected initial tokens 5.0, got %f", rl.tokens)
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(10.0, 3)

	// Should allow up to capacity
	for i := 0; i < 3; i++ {
		if !rl.Allow() {
			t.Errorf("expected Allow() to return true for request %d", i+1)
		}
	}

	// Next should be denied
	if rl.Allow() {
		t.Error("expected Allow() to return false when empty")
	}
}

func TestRateLimiter_AllowN(t *testing.T) {
	rl := NewRateLimiter(10.0, 10)

	if !rl.AllowN(5) {
		t.Error("expected AllowN(5) to succeed")
	}

	if !rl.AllowN(5) {
		t.Error("expected AllowN(5) to succeed again")
	}

	if rl.AllowN(1) {
		t.Error("expected AllowN(1) to fail when empty")
	}
}

func TestRateLimiter_AllowN_InsufficientTokens(t *testing.T) {
	rl := NewRateLimiter(10.0, 5)

	// Request more than available
	if rl.AllowN(10) {
		t.Error("expected AllowN(10) to fail with capacity 5")
	}

	// Tokens should not be consumed on failure
	if !rl.AllowN(5) {
		t.Error("expected AllowN(5) to succeed after failed AllowN(10)")
	}
}

func TestRateLimiter_Wait(t *testing.T) {
	rl := NewRateLimiter(100.0, 1) // 100/s, capacity 1

	// Use the initial token
	rl.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := rl.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have waited ~10ms for 1 token at 100/s
	if elapsed < 5*time.Millisecond {
		t.Error("expected Wait to block until token available")
	}
}

func TestRateLimiter_Wait_ContextCancelled(t *testing.T) {
	rl := NewRateLimiter(0.1, 1) // Very slow refill

	// Use the initial token
	rl.Allow()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := rl.Wait(ctx)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimiter_Wait_DeadlineExceeded(t *testing.T) {
	rl := NewRateLimiter(0.1, 1) // Very slow: 0.1/s

	// Use the initial token
	rl.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx)

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestRateLimiter_Reserve(t *testing.T) {
	rl := NewRateLimiter(100.0, 2)

	// First reserve should be instant
	dur := rl.Reserve()
	if dur != 0 {
		t.Errorf("expected 0 duration for first reserve, got %v", dur)
	}

	// Second reserve should also be instant
	dur = rl.Reserve()
	if dur != 0 {
		t.Errorf("expected 0 duration for second reserve, got %v", dur)
	}

	// Third reserve should return wait duration
	dur = rl.Reserve()
	if dur <= 0 {
		t.Error("expected positive duration when no tokens available")
	}
}

func TestRateLimiter_Tokens(t *testing.T) {
	rl := NewRateLimiter(10.0, 10)

	if rl.Tokens() != 10.0 {
		t.Errorf("expected 10.0 tokens, got %f", rl.Tokens())
	}

	rl.Allow()
	rl.Allow()

	tokens := rl.Tokens()
	if tokens < 7.9 || tokens > 8.1 { // Allow for small timing variations
		t.Errorf("expected ~8.0 tokens, got %f", tokens)
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(100.0, 10) // 100/s, capacity 10

	// Drain the bucket
	for i := 0; i < 10; i++ {
		rl.Allow()
	}

	if rl.Allow() {
		t.Error("expected bucket to be empty")
	}

	// Wait for refill (100/s = 1 token per 10ms)
	time.Sleep(50 * time.Millisecond)

	// Should have refilled some tokens
	if !rl.Allow() {
		t.Error("expected token to be refilled")
	}
}

func TestRateLimiter_RefillCapsAtCapacity(t *testing.T) {
	rl := NewRateLimiter(1000.0, 5) // Very fast refill

	// Wait for lots of tokens to be added
	time.Sleep(100 * time.Millisecond)

	// Should still be capped at capacity
	tokens := rl.Tokens()
	if tokens > 5.0 {
		t.Errorf("expected tokens capped at 5.0, got %f", tokens)
	}
}

func TestRateLimiter_WaitDuration(t *testing.T) {
	rl := NewRateLimiter(100.0, 2)

	// Drain the bucket
	rl.Allow()
	rl.Allow()

	dur := rl.waitDuration()
	if dur <= 0 {
		t.Error("expected positive wait duration when empty")
	}

	// Wait duration should be approximately 1/rate seconds
	expected := 10 * time.Millisecond // 1/100 = 10ms
	if dur < expected/2 || dur > expected*2 {
		t.Errorf("expected wait duration around %v, got %v", expected, dur)
	}
}

func TestRateLimiter_WaitDuration_TokensAvailable(t *testing.T) {
	rl := NewRateLimiter(10.0, 5)

	dur := rl.waitDuration()
	if dur != 0 {
		t.Errorf("expected 0 wait duration when tokens available, got %v", dur)
	}
}

func TestNewMultiRateLimiter(t *testing.T) {
	mrl := NewMultiRateLimiter()

	if mrl.limiters == nil {
		t.Error("expected limiters map to be initialized")
	}
}

func TestMultiRateLimiter_Add(t *testing.T) {
	mrl := NewMultiRateLimiter()
	mrl.Add("send", 10.0, 5)

	limiter := mrl.get("send")
	if limiter == nil {
		t.Error("expected limiter to be added")
	}

	if limiter.rate != 10.0 {
		t.Errorf("expected rate 10.0, got %f", limiter.rate)
	}
}

func TestMultiRateLimiter_Wait(t *testing.T) {
	mrl := NewMultiRateLimiter()
	mrl.Add("send", 100.0, 1)

	ctx := context.Background()

	// First wait should succeed
	err := mrl.Wait(ctx, "send")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMultiRateLimiter_Wait_NotFound(t *testing.T) {
	mrl := NewMultiRateLimiter()

	ctx := context.Background()

	err := mrl.Wait(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent limiter")
	}
}

func TestMultiRateLimiter_Allow(t *testing.T) {
	mrl := NewMultiRateLimiter()
	mrl.Add("api", 10.0, 2)

	if !mrl.Allow("api") {
		t.Error("expected Allow to succeed")
	}
	if !mrl.Allow("api") {
		t.Error("expected Allow to succeed again")
	}
	if mrl.Allow("api") {
		t.Error("expected Allow to fail when empty")
	}
}

func TestMultiRateLimiter_Allow_NotFound(t *testing.T) {
	mrl := NewMultiRateLimiter()

	// Should return true when limiter not found
	if !mrl.Allow("nonexistent") {
		t.Error("expected Allow to return true when limiter not configured")
	}
}

func TestMultiRateLimiter_Reset(t *testing.T) {
	mrl := NewMultiRateLimiter()
	mrl.Add("a", 10.0, 5)
	mrl.Add("b", 10.0, 3)

	// Drain both limiters
	for i := 0; i < 5; i++ {
		mrl.Allow("a")
	}
	for i := 0; i < 3; i++ {
		mrl.Allow("b")
	}

	// Should be empty
	if mrl.Allow("a") {
		t.Error("expected 'a' to be empty before reset")
	}
	if mrl.Allow("b") {
		t.Error("expected 'b' to be empty before reset")
	}

	// Reset all limiters
	mrl.Reset()

	// Should be full again
	if !mrl.Allow("a") {
		t.Error("expected 'a' to be full after reset")
	}
	if !mrl.Allow("b") {
		t.Error("expected 'b' to be full after reset")
	}
}

func TestMultiRateLimiter_Get_NotFound(t *testing.T) {
	mrl := NewMultiRateLimiter()

	limiter := mrl.get("nonexistent")
	if limiter != nil {
		t.Error("expected nil for nonexistent limiter")
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(1000.0, 100)

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow() {
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

func TestMultiRateLimiter_Concurrent(t *testing.T) {
	mrl := NewMultiRateLimiter()
	mrl.Add("op", 1000.0, 50)

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if mrl.Allow("op") {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Should have allowed approximately 50
	if allowed > 60 || allowed < 40 {
		t.Errorf("expected ~50 allowed, got %d", allowed)
	}
}
