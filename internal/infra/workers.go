package infra

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Job represents a unit of work to be processed.
type Job[T any] struct {
	ID      string
	Data    T
	Context context.Context
}

// JobResult contains the result of processing a job.
type JobResult[T, R any] struct {
	Job    Job[T]
	Result R
	Error  error
}

// WorkerPool manages a pool of workers that process jobs concurrently.
type WorkerPool[T, R any] struct {
	workers   int
	processor func(context.Context, T) (R, error)
	jobs      chan Job[T]
	results   chan JobResult[T, R]
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	started   atomic.Bool
	stopped   atomic.Bool

	// Statistics
	processed atomic.Uint64
	failed    atomic.Uint64
	queued    atomic.Int32
}

// WorkerPoolConfig configures a worker pool.
type WorkerPoolConfig[T, R any] struct {
	// Workers is the number of concurrent workers.
	Workers int
	// QueueSize is the maximum number of pending jobs.
	QueueSize int
	// Processor is the function that processes each job.
	Processor func(context.Context, T) (R, error)
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool[T, R any](config WorkerPoolConfig[T, R]) *WorkerPool[T, R] {
	if config.Workers <= 0 {
		config.Workers = 1
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 100
	}
	if config.Processor == nil {
		panic("workers: Processor is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool[T, R]{
		workers:   config.Workers,
		processor: config.Processor,
		jobs:      make(chan Job[T], config.QueueSize),
		results:   make(chan JobResult[T, R], config.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start starts the worker pool.
func (p *WorkerPool[T, R]) Start() {
	if !p.started.CompareAndSwap(false, true) {
		return
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Stop stops the worker pool gracefully.
func (p *WorkerPool[T, R]) Stop() {
	if !p.stopped.CompareAndSwap(false, true) {
		return
	}

	p.cancel()
	close(p.jobs)
	p.wg.Wait()
	close(p.results)
}

// Submit submits a job for processing.
// Returns false if the queue is full or the pool is stopped.
func (p *WorkerPool[T, R]) Submit(job Job[T]) bool {
	if p.stopped.Load() {
		return false
	}

	select {
	case p.jobs <- job:
		p.queued.Add(1)
		return true
	default:
		return false
	}
}

// SubmitWait submits a job and waits for the result.
func (p *WorkerPool[T, R]) SubmitWait(ctx context.Context, job Job[T]) (R, error) {
	wrappedJob := Job[T]{
		ID:      job.ID,
		Data:    job.Data,
		Context: job.Context,
	}

	if !p.Submit(wrappedJob) {
		var zero R
		return zero, context.DeadlineExceeded
	}

	// Watch for our result
	for {
		select {
		case <-ctx.Done():
			var zero R
			return zero, ctx.Err()
		case result, ok := <-p.results:
			if !ok {
				var zero R
				return zero, context.Canceled
			}
			if result.Job.ID == job.ID {
				return result.Result, result.Error
			}
			// Put back results that aren't ours
			select {
			case p.results <- result:
			default:
			}
		}
	}
}

// Results returns the results channel for consuming processed jobs.
func (p *WorkerPool[T, R]) Results() <-chan JobResult[T, R] {
	return p.results
}

// Stats returns pool statistics.
func (p *WorkerPool[T, R]) Stats() WorkerPoolStats {
	return WorkerPoolStats{
		Workers:   p.workers,
		Queued:    int(p.queued.Load()),
		Processed: p.processed.Load(),
		Failed:    p.failed.Load(),
		Running:   p.started.Load() && !p.stopped.Load(),
	}
}

// WorkerPoolStats contains pool statistics.
type WorkerPoolStats struct {
	Workers   int
	Queued    int
	Processed uint64
	Failed    uint64
	Running   bool
}

func (p *WorkerPool[T, R]) worker(_ int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			p.queued.Add(-1)
			p.processJob(job)
		}
	}
}

func (p *WorkerPool[T, R]) processJob(job Job[T]) {
	ctx := job.Context
	if ctx == nil {
		ctx = p.ctx
	}

	result, err := p.processor(ctx, job.Data)

	if err != nil {
		p.failed.Add(1)
	}
	p.processed.Add(1)

	select {
	case p.results <- JobResult[T, R]{Job: job, Result: result, Error: err}:
	case <-p.ctx.Done():
	}
}

// ParallelProcess processes items in parallel with bounded concurrency.
// This is a simpler interface for one-off parallel processing.
func ParallelProcess[T, R any](ctx context.Context, items []T, workers int, processor func(context.Context, T) (R, error)) ([]R, []error) {
	if workers <= 0 {
		workers = 1
	}
	if len(items) == 0 {
		return nil, nil
	}

	results := make([]R, len(items))
	errors := make([]error, len(items))

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, data T) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errors[idx] = ctx.Err()
				return
			}

			result, err := processor(ctx, data)
			results[idx] = result
			errors[idx] = err
		}(i, item)
	}

	wg.Wait()
	return results, errors
}

// ParallelMap applies a function to each item in parallel.
// Returns when all items are processed or context is cancelled.
func ParallelMap[T, R any](ctx context.Context, items []T, workers int, fn func(T) R) []R {
	results, _ := ParallelProcess(ctx, items, workers, func(_ context.Context, item T) (R, error) {
		return fn(item), nil
	})
	return results
}

// ParallelForEach processes items in parallel without returning results.
func ParallelForEach[T any](ctx context.Context, items []T, workers int, fn func(T)) {
	ParallelProcess(ctx, items, workers, func(_ context.Context, item T) (struct{}, error) {
		fn(item)
		return struct{}{}, nil
	})
}

// Batch groups items into batches for processing.
type Batch[T any] struct {
	Items []T
	Index int
}

// BatchItems divides items into batches of the specified size.
func BatchItems[T any](items []T, batchSize int) []Batch[T] {
	if batchSize <= 0 {
		batchSize = 1
	}

	var batches []Batch[T]
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, Batch[T]{
			Items: items[i:end],
			Index: i / batchSize,
		})
	}
	return batches
}

// Throttle limits the rate of function calls.
type Throttle struct {
	interval time.Duration
	lastCall time.Time
	mu       sync.Mutex
}

// NewThrottle creates a new throttle with the given minimum interval.
func NewThrottle(interval time.Duration) *Throttle {
	return &Throttle{interval: interval}
}

// Do executes the function if enough time has passed since the last call.
// Returns true if the function was executed.
func (t *Throttle) Do(fn func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if time.Since(t.lastCall) < t.interval {
		return false
	}

	t.lastCall = time.Now()
	fn()
	return true
}

// DoWait executes the function, waiting if necessary to respect the interval.
func (t *Throttle) DoWait(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	elapsed := time.Since(t.lastCall)
	if elapsed < t.interval {
		time.Sleep(t.interval - elapsed)
	}

	t.lastCall = time.Now()
	fn()
}

// Reset resets the throttle, allowing immediate execution.
func (t *Throttle) Reset() {
	t.mu.Lock()
	t.lastCall = time.Time{}
	t.mu.Unlock()
}
