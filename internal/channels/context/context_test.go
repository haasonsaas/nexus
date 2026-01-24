package context

import (
	"testing"
)

func TestGetChannelInfo(t *testing.T) {
	tests := []struct {
		channel        string
		wantMaxLen     int
		wantMarkdown   bool
		wantMentions   bool
		wantMentionFmt string
	}{
		{"telegram", 4096, true, true, "@%s"},
		{"discord", 2000, true, true, "<@%s>"},
		{"slack", 40000, true, true, "<@%s>"},
		{"sms", 160, false, false, ""},
		{"unknown", 4000, false, false, ""}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			info := GetChannelInfo(tt.channel)
			if info.MaxMessageLength != tt.wantMaxLen {
				t.Errorf("MaxMessageLength = %d, want %d", info.MaxMessageLength, tt.wantMaxLen)
			}
			if info.SupportsMarkdown != tt.wantMarkdown {
				t.Errorf("SupportsMarkdown = %v, want %v", info.SupportsMarkdown, tt.wantMarkdown)
			}
			if info.SupportsMentions != tt.wantMentions {
				t.Errorf("SupportsMentions = %v, want %v", info.SupportsMentions, tt.wantMentions)
			}
			if info.MentionFormat != tt.wantMentionFmt {
				t.Errorf("MentionFormat = %q, want %q", info.MentionFormat, tt.wantMentionFmt)
			}
		})
	}
}

func TestDeliveryContext_FormatMention(t *testing.T) {
	tests := []struct {
		channel string
		userID  string
		want    string
	}{
		{"discord", "123456", "<@123456>"},
		{"slack", "U123ABC", "<@U123ABC>"},
		{"telegram", "johndoe", "@johndoe"},
		{"sms", "user", "user"}, // No mention support
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			dc := New(tt.channel)
			got := dc.FormatMention(tt.userID)
			if got != tt.want {
				t.Errorf("FormatMention(%q) = %q, want %q", tt.userID, got, tt.want)
			}
		})
	}
}

func TestDeliveryContext_Chaining(t *testing.T) {
	dc := New("slack").
		WithUser("U123").
		WithConversation("C456").
		WithThread("T789").
		WithReplyTo("M012")

	if dc.UserID != "U123" {
		t.Errorf("UserID = %q, want %q", dc.UserID, "U123")
	}
	if dc.ConversationID != "C456" {
		t.Errorf("ConversationID = %q, want %q", dc.ConversationID, "C456")
	}
	if dc.ThreadID != "T789" {
		t.Errorf("ThreadID = %q, want %q", dc.ThreadID, "T789")
	}
	if dc.ReplyToMessageID != "M012" {
		t.Errorf("ReplyToMessageID = %q, want %q", dc.ReplyToMessageID, "M012")
	}
}

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bold",
			input: "This is **bold** text",
			want:  "This is bold text",
		},
		{
			name:  "italic asterisk",
			input: "This is *italic* text",
			want:  "This is italic text",
		},
		{
			name:  "italic underscore",
			input: "This is _italic_ text",
			want:  "This is italic text",
		},
		{
			name:  "strikethrough",
			input: "This is ~~deleted~~ text",
			want:  "This is deleted text",
		},
		{
			name:  "inline code",
			input: "Use `code` here",
			want:  "Use code here",
		},
		{
			name:  "link",
			input: "Check [this link](https://example.com)",
			want:  "Check this link",
		},
		{
			name:  "header",
			input: "## Header\nContent",
			want:  "Header\nContent",
		},
		{
			name:  "code block",
			input: "```python\nprint('hello')\n```",
			want:  "print('hello')\n",
		},
		{
			name:  "mixed",
			input: "**Bold** and *italic* with [link](http://x.com)",
			want:  "Bold and italic with link",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("StripMarkdown() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToSlackMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bold",
			input: "This is **bold** text",
			want:  "This is *bold* text",
		},
		{
			name:  "link",
			input: "Check [this link](https://example.com)",
			want:  "Check <https://example.com|this link>",
		},
		{
			name:  "strikethrough",
			input: "This is ~~deleted~~ text",
			want:  "This is ~deleted~ text",
		},
		{
			name:  "combined",
			input: "**Bold** with [link](http://x.com) and ~~strike~~",
			want:  "*Bold* with <http://x.com|link> and ~strike~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToSlackMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("ToSlackMarkdown() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeliveryContext_ShouldChunk(t *testing.T) {
	tests := []struct {
		channel   string
		textLen   int
		wantChunk bool
	}{
		{"telegram", 4000, false},
		{"telegram", 5000, true},
		{"discord", 2000, false},
		{"discord", 2001, true},
		{"sms", 160, false},
		{"sms", 161, true},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			dc := New(tt.channel)
			text := make([]byte, tt.textLen)
			for i := range text {
				text[i] = 'a'
			}
			got := dc.ShouldChunk(string(text))
			if got != tt.wantChunk {
				t.Errorf("ShouldChunk(%d chars) = %v, want %v", tt.textLen, got, tt.wantChunk)
			}
		})
	}
}

