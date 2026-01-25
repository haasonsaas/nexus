package typing

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewTypingController_DefaultConfig(t *testing.T) {
	controller := NewTypingController(nil)
	if controller == nil {
		t.Fatal("expected controller, got nil")
	}
	if controller.config == nil {
		t.Fatal("expected default config")
	}
	if controller.config.TypingIntervalSeconds != DefaultTypingIntervalSeconds {
		t.Errorf("TypingIntervalSeconds = %d, want %d", controller.config.TypingIntervalSeconds, DefaultTypingIntervalSeconds)
	}
	if controller.config.TypingTTLMs != DefaultTypingTTLMs {
		t.Errorf("TypingTTLMs = %d, want %d", controller.config.TypingTTLMs, DefaultTypingTTLMs)
	}
	if controller.config.SilentToken != DefaultSilentToken {
		t.Errorf("SilentToken = %q, want %q", controller.config.SilentToken, DefaultSilentToken)
	}
}

func TestNewTypingController_CustomConfig(t *testing.T) {
	config := &TypingControllerConfig{
		TypingIntervalSeconds: 10,
		TypingTTLMs:           5000,
		SilentToken:           "CUSTOM_SILENT",
	}
	controller := NewTypingController(config)

	if controller.config.TypingIntervalSeconds != 10 {
		t.Errorf("TypingIntervalSeconds = %d, want 10", controller.config.TypingIntervalSeconds)
	}
	if controller.config.TypingTTLMs != 5000 {
		t.Errorf("TypingTTLMs = %d, want 5000", controller.config.TypingTTLMs)
	}
	if controller.config.SilentToken != "CUSTOM_SILENT" {
		t.Errorf("SilentToken = %q, want %q", controller.config.SilentToken, "CUSTOM_SILENT")
	}
}

func TestNewTypingController_ZeroValuesUseDefaults(t *testing.T) {
	config := &TypingControllerConfig{
		TypingIntervalSeconds: 0,
		TypingTTLMs:           0,
		SilentToken:           "",
	}
	controller := NewTypingController(config)

	if controller.config.TypingIntervalSeconds != DefaultTypingIntervalSeconds {
		t.Errorf("TypingIntervalSeconds = %d, want default %d", controller.config.TypingIntervalSeconds, DefaultTypingIntervalSeconds)
	}
	if controller.config.TypingTTLMs != DefaultTypingTTLMs {
		t.Errorf("TypingTTLMs = %d, want default %d", controller.config.TypingTTLMs, DefaultTypingTTLMs)
	}
	if controller.config.SilentToken != DefaultSilentToken {
		t.Errorf("SilentToken = %q, want default %q", controller.config.SilentToken, DefaultSilentToken)
	}
}

func TestTypingController_OnReplyStart(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           1000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	controller.OnReplyStart()

	if !controller.IsActive() {
		t.Error("expected controller to be active")
	}

	count := atomic.LoadInt32(&callCount)
	if count != 1 {
		t.Errorf("expected 1 call, got %d", count)
	}

	// Calling again should not trigger another callback (already started)
	controller.OnReplyStart()
	count = atomic.LoadInt32(&callCount)
	if count != 1 {
		t.Errorf("expected 1 call after second OnReplyStart, got %d", count)
	}
}

func TestTypingController_StartTypingLoop(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	// Start typing loop with short interval for testing
	controller.typingIntervalMs = 50 * time.Millisecond
	controller.StartTypingLoop()

	// Wait for a few ticks
	time.Sleep(200 * time.Millisecond)

	controller.Cleanup()

	count := atomic.LoadInt32(&callCount)
	// Initial call + at least 2 interval calls
	if count < 3 {
		t.Errorf("expected at least 3 calls, got %d", count)
	}
}

