// Package voice provides telephony integration for voice calls.
// It supports multiple providers (Twilio, Plivo, Telnyx) with real-time
// bidirectional audio streaming.
package voice

import (
	"context"
	"time"
)

// ProviderName identifies a telephony provider.
type ProviderName string

const (
	ProviderTwilio ProviderName = "twilio"
	ProviderPlivo  ProviderName = "plivo"
	ProviderTelnyx ProviderName = "telnyx"
	ProviderMock   ProviderName = "mock"
)

// CallState represents the current state of a call.
type CallState string

const (
	// Active states
	StateInitiated CallState = "initiated"
	StateRinging   CallState = "ringing"
	StateAnswered  CallState = "answered"
	StateActive    CallState = "active"
	StateSpeaking  CallState = "speaking"
	StateListening CallState = "listening"

	// Terminal states
	StateCompleted  CallState = "completed"
	StateHangupUser CallState = "hangup-user"
	StateHangupBot  CallState = "hangup-bot"
	StateTimeout    CallState = "timeout"
	StateError      CallState = "error"
	StateFailed     CallState = "failed"
	StateNoAnswer   CallState = "no-answer"
	StateBusy       CallState = "busy"
	StateVoicemail  CallState = "voicemail"
)

// IsTerminal returns true if this is a terminal state.
func (s CallState) IsTerminal() bool {
	switch s {
	case StateCompleted, StateHangupUser, StateHangupBot, StateTimeout,
		StateError, StateFailed, StateNoAnswer, StateBusy, StateVoicemail:
		return true
	}
	return false
}

// CallDirection indicates if a call is inbound or outbound.
type CallDirection string

const (
	DirectionInbound  CallDirection = "inbound"
	DirectionOutbound CallDirection = "outbound"
)

// EndReason describes why a call ended.
type EndReason string

const (
	EndReasonCompleted  EndReason = "completed"
	EndReasonHangupUser EndReason = "hangup-user"
	EndReasonHangupBot  EndReason = "hangup-bot"
	EndReasonTimeout    EndReason = "timeout"
	EndReasonError      EndReason = "error"
	EndReasonFailed     EndReason = "failed"
	EndReasonNoAnswer   EndReason = "no-answer"
	EndReasonBusy       EndReason = "busy"
	EndReasonVoicemail  EndReason = "voicemail"
)

// CallEvent represents an event during a call's lifecycle.
type CallEvent struct {
	ID             string        `json:"id"`
	CallID         string        `json:"call_id"`
	ProviderCallID string        `json:"provider_call_id,omitempty"`
	Type           EventType     `json:"type"`
	Timestamp      time.Time     `json:"timestamp"`
	Direction      CallDirection `json:"direction,omitempty"`
	From           string        `json:"from,omitempty"`
	To             string        `json:"to,omitempty"`

	// Event-specific fields
	Text       string    `json:"text,omitempty"`        // For speaking events
	Transcript string    `json:"transcript,omitempty"`  // For speech events
	IsFinal    bool      `json:"is_final,omitempty"`    // For speech events
	Confidence float64   `json:"confidence,omitempty"`  // For speech events
	Digits     string    `json:"digits,omitempty"`      // For DTMF events
	DurationMs int       `json:"duration_ms,omitempty"` // For silence events
	Reason     EndReason `json:"reason,omitempty"`      // For ended events
	Error      string    `json:"error,omitempty"`       // For error events
	Retryable  bool      `json:"retryable,omitempty"`   // For error events
}

// EventType categorizes call events.
type EventType string

const (
	EventCallInitiated EventType = "call.initiated"
	EventCallRinging   EventType = "call.ringing"
	EventCallAnswered  EventType = "call.answered"
	EventCallActive    EventType = "call.active"
	EventCallSpeaking  EventType = "call.speaking"
	EventCallSpeech    EventType = "call.speech"
	EventCallSilence   EventType = "call.silence"
	EventCallDTMF      EventType = "call.dtmf"
	EventCallEnded     EventType = "call.ended"
	EventCallError     EventType = "call.error"
)

// TranscriptEntry represents a single utterance in a call transcript.
type TranscriptEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Speaker   string    `json:"speaker"` // "bot" or "user"
	Text      string    `json:"text"`
	IsFinal   bool      `json:"is_final"`
}

