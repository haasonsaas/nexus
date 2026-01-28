package personal

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// BaseAdapter provides common functionality for personal messaging adapters.
type BaseAdapter struct {
	channelType models.ChannelType
	messages    chan *models.Message
	config      *Config
	logger      *slog.Logger
	health      *channels.BaseHealthAdapter

	contacts   map[string]*Contact
	contactsMu sync.RWMutex
}

// NewBaseAdapter creates a new base personal adapter.
func NewBaseAdapter(channelType models.ChannelType, cfg *Config, logger *slog.Logger) *BaseAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		cfg = &Config{}
	}

	logger = logger.With("channel", string(channelType))
	health := channels.NewBaseHealthAdapter(channelType, logger)
	return &BaseAdapter{
		channelType: channelType,
		messages:    make(chan *models.Message, 100),
		config:      cfg,
		logger:      logger,
		health:      health,
		contacts:    make(map[string]*Contact),
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
	if b.health == nil {
		return channels.Status{}
	}
	return b.health.Status()
}

// SetStatus updates the connection status.
func (b *BaseAdapter) SetStatus(connected bool, err string) {
	if b.health == nil {
		return
	}
	b.health.SetStatus(connected, err)
}

// Metrics returns the current metrics snapshot.
func (b *BaseAdapter) Metrics() channels.MetricsSnapshot {
	if b.health == nil {
		return channels.MetricsSnapshot{ChannelType: b.channelType}
	}
	return b.health.Metrics()
}

// IncrementSent increments the sent message counter.
func (b *BaseAdapter) IncrementSent() {
	if b.health != nil {
		b.health.RecordMessageSent()
	}
}

// IncrementReceived increments the received message counter.
func (b *BaseAdapter) IncrementReceived() {
	if b.health != nil {
		b.health.RecordMessageReceived()
	}
}

// IncrementErrors increments the error counter.
func (b *BaseAdapter) IncrementErrors() {
	if b.health != nil {
		b.health.RecordMessageFailed()
	}
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
			"peer_id":           raw.PeerID,
			"peer_name":         raw.PeerName,
			"sender_id":         raw.PeerID,
			"sender_name":       raw.PeerName,
			"conversation_type": "dm",
		},
		CreatedAt: raw.Timestamp,
	}

	if raw.GroupID != "" {
		msg.Metadata["group_id"] = raw.GroupID
		msg.Metadata["group_name"] = raw.GroupName
		msg.Metadata["conversation_type"] = "group"
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
	if m == nil || m.adapter == nil {
		return nil, channels.ErrUnavailable("contact search unavailable", nil)
	}
	q := strings.TrimSpace(query)
	m.adapter.contactsMu.RLock()
	defer m.adapter.contactsMu.RUnlock()

	results := make([]*Contact, 0, len(m.adapter.contacts))
	if q == "" {
		for _, contact := range m.adapter.contacts {
			results = append(results, contact)
		}
		sort.Slice(results, func(i, j int) bool {
			return strings.ToLower(results[i].ID) < strings.ToLower(results[j].ID)
		})
		return results, nil
	}

	q = strings.ToLower(q)
	for _, contact := range m.adapter.contacts {
		if contact == nil {
			continue
		}
		if matchesContactQuery(contact, q) {
			results = append(results, contact)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].ID) < strings.ToLower(results[j].ID)
	})
	return results, nil
}

func (m *BaseContactManager) Sync(ctx context.Context) error {
	return fmt.Errorf("contact sync: %w", channels.ErrNotSupported)
}

func (m *BaseContactManager) GetByID(ctx context.Context, id string) (*Contact, error) {
	if c, ok := m.adapter.GetContact(id); ok {
		return c, nil
	}
	return nil, nil
}

func matchesContactQuery(contact *Contact, query string) bool {
	if contact == nil {
		return false
	}
	if containsFold(contact.ID, query) ||
		containsFold(contact.Name, query) ||
		containsFold(contact.Phone, query) ||
		containsFold(contact.Email, query) {
		return true
	}
	return false
}

func containsFold(value string, query string) bool {
	if query == "" {
		return true
	}
	if strings.TrimSpace(value) == "" {
		return false
	}
	return strings.Contains(strings.ToLower(value), query)
}

// BaseMediaHandler provides a stub implementation of MediaHandler.
type BaseMediaHandler struct{}

func (h *BaseMediaHandler) Download(ctx context.Context, mediaID string) ([]byte, string, error) {
	return nil, "", fmt.Errorf("media download: %w", channels.ErrNotSupported)
}

func (h *BaseMediaHandler) Upload(ctx context.Context, data []byte, mimeType string, filename string) (string, error) {
	return "", fmt.Errorf("media upload: %w", channels.ErrNotSupported)
}

func (h *BaseMediaHandler) GetURL(ctx context.Context, mediaID string) (string, error) {
	return "", fmt.Errorf("media url: %w", channels.ErrNotSupported)
}

// BasePresenceManager provides a stub implementation of PresenceManager.
type BasePresenceManager struct{}

func (p *BasePresenceManager) SetTyping(ctx context.Context, peerID string, typing bool) error {
	return fmt.Errorf("presence typing: %w", channels.ErrNotSupported)
}

func (p *BasePresenceManager) SetOnline(ctx context.Context, online bool) error {
	return fmt.Errorf("presence online: %w", channels.ErrNotSupported)
}

func (p *BasePresenceManager) Subscribe(ctx context.Context, peerID string) (<-chan PresenceEvent, error) {
	return nil, fmt.Errorf("presence subscribe: %w", channels.ErrNotSupported)
}

func (p *BasePresenceManager) MarkRead(ctx context.Context, peerID string, messageID string) error {
	return fmt.Errorf("presence mark read: %w", channels.ErrNotSupported)
}
