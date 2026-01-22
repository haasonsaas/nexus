package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// mockStreamingAdapter implements channels.StreamingAdapter for testing.
type mockStreamingAdapter struct {
	mu               sync.Mutex
	typingCalls      int
	startCalls       int
	updateCalls      int
	lastContent      string
	startErr         error
	updateErr        error
	messageID        string
	typingIndicators []time.Time
}

func (m *mockStreamingAdapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.typingCalls++
	m.typingIndicators = append(m.typingIndicators, time.Now())
	return nil
}

func (m *mockStreamingAdapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls++
	if m.startErr != nil {
		return "", m.startErr
	}
	if m.messageID == "" {
		m.messageID = "msg-123"
	}
	return m.messageID, nil
}

func (m *mockStreamingAdapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls++
	m.lastContent = content
	return m.updateErr
}

// mockOutboundAdapter implements channels.OutboundAdapter for testing.
type mockOutboundAdapter struct {
	mu          sync.Mutex
	sendCalls   int
	lastMessage *models.Message
	sendErr     error
}

func (m *mockOutboundAdapter) Send(ctx context.Context, msg *models.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls++
	m.lastMessage = msg
	return m.sendErr
}

func TestStreamingMode_String(t *testing.T) {
	tests := []struct {
		mode     StreamingMode
		expected string
	}{
		{StreamingDisabled, "disabled"},
		{StreamingRealTime, "realtime"},
		{StreamingBuffered, "buffered"},
		{StreamingTypingOnly, "typing_only"},
		{StreamingMode(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.expected {
			t.Errorf("StreamingMode(%d).String() = %s, want %s", tt.mode, got, tt.expected)
		}
	}
}

func TestStreamingHandler_IsEnabled(t *testing.T) {
	t.Run("enabled with adapter", func(t *testing.T) {
		h := NewStreamingHandler(StreamingHandlerConfig{
			Channel:          models.ChannelSlack,
			Behavior:         StreamingBehavior{Mode: StreamingRealTime},
			StreamingAdapter: &mockStreamingAdapter{},
			OutboundAdapter:  &mockOutboundAdapter{},
		})

		if !h.IsEnabled() {
			t.Error("expected handler to be enabled")
		}
	})

	t.Run("disabled mode", func(t *testing.T) {
		h := NewStreamingHandler(StreamingHandlerConfig{
			Channel:          models.ChannelSlack,
			Behavior:         StreamingBehavior{Mode: StreamingDisabled},
			StreamingAdapter: &mockStreamingAdapter{},
			OutboundAdapter:  &mockOutboundAdapter{},
		})

		if h.IsEnabled() {
			t.Error("expected handler to be disabled")
		}
	})

	t.Run("nil adapter", func(t *testing.T) {
		h := NewStreamingHandler(StreamingHandlerConfig{
			Channel:          models.ChannelSlack,
			Behavior:         StreamingBehavior{Mode: StreamingRealTime},
			StreamingAdapter: nil,
			OutboundAdapter:  &mockOutboundAdapter{},
		})

		if h.IsEnabled() {
			t.Error("expected handler to be disabled without adapter")
		}
	})
}

func TestStreamingHandler_SendTypingIndicator(t *testing.T) {
	streaming := &mockStreamingAdapter{}
	h := NewStreamingHandler(StreamingHandlerConfig{
		Channel: models.ChannelSlack,
		Behavior: StreamingBehavior{
			Mode:           StreamingRealTime,
			TypingInterval: 100 * time.Millisecond,
		},
		StreamingAdapter: streaming,
		OutboundAdapter:  &mockOutboundAdapter{},
	})

	msg := &models.Message{ID: "test"}
	ctx := context.Background()

	// First call should send
	if err := h.SendTypingIndicator(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if streaming.typingCalls != 1 {
		t.Errorf("expected 1 typing call, got %d", streaming.typingCalls)
	}

	// Immediate second call should be throttled
	if err := h.SendTypingIndicator(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if streaming.typingCalls != 1 {
		t.Errorf("expected still 1 typing call (throttled), got %d", streaming.typingCalls)
	}

	// After interval, should send again
	time.Sleep(150 * time.Millisecond)
	if err := h.SendTypingIndicator(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if streaming.typingCalls != 2 {
		t.Errorf("expected 2 typing calls, got %d", streaming.typingCalls)
	}
}

func TestStreamingHandler_OnText_RealTime(t *testing.T) {
	streaming := &mockStreamingAdapter{}
	h := NewStreamingHandler(StreamingHandlerConfig{
		Channel: models.ChannelSlack,
		Behavior: StreamingBehavior{
			Mode:           StreamingRealTime,
			UpdateInterval: 50 * time.Millisecond,
		},
		StreamingAdapter: streaming,
		OutboundAdapter:  &mockOutboundAdapter{},
	})

	msg := &models.Message{ID: "test"}
	ctx := context.Background()

	// First text should start streaming
	handled, err := h.OnText(ctx, msg, "Hello")
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Error("expected text to be handled")
	}
	if streaming.startCalls != 1 {
		t.Errorf("expected 1 start call, got %d", streaming.startCalls)
	}

	// Immediate second text should be throttled
	handled, err = h.OnText(ctx, msg, " World")
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Error("expected text to be handled")
	}
	if streaming.updateCalls != 0 {
		t.Errorf("expected 0 update calls (throttled), got %d", streaming.updateCalls)
	}

	// After interval, should update
	time.Sleep(60 * time.Millisecond)
	handled, err = h.OnText(ctx, msg, "!")
	if err != nil {
		t.Fatal(err)
	}
	if streaming.updateCalls != 1 {
		t.Errorf("expected 1 update call, got %d", streaming.updateCalls)
	}
	if streaming.lastContent != "Hello World!" {
		t.Errorf("expected accumulated content 'Hello World!', got %s", streaming.lastContent)
	}
}

func TestStreamingHandler_OnText_TypingOnly(t *testing.T) {
	streaming := &mockStreamingAdapter{}
	h := NewStreamingHandler(StreamingHandlerConfig{
		Channel: models.ChannelTelegram,
		Behavior: StreamingBehavior{
			Mode: StreamingTypingOnly,
		},
		StreamingAdapter: streaming,
		OutboundAdapter:  &mockOutboundAdapter{},
	})

	msg := &models.Message{ID: "test"}
	ctx := context.Background()

	// Text should not be handled in typing-only mode
	handled, err := h.OnText(ctx, msg, "Hello")
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Error("expected text NOT to be handled in typing-only mode")
	}
	if streaming.startCalls != 0 {
		t.Errorf("expected 0 start calls in typing-only mode, got %d", streaming.startCalls)
	}
}

func TestStreamingHandler_OnText_FallbackOnError(t *testing.T) {
	streaming := &mockStreamingAdapter{
		startErr: errors.New("streaming unavailable"),
	}
	h := NewStreamingHandler(StreamingHandlerConfig{
		Channel: models.ChannelSlack,
		Behavior: StreamingBehavior{
			Mode: StreamingRealTime,
		},
		StreamingAdapter: streaming,
		OutboundAdapter:  &mockOutboundAdapter{},
	})

	msg := &models.Message{ID: "test"}
	ctx := context.Background()

	// First text should fail to start streaming and fall back
	handled, err := h.OnText(ctx, msg, "Hello")
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Error("expected text NOT to be handled after fallback")
	}

	// Subsequent text should also not be handled
	handled, err = h.OnText(ctx, msg, " World")
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Error("expected text NOT to be handled in fallback mode")
	}
}

