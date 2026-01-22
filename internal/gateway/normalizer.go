// Package gateway provides the main Nexus gateway server.
//
// normalizer.go provides message normalization at the gateway level.
// This ensures all incoming messages have consistent metadata structure
// regardless of which channel adapter they originate from.
package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Canonical metadata keys used across all channels.
// Adapters may use channel-specific keys, but these are the normalized versions
// that gateway processing code should rely on.
const (
	// MetaUserID is the normalized user identifier across channels.
	MetaUserID = "user_id"

	// MetaUserName is the user's display name.
	MetaUserName = "user_name"

	// MetaChatID is the chat/channel/room identifier.
	MetaChatID = "chat_id"

	// MetaThreadID is the thread/topic identifier for threaded conversations.
	MetaThreadID = "thread_id"

	// MetaReplyTo is the message ID being replied to.
	MetaReplyTo = "reply_to"

	// MetaGroupID is the group chat identifier (for group contexts).
	MetaGroupID = "group_id"

	// MetaGroupName is the group chat display name.
	MetaGroupName = "group_name"

	// MetaPeerID is the peer identifier for personal messaging channels.
	MetaPeerID = "peer_id"

	// MetaPeerName is the peer display name.
	MetaPeerName = "peer_name"

	// MetaIsGroup indicates if the message is from a group chat.
	MetaIsGroup = "is_group"

	// MetaMediaText contains transcribed text from audio/video.
	MetaMediaText = "media_text"

	// MetaMediaErrors contains any errors from media processing.
	MetaMediaErrors = "media_errors"

	// MetaNormalized indicates the message has been normalized.
	MetaNormalized = "_normalized"

	// MetaNormalizedAt is the timestamp when normalization occurred.
	MetaNormalizedAt = "_normalized_at"

	// MetaOriginalChannel preserves channel-specific metadata prefix.
	MetaOriginalPrefix = "_original_"
)

// MessageNormalizer normalizes incoming messages to a consistent format.
type MessageNormalizer struct {
	// preserveOriginal keeps original channel-specific keys with a prefix.
	preserveOriginal bool
}

// NormalizerOption configures the MessageNormalizer.
type NormalizerOption func(*MessageNormalizer)

// WithPreserveOriginal keeps original channel-specific metadata keys.
func WithPreserveOriginal(preserve bool) NormalizerOption {
	return func(n *MessageNormalizer) {
		n.preserveOriginal = preserve
	}
}

