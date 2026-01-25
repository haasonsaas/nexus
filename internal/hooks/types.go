// Package hooks provides an event-driven hook system for agent events.
package hooks

import (
	"context"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// EventType identifies the category of hook event.
type EventType string

const (
	// Message events
	EventMessageReceived  EventType = "message.received"
	EventMessageProcessed EventType = "message.processed"
	EventMessageSent      EventType = "message.sent"

	// Session events
	EventSessionCreated EventType = "session.created"
	EventSessionUpdated EventType = "session.updated"
	EventSessionEnded   EventType = "session.ended"

	// Command events (Clawdbot patterns: command:new, command:reset, command:stop)
	EventCommandNew       EventType = "command.new"
	EventCommandReset     EventType = "command.reset"
	EventCommandStop      EventType = "command.stop"
	EventCommandDetected  EventType = "command.detected"
	EventCommandExecuted  EventType = "command.executed"
	EventCommandCompleted EventType = "command.completed"

	// Tool events
	EventToolCalled        EventType = "tool.called"
	EventToolCompleted     EventType = "tool.completed"
	EventToolResultPersist EventType = "tool.result_persist"

	// Agent events (Clawdbot patterns: agent:bootstrap)
	EventAgentBootstrap EventType = "agent.bootstrap"
	EventAgentStarted   EventType = "agent.started"
	EventAgentCompleted EventType = "agent.completed"
	EventAgentError     EventType = "agent.error"

	// Gateway events (Clawdbot patterns: gateway:startup)
	EventGatewayStartup  EventType = "gateway.startup"
	EventGatewayShutdown EventType = "gateway.shutdown"

	// Gmail hook events
	EventGmailReceived EventType = "gmail.received"
	EventGmailError    EventType = "gmail.error"

	// Lifecycle events (legacy, prefer gateway.* events)
	EventStartup  EventType = "lifecycle.startup"
	EventShutdown EventType = "lifecycle.shutdown"
)

// Event represents a hook event with context and payload.
type Event struct {
	// Type is the event category
	Type EventType `json:"type"`

	// Action is the specific action within the type (optional)
	Action string `json:"action,omitempty"`

	// SessionKey identifies the session this event relates to
	SessionKey string `json:"session_key,omitempty"`

	// ChannelID identifies the channel
	ChannelID string `json:"channel_id,omitempty"`

	// ChannelType is the type of channel (discord, telegram, etc)
	ChannelType models.ChannelType `json:"channel_type,omitempty"`

	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Message associated with this event (if applicable)
	Message *models.Message `json:"message,omitempty"`

	// Messages is a batch of messages (for aggregated events)
	Messages []*models.Message `json:"messages,omitempty"`

	// Context holds additional event-specific data
	Context map[string]any `json:"context,omitempty"`

	// Error if this is an error event
	Error    error  `json:"-"`
	ErrorMsg string `json:"error,omitempty"`
}

// Handler is a function that processes hook events.
// Handlers should be fast and non-blocking. Long-running operations
// should be dispatched to goroutines.
type Handler func(ctx context.Context, event *Event) error

// Priority determines the order handlers are called.
type Priority int

const (
	PriorityHighest Priority = 0
	PriorityHigh    Priority = 25
	PriorityNormal  Priority = 50
	PriorityLow     Priority = 75
	PriorityLowest  Priority = 100
)

// Registration represents a registered hook handler.
type Registration struct {
	// ID is a unique identifier for this registration
	ID string

	// EventKey is the event type or type:action this handler listens for
	EventKey string

	// Handler is the function to call
	Handler Handler

	// Priority determines call order (lower = earlier)
	Priority Priority

	// Name is a human-readable name for debugging
	Name string

	// Source identifies where this handler came from (plugin name, etc)
	Source string
}

// Filter allows selective event handling.
type Filter struct {
	// EventTypes to include (empty = all)
	EventTypes []EventType

	// ChannelTypes to include (empty = all)
	ChannelTypes []models.ChannelType

	// SessionKeys to include (empty = all)
	SessionKeys []string
}

// Matches checks if an event matches the filter.
func (f *Filter) Matches(event *Event) bool {
	if f == nil {
		return true
	}

	if len(f.EventTypes) > 0 {
		found := false
		for _, t := range f.EventTypes {
			if t == event.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(f.ChannelTypes) > 0 {
		found := false
		for _, t := range f.ChannelTypes {
			if t == event.ChannelType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(f.SessionKeys) > 0 {
		found := false
		for _, k := range f.SessionKeys {
			if k == event.SessionKey {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// NewEvent creates a new event with timestamp set.
func NewEvent(eventType EventType, action string) *Event {
	return &Event{
		Type:      eventType,
		Action:    action,
		Timestamp: time.Now(),
		Context:   make(map[string]any),
	}
}

// WithSession sets the session key on the event.
func (e *Event) WithSession(sessionKey string) *Event {
	e.SessionKey = sessionKey
	return e
}

// WithChannel sets the channel info on the event.
func (e *Event) WithChannel(channelID string, channelType models.ChannelType) *Event {
	e.ChannelID = channelID
	e.ChannelType = channelType
	return e
}

// WithMessage sets the message on the event.
func (e *Event) WithMessage(msg *models.Message) *Event {
	e.Message = msg
	return e
}

// WithContext adds context data to the event.
func (e *Event) WithContext(key string, value any) *Event {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}
	e.Context[key] = value
	return e
}

// WithError sets the error on the event.
func (e *Event) WithError(err error) *Event {
	e.Error = err
	if err != nil {
		e.ErrorMsg = err.Error()
	}
	return e
}