func TestDeliveryContext_FormatText(t *testing.T) {
	tests := []struct {
		channel string
		input   string
		want    string
	}{
		// SMS strips markdown
		{"sms", "**bold** and *italic*", "bold and italic"},
		// Signal strips markdown
		{"signal", "Check [link](http://x.com)", "Check link"},
		// Slack converts to mrkdwn
		{"slack", "**bold** text", "*bold* text"},
		// Standard markdown kept as-is for discord
		{"discord", "**bold** text", "**bold** text"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			dc := New(tt.channel)
			got := dc.FormatText(tt.input)
			if got != tt.want {
				t.Errorf("FormatText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannelInfo_Attachments(t *testing.T) {
	// Verify attachment limits are set correctly
	tests := []struct {
		channel      string
		wantAttach   bool
		wantMaxBytes int64
	}{
		{"telegram", true, 50 * 1024 * 1024},
		{"discord", true, 8 * 1024 * 1024},
		{"sms", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			info := GetChannelInfo(tt.channel)
			if info.SupportsAttachments != tt.wantAttach {
				t.Errorf("SupportsAttachments = %v, want %v", info.SupportsAttachments, tt.wantAttach)
			}
			if info.MaxAttachmentBytes != tt.wantMaxBytes {
				t.Errorf("MaxAttachmentBytes = %d, want %d", info.MaxAttachmentBytes, tt.wantMaxBytes)
			}
		})
	}
}

func TestChannelInfo_Capabilities(t *testing.T) {
	// Verify various channel capabilities
	telegram := GetChannelInfo("telegram")
	if !telegram.SupportsEditing {
		t.Error("telegram should support editing")
	}
	if !telegram.SupportsThreads {
		t.Error("telegram should support threads")
	}
	if !telegram.SupportsReactions {
		t.Error("telegram should support reactions")
	}

	signal := GetChannelInfo("signal")
	if signal.SupportsEditing {
		t.Error("signal should not support editing")
	}
	if signal.SupportsThreads {
		t.Error("signal should not support threads")
	}
}

// Additional edge case tests

func TestGetChannelInfo_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"TELEGRAM", "telegram"},
		{"Telegram", "telegram"},
		{"DISCORD", "discord"},
		{"Discord", "discord"},
		{"SLACK", "slack"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			info := GetChannelInfo(tt.input)
			if info.Name != tt.want {
				t.Errorf("GetChannelInfo(%q).Name = %q, want %q", tt.input, info.Name, tt.want)
			}
		})
	}
}

func TestGetChannelInfo_AllChannels(t *testing.T) {
	channels := []string{
		"telegram", "discord", "slack", "whatsapp", "signal",
		"imessage", "matrix", "sms", "email", "teams",
	}

	for _, ch := range channels {
		t.Run(ch, func(t *testing.T) {
			info := GetChannelInfo(ch)
			if info.Name != ch {
				t.Errorf("Name = %q, want %q", info.Name, ch)
			}
		})
	}
}

func TestChannelInfo_HTMLSupport(t *testing.T) {
	// Check which channels support HTML
	tests := []struct {
		channel  string
		wantHTML bool
	}{
		{"matrix", true},
		{"email", true},
		{"teams", true},
		{"telegram", false},
		{"discord", false},
		{"slack", false},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			info := GetChannelInfo(tt.channel)
			if info.SupportsHTML != tt.wantHTML {
				t.Errorf("SupportsHTML = %v, want %v", info.SupportsHTML, tt.wantHTML)
			}
		})
	}
}

func TestChannelInfo_MarkdownFlavor(t *testing.T) {
	tests := []struct {
		channel string
		want    string
	}{
		{"telegram", "telegram"},
		{"discord", "discord"},
		{"slack", "slack"},
		{"whatsapp", "whatsapp"},
		{"signal", "none"},
		{"sms", "none"},
		{"matrix", "standard"},
		{"email", "standard"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			info := GetChannelInfo(tt.channel)
			if info.MarkdownFlavor != tt.want {
				t.Errorf("MarkdownFlavor = %q, want %q", info.MarkdownFlavor, tt.want)
			}
		})
	}
}

