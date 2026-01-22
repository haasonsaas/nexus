package gateway

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMessageNormalizer_Normalize(t *testing.T) {
	n := NewMessageNormalizer()

	t.Run("sets defaults", func(t *testing.T) {
		msg := &models.Message{
			ID:      "test-1",
			Channel: models.ChannelAPI,
		}

		n.Normalize(msg)

		if msg.Direction != models.DirectionInbound {
			t.Errorf("expected DirectionInbound, got %s", msg.Direction)
		}
		if msg.Role != models.RoleUser {
			t.Errorf("expected RoleUser, got %s", msg.Role)
		}
		if msg.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
		if msg.Metadata == nil {
			t.Error("expected Metadata to be initialized")
		}
		if _, ok := msg.Metadata[MetaNormalized]; !ok {
			t.Error("expected MetaNormalized to be set")
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		msg := &models.Message{
			ID:      "test-2",
			Channel: models.ChannelAPI,
		}

		n.Normalize(msg)
		firstNormalized := msg.Metadata[MetaNormalizedAt]

		time.Sleep(time.Millisecond)
		n.Normalize(msg)

		if msg.Metadata[MetaNormalizedAt] != firstNormalized {
			t.Error("normalization should be idempotent")
		}
	})

	t.Run("preserves existing values", func(t *testing.T) {
		createdAt := time.Now().Add(-time.Hour)
		msg := &models.Message{
			ID:        "test-3",
			Channel:   models.ChannelAPI,
			Direction: models.DirectionOutbound,
			Role:      models.RoleAssistant,
			CreatedAt: createdAt,
		}

		n.Normalize(msg)

		if msg.Direction != models.DirectionOutbound {
			t.Errorf("expected DirectionOutbound to be preserved, got %s", msg.Direction)
		}
		if msg.Role != models.RoleAssistant {
			t.Errorf("expected RoleAssistant to be preserved, got %s", msg.Role)
		}
		if !msg.CreatedAt.Equal(createdAt) {
			t.Error("expected CreatedAt to be preserved")
		}
	})
}

func TestMessageNormalizer_NormalizeTelegram(t *testing.T) {
	n := NewMessageNormalizer()

	msg := &models.Message{
		ID:      "tg-1",
		Channel: models.ChannelTelegram,
		Metadata: map[string]any{
			"chat_id":    int64(12345),
			"user_id":    int64(67890),
			"user_first": "John",
			"user_last":  "Doe",
		},
	}

	n.Normalize(msg)

	if msg.Metadata[MetaChatID] != "12345" {
		t.Errorf("expected chat_id to be normalized, got %v", msg.Metadata[MetaChatID])
	}
	if msg.Metadata[MetaUserID] != "67890" {
		t.Errorf("expected user_id to be normalized, got %v", msg.Metadata[MetaUserID])
	}
	if msg.Metadata[MetaUserName] != "John Doe" {
		t.Errorf("expected user_name to be set, got %v", msg.Metadata[MetaUserName])
	}
	if msg.Metadata[MetaPeerID] != "12345" {
		t.Errorf("expected peer_id to be set from chat_id, got %v", msg.Metadata[MetaPeerID])
	}

	// Check original preserved
	if msg.Metadata[MetaOriginalPrefix+"chat_id"] != int64(12345) {
		t.Error("expected original chat_id to be preserved")
	}
}

func TestMessageNormalizer_NormalizeSlack(t *testing.T) {
	n := NewMessageNormalizer()

	msg := &models.Message{
		ID:      "slack-1",
		Channel: models.ChannelSlack,
		Metadata: map[string]any{
			"slack_user_id":   "U12345",
			"slack_channel":   "C67890",
			"slack_thread_ts": "1234567890.123456",
		},
	}

	n.Normalize(msg)

	if msg.Metadata[MetaUserID] != "U12345" {
		t.Errorf("expected user_id to be normalized, got %v", msg.Metadata[MetaUserID])
	}
	if msg.Metadata[MetaChatID] != "C67890" {
		t.Errorf("expected chat_id to be normalized, got %v", msg.Metadata[MetaChatID])
	}
	if msg.Metadata[MetaThreadID] != "1234567890.123456" {
		t.Errorf("expected thread_id to be normalized, got %v", msg.Metadata[MetaThreadID])
	}
}

func TestMessageNormalizer_NormalizeDiscord(t *testing.T) {
	n := NewMessageNormalizer()

	msg := &models.Message{
		ID:      "discord-1",
		Channel: models.ChannelDiscord,
		Metadata: map[string]any{
			"discord_user_id":    "123456789",
			"discord_username":   "testuser",
			"discord_channel_id": "987654321",
			"discord_thread_id":  "111222333",
		},
	}

	n.Normalize(msg)

	if msg.Metadata[MetaUserID] != "123456789" {
		t.Errorf("expected user_id to be normalized, got %v", msg.Metadata[MetaUserID])
	}
	if msg.Metadata[MetaUserName] != "testuser" {
		t.Errorf("expected user_name to be normalized, got %v", msg.Metadata[MetaUserName])
	}
	if msg.Metadata[MetaChatID] != "987654321" {
		t.Errorf("expected chat_id to be normalized, got %v", msg.Metadata[MetaChatID])
	}
	if msg.Metadata[MetaThreadID] != "111222333" {
		t.Errorf("expected thread_id to be normalized, got %v", msg.Metadata[MetaThreadID])
	}
}

func TestMessageNormalizer_NormalizePersonal(t *testing.T) {
	n := NewMessageNormalizer()

	t.Run("direct message", func(t *testing.T) {
		msg := &models.Message{
			ID:      "wa-1",
			Channel: models.ChannelWhatsApp,
			Metadata: map[string]any{
				"peer_id":   "+15551234567",
				"peer_name": "Alice",
			},
		}

		n.Normalize(msg)

		if msg.Metadata[MetaUserID] != "+15551234567" {
			t.Errorf("expected user_id from peer_id, got %v", msg.Metadata[MetaUserID])
		}
		if msg.Metadata[MetaUserName] != "Alice" {
			t.Errorf("expected user_name from peer_name, got %v", msg.Metadata[MetaUserName])
		}
		if msg.Metadata[MetaIsGroup] != false {
			t.Errorf("expected is_group to be false, got %v", msg.Metadata[MetaIsGroup])
		}
	})

	t.Run("group message", func(t *testing.T) {
		msg := &models.Message{
			ID:      "wa-2",
			Channel: models.ChannelWhatsApp,
			Metadata: map[string]any{
				"peer_id":    "+15551234567",
				"peer_name":  "Alice",
				"group_id":   "group-123",
				"group_name": "Family Chat",
			},
		}

		n.Normalize(msg)

		if msg.Metadata[MetaIsGroup] != true {
			t.Errorf("expected is_group to be true, got %v", msg.Metadata[MetaIsGroup])
		}
		if msg.Metadata[MetaGroupID] != "group-123" {
			t.Errorf("expected group_id to be set, got %v", msg.Metadata[MetaGroupID])
		}
		if msg.Metadata[MetaGroupName] != "Family Chat" {
			t.Errorf("expected group_name to be set, got %v", msg.Metadata[MetaGroupName])
		}
	})
}

func TestMessageNormalizer_NormalizeAttachments(t *testing.T) {
	n := NewMessageNormalizer()

	msg := &models.Message{
		ID:      "att-1",
		Channel: models.ChannelAPI,
		Attachments: []models.Attachment{
			{ID: "1", MimeType: "image/jpeg"},
			{ID: "2", MimeType: "audio/mp3"},
			{ID: "3", MimeType: "video/mp4"},
			{ID: "4", MimeType: "application/pdf"},
			{ID: "5", Filename: "doc.xlsx"},
			{ID: "6"}, // No info, should default to document
		},
	}

	n.Normalize(msg)

	expected := []string{"image", "audio", "video", "document", "spreadsheet", "document"}
	for i, att := range msg.Attachments {
		if att.Type != expected[i] {
			t.Errorf("attachment %d: expected type %s, got %s", i, expected[i], att.Type)
		}
	}
}

func TestMessageNormalizer_PreserveOriginal(t *testing.T) {
	t.Run("preserve enabled", func(t *testing.T) {
		n := NewMessageNormalizer(WithPreserveOriginal(true))

		msg := &models.Message{
			ID:      "test-1",
			Channel: models.ChannelSlack,
			Metadata: map[string]any{
				"slack_user_id": "U12345",
			},
		}

		n.Normalize(msg)

		if msg.Metadata[MetaOriginalPrefix+"slack_user_id"] != "U12345" {
			t.Error("expected original slack_user_id to be preserved")
		}
	})

	t.Run("preserve disabled", func(t *testing.T) {
		n := NewMessageNormalizer(WithPreserveOriginal(false))

		msg := &models.Message{
			ID:      "test-2",
			Channel: models.ChannelSlack,
			Metadata: map[string]any{
				"slack_user_id": "U12345",
			},
		}

		n.Normalize(msg)

		if _, ok := msg.Metadata[MetaOriginalPrefix+"slack_user_id"]; ok {
			t.Error("expected original slack_user_id NOT to be preserved")
		}
	})
}

func TestDeriveSessionID(t *testing.T) {
	tests := []struct {
		name     string
		channel  models.ChannelType
		chatID   string
		threadID string
	}{
		{
			name:     "basic",
			channel:  models.ChannelSlack,
			chatID:   "C12345",
			threadID: "",
		},
		{
			name:     "with thread",
			channel:  models.ChannelSlack,
			chatID:   "C12345",
			threadID: "1234567890.123456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := DeriveSessionID(tt.channel, tt.chatID, tt.threadID)
			id2 := DeriveSessionID(tt.channel, tt.chatID, tt.threadID)

			// Should be deterministic
			if id1 != id2 {
				t.Error("session ID should be deterministic")
			}

			// Should be 32 hex chars
			if len(id1) != 32 {
				t.Errorf("expected 32 char ID, got %d", len(id1))
			}
		})
	}

	// Different inputs should produce different IDs
	id1 := DeriveSessionID(models.ChannelSlack, "C12345", "")
	id2 := DeriveSessionID(models.ChannelSlack, "C12345", "thread1")
	id3 := DeriveSessionID(models.ChannelDiscord, "C12345", "")

	if id1 == id2 {
		t.Error("different thread should produce different ID")
	}
	if id1 == id3 {
		t.Error("different channel should produce different ID")
	}
}