// NewMessageNormalizer creates a new message normalizer.
func NewMessageNormalizer(opts ...NormalizerOption) *MessageNormalizer {
	n := &MessageNormalizer{
		preserveOriginal: true, // Default to preserving original keys
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Normalize ensures the message has consistent metadata and required fields.
// It is idempotent - calling it multiple times has no additional effect.
func (n *MessageNormalizer) Normalize(msg *models.Message) {
	if msg == nil {
		return
	}

	// Initialize metadata if nil
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}

	// Skip if already normalized
	if _, ok := msg.Metadata[MetaNormalized]; ok {
		return
	}

	// Set defaults
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if msg.Direction == "" {
		msg.Direction = models.DirectionInbound
	}
	if msg.Role == "" {
		msg.Role = models.RoleUser
	}

	// Normalize channel-specific metadata
	n.normalizeChannelMetadata(msg)

	// Normalize attachments
	n.normalizeAttachments(msg)

	// Mark as normalized
	msg.Metadata[MetaNormalized] = true
	msg.Metadata[MetaNormalizedAt] = time.Now().Format(time.RFC3339)
}

// normalizeChannelMetadata maps channel-specific keys to canonical keys.
func (n *MessageNormalizer) normalizeChannelMetadata(msg *models.Message) {
	switch msg.Channel {
	case models.ChannelTelegram:
		n.normalizeTelegram(msg)
	case models.ChannelSlack:
		n.normalizeSlack(msg)
	case models.ChannelDiscord:
		n.normalizeDiscord(msg)
	case models.ChannelWhatsApp, models.ChannelSignal, models.ChannelIMessage:
		n.normalizePersonal(msg)
	case models.ChannelMatrix:
		n.normalizeMatrix(msg)
	case models.ChannelAPI:
		// API messages are already normalized by the caller
	}
}

func (n *MessageNormalizer) normalizeTelegram(msg *models.Message) {
	meta := msg.Metadata

	// Preserve and map chat_id
	if chatID := meta["chat_id"]; chatID != nil {
		n.preserve(meta, "chat_id")
		meta[MetaChatID] = fmt.Sprintf("%v", chatID)
	}

	// Map user info
	if userID := meta["user_id"]; userID != nil {
		n.preserve(meta, "user_id")
		meta[MetaUserID] = fmt.Sprintf("%v", userID)
	}

	// Build user name from first/last
	var nameParts []string
	if first, ok := meta["user_first"].(string); ok && first != "" {
		nameParts = append(nameParts, first)
	}
	if last, ok := meta["user_last"].(string); ok && last != "" {
		nameParts = append(nameParts, last)
	}
	if len(nameParts) > 0 {
		meta[MetaUserName] = strings.Join(nameParts, " ")
	}

	// Map peer_id for personal messaging compatibility
	if meta[MetaPeerID] == nil && meta[MetaChatID] != nil {
		meta[MetaPeerID] = meta[MetaChatID]
	}
}

func (n *MessageNormalizer) normalizeSlack(msg *models.Message) {
	meta := msg.Metadata

	// Map user ID
	if userID, ok := meta["slack_user_id"].(string); ok && userID != "" {
		n.preserve(meta, "slack_user_id")
		meta[MetaUserID] = userID
	} else if userID, ok := meta["slack_user"].(string); ok && userID != "" {
		n.preserve(meta, "slack_user")
		meta[MetaUserID] = userID
	}

	// Map channel
	if channel, ok := meta["slack_channel"].(string); ok && channel != "" {
		n.preserve(meta, "slack_channel")
		meta[MetaChatID] = channel
	}

	// Map thread
	if threadTS, ok := meta["slack_thread_ts"].(string); ok && threadTS != "" {
		n.preserve(meta, "slack_thread_ts")
		meta[MetaThreadID] = threadTS
	}
}

func (n *MessageNormalizer) normalizeDiscord(msg *models.Message) {
	meta := msg.Metadata

	// Map user ID
	if userID, ok := meta["discord_user_id"].(string); ok && userID != "" {
		n.preserve(meta, "discord_user_id")
		meta[MetaUserID] = userID
	}

	// Map user name
	if userName, ok := meta["discord_username"].(string); ok && userName != "" {
		n.preserve(meta, "discord_username")
		meta[MetaUserName] = userName
	}

	// Map channel
	if channelID, ok := meta["discord_channel_id"].(string); ok && channelID != "" {
		n.preserve(meta, "discord_channel_id")
		meta[MetaChatID] = channelID
	}

	// Map thread
	if threadID, ok := meta["discord_thread_id"].(string); ok && threadID != "" {
		n.preserve(meta, "discord_thread_id")
		meta[MetaThreadID] = threadID
	}
}

func (n *MessageNormalizer) normalizePersonal(msg *models.Message) {
	meta := msg.Metadata

	// Personal messaging channels already use peer_id/peer_name
	// Just ensure they're present

	// Check for group context
	if groupID, ok := meta["group_id"].(string); ok && groupID != "" {
		meta[MetaIsGroup] = true
		meta[MetaGroupID] = groupID
		if groupName, ok := meta["group_name"].(string); ok {
			meta[MetaGroupName] = groupName
		}
	} else {
		meta[MetaIsGroup] = false
	}

	// Map peer_id to user_id for consistency
	if peerID, ok := meta["peer_id"].(string); ok && peerID != "" {
		meta[MetaUserID] = peerID
	}
	if peerName, ok := meta["peer_name"].(string); ok && peerName != "" {
		meta[MetaUserName] = peerName
	}
}

func (n *MessageNormalizer) normalizeMatrix(msg *models.Message) {
	meta := msg.Metadata

	// Map sender to user_id
	if sender, ok := meta["sender"].(string); ok && sender != "" {
		n.preserve(meta, "sender")
		meta[MetaUserID] = sender
		meta[MetaPeerID] = sender
	}

	// Map room_id to chat_id
	if roomID, ok := meta["room_id"].(string); ok && roomID != "" {
		n.preserve(meta, "room_id")
		meta[MetaChatID] = roomID
	}
}

// preserve copies the original value to a prefixed key if configured.
func (n *MessageNormalizer) preserve(meta map[string]any, key string) {
	if !n.preserveOriginal {
		return
	}
	if val, ok := meta[key]; ok {
		meta[MetaOriginalPrefix+key] = val
	}
}

// normalizeAttachments ensures all attachments have consistent type detection.
func (n *MessageNormalizer) normalizeAttachments(msg *models.Message) {
	for i := range msg.Attachments {
		att := &msg.Attachments[i]

		// Detect type from MIME type if not set
		if att.Type == "" && att.MimeType != "" {
			att.Type = detectAttachmentType(att.MimeType)
		}

		// Fall back to filename extension
		if att.Type == "" && att.Filename != "" {
			att.Type = detectTypeFromFilename(att.Filename)
		}

		// Default to document
		if att.Type == "" {
			att.Type = "document"
		}
	}
}

// detectAttachmentType determines attachment type from MIME type.
func detectAttachmentType(mimeType string) string {
	mimeType = strings.ToLower(mimeType)

	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "text/"):
		return "text"
	case mimeType == "application/pdf":
		return "document"
	case strings.Contains(mimeType, "spreadsheet") || strings.Contains(mimeType, "excel"):
		return "spreadsheet"
	case strings.Contains(mimeType, "document") || strings.Contains(mimeType, "word"):
		return "document"
	case strings.Contains(mimeType, "presentation") || strings.Contains(mimeType, "powerpoint"):
		return "presentation"
	default:
		return "document"
	}
}

