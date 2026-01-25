// Package debounce provides utilities for batching and debouncing messages.
package debounce

import (
	"sync"
	"time"
)

// DebounceConfig holds configuration for debouncing inbound messages.
type DebounceConfig struct {
	// DebounceMs is the base debounce delay in milliseconds.
	DebounceMs int

	// ByChannel maps channel identifiers to channel-specific debounce delays.
	ByChannel map[string]int
}

// ResolveDebounceMs resolves the effective debounce duration using the
// priority: override > byChannel > base. Returns a time.Duration.
func ResolveDebounceMs(config DebounceConfig, channel string, override *int) time.Duration {
	// Priority 1: explicit override
	if override != nil && *override >= 0 {
		return time.Duration(*override) * time.Millisecond
	}

	// Priority 2: channel-specific setting
	if config.ByChannel != nil {
		if ms, ok := config.ByChannel[channel]; ok && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}

	// Priority 3: base config
	if config.DebounceMs >= 0 {
		return time.Duration(config.DebounceMs) * time.Millisecond
	}

	return 0
}

// DebounceBuffer holds pending items and their flush timer.
type DebounceBuffer[T any] struct {
	Items []*T
	Timer *time.Timer
}

// Debouncer batches items by key and flushes them after a debounce delay.
type Debouncer[T any] struct {
	mu      sync.Mutex
	buffers map[string]*DebounceBuffer[T]
	stopped bool

	debounceMs     time.Duration
	buildKey       func(item *T) string
	shouldDebounce func(item *T) bool
	onFlush        func(items []*T) error
	onError        func(err error, items []*T)
}

// DebouncerOption configures a Debouncer.
type DebouncerOption[T any] func(*Debouncer[T])

// WithDebounceMs sets the debounce delay.
func WithDebounceMs[T any](ms int) DebouncerOption[T] {
	return func(d *Debouncer[T]) {
		if ms < 0 {
			ms = 0
		}
		d.debounceMs = time.Duration(ms) * time.Millisecond
	}
}

// WithDebounceDuration sets the debounce delay as a duration.
func WithDebounceDuration[T any](dur time.Duration) DebouncerOption[T] {
	return func(d *Debouncer[T]) {
		if dur < 0 {
			dur = 0
		}
		d.debounceMs = dur
	}
}

// WithBuildKey sets the function to generate grouping keys for items.
func WithBuildKey[T any](fn func(item *T) string) DebouncerOption[T] {
	return func(d *Debouncer[T]) {
		d.buildKey = fn
	}
}

// WithShouldDebounce sets an optional predicate to determine if an item
// should be debounced. If nil or returns true, the item is debounced.
func WithShouldDebounce[T any](fn func(item *T) bool) DebouncerOption[T] {
	return func(d *Debouncer[T]) {
		d.shouldDebounce = fn
	}
}

// WithOnFlush sets the callback invoked when items are flushed.
func WithOnFlush[T any](fn func(items []*T) error) DebouncerOption[T] {
	return func(d *Debouncer[T]) {
		d.onFlush = fn
	}
}

// WithOnError sets an optional callback for flush errors.
func WithOnError[T any](fn func(err error, items []*T)) DebouncerOption[T] {
	return func(d *Debouncer[T]) {
		d.onError = fn
	}
}

// NewDebouncer creates a new Debouncer with the given options.
func NewDebouncer[T any](opts ...DebouncerOption[T]) *Debouncer[T] {
	d := &Debouncer[T]{
		buffers: make(map[string]*DebounceBuffer[T]),
	}

	for _, opt := range opts {
		opt(d)
	}

	// Provide default buildKey if not set
	if d.buildKey == nil {
		d.buildKey = func(item *T) string {
			return "default"
		}
	}

	// Provide default onFlush if not set
	if d.onFlush == nil {
		d.onFlush = func(items []*T) error {
			return nil
		}
	}

	return d
}

// Enqueue adds an item to the debouncer. If debouncing is disabled or
// shouldDebounce returns false, the item is flushed immediately.
func (d *Debouncer[T]) Enqueue(item *T) {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}

	key := d.buildKey(item)
	canDebounce := d.debounceMs > 0 && (d.shouldDebounce == nil || d.shouldDebounce(item))

	if !canDebounce || key == "" {
		// Flush any existing buffer for this key first
		if key != "" {
			if buf, exists := d.buffers[key]; exists {
				d.flushBufferLocked(key, buf)
			}
		}
		d.mu.Unlock()

		// Flush the single item immediately
		d.flushItems([]*T{item})
		return
	}

	existing, exists := d.buffers[key]
	if exists {
		existing.Items = append(existing.Items, item)
		// Reset the timer
		if existing.Timer != nil {
			existing.Timer.Stop()
		}
		existing.Timer = time.AfterFunc(d.debounceMs, func() {
			d.flushKeyInternal(key)
		})
		d.mu.Unlock()
		return
	}

	// Create new buffer
	buf := &DebounceBuffer[T]{
		Items: []*T{item},
	}
	buf.Timer = time.AfterFunc(d.debounceMs, func() {
		d.flushKeyInternal(key)
	})
	d.buffers[key] = buf
	d.mu.Unlock()
}

// FlushKey manually flushes all pending items for the given key.
func (d *Debouncer[T]) FlushKey(key string) {
	d.flushKeyInternal(key)
}

// flushKeyInternal handles the actual flushing logic.
func (d *Debouncer[T]) flushKeyInternal(key string) {
	d.mu.Lock()
	buf, exists := d.buffers[key]
	if !exists || d.stopped {
		d.mu.Unlock()
		return
	}

	d.flushBufferLocked(key, buf)
	d.mu.Unlock()
}

// flushBufferLocked removes the buffer and calls flushItems.
// Must be called with d.mu held.
func (d *Debouncer[T]) flushBufferLocked(key string, buf *DebounceBuffer[T]) {
	delete(d.buffers, key)
	if buf.Timer != nil {
		buf.Timer.Stop()
		buf.Timer = nil
	}

	if len(buf.Items) == 0 {
		return
	}

	items := buf.Items
	buf.Items = nil

	// Release lock before calling flush
	d.mu.Unlock()
	d.flushItems(items)
	d.mu.Lock()
}

// flushItems invokes onFlush and handles errors.
func (d *Debouncer[T]) flushItems(items []*T) {
	if len(items) == 0 {
		return
	}

	err := d.onFlush(items)
	if err != nil && d.onError != nil {
		d.onError(err, items)
	}
}

// Stop stops all pending timers and prevents further processing.
func (d *Debouncer[T]) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stopped = true

	for key, buf := range d.buffers {
		if buf.Timer != nil {
			buf.Timer.Stop()
			buf.Timer = nil
		}
		delete(d.buffers, key)
	}
}

// PendingCount returns the number of keys with pending items.
func (d *Debouncer[T]) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.buffers)
}

// PendingItems returns the total number of pending items across all keys.
func (d *Debouncer[T]) PendingItems() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for _, buf := range d.buffers {
		count += len(buf.Items)
	}
	return count
}
