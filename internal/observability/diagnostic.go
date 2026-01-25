// Package observability provides diagnostic event types and emission.
package observability

import (
	"sync"
	"sync/atomic"
	"time"
)

// DiagnosticSessionState represents the state of a session.
type DiagnosticSessionState string

const (
	SessionStateIdle       DiagnosticSessionState = "idle"
	SessionStateProcessing DiagnosticSessionState = "processing"
	SessionStateWaiting    DiagnosticSessionState = "waiting"
)

// DiagnosticEventType identifies the type of diagnostic event.
type DiagnosticEventType string

const (
	EventTypeModelUsage          DiagnosticEventType = "model.usage"
	EventTypeWebhookReceived     DiagnosticEventType = "webhook.received"
	EventTypeWebhookProcessed    DiagnosticEventType = "webhook.processed"
	EventTypeWebhookError        DiagnosticEventType = "webhook.error"
	EventTypeMessageQueued       DiagnosticEventType = "message.queued"
	EventTypeMessageProcessed    DiagnosticEventType = "message.processed"
	EventTypeSessionState        DiagnosticEventType = "session.state"
	EventTypeSessionStuck        DiagnosticEventType = "session.stuck"
	EventTypeLaneEnqueue         DiagnosticEventType = "queue.lane.enqueue"
	EventTypeLaneDequeue         DiagnosticEventType = "queue.lane.dequeue"
	EventTypeRunAttempt          DiagnosticEventType = "run.attempt"
	EventTypeDiagnosticHeartbeat DiagnosticEventType = "diagnostic.heartbeat"
)

// DiagnosticEvent is the base event structure.
type DiagnosticEvent struct {
	Type DiagnosticEventType `json:"type"`
	Seq  int64               `json:"seq"`
	Ts   int64               `json:"ts"`
}

// ModelUsageEvent tracks token usage for a model request.
type ModelUsageEvent struct {
	DiagnosticEvent
	SessionKey string          `json:"session_key,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Channel    string          `json:"channel,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	Model      string          `json:"model,omitempty"`
	Usage      UsageDetails    `json:"usage"`
	Context    *ContextDetails `json:"context,omitempty"`
	CostUSD    float64         `json:"cost_usd,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
}

// UsageDetails contains token usage breakdown.
type UsageDetails struct {
	Input        int64 `json:"input,omitempty"`
	Output       int64 `json:"output,omitempty"`
	CacheRead    int64 `json:"cache_read,omitempty"`
	CacheWrite   int64 `json:"cache_write,omitempty"`
	PromptTokens int64 `json:"prompt_tokens,omitempty"`
	Total        int64 `json:"total,omitempty"`
}

// ContextDetails contains context window information.
type ContextDetails struct {
	Limit int64 `json:"limit,omitempty"`
	Used  int64 `json:"used,omitempty"`
}

// WebhookReceivedEvent tracks incoming webhooks.
type WebhookReceivedEvent struct {
	DiagnosticEvent
	Channel    string `json:"channel"`
	UpdateType string `json:"update_type,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
}

// WebhookProcessedEvent tracks processed webhooks.
type WebhookProcessedEvent struct {
	DiagnosticEvent
	Channel    string `json:"channel"`
	UpdateType string `json:"update_type,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// WebhookErrorEvent tracks webhook errors.
type WebhookErrorEvent struct {
	DiagnosticEvent
	Channel    string `json:"channel"`
	UpdateType string `json:"update_type,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	Error      string `json:"error"`
}

// MessageQueuedEvent tracks queued messages.
type MessageQueuedEvent struct {
	DiagnosticEvent
	SessionKey string `json:"session_key,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Source     string `json:"source"`
	QueueDepth int    `json:"queue_depth,omitempty"`
}

// MessageProcessedEvent tracks processed messages.
type MessageProcessedEvent struct {
	DiagnosticEvent
	Channel    string `json:"channel"`
	MessageID  string `json:"message_id,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Outcome    string `json:"outcome"` // "completed", "skipped", "error"
	Reason     string `json:"reason,omitempty"`
	Error      string `json:"error,omitempty"`
}

// SessionStateEvent tracks session state changes.
type SessionStateEvent struct {
	DiagnosticEvent
	SessionKey string                 `json:"session_key,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	PrevState  DiagnosticSessionState `json:"prev_state,omitempty"`
	State      DiagnosticSessionState `json:"state"`
	Reason     string                 `json:"reason,omitempty"`
	QueueDepth int                    `json:"queue_depth,omitempty"`
}

// SessionStuckEvent tracks stuck sessions.
type SessionStuckEvent struct {
	DiagnosticEvent
	SessionKey string                 `json:"session_key,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	State      DiagnosticSessionState `json:"state"`
	AgeMs      int64                  `json:"age_ms"`
	QueueDepth int                    `json:"queue_depth,omitempty"`
}

// LaneEnqueueEvent tracks queue lane enqueues.
type LaneEnqueueEvent struct {
	DiagnosticEvent
	Lane      string `json:"lane"`
	QueueSize int    `json:"queue_size"`
}

// LaneDequeueEvent tracks queue lane dequeues.
type LaneDequeueEvent struct {
	DiagnosticEvent
	Lane      string `json:"lane"`
	QueueSize int    `json:"queue_size"`
	WaitMs    int64  `json:"wait_ms"`
}