func TestTypingController_StartTypingLoopIdempotent(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	controller.typingIntervalMs = 50 * time.Millisecond

	// Multiple calls should only start one loop
	controller.StartTypingLoop()
	controller.StartTypingLoop()
	controller.StartTypingLoop()

	time.Sleep(100 * time.Millisecond)

	controller.Cleanup()

	count := atomic.LoadInt32(&callCount)
	// Should have initial call + maybe 1-2 interval calls, not triple that
	if count > 5 {
		t.Errorf("expected fewer calls (loop started only once), got %d", count)
	}
}

func TestTypingController_TTLStopsTyping(t *testing.T) {
	var callCount int32
	var logMessages []string
	var mu sync.Mutex

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           100, // Very short TTL
		SilentToken:           "NO_REPLY",
		Log: func(message string) {
			mu.Lock()
			logMessages = append(logMessages, message)
			mu.Unlock()
		},
	})

	controller.typingIntervalMs = 20 * time.Millisecond
	controller.typingTTLMs = 100 * time.Millisecond

	controller.StartTypingLoop()

	// Wait for TTL to expire
	time.Sleep(250 * time.Millisecond)

	if controller.IsActive() {
		t.Error("expected controller to be inactive after TTL")
	}

	if !controller.IsSealed() {
		t.Error("expected controller to be sealed after TTL cleanup")
	}

	mu.Lock()
	hasLogMessage := len(logMessages) > 0
	mu.Unlock()

	if !hasLogMessage {
		t.Error("expected log message about TTL reached")
	}
}

func TestTypingController_SealingPreventsRestart(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})

	controller.OnReplyStart()
	initialCount := atomic.LoadInt32(&callCount)

	controller.Cleanup()

	if !controller.IsSealed() {
		t.Error("expected controller to be sealed after cleanup")
	}

	// Try to restart after sealing
	controller.OnReplyStart()
	controller.StartTypingLoop()
	controller.StartTypingOnText("hello")

	count := atomic.LoadInt32(&callCount)
	if count != initialCount {
		t.Errorf("expected no additional calls after sealing, got %d (initial: %d)", count, initialCount)
	}

	if controller.IsActive() {
		t.Error("expected controller to be inactive after sealing")
	}
}

func TestTypingController_SilentTokenSuppressesTyping(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	// Silent token at start
	controller.StartTypingOnText("NO_REPLY rest of text")
	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("expected no calls for silent reply at start")
	}

	// Silent token at end
	controller.StartTypingOnText("some text NO_REPLY")
	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("expected no calls for silent reply at end")
	}

	// Empty text
	controller.StartTypingOnText("")
	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("expected no calls for empty text")
	}

	// Whitespace only
	controller.StartTypingOnText("   ")
	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("expected no calls for whitespace-only text")
	}

	// Normal text should trigger
	controller.StartTypingOnText("Hello world")
	if atomic.LoadInt32(&callCount) != 1 {
		t.Error("expected call for normal text")
	}
}

func TestTypingController_MarkRunCompleteAndDispatchIdle(t *testing.T) {
	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart:          func() {},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})

	controller.OnReplyStart()

	// Mark only run complete - should not stop
	controller.MarkRunComplete()
	// Active check might still be true until both conditions met
	// but it won't be sealed

	if controller.IsSealed() {
		t.Error("expected controller to not be sealed with only runComplete")
	}

	// Mark dispatch idle - now both are true, should cleanup
	controller.MarkDispatchIdle()

	if !controller.IsSealed() {
		t.Error("expected controller to be sealed when both runComplete and dispatchIdle")
	}

	if controller.IsActive() {
		t.Error("expected controller to be inactive after cleanup")
	}
}

func TestTypingController_MarkDispatchIdleFirst(t *testing.T) {
	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart:          func() {},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})

	controller.OnReplyStart()

	// Mark dispatch idle first
	controller.MarkDispatchIdle()

	if controller.IsSealed() {
		t.Error("expected controller to not be sealed with only dispatchIdle")
	}

	// Now mark run complete
	controller.MarkRunComplete()

	if !controller.IsSealed() {
		t.Error("expected controller to be sealed when both conditions met")
	}
}

