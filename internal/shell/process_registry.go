// Package shell provides bash process management for tracking shell sessions.
package shell

import (
	"log/slog"
	"sync"
	"time"
)

// TTL configuration for finished sessions.
const (
	DefaultJobTTL = 30 * time.Minute
	MinJobTTL     = 1 * time.Minute
	MaxJobTTL     = 3 * time.Hour

	DefaultPendingOutputChars = 30_000
	DefaultTailChars          = 2000
)

// ProcessStatus represents the state of a shell process.
type ProcessStatus string

const (
	ProcessStatusRunning   ProcessStatus = "running"
	ProcessStatusCompleted ProcessStatus = "completed"
	ProcessStatusFailed    ProcessStatus = "failed"
	ProcessStatusKilled    ProcessStatus = "killed"
)

// ProcessSession represents an active shell process.
type ProcessSession struct {
	ID         string
	Command    string
	ScopeKey   string
	SessionKey string
	PID        int
	StartedAt  time.Time
	CWD        string

	// Output configuration
	MaxOutputChars        int
	PendingMaxOutputChars int

	// Output buffers
	PendingStdout      []string
	PendingStderr      []string
	PendingStdoutChars int
	PendingStderrChars int
	TotalOutputChars   int

	// Aggregated output
	Aggregated string
	Tail       string

	// Exit info
	ExitCode   *int
	ExitSignal string
	Exited     bool
	Truncated  bool

	// Background handling
	Backgrounded bool
	NotifyOnExit bool
	ExitNotified bool
}

// FinishedSession represents a completed shell process.
type FinishedSession struct {
	ID               string
	Command          string
	ScopeKey         string
	StartedAt        time.Time
	EndedAt          time.Time
	CWD              string
	Status           ProcessStatus
	ExitCode         *int
	ExitSignal       string
	Aggregated       string
	Tail             string
	Truncated        bool
	TotalOutputChars int
}

// ProcessRegistry manages active and finished shell sessions.
type ProcessRegistry struct {
	runningSessions  map[string]*ProcessSession
	finishedSessions map[string]*FinishedSession
	logger           *slog.Logger
	jobTTL           time.Duration
	mu               sync.RWMutex

	// Sweeper management
	sweeperStop chan struct{}
	sweeperDone chan struct{}
}

// NewProcessRegistry creates a new process registry.
func NewProcessRegistry(logger *slog.Logger) *ProcessRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProcessRegistry{
		runningSessions:  make(map[string]*ProcessSession),
		finishedSessions: make(map[string]*FinishedSession),
		logger:           logger.With("component", "process_registry"),
		jobTTL:           DefaultJobTTL,
	}
}

// ClampTTL ensures the TTL is within valid bounds.
func ClampTTL(ttl time.Duration) time.Duration {
	if ttl < MinJobTTL {
		return MinJobTTL
	}
	if ttl > MaxJobTTL {
		return MaxJobTTL
	}
	return ttl
}

// SetJobTTL updates the TTL for finished sessions.
func (r *ProcessRegistry) SetJobTTL(ttl time.Duration) {
	r.mu.Lock()
	r.jobTTL = ClampTTL(ttl)
	r.mu.Unlock()

	// Restart sweeper with new TTL
	r.StopSweeper()
	r.StartSweeper()
}

// GetJobTTL returns the current job TTL.
func (r *ProcessRegistry) GetJobTTL() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.jobTTL
}

// IsSessionIDTaken checks if a session ID is already in use.
func (r *ProcessRegistry) IsSessionIDTaken(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, running := r.runningSessions[id]
	_, finished := r.finishedSessions[id]
	return running || finished
}

// AddSession registers a new running session.
func (r *ProcessRegistry) AddSession(session *ProcessSession) {
	if session == nil {
		return
	}

	r.mu.Lock()
	r.runningSessions[session.ID] = session
	r.mu.Unlock()

	r.StartSweeper()

	r.logger.Debug("added session",
		"id", session.ID,
		"command", session.Command,
		"pid", session.PID)
}

// GetSession retrieves a running session by ID.
func (r *ProcessRegistry) GetSession(id string) (*ProcessSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.runningSessions[id]
	return session, exists
}