// CallRecord contains the full state of a call.
type CallRecord struct {
	CallID         string                 `json:"call_id"`
	ProviderCallID string                 `json:"provider_call_id,omitempty"`
	Provider       ProviderName           `json:"provider"`
	Direction      CallDirection          `json:"direction"`
	State          CallState              `json:"state"`
	From           string                 `json:"from"`
	To             string                 `json:"to"`
	SessionKey     string                 `json:"session_key,omitempty"`
	StartedAt      time.Time              `json:"started_at"`
	AnsweredAt     *time.Time             `json:"answered_at,omitempty"`
	EndedAt        *time.Time             `json:"ended_at,omitempty"`
	EndReason      EndReason              `json:"end_reason,omitempty"`
	Transcript     []TranscriptEntry      `json:"transcript"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// InitiateCallInput contains parameters for starting an outbound call.
type InitiateCallInput struct {
	CallID      string            `json:"call_id"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	WebhookURL  string            `json:"webhook_url"`
	ClientState map[string]string `json:"client_state,omitempty"`
	InlineTwiML string            `json:"inline_twiml,omitempty"`
}

// InitiateCallResult contains the result of initiating a call.
type InitiateCallResult struct {
	ProviderCallID string `json:"provider_call_id"`
	Status         string `json:"status"` // "initiated" or "queued"
}

// HangupCallInput contains parameters for ending a call.
type HangupCallInput struct {
	CallID         string    `json:"call_id"`
	ProviderCallID string    `json:"provider_call_id"`
	Reason         EndReason `json:"reason"`
}

// PlayTTSInput contains parameters for text-to-speech playback.
type PlayTTSInput struct {
	CallID         string `json:"call_id"`
	ProviderCallID string `json:"provider_call_id"`
	Text           string `json:"text"`
	Voice          string `json:"voice,omitempty"`
	Locale         string `json:"locale,omitempty"`
}

// StartListeningInput contains parameters for starting speech recognition.
type StartListeningInput struct {
	CallID         string `json:"call_id"`
	ProviderCallID string `json:"provider_call_id"`
	Language       string `json:"language,omitempty"`
}

// WebhookContext provides context for processing webhook requests.
type WebhookContext struct {
	Headers map[string]string
	Body    string
	URL     string
	Method  string
	Query   map[string]string
}

// WebhookParseResult contains the result of parsing a webhook.
type WebhookParseResult struct {
	Events          []CallEvent
	ResponseBody    string
	ResponseHeaders map[string]string
	StatusCode      int
}

// Provider defines the interface for telephony providers.
type Provider interface {
	// Name returns the provider identifier.
	Name() ProviderName

	// InitiateCall starts an outbound call.
	InitiateCall(ctx context.Context, input *InitiateCallInput) (*InitiateCallResult, error)

	// HangupCall ends an active call.
	HangupCall(ctx context.Context, input *HangupCallInput) error

	// PlayTTS plays text-to-speech audio.
	PlayTTS(ctx context.Context, input *PlayTTSInput) error

	// StartListening begins speech recognition.
	StartListening(ctx context.Context, input *StartListeningInput) error

	// StopListening stops speech recognition.
	StopListening(ctx context.Context, callID, providerCallID string) error

	// VerifyWebhook validates webhook authenticity.
	VerifyWebhook(ctx *WebhookContext) (bool, error)

	// ParseWebhook parses a webhook into events.
	ParseWebhook(ctx *WebhookContext) (*WebhookParseResult, error)
}

// CallManager manages active calls and their state.
type CallManager interface {
	// InitiateCall starts a new outbound call.
	InitiateCall(ctx context.Context, to, from string, message string) (*CallRecord, error)

	// AnswerCall handles an incoming call.
	AnswerCall(ctx context.Context, event *CallEvent) (*CallRecord, error)

	// SpeakToUser plays TTS to the user.
	SpeakToUser(ctx context.Context, callID, text string) error

	// EndCall terminates a call.
	EndCall(ctx context.Context, callID string, reason EndReason) error

	// GetCall retrieves a call record.
	GetCall(callID string) (*CallRecord, bool)

	// HandleEvent processes a call event.
	HandleEvent(ctx context.Context, event *CallEvent) error
}
