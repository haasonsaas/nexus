package channels

import (
	"context"

	"github.com/haasonsaas/nexus/pkg/models"
)

// MessageAction represents the type of action to perform on a message.
type MessageAction string

const (
	// ActionSend sends a new message.
	ActionSend MessageAction = "send"

	// ActionEdit edits an existing message.
	ActionEdit MessageAction = "edit"

	// ActionDelete deletes a message.
	ActionDelete MessageAction = "delete"

	// ActionReact adds a reaction to a message.
	ActionReact MessageAction = "react"

	// ActionUnreact removes a reaction from a message.
	ActionUnreact MessageAction = "unreact"

	// ActionReply replies to a specific message (threaded).
	ActionReply MessageAction = "reply"

	// ActionPin pins a message.
	ActionPin MessageAction = "pin"

	// ActionUnpin unpins a message.
	ActionUnpin MessageAction = "unpin"

	// ActionTyping sends a typing indicator.
	ActionTyping MessageAction = "typing"
)

// AllMessageActions returns all defined message actions.
func AllMessageActions() []MessageAction {
	return []MessageAction{
		ActionSend,
		ActionEdit,
		ActionDelete,
		ActionReact,
		ActionUnreact,
		ActionReply,
		ActionPin,
		ActionUnpin,
		ActionTyping,
	}
}

// MessageActionRequest represents a request to perform an action on a message.
type MessageActionRequest struct {
	// Action is the type of action to perform.
	Action MessageAction `json:"action"`

	// ChannelID is the target channel/chat ID.
	ChannelID string `json:"channel_id"`

	// MessageID is the ID of the target message (for edit, delete, react, pin, etc.).
	MessageID string `json:"message_id,omitempty"`

	// Content is the message content (for send, edit, reply).
	Content string `json:"content,omitempty"`

	// Reaction is the emoji/reaction to add or remove.
	Reaction string `json:"reaction,omitempty"`

	// ReplyToID is the message ID to reply to (for threaded replies).
	ReplyToID string `json:"reply_to_id,omitempty"`

	// Metadata contains additional channel-specific parameters.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MessageActionResult represents the result of a message action.
type MessageActionResult struct {
	// Success indicates whether the action was successful.
	Success bool `json:"success"`

	// MessageID is the ID of the affected/created message.
	MessageID string `json:"message_id,omitempty"`

	// Error contains the error message if the action failed.
	Error string `json:"error,omitempty"`

	// Metadata contains additional channel-specific response data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Capabilities declares the features supported by a channel adapter.
type Capabilities struct {
	// Send indicates the adapter can send messages.
	Send bool `json:"send"`

	// Edit indicates the adapter can edit sent messages.
	Edit bool `json:"edit"`

	// Delete indicates the adapter can delete messages.
	Delete bool `json:"delete"`

	// React indicates the adapter can add/remove reactions.
	React bool `json:"react"`

	// Reply indicates the adapter supports threaded replies.
	Reply bool `json:"reply"`

	// Pin indicates the adapter can pin/unpin messages.
	Pin bool `json:"pin"`

	// Typing indicates the adapter can send typing indicators.
	Typing bool `json:"typing"`

	// Attachments indicates the adapter supports file attachments.
	Attachments bool `json:"attachments"`

	// RichText indicates the adapter supports formatted text (markdown, etc.).
	RichText bool `json:"rich_text"`

	// Threads indicates the adapter supports message threads.
	Threads bool `json:"threads"`

	// MaxMessageLength is the maximum message length (0 = unlimited).
	MaxMessageLength int `json:"max_message_length,omitempty"`

	// MaxAttachmentSize is the maximum attachment size in bytes (0 = unlimited).
	MaxAttachmentSize int64 `json:"max_attachment_size,omitempty"`
}

// SupportsAction checks if the capabilities include support for the given action.
func (c Capabilities) SupportsAction(action MessageAction) bool {
	switch action {
	case ActionSend:
		return c.Send
	case ActionEdit:
		return c.Edit
	case ActionDelete:
		return c.Delete
	case ActionReact, ActionUnreact:
		return c.React
	case ActionReply:
		return c.Reply
	case ActionPin, ActionUnpin:
		return c.Pin
	case ActionTyping:
		return c.Typing
	default:
		return false
	}
}

// MessageActionsAdapter represents adapters that support message actions beyond basic send.
type MessageActionsAdapter interface {
	// Capabilities returns the features supported by this adapter.
	Capabilities() Capabilities

	// ExecuteAction performs a message action.
	ExecuteAction(ctx context.Context, req *MessageActionRequest) (*MessageActionResult, error)
}

// EditableAdapter is a convenience interface for adapters that support editing messages.
type EditableAdapter interface {
	// EditMessage edits an existing message.
	EditMessage(ctx context.Context, channelID, messageID, newContent string) error
}

// DeletableAdapter is a convenience interface for adapters that support deleting messages.
type DeletableAdapter interface {
	// DeleteMessage deletes a message.
	DeleteMessage(ctx context.Context, channelID, messageID string) error
}

// ReactableAdapter is a convenience interface for adapters that support reactions.
type ReactableAdapter interface {
	// AddReaction adds a reaction to a message.
	AddReaction(ctx context.Context, channelID, messageID, reaction string) error

	// RemoveReaction removes a reaction from a message.
	RemoveReaction(ctx context.Context, channelID, messageID, reaction string) error
}

// ReplyableAdapter is a convenience interface for adapters that support threaded replies.
type ReplyableAdapter interface {
	// SendReply sends a reply to a specific message.
	SendReply(ctx context.Context, channelID, replyToID string, msg *models.Message) error
}

// PinnableAdapter is a convenience interface for adapters that support pinning messages.
type PinnableAdapter interface {
	// PinMessage pins a message.
	PinMessage(ctx context.Context, channelID, messageID string) error

	// UnpinMessage unpins a message.
	UnpinMessage(ctx context.Context, channelID, messageID string) error
}
