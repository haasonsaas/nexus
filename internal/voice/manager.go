package voice

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultCallManager provides a basic implementation of CallManager.
type DefaultCallManager struct {
	provider Provider
	calls    map[string]*CallRecord
	mu       sync.RWMutex

	// Event handlers
	onEvent func(context.Context, *CallEvent)
}

// ManagerConfig holds configuration for the call manager.
type ManagerConfig struct {
	// Provider is the telephony provider to use
	Provider Provider

	// OnEvent is called when call events occur
	OnEvent func(context.Context, *CallEvent)
}

// NewDefaultCallManager creates a new call manager.
func NewDefaultCallManager(cfg ManagerConfig) (*DefaultCallManager, error) {
	if cfg.Provider == nil {
		return nil, errors.New("voice: provider is required")
	}

	return &DefaultCallManager{
		provider: cfg.Provider,
		calls:    make(map[string]*CallRecord),
		onEvent:  cfg.OnEvent,
	}, nil
}

// InitiateCall starts a new outbound call.
func (m *DefaultCallManager) InitiateCall(ctx context.Context, to, from, webhookURL, message string) (*CallRecord, error) {
	callID := uuid.New().String()
	now := time.Now()

	record := &CallRecord{
		CallID:     callID,
		Provider:   m.provider.Name(),
		Direction:  DirectionOutbound,
		State:      StateInitiated,
		From:       from,
		To:         to,
		StartedAt:  now,
		Transcript: []TranscriptEntry{},
	}

	// Store the call record
	m.mu.Lock()
	m.calls[callID] = record
	m.mu.Unlock()

	// Initiate the call
	input := &InitiateCallInput{
		CallID:     callID,
		From:       from,
		To:         to,
		WebhookURL: webhookURL,
	}

	result, err := m.provider.InitiateCall(ctx, input)
	if err != nil {
		m.mu.Lock()
		record.State = StateFailed
		record.EndReason = EndReasonFailed
		endTime := time.Now()
		record.EndedAt = &endTime
		m.mu.Unlock()
		return record, err
	}

	m.mu.Lock()
	record.ProviderCallID = result.ProviderCallID
	m.mu.Unlock()

	// If there's an initial message, speak it after the call connects
	// (handled by event processing)

	return record, nil
}

// AnswerCall handles an incoming call.
func (m *DefaultCallManager) AnswerCall(ctx context.Context, event *CallEvent) (*CallRecord, error) {
	callID := event.CallID
	now := time.Now()

	record := &CallRecord{
		CallID:         callID,
		ProviderCallID: event.ProviderCallID,
		Provider:       m.provider.Name(),
		Direction:      DirectionInbound,
		State:          StateAnswered,
		From:           event.From,
		To:             event.To,
		StartedAt:      now,
		AnsweredAt:     &now,
		Transcript:     []TranscriptEntry{},
	}

	m.mu.Lock()
	m.calls[callID] = record
	m.mu.Unlock()

	return record, nil
}

// SpeakToUser plays TTS to the user.
func (m *DefaultCallManager) SpeakToUser(ctx context.Context, callID, text string) error {
	m.mu.RLock()
	record, ok := m.calls[callID]
	m.mu.RUnlock()

	if !ok {
		return errors.New("voice: call not found")
	}

	if record.State.IsTerminal() {
		return errors.New("voice: call has ended")
	}

	input := &PlayTTSInput{
		CallID:         callID,
		ProviderCallID: record.ProviderCallID,
		Text:           text,
	}

	if err := m.provider.PlayTTS(ctx, input); err != nil {
		return err
	}

	// Update transcript
	m.mu.Lock()
	record.State = StateSpeaking
	record.Transcript = append(record.Transcript, TranscriptEntry{
		Timestamp: time.Now(),
		Speaker:   "bot",
		Text:      text,
		IsFinal:   true,
	})
	m.mu.Unlock()

	return nil
}