// detectTypeFromFilename determines attachment type from filename extension.
func detectTypeFromFilename(filename string) string {
	ext := strings.ToLower(filename)
	if idx := strings.LastIndex(ext, "."); idx >= 0 {
		ext = ext[idx+1:]
	}

	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "heic":
		return "image"
	case "mp3", "wav", "ogg", "m4a", "aac", "flac", "opus":
		return "audio"
	case "mp4", "mov", "avi", "mkv", "webm":
		return "video"
	case "txt", "md", "json", "xml", "csv", "log":
		return "text"
	case "pdf", "doc", "docx", "rtf":
		return "document"
	case "xls", "xlsx":
		return "spreadsheet"
	case "ppt", "pptx":
		return "presentation"
	default:
		return "document"
	}
}

// DeriveSessionID creates a deterministic session ID from channel and metadata.
// This provides consistent session derivation across all channels.
func DeriveSessionID(channel models.ChannelType, chatID, threadID string) string {
	var parts []string
	parts = append(parts, string(channel))
	parts = append(parts, chatID)
	if threadID != "" {
		parts = append(parts, threadID)
	}

	key := strings.Join(parts, ":")
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:16]) // 32 hex chars
}

// ExtractSessionKey extracts the session key components from normalized metadata.
func ExtractSessionKey(msg *models.Message) (chatID, threadID string) {
	if msg.Metadata == nil {
		return "", ""
	}

	if id, ok := msg.Metadata[MetaChatID].(string); ok {
		chatID = id
	}
	if id, ok := msg.Metadata[MetaThreadID].(string); ok {
		threadID = id
	}

	return chatID, threadID
}

// ExtractUserInfo extracts user identification from normalized metadata.
func ExtractUserInfo(msg *models.Message) (userID, userName string) {
	if msg.Metadata == nil {
		return "", ""
	}

	if id, ok := msg.Metadata[MetaUserID].(string); ok {
		userID = id
	}
	if name, ok := msg.Metadata[MetaUserName].(string); ok {
		userName = name
	}

	return userID, userName
}

// IsGroupMessage checks if the message is from a group context.
func IsGroupMessage(msg *models.Message) bool {
	if msg.Metadata == nil {
		return false
	}
	if isGroup, ok := msg.Metadata[MetaIsGroup].(bool); ok {
		return isGroup
	}
	// Check for group_id presence as fallback
	if groupID, ok := msg.Metadata[MetaGroupID].(string); ok && groupID != "" {
		return true
	}
	return false
}

// GetReplyTo returns the message ID being replied to, if any.
func GetReplyTo(msg *models.Message) string {
	if msg.Metadata == nil {
		return ""
	}
	if replyTo, ok := msg.Metadata[MetaReplyTo].(string); ok {
		return replyTo
	}
	return ""
}
