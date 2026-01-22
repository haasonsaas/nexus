// Package imessage provides an iMessage channel adapter for macOS.
//go:build darwin
// +build darwin

package imessage

import (
	"context"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
)

// contactManager implements personal.ContactManager for iMessage.
type contactManager struct {
	adapter *Adapter
}

func (c *contactManager) Resolve(ctx context.Context, identifier string) (*personal.Contact, error) {
	// First check cache
	if contact, ok := c.adapter.GetContact(identifier); ok {
		return contact, nil
	}

	// Query handle from database
	query := `
		SELECT h.ROWID, h.id, h.service
		FROM handle h
		WHERE h.id = ?
	`

	var rowID int64
	var handleID, service string

	err := c.adapter.db.QueryRowContext(ctx, query, identifier).Scan(&rowID, &handleID, &service)
	if err != nil {
		return nil, nil
	}

	contact := &personal.Contact{
		ID:   handleID,
		Name: handleID,
	}

	// Parse phone number if applicable
	if service == "iMessage" {
		contact.Phone = handleID
	} else if service == "SMS" {
		contact.Phone = handleID
	}

	c.adapter.SetContact(contact)
	return contact, nil
}

func (c *contactManager) Search(ctx context.Context, query string) ([]*personal.Contact, error) {
	// Search handles
	sqlQuery := `
		SELECT h.id, h.service
		FROM handle h
		WHERE h.id LIKE ?
		LIMIT 50
	`

	rows, err := c.adapter.db.QueryContext(ctx, sqlQuery, "%"+query+"%")
	if err != nil {
		return nil, channels.ErrInternal("failed to search contacts", err)
	}
	defer rows.Close()

	var contacts []*personal.Contact
	for rows.Next() {
		var handleID, service string
		if err := rows.Scan(&handleID, &service); err != nil {
			continue
		}

		contacts = append(contacts, &personal.Contact{
			ID:   handleID,
			Name: handleID,
		})
	}

	return contacts, nil
}

func (c *contactManager) Sync(ctx context.Context) error {
	// Load all handles into cache
	query := `
		SELECT h.id, h.service
		FROM handle h
	`

	rows, err := c.adapter.db.QueryContext(ctx, query)
	if err != nil {
		return channels.ErrInternal("failed to list handles", err)
	}
	defer rows.Close()

	for rows.Next() {
		var handleID, service string
		if err := rows.Scan(&handleID, &service); err != nil {
			continue
		}

		c.adapter.SetContact(&personal.Contact{
			ID:   handleID,
			Name: handleID,
		})
	}

	return nil
}

func (c *contactManager) GetByID(ctx context.Context, id string) (*personal.Contact, error) {
	return c.Resolve(ctx, id)
}