func TestStreamingHandler_Finalize(t *testing.T) {
	t.Run("streaming active", func(t *testing.T) {
		streaming := &mockStreamingAdapter{}
		outbound := &mockOutboundAdapter{}
		h := NewStreamingHandler(StreamingHandlerConfig{
			Channel: models.ChannelSlack,
			Behavior: StreamingBehavior{
				Mode: StreamingRealTime,
			},
			StreamingAdapter: streaming,
			OutboundAdapter:  outbound,
		})

		msg := &models.Message{ID: "test"}
		ctx := context.Background()

		// Start streaming
		_, _ = h.OnText(ctx, msg, "Hello")

		// Finalize should update, not send new message
		if err := h.Finalize(ctx, msg, "Hello World"); err != nil {
			t.Fatal(err)
		}

		if streaming.updateCalls != 1 {
			t.Errorf("expected 1 update call, got %d", streaming.updateCalls)
		}
		if outbound.sendCalls != 0 {
			t.Errorf("expected 0 send calls, got %d", outbound.sendCalls)
		}
	})

	t.Run("non-streaming", func(t *testing.T) {
		streaming := &mockStreamingAdapter{}
		outbound := &mockOutboundAdapter{}
		h := NewStreamingHandler(StreamingHandlerConfig{
			Channel: models.ChannelSlack,
			Behavior: StreamingBehavior{
				Mode: StreamingDisabled,
			},
			StreamingAdapter: streaming,
			OutboundAdapter:  outbound,
		})

		msg := &models.Message{ID: "test"}
		ctx := context.Background()

		// Finalize should send complete message
		if err := h.Finalize(ctx, msg, "Hello World"); err != nil {
			t.Fatal(err)
		}

		if streaming.updateCalls != 0 {
			t.Errorf("expected 0 update calls, got %d", streaming.updateCalls)
		}
		if outbound.sendCalls != 1 {
			t.Errorf("expected 1 send call, got %d", outbound.sendCalls)
		}
		if outbound.lastMessage.Content != "Hello World" {
			t.Errorf("expected content 'Hello World', got %s", outbound.lastMessage.Content)
		}
	})
}