// GetFinishedSession retrieves a finished session by ID.
func (r *ProcessRegistry) GetFinishedSession(id string) (*FinishedSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.finishedSessions[id]
	return session, exists
}

// DeleteSession removes a session from both running and finished maps.
func (r *ProcessRegistry) DeleteSession(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.runningSessions, id)
	delete(r.finishedSessions, id)

	r.logger.Debug("deleted session", "id", id)
}

// AppendOutput adds output to a session's pending buffers.
func (r *ProcessRegistry) AppendOutput(session *ProcessSession, stream string, chunk string) {
	if session == nil || chunk == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Initialize buffers if needed
	if session.PendingStdout == nil {
		session.PendingStdout = make([]string, 0)
	}
	if session.PendingStderr == nil {
		session.PendingStderr = make([]string, 0)
	}

	// Determine pending cap
	pendingCap := session.PendingMaxOutputChars
	if pendingCap <= 0 {
		pendingCap = DefaultPendingOutputChars
	}
	if session.MaxOutputChars > 0 && pendingCap > session.MaxOutputChars {
		pendingCap = session.MaxOutputChars
	}

	// Select buffer based on stream
	var buffer *[]string
	var pendingChars *int
	if stream == "stdout" {
		buffer = &session.PendingStdout
		pendingChars = &session.PendingStdoutChars
	} else {
		buffer = &session.PendingStderr
		pendingChars = &session.PendingStderrChars
	}

	// Append chunk
	*buffer = append(*buffer, chunk)
	*pendingChars += len(chunk)

	// Cap pending buffer if needed
	if *pendingChars > pendingCap {
		session.Truncated = true
		*pendingChars = capPendingBuffer(buffer, *pendingChars, pendingCap)
	}

	// Update total output chars
	session.TotalOutputChars += len(chunk)

	// Update aggregated output
	maxOutput := session.MaxOutputChars
	if maxOutput <= 0 {
		maxOutput = DefaultPendingOutputChars
	}
	newAggregated := TrimWithCap(session.Aggregated+chunk, maxOutput)
	if len(newAggregated) < len(session.Aggregated)+len(chunk) {
		session.Truncated = true
	}
	session.Aggregated = newAggregated

	// Update tail
	session.Tail = Tail(session.Aggregated, DefaultTailChars)
}

// DrainSession retrieves and clears pending output from a session.
func (r *ProcessRegistry) DrainSession(session *ProcessSession) (stdout, stderr string) {
	if session == nil {
		return "", ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Join and return pending output
	for _, chunk := range session.PendingStdout {
		stdout += chunk
	}
	for _, chunk := range session.PendingStderr {
		stderr += chunk
	}

	// Clear pending buffers
	session.PendingStdout = make([]string, 0)
	session.PendingStderr = make([]string, 0)
	session.PendingStdoutChars = 0
	session.PendingStderrChars = 0

	return stdout, stderr
}

// MarkExited marks a session as exited and moves it to finished if backgrounded.
func (r *ProcessRegistry) MarkExited(session *ProcessSession, exitCode *int, exitSignal string, status ProcessStatus) {
	if session == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	session.Exited = true
	session.ExitCode = exitCode
	session.ExitSignal = exitSignal
	session.Tail = Tail(session.Aggregated, DefaultTailChars)

	r.moveToFinishedLocked(session, status)
}

// MarkBackgrounded marks a session as running in the background.
func (r *ProcessRegistry) MarkBackgrounded(session *ProcessSession) {
	if session == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	session.Backgrounded = true
}

// moveToFinishedLocked moves a session from running to finished.
// Must be called with lock held.
func (r *ProcessRegistry) moveToFinishedLocked(session *ProcessSession, status ProcessStatus) {
	delete(r.runningSessions, session.ID)

	// Only store in finished if backgrounded
	if !session.Backgrounded {
		return
	}

	r.finishedSessions[session.ID] = &FinishedSession{
		ID:               session.ID,
		Command:          session.Command,
		ScopeKey:         session.ScopeKey,
		StartedAt:        session.StartedAt,
		EndedAt:          time.Now(),
		CWD:              session.CWD,
		Status:           status,
		ExitCode:         session.ExitCode,
		ExitSignal:       session.ExitSignal,
		Aggregated:       session.Aggregated,
		Tail:             session.Tail,
		Truncated:        session.Truncated,
		TotalOutputChars: session.TotalOutputChars,
	}

	r.logger.Debug("session finished",
		"id", session.ID,
		"status", status,
		"exit_code", session.ExitCode)
}

// ListRunningSessions returns all backgrounded running sessions.
func (r *ProcessRegistry) ListRunningSessions() []*ProcessSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]*ProcessSession, 0)
	for _, s := range r.runningSessions {
		if s.Backgrounded {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

// ListFinishedSessions returns all finished sessions.
func (r *ProcessRegistry) ListFinishedSessions() []*FinishedSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]*FinishedSession, 0, len(r.finishedSessions))
	for _, s := range r.finishedSessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// ClearFinished removes all finished sessions.