// EndCall terminates a call.
func (m *DefaultCallManager) EndCall(ctx context.Context, callID string, reason EndReason) error {
	m.mu.RLock()
	record, ok := m.calls[callID]
	m.mu.RUnlock()

	if !ok {
		return errors.New("voice: call not found")
	}

	if record.State.IsTerminal() {
		return nil // Already ended
	}

	input := &HangupCallInput{
		CallID:         callID,
		ProviderCallID: record.ProviderCallID,
		Reason:         reason,
	}

	if err := m.provider.HangupCall(ctx, input); err != nil {
		return err
	}

	m.mu.Lock()
	record.State = CallState(reason)
	record.EndReason = reason
	endTime := time.Now()
	record.EndedAt = &endTime
	m.mu.Unlock()

	return nil
}

// GetCall retrieves a call record.
func (m *DefaultCallManager) GetCall(callID string) (*CallRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.calls[callID]
	return record, ok
}

// HandleEvent processes a call event.
func (m *DefaultCallManager) HandleEvent(ctx context.Context, event *CallEvent) error {
	m.mu.Lock()
	record, ok := m.calls[event.CallID]
	if !ok {
		// Create record for inbound calls
		if event.Direction == DirectionInbound && event.Type == EventCallInitiated {
			record = &CallRecord{
				CallID:         event.CallID,
				ProviderCallID: event.ProviderCallID,
				Provider:       m.provider.Name(),
				Direction:      DirectionInbound,
				State:          StateInitiated,
				From:           event.From,
				To:             event.To,
				StartedAt:      event.Timestamp,
				Transcript:     []TranscriptEntry{},
			}
			m.calls[event.CallID] = record
			ok = true
		}
	}
	m.mu.Unlock()

	if !ok {
		// Unknown call, skip
		return nil
	}

	// Update state based on event
	m.mu.Lock()
	switch event.Type {
	case EventCallInitiated:
		record.State = StateInitiated
	case EventCallRinging:
		record.State = StateRinging
	case EventCallAnswered:
		record.State = StateActive
		now := event.Timestamp
		record.AnsweredAt = &now
	case EventCallSpeaking:
		record.State = StateSpeaking
		record.Transcript = append(record.Transcript, TranscriptEntry{
			Timestamp: event.Timestamp,
			Speaker:   "bot",
			Text:      event.Text,
			IsFinal:   true,
		})
	case EventCallSpeech:
		record.State = StateListening
		record.Transcript = append(record.Transcript, TranscriptEntry{
			Timestamp: event.Timestamp,
			Speaker:   "user",
			Text:      event.Transcript,
			IsFinal:   event.IsFinal,
		})
	case EventCallEnded:
		record.State = CallState(event.Reason)
		record.EndReason = event.Reason
		endTime := event.Timestamp
		record.EndedAt = &endTime
	case EventCallError:
		record.State = StateError
		record.EndReason = EndReasonError
		endTime := event.Timestamp
		record.EndedAt = &endTime
	}
	m.mu.Unlock()

	// Notify event handler
	if m.onEvent != nil {
		m.onEvent(ctx, event)
	}

	return nil
}

// HandleWebhook processes a webhook request and returns the response.
func (m *DefaultCallManager) HandleWebhook(ctx context.Context, webhookCtx *WebhookContext) (*WebhookParseResult, error) {
	// Verify webhook
	valid, err := m.provider.VerifyWebhook(webhookCtx)
	if err != nil {
		return nil, err
	}
	if !valid {
		return &WebhookParseResult{
			StatusCode:   401,
			ResponseBody: "Unauthorized",
		}, nil
	}

	// Parse webhook
	result, err := m.provider.ParseWebhook(webhookCtx)
	if err != nil {
		return nil, err
	}

	// Process events
	for _, event := range result.Events {
		if err := m.HandleEvent(ctx, &event); err != nil {
			// Best effort: webhook responses should still acknowledge receipt.
			_ = err
		}
	}

	return result, nil
}

// CleanupStaleCALLS removes ended calls older than the specified duration.
func (m *DefaultCallManager) CleanupStaleCalls(olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)
	removed := 0

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, record := range m.calls {
		if record.State.IsTerminal() && record.EndedAt != nil && record.EndedAt.Before(cutoff) {
			delete(m.calls, id)
			removed++
		}
	}

	return removed
}
