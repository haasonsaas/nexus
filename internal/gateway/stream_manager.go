package gateway

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// StreamManager handles buffered streaming updates for a single response.
type StreamManager struct {
	mu sync.Mutex

	behavior  StreamingBehavior
	streaming channels.StreamingAdapter
	outbound  channels.OutboundAdapter

	started     atomic.Bool
	messageID   string
	lastUpdate  time.Time
	accumulated string

	fallbackToNonStreaming atomic.Bool
}

// NewStreamManager creates a new stream manager.
func NewStreamManager(behavior StreamingBehavior, streaming channels.StreamingAdapter, outbound channels.OutboundAdapter) *StreamManager {
	return &StreamManager{
		behavior:  behavior,
		streaming: streaming,
		outbound:  outbound,
	}
}

// IsEnabled returns true if streaming is enabled.
func (m *StreamManager) IsEnabled() bool {
	return m.behavior.Mode != StreamingDisabled && m.streaming != nil
}

// OnText handles incoming text from the LLM stream.
// Returns true if the text was handled via streaming, false if it should be buffered.
func (m *StreamManager) OnText(ctx context.Context, msg *models.Message, text string) (bool, error) {
	if !m.IsEnabled() || m.behavior.Mode == StreamingTypingOnly {
		return false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.accumulated += text

	// Start streaming on first text
	if !m.started.Load() {
		messageID, err := m.streaming.StartStreamingResponse(ctx, msg)
		if err != nil {
			m.fallbackToNonStreaming.Store(true)
			return false, nil
		}
		m.messageID = messageID
		m.started.Store(true)
		m.lastUpdate = time.Now()
		return true, nil
	}

	if m.fallbackToNonStreaming.Load() {
		return false, nil
	}

	// Throttle updates
	if m.behavior.UpdateInterval > 0 && time.Since(m.lastUpdate) < m.behavior.UpdateInterval {
		return true, nil
	}

	if err := m.streaming.UpdateStreamingResponse(ctx, msg, m.messageID, m.accumulated); err != nil {
		// Continue accumulating; send at finalize.
		return true, nil
	}
	m.lastUpdate = time.Now()
	return true, nil
}

// Finalize completes the streaming response.
func (m *StreamManager) Finalize(ctx context.Context, msg *models.Message, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	msg.Content = content

	if m.started.Load() && !m.fallbackToNonStreaming.Load() && m.messageID != "" {
		if err := m.streaming.UpdateStreamingResponse(ctx, msg, m.messageID, content); err != nil {
			return m.outbound.Send(ctx, msg)
		}
		return nil
	}

	return m.outbound.Send(ctx, msg)
}

// WasStreaming reports whether streaming was used.
func (m *StreamManager) WasStreaming() bool {
	return m.started.Load() && !m.fallbackToNonStreaming.Load()
}

// Reset clears the manager for a new response.
func (m *StreamManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.started.Store(false)
	m.fallbackToNonStreaming.Store(false)
	m.messageID = ""
	m.lastUpdate = time.Time{}
	m.accumulated = ""
}
