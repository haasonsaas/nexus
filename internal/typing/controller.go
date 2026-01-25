// Package typing provides a typing indicator controller for managing
// typing state during async reply processing.
package typing

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultTypingIntervalSeconds is the default interval for typing indicator refresh.
const DefaultTypingIntervalSeconds = 6

// DefaultTypingTTLMs is the default time-to-live for typing indicators (2 minutes).
const DefaultTypingTTLMs = 2 * 60 * 1000

// DefaultSilentToken is the default token that suppresses typing indicators.
const DefaultSilentToken = "NO_REPLY"

// OnReplyStartFunc is called when a reply starts to trigger typing indicator.
type OnReplyStartFunc func()

// LogFunc is called for logging messages.
type LogFunc func(message string)

// TypingControllerConfig configures the typing controller behavior.
type TypingControllerConfig struct {
	// OnReplyStart is called to trigger the typing indicator.
	OnReplyStart OnReplyStartFunc

	// TypingIntervalSeconds is the interval between typing indicator refreshes.
	// Default: 6 seconds
	TypingIntervalSeconds int

	// TypingTTLMs is the time-to-live for typing indicators in milliseconds.
	// After this time without activity, typing stops automatically.
	// Default: 2 minutes (120000 ms)
	TypingTTLMs int

	// SilentToken is a token that, when present in text, suppresses typing.
	// Default: "NO_REPLY"
	SilentToken string

	// Log is called for logging messages.
	Log LogFunc
}

// DefaultConfig returns the default typing controller configuration.
func DefaultConfig() *TypingControllerConfig {
	return &TypingControllerConfig{
		TypingIntervalSeconds: DefaultTypingIntervalSeconds,
		TypingTTLMs:           DefaultTypingTTLMs,
		SilentToken:           DefaultSilentToken,
	}
}

// TypingController manages typing indicator state during reply processing.
//
// The controller coordinates typing state across multiple events:
//   - Reply start triggers initial typing indicator
//   - Tool executions refresh the typing loop
//   - TTL timer stops typing after extended inactivity
//   - Run completion and dispatch idle events trigger cleanup
//
// The controller uses a "sealed" state to prevent late callbacks
// from restarting typing after cleanup. This is important because
// callbacks can fire late due to async event handling.
type TypingController struct {
	mu sync.Mutex

	config *TypingControllerConfig

	// State flags
	started      bool
	active       bool
	runComplete  bool
	dispatchIdle bool
	sealed       bool

	// Timers
	typingTimer    *time.Ticker
	typingTTLTimer *time.Timer
	stopCh         chan struct{}
	doneCh         chan struct{}

	// Derived values
	typingIntervalMs time.Duration
	typingTTLMs      time.Duration
}

// NewTypingController creates a new typing controller with the given configuration.
// If config is nil, DefaultConfig is used.
func NewTypingController(config *TypingControllerConfig) *TypingController {
	if config == nil {
		config = DefaultConfig()
	}

	// Apply defaults for zero values
	if config.TypingIntervalSeconds <= 0 {
		config.TypingIntervalSeconds = DefaultTypingIntervalSeconds
	}
	if config.TypingTTLMs <= 0 {
		config.TypingTTLMs = DefaultTypingTTLMs
	}
	if config.SilentToken == "" {
		config.SilentToken = DefaultSilentToken
	}

	return &TypingController{
		config:           config,
		typingIntervalMs: time.Duration(config.TypingIntervalSeconds) * time.Second,
		typingTTLMs:      time.Duration(config.TypingTTLMs) * time.Millisecond,
	}
}

// OnReplyStart triggers the typing indicator when a reply starts.
// This is called at the beginning of a reply to ensure typing is shown.
func (c *TypingController) OnReplyStart() {
	c.ensureStart()
}

// StartTypingLoop starts the periodic typing indicator refresh loop.
// This should be called when tool execution begins to keep typing active.
func (c *TypingController) StartTypingLoop() {
	c.mu.Lock()
	if c.sealed {
		c.mu.Unlock()
		return
	}
	if c.runComplete {
		c.mu.Unlock()
		return
	}

	// Always refresh TTL when called, even if loop already running.
	// This keeps typing alive during long tool executions.
	c.refreshTypingTTLLocked()

	if c.config.OnReplyStart == nil {
		c.mu.Unlock()
		return
	}
	if c.typingIntervalMs <= 0 {
		c.mu.Unlock()
		return
	}
	if c.typingTimer != nil {
		c.mu.Unlock()
		return
	}

	c.ensureStartLocked()

	c.stopCh = make(chan struct{})
	c.doneCh = make(chan struct{})
	c.typingTimer = time.NewTicker(c.typingIntervalMs)
	c.mu.Unlock()

	go c.runTypingLoop()
}

// runTypingLoop is the goroutine that refreshes typing at intervals.
func (c *TypingController) runTypingLoop() {
	defer func() {
		c.mu.Lock()
		if c.typingTimer != nil {
			c.typingTimer.Stop()
			c.typingTimer = nil
		}
		close(c.doneCh)
		c.mu.Unlock()
	}()

	for {
		select {
		case <-c.stopCh:
			return
		case <-c.typingTimer.C:
			c.triggerTyping()
		}
	}
}

