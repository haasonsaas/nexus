package heartbeat

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// HeartbeatConfig configures heartbeat behavior.
type HeartbeatConfig struct {
	// IntervalMs is the interval between heartbeats in milliseconds.
	IntervalMs int
	// AckMaxChars is the maximum characters in an acknowledgment.
	AckMaxChars int
	// TimeoutMs is the timeout for delivery in milliseconds.
	TimeoutMs int
	// RetryAttempts is the number of retry attempts.
	RetryAttempts int
	// RetryDelayMs is the delay between retries in milliseconds.
	RetryDelayMs int
	// VisibilityMode defines how heartbeats are displayed ("typing", "presence", "none").
	VisibilityMode string
	// DeliveryTarget is an optional specific delivery target.
	DeliveryTarget string
}

// DefaultConfig returns sensible defaults for heartbeat configuration.
func DefaultConfig() *HeartbeatConfig {
	return &HeartbeatConfig{
		IntervalMs:     5000,
		AckMaxChars:    500,
		TimeoutMs:      10000,
		RetryAttempts:  3,
		RetryDelayMs:   1000,
		VisibilityMode: "typing",
	}
}

// HeartbeatEvent represents an event during heartbeat.
type HeartbeatEvent struct {
	Type      string    `json:"type"` // "start", "tick", "ack", "error", "stop"
	Timestamp time.Time `json:"timestamp"`
	RunID     string    `json:"runId,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// HeartbeatAck represents an acknowledgment message.
type HeartbeatAck struct {
	Text      string
	Timestamp time.Time
	Delivered bool
}

// DeliveryFunc is called to deliver heartbeat acknowledgments.
type DeliveryFunc func(ctx context.Context, ack *HeartbeatAck) error

// EventFunc is called when heartbeat events occur.
type EventFunc func(event *HeartbeatEvent)

// Runner manages heartbeat delivery during long-running operations.
type Runner struct {
	config  *HeartbeatConfig
	deliver DeliveryFunc
	onEvent EventFunc

	runID     string
	sessionID string

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	ticker  *time.Ticker
	doneCh  chan struct{} // signals when the run loop has exited

	lastAck  *HeartbeatAck
	ackQueue []*HeartbeatAck
}

// NewRunner creates a new heartbeat runner.
func NewRunner(config *HeartbeatConfig, deliver DeliveryFunc, onEvent EventFunc) *Runner {
	if config == nil {
		config = DefaultConfig()
	}
	return &Runner{
		config:   config,
		deliver:  deliver,
		onEvent:  onEvent,
		ackQueue: make([]*HeartbeatAck, 0),
	}
}

// Start begins heartbeat delivery.
func (r *Runner) Start(ctx context.Context, runID, sessionID string) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.runID = runID
	r.sessionID = sessionID
	if r.runID == "" {
		r.runID = uuid.New().String()
	}
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	interval := time.Duration(r.config.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 5 * time.Second
	}
	r.ticker = time.NewTicker(interval)
	r.mu.Unlock()

	r.emitEvent(&HeartbeatEvent{
		Type:      "start",
		Timestamp: time.Now(),
		RunID:     r.runID,
		SessionID: r.sessionID,
	})

	go r.run(ctx)
}

// run is the main heartbeat loop.
func (r *Runner) run(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		if r.ticker != nil {
			r.ticker.Stop()
			r.ticker = nil
		}
		r.running = false
		close(r.doneCh)
		r.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			r.emitEvent(&HeartbeatEvent{
				Type:      "stop",
				Timestamp: time.Now(),
				RunID:     r.runID,
				SessionID: r.sessionID,
				Message:   "context cancelled",
			})
			return
		case <-r.stopCh:
			r.emitEvent(&HeartbeatEvent{
				Type:      "stop",
				Timestamp: time.Now(),
				RunID:     r.runID,
				SessionID: r.sessionID,
				Message:   "stopped",
			})
			return
		case <-r.ticker.C:
			r.tick(ctx)
		}
	}
}

// Stop halts heartbeat delivery.
func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	close(r.stopCh)
	doneCh := r.doneCh
	r.mu.Unlock()

	// Wait for the run loop to exit
	if doneCh != nil {
		<-doneCh
	}
}

// IsRunning returns true if heartbeats are active.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// QueueAck queues an acknowledgment for delivery.
func (r *Runner) QueueAck(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ack := &HeartbeatAck{
		Text:      r.truncateAck(text),
		Timestamp: time.Now(),
		Delivered: false,
	}
	r.ackQueue = append(r.ackQueue, ack)
}

// SetDeliveryTarget updates the delivery target.
func (r *Runner) SetDeliveryTarget(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config.DeliveryTarget = target
}

// GetLastAck returns the last delivered acknowledgment.
func (r *Runner) GetLastAck() *HeartbeatAck {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastAck
}

// tick performs one heartbeat cycle.
func (r *Runner) tick(ctx context.Context) {
	r.emitEvent(&HeartbeatEvent{
		Type:      "tick",
		Timestamp: time.Now(),
		RunID:     r.runID,
		SessionID: r.sessionID,
	})

	// Get next ack from queue
	r.mu.Lock()
	var ack *HeartbeatAck
	if len(r.ackQueue) > 0 {
		ack = r.ackQueue[0]
		r.ackQueue = r.ackQueue[1:]
	}
	r.mu.Unlock()

	if ack == nil {
		return
	}

	// Deliver with retry
	err := r.deliverWithRetry(ctx, ack)
	if err != nil {
		r.emitEvent(&HeartbeatEvent{
			Type:      "error",
			Timestamp: time.Now(),
			RunID:     r.runID,
			SessionID: r.sessionID,
			Error:     err.Error(),
		})
		return
	}

	ack.Delivered = true
	r.mu.Lock()
	r.lastAck = ack
	r.mu.Unlock()

	r.emitEvent(&HeartbeatEvent{
		Type:      "ack",
		Timestamp: time.Now(),
		RunID:     r.runID,
		SessionID: r.sessionID,
		Message:   ack.Text,
	})
}

// deliverWithRetry attempts delivery with retries.
func (r *Runner) deliverWithRetry(ctx context.Context, ack *HeartbeatAck) error {
	if r.deliver == nil {
		return nil
	}

	timeout := time.Duration(r.config.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	retryDelay := time.Duration(r.config.RetryDelayMs) * time.Millisecond
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	maxAttempts := r.config.RetryAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Create timeout context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)

		err := r.deliver(attemptCtx, ack)
		cancel()

		if err == nil {
			return nil
		}

		lastErr = err

		// Check if context is done
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Don't sleep after the last attempt
		if attempt < maxAttempts-1 {
			// Exponential backoff: retryDelay * 2^attempt
			sleepDuration := retryDelay * time.Duration(1<<uint(attempt))
			select {
			case <-time.After(sleepDuration):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return lastErr
}

// truncateAck truncates acknowledgment to max chars.
func (r *Runner) truncateAck(text string) string {
	maxChars := r.config.AckMaxChars
	if maxChars <= 0 {
		maxChars = 500
	}

	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}

	// Truncate and add ellipsis
	if maxChars > 3 {
		return string(runes[:maxChars-3]) + "..."
	}
	return string(runes[:maxChars])
}

// emitEvent safely calls the event callback.
func (r *Runner) emitEvent(event *HeartbeatEvent) {
	if r.onEvent != nil {
		r.onEvent(event)
	}
}

// Scheduler manages multiple heartbeat runners.
type Scheduler struct {
	mu      sync.Mutex
	runners map[string]*Runner // keyed by sessionID
	config  *HeartbeatConfig
}

// NewScheduler creates a heartbeat scheduler.
func NewScheduler(config *HeartbeatConfig) *Scheduler {
	if config == nil {
		config = DefaultConfig()
	}
	return &Scheduler{
		runners: make(map[string]*Runner),
		config:  config,
	}
}

// GetOrCreate returns existing runner or creates new one.
func (s *Scheduler) GetOrCreate(sessionID string, deliver DeliveryFunc, onEvent EventFunc) *Runner {
	s.mu.Lock()
	defer s.mu.Unlock()

	if runner, exists := s.runners[sessionID]; exists {
		return runner
	}

	// Create a copy of config for this runner
	configCopy := *s.config
	runner := NewRunner(&configCopy, deliver, onEvent)
	s.runners[sessionID] = runner
	return runner
}

// StopAll stops all runners.
func (s *Scheduler) StopAll() {
	s.mu.Lock()
	runners := make([]*Runner, 0, len(s.runners))
	for _, r := range s.runners {
		runners = append(runners, r)
	}
	s.runners = make(map[string]*Runner)
	s.mu.Unlock()

	// Stop all runners outside the lock
	for _, r := range runners {
		r.Stop()
	}
}

// StopSession stops runner for a specific session.
func (s *Scheduler) StopSession(sessionID string) {
	s.mu.Lock()
	runner, exists := s.runners[sessionID]
	if exists {
		delete(s.runners, sessionID)
	}
	s.mu.Unlock()

	if runner != nil {
		runner.Stop()
	}
}

// Active returns count of active runners.
func (s *Scheduler) Active() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, r := range s.runners {
		if r.IsRunning() {
			count++
		}
	}
	return count
}

// Get returns the runner for a session, or nil if not found.
func (s *Scheduler) Get(sessionID string) *Runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runners[sessionID]
}