// RunAttemptEvent tracks run attempts.
type RunAttemptEvent struct {
	DiagnosticEvent
	SessionKey string `json:"session_key,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	RunID      string `json:"run_id"`
	Attempt    int    `json:"attempt"`
}

// DiagnosticHeartbeatEvent tracks diagnostic heartbeats.
type DiagnosticHeartbeatEvent struct {
	DiagnosticEvent
	Webhooks WebhookStats `json:"webhooks"`
	Active   int          `json:"active"`
	Waiting  int          `json:"waiting"`
	Queued   int          `json:"queued"`
}

// WebhookStats contains webhook statistics.
type WebhookStats struct {
	Received  int64 `json:"received"`
	Processed int64 `json:"processed"`
	Errors    int64 `json:"errors"`
}

// DiagnosticEventPayload is a union type for all diagnostic events.
type DiagnosticEventPayload interface {
	EventType() DiagnosticEventType
	Sequence() int64
	Timestamp() int64
}

// Implement DiagnosticEventPayload for all event types
func (e *DiagnosticEvent) EventType() DiagnosticEventType { return e.Type }
func (e *DiagnosticEvent) Sequence() int64                { return e.Seq }
func (e *DiagnosticEvent) Timestamp() int64               { return e.Ts }

// DiagnosticListener receives diagnostic events.
type DiagnosticListener func(event DiagnosticEventPayload)

// DiagnosticEmitter manages diagnostic event emission.
type DiagnosticEmitter struct {
	mu        sync.RWMutex
	seq       int64
	enabled   bool
	listeners []DiagnosticListener
}

var globalEmitter = &DiagnosticEmitter{}

// SetDiagnosticsEnabled enables or disables diagnostic events.
func SetDiagnosticsEnabled(enabled bool) {
	globalEmitter.mu.Lock()
	defer globalEmitter.mu.Unlock()
	globalEmitter.enabled = enabled
}

// IsDiagnosticsEnabled returns whether diagnostics are enabled.
func IsDiagnosticsEnabled() bool {
	globalEmitter.mu.RLock()
	defer globalEmitter.mu.RUnlock()
	return globalEmitter.enabled
}

// OnDiagnosticEvent registers a listener for diagnostic events.
func OnDiagnosticEvent(listener DiagnosticListener) func() {
	globalEmitter.mu.Lock()
	defer globalEmitter.mu.Unlock()
	globalEmitter.listeners = append(globalEmitter.listeners, listener)

	// Return unsubscribe function
	return func() {
		globalEmitter.mu.Lock()
		defer globalEmitter.mu.Unlock()
		for i, l := range globalEmitter.listeners {
			// Compare function pointers (this is a simplification)
			if &l == &listener {
				globalEmitter.listeners = append(globalEmitter.listeners[:i], globalEmitter.listeners[i+1:]...)
				break
			}
		}
	}
}

// nextSeq returns the next sequence number.
func nextSeq() int64 {
	return atomic.AddInt64(&globalEmitter.seq, 1)
}

// emit sends an event to all listeners.
func emit(event DiagnosticEventPayload) {
	globalEmitter.mu.RLock()
	if !globalEmitter.enabled {
		globalEmitter.mu.RUnlock()
		return
	}
	listeners := make([]DiagnosticListener, len(globalEmitter.listeners))
	copy(listeners, globalEmitter.listeners)
	globalEmitter.mu.RUnlock()

	for _, listener := range listeners {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					_ = recovered
				}
			}() // Ignore listener panics
			listener(event)
		}()
	}
}

// EmitModelUsage emits a model usage event.
func EmitModelUsage(e *ModelUsageEvent) {
	e.Type = EventTypeModelUsage
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitWebhookReceived emits a webhook received event.
func EmitWebhookReceived(e *WebhookReceivedEvent) {
	e.Type = EventTypeWebhookReceived
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitWebhookProcessed emits a webhook processed event.
func EmitWebhookProcessed(e *WebhookProcessedEvent) {
	e.Type = EventTypeWebhookProcessed
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitWebhookError emits a webhook error event.
func EmitWebhookError(e *WebhookErrorEvent) {
	e.Type = EventTypeWebhookError
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitMessageQueued emits a message queued event.
func EmitMessageQueued(e *MessageQueuedEvent) {
	e.Type = EventTypeMessageQueued
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitMessageProcessed emits a message processed event.
func EmitMessageProcessed(e *MessageProcessedEvent) {
	e.Type = EventTypeMessageProcessed
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitSessionState emits a session state event.
func EmitSessionState(e *SessionStateEvent) {
	e.Type = EventTypeSessionState
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitSessionStuck emits a session stuck event.
func EmitSessionStuck(e *SessionStuckEvent) {
	e.Type = EventTypeSessionStuck
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitLaneEnqueue emits a lane enqueue event.
func EmitLaneEnqueue(e *LaneEnqueueEvent) {
	e.Type = EventTypeLaneEnqueue
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitLaneDequeue emits a lane dequeue event.
func EmitLaneDequeue(e *LaneDequeueEvent) {
	e.Type = EventTypeLaneDequeue
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitRunAttempt emits a run attempt event.
func EmitRunAttempt(e *RunAttemptEvent) {
	e.Type = EventTypeRunAttempt
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// EmitDiagnosticHeartbeat emits a diagnostic heartbeat event.
func EmitDiagnosticHeartbeat(e *DiagnosticHeartbeatEvent) {
	e.Type = EventTypeDiagnosticHeartbeat
	e.Seq = nextSeq()
	e.Ts = time.Now().UnixMilli()
	emit(e)
}

// ResetDiagnosticsForTest resets diagnostic state for testing.
func ResetDiagnosticsForTest() {
	globalEmitter.mu.Lock()
	defer globalEmitter.mu.Unlock()
	atomic.StoreInt64(&globalEmitter.seq, 0)
	globalEmitter.listeners = nil
}
