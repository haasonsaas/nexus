package ratelimit

import (
	"testing"
	"time"
)

func TestBucket_Allow(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         5,
		Enabled:           true,
	}
	bucket := NewBucket(config)

	// Should allow burst size requests
	for i := 0; i < 5; i++ {
		if !bucket.Allow() {
			t.Errorf("request %d should be allowed", i)
		}
	}

	// Next request should be denied
	if bucket.Allow() {
		t.Error("request after burst should be denied")
	}
}

func TestBucket_Refill(t *testing.T) {
	config := Config{
		RequestsPerSecond: 100, // Fast refill for test
		BurstSize:         2,
		Enabled:           true,
	}
	bucket := NewBucket(config)

	// Exhaust tokens
	bucket.Allow()
	bucket.Allow()

	if bucket.Allow() {
		t.Error("should be denied after exhausting tokens")
	}

	// Wait for refill
	time.Sleep(50 * time.Millisecond)

	// Should have some tokens back
	if !bucket.Allow() {
		t.Error("should be allowed after refill")
	}
}

func TestBucket_Tokens(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         5,
		Enabled:           true,
	}
	bucket := NewBucket(config)

	initial := bucket.Tokens()
	if initial != 5 {
		t.Errorf("initial tokens = %f, want 5", initial)
	}

	bucket.Allow()
	after := bucket.Tokens()
	if after >= initial {
		t.Error("tokens should decrease after Allow()")
	}
}

func TestBucket_WaitTime(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         1,
		Enabled:           true,
	}
	bucket := NewBucket(config)

	// No wait initially
	if bucket.WaitTime() != 0 {
		t.Error("should not wait when tokens available")
	}

	// Exhaust tokens
	bucket.Allow()

	// Should need to wait
	wait := bucket.WaitTime()
	if wait <= 0 {
		t.Error("should need to wait when no tokens")
	}
}

func TestLimiter_Allow(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         3,
		Enabled:           true,
	}
	limiter := NewLimiter(config)

	// Different keys should have separate limits
	for i := 0; i < 3; i++ {
		if !limiter.Allow("user1") {
			t.Errorf("user1 request %d should be allowed", i)
		}
	}

	// user1 exhausted
	if limiter.Allow("user1") {
		t.Error("user1 should be rate limited")
	}

	// user2 should still be allowed
	if !limiter.Allow("user2") {
		t.Error("user2 should be allowed")
	}
}

func TestLimiter_Disabled(t *testing.T) {
	config := Config{
		RequestsPerSecond: 1,
		BurstSize:         1,
		Enabled:           false,
	}
	limiter := NewLimiter(config)

	// Should always allow when disabled
	for i := 0; i < 100; i++ {
		if !limiter.Allow("user1") {
			t.Error("disabled limiter should always allow")
		}
	}
}

func TestLimiter_Reset(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         2,
		Enabled:           true,
	}
	limiter := NewLimiter(config)

	// Exhaust
	limiter.Allow("user1")
	limiter.Allow("user1")

	if limiter.Allow("user1") {
		t.Error("should be rate limited")
	}

	// Reset
	limiter.Reset("user1")

	// Should be allowed again
	if !limiter.Allow("user1") {
		t.Error("should be allowed after reset")
	}
}

func TestLimiter_GetStatus(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         5,
		Enabled:           true,
	}
	limiter := NewLimiter(config)

	status := limiter.GetStatus("user1")
	if !status.AllowedNow {
		t.Error("should be allowed initially")
	}
	if status.TokensRemaining != 5 {
		t.Errorf("initial tokens = %f, want 5", status.TokensRemaining)
	}
}

func TestCompositeKey(t *testing.T) {
	key := CompositeKey("channel", "telegram", "user", "12345")
	expected := "channel:telegram:user:12345"
	if key != expected {
		t.Errorf("CompositeKey() = %q, want %q", key, expected)
	}
}

func TestMultiLimiter_Allow(t *testing.T) {
	globalLimiter := NewLimiter(Config{
		RequestsPerSecond: 100,
		BurstSize:         10,
		Enabled:           true,
	})
	userLimiter := NewLimiter(Config{
		RequestsPerSecond: 10,
		BurstSize:         2,
		Enabled:           true,
	})

	multi := NewMultiLimiter(globalLimiter, userLimiter)

	// Should allow initial requests
	if !multi.Allow("user1") {
		t.Error("first request should be allowed")
	}
	if !multi.Allow("user1") {
		t.Error("second request should be allowed")
	}

	// User limiter exhausted
	if multi.Allow("user1") {
		t.Error("user should be rate limited")
	}
}

func TestMultiLimiter_WaitTime(t *testing.T) {
	limiter1 := NewLimiter(Config{
		RequestsPerSecond: 100,
		BurstSize:         1,
		Enabled:           true,
	})
	limiter2 := NewLimiter(Config{
		RequestsPerSecond: 10, // Slower refill
		BurstSize:         1,
		Enabled:           true,
	})

	multi := NewMultiLimiter(limiter1, limiter2)

	// Exhaust both
	multi.Allow("user1")

	wait := multi.WaitTime("user1")
	// Should return the longer wait time (limiter2)
	if wait <= 0 {
		t.Error("should need to wait")
	}
}

func TestBucket_AllowN(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         5,
		Enabled:           true,
	}
	bucket := NewBucket(config)

	// Should allow 3 of 5
	if !bucket.AllowN(3) {
		t.Error("should allow 3 requests")
	}

	// Should allow 2 more
	if !bucket.AllowN(2) {
		t.Error("should allow 2 more requests")
	}

	// Should deny 1
	if bucket.AllowN(1) {
		t.Error("should deny when no tokens left")
	}
}

func TestLimiter_AllowN(t *testing.T) {
	config := Config{
		RequestsPerSecond: 10,
		BurstSize:         5,
		Enabled:           true,
	}
	limiter := NewLimiter(config)

	if !limiter.AllowN("user1", 5) {
		t.Error("should allow 5 requests")
	}

	if limiter.AllowN("user1", 1) {
		t.Error("should deny when exhausted")
	}
}
