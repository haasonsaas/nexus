package channels

import (
	"fmt"
	"sort"
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ChatChannelID represents a supported chat channel.
// This type provides a unified identifier for all messaging platforms.
type ChatChannelID string

const (
	ChannelTelegram      ChatChannelID = "telegram"
	ChannelWhatsApp      ChatChannelID = "whatsapp"
	ChannelDiscord       ChatChannelID = "discord"
	ChannelGoogleChat    ChatChannelID = "googlechat"
	ChannelSlack         ChatChannelID = "slack"
	ChannelSignal        ChatChannelID = "signal"
	ChannelIMessage      ChatChannelID = "imessage"
	ChannelMatrix        ChatChannelID = "matrix"
	ChannelWeb           ChatChannelID = "web"
	ChannelAPI           ChatChannelID = "api"
	ChannelCLI           ChatChannelID = "cli"
	ChannelTeams         ChatChannelID = "teams"
	ChannelEmail         ChatChannelID = "email"
	ChannelMattermost    ChatChannelID = "mattermost"
	ChannelNextcloudTalk ChatChannelID = "nextcloud-talk"
	ChannelNostr         ChatChannelID = "nostr"
	ChannelZalo          ChatChannelID = "zalo"
	ChannelBlueBubbles   ChatChannelID = "bluebubbles"
)

// ChatChannelOrder defines the preferred channel ordering for UI display.
// Channels are ordered by popularity and ease of setup.
var ChatChannelOrder = []ChatChannelID{
	ChannelTelegram,
	ChannelWhatsApp,
	ChannelDiscord,
	ChannelGoogleChat,
	ChannelSlack,
	ChannelSignal,
	ChannelIMessage,
	ChannelMatrix,
	ChannelTeams,
	ChannelEmail,
	ChannelMattermost,
	ChannelNextcloudTalk,
	ChannelNostr,
	ChannelZalo,
	ChannelBlueBubbles,
	ChannelWeb,
	ChannelAPI,
	ChannelCLI,
}

// DefaultChatChannel is the default channel for new configurations.
const DefaultChatChannel = ChannelWhatsApp

// ChannelMeta contains metadata for a channel.
// This provides display information and documentation links for each channel.
type ChannelMeta struct {
	ID             ChatChannelID // Unique identifier
	Label          string        // Display name
	SelectionLabel string        // Label for selection UI (includes connection method)
	DetailLabel    string        // Label for detail views
	DocsPath       string        // Documentation path (relative to docs site)
	DocsLabel      string        // Documentation link label
	Blurb          string        // Short description for setup guidance
	SystemImage    string        // SF Symbol name (macOS/iOS)
	Aliases        []string      // Alternative names for normalization
}

// chatChannelMeta stores metadata for each channel.
var chatChannelMeta = map[ChatChannelID]*ChannelMeta{
	ChannelTelegram: {
		ID:             ChannelTelegram,
		Label:          "Telegram",
		SelectionLabel: "Telegram (Bot API)",
		DetailLabel:    "Telegram Bot",
		DocsPath:       "/channels/telegram",
		DocsLabel:      "telegram",
		Blurb:          "simplest way to get started — register a bot with @BotFather",
		SystemImage:    "paperplane",
		Aliases:        []string{"tg"},
	},
	ChannelWhatsApp: {
		ID:             ChannelWhatsApp,
		Label:          "WhatsApp",
		SelectionLabel: "WhatsApp (QR link)",
		DetailLabel:    "WhatsApp Web",
		DocsPath:       "/channels/whatsapp",
		DocsLabel:      "whatsapp",
		Blurb:          "works with your own number; recommend a separate phone",
		SystemImage:    "message",
		Aliases:        []string{"wa"},
	},
	ChannelDiscord: {
		ID:             ChannelDiscord,
		Label:          "Discord",
		SelectionLabel: "Discord (Bot API)",
		DetailLabel:    "Discord Bot",
		DocsPath:       "/channels/discord",
		DocsLabel:      "discord",
		Blurb:          "very well supported with rich embeds and slash commands",
		SystemImage:    "bubble.left.and.bubble.right",
	},
	ChannelGoogleChat: {
		ID:             ChannelGoogleChat,
		Label:          "Google Chat",
		SelectionLabel: "Google Chat (Webhook)",
		DetailLabel:    "Google Chat Bot",
		DocsPath:       "/channels/googlechat",
		DocsLabel:      "google-chat",
		Blurb:          "Google Workspace integration via webhooks",
		SystemImage:    "message.badge.filled.fill",
		Aliases:        []string{"gchat", "google-chat"},
	},
	ChannelSlack: {
		ID:             ChannelSlack,
		Label:          "Slack",
		SelectionLabel: "Slack (Socket Mode)",
		DetailLabel:    "Slack Bot",
		DocsPath:       "/channels/slack",
		DocsLabel:      "slack",
		Blurb:          "supported via Socket Mode for real-time messaging",
		SystemImage:    "number",
	},
	ChannelSignal: {
		ID:             ChannelSignal,
		Label:          "Signal",
		SelectionLabel: "Signal (signal-cli)",
		DetailLabel:    "Signal REST",
		DocsPath:       "/channels/signal",
		DocsLabel:      "signal",
		Blurb:          "signal-cli linked device for privacy-focused messaging",
		SystemImage:    "antenna.radiowaves.left.and.right",
	},
	ChannelIMessage: {
		ID:             ChannelIMessage,
		Label:          "iMessage",
		SelectionLabel: "iMessage (BlueBubbles/AppleScript)",
		DetailLabel:    "iMessage",
		DocsPath:       "/channels/imessage",
		DocsLabel:      "imessage",
		Blurb:          "requires macOS with Messages app configured",
		SystemImage:    "bubble.left.fill",
		Aliases:        []string{"imsg"},
	},
	ChannelMatrix: {
		ID:             ChannelMatrix,
		Label:          "Matrix",
		SelectionLabel: "Matrix (Client-Server API)",
		DetailLabel:    "Matrix Bot",
		DocsPath:       "/channels/matrix",
		DocsLabel:      "matrix",
		Blurb:          "federated, open protocol for secure communication",
		SystemImage:    "square.grid.3x3",
	},
	ChannelTeams: {
		ID:             ChannelTeams,
		Label:          "Microsoft Teams",
		SelectionLabel: "Teams (Bot Framework)",
		DetailLabel:    "Teams Bot",
		DocsPath:       "/channels/teams",
		DocsLabel:      "teams",
		Blurb:          "Microsoft 365 integration via Bot Framework",
		SystemImage:    "person.3",
		Aliases:        []string{"msteams", "ms-teams"},
	},
	ChannelEmail: {
		ID:             ChannelEmail,
		Label:          "Email",
		SelectionLabel: "Email (IMAP/SMTP)",
		DetailLabel:    "Email Bot",
		DocsPath:       "/channels/email",
		DocsLabel:      "email",
		Blurb:          "email-based AI assistant via IMAP polling",
		SystemImage:    "envelope",
		Aliases:        []string{"mail"},
	},
	ChannelMattermost: {
		ID:             ChannelMattermost,
		Label:          "Mattermost",
		SelectionLabel: "Mattermost (Bot API)",
		DetailLabel:    "Mattermost Bot",
		DocsPath:       "/channels/mattermost",
		DocsLabel:      "mattermost",
		Blurb:          "self-hosted team collaboration platform",
		SystemImage:    "bubble.middle.bottom",
		Aliases:        []string{"mm"},
	},
	ChannelNextcloudTalk: {
		ID:             ChannelNextcloudTalk,
		Label:          "Nextcloud Talk",
		SelectionLabel: "Nextcloud Talk (Bot API)",
		DetailLabel:    "Nextcloud Talk Bot",
		DocsPath:       "/channels/nextcloud-talk",
		DocsLabel:      "nextcloud-talk",
		Blurb:          "Nextcloud's built-in communication platform",
		SystemImage:    "cloud",
		Aliases:        []string{"nextcloud", "nc-talk"},
	},
	ChannelNostr: {
		ID:             ChannelNostr,
		Label:          "Nostr",
		SelectionLabel: "Nostr (NIP-01)",
		DetailLabel:    "Nostr Bot",
		DocsPath:       "/channels/nostr",
		DocsLabel:      "nostr",
		Blurb:          "decentralized social protocol",
		SystemImage:    "bolt.horizontal",
	},
	ChannelZalo: {
		ID:             ChannelZalo,
		Label:          "Zalo",
		SelectionLabel: "Zalo (OA API)",
		DetailLabel:    "Zalo Official Account",
		DocsPath:       "/channels/zalo",
		DocsLabel:      "zalo",
		Blurb:          "Vietnam's popular messaging platform",
		SystemImage:    "ellipsis.message",
	},
	ChannelBlueBubbles: {
		ID:             ChannelBlueBubbles,
		Label:          "BlueBubbles",
		SelectionLabel: "BlueBubbles (REST API)",
		DetailLabel:    "BlueBubbles Server",
		DocsPath:       "/channels/bluebubbles",
		DocsLabel:      "bluebubbles",
		Blurb:          "iMessage bridge server for cross-platform access",
		SystemImage:    "bubble.left.and.bubble.right.fill",
		Aliases:        []string{"bb"},
	},
	ChannelWeb: {
		ID:             ChannelWeb,
		Label:          "Web",
		SelectionLabel: "Web (WebSocket)",
		DetailLabel:    "Web Chat",
		DocsPath:       "/channels/web",
		DocsLabel:      "web",
		Blurb:          "embeddable web chat widget",
		SystemImage:    "globe",
	},
	ChannelAPI: {
		ID:             ChannelAPI,
		Label:          "API",
		SelectionLabel: "API (REST/GraphQL)",
		DetailLabel:    "API Client",
		DocsPath:       "/channels/api",
		DocsLabel:      "api",
		Blurb:          "programmatic access via REST or GraphQL",
		SystemImage:    "terminal",
	},
	ChannelCLI: {
		ID:             ChannelCLI,
		Label:          "CLI",
		SelectionLabel: "CLI (Terminal)",
		DetailLabel:    "Command Line",
		DocsPath:       "/channels/cli",
		DocsLabel:      "cli",
		Blurb:          "command-line interface for terminal users",
		SystemImage:    "apple.terminal",
	},
}

// chatChannelAliases maps aliases to canonical IDs.
var chatChannelAliases = map[string]ChatChannelID{
	"imsg":        ChannelIMessage,
	"google-chat": ChannelGoogleChat,
	"gchat":       ChannelGoogleChat,
	"tg":          ChannelTelegram,
	"wa":          ChannelWhatsApp,
	"msteams":     ChannelTeams,
	"ms-teams":    ChannelTeams,
	"mail":        ChannelEmail,
	"mm":          ChannelMattermost,
	"nextcloud":   ChannelNextcloudTalk,
	"nc-talk":     ChannelNextcloudTalk,
	"bb":          ChannelBlueBubbles,
}

// ChannelCapabilities defines feature support for a channel.
// This enables runtime feature detection for adaptive behavior.
type ChannelCapabilities struct {
	SupportsReactions   bool // Can add emoji reactions to messages
	SupportsTyping      bool // Can show typing indicators
	SupportsThreads     bool // Can organize messages into threads
	SupportsAttachments bool // Can send file attachments
	SupportsMentions    bool // Can mention users with @
	SupportsEditing     bool // Can edit sent messages
	SupportsDeleting    bool // Can delete sent messages
	SupportsRichText    bool // Can send formatted text (markdown, HTML)
	SupportsEmbeds      bool // Can send rich embeds/cards
	MaxMessageLength    int  // Maximum characters per message (0 = unlimited)
}

// channelCapabilities stores capabilities for each channel.
var channelCapabilities = map[ChatChannelID]*ChannelCapabilities{
	ChannelTelegram: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      false,
		MaxMessageLength:    4096,
	},
	ChannelWhatsApp: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     false,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      false,
		MaxMessageLength:    65536,
	},
	ChannelDiscord: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    2000,
	},
	ChannelGoogleChat: {
		SupportsReactions:   true,
		SupportsTyping:      false,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    28000,
	},
	ChannelSlack: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    40000,
	},
	ChannelSignal: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     false,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     false,
		SupportsDeleting:    true,
		SupportsRichText:    false,
		SupportsEmbeds:      false,
		MaxMessageLength:    0, // No documented limit
	},
	ChannelIMessage: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     false,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    false,
		SupportsRichText:    false,
		SupportsEmbeds:      false,
		MaxMessageLength:    0, // No documented limit
	},
	ChannelMatrix: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      false,
		MaxMessageLength:    65536,
	},
	ChannelTeams: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    28000,
	},
	ChannelEmail: {
		SupportsReactions:   false,
		SupportsTyping:      false,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    false,
		SupportsEditing:     false,
		SupportsDeleting:    false,
		SupportsRichText:    true,
		SupportsEmbeds:      false,
		MaxMessageLength:    0, // No limit
	},
	ChannelMattermost: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    16383,
	},
	ChannelNextcloudTalk: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      false,
		MaxMessageLength:    32000,
	},
	ChannelNostr: {
		SupportsReactions:   true,
		SupportsTyping:      false,
		SupportsThreads:     true,
		SupportsAttachments: false,
		SupportsMentions:    true,
		SupportsEditing:     false,
		SupportsDeleting:    false,
		SupportsRichText:    false,
		SupportsEmbeds:      false,
		MaxMessageLength:    65535,
	},
	ChannelZalo: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     false,
		SupportsAttachments: true,
		SupportsMentions:    false,
		SupportsEditing:     false,
		SupportsDeleting:    false,
		SupportsRichText:    false,
		SupportsEmbeds:      true,
		MaxMessageLength:    2000,
	},
	ChannelBlueBubbles: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     false,
		SupportsAttachments: true,
		SupportsMentions:    true,
		SupportsEditing:     true,
		SupportsDeleting:    false,
		SupportsRichText:    false,
		SupportsEmbeds:      false,
		MaxMessageLength:    0, // No documented limit
	},
	ChannelWeb: {
		SupportsReactions:   true,
		SupportsTyping:      true,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    false,
		SupportsEditing:     true,
		SupportsDeleting:    true,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    0, // No limit
	},
	ChannelAPI: {
		SupportsReactions:   false,
		SupportsTyping:      false,
		SupportsThreads:     true,
		SupportsAttachments: true,
		SupportsMentions:    false,
		SupportsEditing:     false,
		SupportsDeleting:    false,
		SupportsRichText:    true,
		SupportsEmbeds:      true,
		MaxMessageLength:    0, // No limit
	},
	ChannelCLI: {
		SupportsReactions:   false,
		SupportsTyping:      true,
		SupportsThreads:     false,
		SupportsAttachments: false,
		SupportsMentions:    false,
		SupportsEditing:     false,
		SupportsDeleting:    false,
		SupportsRichText:    true,
		SupportsEmbeds:      false,
		MaxMessageLength:    0, // No limit
	},
}

