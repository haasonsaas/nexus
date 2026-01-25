package channels

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ============================================================================
// Adapter Registry Tests (existing)
// ============================================================================

type inboundOnlyAdapter struct {
	messages chan *models.Message
}

func (a *inboundOnlyAdapter) Type() models.ChannelType { return models.ChannelTelegram }

func (a *inboundOnlyAdapter) Messages() <-chan *models.Message { return a.messages }

type outboundOnlyAdapter struct{}

func (outboundOnlyAdapter) Type() models.ChannelType { return models.ChannelDiscord }

func (outboundOnlyAdapter) Send(ctx context.Context, msg *models.Message) error { return nil }

func TestRegistryGetOutbound(t *testing.T) {
	registry := NewRegistry()
	registry.Register(outboundOnlyAdapter{})

	if _, ok := registry.GetOutbound(models.ChannelDiscord); !ok {
		t.Fatalf("expected outbound adapter to be registered")
	}
}

func TestAggregateMessagesUsesInboundAdapters(t *testing.T) {
	registry := NewRegistry()
	inbound := &inboundOnlyAdapter{messages: make(chan *models.Message, 1)}
	registry.Register(inbound)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := registry.AggregateMessages(ctx)
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	inbound.messages <- msg

	got := <-out
	if got != msg {
		t.Fatalf("expected message to pass through, got %#v", got)
	}
}

// ============================================================================
// Channel Metadata Registry Tests (new)
// ============================================================================

func TestListChatChannels(t *testing.T) {
	channels := ListChatChannels()

	if len(channels) == 0 {
		t.Fatal("expected at least one channel")
	}

	// Verify order matches ChatChannelOrder
	for i, meta := range channels {
		if i >= len(ChatChannelOrder) {
			break
		}
		if meta.ID != ChatChannelOrder[i] {
			t.Errorf("channel at index %d: expected %s, got %s", i, ChatChannelOrder[i], meta.ID)
		}
	}

	// Verify each channel has required fields
	for _, meta := range channels {
		if meta.ID == "" {
			t.Error("channel has empty ID")
		}
		if meta.Label == "" {
			t.Errorf("channel %s has empty Label", meta.ID)
		}
		if meta.SelectionLabel == "" {
			t.Errorf("channel %s has empty SelectionLabel", meta.ID)
		}
	}
}

func TestListChatChannelAliases(t *testing.T) {
	aliases := ListChatChannelAliases()

	if len(aliases) == 0 {
		t.Fatal("expected at least one alias")
	}

	// Verify aliases are sorted
	for i := 1; i < len(aliases); i++ {
		if aliases[i-1] > aliases[i] {
			t.Errorf("aliases not sorted: %s > %s", aliases[i-1], aliases[i])
		}
	}

	// Verify all aliases resolve to valid channel IDs
	for _, alias := range aliases {
		id := NormalizeChatChannelID(alias)
		if id == "" {
			t.Errorf("alias %s does not resolve to a valid channel ID", alias)
		}
	}
}

func TestGetChatChannelMeta(t *testing.T) {
	tests := []struct {
		id       ChatChannelID
		wantNil  bool
		wantName string
	}{
		{ChannelTelegram, false, "Telegram"},
		{ChannelWhatsApp, false, "WhatsApp"},
		{ChannelDiscord, false, "Discord"},
		{ChannelSlack, false, "Slack"},
		{ChannelSignal, false, "Signal"},
		{ChannelIMessage, false, "iMessage"},
		{ChannelMatrix, false, "Matrix"},
		{ChannelWeb, false, "Web"},
		{ChannelAPI, false, "API"},
		{ChannelCLI, false, "CLI"},
		{"nonexistent", true, ""},
		{"", true, ""},
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			meta := GetChatChannelMeta(tc.id)
			if tc.wantNil {
				if meta != nil {
					t.Errorf("expected nil for ID %q, got %+v", tc.id, meta)
				}
				return
			}
			if meta == nil {
				t.Fatalf("expected non-nil for ID %q", tc.id)
			}
			if meta.Label != tc.wantName {
				t.Errorf("expected Label %q, got %q", tc.wantName, meta.Label)
			}
		})
	}
}