func TestStreamingHandler_Reset(t *testing.T) {
	streaming := &mockStreamingAdapter{}
	h := NewStreamingHandler(StreamingHandlerConfig{
		Channel: models.ChannelSlack,
		Behavior: StreamingBehavior{
			Mode: StreamingRealTime,
		},
		StreamingAdapter: streaming,
		OutboundAdapter:  &mockOutboundAdapter{},
	})

	msg := &models.Message{ID: "test"}
	ctx := context.Background()

	// Start streaming
	_, _ = h.OnText(ctx, msg, "Hello")

	if !h.WasStreaming() {
		t.Error("expected WasStreaming to be true")
	}

	// Reset
	h.Reset()

	if h.WasStreaming() {
		t.Error("expected WasStreaming to be false after reset")
	}
}

func TestStreamingRegistry_GetBehavior(t *testing.T) {
	r := NewStreamingRegistry()

	// Known channel
	behavior := r.GetBehavior(models.ChannelSlack)
	if behavior.Mode != StreamingRealTime {
		t.Errorf("expected Slack to have RealTime mode, got %s", behavior.Mode)
	}

	// Unknown channel should get default
	behavior = r.GetBehavior("unknown")
	if behavior.Mode != StreamingDisabled {
		t.Errorf("expected unknown channel to have Disabled mode, got %s", behavior.Mode)
	}
}

func TestStreamingRegistry_SetBehavior(t *testing.T) {
	r := NewStreamingRegistry()

	// Override Slack behavior
	r.SetBehavior(models.ChannelSlack, StreamingBehavior{
		Mode:           StreamingBuffered,
		UpdateInterval: 5 * time.Second,
	})

	behavior := r.GetBehavior(models.ChannelSlack)
	if behavior.Mode != StreamingBuffered {
		t.Errorf("expected Slack to have Buffered mode, got %s", behavior.Mode)
	}
	if behavior.UpdateInterval != 5*time.Second {
		t.Errorf("expected 5s update interval, got %v", behavior.UpdateInterval)
	}
}

