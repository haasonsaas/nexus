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

	"github.com/google/uuid"
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
	if c == nil || c.adapter == nil || c.adapter.client == nil {
		return nil, channels.ErrUnavailable("contact search unavailable", nil)
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return []*personal.Contact{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	contacts, err := c.adapter.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, channels.ErrConnection("failed to get contacts", err)
	}
	q = strings.ToLower(q)
	results := make([]*personal.Contact, 0)
	for jid, contact := range contacts {
		name := contact.FullName
		if name == "" {
			name = contact.PushName
		}
		if !matchesQuery(q, jid.String(), jid.User, name, contact.PushName) {
			continue
		}
		result := &personal.Contact{
			ID:    jid.String(),
			Name:  name,
			Phone: jid.User,
		}
		if result.Name == "" {
			result.Name = jid.User
		}
		c.adapter.SetContact(result)
		results = append(results, result)
		if len(results) >= 50 {
			break
		}
	}
	return results, nil
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

func matchesQuery(query string, values ...string) bool {
	if query == "" {
		return true
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

// mediaHandler implements personal.MediaHandler for WhatsApp.
type mediaHandler struct {
	adapter *Adapter
}

func (m *mediaHandler) Download(ctx context.Context, mediaID string) ([]byte, string, error) {
	if m == nil || m.adapter == nil {
		return nil, "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return nil, "", channels.ErrInvalidInput("media id required", nil)
	}
	entry, ok := m.adapter.getMedia(mediaID)
	if !ok {
		return nil, "", channels.ErrNotFound("media not found", nil)
	}
	data := entry.data
	if len(data) == 0 && entry.path != "" {
		payload, err := os.ReadFile(entry.path)
		if err != nil {
			return nil, "", channels.ErrConnection("failed to read media", err)
		}
		data = payload
		m.adapter.mediaMu.Lock()
		entry.data = payload
		m.adapter.mediaCache[mediaID] = entry
		m.adapter.mediaMu.Unlock()
	}
	if len(data) == 0 {
		return nil, "", channels.ErrNotFound("media not found", nil)
	}
	mimeType := entry.mimeType
	if mimeType == "" {
		mimeType = detectMimeType(data, entry.filename, entry.path)
	}
	return data, mimeType, nil
}

func (m *mediaHandler) Upload(ctx context.Context, data []byte, mimeType string, filename string) (string, error) {
	if m == nil || m.adapter == nil {
		return "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	if len(data) == 0 {
		return "", channels.ErrInvalidInput("media data required", nil)
	}
	mediaID := uuid.NewString()
	if mimeType == "" {
		mimeType = detectMimeType(data, filename, "")
	}
	if _, err := m.adapter.storeMedia(mediaID, data, mimeType, filename); err != nil {
		return "", channels.ErrConnection("failed to store media", err)
	}
	return mediaID, nil
}

func (m *mediaHandler) GetURL(ctx context.Context, mediaID string) (string, error) {
	if m == nil || m.adapter == nil {
		return "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return "", channels.ErrInvalidInput("media id required", nil)
	}
	entry, ok := m.adapter.getMedia(mediaID)
	if !ok {
		return "", channels.ErrNotFound("media not found", nil)
	}
	if entry.path == "" {
		return "", channels.ErrUnavailable("media URL not available", nil)
	}
	return "file://" + entry.path, nil
}

func detectMimeType(data []byte, filename string, path string) string {
	if filename != "" {
		if mimeType := mimeTypeForName(filename); mimeType != "" {
			return mimeType
		}
	}
	if path != "" {
		if mimeType := mimeTypeForName(path); mimeType != "" {
			return mimeType
		}
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return ""
}

func mimeTypeForName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(lower, ".mp3"), strings.HasSuffix(lower, ".mpeg"):
		return "audio/mpeg"
	case strings.HasSuffix(lower, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(lower, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(lower, ".mov"):
		return "video/quicktime"
	default:
		return ""
	}
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
func downloadURL(ctx context.Context, url string) ([]byte, error) {
	raw := strings.TrimSpace(url)
	if raw == "" {
		return nil, channels.ErrInvalidInput("missing attachment url (set attachment.url)", nil)
	}
	isFileURL := strings.HasPrefix(raw, "file://")
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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

	path := strings.TrimPrefix(raw, "file://")
	if strings.TrimSpace(path) != "" {
		info, err := os.Stat(path)
		if err != nil {
			if isFileURL {
				return nil, channels.ErrInvalidInput("attachment file not found", err)
			}
		} else if info.IsDir() {
			if isFileURL {
				return nil, channels.ErrInvalidInput("attachment path is a directory", nil)
			}
		} else {
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
	} else if isFileURL {
		return nil, channels.ErrInvalidInput("missing attachment path", nil)
	}

	if isFileURL {
		return nil, channels.ErrInvalidInput("attachment file not found", nil)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, channels.ErrConnection("failed to create download request", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, channels.ErrConnection("failed to download attachment", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, channels.ErrConnection(fmt.Sprintf("unexpected status code: %d", resp.StatusCode), nil)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, channels.ErrConnection("failed to read attachment", err)
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
