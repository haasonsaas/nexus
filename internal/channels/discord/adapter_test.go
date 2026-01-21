package discord

import (
	"context"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// mockDiscordSession is a mock implementation for testing
type mockDiscordSession struct {
	openCalled           bool
	closeCalled          bool
	channelMessageSendFn func(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	channelFileSendFn    func(channelID, name string, r interface{}, options ...discordgo.RequestOption) (*discordgo.Message, error)
	messageReactionAddFn func(channelID, messageID, emoji string) error
	threadStartFn        func(channelID, name string, archiveDuration int) (*discordgo.Channel, error)
}

func (m *mockDiscordSession) Open() error {
	m.openCalled = true
	return nil
}

func (m *mockDiscordSession) Close() error {
	m.closeCalled = true
	return nil
}

func (m *mockDiscordSession) ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	if m.channelMessageSendFn != nil {
		return m.channelMessageSendFn(channelID, content, options...)
	}
	return &discordgo.Message{
		ID:        "test-msg-id",
		ChannelID: channelID,
		Content:   content,
	}, nil
}

func (m *mockDiscordSession) ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return &discordgo.Message{
		ID:        "test-msg-id",
		ChannelID: channelID,
		Content:   data.Content,
		Embeds:    data.Embeds,
	}, nil
}

func (m *mockDiscordSession) ChannelFileSend(channelID, name string, r interface{}, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	if m.channelFileSendFn != nil {
		return m.channelFileSendFn(channelID, name, r, options...)
	}
	return &discordgo.Message{ID: "test-msg-id"}, nil
}

func (m *mockDiscordSession) MessageReactionAdd(channelID, messageID, emoji string) error {
	if m.messageReactionAddFn != nil {
		return m.messageReactionAddFn(channelID, messageID, emoji)
	}
	return nil
}

func (m *mockDiscordSession) ThreadStart(channelID, name string, archiveDuration int) (*discordgo.Channel, error) {
	if m.threadStartFn != nil {
		return m.threadStartFn(channelID, name, archiveDuration)
	}
	return &discordgo.Channel{
		ID:   "test-thread-id",
		Name: name,
		Type: discordgo.ChannelTypeGuildPublicThread,
	}, nil
}

func (m *mockDiscordSession) AddHandler(handler interface{}) func() {
	return func() {}
}

func (m *mockDiscordSession) ApplicationCommandBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand, options ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
	return commands, nil
}

func TestNewAdapter(t *testing.T) {
	token := "test-token"
	adapter := NewAdapter(token)

	if adapter == nil {
		t.Fatal("NewAdapter returned nil")
	}

	if adapter.Type() != models.ChannelDiscord {
		t.Errorf("Expected channel type %s, got %s", models.ChannelDiscord, adapter.Type())
	}

	status := adapter.Status()
	if status.Connected {
		t.Error("Expected adapter to be disconnected initially")
	}
}

func TestAdapter_StartStop(t *testing.T) {
	adapter := NewAdapter("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	ctx := context.Background()

	// Test Start
	err := adapter.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !mock.openCalled {
		t.Error("Expected session.Open to be called")
	}

	status := adapter.Status()
	if !status.Connected {
		t.Error("Expected adapter to be connected after Start")
	}

	// Test Stop
	err = adapter.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if !mock.closeCalled {
		t.Error("Expected session.Close to be called")
	}

	status = adapter.Status()
	if status.Connected {
		t.Error("Expected adapter to be disconnected after Stop")
	}
}

func TestAdapter_Messages(t *testing.T) {
	adapter := NewAdapter("test-token")

	msgChan := adapter.Messages()
	if msgChan == nil {
		t.Fatal("Messages() returned nil channel")
	}
}

func TestConvertDiscordMessage(t *testing.T) {
	tests := []struct {
		name     string
		discord  *discordgo.Message
		expected *models.Message
	}{
		{
			name: "simple text message",
			discord: &discordgo.Message{
				ID:        "discord-msg-123",
				ChannelID: "channel-456",
				Content:   "Hello, world!",
				Author: &discordgo.User{
					ID:       "user-789",
					Username: "testuser",
				},
				Timestamp: discordgo.Timestamp("2024-01-20T12:00:00Z"),
			},
			expected: &models.Message{
				Channel:    models.ChannelDiscord,
				ChannelID:  "discord-msg-123",
				Direction:  models.DirectionInbound,
				Role:       models.RoleUser,
				Content:    "Hello, world!",
				Metadata: map[string]any{
					"discord_channel_id": "channel-456",
					"discord_user_id":    "user-789",
					"discord_username":   "testuser",
				},
			},
		},
		{
			name: "message with attachments",
			discord: &discordgo.Message{
				ID:        "discord-msg-124",
				ChannelID: "channel-456",
				Content:   "Check this image",
				Author: &discordgo.User{
					ID:       "user-789",
					Username: "testuser",
				},
				Timestamp: discordgo.Timestamp("2024-01-20T12:00:00Z"),
				Attachments: []*discordgo.MessageAttachment{
					{
						ID:          "attach-001",
						Filename:    "image.png",
						URL:         "https://cdn.discord.com/image.png",
						ContentType: "image/png",
						Size:        1024,
					},
				},
			},
			expected: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "discord-msg-124",
				Direction: models.DirectionInbound,
				Role:      models.RoleUser,
				Content:   "Check this image",
				Attachments: []models.Attachment{
					{
						ID:       "attach-001",
						Type:     "image",
						URL:      "https://cdn.discord.com/image.png",
						Filename: "image.png",
						MimeType: "image/png",
						Size:     1024,
					},
				},
				Metadata: map[string]any{
					"discord_channel_id": "channel-456",
					"discord_user_id":    "user-789",
					"discord_username":   "testuser",
				},
			},
		},
		{
			name: "message in thread",
			discord: &discordgo.Message{
				ID:        "discord-msg-125",
				ChannelID: "thread-789",
				Content:   "Thread reply",
				Author: &discordgo.User{
					ID:       "user-789",
					Username: "testuser",
				},
				Timestamp: discordgo.Timestamp("2024-01-20T12:00:00Z"),
				Thread: &discordgo.Channel{
					ID:       "thread-789",
					ParentID: "channel-456",
					Name:     "Discussion Thread",
				},
			},
			expected: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "discord-msg-125",
				Direction: models.DirectionInbound,
				Role:      models.RoleUser,
				Content:   "Thread reply",
				Metadata: map[string]any{
					"discord_channel_id":  "thread-789",
					"discord_user_id":     "user-789",
					"discord_username":    "testuser",
					"discord_thread_id":   "thread-789",
					"discord_thread_name": "Discussion Thread",
					"discord_parent_id":   "channel-456",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDiscordMessage(tt.discord)

			if result.Channel != tt.expected.Channel {
				t.Errorf("Channel: got %v, want %v", result.Channel, tt.expected.Channel)
			}
			if result.ChannelID != tt.expected.ChannelID {
				t.Errorf("ChannelID: got %v, want %v", result.ChannelID, tt.expected.ChannelID)
			}
			if result.Direction != tt.expected.Direction {
				t.Errorf("Direction: got %v, want %v", result.Direction, tt.expected.Direction)
			}
			if result.Role != tt.expected.Role {
				t.Errorf("Role: got %v, want %v", result.Role, tt.expected.Role)
			}
			if result.Content != tt.expected.Content {
				t.Errorf("Content: got %v, want %v", result.Content, tt.expected.Content)
			}

			// Check attachments
			if len(result.Attachments) != len(tt.expected.Attachments) {
				t.Errorf("Attachments length: got %d, want %d", len(result.Attachments), len(tt.expected.Attachments))
			}
			for i, att := range result.Attachments {
				if i >= len(tt.expected.Attachments) {
					break
				}
				exp := tt.expected.Attachments[i]
				if att.ID != exp.ID || att.Type != exp.Type || att.URL != exp.URL {
					t.Errorf("Attachment %d mismatch: got %+v, want %+v", i, att, exp)
				}
			}

			// Check metadata
			for key, val := range tt.expected.Metadata {
				if result.Metadata[key] != val {
					t.Errorf("Metadata[%s]: got %v, want %v", key, result.Metadata[key], val)
				}
			}
		})
	}
}

