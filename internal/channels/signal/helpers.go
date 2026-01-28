package signal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	channelcontext "github.com/haasonsaas/nexus/internal/channels/context"
	"github.com/haasonsaas/nexus/internal/channels/personal"
)

// contactManager implements personal.ContactManager for Signal.
type contactManager struct {
	adapter *Adapter
}

func (c *contactManager) Resolve(ctx context.Context, identifier string) (*personal.Contact, error) {
	// First check cache
	if contact, ok := c.adapter.GetContact(identifier); ok {
		return contact, nil
	}

	// Try to get contact info from signal-cli
	req := map[string]any{
		"method": "listContacts",
	}

	result, err := c.adapter.call(ctx, req)
	if err != nil {
		return nil, channels.ErrConnection("failed to list contacts", err)
	}

	var contacts []signalContact
	if err := json.Unmarshal(result, &contacts); err != nil {
		return nil, channels.ErrInternal("failed to parse contacts", err)
	}

	for _, sc := range contacts {
		if sc.Number == identifier || sc.UUID == identifier {
			contact := &personal.Contact{
				ID:    sc.Number,
				Name:  sc.Name,
				Phone: sc.Number,
			}
			c.adapter.SetContact(contact)
			return contact, nil
		}
	}

	return nil, nil
}

func (c *contactManager) Search(ctx context.Context, query string) ([]*personal.Contact, error) {
	if c == nil || c.adapter == nil || c.adapter.stdin == nil || c.adapter.pending == nil {
		return nil, channels.ErrUnavailable("contact search unavailable", nil)
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return []*personal.Contact{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req := map[string]any{
		"method": "listContacts",
	}
	result, err := c.adapter.call(ctx, req)
	if err != nil {
		return nil, channels.ErrConnection("failed to list contacts", err)
	}
	var contacts []signalContact
	if err := json.Unmarshal(result, &contacts); err != nil {
		return nil, channels.ErrInternal("failed to parse contacts", err)
	}
	q = strings.ToLower(q)
	results := make([]*personal.Contact, 0)
	for _, sc := range contacts {
		if !matchesQuery(q, sc.Name, sc.Number, sc.UUID) {
			continue
		}
		id := sc.Number
		if id == "" {
			id = sc.UUID
		}
		contact := &personal.Contact{
			ID:    id,
			Name:  sc.Name,
			Phone: sc.Number,
		}
		if contact.Name == "" {
			contact.Name = id
		}
		c.adapter.SetContact(contact)
		results = append(results, contact)
		if len(results) >= 50 {
			break
		}
	}
	return results, nil
}

func (c *contactManager) Sync(ctx context.Context) error {
	// Request sync from signal-cli
	req := map[string]any{
		"method": "listContacts",
	}

	result, err := c.adapter.call(ctx, req)
	if err != nil {
		return channels.ErrConnection("failed to list contacts", err)
	}

	var contacts []signalContact
	if err := json.Unmarshal(result, &contacts); err != nil {
		return channels.ErrInternal("failed to parse contacts", err)
	}

	for _, sc := range contacts {
		c.adapter.SetContact(&personal.Contact{
			ID:    sc.Number,
			Name:  sc.Name,
			Phone: sc.Number,
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

// presenceManager implements personal.PresenceManager for Signal.
type presenceManager struct {
	adapter *Adapter
}

func (p *presenceManager) SetTyping(ctx context.Context, peerID string, typing bool) error {
	if !p.adapter.config.Personal.Presence.SendTyping {
		return nil
	}

	action := "STARTED"
	if !typing {
		action = "STOPPED"
	}

	req := map[string]any{
		"method": "sendTyping",
		"params": map[string]any{
			"recipient": peerID,
			"action":    action,
		},
	}

	_, err := p.adapter.call(ctx, req)
	return err
}

func (p *presenceManager) SetOnline(ctx context.Context, online bool) error {
	// Signal doesn't have explicit online status
	return nil
}

func (p *presenceManager) Subscribe(ctx context.Context, peerID string) (<-chan personal.PresenceEvent, error) {
	// Signal typing notifications come through the main event stream
	ch := make(chan personal.PresenceEvent, 10)
	return ch, nil
}

func (p *presenceManager) MarkRead(ctx context.Context, peerID string, messageID string) error {
	if !p.adapter.config.Personal.Presence.SendReadReceipts {
		return nil
	}

	req := map[string]any{
		"method": "sendReceipt",
		"params": map[string]any{
			"recipient":       peerID,
			"targetTimestamp": messageID,
			"type":            "read",
		},
	}

	_, err := p.adapter.call(ctx, req)
	return err
}

// signalContact represents a Signal contact from signal-cli.
type signalContact struct {
	Number string `json:"number"`
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
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

	maxBytes := channelcontext.GetChannelInfo("signal").MaxAttachmentBytes
	if maxBytes <= 0 {
		maxBytes = 100 * 1024 * 1024
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