// ListChatChannels returns all channels in preferred order.
func ListChatChannels() []*ChannelMeta {
	result := make([]*ChannelMeta, 0, len(ChatChannelOrder))
	for _, id := range ChatChannelOrder {
		if meta, ok := chatChannelMeta[id]; ok {
			result = append(result, meta)
		}
	}
	return result
}

// ListChatChannelAliases returns all registered aliases sorted alphabetically.
func ListChatChannelAliases() []string {
	aliases := make([]string, 0, len(chatChannelAliases))
	for alias := range chatChannelAliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

// GetChatChannelMeta returns metadata for a channel.
// Returns nil if the channel is not found.
func GetChatChannelMeta(id ChatChannelID) *ChannelMeta {
	return chatChannelMeta[id]
}

// NormalizeChatChannelID normalizes a channel ID string to its canonical form.
// It handles aliases, case normalization, and whitespace trimming.
// Returns empty string if the input is not a valid channel ID or alias.
func NormalizeChatChannelID(raw string) ChatChannelID {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ""
	}

	// Check if it's already a valid channel ID
	id := ChatChannelID(normalized)
	if _, ok := chatChannelMeta[id]; ok {
		return id
	}

	// Check aliases
	if canonical, ok := chatChannelAliases[normalized]; ok {
		return canonical
	}

	return ""
}

