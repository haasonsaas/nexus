package attention

import (
	"context"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ChannelSource wraps a channel adapter as an ItemSource.
type ChannelSource struct {
	adapter channels.InboundAdapter
	items   chan *Item
	cancel  context.CancelFunc
}

// NewChannelSource creates a source that converts channel messages to attention items.
func NewChannelSource(adapter channels.InboundAdapter) *ChannelSource {
	return &ChannelSource{
		adapter: adapter,
		items:   make(chan *Item, 100),
	}
}

// Items returns the channel of attention items.
func (s *ChannelSource) Items() <-chan *Item {
	return s.items
}

// Start begins converting messages to attention items.
func (s *ChannelSource) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.processMessages(ctx)
}

// Stop stops the source.
func (s *ChannelSource) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// processMessages reads from the adapter and converts to attention items.
func (s *ChannelSource) processMessages(ctx context.Context) {
	msgs := s.adapter.Messages()

	for {
		select {
		case <-ctx.Done():
			close(s.items)
			return
		case msg, ok := <-msgs:
			if !ok {
				close(s.items)
				return
			}
			item := ItemFromMessage(msg)
			select {
			case s.items <- item:
			default:
				// Channel full, drop item
			}
		}
	}
}

// MessageChannelSource wraps a message channel directly as an ItemSource.
type MessageChannelSource struct {
	messages <-chan *models.Message
	items    chan *Item
	cancel   context.CancelFunc
}

// NewMessageChannelSource creates a source from a message channel.
func NewMessageChannelSource(messages <-chan *models.Message) *MessageChannelSource {
	return &MessageChannelSource{
		messages: messages,
		items:    make(chan *Item, 100),
	}
}

// Items returns the channel of attention items.
func (s *MessageChannelSource) Items() <-chan *Item {
	return s.items
}

// Start begins converting messages to attention items.
func (s *MessageChannelSource) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.processMessages(ctx)
}

// Stop stops the source.
func (s *MessageChannelSource) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// processMessages reads from the message channel and converts to attention items.
func (s *MessageChannelSource) processMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			close(s.items)
			return
		case msg, ok := <-s.messages:
			if !ok {
				close(s.items)
				return
			}
			item := ItemFromMessage(msg)
			select {
			case s.items <- item:
			default:
				// Channel full, drop item
			}
		}
	}
}

// TicketSource creates attention items from ServiceNow tickets.
type TicketSource struct {
	items chan *Item
}

// NewTicketSource creates a source for ticket-based attention items.
func NewTicketSource() *TicketSource {
	return &TicketSource{
		items: make(chan *Item, 100),
	}
}

// Items returns the channel of attention items.
func (s *TicketSource) Items() <-chan *Item {
	return s.items
}

// AddTicket adds a ticket as an attention item.
func (s *TicketSource) AddTicket(ticket TicketInfo) {
	item := &Item{
		ID:         ticket.ID,
		Type:       ItemTypeTicket,
		Channel:    models.ChannelType("servicenow"),
		ChannelID:  "servicenow:" + ticket.Number,
		ExternalID: ticket.ID,
		Title:      ticket.Number + ": " + ticket.ShortDescription,
		Preview:    truncate(ticket.Description, 200),
		Content:    ticket.Description,
		Sender: Sender{
			ID:   ticket.CallerID,
			Name: ticket.CallerName,
		},
		Priority:   mapTicketPriority(ticket.Priority),
		Status:     mapTicketStatus(ticket.State),
		ReceivedAt: ticket.OpenedAt,
		Metadata: map[string]any{
			"ticket_number":    ticket.Number,
			"ticket_state":     ticket.State,
			"ticket_priority":  ticket.Priority,
			"assigned_to":      ticket.AssignedTo,
			"assignment_group": ticket.AssignmentGroup,
			"category":         ticket.Category,
		},
	}

	select {
	case s.items <- item:
	default:
		// Channel full
	}
}

// TicketInfo holds information about a ServiceNow ticket.
type TicketInfo struct {
	ID               string
	Number           string
	ShortDescription string
	Description      string
	State            string
	Priority         string
	CallerID         string
	CallerName       string
	AssignedTo       string
	AssignmentGroup  string
	Category         string
	OpenedAt         time.Time
}

// mapTicketPriority converts ServiceNow priority to attention priority.
func mapTicketPriority(p string) Priority {
	switch p {
	case "1", "Critical":
		return PriorityCritical
	case "2", "High":
		return PriorityHigh
	case "3", "Moderate":
		return PriorityNormal
	case "4", "Low", "5", "Planning":
		return PriorityLow
	default:
		return PriorityNormal
	}
}

// mapTicketStatus converts ServiceNow state to attention status.
func mapTicketStatus(s string) Status {
	switch s {
	case "1", "New":
		return StatusNew
	case "2", "In Progress":
		return StatusInProgress
	case "3", "On Hold":
		return StatusSnoozed
	case "6", "Resolved", "7", "Closed":
		return StatusHandled
	default:
		return StatusNew
	}
}
