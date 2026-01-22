package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// MessageDebouncer batches rapid incoming messages from the same session
// before processing them. This prevents the agent from being overwhelmed
// by users who send multiple quick messages in succession.
type MessageDebouncer struct {
	// debounceMs is the delay to wait for additional messages
	debounceMs time.Duration

	// maxWaitMs is the maximum time to wait before forcing a flush
	maxWaitMs time.Duration

	// onFlush is called when messages are ready to be processed
	onFlush func(ctx context.Context, messages []*models.Message) error

	// onError is called when flush encounters an error
	onError func(err error, messages []*models.Message)

	mu      sync.Mutex
	buffers map[string]*debounceBuffer
	closed  bool
}

type debounceBuffer struct {
	messages   []*models.Message
	timer      *time.Timer
	firstSeen  time.Time
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// DebounceConfig configures the message debouncer.
type DebounceConfig struct {
	// DebounceMs is the delay to wait for additional messages (default: 500ms)
	DebounceMs int `yaml:"debounce_ms"`

	// MaxWaitMs is the maximum time to batch messages (default: 2000ms)
	MaxWaitMs int `yaml:"max_wait_ms"`

	// Enabled controls whether debouncing is active (default: true)
	Enabled bool `yaml:"enabled"`

	// ByChannel allows per-channel debounce configuration
	ByChannel map[string]int `yaml:"by_channel"`
}

// DefaultDebounceConfig returns sensible defaults for message debouncing.
func DefaultDebounceConfig() DebounceConfig {
	return DebounceConfig{
		DebounceMs: 500,
		MaxWaitMs:  2000,
		Enabled:    true,
	}
}

// NewMessageDebouncer creates a new message debouncer.
func NewMessageDebouncer(
	debounceMs time.Duration,
	maxWaitMs time.Duration,
	onFlush func(ctx context.Context, messages []*models.Message) error,
) *MessageDebouncer {
	if debounceMs <= 0 {
		debounceMs = 500 * time.Millisecond
	}
	if maxWaitMs <= 0 {
		maxWaitMs = 2000 * time.Millisecond
	}

	return &MessageDebouncer{
		debounceMs: debounceMs,
		maxWaitMs:  maxWaitMs,
		onFlush:    onFlush,
		buffers:    make(map[string]*debounceBuffer),
	}
}

// SetErrorHandler sets the error handler for flush failures.
func (d *MessageDebouncer) SetErrorHandler(handler func(err error, messages []*models.Message)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onError = handler
}

// Enqueue adds a message to be processed, potentially batching it with others.
// The key determines which messages get batched together (usually session key).
func (d *MessageDebouncer) Enqueue(ctx context.Context, key string, msg *models.Message) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}

	buf, exists := d.buffers[key]
	if exists {
		buf.messages = append(buf.messages, msg)
		d.resetTimer(key, buf)
		return
	}

	// Create new buffer
	bufCtx, cancel := context.WithCancel(ctx)
	buf = &debounceBuffer{
		messages:   []*models.Message{msg},
		firstSeen:  time.Now(),
		ctx:        bufCtx,
		cancelFunc: cancel,
	}
	d.buffers[key] = buf
	d.scheduleFlush(key, buf)
}

// EnqueueImmediate processes a message immediately without debouncing.
// This is useful for priority messages or commands.
func (d *MessageDebouncer) EnqueueImmediate(ctx context.Context, key string, msg *models.Message) error {
	d.mu.Lock()
	// Flush any pending messages for this key first
	if buf, exists := d.buffers[key]; exists {
		d.flushBufferLocked(key, buf)
	}
	d.mu.Unlock()

	// Process the new message immediately
	return d.onFlush(ctx, []*models.Message{msg})
}

// Flush immediately processes all pending messages for a key.
func (d *MessageDebouncer) Flush(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if buf, exists := d.buffers[key]; exists {
		d.flushBufferLocked(key, buf)
	}
}

// FlushAll immediately processes all pending messages.
func (d *MessageDebouncer) FlushAll() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for key, buf := range d.buffers {
		d.flushBufferLocked(key, buf)
	}
}

// Close stops the debouncer and flushes all pending messages.
func (d *MessageDebouncer) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()

	d.FlushAll()
}

// PendingCount returns the number of keys with pending messages.
func (d *MessageDebouncer) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.buffers)
}

// PendingMessages returns the number of pending messages for a key.
func (d *MessageDebouncer) PendingMessages(key string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if buf, exists := d.buffers[key]; exists {
		return len(buf.messages)
	}
	return 0
}

func (d *MessageDebouncer) scheduleFlush(key string, buf *debounceBuffer) {
	if buf.timer != nil {
		buf.timer.Stop()
	}

	// Calculate delay, respecting maxWait
	delay := d.debounceMs
	elapsed := time.Since(buf.firstSeen)
	remaining := d.maxWaitMs - elapsed
	if remaining < delay {
		delay = remaining
	}
	if delay <= 0 {
		delay = time.Millisecond // Minimum delay
	}

	buf.timer = time.AfterFunc(delay, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if currentBuf, exists := d.buffers[key]; exists && currentBuf == buf {
			d.flushBufferLocked(key, buf)
		}
	})
}

func (d *MessageDebouncer) resetTimer(key string, buf *debounceBuffer) {
	// Check if we've exceeded maxWait
	if time.Since(buf.firstSeen) >= d.maxWaitMs {
		d.flushBufferLocked(key, buf)
		return
	}
	d.scheduleFlush(key, buf)
}

func (d *MessageDebouncer) flushBufferLocked(key string, buf *debounceBuffer) {
	delete(d.buffers, key)

	if buf.timer != nil {
		buf.timer.Stop()
		buf.timer = nil
	}

	if len(buf.messages) == 0 {
		buf.cancelFunc()
		return
	}

	messages := buf.messages
	ctx := buf.ctx

	// Process async to avoid holding lock
	go func() {
		defer buf.cancelFunc()
		if err := d.onFlush(ctx, messages); err != nil && d.onError != nil {
			d.onError(err, messages)
		}
	}()
}

// ShouldDebounce determines if a message should be debounced.
// Control messages and commands are typically not debounced.
func ShouldDebounce(msg *models.Message) bool {
	if msg == nil {
		return false
	}

	// Don't debounce if message has a command prefix
	text := msg.Content
	if len(text) > 0 && (text[0] == '/' || text[0] == '!') {
		return false
	}

	return true
}

// BuildDebounceKey creates a key for batching messages.
// Messages with the same key will be batched together.
func BuildDebounceKey(msg *models.Message) string {
	if msg == nil {
		return ""
	}

	// Key by channel + session for conversation-based batching
	return msg.ChannelID + ":" + msg.SessionID
}