func TestAdapter_Send(t *testing.T) {
	tests := []struct {
		name    string
		message *models.Message
		wantErr bool
	}{
		{
			name: "simple text message",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Hello from test",
				Metadata: map[string]any{
					"discord_channel_id": "channel-123",
				},
			},
			wantErr: false,
		},
		{
			name: "message with metadata for embed",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Check this out",
				Metadata: map[string]any{
					"discord_channel_id": "channel-123",
					"discord_embed_title": "Important",
					"discord_embed_color": 0x00FF00,
				},
			},
			wantErr: false,
		},
		{
			name: "message with reaction",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "React to this",
				Metadata: map[string]any{
					"discord_channel_id":      "channel-123",
					"discord_reaction_emoji":  "👍",
					"discord_reaction_msg_id": "msg-to-react",
				},
			},
			wantErr: false,
		},
		{
			name: "message to create thread",
			message: &models.Message{
				Channel:   models.ChannelDiscord,
				ChannelID: "channel-123",
				Content:   "Thread starter",
				Metadata: map[string]any{
					"discord_channel_id":   "channel-123",
					"discord_create_thread": true,
					"discord_thread_name":   "Discussion",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewAdapter("test-token")
			mock := &mockDiscordSession{}
			adapter.session = mock
			adapter.status.Connected = true

			ctx := context.Background()
			err := adapter.Send(ctx, tt.message)

			if (err != nil) != tt.wantErr {
				t.Errorf("Send() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdapter_SlashCommandHandling(t *testing.T) {
	adapter := NewAdapter("test-token")
	mock := &mockDiscordSession{}
	adapter.session = mock

	// Register slash commands
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "help",
			Description: "Show help information",
		},
		{
			Name:        "ask",
			Description: "Ask the AI a question",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "question",
					Description: "Your question",
					Required:    true,
				},
			},
		},
	}

	err := adapter.RegisterSlashCommands(commands, "guild-123")
	if err != nil {
		t.Fatalf("RegisterSlashCommands failed: %v", err)
	}
}

func TestReconnectionBackoff(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
		maxWait  time.Duration
	}{
		{attempt: 0, expected: 1 * time.Second, maxWait: 60 * time.Second},
		{attempt: 1, expected: 2 * time.Second, maxWait: 60 * time.Second},
		{attempt: 2, expected: 4 * time.Second, maxWait: 60 * time.Second},
		{attempt: 3, expected: 8 * time.Second, maxWait: 60 * time.Second},
		{attempt: 10, expected: 60 * time.Second, maxWait: 60 * time.Second}, // Should cap at max
	}

	for _, tt := range tests {
		got := calculateBackoff(tt.attempt, tt.maxWait)
		if got != tt.expected {
			t.Errorf("calculateBackoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestDetectAttachmentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"image/gif", "image"},
		{"audio/mpeg", "audio"},
		{"audio/wav", "audio"},
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"application/pdf", "document"},
		{"text/plain", "document"},
		{"unknown/type", "document"},
	}

	for _, tt := range tests {
		got := detectAttachmentType(tt.contentType)
		if got != tt.expected {
			t.Errorf("detectAttachmentType(%s) = %s, want %s", tt.contentType, got, tt.expected)
		}
	}
}
