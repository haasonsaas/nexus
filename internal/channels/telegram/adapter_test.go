package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestAdapter_Type(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if got := adapter.Type(); got != models.ChannelTelegram {
		t.Errorf("Type() = %v, want %v", got, models.ChannelTelegram)
	}
}

func TestAdapter_Status(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	// Initially not connected
	status := adapter.Status()
	if status.Connected {
		t.Error("Status().Connected = true, want false")
	}
}

func TestAdapter_Messages(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	msgChan := adapter.Messages()
	if msgChan == nil {
		t.Error("Messages() returned nil channel")
	}
}

func TestConvertTelegramMessage_TextMessage(t *testing.T) {
	tests := []struct {
		name     string
		teleMsg  *mockTelegramMessage
		wantText string
		wantRole models.Role
	}{
		{
			name: "simple text message",
			teleMsg: &mockTelegramMessage{
				messageID: 123,
				chatID:    456789,
				text:      "Hello, world!",
				fromID:    111,
				fromFirst: "John",
				fromLast:  "Doe",
				date:      time.Now().Unix(),
			},
			wantText: "Hello, world!",
			wantRole: models.RoleUser,
		},
		{
			name: "empty text message",
			teleMsg: &mockTelegramMessage{
				messageID: 124,
				chatID:    456789,
				text:      "",
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
			},
			wantText: "",
			wantRole: models.RoleUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTelegramMessage(tt.teleMsg)

			if got.Content != tt.wantText {
				t.Errorf("Content = %q, want %q", got.Content, tt.wantText)
			}

			if got.Role != tt.wantRole {
				t.Errorf("Role = %v, want %v", got.Role, tt.wantRole)
			}

			if got.Channel != models.ChannelTelegram {
				t.Errorf("Channel = %v, want %v", got.Channel, models.ChannelTelegram)
			}

			if got.Direction != models.DirectionInbound {
				t.Errorf("Direction = %v, want %v", got.Direction, models.DirectionInbound)
			}
		})
	}
}

func TestConvertTelegramMessage_WithAttachments(t *testing.T) {
	tests := []struct {
		name           string
		teleMsg        *mockTelegramMessage
		wantAttachType string
	}{
		{
			name: "photo attachment",
			teleMsg: &mockTelegramMessage{
				messageID: 125,
				chatID:    456789,
				text:      "Check this photo",
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
				hasPhoto:  true,
				photoID:   "photo123",
			},
			wantAttachType: "image",
		},
		{
			name: "document attachment",
			teleMsg: &mockTelegramMessage{
				messageID:  126,
				chatID:     456789,
				text:       "Here's a document",
				fromID:     111,
				fromFirst:  "John",
				date:       time.Now().Unix(),
				hasDoc:     true,
				docID:      "doc123",
				docName:    "report.pdf",
				docMime:    "application/pdf",
			},
			wantAttachType: "document",
		},
		{
			name: "audio attachment",
			teleMsg: &mockTelegramMessage{
				messageID: 127,
				chatID:    456789,
				fromID:    111,
				fromFirst: "John",
				date:      time.Now().Unix(),
				hasAudio:  true,
				audioID:   "audio123",
			},
			wantAttachType: "audio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTelegramMessage(tt.teleMsg)

			if len(got.Attachments) == 0 {
				t.Error("Expected attachments, got none")
				return
			}

			if got.Attachments[0].Type != tt.wantAttachType {
				t.Errorf("Attachment type = %v, want %v", got.Attachments[0].Type, tt.wantAttachType)
			}
		})
	}
}

func TestAdapter_Lifecycle(t *testing.T) {
	cfg := Config{
		Token: "test-token",
		Mode:  ModeLongPolling,
	}

	adapter, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start should not block
	errChan := make(chan error, 1)
	go func() {
		errChan <- adapter.Start(ctx)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop should work
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()

	if err := adapter.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Wait for start to complete
	select {
	case <-errChan:
		// Expected to complete after stop
	case <-time.After(3 * time.Second):
		t.Error("Start() did not return after Stop()")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid long polling config",
			cfg: Config{
				Token: "valid-token",
				Mode:  ModeLongPolling,
			},
			wantErr: false,
		},
		{
			name: "valid webhook config",
			cfg: Config{
				Token:      "valid-token",
				Mode:       ModeWebhook,
				WebhookURL: "https://example.com/webhook",
			},
			wantErr: false,
		},
		{
			name: "missing token",
			cfg: Config{
				Mode: ModeLongPolling,
			},
			wantErr: true,
		},
		{
			name: "webhook without URL",
			cfg: Config{
				Token: "valid-token",
				Mode:  ModeWebhook,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// mockTelegramMessage simulates a Telegram message for testing
type mockTelegramMessage struct {
	messageID int64
	chatID    int64
	text      string
	fromID    int64
	fromFirst string
	fromLast  string
	date      int64

	// Attachments
	hasPhoto bool
	photoID  string

	hasDoc  bool
	docID   string
	docName string
	docMime string

	hasAudio bool
	audioID  string
}

func (m *mockTelegramMessage) GetMessageID() int64 {
	return m.messageID
}

func (m *mockTelegramMessage) GetChatID() int64 {
	return m.chatID
}

func (m *mockTelegramMessage) GetText() string {
	return m.text
}

func (m *mockTelegramMessage) GetFrom() userInterface {
	return &mockUser{
		id:        m.fromID,
		firstName: m.fromFirst,
		lastName:  m.fromLast,
	}
}

func (m *mockTelegramMessage) GetDate() int64 {
	return m.date
}

func (m *mockTelegramMessage) HasPhoto() bool {
	return m.hasPhoto
}

func (m *mockTelegramMessage) GetPhotoID() string {
	return m.photoID
}

func (m *mockTelegramMessage) HasDocument() bool {
	return m.hasDoc
}

func (m *mockTelegramMessage) GetDocumentID() string {
	return m.docID
}

func (m *mockTelegramMessage) GetDocumentName() string {
	return m.docName
}

func (m *mockTelegramMessage) GetDocumentMimeType() string {
	return m.docMime
}

func (m *mockTelegramMessage) HasAudio() bool {
	return m.hasAudio
}

func (m *mockTelegramMessage) GetAudioID() string {
	return m.audioID
}

type mockUser struct {
	id        int64
	firstName string
	lastName  string
}

func (u mockUser) GetID() int64 {
	return u.id
}

func (u mockUser) GetFirstName() string {
	return u.firstName
}

func (u mockUser) GetLastName() string {
	return u.lastName
}
