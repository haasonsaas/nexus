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
	return nil, nil
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
func downloadURL(url string) ([]byte, error) {
	raw := strings.TrimSpace(url)
	if raw == "" {
		return nil, channels.ErrInvalidInput("missing attachment url", nil)
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
