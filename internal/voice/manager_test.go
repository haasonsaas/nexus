package voice

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name           ProviderName
	initiateErr    error
	initiateResult *InitiateCallResult
	hangupErr      error
	playTTSErr     error
	verifyResult   bool
	verifyErr      error
	parseResult    *WebhookParseResult
	parseErr       error
}

func (p *mockProvider) Name() ProviderName                                           { return p.name }
func (p *mockProvider) InitiateCall(_ context.Context, _ *InitiateCallInput) (*InitiateCallResult, error) {
	return p.initiateResult, p.initiateErr
}
func (p *mockProvider) HangupCall(_ context.Context, _ *HangupCallInput) error       { return p.hangupErr }
func (p *mockProvider) PlayTTS(_ context.Context, _ *PlayTTSInput) error             { return p.playTTSErr }
func (p *mockProvider) StartListening(_ context.Context, _ *StartListeningInput) error { return nil }
func (p *mockProvider) StopListening(_ context.Context, _, _ string) error           { return nil }
func (p *mockProvider) VerifyWebhook(_ *WebhookContext) (bool, error)                { return p.verifyResult, p.verifyErr }
func (p *mockProvider) ParseWebhook(_ *WebhookContext) (*WebhookParseResult, error)  { return p.parseResult, p.parseErr }

func TestNewDefaultCallManager_NilProvider(t *testing.T) {
	_, err := NewDefaultCallManager(ManagerConfig{})
	if err == nil {
		t.Fatal("expected error when provider is nil")
	}
}

func TestNewDefaultCallManager_Success(t *testing.T) {
	mgr, err := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestSpeakToUser_CallNotFound(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})
	err := mgr.SpeakToUser(context.Background(), "nonexistent", "hello")
	if err == nil {
		t.Fatal("expected error for nonexistent call")
	}
}

func TestSpeakToUser_TerminalCall(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})

	// Manually insert a terminated call record.
	mgr.mu.Lock()
	mgr.calls["ended-call"] = &CallRecord{
		CallID: "ended-call",
		State:  StateCompleted,
	}
	mgr.mu.Unlock()

	err := mgr.SpeakToUser(context.Background(), "ended-call", "hello")
	if err == nil {
		t.Fatal("expected error for terminal call")
	}
}

func TestSpeakToUser_Success(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})

	mgr.mu.Lock()
	mgr.calls["active-call"] = &CallRecord{
		CallID:     "active-call",
		State:      StateActive,
		Transcript: []TranscriptEntry{},
	}
	mgr.mu.Unlock()

	err := mgr.SpeakToUser(context.Background(), "active-call", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	record, ok := mgr.GetCall("active-call")
	if !ok {
		t.Fatal("call not found after SpeakToUser")
	}
	if record.State != StateSpeaking {
		t.Fatalf("expected state %q, got %q", StateSpeaking, record.State)
	}
	if len(record.Transcript) != 1 {
		t.Fatalf("expected 1 transcript entry, got %d", len(record.Transcript))
	}
}

func TestEndCall_NotFound(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})
	err := mgr.EndCall(context.Background(), "no-such-call", EndReasonCompleted)
	if err == nil {
		t.Fatal("expected error for nonexistent call")
	}
}

func TestEndCall_AlreadyTerminal(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})

	mgr.mu.Lock()
	mgr.calls["done"] = &CallRecord{
		CallID: "done",
		State:  StateCompleted,
	}
	mgr.mu.Unlock()

	// Should return nil for already-terminated calls.
	err := mgr.EndCall(context.Background(), "done", EndReasonCompleted)
	if err != nil {
		t.Fatalf("expected nil for already-terminal call, got %v", err)
	}
}

func TestInitiateCall_ProviderError(t *testing.T) {
	provErr := errors.New("provider failure")
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{
			name:        ProviderMock,
			initiateErr: provErr,
		},
	})

	record, err := mgr.InitiateCall(context.Background(), "+1234", "+5678", "http://hook", "hi")
	if err == nil {
		t.Fatal("expected error from provider")
	}
	if record == nil {
		t.Fatal("expected record even on error")
	}
	if record.State != StateFailed {
		t.Fatalf("expected state %q, got %q", StateFailed, record.State)
	}
}

func TestInitiateCall_Success(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{
			name:           ProviderMock,
			initiateResult: &InitiateCallResult{ProviderCallID: "prov-123", Status: "initiated"},
		},
	})

	record, err := mgr.InitiateCall(context.Background(), "+1234", "+5678", "http://hook", "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if record.ProviderCallID != "prov-123" {
		t.Fatalf("expected provider call ID 'prov-123', got %q", record.ProviderCallID)
	}
	if record.Direction != DirectionOutbound {
		t.Fatalf("expected direction %q, got %q", DirectionOutbound, record.Direction)
	}
}

func TestGetCall_NotFound(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})
	_, ok := mgr.GetCall("missing")
	if ok {
		t.Fatal("expected false for missing call")
	}
}

func TestHandleEvent_UnknownCall(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})
	// Unknown non-inbound event should be silently ignored.
	err := mgr.HandleEvent(context.Background(), &CallEvent{
		CallID: "unknown",
		Type:   EventCallSpeech,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleEvent_InboundInitiated(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})
	err := mgr.HandleEvent(context.Background(), &CallEvent{
		CallID:    "inbound-1",
		Type:      EventCallInitiated,
		Direction: DirectionInbound,
		From:      "+111",
		To:        "+222",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	record, ok := mgr.GetCall("inbound-1")
	if !ok {
		t.Fatal("expected inbound call to be created")
	}
	if record.Direction != DirectionInbound {
		t.Fatalf("expected inbound direction, got %q", record.Direction)
	}
}

func TestCleanupStaleCalls(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{name: ProviderMock},
	})

	past := time.Now().Add(-2 * time.Hour)
	mgr.mu.Lock()
	mgr.calls["stale"] = &CallRecord{
		CallID:  "stale",
		State:   StateCompleted,
		EndedAt: &past,
	}
	mgr.calls["recent"] = &CallRecord{
		CallID: "recent",
		State:  StateActive,
	}
	mgr.mu.Unlock()

	removed := mgr.CleanupStaleCalls(1 * time.Hour)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if _, ok := mgr.GetCall("stale"); ok {
		t.Fatal("stale call should have been removed")
	}
	if _, ok := mgr.GetCall("recent"); !ok {
		t.Fatal("recent call should still exist")
	}
}

func TestHandleWebhook_VerifyFails(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{
			name:      ProviderMock,
			verifyErr: errors.New("verify failed"),
		},
	})
	_, err := mgr.HandleWebhook(context.Background(), &WebhookContext{})
	if err == nil {
		t.Fatal("expected error from verify failure")
	}
}

func TestHandleWebhook_Unauthorized(t *testing.T) {
	mgr, _ := NewDefaultCallManager(ManagerConfig{
		Provider: &mockProvider{
			name:         ProviderMock,
			verifyResult: false,
		},
	})
	result, err := mgr.HandleWebhook(context.Background(), &WebhookContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", result.StatusCode)
	}
}