func TestExtractSessionKey(t *testing.T) {
	msg := &models.Message{
		Metadata: map[string]any{
			MetaChatID:   "chat-123",
			MetaThreadID: "thread-456",
		},
	}

	chatID, threadID := ExtractSessionKey(msg)

	if chatID != "chat-123" {
		t.Errorf("expected chat-123, got %s", chatID)
	}
	if threadID != "thread-456" {
		t.Errorf("expected thread-456, got %s", threadID)
	}
}

func TestExtractUserInfo(t *testing.T) {
	msg := &models.Message{
		Metadata: map[string]any{
			MetaUserID:   "user-123",
			MetaUserName: "John Doe",
		},
	}

	userID, userName := ExtractUserInfo(msg)

	if userID != "user-123" {
		t.Errorf("expected user-123, got %s", userID)
	}
	if userName != "John Doe" {
		t.Errorf("expected John Doe, got %s", userName)
	}
}

func TestIsGroupMessage(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		expected bool
	}{
		{
			name:     "nil metadata",
			metadata: nil,
			expected: false,
		},
		{
			name:     "explicit false",
			metadata: map[string]any{MetaIsGroup: false},
			expected: false,
		},
		{
			name:     "explicit true",
			metadata: map[string]any{MetaIsGroup: true},
			expected: true,
		},
		{
			name:     "has group_id",
			metadata: map[string]any{MetaGroupID: "group-123"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &models.Message{Metadata: tt.metadata}
			if got := IsGroupMessage(msg); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestDetectAttachmentType(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"image/jpeg", "image"},
		{"image/png", "image"},
		{"audio/mp3", "audio"},
		{"audio/wav", "audio"},
		{"video/mp4", "video"},
		{"video/webm", "video"},
		{"text/plain", "text"},
		{"text/html", "text"},
		{"application/pdf", "document"},
		{"application/vnd.ms-excel", "spreadsheet"},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "spreadsheet"},
		{"application/msword", "document"},
		{"application/vnd.ms-powerpoint", "presentation"},
		{"application/octet-stream", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			if got := detectAttachmentType(tt.mimeType); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestDetectTypeFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"photo.jpg", "image"},
		{"photo.PNG", "image"},
		{"song.mp3", "audio"},
		{"movie.mp4", "video"},
		{"notes.txt", "text"},
		{"report.pdf", "document"},
		{"data.xlsx", "spreadsheet"},
		{"slides.pptx", "presentation"},
		{"unknown.xyz", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := detectTypeFromFilename(tt.filename); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}
