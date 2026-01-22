// Package personal provides shared types and interfaces for personal messaging channels.
package personal

import (
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Contact represents a contact in a personal messaging service.
type Contact struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Phone    string         `json:"phone,omitempty"`
	Email    string         `json:"email,omitempty"`
	Avatar   string         `json:"avatar,omitempty"`
	Verified bool           `json:"verified"`
	Extra    map[string]any `json:"extra,omitempty"`
}

// Conversation represents a chat conversation (DM or group).
type Conversation struct {
	ID           string           `json:"id"`
	Type         ConversationType `json:"type"`
	Name         string           `json:"name,omitempty"`
	Participants []*Contact       `json:"participants"`
	LastMessage  *models.Message  `json:"last_message,omitempty"`
	UnreadCount  int              `json:"unread_count"`
	Muted        bool             `json:"muted"`
	Pinned       bool             `json:"pinned"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// ConversationType indicates if a conversation is DM or group.
type ConversationType string

const (
	ConversationDM    ConversationType = "dm"
	ConversationGroup ConversationType = "group"
)

// PresenceEvent represents a presence update (online, typing, etc.).
type PresenceEvent struct {
	PeerID    string       `json:"peer_id"`
	Type      PresenceType `json:"type"`
	Timestamp time.Time    `json:"timestamp"`
}

// PresenceType indicates the type of presence event.
type PresenceType string

const (
	PresenceOnline        PresenceType = "online"
	PresenceOffline       PresenceType = "offline"
	PresenceTyping        PresenceType = "typing"
	PresenceStoppedTyping PresenceType = "stopped_typing"
)

// RawMessage is the intermediate format for protocol-specific messages.
type RawMessage struct {
	ID          string
	Content     string
	PeerID      string
	PeerName    string
	GroupID     string
	GroupName   string
	Timestamp   time.Time
	Attachments []RawAttachment
	ReplyTo     string
	Extra       map[string]any
}

// RawAttachment represents a raw attachment before processing.
type RawAttachment struct {
	ID       string
	MIMEType string
	Filename string
	Size     int64
	URL      string
	Data     []byte // Optional: inline data
}

// ListOptions provides options for listing conversations.
type ListOptions struct {
	Limit   int
	Offset  int
	After   time.Time
	Before  time.Time
	Unread  bool
	GroupID string
}

// Config holds common configuration for personal channels.
type Config struct {
	SessionPath string `yaml:"session_path"`
	MediaPath   string `yaml:"media_path"`
	SyncOnStart bool   `yaml:"sync_on_start"`

	Presence PresenceConfig `yaml:"presence"`
}

// PresenceConfig holds presence-related settings.
type PresenceConfig struct {
	SendReadReceipts bool `yaml:"send_read_receipts"`
	SendTyping       bool `yaml:"send_typing"`
	BroadcastOnline  bool `yaml:"broadcast_online"`
}
