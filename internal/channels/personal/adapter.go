package personal

import (
	"context"

	"github.com/haasonsaas/nexus/internal/channels"
)

// Adapter extends the base channel adapter with personal messaging features.
type Adapter interface {
	channels.Adapter
	channels.LifecycleAdapter
	channels.OutboundAdapter
	channels.InboundAdapter
	channels.HealthAdapter

	// Personal messaging features
	Contacts() ContactManager
	Media() MediaHandler
	Presence() PresenceManager

	// Conversation management
	GetConversation(ctx context.Context, peerID string) (*Conversation, error)
	ListConversations(ctx context.Context, opts ListOptions) ([]*Conversation, error)
}

// ContactManager handles contact operations.
type ContactManager interface {
	// Resolve finds a contact by identifier (phone, JID, etc.).
	Resolve(ctx context.Context, identifier string) (*Contact, error)

	// Search finds contacts matching a query.
	Search(ctx context.Context, query string) ([]*Contact, error)

	// Sync synchronizes contacts from the remote service.
	Sync(ctx context.Context) error

	// GetByID returns a contact by ID.
	GetByID(ctx context.Context, id string) (*Contact, error)
}

// MediaHandler handles media upload/download operations.
type MediaHandler interface {
	// Download retrieves media data by ID.
	Download(ctx context.Context, mediaID string) (data []byte, mimeType string, err error)

	// Upload uploads media and returns a reference ID.
	Upload(ctx context.Context, data []byte, mimeType string, filename string) (mediaID string, err error)

	// GetURL returns a URL for accessing media.
	GetURL(ctx context.Context, mediaID string) (string, error)
}

// PresenceManager handles online/typing status.
type PresenceManager interface {
	// SetTyping sets typing indicator for a peer.
	SetTyping(ctx context.Context, peerID string, typing bool) error

	// SetOnline sets online status.
	SetOnline(ctx context.Context, online bool) error

	// Subscribe subscribes to presence events for a peer.
	Subscribe(ctx context.Context, peerID string) (<-chan PresenceEvent, error)

	// MarkRead marks messages as read up to a given message ID.
	MarkRead(ctx context.Context, peerID string, messageID string) error
}

// MessageSender provides a simplified interface for sending messages.
type MessageSender interface {
	// SendText sends a text message.
	SendText(ctx context.Context, peerID string, text string) error

	// SendMedia sends media with optional caption.
	SendMedia(ctx context.Context, peerID string, mediaID string, caption string) error

	// SendReply sends a reply to a specific message.
	SendReply(ctx context.Context, peerID string, replyToID string, text string) error
}
