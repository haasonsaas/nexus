// Package gateway provides the main Nexus gateway server.
//
// streaming.go formalizes streaming UX contracts for different channels.
// It defines how streaming responses behave across different platforms
// with their varying capabilities and rate limits.
package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// StreamingMode defines how streaming updates are delivered.
type StreamingMode int

const (
	// StreamingDisabled means no streaming - send complete message at end.
	StreamingDisabled StreamingMode = iota

	// StreamingRealTime provides token-by-token updates (subject to throttling).
	StreamingRealTime

	// StreamingBuffered accumulates content and updates at intervals.
	StreamingBuffered

	// StreamingTypingOnly shows typing indicator but sends complete message.
	StreamingTypingOnly
)

// String returns the streaming mode name.
func (m StreamingMode) String() string {
	switch m {
	case StreamingDisabled:
		return "disabled"
	case StreamingRealTime:
		return "realtime"
	case StreamingBuffered:
		return "buffered"
	case StreamingTypingOnly:
		return "typing_only"
	default:
		return "unknown"
	}
}

// StreamingBehavior defines the streaming characteristics for a channel.
type StreamingBehavior struct {
	// Mode determines how streaming updates are delivered.
	Mode StreamingMode

	// UpdateInterval is the minimum time between streaming updates.
	// Used to avoid hitting API rate limits.
	UpdateInterval time.Duration

	// TypingInterval is how often to refresh the typing indicator.
	TypingInterval time.Duration

	// TypingDuration is how long a typing indicator lasts before expiring.
	// Some platforms auto-expire typing indicators after a certain time.
	TypingDuration time.Duration

	// MaxMessageLength is the maximum message length the platform supports.
	// Zero means no limit.
	MaxMessageLength int

	// SupportsEdit indicates if the platform supports editing sent messages.
	SupportsEdit bool

	// SupportsMarkdown indicates if the platform renders markdown.
	SupportsMarkdown bool

	// SplitLongMessages indicates if long messages should be split.
	SplitLongMessages bool
}

// DefaultStreamingBehaviors provides sensible defaults for each channel.
var DefaultStreamingBehaviors = map[models.ChannelType]StreamingBehavior{
	models.ChannelTelegram: {
		Mode:              StreamingTypingOnly, // Telegram doesn't support message editing in real-time well
		UpdateInterval:    2 * time.Second,
		TypingInterval:    4 * time.Second,
		TypingDuration:    5 * time.Second,
		MaxMessageLength:  4096,
		SupportsEdit:      true,
		SupportsMarkdown:  true,
		SplitLongMessages: true,
	},
	models.ChannelDiscord: {
		Mode:              StreamingRealTime,
		UpdateInterval:    1 * time.Second, // Discord rate limits message edits
		TypingInterval:    4 * time.Second,
		TypingDuration:    10 * time.Second,
		MaxMessageLength:  2000,
		SupportsEdit:      true,
		SupportsMarkdown:  true,
		SplitLongMessages: true,
	},
	models.ChannelSlack: {
		Mode:              StreamingRealTime,
		UpdateInterval:    1 * time.Second, // Slack has API rate limits
		TypingInterval:    3 * time.Second,
		TypingDuration:    3 * time.Second,
		MaxMessageLength:  40000,
		SupportsEdit:      true,
		SupportsMarkdown:  true, // Slack uses mrkdwn
		SplitLongMessages: false,
	},
	models.ChannelAPI: {
		Mode:              StreamingRealTime,
		UpdateInterval:    0, // No throttling for API
		TypingInterval:    0,
		TypingDuration:    0,
		MaxMessageLength:  0, // No limit
		SupportsEdit:      false,
		SupportsMarkdown:  true,
		SplitLongMessages: false,
	},
	models.ChannelWhatsApp: {
		Mode:              StreamingTypingOnly,
		UpdateInterval:    0,
		TypingInterval:    4 * time.Second,
		TypingDuration:    5 * time.Second,
		MaxMessageLength:  65536,
		SupportsEdit:      false, // WhatsApp doesn't support editing
		SupportsMarkdown:  true,
		SplitLongMessages: true,
	},
	models.ChannelSignal: {
		Mode:              StreamingTypingOnly,
		UpdateInterval:    0,
		TypingInterval:    4 * time.Second,
		TypingDuration:    5 * time.Second,
		MaxMessageLength:  0,
		SupportsEdit:      false,
		SupportsMarkdown:  false,
		SplitLongMessages: false,
	},
	models.ChannelIMessage: {
		Mode:              StreamingTypingOnly,
		UpdateInterval:    0,
		TypingInterval:    0, // iMessage typing is per-message
		TypingDuration:    0,
		MaxMessageLength:  0,
		SupportsEdit:      false,
		SupportsMarkdown:  false,
		SplitLongMessages: false,
	},
	models.ChannelMatrix: {
		Mode:              StreamingRealTime,
		UpdateInterval:    1 * time.Second,
		TypingInterval:    4 * time.Second,
		TypingDuration:    30 * time.Second,
		MaxMessageLength:  0,
		SupportsEdit:      true,
		SupportsMarkdown:  true,
		SplitLongMessages: false,
	},
}

