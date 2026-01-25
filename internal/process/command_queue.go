// Package process provides command queue management for serializing
// command executions across multiple lanes.
package process

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CommandLane represents different execution lanes for command processing.
// Each lane operates independently, allowing parallel execution across lanes
// while maintaining serialization within a lane.
type CommandLane string

const (
	// LaneMain is the default lane for user-initiated commands.
	LaneMain CommandLane = "main"
	// LaneCron is used for scheduled/cron job executions.
	LaneCron CommandLane = "cron"
	// LaneSubagent is used for subagent operations.
	LaneSubagent CommandLane = "subagent"
	// LaneNested is used for nested command executions.
	LaneNested CommandLane = "nested"
)

// DefaultWarnAfterMs is the default threshold for warning about long wait times.
const DefaultWarnAfterMs = 2000

// QueueEntry represents a task waiting to be executed in a command queue.
type QueueEntry struct {
	// Task is the function to execute. It receives a context and returns a result and error.
	Task func(ctx context.Context) (any, error)
	// EnqueuedAt is the timestamp when this entry was added to the queue.
	EnqueuedAt time.Time
	// WarnAfterMs is the threshold in milliseconds after which OnWait is called.
	WarnAfterMs int
	// OnWait is called when wait time exceeds WarnAfterMs.
	// waitMs is how long the task has been waiting, queuedAhead is remaining queue size.
	OnWait func(waitMs int, queuedAhead int)

	// result and err channels for communicating task completion
	resultCh chan any
	errCh    chan error
}

// LaneState manages the state of a single command lane.
type LaneState struct {
	Lane          CommandLane
	queue         []*QueueEntry
	active        int
	maxConcurrent int
	draining      bool
	mu            sync.Mutex
}

// EnqueueOptions configures how a task is enqueued.
type EnqueueOptions struct {
	// WarnAfterMs is the threshold in milliseconds for wait time warnings.
	// Defaults to DefaultWarnAfterMs if not set.
	WarnAfterMs int
	// OnWait is called when the task has been waiting longer than WarnAfterMs.
	OnWait func(waitMs int, queuedAhead int)
	// Context is the context for task execution. Defaults to context.Background().
	Context context.Context
}

// CommandQueue manages multiple command lanes for serializing command executions.
// It provides lane isolation so tasks in different lanes don't block each other,
// while tasks within a lane are serialized based on concurrency limits.
type CommandQueue struct {
	lanes map[CommandLane]*LaneState
	mu    sync.RWMutex
}

// NewCommandQueue creates a new CommandQueue with default lane configurations.
func NewCommandQueue() *CommandQueue {
	cq := &CommandQueue{
		lanes: make(map[CommandLane]*LaneState),
	}
	return cq
}

// getLaneState returns the lane state, creating it if necessary.
// Must be called with cq.mu held for writing.
func (cq *CommandQueue) getLaneState(lane CommandLane) *LaneState {
	if lane == "" {
		lane = LaneMain
	}
	state, exists := cq.lanes[lane]
	if exists {
		return state
	}
	state = &LaneState{
		Lane:          lane,
		queue:         make([]*QueueEntry, 0),
		active:        0,
		maxConcurrent: 1,
		draining:      false,
	}
	cq.lanes[lane] = state
	return state
}

// ensureState gets or creates a lane state with proper locking.
func (cq *CommandQueue) ensureState(lane CommandLane) *LaneState {
	if lane == "" {
		lane = LaneMain
	}

	// Try read lock first for common case
	cq.mu.RLock()
	state, exists := cq.lanes[lane]
	cq.mu.RUnlock()
	if exists {
		return state
	}

	// Need write lock to create
	cq.mu.Lock()
	defer cq.mu.Unlock()
	return cq.getLaneState(lane)
}

// SetLaneConcurrency sets the maximum number of concurrent tasks for a lane.
// The value is clamped to a minimum of 1.
func (cq *CommandQueue) SetLaneConcurrency(lane CommandLane, maxConcurrent int) {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	state := cq.ensureState(lane)
	state.mu.Lock()
	state.maxConcurrent = maxConcurrent
	state.mu.Unlock()

	// Try to drain in case we can now run more tasks
	cq.drainLane(lane)
}

// drainLane processes queued tasks up to the concurrency limit.
func (cq *CommandQueue) drainLane(lane CommandLane) {
	state := cq.ensureState(lane)

	state.mu.Lock()
	if state.draining {
		state.mu.Unlock()
		return
	}
	state.draining = true
	state.mu.Unlock()

	cq.pump(state)
}

// pump processes tasks from the queue while respecting concurrency limits.
func (cq *CommandQueue) pump(state *LaneState) {
	for {
		state.mu.Lock()
		if state.active >= state.maxConcurrent || len(state.queue) == 0 {
			state.draining = false
			state.mu.Unlock()
			return
		}

		entry := state.queue[0]
		state.queue = state.queue[1:]
		queuedAhead := len(state.queue)

		waitedMs := int(time.Since(entry.EnqueuedAt).Milliseconds())
		if waitedMs >= entry.WarnAfterMs && entry.OnWait != nil {
			entry.OnWait(waitedMs, queuedAhead)
		}

		state.active++
		state.mu.Unlock()

		// Execute task in goroutine
		go func(e *QueueEntry) {
			ctx := context.Background()
			result, err := e.Task(ctx)

			state.mu.Lock()
			state.active--
			state.mu.Unlock()

			// Send result
			if err != nil {
				e.errCh <- err
			} else {
				e.resultCh <- result
			}

			// Continue pumping
			cq.pump(state)
		}(entry)
	}
}

