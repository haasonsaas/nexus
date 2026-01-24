package hooks

import (
	"errors"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestEventType_Constants(t *testing.T) {
	// Verify event type constants are properly defined
	tests := []struct {
		name     string
		event    EventType
		expected string
	}{
		{"MessageReceived", EventMessageReceived, "message.received"},
		{"MessageProcessed", EventMessageProcessed, "message.processed"},
		{"MessageSent", EventMessageSent, "message.sent"},
		{"SessionCreated", EventSessionCreated, "session.created"},
		{"SessionUpdated", EventSessionUpdated, "session.updated"},
		{"SessionEnded", EventSessionEnded, "session.ended"},
		{"CommandNew", EventCommandNew, "command.new"},
		{"CommandReset", EventCommandReset, "command.reset"},
		{"CommandStop", EventCommandStop, "command.stop"},
		{"CommandDetected", EventCommandDetected, "command.detected"},
		{"CommandExecuted", EventCommandExecuted, "command.executed"},
		{"CommandCompleted", EventCommandCompleted, "command.completed"},
		{"ToolCalled", EventToolCalled, "tool.called"},
		{"ToolCompleted", EventToolCompleted, "tool.completed"},
		{"ToolResultPersist", EventToolResultPersist, "tool.result_persist"},
		{"AgentBootstrap", EventAgentBootstrap, "agent.bootstrap"},
		{"AgentStarted", EventAgentStarted, "agent.started"},
		{"AgentCompleted", EventAgentCompleted, "agent.completed"},
		{"AgentError", EventAgentError, "agent.error"},
		{"GatewayStartup", EventGatewayStartup, "gateway.startup"},
		{"GatewayShutdown", EventGatewayShutdown, "gateway.shutdown"},
		{"Startup", EventStartup, "lifecycle.startup"},
		{"Shutdown", EventShutdown, "lifecycle.shutdown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.event) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.event)
			}
		})
	}
}

func TestPriority_Constants(t *testing.T) {
	tests := []struct {
		name     string
		priority Priority
		expected Priority
	}{
		{"Highest", PriorityHighest, 0},
		{"High", PriorityHigh, 25},
		{"Normal", PriorityNormal, 50},
		{"Low", PriorityLow, 75},
		{"Lowest", PriorityLowest, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.priority != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, tt.priority)
			}
		})
	}

	// Verify ordering: Highest < High < Normal < Low < Lowest
	if !(PriorityHighest < PriorityHigh && PriorityHigh < PriorityNormal &&
		PriorityNormal < PriorityLow && PriorityLow < PriorityLowest) {
		t.Error("priority constants are not in proper order")
	}
}

func TestNewEvent(t *testing.T) {
	eventType := EventMessageReceived
	action := "test_action"

	event := NewEvent(eventType, action)

	if event.Type != eventType {
		t.Errorf("expected type %s, got %s", eventType, event.Type)
	}
	if event.Action != action {
		t.Errorf("expected action %s, got %s", action, event.Action)
	}
	if event.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if event.Context == nil {
		t.Error("expected non-nil context map")
	}
	// Timestamp should be recent (within 1 second)
	if time.Since(event.Timestamp) > time.Second {
		t.Error("timestamp should be recent")
	}
}

func TestEvent_WithSession(t *testing.T) {
	event := NewEvent(EventMessageReceived, "")
	sessionKey := "session-12345"

	result := event.WithSession(sessionKey)

	// Should return same event for chaining
	if result != event {
		t.Error("expected same event instance for chaining")
	}
	if event.SessionKey != sessionKey {
		t.Errorf("expected session %s, got %s", sessionKey, event.SessionKey)
	}
}

func TestEvent_WithChannel(t *testing.T) {
	event := NewEvent(EventMessageReceived, "")
	channelID := "channel-456"
	channelType := models.ChannelDiscord

	result := event.WithChannel(channelID, channelType)

	if result != event {
		t.Error("expected same event instance for chaining")
	}
	if event.ChannelID != channelID {
		t.Errorf("expected channel ID %s, got %s", channelID, event.ChannelID)
	}
	if event.ChannelType != channelType {
		t.Errorf("expected channel type %s, got %s", channelType, event.ChannelType)
	}
}

func TestEvent_WithMessage(t *testing.T) {
	event := NewEvent(EventMessageReceived, "")
	msg := &models.Message{
		ID:      "msg-123",
		Content: "Hello world",
	}

	result := event.WithMessage(msg)

	if result != event {
		t.Error("expected same event instance for chaining")
	}
	if event.Message != msg {
		t.Error("expected message to be set")
	}
	if event.Message.ID != "msg-123" {
		t.Errorf("expected message ID msg-123, got %s", event.Message.ID)
	}
}

func TestEvent_WithContext(t *testing.T) {
	event := NewEvent(EventMessageReceived, "")

	// First context addition
	event.WithContext("key1", "value1")
	if event.Context["key1"] != "value1" {
		t.Error("expected key1 to be set")
	}

	// Second context addition
	event.WithContext("key2", 42)
	if event.Context["key2"] != 42 {
		t.Error("expected key2 to be set")
	}

	// Verify both keys exist
	if len(event.Context) < 2 {
		t.Errorf("expected at least 2 context entries, got %d", len(event.Context))
	}
}