// IsValidChannelID checks if a channel ID is valid.
func IsValidChannelID(id ChatChannelID) bool {
	_, ok := chatChannelMeta[id]
	return ok
}

// FormatChannelPrimerLine formats a channel for display in a primer/overview.
// Example: "Telegram — simplest way to get started — register a bot with @BotFather"
func FormatChannelPrimerLine(meta *ChannelMeta) string {
	if meta == nil {
		return ""
	}
	if meta.Blurb == "" {
		return meta.Label
	}
	return fmt.Sprintf("%s — %s", meta.Label, meta.Blurb)
}

// FormatChannelSelectionLine formats a channel for selection UI with optional docs link.
// Example: "Telegram (Bot API) [docs: https://example.com/channels/telegram]"
func FormatChannelSelectionLine(meta *ChannelMeta, docsURL string) string {
	if meta == nil {
		return ""
	}
	if docsURL == "" || meta.DocsPath == "" {
		return meta.SelectionLabel
	}
	return fmt.Sprintf("%s [docs: %s%s]", meta.SelectionLabel, docsURL, meta.DocsPath)
}

// GetChannelCapabilities returns capabilities for a channel.
// Returns nil if the channel is not found.
func GetChannelCapabilities(id ChatChannelID) *ChannelCapabilities {
	return channelCapabilities[id]
}

