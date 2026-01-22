package infra

import (
	"strings"
	"sync"
	"time"
)

// SystemEvent represents an ephemeral system event for prompt context.
type SystemEvent struct {
	Text      string
	Timestamp time.Time
}

// SystemEventsQueue manages session-scoped ephemeral events.
// Events are intended to be prefixed to prompts to inform the LLM
// about recent system state changes.
type SystemEventsQueue struct {
	mu     sync.RWMutex
	queues map[string]*sessionEvents

	// MaxEvents is the maximum number of events per session.
	MaxEvents int
}

type sessionEvents struct {
	events         []SystemEvent
	lastText       string
	lastContextKey string
}

// NewSystemEventsQueue creates a new system events queue.
func NewSystemEventsQueue() *SystemEventsQueue {
	return &SystemEventsQueue{
		queues:    make(map[string]*sessionEvents),
		MaxEvents: 20,
	}
}

func normalizeContextKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

// Enqueue adds a system event to a session's queue.
// Consecutive duplicate texts are suppressed.
func (q *SystemEventsQueue) Enqueue(sessionKey, text string, contextKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	session, ok := q.queues[sessionKey]
	if !ok {
		session = &sessionEvents{
			events: make([]SystemEvent, 0, q.MaxEvents),
		}
		q.queues[sessionKey] = session
	}

	session.lastContextKey = normalizeContextKey(contextKey)

	// Skip consecutive duplicates
	if session.lastText == text {
		return
	}
	session.lastText = text

	session.events = append(session.events, SystemEvent{
		Text:      text,
		Timestamp: time.Now(),
	})

	// Trim to max size
	if len(session.events) > q.MaxEvents {
		session.events = session.events[len(session.events)-q.MaxEvents:]
	}
}

// IsContextChanged checks if the context key has changed for a session.
func (q *SystemEventsQueue) IsContextChanged(sessionKey, contextKey string) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	session, ok := q.queues[sessionKey]
	if !ok {
		return normalizeContextKey(contextKey) != ""
	}

	return normalizeContextKey(contextKey) != session.lastContextKey
}

// Drain removes and returns all events for a session.
func (q *SystemEventsQueue) Drain(sessionKey string) []SystemEvent {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	session, ok := q.queues[sessionKey]
	if !ok || len(session.events) == 0 {
		return nil
	}

	events := make([]SystemEvent, len(session.events))
	copy(events, session.events)

	delete(q.queues, sessionKey)
	return events
}

// DrainText removes and returns all event texts for a session.
func (q *SystemEventsQueue) DrainText(sessionKey string) []string {
	events := q.Drain(sessionKey)
	if events == nil {
		return nil
	}

	texts := make([]string, len(events))
	for i, e := range events {
		texts[i] = e.Text
	}
	return texts
}

// Peek returns events without removing them.
func (q *SystemEventsQueue) Peek(sessionKey string) []SystemEvent {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	session, ok := q.queues[sessionKey]
	if !ok {
		return nil
	}

	events := make([]SystemEvent, len(session.events))
	copy(events, session.events)
	return events
}

// PeekText returns event texts without removing them.
func (q *SystemEventsQueue) PeekText(sessionKey string) []string {
	events := q.Peek(sessionKey)
	if events == nil {
		return nil
	}

	texts := make([]string, len(events))
	for i, e := range events {
		texts[i] = e.Text
	}
	return texts
}

// HasEvents returns true if the session has pending events.
func (q *SystemEventsQueue) HasEvents(sessionKey string) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	session, ok := q.queues[sessionKey]
	return ok && len(session.events) > 0
}

// Count returns the number of pending events for a session.
func (q *SystemEventsQueue) Count(sessionKey string) int {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return 0
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	session, ok := q.queues[sessionKey]
	if !ok {
		return 0
	}
	return len(session.events)
}

// Clear removes all events for a session.
func (q *SystemEventsQueue) Clear(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.queues, sessionKey)
}

// Reset clears all sessions. Primarily for testing.
func (q *SystemEventsQueue) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.queues = make(map[string]*sessionEvents)
}

// SessionCount returns the number of sessions with events.
func (q *SystemEventsQueue) SessionCount() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.queues)
}

// DefaultSystemEvents is a global system events queue.
var DefaultSystemEvents = NewSystemEventsQueue()

// EnqueueSystemEvent adds an event to the default queue.
func EnqueueSystemEvent(sessionKey, text string, contextKey string) {
	DefaultSystemEvents.Enqueue(sessionKey, text, contextKey)
}

// DrainSystemEvents removes and returns all events from the default queue.
func DrainSystemEvents(sessionKey string) []string {
	return DefaultSystemEvents.DrainText(sessionKey)
}

// HasSystemEvents checks if the default queue has events.
func HasSystemEvents(sessionKey string) bool {
	return DefaultSystemEvents.HasEvents(sessionKey)
}
