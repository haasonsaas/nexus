package personal

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// BaseAdapter provides common functionality for personal messaging adapters.
type BaseAdapter struct {
	channelType models.ChannelType
	messages    chan *models.Message
	config      *Config
	logger      *slog.Logger

	contacts   map[string]*Contact
	contactsMu sync.RWMutex

	status   channels.Status
	statusMu sync.RWMutex

	metrics   *adapterMetrics
	metricsMu sync.RWMutex
}

type adapterMetrics struct {
	messagesSent     uint64
	messagesReceived uint64
	messagesFailed   uint64
	lastActivity     time.Time
}

// NewBaseAdapter creates a new base personal adapter.
func NewBaseAdapter(channelType models.ChannelType, cfg *Config, logger *slog.Logger) *BaseAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		cfg = &Config{}
	}

	return &BaseAdapter{
		channelType: channelType,
		messages:    make(chan *models.Message, 100),
		config:      cfg,
		logger:      logger.With("channel", string(channelType)),
		contacts:    make(map[string]*Contact),
		metrics:     &adapterMetrics{},
	}
}

// Type returns the channel type.
func (b *BaseAdapter) Type() models.ChannelType {
	return b.channelType
}

// Messages returns the channel for receiving inbound messages.
func (b *BaseAdapter) Messages() <-chan *models.Message {
	return b.messages
}

// Status returns the current connection status.
func (b *BaseAdapter) Status() channels.Status {
	b.statusMu.RLock()
	defer b.statusMu.RUnlock()
	return b.status
}

// SetStatus updates the connection status.
func (b *BaseAdapter) SetStatus(connected bool, err string) {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	b.status = channels.Status{
		Connected: connected,
		Error:     err,
		LastPing:  time.Now().Unix(),
	}
}

// Metrics returns the current metrics snapshot.
func (b *BaseAdapter) Metrics() channels.MetricsSnapshot {
	b.metricsMu.RLock()
	defer b.metricsMu.RUnlock()
	return channels.MetricsSnapshot{
		ChannelType:      b.channelType,
		MessagesSent:     b.metrics.messagesSent,
		MessagesReceived: b.metrics.messagesReceived,
		MessagesFailed:   b.metrics.messagesFailed,
	}
}

// IncrementSent increments the sent message counter.
func (b *BaseAdapter) IncrementSent() {
	b.metricsMu.Lock()
	defer b.metricsMu.Unlock()
	b.metrics.messagesSent++
	b.metrics.lastActivity = time.Now()
}

// IncrementReceived increments the received message counter.
func (b *BaseAdapter) IncrementReceived() {
	b.metricsMu.Lock()
	defer b.metricsMu.Unlock()
	b.metrics.messagesReceived++
	b.metrics.lastActivity = time.Now()
}

// IncrementErrors increments the error counter.
func (b *BaseAdapter) IncrementErrors() {
	b.metricsMu.Lock()
	defer b.metricsMu.Unlock()
	b.metrics.messagesFailed++
}

// Logger returns the adapter's logger.
func (b *BaseAdapter) Logger() *slog.Logger {
	return b.logger
}

// Config returns the adapter's configuration.
func (b *BaseAdapter) Config() *Config {
	return b.config
}

// NormalizeInbound converts a raw message to the standard format.
func (b *BaseAdapter) NormalizeInbound(raw RawMessage) *models.Message {
	msg := &models.Message{
		ID:        raw.ID,
		Channel:   b.channelType,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   raw.Content,
		Metadata: map[string]any{
			"peer_id":   raw.PeerID,
			"peer_name": raw.PeerName,
		},
		CreatedAt: raw.Timestamp,
	}

	if raw.GroupID != "" {
		msg.Metadata["group_id"] = raw.GroupID
		msg.Metadata["group_name"] = raw.GroupName
	}

	if raw.ReplyTo != "" {
		msg.Metadata["reply_to"] = raw.ReplyTo
	}

	for k, v := range raw.Extra {
		msg.Metadata[k] = v
	}

	return msg
}

// ProcessAttachments adds attachments to a message.
func (b *BaseAdapter) ProcessAttachments(raw RawMessage, msg *models.Message) {
	for _, att := range raw.Attachments {
		msg.Attachments = append(msg.Attachments, models.Attachment{
			ID:       att.ID,
			Type:     att.MIMEType,
			URL:      att.URL,
			Filename: att.Filename,
			MimeType: att.MIMEType,
			Size:     att.Size,
		})
	}
}

// Emit sends a message to the inbound channel.
func (b *BaseAdapter) Emit(msg *models.Message) bool {
	select {
	case b.messages <- msg:
		b.IncrementReceived()
		return true
	default:
		b.logger.Warn("message channel full, dropping message",
			"message_id", msg.ID)
		return false
	}
}

// GetContact retrieves a cached contact.
func (b *BaseAdapter) GetContact(id string) (*Contact, bool) {
	b.contactsMu.RLock()
	defer b.contactsMu.RUnlock()
	c, ok := b.contacts[id]
	return c, ok
}

// SetContact caches a contact.
func (b *BaseAdapter) SetContact(contact *Contact) {
	if contact == nil || contact.ID == "" {
		return
	}
	b.contactsMu.Lock()
	defer b.contactsMu.Unlock()
	b.contacts[contact.ID] = contact
}

// Close closes the messages channel.
func (b *BaseAdapter) Close() {
	close(b.messages)
}

// BaseContactManager provides a stub implementation of ContactManager.
type BaseContactManager struct {
	adapter *BaseAdapter
}

// NewBaseContactManager creates a new base contact manager.
func NewBaseContactManager(adapter *BaseAdapter) *BaseContactManager {
	return &BaseContactManager{adapter: adapter}
}

func (m *BaseContactManager) Resolve(ctx context.Context, identifier string) (*Contact, error) {
	if c, ok := m.adapter.GetContact(identifier); ok {
		return c, nil
	}
	return nil, nil
}

func (m *BaseContactManager) Search(ctx context.Context, query string) ([]*Contact, error) {
	return nil, nil
}

func (m *BaseContactManager) Sync(ctx context.Context) error {
	return nil
}

func (m *BaseContactManager) GetByID(ctx context.Context, id string) (*Contact, error) {
	if c, ok := m.adapter.GetContact(id); ok {
		return c, nil
	}
	return nil, nil
}

// BaseMediaHandler provides a stub implementation of MediaHandler.
type BaseMediaHandler struct{}

func (h *BaseMediaHandler) Download(ctx context.Context, mediaID string) ([]byte, string, error) {
	return nil, "", nil
}

func (h *BaseMediaHandler) Upload(ctx context.Context, data []byte, mimeType string, filename string) (string, error) {
	return "", nil
}

func (h *BaseMediaHandler) GetURL(ctx context.Context, mediaID string) (string, error) {
	return "", nil
}

// BasePresenceManager provides a stub implementation of PresenceManager.
type BasePresenceManager struct{}

func (p *BasePresenceManager) SetTyping(ctx context.Context, peerID string, typing bool) error {
	return nil
}

func (p *BasePresenceManager) SetOnline(ctx context.Context, online bool) error {
	return nil
}

func (p *BasePresenceManager) Subscribe(ctx context.Context, peerID string) (<-chan PresenceEvent, error) {
	return nil, nil
}

func (p *BasePresenceManager) MarkRead(ctx context.Context, peerID string, messageID string) error {
	return nil
}