// ToModelChannelType converts a ChatChannelID to the models.ChannelType.
// Returns empty string if no mapping exists.
func ToModelChannelType(id ChatChannelID) models.ChannelType {
	switch id {
	case ChannelTelegram:
		return models.ChannelTelegram
	case ChannelDiscord:
		return models.ChannelDiscord
	case ChannelSlack:
		return models.ChannelSlack
	case ChannelAPI:
		return models.ChannelAPI
	case ChannelWhatsApp:
		return models.ChannelWhatsApp
	case ChannelSignal:
		return models.ChannelSignal
	case ChannelIMessage:
		return models.ChannelIMessage
	case ChannelMatrix:
		return models.ChannelMatrix
	case ChannelTeams:
		return models.ChannelTeams
	case ChannelEmail:
		return models.ChannelEmail
	case ChannelMattermost:
		return models.ChannelMattermost
	case ChannelNextcloudTalk:
		return models.ChannelNextcloudTalk
	case ChannelNostr:
		return models.ChannelNostr
	case ChannelZalo:
		return models.ChannelZalo
	case ChannelBlueBubbles:
		return models.ChannelBlueBubbles
	default:
		return ""
	}
}

// FromModelChannelType converts a models.ChannelType to ChatChannelID.
// Returns empty string if no mapping exists.
func FromModelChannelType(ct models.ChannelType) ChatChannelID {
	switch ct {
	case models.ChannelTelegram:
		return ChannelTelegram
	case models.ChannelDiscord:
		return ChannelDiscord
	case models.ChannelSlack:
		return ChannelSlack
	case models.ChannelAPI:
		return ChannelAPI
	case models.ChannelWhatsApp:
		return ChannelWhatsApp
	case models.ChannelSignal:
		return ChannelSignal
	case models.ChannelIMessage:
		return ChannelIMessage
	case models.ChannelMatrix:
		return ChannelMatrix
	case models.ChannelTeams:
		return ChannelTeams
	case models.ChannelEmail:
		return ChannelEmail
	case models.ChannelMattermost:
		return ChannelMattermost
	case models.ChannelNextcloudTalk:
		return ChannelNextcloudTalk
	case models.ChannelNostr:
		return ChannelNostr
	case models.ChannelZalo:
		return ChannelZalo
	case models.ChannelBlueBubbles:
		return ChannelBlueBubbles
	default:
		return ""
	}
}

