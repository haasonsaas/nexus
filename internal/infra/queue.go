package infra

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

// CommandQueue provides multi-lane task serialization.
// The default "main" lane preserves serial execution. Additional lanes
// allow controlled parallelism (e.g., cron jobs) without blocking the
// main processing pipeline.
type CommandQueue struct {
	mu    sync.Mutex
	lanes map[string]*laneState
}

type laneState struct {
	name          string
	queue         []*queueEntry
	active        int
	maxConcurrent int
	draining      bool
	cond          *sync.Cond
}

type queueEntry struct {
	task       func(context.Context) (any, error)
	ctx        context.Context
	result     chan taskResult
	enqueuedAt time.Time
	warnAfter  time.Duration
	onWait     func(waited time.Duration, queueLen int)
}

type taskResult struct {
	value any
	err   error
}

// QueueOptions configures task enqueueing behavior.
type QueueOptions struct {
	// WarnAfter triggers OnWait callback if task waits longer than this duration.
	WarnAfter time.Duration

	// OnWait is called when a task has waited longer than WarnAfter.
	OnWait func(waited time.Duration, queueLen int)
}

// NewCommandQueue creates a new multi-lane command queue.
func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		lanes: make(map[string]*laneState),
	}
}

func (q *CommandQueue) getLane(name string) *laneState {
	if name == "" {
		name = "main"
	}

	lane, ok := q.lanes[name]
	if !ok {
		lane = &laneState{
			name:          name,
			queue:         make([]*queueEntry, 0),
			maxConcurrent: 1,
		}
		lane.cond = sync.NewCond(&q.mu)
		q.lanes[name] = lane
	}
	return lane
}

// SetLaneConcurrency sets the maximum concurrent tasks for a lane.
func (q *CommandQueue) SetLaneConcurrency(lane string, maxConcurrent int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	l := q.getLane(lane)
	l.maxConcurrent = maxConcurrent

	// Wake up drain loop if concurrency increased
	l.cond.Broadcast()
}

// Enqueue adds a task to the default "main" lane.
func (q *CommandQueue) Enqueue(ctx context.Context, task func(context.Context) (any, error), opts *QueueOptions) (any, error) {
	return q.EnqueueInLane(ctx, "main", task, opts)
}

// EnqueueInLane adds a task to a specific lane.
func (q *CommandQueue) EnqueueInLane(ctx context.Context, lane string, task func(context.Context) (any, error), opts *QueueOptions) (any, error) {
	if opts == nil {
		opts = &QueueOptions{WarnAfter: 2 * time.Second}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}

	resultCh := make(chan taskResult, 1)
	entry := &queueEntry{
		task:       task,
		ctx:        ctx,
		result:     resultCh,
		enqueuedAt: time.Now(),
		warnAfter:  opts.WarnAfter,
		onWait:     opts.OnWait,
	}

	q.mu.Lock()
	l := q.getLane(lane)
	l.queue = append(l.queue, entry)

	// Start draining if not already
	if !l.draining {
		l.draining = true
		go q.drainLane(l)
	}
	q.mu.Unlock()

	// Wait for result or context cancellation
	select {
	case result := <-resultCh:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *CommandQueue) drainLane(l *laneState) {
	for {
		q.mu.Lock()

		// Wait until we can process something or queue is empty
		for l.active >= l.maxConcurrent && len(l.queue) > 0 {
			l.cond.Wait()
		}

		// Check if queue is empty
		if len(l.queue) == 0 {
			l.draining = false
			q.mu.Unlock()
			return
		}

		// Pop the next entry
		entry := l.queue[0]
		l.queue = l.queue[1:]

		// Check wait time and call callback if needed
		waited := time.Since(entry.enqueuedAt)
		if waited >= entry.warnAfter && entry.onWait != nil {
			entry.onWait(waited, len(l.queue))
		}

		l.active++
		q.mu.Unlock()

		// Execute task
		go func(e *queueEntry) {
			var (
				value any
				err   error
			)
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("task panicked: %v\n%s", rec, debug.Stack())
				}

				q.mu.Lock()
				l.active--
				l.cond.Broadcast()
				q.mu.Unlock()

				e.result <- taskResult{value: value, err: err}
			}()

			if e.ctx.Err() != nil {
				err = e.ctx.Err()
				return
			}

			value, err = e.task(e.ctx)
		}(entry)
	}
}

// QueueSize returns the number of pending and active tasks in a lane.
func (q *CommandQueue) QueueSize(lane string) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	l, ok := q.lanes[lane]
	if !ok {
		return 0
	}
	return len(l.queue) + l.active
}

// TotalQueueSize returns the total number of tasks across all lanes.
func (q *CommandQueue) TotalQueueSize() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	total := 0
	for _, l := range q.lanes {
		total += len(l.queue) + l.active
	}
	return total
}

// ClearLane removes all pending tasks from a lane.
// Returns the number of tasks removed.
func (q *CommandQueue) ClearLane(lane string) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	l, ok := q.lanes[lane]
	if !ok {
		return 0
	}

	removed := len(l.queue)
	l.queue = l.queue[:0]
	return removed
}

// LaneStats contains statistics for a lane.
type LaneStats struct {
	Name          string
	Pending       int
	Active        int
	MaxConcurrent int
}

// Stats returns statistics for all lanes.
func (q *CommandQueue) Stats() []LaneStats {
	q.mu.Lock()
	defer q.mu.Unlock()

	stats := make([]LaneStats, 0, len(q.lanes))
	for _, l := range q.lanes {
		stats = append(stats, LaneStats{
			Name:          l.name,
			Pending:       len(l.queue),
			Active:        l.active,
			MaxConcurrent: l.maxConcurrent,
		})
	}
	return stats
}

// EnqueueVoid is a convenience method for tasks that don't return a value.
func (q *CommandQueue) EnqueueVoid(ctx context.Context, task func(context.Context) error, opts *QueueOptions) error {
	_, err := q.Enqueue(ctx, func(ctx context.Context) (any, error) {
		return nil, task(ctx)
	}, opts)
	return err
}

// EnqueueVoidInLane is a convenience method for void tasks in a specific lane.
func (q *CommandQueue) EnqueueVoidInLane(ctx context.Context, lane string, task func(context.Context) error, opts *QueueOptions) error {
	_, err := q.EnqueueInLane(ctx, lane, func(ctx context.Context) (any, error) {
		return nil, task(ctx)
	}, opts)
	return err
}

// DefaultQueue is a global command queue instance.
var DefaultQueue = NewCommandQueue()

// Enqueue adds a task to the default queue's main lane.
func Enqueue(ctx context.Context, task func(context.Context) (any, error), opts *QueueOptions) (any, error) {
	return DefaultQueue.Enqueue(ctx, task, opts)
}

// EnqueueInLane adds a task to a specific lane in the default queue.
func EnqueueInLane(ctx context.Context, lane string, task func(context.Context) (any, error), opts *QueueOptions) (any, error) {
	return DefaultQueue.EnqueueInLane(ctx, lane, task, opts)
}