func TestStreamingRegistry_CreateHandler(t *testing.T) {
	r := NewStreamingRegistry()
	streaming := &mockStreamingAdapter{}
	outbound := &mockOutboundAdapter{}

	h := r.CreateHandler(models.ChannelSlack, streaming, outbound)

	if h.Mode() != StreamingRealTime {
		t.Errorf("expected handler to have RealTime mode, got %s", h.Mode())
	}
	if !h.IsEnabled() {
		t.Error("expected handler to be enabled")
	}
}

func TestSplitMessage(t *testing.T) {
	t.Run("no split needed", func(t *testing.T) {
		behavior := StreamingBehavior{
			MaxMessageLength:  100,
			SplitLongMessages: true,
		}
		content := "Hello World"
		parts := SplitMessage(content, behavior)

		if len(parts) != 1 {
			t.Errorf("expected 1 part, got %d", len(parts))
		}
		if parts[0] != content {
			t.Errorf("expected original content, got %s", parts[0])
		}
	})

	t.Run("no limit", func(t *testing.T) {
		behavior := StreamingBehavior{
			MaxMessageLength:  0,
			SplitLongMessages: true,
		}
		content := "Hello World"
		parts := SplitMessage(content, behavior)

		if len(parts) != 1 {
			t.Errorf("expected 1 part, got %d", len(parts))
		}
	})

	t.Run("truncate instead of split", func(t *testing.T) {
		behavior := StreamingBehavior{
			MaxMessageLength:  5,
			SplitLongMessages: false,
		}
		content := "Hello World"
		parts := SplitMessage(content, behavior)

		if len(parts) != 1 {
			t.Errorf("expected 1 part, got %d", len(parts))
		}
		if parts[0] != "Hello" {
			t.Errorf("expected truncated 'Hello', got %s", parts[0])
		}
	})

	t.Run("split at space", func(t *testing.T) {
		behavior := StreamingBehavior{
			MaxMessageLength:  10,
			SplitLongMessages: true,
		}
		content := "Hello World Test"
		parts := SplitMessage(content, behavior)

		if len(parts) != 2 {
			t.Errorf("expected 2 parts, got %d: %v", len(parts), parts)
		}
	})

	t.Run("split at newline", func(t *testing.T) {
		behavior := StreamingBehavior{
			MaxMessageLength:  15,
			SplitLongMessages: true,
		}
		content := "Hello\nWorld\nTest"
		parts := SplitMessage(content, behavior)

		if len(parts) < 2 {
			t.Errorf("expected at least 2 parts, got %d: %v", len(parts), parts)
		}
	})
}

func TestDefaultStreamingBehaviors(t *testing.T) {
	// Verify all known channels have behaviors
	knownChannels := []models.ChannelType{
		models.ChannelTelegram,
		models.ChannelDiscord,
		models.ChannelSlack,
		models.ChannelAPI,
		models.ChannelWhatsApp,
		models.ChannelSignal,
		models.ChannelIMessage,
		models.ChannelMatrix,
	}

	for _, channel := range knownChannels {
		behavior, ok := DefaultStreamingBehaviors[channel]
		if !ok {
			t.Errorf("missing default behavior for %s", channel)
			continue
		}

		// Basic sanity checks
		if behavior.Mode > StreamingTypingOnly {
			t.Errorf("invalid mode for %s: %d", channel, behavior.Mode)
		}
	}

	// Check specific behaviors
	if DefaultStreamingBehaviors[models.ChannelDiscord].Mode != StreamingRealTime {
		t.Error("Discord should support real-time streaming")
	}
	if DefaultStreamingBehaviors[models.ChannelTelegram].Mode != StreamingTypingOnly {
		t.Error("Telegram should use typing-only streaming")
	}
	if DefaultStreamingBehaviors[models.ChannelAPI].UpdateInterval != 0 {
		t.Error("API should have no throttling")
	}
}
