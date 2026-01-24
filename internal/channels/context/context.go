// Package context provides delivery context and formatting utilities for channel adapters.
// It unifies channel information, capabilities, and message formatting in one place.
package context

import (
	"regexp"
	"strings"
)

// ChannelInfo describes a channel's capabilities and constraints.
type ChannelInfo struct {
	// Name is the channel type identifier (telegram, discord, etc.).
	Name string

	// MaxMessageLength is the maximum message length in characters.
	MaxMessageLength int

	// SupportsMarkdown indicates if the channel supports markdown formatting.
	SupportsMarkdown bool

	// SupportsHTML indicates if the channel supports HTML formatting.
	SupportsHTML bool

	// MarkdownFlavor indicates which markdown flavor is supported.
	// Values: "standard", "slack", "telegram", "discord", "none"
	MarkdownFlavor string

	// SupportsMentions indicates if the channel supports @mentions.
	SupportsMentions bool

	// MentionFormat is the format string for mentions (e.g., "<@%s>" for Slack).
	MentionFormat string

	// SupportsThreads indicates if the channel supports threaded replies.
	SupportsThreads bool

	// SupportsReactions indicates if the channel supports emoji reactions.
	SupportsReactions bool

	// SupportsEditing indicates if sent messages can be edited.
	SupportsEditing bool

	// SupportsAttachments indicates if the channel supports file attachments.
	SupportsAttachments bool

	// MaxAttachmentBytes is the maximum attachment size in bytes.
	MaxAttachmentBytes int64
}

// Channels contains predefined channel information.
var Channels = map[string]ChannelInfo{
	"telegram": {
		Name:                "telegram",
		MaxMessageLength:    4096,
		SupportsMarkdown:    true,
		MarkdownFlavor:      "telegram",
		SupportsMentions:    true,
		MentionFormat:       "@%s",
		SupportsThreads:     true,
		SupportsReactions:   true,
		SupportsEditing:     true,
		SupportsAttachments: true,
		MaxAttachmentBytes:  50 * 1024 * 1024,
	},
	"discord": {
		Name:                "discord",
		MaxMessageLength:    2000,
		SupportsMarkdown:    true,
		MarkdownFlavor:      "discord",
		SupportsMentions:    true,
		MentionFormat:       "<@%s>",
		SupportsThreads:     true,
		SupportsReactions:   true,
		SupportsEditing:     true,
		SupportsAttachments: true,
		MaxAttachmentBytes:  8 * 1024 * 1024,
	},
	"slack": {
		Name:                "slack",
		MaxMessageLength:    40000,
		SupportsMarkdown:    true,
		MarkdownFlavor:      "slack",
		SupportsMentions:    true,
		MentionFormat:       "<@%s>",
		SupportsThreads:     true,
		SupportsReactions:   true,
		SupportsEditing:     true,
		SupportsAttachments: true,
		MaxAttachmentBytes:  1024 * 1024 * 1024,
	},
	"whatsapp": {
		Name:                "whatsapp",
		MaxMessageLength:    65536,
		SupportsMarkdown:    true,
		MarkdownFlavor:      "whatsapp",
		SupportsMentions:    true,
		MentionFormat:       "@%s",
		SupportsThreads:     true,
		SupportsReactions:   true,
		SupportsEditing:     false,
		SupportsAttachments: true,
		MaxAttachmentBytes:  16 * 1024 * 1024,
	},
	"signal": {
		Name:                "signal",
		MaxMessageLength:    65536,
		SupportsMarkdown:    false,
		MarkdownFlavor:      "none",
		SupportsMentions:    false,
		SupportsThreads:     false,
		SupportsReactions:   true,
		SupportsEditing:     false,
		SupportsAttachments: true,
		MaxAttachmentBytes:  100 * 1024 * 1024,
	},
	"imessage": {
		Name:                "imessage",
		MaxMessageLength:    20000,
		SupportsMarkdown:    false,
		MarkdownFlavor:      "none",
		SupportsMentions:    false,
		SupportsThreads:     false,
		SupportsReactions:   true,
		SupportsEditing:     false,
		SupportsAttachments: true,
		MaxAttachmentBytes:  100 * 1024 * 1024,
	},
	"matrix": {
		Name:                "matrix",
		MaxMessageLength:    65536,
		SupportsMarkdown:    true,
		SupportsHTML:        true,
		MarkdownFlavor:      "standard",
		SupportsMentions:    true,
		MentionFormat:       "@%s",
		SupportsThreads:     true,
		SupportsReactions:   true,
		SupportsEditing:     true,
		SupportsAttachments: true,
		MaxAttachmentBytes:  100 * 1024 * 1024,
	},
	"sms": {
		Name:             "sms",
		MaxMessageLength: 160,
		SupportsMarkdown: false,
		MarkdownFlavor:   "none",
		SupportsMentions: false,
	},
	"email": {
		Name:                "email",
		MaxMessageLength:    0, // Unlimited
		SupportsMarkdown:    true,
		SupportsHTML:        true,
		MarkdownFlavor:      "standard",
		SupportsMentions:    false,
		SupportsAttachments: true,
		MaxAttachmentBytes:  25 * 1024 * 1024,
	},
	"teams": {
		Name:                "teams",
		MaxMessageLength:    28000,
		SupportsMarkdown:    true,
		SupportsHTML:        true,
		MarkdownFlavor:      "standard",
		SupportsMentions:    true,
		MentionFormat:       "<at>%s</at>",
		SupportsThreads:     true,
		SupportsReactions:   true,
		SupportsEditing:     true,
		SupportsAttachments: true,
		MaxAttachmentBytes:  250 * 1024 * 1024,
	},
}