// StreamingHandler manages the streaming lifecycle for a single response.
type StreamingHandler struct {
	mu sync.Mutex

	// Channel information
	channel  models.ChannelType
	behavior StreamingBehavior

	// Adapters
	streaming channels.StreamingAdapter
	outbound  channels.OutboundAdapter

	// Stream manager
	manager *StreamManager

	// Typing state
	lastTyping time.Time
}

// StreamingHandlerConfig configures a StreamingHandler.
type StreamingHandlerConfig struct {
	Channel          models.ChannelType
	Behavior         StreamingBehavior
	StreamingAdapter channels.StreamingAdapter
	OutboundAdapter  channels.OutboundAdapter
}

// NewStreamingHandler creates a new streaming handler.
func NewStreamingHandler(cfg StreamingHandlerConfig) *StreamingHandler {
	return &StreamingHandler{
		channel:   cfg.Channel,
		behavior:  cfg.Behavior,
		streaming: cfg.StreamingAdapter,
		outbound:  cfg.OutboundAdapter,
		manager:   NewStreamManager(cfg.Behavior, cfg.StreamingAdapter, cfg.OutboundAdapter),
	}
}

// Behavior returns the streaming behavior configuration.
func (h *StreamingHandler) Behavior() StreamingBehavior {
	return h.behavior
}

// Mode returns the configured streaming mode.
func (h *StreamingHandler) Mode() StreamingMode {
	return h.behavior.Mode
}

// IsEnabled returns true if streaming is enabled for this handler.
func (h *StreamingHandler) IsEnabled() bool {
	if h.manager == nil {
		return false
	}
	return h.manager.IsEnabled()
}

// SendTypingIndicator sends a typing indicator if supported and needed.
func (h *StreamingHandler) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	if h.streaming == nil {
		return nil
	}
	if h.behavior.Mode == StreamingDisabled {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if we should refresh
	if h.behavior.TypingInterval > 0 && time.Since(h.lastTyping) < h.behavior.TypingInterval {
		return nil
	}

	if err := h.streaming.SendTypingIndicator(ctx, msg); err != nil {
		return err
	}
	h.lastTyping = time.Now()
	return nil
}

