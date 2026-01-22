package infra

import (
	"context"
	"sync"
	"time"
)

// HeartbeatConfig configures a heartbeat runner.
type HeartbeatConfig struct {
	// Interval is the time between heartbeats.
	Interval time.Duration

	// InitialDelay is the delay before the first heartbeat.
	// Defaults to Interval if not set.
	InitialDelay time.Duration

	// SkipIfBusy skips heartbeat if the queue has pending items.
	SkipIfBusy bool

	// Queue is the command queue to check for busyness.
	// If nil and SkipIfBusy is true, uses DefaultQueue.
	Queue *CommandQueue

	// QueueLane is the lane to check for busyness.
	// Defaults to "main".
	QueueLane string

	// DedupeWindow is the duration to suppress duplicate results.
	// Set to 0 to disable deduplication.
	DedupeWindow time.Duration

	// OnHeartbeat is called for each heartbeat execution.
	// Return true to indicate success, false to skip recording.
	OnHeartbeat func(ctx context.Context) (result string, ok bool)

	// OnSkip is called when a heartbeat is skipped.
	OnSkip func(reason string)

	// OnError is called when a heartbeat fails.
	OnError func(err error)
}

// HeartbeatRunner manages periodic heartbeat execution.
type HeartbeatRunner struct {
	config     HeartbeatConfig
	mu         sync.Mutex
	running    bool
	stopCh     chan struct{}
	lastResult string
	lastRanAt  time.Time
}

// HeartbeatResult contains the outcome of a heartbeat execution.
type HeartbeatResult struct {
	Status    HeartbeatStatus
	Reason    string
	Duration  time.Duration
	Result    string
	Timestamp time.Time
}

// HeartbeatStatus indicates the outcome of a heartbeat.
type HeartbeatStatus string

const (
	HeartbeatStatusRan       HeartbeatStatus = "ran"
	HeartbeatStatusSkipped   HeartbeatStatus = "skipped"
	HeartbeatStatusFailed    HeartbeatStatus = "failed"
	HeartbeatStatusDuplicate HeartbeatStatus = "duplicate"
)

// NewHeartbeatRunner creates a new heartbeat runner.
func NewHeartbeatRunner(config HeartbeatConfig) *HeartbeatRunner {
	if config.QueueLane == "" {
		config.QueueLane = "main"
	}
	if config.InitialDelay == 0 {
		config.InitialDelay = config.Interval
	}

	return &HeartbeatRunner{
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Start begins the heartbeat runner.
func (r *HeartbeatRunner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.mu.Unlock()

	go r.run(ctx)
}

// Stop halts the heartbeat runner.
func (r *HeartbeatRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}

	close(r.stopCh)
	r.running = false
}

// IsRunning returns true if the heartbeat runner is active.
func (r *HeartbeatRunner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// RunOnce executes a single heartbeat immediately.
func (r *HeartbeatRunner) RunOnce(ctx context.Context) HeartbeatResult {
	return r.executeHeartbeat(ctx, "manual")
}

func (r *HeartbeatRunner) run(ctx context.Context) {
	// Wait for initial delay
	select {
	case <-time.After(r.config.InitialDelay):
	case <-r.stopCh:
		return
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.executeHeartbeat(ctx, "interval")
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (r *HeartbeatRunner) executeHeartbeat(ctx context.Context, _ string) HeartbeatResult {
	startedAt := time.Now()

	// Check if queue is busy
	if r.config.SkipIfBusy {
		queue := r.config.Queue
		if queue == nil {
			queue = DefaultQueue
		}
		if queue.QueueSize(r.config.QueueLane) > 0 {
			result := HeartbeatResult{
				Status:    HeartbeatStatusSkipped,
				Reason:    "queue-busy",
				Duration:  time.Since(startedAt),
				Timestamp: startedAt,
			}
			if r.config.OnSkip != nil {
				r.config.OnSkip("queue-busy")
			}
			return result
		}
	}

	// Execute heartbeat
	if r.config.OnHeartbeat == nil {
		return HeartbeatResult{
			Status:    HeartbeatStatusSkipped,
			Reason:    "no-handler",
			Duration:  time.Since(startedAt),
			Timestamp: startedAt,
		}
	}

	resultStr, ok := r.config.OnHeartbeat(ctx)
	if !ok {
		result := HeartbeatResult{
			Status:    HeartbeatStatusSkipped,
			Reason:    "handler-skip",
			Duration:  time.Since(startedAt),
			Timestamp: startedAt,
		}
		if r.config.OnSkip != nil {
			r.config.OnSkip("handler-skip")
		}
		return result
	}

	// Check for duplicates
	r.mu.Lock()
	isDuplicate := false
	if r.config.DedupeWindow > 0 && resultStr != "" {
		if resultStr == r.lastResult && time.Since(r.lastRanAt) < r.config.DedupeWindow {
			isDuplicate = true
		}
	}

	if !isDuplicate {
		r.lastResult = resultStr
		r.lastRanAt = startedAt
	}
	r.mu.Unlock()

	if isDuplicate {
		result := HeartbeatResult{
			Status:    HeartbeatStatusDuplicate,
			Reason:    "duplicate-suppressed",
			Duration:  time.Since(startedAt),
			Result:    resultStr,
			Timestamp: startedAt,
		}
		if r.config.OnSkip != nil {
			r.config.OnSkip("duplicate")
		}
		return result
	}

	return HeartbeatResult{
		Status:    HeartbeatStatusRan,
		Duration:  time.Since(startedAt),
		Result:    resultStr,
		Timestamp: startedAt,
	}
}

// MultiHeartbeatRunner manages multiple heartbeat runners with different intervals.
type MultiHeartbeatRunner struct {
	mu      sync.Mutex
	runners map[string]*HeartbeatRunner
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewMultiHeartbeatRunner creates a runner for managing multiple heartbeats.
func NewMultiHeartbeatRunner() *MultiHeartbeatRunner {
	return &MultiHeartbeatRunner{
		runners: make(map[string]*HeartbeatRunner),
	}
}

// Add adds a named heartbeat configuration.
func (m *MultiHeartbeatRunner) Add(name string, config HeartbeatConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.runners[name] = NewHeartbeatRunner(config)

	// If already running, start the new runner
	if m.running && m.ctx != nil {
		m.runners[name].Start(m.ctx)
	}
}

// Remove removes a named heartbeat.
func (m *MultiHeartbeatRunner) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if runner, ok := m.runners[name]; ok {
		runner.Stop()
		delete(m.runners, name)
	}
}

// Start begins all heartbeat runners.
func (m *MultiHeartbeatRunner) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.running = true

	for _, runner := range m.runners {
		runner.Start(m.ctx)
	}
}

// Stop halts all heartbeat runners.
func (m *MultiHeartbeatRunner) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	if m.cancel != nil {
		m.cancel()
	}

	for _, runner := range m.runners {
		runner.Stop()
	}

	m.running = false
}

// RunOnce executes a single heartbeat for a named runner.
func (m *MultiHeartbeatRunner) RunOnce(ctx context.Context, name string) (HeartbeatResult, bool) {
	m.mu.Lock()
	runner, ok := m.runners[name]
	m.mu.Unlock()

	if !ok {
		return HeartbeatResult{}, false
	}

	return runner.RunOnce(ctx), true
}

// Names returns the names of all registered heartbeats.
func (m *MultiHeartbeatRunner) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	names := make([]string, 0, len(m.runners))
	for name := range m.runners {
		names = append(names, name)
	}
	return names
}