func TestEvent_WithContext_NilContext(t *testing.T) {
	// Create event with nil context manually
	event := &Event{
		Type:    EventMessageReceived,
		Context: nil,
	}

	event.WithContext("key", "value")

	if event.Context == nil {
		t.Error("expected context to be initialized")
	}
	if event.Context["key"] != "value" {
		t.Error("expected key to be set")
	}
}

func TestEvent_WithError(t *testing.T) {
	event := NewEvent(EventAgentError, "")
	err := errors.New("something went wrong")

	result := event.WithError(err)

	if result != event {
		t.Error("expected same event instance for chaining")
	}
	if event.Error != err {
		t.Error("expected error to be set")
	}
	if event.ErrorMsg != "something went wrong" {
		t.Errorf("expected error msg 'something went wrong', got %s", event.ErrorMsg)
	}
}

func TestEvent_WithError_Nil(t *testing.T) {
	event := NewEvent(EventAgentError, "")

	event.WithError(nil)

	if event.Error != nil {
		t.Error("expected nil error")
	}
	if event.ErrorMsg != "" {
		t.Error("expected empty error message")
	}
}

func TestEvent_ChainedBuilders(t *testing.T) {
	err := errors.New("test error")
	msg := &models.Message{ID: "msg-1"}

	event := NewEvent(EventAgentError, "failure").
		WithSession("session-abc").
		WithChannel("channel-xyz", models.ChannelTelegram).
		WithMessage(msg).
		WithContext("retry_count", 3).
		WithContext("model", "claude-3").
		WithError(err)

	if event.Type != EventAgentError {
		t.Error("type mismatch")
	}
	if event.Action != "failure" {
		t.Error("action mismatch")
	}
	if event.SessionKey != "session-abc" {
		t.Error("session mismatch")
	}
	if event.ChannelID != "channel-xyz" {
		t.Error("channel ID mismatch")
	}
	if event.ChannelType != models.ChannelTelegram {
		t.Error("channel type mismatch")
	}
	if event.Message != msg {
		t.Error("message mismatch")
	}
	if event.Context["retry_count"] != 3 {
		t.Error("context retry_count mismatch")
	}
	if event.Context["model"] != "claude-3" {
		t.Error("context model mismatch")
	}
	if event.Error != err {
		t.Error("error mismatch")
	}
}

func TestFilter_Matches_ChannelTypes(t *testing.T) {
	tests := []struct {
		name   string
		filter *Filter
		event  *Event
		want   bool
	}{
		{
			name: "channel type filter matches",
			filter: &Filter{
				ChannelTypes: []models.ChannelType{models.ChannelDiscord, models.ChannelTelegram},
			},
			event: NewEvent(EventMessageReceived, "").WithChannel("123", models.ChannelDiscord),
			want:  true,
		},
		{
			name: "channel type filter does not match",
			filter: &Filter{
				ChannelTypes: []models.ChannelType{models.ChannelSlack},
			},
			event: NewEvent(EventMessageReceived, "").WithChannel("123", models.ChannelDiscord),
			want:  false,
		},
		{
			name: "empty channel types matches all",
			filter: &Filter{
				ChannelTypes: []models.ChannelType{},
			},
			event: NewEvent(EventMessageReceived, "").WithChannel("123", models.ChannelDiscord),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.Matches(tt.event); got != tt.want {
				t.Errorf("Filter.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Matches_CombinedFilters(t *testing.T) {
	filter := &Filter{
		EventTypes:   []EventType{EventMessageReceived, EventMessageSent},
		ChannelTypes: []models.ChannelType{models.ChannelDiscord},
		SessionKeys:  []string{"session-1"},
	}

	tests := []struct {
		name  string
		event *Event
		want  bool
	}{
		{
			name: "all filters match",
			event: NewEvent(EventMessageReceived, "").
				WithChannel("123", models.ChannelDiscord).
				WithSession("session-1"),
			want: true,
		},
		{
			name: "event type does not match",
			event: NewEvent(EventSessionCreated, "").
				WithChannel("123", models.ChannelDiscord).
				WithSession("session-1"),
			want: false,
		},
		{
			name: "channel type does not match",
			event: NewEvent(EventMessageReceived, "").
				WithChannel("123", models.ChannelSlack).
				WithSession("session-1"),
			want: false,
		},
		{
			name: "session key does not match",
			event: NewEvent(EventMessageReceived, "").
				WithChannel("123", models.ChannelDiscord).
				WithSession("session-2"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filter.Matches(tt.event); got != tt.want {
				t.Errorf("Filter.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistration_Fields(t *testing.T) {
	reg := &Registration{
		ID:       "reg-123",
		EventKey: "message.received",
		Priority: PriorityHigh,
		Name:     "TestHandler",
		Source:   "test-plugin",
	}

	if reg.ID != "reg-123" {
		t.Error("ID mismatch")
	}
	if reg.EventKey != "message.received" {
		t.Error("EventKey mismatch")
	}
	if reg.Priority != PriorityHigh {
		t.Error("Priority mismatch")
	}
	if reg.Name != "TestHandler" {
		t.Error("Name mismatch")
	}
	if reg.Source != "test-plugin" {
		t.Error("Source mismatch")
	}
}