// ShouldRefreshTyping returns true if the typing indicator should be refreshed.
func (h *StreamingHandler) ShouldRefreshTyping() bool {
	if h.behavior.TypingInterval == 0 {
		return false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Since(h.lastTyping) >= h.behavior.TypingInterval
}

// OnText handles incoming text from the LLM stream.
// Returns true if the text was handled via streaming, false if it should be buffered.
func (h *StreamingHandler) OnText(ctx context.Context, msg *models.Message, text string) (handled bool, err error) {
	if h.manager == nil {
		return false, nil
	}
	return h.manager.OnText(ctx, msg, text)
}

// Finalize completes the streaming response.
// If streaming was active, sends final update. Otherwise sends complete message.
func (h *StreamingHandler) Finalize(ctx context.Context, msg *models.Message, content string) error {
	if h.manager == nil {
		return h.outbound.Send(ctx, msg)
	}
	return h.manager.Finalize(ctx, msg, content)
}

// WasStreaming returns true if streaming was actually used for this response.
func (h *StreamingHandler) WasStreaming() bool {
	if h.manager == nil {
		return false
	}
	return h.manager.WasStreaming()
}

// Reset prepares the handler for a new response.
func (h *StreamingHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.manager != nil {
		h.manager.Reset()
	}
	h.lastTyping = time.Time{}
}

// StreamingRegistry manages streaming behaviors and handlers.
type StreamingRegistry struct {
	mu        sync.RWMutex
	behaviors map[models.ChannelType]StreamingBehavior
}

// NewStreamingRegistry creates a new streaming registry with default behaviors.
func NewStreamingRegistry() *StreamingRegistry {
	r := &StreamingRegistry{
		behaviors: make(map[models.ChannelType]StreamingBehavior),
	}
	// Copy default behaviors
	for channel, behavior := range DefaultStreamingBehaviors {
		r.behaviors[channel] = behavior
	}
	return r
}

// GetBehavior returns the streaming behavior for a channel.
func (r *StreamingRegistry) GetBehavior(channel models.ChannelType) StreamingBehavior {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if behavior, ok := r.behaviors[channel]; ok {
		return behavior
	}
	// Default fallback
	return StreamingBehavior{
		Mode:              StreamingDisabled,
		UpdateInterval:    time.Second,
		TypingInterval:    4 * time.Second,
		SplitLongMessages: false,
	}
}

// SetBehavior configures the streaming behavior for a channel.
func (r *StreamingRegistry) SetBehavior(channel models.ChannelType, behavior StreamingBehavior) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.behaviors[channel] = behavior
}

// CreateHandler creates a streaming handler for a channel with its adapters.
func (r *StreamingRegistry) CreateHandler(
	channel models.ChannelType,
	streaming channels.StreamingAdapter,
	outbound channels.OutboundAdapter,
) *StreamingHandler {
	behavior := r.GetBehavior(channel)
	return NewStreamingHandler(StreamingHandlerConfig{
		Channel:          channel,
		Behavior:         behavior,
		StreamingAdapter: streaming,
		OutboundAdapter:  outbound,
	})
}

// SplitMessage splits a message if it exceeds the channel's limit.
func SplitMessage(content string, behavior StreamingBehavior) []string {
	if behavior.MaxMessageLength == 0 || len(content) <= behavior.MaxMessageLength {
		return []string{content}
	}

	if !behavior.SplitLongMessages {
		// Truncate instead of split
		return []string{content[:behavior.MaxMessageLength]}
	}

	var parts []string
	remaining := content

	for len(remaining) > 0 {
		if len(remaining) <= behavior.MaxMessageLength {
			parts = append(parts, remaining)
			break
		}

		// Find a good split point (newline, space, or just max length)
		splitAt := behavior.MaxMessageLength
		chunk := remaining[:splitAt]

		// Try to split at last newline
		if idx := lastIndexNewline(chunk); idx > splitAt/2 {
			splitAt = idx + 1
		} else if idx := lastIndexSpace(chunk); idx > splitAt/2 {
			// Try to split at last space
			splitAt = idx + 1
		}

		parts = append(parts, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return parts
}

func lastIndexNewline(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}

func lastIndexSpace(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ' ' || s[i] == '\t' {
			return i
		}
	}
	return -1
}