// GetAllChannelIDs returns all registered channel IDs.
func GetAllChannelIDs() []ChatChannelID {
	ids := make([]ChatChannelID, 0, len(chatChannelMeta))
	for id := range chatChannelMeta {
		ids = append(ids, id)
	}
	return ids
}

// GetChannelsWithCapability returns all channels that have a specific capability.
func GetChannelsWithCapability(check func(*ChannelCapabilities) bool) []*ChannelMeta {
	var result []*ChannelMeta
	for _, id := range ChatChannelOrder {
		caps := channelCapabilities[id]
		if caps != nil && check(caps) {
			if meta := chatChannelMeta[id]; meta != nil {
				result = append(result, meta)
			}
		}
	}
	return result
}

// GetChannelsWithReactions returns all channels that support reactions.
func GetChannelsWithReactions() []*ChannelMeta {
	return GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
		return c.SupportsReactions
	})
}

// GetChannelsWithTyping returns all channels that support typing indicators.
func GetChannelsWithTyping() []*ChannelMeta {
	return GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
		return c.SupportsTyping
	})
}

// GetChannelsWithThreads returns all channels that support threads.
func GetChannelsWithThreads() []*ChannelMeta {
	return GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
		return c.SupportsThreads
	})
}

// GetChannelsWithAttachments returns all channels that support attachments.
func GetChannelsWithAttachments() []*ChannelMeta {
	return GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
		return c.SupportsAttachments
	})
}

// GetChannelsWithEditing returns all channels that support message editing.
func GetChannelsWithEditing() []*ChannelMeta {
	return GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
		return c.SupportsEditing
	})
}

// GetChannelsWithEmbeds returns all channels that support rich embeds.
func GetChannelsWithEmbeds() []*ChannelMeta {
	return GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
		return c.SupportsEmbeds
	})
}
