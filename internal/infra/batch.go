package infra

import (
	"context"
	"sync"
	"time"
)

// BatchProcessor collects items and processes them in batches.
type BatchProcessor[T any, R any] struct {
	mu sync.Mutex

	// Configuration
	config BatchConfig

	// Current batch state
	items     []batchItem[T, R]
	timer     *time.Timer
	processing bool

	// The batch processing function
	processFn func(context.Context, []T) ([]R, error)
}

// BatchConfig configures a batch processor.
type BatchConfig struct {
	// MaxSize is the maximum batch size before automatic flush.
	MaxSize int

	// MaxWait is the maximum time to wait before flushing.
	MaxWait time.Duration

	// MinSize is the minimum batch size before flushing (unless MaxWait exceeded).
	MinSize int
}

type batchItem[T any, R any] struct {
	item     T
	resultCh chan batchResult[R]
}

type batchResult[R any] struct {
	result R
	err    error
}

// NewBatchProcessor creates a new batch processor.
func NewBatchProcessor[T any, R any](
	config BatchConfig,
	processFn func(context.Context, []T) ([]R, error),
) *BatchProcessor[T, R] {
	if config.MaxSize <= 0 {
		config.MaxSize = 100
	}
	if config.MaxWait <= 0 {
		config.MaxWait = 100 * time.Millisecond
	}

	return &BatchProcessor[T, R]{
		config:    config,
		items:     make([]batchItem[T, R], 0, config.MaxSize),
		processFn: processFn,
	}
}

// Submit adds an item to the batch and waits for the result.
func (bp *BatchProcessor[T, R]) Submit(ctx context.Context, item T) (R, error) {
	resultCh := make(chan batchResult[R], 1)

	bp.mu.Lock()
	bp.items = append(bp.items, batchItem[T, R]{item: item, resultCh: resultCh})

	shouldFlush := len(bp.items) >= bp.config.MaxSize

	// Start timer on first item
	if len(bp.items) == 1 && bp.timer == nil {
		bp.timer = time.AfterFunc(bp.config.MaxWait, func() {
			bp.flush()
		})
	}

	bp.mu.Unlock()

	if shouldFlush {
		bp.flush()
	}

	// Wait for result
	select {
	case <-ctx.Done():
		var zero R
		return zero, ctx.Err()
	case result := <-resultCh:
		return result.result, result.err
	}
}

// SubmitAsync adds an item to the batch and returns a channel for the result.
func (bp *BatchProcessor[T, R]) SubmitAsync(item T) <-chan batchResult[R] {
	resultCh := make(chan batchResult[R], 1)

	bp.mu.Lock()
	bp.items = append(bp.items, batchItem[T, R]{item: item, resultCh: resultCh})

	shouldFlush := len(bp.items) >= bp.config.MaxSize

	// Start timer on first item
	if len(bp.items) == 1 && bp.timer == nil {
		bp.timer = time.AfterFunc(bp.config.MaxWait, func() {
			bp.flush()
		})
	}

	bp.mu.Unlock()

	if shouldFlush {
		bp.flush()
	}

	return resultCh
}

// Flush immediately processes any pending items.
func (bp *BatchProcessor[T, R]) Flush() {
	bp.flush()
}

// flush processes the current batch.
func (bp *BatchProcessor[T, R]) flush() {
	bp.mu.Lock()

	// Already processing or nothing to process
	if bp.processing || len(bp.items) == 0 {
		bp.mu.Unlock()
		return
	}

	// Check minimum size (unless timer expired)
	if len(bp.items) < bp.config.MinSize && bp.timer != nil {
		bp.mu.Unlock()
		return
	}

	// Stop timer
	if bp.timer != nil {
		bp.timer.Stop()
		bp.timer = nil
	}

	// Take the batch
	items := bp.items
	bp.items = make([]batchItem[T, R], 0, bp.config.MaxSize)
	bp.processing = true

	bp.mu.Unlock()

	// Process the batch
	go bp.processBatch(items)
}

// processBatch processes a batch of items.
func (bp *BatchProcessor[T, R]) processBatch(items []batchItem[T, R]) {
	// Extract items for processing
	toProcess := make([]T, len(items))
	for i, item := range items {
		toProcess[i] = item.item
	}

	// Process the batch
	results, err := bp.processFn(context.Background(), toProcess)

	// Distribute results
	for i, item := range items {
		if err != nil {
			item.resultCh <- batchResult[R]{err: err}
		} else if i < len(results) {
			item.resultCh <- batchResult[R]{result: results[i]}
		} else {
			// No result for this item
			var zero R
			item.resultCh <- batchResult[R]{result: zero}
		}
		close(item.resultCh)
	}

	// Mark processing as done and check for more items
	bp.mu.Lock()
	bp.processing = false
	hasMore := len(bp.items) >= bp.config.MaxSize
	bp.mu.Unlock()

	// Process any accumulated items
	if hasMore {
		bp.flush()
	}
}

// Pending returns the number of pending items.
func (bp *BatchProcessor[T, R]) Pending() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return len(bp.items)
}

// SimpleBatchProcessor is a batch processor without individual results.
type SimpleBatchProcessor[T any] struct {
	mu sync.Mutex

	// Configuration
	config BatchConfig

	// Current batch state
	items     []T
	timer     *time.Timer
	processing bool

	// The batch processing function
	processFn func(context.Context, []T) error
}