func TestDeliveryContext_New(t *testing.T) {
	dc := New("telegram")

	if dc.Channel.Name != "telegram" {
		t.Errorf("Channel.Name = %q, want %q", dc.Channel.Name, "telegram")
	}
	if dc.UserID != "" {
		t.Error("UserID should be empty")
	}
	if dc.ConversationID != "" {
		t.Error("ConversationID should be empty")
	}
	if dc.ThreadID != "" {
		t.Error("ThreadID should be empty")
	}
	if dc.ReplyToMessageID != "" {
		t.Error("ReplyToMessageID should be empty")
	}
}

func TestDeliveryContext_FormatMention_EmptyFormat(t *testing.T) {
	// For channels without mention support
	dc := New("sms")
	got := dc.FormatMention("12345")
	if got != "12345" {
		t.Errorf("FormatMention() = %q, want %q", got, "12345")
	}
}

func TestDeliveryContext_ShouldChunk_UnlimitedChannel(t *testing.T) {
	// Email has no message limit (0)
	dc := New("email")

	// Even very long text shouldn't need chunking
	longText := make([]byte, 1000000)
	for i := range longText {
		longText[i] = 'a'
	}

	if dc.ShouldChunk(string(longText)) {
		t.Error("email channel should not require chunking")
	}
}

func TestStripMarkdown_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no markdown", "plain text", "plain text"},
		{"nested bold italic", "***bold italic***", "bold italic"}, // Both stripped
		{"multiple headers", "# H1\n## H2\n### H3", "H1\nH2\nH3"},
		{"dash bullet points", "- item 1\n- item 2", "- item 1\n- item 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("StripMarkdown() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToSlackMarkdown_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no markdown", "plain text", "plain text"},
		{"multiple bold", "**one** and **two**", "*one* and *two*"},
		{"multiple links", "[a](http://a.com) [b](http://b.com)", "<http://a.com|a> <http://b.com|b>"},
		{"mixed", "**bold** _italic_ ~~strike~~", "*bold* _italic_ ~strike~"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToSlackMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("ToSlackMarkdown() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToTelegramMarkdown(t *testing.T) {
	// ToTelegramMarkdown escapes special characters
	input := "Hello_world"
	got := ToTelegramMarkdown(input)

	// The underscore should be escaped
	if got == input {
		t.Error("ToTelegramMarkdown should escape special characters")
	}
}

func TestDeliveryContext_FormatText_UnknownChannel(t *testing.T) {
	dc := New("unknown_channel")

	// Unknown channels use "none" flavor, which strips markdown
	input := "**bold** text"
	got := dc.FormatText(input)

	if got != "bold text" {
		t.Errorf("FormatText() = %q, want %q", got, "bold text")
	}
}

func TestChannelInfo_Teams(t *testing.T) {
	info := GetChannelInfo("teams")

	if info.MaxMessageLength != 28000 {
		t.Errorf("MaxMessageLength = %d, want 28000", info.MaxMessageLength)
	}
	if !info.SupportsMarkdown {
		t.Error("teams should support markdown")
	}
	if !info.SupportsHTML {
		t.Error("teams should support HTML")
	}
	if info.MentionFormat != "<at>%s</at>" {
		t.Errorf("MentionFormat = %q, want %q", info.MentionFormat, "<at>%s</at>")
	}
}

func TestChannelInfo_WhatsApp(t *testing.T) {
	info := GetChannelInfo("whatsapp")

	if info.MaxMessageLength != 65536 {
		t.Errorf("MaxMessageLength = %d, want 65536", info.MaxMessageLength)
	}
	if !info.SupportsReactions {
		t.Error("whatsapp should support reactions")
	}
	if info.SupportsEditing {
		t.Error("whatsapp should not support editing")
	}
	if info.MentionFormat != "@%s" {
		t.Errorf("MentionFormat = %q, want %q", info.MentionFormat, "@%s")
	}
}

func TestChannelInfo_Matrix(t *testing.T) {
	info := GetChannelInfo("matrix")

	if !info.SupportsMarkdown {
		t.Error("matrix should support markdown")
	}
	if !info.SupportsHTML {
		t.Error("matrix should support HTML")
	}
	if info.MarkdownFlavor != "standard" {
		t.Errorf("MarkdownFlavor = %q, want standard", info.MarkdownFlavor)
	}
	if !info.SupportsEditing {
		t.Error("matrix should support editing")
	}
}

func TestChannelInfo_IMessage(t *testing.T) {
	info := GetChannelInfo("imessage")

	if info.MaxMessageLength != 20000 {
		t.Errorf("MaxMessageLength = %d, want 20000", info.MaxMessageLength)
	}
	if info.SupportsMarkdown {
		t.Error("imessage should not support markdown")
	}
	if info.SupportsEditing {
		t.Error("imessage should not support editing")
	}
	if !info.SupportsReactions {
		t.Error("imessage should support reactions")
	}
}
