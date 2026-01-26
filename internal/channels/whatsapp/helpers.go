package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	channelcontext "github.com/haasonsaas/nexus/internal/channels/context"
	"github.com/haasonsaas/nexus/internal/channels/personal"

	"go.mau.fi/whatsmeow/types"
)

// contactManager implements personal.ContactManager for WhatsApp.
type contactManager struct {
	adapter *Adapter
}

func (c *contactManager) Resolve(ctx context.Context, identifier string) (*personal.Contact, error) {
	// First check cache
	if contact, ok := c.adapter.GetContact(identifier); ok {
		return contact, nil
	}

	jid, err := types.ParseJID(identifier)
	if err != nil {
		// Try as phone number
		jid = types.NewJID(identifier, types.DefaultUserServer)
	}

	contact, err := c.adapter.client.Store.Contacts.GetContact(ctx, jid)
	if err != nil {
		return nil, channels.ErrNotFound("contact not found", err)
	}

	result := &personal.Contact{
		ID:    jid.String(),
		Name:  contact.FullName,
		Phone: jid.User,
	}

	if contact.PushName != "" && result.Name == "" {
		result.Name = contact.PushName
	}

	c.adapter.SetContact(result)
	return result, nil
}

func (c *contactManager) Search(ctx context.Context, query string) ([]*personal.Contact, error) {
	// whatsmeow doesn't have a contact search API
	// We could search our local cache
	return nil, nil
}

func (c *contactManager) Sync(ctx context.Context) error {
	// Sync contacts from WhatsApp
	contacts, err := c.adapter.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return channels.ErrConnection("failed to get contacts", err)
	}

	for jid, contact := range contacts {
		c.adapter.SetContact(&personal.Contact{
			ID:    jid.String(),
			Name:  contact.FullName,
			Phone: jid.User,
		})
	}

	return nil
}

func (c *contactManager) GetByID(ctx context.Context, id string) (*personal.Contact, error) {
	return c.Resolve(ctx, id)
}

// mediaHandler implements personal.MediaHandler for WhatsApp.
type mediaHandler struct {
	adapter *Adapter
}

func (m *mediaHandler) Download(ctx context.Context, mediaID string) ([]byte, string, error) {
	// WhatsApp media download requires the full media message
	// This is a simplified stub - real implementation would need message history
	return nil, "", channels.ErrUnavailable("media download not implemented", nil)
}

func (m *mediaHandler) Upload(ctx context.Context, data []byte, mimeType string, filename string) (string, error) {
	// WhatsApp media upload is done inline with sending
	// This is a simplified stub
	return "", channels.ErrUnavailable("standalone media upload not supported", nil)
}

func (m *mediaHandler) GetURL(ctx context.Context, mediaID string) (string, error) {
	return "", channels.ErrUnavailable("media URL not available", nil)
}

// presenceManager implements personal.PresenceManager for WhatsApp.
type presenceManager struct {
	adapter *Adapter
}

func (p *presenceManager) SetTyping(ctx context.Context, peerID string, typing bool) error {
	if !p.adapter.config.Personal.Presence.SendTyping {
		return nil
	}

	jid, err := types.ParseJID(peerID)
	if err != nil {
		return channels.ErrInvalidInput("invalid peer ID", err)
	}

	var presence types.ChatPresence
	if typing {
		presence = types.ChatPresenceComposing
	} else {
		presence = types.ChatPresencePaused
	}

	return p.adapter.client.SendChatPresence(ctx, jid, presence, types.ChatPresenceMediaText)
}

func (p *presenceManager) SetOnline(ctx context.Context, online bool) error {
	if !p.adapter.config.Personal.Presence.BroadcastOnline {
		return nil
	}

	var presence types.Presence
	if online {
		presence = types.PresenceAvailable
	} else {
		presence = types.PresenceUnavailable
	}

	return p.adapter.client.SendPresence(ctx, presence)
}

func (p *presenceManager) Subscribe(ctx context.Context, peerID string) (<-chan personal.PresenceEvent, error) {
	jid, err := types.ParseJID(peerID)
	if err != nil {
		return nil, channels.ErrInvalidInput("invalid peer ID", err)
	}

	if err := p.adapter.client.SubscribePresence(ctx, jid); err != nil {
		return nil, channels.ErrConnection("failed to subscribe", err)
	}

	// Note: Events will come through the main event handler
	// This is a simplified implementation
	ch := make(chan personal.PresenceEvent, 10)
	return ch, nil
}

func (p *presenceManager) MarkRead(ctx context.Context, peerID string, messageID string) error {
	if !p.adapter.config.Personal.Presence.SendReadReceipts {
		return nil
	}

	jid, err := types.ParseJID(peerID)
	if err != nil {
		return channels.ErrInvalidInput("invalid peer ID", err)
	}

	return p.adapter.client.MarkRead(ctx, []types.MessageID{types.MessageID(messageID)}, time.Now(), jid, jid)
}

// downloadURL downloads content from a URL.
func downloadURL(url string) ([]byte, error) {
	raw := strings.TrimSpace(url)
	if raw == "" {
		return nil, channels.ErrInvalidInput("missing attachment url", nil)
	}

	maxBytes := channelcontext.GetChannelInfo("whatsapp").MaxAttachmentBytes
	if maxBytes <= 0 {
		maxBytes = 16 * 1024 * 1024
	}

	if strings.HasPrefix(raw, "data:") {
		payload, err := decodeDataURL(raw)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > maxBytes {
			return nil, channels.ErrConnection(fmt.Sprintf("download too large (%d bytes)", len(payload)), nil)
		}
		return payload, nil
	}

	path := raw
	if strings.HasPrefix(path, "file://") {
		path = strings.TrimPrefix(path, "file://")
	}
	if strings.TrimSpace(path) != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if info.Size() > maxBytes {
				return nil, channels.ErrConnection(fmt.Sprintf("download too large (%d bytes)", info.Size()), nil)
			}
			f, err := os.Open(path)
			if err != nil {
				return nil, channels.ErrConnection("failed to open attachment file", err)
			}
			defer f.Close()

			payload, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
			if err != nil {
				return nil, err
			}
			if int64(len(payload)) > maxBytes {
				return nil, channels.ErrConnection(fmt.Sprintf("download too large (%d bytes)", len(payload)), nil)
			}
			return payload, nil
		}
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, channels.ErrConnection(fmt.Sprintf("unexpected status code: %d", resp.StatusCode), nil)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, channels.ErrConnection(fmt.Sprintf("download too large (%d bytes)", len(data)), nil)
	}
	return data, nil
}

func decodeDataURL(raw string) ([]byte, error) {
	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return nil, channels.ErrInvalidInput("invalid data url format", nil)
	}

	meta := strings.TrimPrefix(parts[0], "data:")
	payload := parts[1]

	base64Encoded := false
	for _, seg := range strings.Split(meta, ";") {
		if strings.EqualFold(strings.TrimSpace(seg), "base64") {
			base64Encoded = true
			break
		}
	}
	if !base64Encoded {
		return nil, channels.ErrInvalidInput("data url must be base64 encoded", nil)
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, channels.ErrInvalidInput("decode data url", err)
	}
	return decoded, nil
}