// StartTypingOnText starts the typing loop if the text is not a silent reply.
// This is used when processing text content to conditionally show typing.
func (c *TypingController) StartTypingOnText(text string) {
	c.mu.Lock()
	if c.sealed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if c.config.SilentToken != "" && isSilentReplyText(trimmed, c.config.SilentToken) {
		return
	}

	c.mu.Lock()
	c.refreshTypingTTLLocked()
	c.mu.Unlock()

	c.StartTypingLoop()
}

// RefreshTypingTTL resets the TTL timer to extend typing duration.
// Call this when activity occurs to prevent premature typing stop.
func (c *TypingController) RefreshTypingTTL() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshTypingTTLLocked()
}

// refreshTypingTTLLocked resets the TTL timer while holding the lock.
func (c *TypingController) refreshTypingTTLLocked() {
	if c.sealed {
		return
	}
	if c.typingIntervalMs <= 0 {
		return
	}
	if c.typingTTLMs <= 0 {
		return
	}

	if c.typingTTLTimer != nil {
		c.typingTTLTimer.Stop()
	}

	c.typingTTLTimer = time.AfterFunc(c.typingTTLMs, func() {
		c.mu.Lock()
		if c.typingTimer == nil {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		c.log("typing TTL reached (" + formatTypingTTL(int(c.typingTTLMs.Milliseconds())) + "); stopping typing indicator")
		c.Cleanup()
	})
}

// IsActive returns true if typing is currently active and not sealed.
func (c *TypingController) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active && !c.sealed
}

// MarkRunComplete signals that the model run has completed.
// Typing stops when both run is complete and dispatch is idle.
func (c *TypingController) MarkRunComplete() {
	c.mu.Lock()
	c.runComplete = true
	c.maybeStopOnIdleLocked()
	c.mu.Unlock()
}

// MarkDispatchIdle signals that the dispatch queue is empty.
// Typing stops when both run is complete and dispatch is idle.
func (c *TypingController) MarkDispatchIdle() {
	c.mu.Lock()
	c.dispatchIdle = true
	c.maybeStopOnIdleLocked()
	c.mu.Unlock()
}

// Cleanup stops all timers and seals the controller.
// After cleanup, the controller cannot be restarted.
func (c *TypingController) Cleanup() {
	c.mu.Lock()
	if c.sealed {
		c.mu.Unlock()
		return
	}
	c.cleanupLocked()
	c.mu.Unlock()
}

// cleanupLocked performs cleanup while holding the lock.
func (c *TypingController) cleanupLocked() {
	if c.typingTTLTimer != nil {
		c.typingTTLTimer.Stop()
		c.typingTTLTimer = nil
	}

	if c.stopCh != nil {
		close(c.stopCh)
		c.stopCh = nil
	}

	c.resetCycleLocked()
	c.sealed = true
}

// resetCycleLocked resets the state flags while holding the lock.
func (c *TypingController) resetCycleLocked() {
	c.started = false
	c.active = false
	c.runComplete = false
	c.dispatchIdle = false
}

// ensureStart triggers the initial typing indicator if not already started.
func (c *TypingController) ensureStart() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureStartLocked()
}

// ensureStartLocked triggers initial typing while holding the lock.
func (c *TypingController) ensureStartLocked() {
	if c.sealed {
		return
	}
	// Late callbacks after a run completed should never restart typing.
	if c.runComplete {
		return
	}
	if !c.active {
		c.active = true
	}
	if c.started {
		return
	}
	c.started = true
	c.triggerTypingLocked()
}

// triggerTyping calls the typing callback.
func (c *TypingController) triggerTyping() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.triggerTypingLocked()
}

// triggerTypingLocked calls the typing callback while holding the lock.
func (c *TypingController) triggerTypingLocked() {
	if c.sealed {
		return
	}
	if c.config.OnReplyStart != nil {
		c.config.OnReplyStart()
	}
}

// maybeStopOnIdleLocked checks if typing should stop and performs cleanup.
func (c *TypingController) maybeStopOnIdleLocked() {
	if !c.active {
		return
	}
	// Stop only when the model run is done and the dispatcher queue is empty.
	if c.runComplete && c.dispatchIdle {
		c.cleanupLocked()
	}
}

// log logs a message if a log function is configured.
func (c *TypingController) log(message string) {
	if c.config.Log != nil {
		c.config.Log(message)
	}
}

// IsSealed returns true if the controller has been sealed.
func (c *TypingController) IsSealed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sealed
}

// formatTypingTTL formats the TTL duration for logging.
func formatTypingTTL(ms int) string {
	if ms%60000 == 0 {
		return string(rune('0'+ms/60000)) + "m"
	}
	seconds := ms / 1000
	if seconds < 10 {
		return string(rune('0'+seconds)) + "s"
	}
	return formatInt(seconds) + "s"
}

// formatInt formats an integer as a string.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + formatInt(-n)
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

// isSilentReplyText checks if text contains the silent token.
// The token can appear at the start or end of the text.
func isSilentReplyText(text string, token string) bool {
	if text == "" {
		return false
	}

	escaped := regexp.QuoteMeta(token)

	// Check for token at prefix: ^\s*TOKEN(?=$|\W)
	prefixPattern := regexp.MustCompile(`^\s*` + escaped + `(?:$|\W)`)
	if prefixPattern.MatchString(text) {
		return true
	}

	// Check for token at suffix: \bTOKEN\b\W*$
	suffixPattern := regexp.MustCompile(`\b` + escaped + `\b\W*$`)
	return suffixPattern.MatchString(text)
}