func TestNormalizeChatChannelID(t *testing.T) {
	tests := []struct {
		input string
		want  ChatChannelID
	}{
		// Direct channel IDs
		{"telegram", ChannelTelegram},
		{"whatsapp", ChannelWhatsApp},
		{"discord", ChannelDiscord},
		{"slack", ChannelSlack},
		{"signal", ChannelSignal},
		{"imessage", ChannelIMessage},
		{"matrix", ChannelMatrix},
		{"web", ChannelWeb},
		{"api", ChannelAPI},
		{"cli", ChannelCLI},

		// Case insensitivity
		{"TELEGRAM", ChannelTelegram},
		{"Telegram", ChannelTelegram},
		{"TeLEGram", ChannelTelegram},
		{"WHATSAPP", ChannelWhatsApp},
		{"WhatsApp", ChannelWhatsApp},

		// Whitespace handling
		{"  telegram  ", ChannelTelegram},
		{"\ttelegram\n", ChannelTelegram},

		// Aliases
		{"tg", ChannelTelegram},
		{"wa", ChannelWhatsApp},
		{"imsg", ChannelIMessage},
		{"gchat", ChannelGoogleChat},
		{"google-chat", ChannelGoogleChat},
		{"msteams", ChannelTeams},
		{"ms-teams", ChannelTeams},
		{"mail", ChannelEmail},
		{"mm", ChannelMattermost},
		{"bb", ChannelBlueBubbles},

		// Invalid inputs
		{"", ""},
		{"   ", ""},
		{"nonexistent", ""},
		{"invalid-channel", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeChatChannelID(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeChatChannelID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsValidChannelID(t *testing.T) {
	tests := []struct {
		id   ChatChannelID
		want bool
	}{
		{ChannelTelegram, true},
		{ChannelWhatsApp, true},
		{ChannelDiscord, true},
		{ChannelSlack, true},
		{ChannelSignal, true},
		{ChannelIMessage, true},
		{ChannelMatrix, true},
		{ChannelWeb, true},
		{ChannelAPI, true},
		{ChannelCLI, true},
		{ChannelTeams, true},
		{ChannelEmail, true},
		{ChannelMattermost, true},
		{"", false},
		{"nonexistent", false},
		{"tg", false}, // aliases are not valid IDs
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			got := IsValidChannelID(tc.id)
			if got != tc.want {
				t.Errorf("IsValidChannelID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

func TestFormatChannelPrimerLine(t *testing.T) {
	tests := []struct {
		id       ChatChannelID
		contains string
	}{
		{ChannelTelegram, "Telegram"},
		{ChannelWhatsApp, "WhatsApp"},
		{ChannelDiscord, "Discord"},
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			meta := GetChatChannelMeta(tc.id)
			line := FormatChannelPrimerLine(meta)
			if line == "" {
				t.Fatal("expected non-empty line")
			}
			if !containsString(line, tc.contains) {
				t.Errorf("line %q should contain %q", line, tc.contains)
			}
			// Should contain the blurb if it exists
			if meta.Blurb != "" && !containsString(line, meta.Blurb) {
				t.Errorf("line %q should contain blurb %q", line, meta.Blurb)
			}
		})
	}

	// Nil meta should return empty string
	if line := FormatChannelPrimerLine(nil); line != "" {
		t.Errorf("FormatChannelPrimerLine(nil) = %q, want empty", line)
	}
}

func TestFormatChannelSelectionLine(t *testing.T) {
	meta := GetChatChannelMeta(ChannelTelegram)
	if meta == nil {
		t.Fatal("expected telegram meta")
	}

	// Without docs URL
	line := FormatChannelSelectionLine(meta, "")
	if line != meta.SelectionLabel {
		t.Errorf("expected %q, got %q", meta.SelectionLabel, line)
	}

	// With docs URL
	docsURL := "https://docs.example.com"
	line = FormatChannelSelectionLine(meta, docsURL)
	if !containsString(line, docsURL) {
		t.Errorf("line %q should contain docs URL %q", line, docsURL)
	}
	if !containsString(line, meta.DocsPath) {
		t.Errorf("line %q should contain docs path %q", line, meta.DocsPath)
	}

	// Nil meta should return empty string
	if line := FormatChannelSelectionLine(nil, docsURL); line != "" {
		t.Errorf("FormatChannelSelectionLine(nil, _) = %q, want empty", line)
	}
}

func TestGetChannelCapabilities(t *testing.T) {
	tests := []struct {
		id                    ChatChannelID
		wantReactions         bool
		wantTyping            bool
		wantMinMessageLength  int
		expectMessageLengthGT int
	}{
		{ChannelTelegram, true, true, 4096, 0},
		{ChannelWhatsApp, true, true, 0, 1000},
		{ChannelDiscord, true, true, 2000, 0},
		{ChannelSlack, true, true, 0, 10000},
		{ChannelEmail, false, false, 0, 0},
		{ChannelCLI, false, true, 0, 0},
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			caps := GetChannelCapabilities(tc.id)
			if caps == nil {
				t.Fatalf("expected non-nil capabilities for %s", tc.id)
			}
			if caps.SupportsReactions != tc.wantReactions {
				t.Errorf("SupportsReactions = %v, want %v", caps.SupportsReactions, tc.wantReactions)
			}
			if caps.SupportsTyping != tc.wantTyping {
				t.Errorf("SupportsTyping = %v, want %v", caps.SupportsTyping, tc.wantTyping)
			}
			if tc.wantMinMessageLength > 0 && caps.MaxMessageLength != tc.wantMinMessageLength {
				t.Errorf("MaxMessageLength = %d, want %d", caps.MaxMessageLength, tc.wantMinMessageLength)
			}
			if tc.expectMessageLengthGT > 0 && caps.MaxMessageLength <= tc.expectMessageLengthGT {
				t.Errorf("MaxMessageLength = %d, want > %d", caps.MaxMessageLength, tc.expectMessageLengthGT)
			}
		})
	}

	// Nonexistent channel should return nil
	if caps := GetChannelCapabilities("nonexistent"); caps != nil {
		t.Errorf("GetChannelCapabilities(nonexistent) = %+v, want nil", caps)
	}
}

func TestDefaultChatChannel(t *testing.T) {
	if DefaultChatChannel == "" {
		t.Error("DefaultChatChannel should not be empty")
	}
	if !IsValidChannelID(DefaultChatChannel) {
		t.Errorf("DefaultChatChannel %q is not a valid channel ID", DefaultChatChannel)
	}
}

func TestChatChannelOrderCompleteness(t *testing.T) {
	// All channels in the order list should have metadata
	for _, id := range ChatChannelOrder {
		meta := GetChatChannelMeta(id)
		if meta == nil {
			t.Errorf("channel %s in order list has no metadata", id)
		}
	}
}

func TestChannelMetadataCompleteness(t *testing.T) {
	channels := ListChatChannels()
	for _, meta := range channels {
		// Check required fields
		if meta.DocsPath == "" {
			t.Errorf("channel %s has empty DocsPath", meta.ID)
		}
		if meta.SystemImage == "" {
			t.Errorf("channel %s has empty SystemImage", meta.ID)
		}
	}
}

func TestChannelCapabilitiesCompleteness(t *testing.T) {
	// Every channel with metadata should have capabilities
	for _, id := range ChatChannelOrder {
		caps := GetChannelCapabilities(id)
		if caps == nil {
			t.Errorf("channel %s has no capabilities defined", id)
		}
	}
}

func TestToModelChannelType(t *testing.T) {
	tests := []struct {
		id   ChatChannelID
		want models.ChannelType
	}{
		{ChannelTelegram, models.ChannelTelegram},
		{ChannelWhatsApp, models.ChannelWhatsApp},
		{ChannelDiscord, models.ChannelDiscord},
		{ChannelSlack, models.ChannelSlack},
		{ChannelAPI, models.ChannelAPI},
		{ChannelSignal, models.ChannelSignal},
		{ChannelIMessage, models.ChannelIMessage},
		{ChannelMatrix, models.ChannelMatrix},
		{ChannelTeams, models.ChannelTeams},
		{ChannelEmail, models.ChannelEmail},
		{ChannelMattermost, models.ChannelMattermost},
		{ChannelNextcloudTalk, models.ChannelNextcloudTalk},
		{ChannelNostr, models.ChannelNostr},
		{ChannelZalo, models.ChannelZalo},
		{ChannelBlueBubbles, models.ChannelBlueBubbles},
		// Channels without direct model mapping
		{ChannelWeb, ""},
		{ChannelCLI, ""},
		{ChannelGoogleChat, ""},
		{"nonexistent", ""},
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			got := ToModelChannelType(tc.id)
			if got != tc.want {
				t.Errorf("ToModelChannelType(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

func TestFromModelChannelType(t *testing.T) {
	tests := []struct {
		ct   models.ChannelType
		want ChatChannelID
	}{
		{models.ChannelTelegram, ChannelTelegram},
		{models.ChannelWhatsApp, ChannelWhatsApp},
		{models.ChannelDiscord, ChannelDiscord},
		{models.ChannelSlack, ChannelSlack},
		{models.ChannelAPI, ChannelAPI},
		{models.ChannelSignal, ChannelSignal},
		{models.ChannelIMessage, ChannelIMessage},
		{models.ChannelMatrix, ChannelMatrix},
		{models.ChannelTeams, ChannelTeams},
		{models.ChannelEmail, ChannelEmail},
		{models.ChannelMattermost, ChannelMattermost},
		{models.ChannelNextcloudTalk, ChannelNextcloudTalk},
		{models.ChannelNostr, ChannelNostr},
		{models.ChannelZalo, ChannelZalo},
		{models.ChannelBlueBubbles, ChannelBlueBubbles},
		{"nonexistent", ""},
	}

	for _, tc := range tests {
		t.Run(string(tc.ct), func(t *testing.T) {
			got := FromModelChannelType(tc.ct)
			if got != tc.want {
				t.Errorf("FromModelChannelType(%q) = %q, want %q", tc.ct, got, tc.want)
			}
		})
	}
}

func TestModelTypeRoundTrip(t *testing.T) {
	// Test that converting to model type and back preserves the ID
	// (for channels that have model type mappings)
	modelMappedChannels := []ChatChannelID{
		ChannelTelegram,
		ChannelWhatsApp,
		ChannelDiscord,
		ChannelSlack,
		ChannelAPI,
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
	}

	for _, id := range modelMappedChannels {
		t.Run(string(id), func(t *testing.T) {
			modelType := ToModelChannelType(id)
			if modelType == "" {
				t.Fatalf("expected model type for %s", id)
			}
			backToID := FromModelChannelType(modelType)
			if backToID != id {
				t.Errorf("round trip failed: %s -> %s -> %s", id, modelType, backToID)
			}
		})
	}
}

func TestGetAllChannelIDs(t *testing.T) {
	ids := GetAllChannelIDs()
	if len(ids) == 0 {
		t.Fatal("expected at least one channel ID")
	}

	// All IDs should be valid
	for _, id := range ids {
		if !IsValidChannelID(id) {
			t.Errorf("GetAllChannelIDs returned invalid ID: %s", id)
		}
	}

	// Should match the number of entries in chatChannelMeta
	channels := ListChatChannels()
	if len(ids) != len(channels) {
		t.Errorf("GetAllChannelIDs returned %d IDs, but ListChatChannels has %d", len(ids), len(channels))
	}
}

func TestGetChannelsWithCapability(t *testing.T) {
	t.Run("reactions", func(t *testing.T) {
		channels := GetChannelsWithReactions()
		if len(channels) == 0 {
			t.Fatal("expected at least one channel with reactions")
		}
		for _, meta := range channels {
			caps := GetChannelCapabilities(meta.ID)
			if !caps.SupportsReactions {
				t.Errorf("channel %s should support reactions", meta.ID)
			}
		}
	})

	t.Run("typing", func(t *testing.T) {
		channels := GetChannelsWithTyping()
		if len(channels) == 0 {
			t.Fatal("expected at least one channel with typing")
		}
		for _, meta := range channels {
			caps := GetChannelCapabilities(meta.ID)
			if !caps.SupportsTyping {
				t.Errorf("channel %s should support typing", meta.ID)
			}
		}
	})

	t.Run("threads", func(t *testing.T) {
		channels := GetChannelsWithThreads()
		if len(channels) == 0 {
			t.Fatal("expected at least one channel with threads")
		}
		for _, meta := range channels {
			caps := GetChannelCapabilities(meta.ID)
			if !caps.SupportsThreads {
				t.Errorf("channel %s should support threads", meta.ID)
			}
		}
	})

	t.Run("attachments", func(t *testing.T) {
		channels := GetChannelsWithAttachments()
		if len(channels) == 0 {
			t.Fatal("expected at least one channel with attachments")
		}
		for _, meta := range channels {
			caps := GetChannelCapabilities(meta.ID)
			if !caps.SupportsAttachments {
				t.Errorf("channel %s should support attachments", meta.ID)
			}
		}
	})

	t.Run("editing", func(t *testing.T) {
		channels := GetChannelsWithEditing()
		if len(channels) == 0 {
			t.Fatal("expected at least one channel with editing")
		}
		for _, meta := range channels {
			caps := GetChannelCapabilities(meta.ID)
			if !caps.SupportsEditing {
				t.Errorf("channel %s should support editing", meta.ID)
			}
		}
	})

	t.Run("embeds", func(t *testing.T) {
		channels := GetChannelsWithEmbeds()
		if len(channels) == 0 {
			t.Fatal("expected at least one channel with embeds")
		}
		for _, meta := range channels {
			caps := GetChannelCapabilities(meta.ID)
			if !caps.SupportsEmbeds {
				t.Errorf("channel %s should support embeds", meta.ID)
			}
		}
	})

	t.Run("custom capability check", func(t *testing.T) {
		// Channels with high message limits
		highLimit := GetChannelsWithCapability(func(c *ChannelCapabilities) bool {
			return c.MaxMessageLength >= 10000
		})
		if len(highLimit) == 0 {
			t.Fatal("expected at least one channel with high message limit")
		}
		for _, meta := range highLimit {
			caps := GetChannelCapabilities(meta.ID)
			if caps.MaxMessageLength < 10000 {
				t.Errorf("channel %s has limit %d, want >= 10000", meta.ID, caps.MaxMessageLength)
			}
		}
	})
}

func TestChannelAliasesPointToValidChannels(t *testing.T) {
	for alias, id := range chatChannelAliases {
		if !IsValidChannelID(id) {
			t.Errorf("alias %q points to invalid channel ID %q", alias, id)
		}
	}
}

func TestChannelMetaAliasesMatchGlobalAliases(t *testing.T) {
	// Verify that aliases defined in ChannelMeta are also in chatChannelAliases
	for _, meta := range chatChannelMeta {
		for _, alias := range meta.Aliases {
			if canonical, ok := chatChannelAliases[alias]; !ok {
				t.Errorf("channel %s meta has alias %q not in global aliases", meta.ID, alias)
			} else if canonical != meta.ID {
				t.Errorf("alias %q: meta says %s, global says %s", alias, meta.ID, canonical)
			}
		}
	}
}

// Helper function for string contains check
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 &&
			func() bool {
				for i := 0; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}()))
}