// GetChannelInfo returns channel information for the given channel type.
func GetChannelInfo(channel string) ChannelInfo {
	if info, ok := Channels[strings.ToLower(channel)]; ok {
		return info
	}
	// Return a default for unknown channels
	return ChannelInfo{
		Name:             channel,
		MaxMessageLength: 4000,
		SupportsMarkdown: false,
		MarkdownFlavor:   "none",
	}
}

// DeliveryContext provides context for message delivery.
type DeliveryContext struct {
	// Channel is the channel information.
	Channel ChannelInfo

	// UserID is the target user identifier.
	UserID string

	// ConversationID is the conversation/chat/channel identifier.
	ConversationID string

	// ThreadID is the thread identifier for threaded replies.
	ThreadID string

	// ReplyToMessageID is the message ID to reply to.
	ReplyToMessageID string
}

// New creates a new delivery context for the given channel.
func New(channel string) *DeliveryContext {
	return &DeliveryContext{
		Channel: GetChannelInfo(channel),
	}
}

// WithUser sets the target user ID.
func (dc *DeliveryContext) WithUser(userID string) *DeliveryContext {
	dc.UserID = userID
	return dc
}

// WithConversation sets the conversation ID.
func (dc *DeliveryContext) WithConversation(convID string) *DeliveryContext {
	dc.ConversationID = convID
	return dc
}

// WithThread sets the thread ID.
func (dc *DeliveryContext) WithThread(threadID string) *DeliveryContext {
	dc.ThreadID = threadID
	return dc
}

// WithReplyTo sets the message ID to reply to.
func (dc *DeliveryContext) WithReplyTo(msgID string) *DeliveryContext {
	dc.ReplyToMessageID = msgID
	return dc
}

// FormatMention formats a user mention for the channel.
func (dc *DeliveryContext) FormatMention(userID string) string {
	if !dc.Channel.SupportsMentions || dc.Channel.MentionFormat == "" {
		return userID
	}
	return strings.Replace(dc.Channel.MentionFormat, "%s", userID, 1)
}

// FormatText formats text for the channel, converting markdown if needed.
func (dc *DeliveryContext) FormatText(text string) string {
	switch dc.Channel.MarkdownFlavor {
	case "none":
		return StripMarkdown(text)
	case "slack":
		return ToSlackMarkdown(text)
	case "telegram":
		return ToTelegramMarkdown(text)
	default:
		return text
	}
}

// StripMarkdown removes markdown formatting from text.
func StripMarkdown(text string) string {
	// Remove code blocks
	codeBlockRegex := regexp.MustCompile("```[\\s\\S]*?```")
	text = codeBlockRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Extract code content without fences
		content := strings.TrimPrefix(match, "```")
		content = strings.TrimSuffix(content, "```")
		// Remove language identifier on first line
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
		return content
	})

	// Remove inline code
	inlineCodeRegex := regexp.MustCompile("`([^`]+)`")
	text = inlineCodeRegex.ReplaceAllString(text, "$1")

	// Remove bold
	boldRegex := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRegex.ReplaceAllString(text, "$1")

	// Remove italic
	italicRegex := regexp.MustCompile(`\*([^*]+)\*`)
	text = italicRegex.ReplaceAllString(text, "$1")
	italicUnderscoreRegex := regexp.MustCompile(`_([^_]+)_`)
	text = italicUnderscoreRegex.ReplaceAllString(text, "$1")

	// Remove strikethrough
	strikeRegex := regexp.MustCompile(`~~([^~]+)~~`)
	text = strikeRegex.ReplaceAllString(text, "$1")

	// Convert links to plain text
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	text = linkRegex.ReplaceAllString(text, "$1")

	// Remove headers
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s*`)
	text = headerRegex.ReplaceAllString(text, "")

	// Convert bullet points to dashes
	bulletRegex := regexp.MustCompile(`(?m)^[*-]\s+`)
	text = bulletRegex.ReplaceAllString(text, "- ")

	return text
}

// ToSlackMarkdown converts standard markdown to Slack's mrkdwn format.
func ToSlackMarkdown(text string) string {
	// Slack uses single * for bold, _ for italic
	// Convert **bold** to *bold*
	boldRegex := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRegex.ReplaceAllString(text, "*$1*")

	// Keep _italic_ as is (Slack uses _ for italic too)

	// Convert [text](url) to <url|text>
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRegex.ReplaceAllString(text, "<$2|$1>")

	// Convert ~~strikethrough~~ to ~strikethrough~
	strikeRegex := regexp.MustCompile(`~~([^~]+)~~`)
	text = strikeRegex.ReplaceAllString(text, "~$1~")

	return text
}

// ToTelegramMarkdown converts standard markdown to Telegram's MarkdownV2 format.
func ToTelegramMarkdown(text string) string {
	// Telegram MarkdownV2 requires escaping special characters
	// outside of code blocks and special syntax

	// Escape special characters that need escaping
	escapeChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	for _, char := range escapeChars {
		// Don't escape if it's part of markdown syntax
		// This is a simplified approach - full implementation would need state tracking
		text = strings.ReplaceAll(text, char, "\\"+char)
	}

	// Convert escaped markdown back to proper format
	// **bold** -> *bold*
	text = strings.ReplaceAll(text, "\\*\\*", "*")

	return text
}

// ShouldChunk returns true if the text exceeds the channel's message limit.
func (dc *DeliveryContext) ShouldChunk(text string) bool {
	if dc.Channel.MaxMessageLength <= 0 {
		return false
	}
	return len(text) > dc.Channel.MaxMessageLength
}
