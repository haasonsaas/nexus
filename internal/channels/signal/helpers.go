package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
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
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, channels.ErrConnection(fmt.Sprintf("unexpected status code: %d", resp.StatusCode), nil)
	}

	return io.ReadAll(resp.Body)
}
