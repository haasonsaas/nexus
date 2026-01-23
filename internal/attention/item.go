// Package attention provides a unified attention layer that aggregates items
// from multiple channels (email, Teams, ServiceNow, etc.) into a single feed.
package attention

import (
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ItemType categorizes the kind of attention item.
type ItemType string

const (
	ItemTypeMessage     ItemType = "message"
	ItemTypeEmail       ItemType = "email"
	ItemTypeTicket      ItemType = "ticket"
	ItemTypeMention     ItemType = "mention"
	ItemTypeTask        ItemType = "task"
	ItemTypeReminder    ItemType = "reminder"
	ItemTypeNotification ItemType = "notification"
)

// Priority levels for attention items.
type Priority int

const (
	PriorityLow      Priority = 1
	PriorityNormal   Priority = 2
	PriorityHigh     Priority = 3
	PriorityUrgent   Priority = 4
	PriorityCritical Priority = 5
)

// Status represents the state of an attention item.
type Status string

const (
	StatusNew       Status = "new"
	StatusViewed    Status = "viewed"
	StatusInProgress Status = "in_progress"
	StatusSnoozed   Status = "snoozed"
	StatusHandled   Status = "handled"
	StatusArchived  Status = "archived"
)

// Item represents a single attention-requiring event from any channel.
type Item struct {
	// ID is a unique identifier for this attention item
	ID string `json:"id"`

	// Type categorizes the item (message, email, ticket, etc.)
	Type ItemType `json:"type"`

	// Channel is the source channel (teams, email, slack, servicenow, etc.)
	Channel models.ChannelType `json:"channel"`

	// ChannelID is the specific channel/conversation ID
	ChannelID string `json:"channel_id"`

	// ExternalID is the ID from the source system
	ExternalID string `json:"external_id"`

	// Title is a brief summary of the item
	Title string `json:"title"`

	// Preview is a short preview of the content
	Preview string `json:"preview"`

	// Content is the full content (may be truncated for large items)
	Content string `json:"content,omitempty"`

	// Sender information
	Sender Sender `json:"sender"`

	// Priority indicates urgency level
	Priority Priority `json:"priority"`

	// Status tracks the attention state
	Status Status `json:"status"`

	// Timestamps
	ReceivedAt   time.Time  `json:"received_at"`
	ViewedAt     *time.Time `json:"viewed_at,omitempty"`
	SnoozedUntil *time.Time `json:"snoozed_until,omitempty"`
	HandledAt    *time.Time `json:"handled_at,omitempty"`

	// Tags for categorization and filtering
	Tags []string `json:"tags,omitempty"`

	// Metadata holds channel-specific data
	Metadata map[string]any `json:"metadata,omitempty"`

	// RelatedItems links to related attention items (e.g., thread members)
	RelatedItems []string `json:"related_items,omitempty"`

	// OriginalMessage holds the source message if applicable
	OriginalMessage *models.Message `json:"original_message,omitempty"`
}

// Sender represents who originated an attention item.
type Sender struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Channel     models.ChannelType `json:"channel"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// IsActive returns true if the item still requires attention.
func (i *Item) IsActive() bool {
	switch i.Status {
	case StatusNew, StatusViewed, StatusInProgress:
		return true
	case StatusSnoozed:
		if i.SnoozedUntil != nil && time.Now().After(*i.SnoozedUntil) {
			return true
		}
		return false
	default:
		return false
	}
}

// SetViewed marks the item as viewed.
func (i *Item) SetViewed() {
	now := time.Now()
	i.ViewedAt = &now
	if i.Status == StatusNew {
		i.Status = StatusViewed
	}
}

// SetHandled marks the item as handled.
func (i *Item) SetHandled() {
	now := time.Now()
	i.HandledAt = &now
	i.Status = StatusHandled
}

// Snooze postpones the item until the given time.
func (i *Item) Snooze(until time.Time) {
	i.SnoozedUntil = &until
	i.Status = StatusSnoozed
}

// Unsnooze brings a snoozed item back to active status.
func (i *Item) Unsnooze() {
	i.SnoozedUntil = nil
	if i.ViewedAt != nil {
		i.Status = StatusViewed
	} else {
		i.Status = StatusNew
	}
}

// ItemFromMessage converts a Nexus message to an attention item.
func ItemFromMessage(msg *models.Message) *Item {
	itemType := ItemTypeMessage
	switch msg.Channel {
	case models.ChannelEmail:
		itemType = ItemTypeEmail
	}

	title := truncate(msg.Content, 80)
	preview := truncate(msg.Content, 200)

	// Extract sender info from metadata
	sender := Sender{
		Channel: msg.Channel,
	}
	if msg.Metadata != nil {
		if name, ok := msg.Metadata["sender_name"].(string); ok {
			sender.Name = name
		}
		if email, ok := msg.Metadata["sender_email"].(string); ok {
			sender.Email = email
		}
		if id, ok := msg.Metadata["sender_id"].(string); ok {
			sender.ID = id
		}
	}

	// Check for subject in email metadata
	if msg.Metadata != nil {
		if subject, ok := msg.Metadata["subject"].(string); ok && subject != "" {
			title = subject
		}
	}

	return &Item{
		ID:              msg.ID,
		Type:            itemType,
		Channel:         msg.Channel,
		ChannelID:       msg.ChannelID,
		ExternalID:      msg.ID,
		Title:           title,
		Preview:         preview,
		Content:         msg.Content,
		Sender:          sender,
		Priority:        PriorityNormal,
		Status:          StatusNew,
		ReceivedAt:      msg.CreatedAt,
		Metadata:        msg.Metadata,
		OriginalMessage: msg,
	}
}

// truncate shortens a string to max length, adding ellipsis if needed.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