// NewSimpleBatchProcessor creates a batch processor without per-item results.
func NewSimpleBatchProcessor[T any](
	config BatchConfig,
	processFn func(context.Context, []T) error,
) *SimpleBatchProcessor[T] {
	if config.MaxSize <= 0 {
		config.MaxSize = 100
	}
	if config.MaxWait <= 0 {
		config.MaxWait = 100 * time.Millisecond
	}

	return &SimpleBatchProcessor[T]{
		config:    config,
		items:     make([]T, 0, config.MaxSize),
		processFn: processFn,
	}
}

// Add adds an item to the batch.
func (bp *SimpleBatchProcessor[T]) Add(item T) {
	bp.mu.Lock()
	bp.items = append(bp.items, item)
	shouldFlush := len(bp.items) >= bp.config.MaxSize

	// Start timer on first item
	if len(bp.items) == 1 && bp.timer == nil {
		bp.timer = time.AfterFunc(bp.config.MaxWait, func() {
			bp.flush()
		})
	}

	bp.mu.Unlock()

	if shouldFlush {
		bp.flush()
	}
}

// AddMany adds multiple items to the batch.
func (bp *SimpleBatchProcessor[T]) AddMany(items []T) {
	bp.mu.Lock()
	bp.items = append(bp.items, items...)
	shouldFlush := len(bp.items) >= bp.config.MaxSize

	// Start timer on first item
	if bp.timer == nil && len(bp.items) > 0 {
		bp.timer = time.AfterFunc(bp.config.MaxWait, func() {
			bp.flush()
		})
	}

	bp.mu.Unlock()

	if shouldFlush {
		bp.flush()
	}
}

// Flush immediately processes any pending items.
func (bp *SimpleBatchProcessor[T]) Flush() {
	bp.flush()
}

// flush processes the current batch.
func (bp *SimpleBatchProcessor[T]) flush() {
	bp.mu.Lock()

	// Already processing or nothing to process
	if bp.processing || len(bp.items) == 0 {
		bp.mu.Unlock()
		return
	}

	// Stop timer
	if bp.timer != nil {
		bp.timer.Stop()
		bp.timer = nil
	}

	// Take the batch
	items := bp.items
	bp.items = make([]T, 0, bp.config.MaxSize)
	bp.processing = true

	bp.mu.Unlock()

	// Process the batch
	go bp.processBatch(items)
}

// processBatch processes a batch of items.
func (bp *SimpleBatchProcessor[T]) processBatch(items []T) {
	defer func() {
		bp.mu.Lock()
		bp.processing = false
		bp.mu.Unlock()
	}()

	_ = bp.processFn(context.Background(), items)
}

// Pending returns the number of pending items.
func (bp *SimpleBatchProcessor[T]) Pending() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return len(bp.items)
}

// BatchAggregator collects items and aggregates them periodically.
type BatchAggregator[K comparable, V any] struct {
	mu sync.Mutex

	// Configuration
	interval time.Duration

	// Current state
	data    map[K]V
	timer   *time.Timer
	stopped bool

	// Aggregation function
	mergeFn func(existing, new V) V

	// Flush callback
	flushFn func(context.Context, map[K]V)
}

// NewBatchAggregator creates a new batch aggregator.
func NewBatchAggregator[K comparable, V any](
	interval time.Duration,
	mergeFn func(existing, new V) V,
	flushFn func(context.Context, map[K]V),
) *BatchAggregator[K, V] {
	if interval <= 0 {
		interval = time.Second
	}

	return &BatchAggregator[K, V]{
		interval: interval,
		data:     make(map[K]V),
		mergeFn:  mergeFn,
		flushFn:  flushFn,
	}
}

// Add adds or merges a value for the key.
func (ba *BatchAggregator[K, V]) Add(key K, value V) {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	if ba.stopped {
		return
	}

	if existing, ok := ba.data[key]; ok {
		ba.data[key] = ba.mergeFn(existing, value)
	} else {
		ba.data[key] = value
	}

	// Start timer on first item
	if ba.timer == nil {
		ba.timer = time.AfterFunc(ba.interval, func() {
			ba.flush()
		})
	}
}

// Flush immediately processes accumulated data.
func (ba *BatchAggregator[K, V]) Flush() {
	ba.flush()
}

// flush processes the accumulated data.
func (ba *BatchAggregator[K, V]) flush() {
	ba.mu.Lock()

	if len(ba.data) == 0 {
		if ba.timer != nil {
			ba.timer.Stop()
			ba.timer = nil
		}
		ba.mu.Unlock()
		return
	}

	// Stop timer
	if ba.timer != nil {
		ba.timer.Stop()
		ba.timer = nil
	}

	// Take the data
	data := ba.data
	ba.data = make(map[K]V)

	ba.mu.Unlock()

	// Flush in goroutine
	go ba.flushFn(context.Background(), data)
}

// Stop stops the aggregator.
func (ba *BatchAggregator[K, V]) Stop() {
	ba.mu.Lock()
	ba.stopped = true
	if ba.timer != nil {
		ba.timer.Stop()
		ba.timer = nil
	}
	ba.mu.Unlock()

	// Final flush
	ba.flush()
}

// Count returns the number of keys in the current batch.
func (ba *BatchAggregator[K, V]) Count() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	return len(ba.data)
}