func TestTypingController_CleanupStopsAllTimers(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})

	controller.typingIntervalMs = 20 * time.Millisecond

	controller.StartTypingLoop()
	time.Sleep(50 * time.Millisecond)

	countBeforeCleanup := atomic.LoadInt32(&callCount)

	controller.Cleanup()

	// Wait and verify no more calls
	time.Sleep(100 * time.Millisecond)

	countAfterWait := atomic.LoadInt32(&callCount)

	if countAfterWait != countBeforeCleanup {
		t.Errorf("expected no more calls after cleanup, got %d more", countAfterWait-countBeforeCleanup)
	}
}

func TestTypingController_CleanupIdempotent(t *testing.T) {
	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart:          func() {},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})

	controller.OnReplyStart()

	// Multiple cleanups should not panic
	controller.Cleanup()
	controller.Cleanup()
	controller.Cleanup()

	if !controller.IsSealed() {
		t.Error("expected controller to remain sealed")
	}
}

func TestTypingController_RefreshTypingTTL(t *testing.T) {
	var cleanedUp atomic.Int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart:          func() {},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           100,
		SilentToken:           "NO_REPLY",
		Log: func(message string) {
			cleanedUp.Store(1)
		},
	})

	controller.typingIntervalMs = 20 * time.Millisecond
	controller.typingTTLMs = 100 * time.Millisecond

	controller.StartTypingLoop()

	// Keep refreshing TTL before it expires
	for i := 0; i < 3; i++ {
		time.Sleep(50 * time.Millisecond)
		controller.RefreshTypingTTL()
	}

	// After refreshing, should still be active
	if !controller.IsActive() {
		t.Error("expected controller to still be active after TTL refresh")
	}

	// Now wait for TTL to actually expire
	time.Sleep(200 * time.Millisecond)

	if controller.IsActive() {
		t.Error("expected controller to be inactive after TTL finally expired")
	}
}

func TestTypingController_ConcurrentAccess(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	controller.typingIntervalMs = 10 * time.Millisecond

	var wg sync.WaitGroup
	const goroutines = 10
	const iterations = 100

	// Concurrent OnReplyStart calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				controller.OnReplyStart()
			}
		}()
	}

	// Concurrent IsActive checks
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = controller.IsActive()
			}
		}()
	}

	// Concurrent RefreshTypingTTL calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				controller.RefreshTypingTTL()
			}
		}()
	}

	wg.Wait()

	// Should not have panicked and should have exactly 1 call
	// (OnReplyStart is idempotent after started)
	count := atomic.LoadInt32(&callCount)
	if count != 1 {
		t.Errorf("expected exactly 1 call (idempotent), got %d", count)
	}
}

func TestTypingController_RunCompleteBlocksRestart(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	controller.OnReplyStart()
	if atomic.LoadInt32(&callCount) != 1 {
		t.Error("expected initial call")
	}

	// Mark run complete
	controller.MarkRunComplete()

	// Try to start again after run complete (simulating late callback)
	controller.OnReplyStart()
	controller.StartTypingLoop()

	count := atomic.LoadInt32(&callCount)
	if count != 1 {
		t.Errorf("expected no additional calls after runComplete, got %d", count)
	}
}

func TestTypingController_NoCallbackNoLoop(t *testing.T) {
	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart:          nil, // No callback
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	controller.typingIntervalMs = 10 * time.Millisecond

	// Should not panic and should not start loop
	controller.StartTypingLoop()

	time.Sleep(50 * time.Millisecond)

	// typingTimer should still be nil since no callback
	controller.mu.Lock()
	hasTimer := controller.typingTimer != nil
	controller.mu.Unlock()

	if hasTimer {
		t.Error("expected no timer when OnReplyStart is nil")
	}
}