func (r *ProcessRegistry) ClearFinished() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.finishedSessions = make(map[string]*FinishedSession)
	r.logger.Debug("cleared finished sessions")
}

// Reset clears all sessions and stops the sweeper (useful for tests).
func (r *ProcessRegistry) Reset() {
	r.StopSweeper()

	r.mu.Lock()
	defer r.mu.Unlock()

	r.runningSessions = make(map[string]*ProcessSession)
	r.finishedSessions = make(map[string]*FinishedSession)
	r.logger.Debug("reset registry")
}

// StartSweeper starts the background goroutine that prunes old finished sessions.
func (r *ProcessRegistry) StartSweeper() {
	r.mu.Lock()
	if r.sweeperStop != nil {
		r.mu.Unlock()
		return // Already running
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	r.sweeperStop = stop
	r.sweeperDone = done
	ttl := r.jobTTL
	r.mu.Unlock()

	// Sweep interval is at most 30 seconds or 1/6 of TTL
	interval := ttl / 6
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}

	go r.sweepLoop(interval, stop, done)
}

// StopSweeper stops the background sweeper goroutine.
func (r *ProcessRegistry) StopSweeper() {
	r.mu.Lock()
	if r.sweeperStop == nil {
		r.mu.Unlock()
		return
	}

	stop := r.sweeperStop
	done := r.sweeperDone
	r.sweeperStop = nil
	r.sweeperDone = nil
	r.mu.Unlock()

	close(stop)
	<-done
}

// sweepLoop runs the sweeper until stopped.
func (r *ProcessRegistry) sweepLoop(interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.pruneFinishedSessions()
		}
	}
}

// pruneFinishedSessions removes finished sessions older than TTL.
func (r *ProcessRegistry) pruneFinishedSessions() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-r.jobTTL)
	for id, session := range r.finishedSessions {
		if session.EndedAt.Before(cutoff) {
			delete(r.finishedSessions, id)
			r.logger.Debug("pruned finished session", "id", id)
		}
	}
}

// RunningCount returns the number of running sessions.
func (r *ProcessRegistry) RunningCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.runningSessions)
}

// FinishedCount returns the number of finished sessions.
func (r *ProcessRegistry) FinishedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.finishedSessions)
}

// Helper functions

// Tail returns the last n characters of text.
func Tail(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[len(text)-n:]
}

// TrimWithCap trims text to at most max characters, keeping the end.
func TrimWithCap(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[len(text)-max:]
}

// capPendingBuffer trims the buffer to fit within cap characters.
// Returns the new pending char count.
func capPendingBuffer(buffer *[]string, pendingChars, cap int) int {
	if pendingChars <= cap {
		return pendingChars
	}

	// If the last chunk alone is >= cap, just keep the tail of it
	if len(*buffer) > 0 {
		last := (*buffer)[len(*buffer)-1]
		if len(last) >= cap {
			*buffer = []string{last[len(last)-cap:]}
			return cap
		}
	}

	// Remove chunks from the front until we're under cap
	for len(*buffer) > 0 && pendingChars-len((*buffer)[0]) >= cap {
		pendingChars -= len((*buffer)[0])
		*buffer = (*buffer)[1:]
	}

	// Trim the first remaining chunk if still over cap
	if len(*buffer) > 0 && pendingChars > cap {
		overflow := pendingChars - cap
		(*buffer)[0] = (*buffer)[0][overflow:]
		pendingChars = cap
	}

	return pendingChars
}