// EnqueueInLane adds a task to the specified lane and returns the result.
// The task will be executed when it reaches the front of the queue and
// there's available concurrency capacity.
func EnqueueInLane[T any](cq *CommandQueue, lane CommandLane, task func(ctx context.Context) (T, error), opts *EnqueueOptions) (T, error) {
	if lane == "" {
		lane = LaneMain
	}

	warnAfterMs := DefaultWarnAfterMs
	var onWait func(int, int)
	ctx := context.Background()

	if opts != nil {
		if opts.WarnAfterMs > 0 {
			warnAfterMs = opts.WarnAfterMs
		}
		onWait = opts.OnWait
		if opts.Context != nil {
			ctx = opts.Context
		}
	}

	// Create channels for result communication
	resultCh := make(chan any, 1)
	errCh := make(chan error, 1)

	// Wrap the typed task
	wrappedTask := func(taskCtx context.Context) (any, error) {
		return task(taskCtx)
	}

	entry := &QueueEntry{
		Task:        wrappedTask,
		EnqueuedAt:  time.Now(),
		WarnAfterMs: warnAfterMs,
		OnWait:      onWait,
		resultCh:    resultCh,
		errCh:       errCh,
	}

	state := cq.ensureState(lane)
	state.mu.Lock()
	state.queue = append(state.queue, entry)
	state.mu.Unlock()

	// Start draining
	cq.drainLane(lane)

	// Wait for result
	var zero T
	select {
	case result := <-resultCh:
		if result == nil {
			return zero, nil
		}
		typed, ok := result.(T)
		if !ok {
			return zero, fmt.Errorf("unexpected task result type %T", result)
		}
		return typed, nil
	case err := <-errCh:
		return zero, err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Enqueue adds a task to the main lane and returns the result.
// This is a convenience wrapper around EnqueueInLane.
func Enqueue[T any](cq *CommandQueue, task func(ctx context.Context) (T, error), opts *EnqueueOptions) (T, error) {
	return EnqueueInLane(cq, LaneMain, task, opts)
}

// GetQueueSize returns the total number of tasks (queued + active) in a lane.
func (cq *CommandQueue) GetQueueSize(lane CommandLane) int {
	if lane == "" {
		lane = LaneMain
	}

	cq.mu.RLock()
	state, exists := cq.lanes[lane]
	cq.mu.RUnlock()

	if !exists {
		return 0
	}

	state.mu.Lock()
	size := len(state.queue) + state.active
	state.mu.Unlock()
	return size
}

// GetTotalQueueSize returns the total number of tasks across all lanes.
func (cq *CommandQueue) GetTotalQueueSize() int {
	cq.mu.RLock()
	defer cq.mu.RUnlock()

	total := 0
	for _, state := range cq.lanes {
		state.mu.Lock()
		total += len(state.queue) + state.active
		state.mu.Unlock()
	}
	return total
}

// ClearLane removes all queued (but not active) tasks from a lane.
// Returns the number of tasks removed.
func (cq *CommandQueue) ClearLane(lane CommandLane) int {
	if lane == "" {
		lane = LaneMain
	}

	cq.mu.RLock()
	state, exists := cq.lanes[lane]
	cq.mu.RUnlock()

	if !exists {
		return 0
	}

	state.mu.Lock()
	removed := len(state.queue)
	// Signal error to all waiting tasks
	for _, entry := range state.queue {
		entry.errCh <- context.Canceled
	}
	state.queue = make([]*QueueEntry, 0)
	state.mu.Unlock()

	return removed
}

// GetActiveTasks returns the number of currently executing tasks in a lane.
func (cq *CommandQueue) GetActiveTasks(lane CommandLane) int {
	if lane == "" {
		lane = LaneMain
	}

	cq.mu.RLock()
	state, exists := cq.lanes[lane]
	cq.mu.RUnlock()

	if !exists {
		return 0
	}

	state.mu.Lock()
	active := state.active
	state.mu.Unlock()
	return active
}

// GetPendingTasks returns the number of queued (waiting) tasks in a lane.
func (cq *CommandQueue) GetPendingTasks(lane CommandLane) int {
	if lane == "" {
		lane = LaneMain
	}

	cq.mu.RLock()
	state, exists := cq.lanes[lane]
	cq.mu.RUnlock()

	if !exists {
		return 0
	}

	state.mu.Lock()
	pending := len(state.queue)
	state.mu.Unlock()
	return pending
}

// GetLaneStats returns statistics for a lane.
type LaneStats struct {
	Lane          CommandLane
	Pending       int
	Active        int
	MaxConcurrent int
}

// GetLaneStats returns statistics for a specific lane.
func (cq *CommandQueue) GetLaneStats(lane CommandLane) LaneStats {
	if lane == "" {
		lane = LaneMain
	}

	cq.mu.RLock()
	state, exists := cq.lanes[lane]
	cq.mu.RUnlock()

	if !exists {
		return LaneStats{Lane: lane}
	}

	state.mu.Lock()
	stats := LaneStats{
		Lane:          lane,
		Pending:       len(state.queue),
		Active:        state.active,
		MaxConcurrent: state.maxConcurrent,
	}
	state.mu.Unlock()
	return stats
}

// GetAllLaneStats returns statistics for all active lanes.
func (cq *CommandQueue) GetAllLaneStats() []LaneStats {
	cq.mu.RLock()
	defer cq.mu.RUnlock()

	stats := make([]LaneStats, 0, len(cq.lanes))
	for _, state := range cq.lanes {
		state.mu.Lock()
		stats = append(stats, LaneStats{
			Lane:          state.Lane,
			Pending:       len(state.queue),
			Active:        state.active,
			MaxConcurrent: state.maxConcurrent,
		})
		state.mu.Unlock()
	}
	return stats
}