func TestIsSilentReplyText(t *testing.T) {
	tests := []struct {
		text   string
		token  string
		silent bool
	}{
		// Token at start
		{"NO_REPLY rest of text", "NO_REPLY", true},
		{"NO_REPLY", "NO_REPLY", true},
		{"  NO_REPLY something", "NO_REPLY", true},

		// Token at end
		{"some text NO_REPLY", "NO_REPLY", true},
		{"some text NO_REPLY  ", "NO_REPLY", true},
		{"text NO_REPLY.", "NO_REPLY", true},

		// Token in middle (should not match - only prefix and suffix are checked)
		{"some NO_REPLY text", "NO_REPLY", false},

		// No token
		{"Hello world", "NO_REPLY", false},
		{"", "NO_REPLY", false},

		// Different token
		{"SILENT start", "SILENT", true},
		{"end SILENT", "SILENT", true},

		// Partial match (should not be silent)
		{"NO_REPLYING", "NO_REPLY", false},
		{"PRENO_REPLY", "NO_REPLY", false},
	}

	for _, tt := range tests {
		result := isSilentReplyText(tt.text, tt.token)
		if result != tt.silent {
			t.Errorf("isSilentReplyText(%q, %q) = %v, want %v", tt.text, tt.token, result, tt.silent)
		}
	}
}

func TestFormatTypingTTL(t *testing.T) {
	tests := []struct {
		ms       int
		expected string
	}{
		{60000, "1m"},
		{120000, "2m"},
		{30000, "30s"},
		{5000, "5s"},
		{90000, "90s"},
	}

	for _, tt := range tests {
		result := formatTypingTTL(tt.ms)
		if result != tt.expected {
			t.Errorf("formatTypingTTL(%d) = %q, want %q", tt.ms, result, tt.expected)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.TypingIntervalSeconds != DefaultTypingIntervalSeconds {
		t.Errorf("TypingIntervalSeconds = %d, want %d", config.TypingIntervalSeconds, DefaultTypingIntervalSeconds)
	}
	if config.TypingTTLMs != DefaultTypingTTLMs {
		t.Errorf("TypingTTLMs = %d, want %d", config.TypingTTLMs, DefaultTypingTTLMs)
	}
	if config.SilentToken != DefaultSilentToken {
		t.Errorf("SilentToken = %q, want %q", config.SilentToken, DefaultSilentToken)
	}
}

func TestTypingController_StartTypingOnTextNormal(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	controller.typingIntervalMs = 50 * time.Millisecond

	controller.StartTypingOnText("Hello, I'm responding!")

	if !controller.IsActive() {
		t.Error("expected controller to be active")
	}

	time.Sleep(100 * time.Millisecond)

	count := atomic.LoadInt32(&callCount)
	if count < 1 {
		t.Errorf("expected at least 1 call, got %d", count)
	}
}

func TestTypingController_NegativeIntervalDisablesLoop(t *testing.T) {
	var callCount int32

	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart: func() {
			atomic.AddInt32(&callCount, 1)
		},
		TypingIntervalSeconds: -1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})
	defer controller.Cleanup()

	// Negative interval should be replaced with default
	if controller.config.TypingIntervalSeconds != DefaultTypingIntervalSeconds {
		t.Errorf("expected default interval, got %d", controller.config.TypingIntervalSeconds)
	}
}

func TestTypingController_IsActiveAfterCleanup(t *testing.T) {
	controller := NewTypingController(&TypingControllerConfig{
		OnReplyStart:          func() {},
		TypingIntervalSeconds: 1,
		TypingTTLMs:           5000,
		SilentToken:           "NO_REPLY",
	})

	controller.OnReplyStart()

	if !controller.IsActive() {
		t.Error("expected active before cleanup")
	}

	controller.Cleanup()

	if controller.IsActive() {
		t.Error("expected inactive after cleanup")
	}
}
