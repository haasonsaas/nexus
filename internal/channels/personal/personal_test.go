package personal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("empty config is valid", func(t *testing.T) {
		cfg := &Config{}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v, expected nil", err)
		}
	})

	t.Run("full config is valid", func(t *testing.T) {
		cfg := &Config{
			SessionPath: "/tmp/sessions",
			MediaPath:   "/tmp/media",
			SyncOnStart: true,
			Presence: PresenceConfig{
				SendReadReceipts: true,
				SendTyping:       true,
				BroadcastOnline:  false,
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v, expected nil", err)
		}
	})
}

func TestNewBaseAdapter(t *testing.T) {
	t.Run("creates adapter with nil config and logger", func(t *testing.T) {
		adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
		if adapter.Type() != models.ChannelWhatsApp {
			t.Errorf("Type() = %v, want %v", adapter.Type(), models.ChannelWhatsApp)
		}
		if adapter.logger == nil {
			t.Error("expected logger to default")
		}
		if adapter.config == nil {
			t.Error("expected config to default")
		}
	})

	t.Run("uses provided config and logger", func(t *testing.T) {
		cfg := &Config{SessionPath: "/custom"}
		adapter := NewBaseAdapter(models.ChannelSignal, cfg, nil)
		if adapter.Config().SessionPath != "/custom" {
			t.Errorf("Config().SessionPath = %q, want %q", adapter.Config().SessionPath, "/custom")
		}
	})
}

func TestBaseAdapter_Type(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelTelegram, nil, nil)
	if adapter.Type() != models.ChannelTelegram {
		t.Errorf("Type() = %v, want %v", adapter.Type(), models.ChannelTelegram)
	}
}

func TestBaseAdapter_Messages(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)
	ch := adapter.Messages()
	if ch == nil {
		t.Error("Messages() should return non-nil channel")
	}
}

func TestBaseAdapter_Status(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	status := adapter.Status()
	if status.Connected {
		t.Error("expected Connected = false initially")
	}

	adapter.SetStatus(true, "")
	status = adapter.Status()
	if !status.Connected {
		t.Error("expected Connected = true after SetStatus")
	}
	if status.LastPing == 0 {
		t.Error("expected LastPing to be set")
	}

	adapter.SetStatus(false, "connection lost")
	status = adapter.Status()
	if status.Connected {
		t.Error("expected Connected = false after SetStatus(false)")
	}
	if status.Error != "connection lost" {
		t.Errorf("Error = %q, want %q", status.Error, "connection lost")
	}
}

func TestBaseAdapter_Metrics(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	metrics := adapter.Metrics()
	if metrics.ChannelType != models.ChannelWhatsApp {
		t.Errorf("ChannelType = %v, want %v", metrics.ChannelType, models.ChannelWhatsApp)
	}
	if metrics.MessagesSent != 0 {
		t.Errorf("MessagesSent = %d, want 0", metrics.MessagesSent)
	}

	adapter.IncrementSent()
	adapter.IncrementSent()
	adapter.IncrementReceived()
	adapter.IncrementErrors()

	metrics = adapter.Metrics()
	if metrics.MessagesSent != 2 {
		t.Errorf("MessagesSent = %d, want 2", metrics.MessagesSent)
	}
	if metrics.MessagesReceived != 1 {
		t.Errorf("MessagesReceived = %d, want 1", metrics.MessagesReceived)
	}
	if metrics.MessagesFailed != 1 {
		t.Errorf("MessagesFailed = %d, want 1", metrics.MessagesFailed)
	}
}

func TestBaseAdapter_Logger(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)
	if adapter.Logger() == nil {
		t.Error("Logger() should return non-nil logger")
	}
}

func TestBaseAdapter_NormalizeInbound(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	t.Run("normalizes basic DM", func(t *testing.T) {
		now := time.Now()
		raw := RawMessage{
			ID:        "msg-1",
			Content:   "Hello",
			PeerID:    "peer-123",
			PeerName:  "John",
			Timestamp: now,
		}

		msg := adapter.NormalizeInbound(raw)
		if msg.ID != "msg-1" {
			t.Errorf("ID = %q, want %q", msg.ID, "msg-1")
		}
		if msg.Channel != models.ChannelWhatsApp {
			t.Errorf("Channel = %v, want %v", msg.Channel, models.ChannelWhatsApp)
		}
		if msg.Direction != models.DirectionInbound {
			t.Errorf("Direction = %v, want %v", msg.Direction, models.DirectionInbound)
		}
		if msg.Content != "Hello" {
			t.Errorf("Content = %q, want %q", msg.Content, "Hello")
		}
		if msg.Metadata["peer_id"] != "peer-123" {
			t.Errorf("Metadata[peer_id] = %v, want %q", msg.Metadata["peer_id"], "peer-123")
		}
		if msg.Metadata["conversation_type"] != "dm" {
			t.Errorf("Metadata[conversation_type] = %v, want %q", msg.Metadata["conversation_type"], "dm")
		}
	})

	t.Run("normalizes group message", func(t *testing.T) {
		raw := RawMessage{
			ID:        "msg-2",
			Content:   "Hi group",
			PeerID:    "peer-456",
			PeerName:  "Jane",
			GroupID:   "group-1",
			GroupName: "Friends",
			Timestamp: time.Now(),
		}

		msg := adapter.NormalizeInbound(raw)
		if msg.Metadata["group_id"] != "group-1" {
			t.Errorf("Metadata[group_id] = %v, want %q", msg.Metadata["group_id"], "group-1")
		}
		if msg.Metadata["group_name"] != "Friends" {
			t.Errorf("Metadata[group_name] = %v, want %q", msg.Metadata["group_name"], "Friends")
		}
		if msg.Metadata["conversation_type"] != "group" {
			t.Errorf("Metadata[conversation_type] = %v, want %q", msg.Metadata["conversation_type"], "group")
		}
	})

	t.Run("includes reply_to", func(t *testing.T) {
		raw := RawMessage{
			ID:        "msg-3",
			Content:   "Reply",
			PeerID:    "peer-789",
			ReplyTo:   "original-msg",
			Timestamp: time.Now(),
		}

		msg := adapter.NormalizeInbound(raw)
		if msg.Metadata["reply_to"] != "original-msg" {
			t.Errorf("Metadata[reply_to] = %v, want %q", msg.Metadata["reply_to"], "original-msg")
		}
	})

	t.Run("copies extra metadata", func(t *testing.T) {
		raw := RawMessage{
			ID:        "msg-4",
			Content:   "With extra",
			PeerID:    "peer-000",
			Timestamp: time.Now(),
			Extra: map[string]any{
				"custom_field": "custom_value",
			},
		}

		msg := adapter.NormalizeInbound(raw)
		if msg.Metadata["custom_field"] != "custom_value" {
			t.Errorf("Metadata[custom_field] = %v, want %q", msg.Metadata["custom_field"], "custom_value")
		}
	})
}

func TestBaseAdapter_ProcessAttachments(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	raw := RawMessage{
		ID:      "msg-1",
		Content: "With attachments",
		Attachments: []RawAttachment{
			{
				ID:       "att-1",
				MIMEType: "image/png",
				Filename: "photo.png",
				Size:     1024,
				URL:      "https://example.com/photo.png",
			},
			{
				ID:       "att-2",
				MIMEType: "application/pdf",
				Filename: "doc.pdf",
				Size:     2048,
			},
		},
	}

	msg := &models.Message{}
	adapter.ProcessAttachments(raw, msg)

	if len(msg.Attachments) != 2 {
		t.Fatalf("len(Attachments) = %d, want 2", len(msg.Attachments))
	}

	if msg.Attachments[0].ID != "att-1" {
		t.Errorf("Attachments[0].ID = %q, want %q", msg.Attachments[0].ID, "att-1")
	}
	if msg.Attachments[0].Filename != "photo.png" {
		t.Errorf("Attachments[0].Filename = %q, want %q", msg.Attachments[0].Filename, "photo.png")
	}
	if msg.Attachments[0].Size != 1024 {
		t.Errorf("Attachments[0].Size = %d, want %d", msg.Attachments[0].Size, 1024)
	}
}

func TestBaseAdapter_Emit(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	msg := &models.Message{ID: "msg-1", Content: "Test"}
	if !adapter.Emit(msg) {
		t.Error("Emit() should return true for non-full channel")
	}

	metrics := adapter.Metrics()
	if metrics.MessagesReceived != 1 {
		t.Errorf("MessagesReceived = %d, want 1 after Emit", metrics.MessagesReceived)
	}

	// Receive the emitted message
	select {
	case received := <-adapter.Messages():
		if received.ID != "msg-1" {
			t.Errorf("received ID = %q, want %q", received.ID, "msg-1")
		}
	default:
		t.Error("expected message in channel")
	}
}

func TestBaseAdapter_Contacts(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	// Initially no contact
	_, ok := adapter.GetContact("user-1")
	if ok {
		t.Error("expected no contact initially")
	}

	// Set contact
	contact := &Contact{
		ID:    "user-1",
		Name:  "John Doe",
		Phone: "+1234567890",
	}
	adapter.SetContact(contact)

	// Get contact
	retrieved, ok := adapter.GetContact("user-1")
	if !ok {
		t.Error("expected contact after SetContact")
	}
	if retrieved.Name != "John Doe" {
		t.Errorf("Name = %q, want %q", retrieved.Name, "John Doe")
	}

	// Nil contact is ignored
	adapter.SetContact(nil)
	adapter.SetContact(&Contact{}) // Empty ID is ignored

	// Only one contact should exist
	_, ok = adapter.GetContact("")
	if ok {
		t.Error("expected no contact with empty ID")
	}
}

func TestBaseAdapter_Close(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)

	// Close should not panic
	adapter.Close()

	// Channel should be closed
	_, ok := <-adapter.messages
	if ok {
		t.Error("channel should be closed")
	}
}

func TestContact_Struct(t *testing.T) {
	contact := Contact{
		ID:       "c-1",
		Name:     "Jane",
		Phone:    "+1987654321",
		Email:    "jane@example.com",
		Avatar:   "https://example.com/avatar.jpg",
		Verified: true,
		Extra:    map[string]any{"status": "active"},
	}

	if contact.ID != "c-1" {
		t.Errorf("ID = %q, want %q", contact.ID, "c-1")
	}
	if contact.Verified != true {
		t.Error("Verified should be true")
	}
}

func TestConversation_Struct(t *testing.T) {
	now := time.Now()
	conv := Conversation{
		ID:          "conv-1",
		Type:        ConversationGroup,
		Name:        "Team Chat",
		UnreadCount: 5,
		Muted:       false,
		Pinned:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if conv.ID != "conv-1" {
		t.Errorf("ID = %q, want %q", conv.ID, "conv-1")
	}
	if conv.Type != ConversationGroup {
		t.Errorf("Type = %v, want %v", conv.Type, ConversationGroup)
	}
}

func TestConversationType_Constants(t *testing.T) {
	if ConversationDM != "dm" {
		t.Errorf("ConversationDM = %q, want %q", ConversationDM, "dm")
	}
	if ConversationGroup != "group" {
		t.Errorf("ConversationGroup = %q, want %q", ConversationGroup, "group")
	}
}

func TestPresenceType_Constants(t *testing.T) {
	if PresenceOnline != "online" {
		t.Errorf("PresenceOnline = %q, want %q", PresenceOnline, "online")
	}
	if PresenceOffline != "offline" {
		t.Errorf("PresenceOffline = %q, want %q", PresenceOffline, "offline")
	}
	if PresenceTyping != "typing" {
		t.Errorf("PresenceTyping = %q, want %q", PresenceTyping, "typing")
	}
	if PresenceStoppedTyping != "stopped_typing" {
		t.Errorf("PresenceStoppedTyping = %q, want %q", PresenceStoppedTyping, "stopped_typing")
	}
}

func TestPresenceEvent_Struct(t *testing.T) {
	now := time.Now()
	event := PresenceEvent{
		PeerID:    "peer-1",
		Type:      PresenceTyping,
		Timestamp: now,
	}

	if event.PeerID != "peer-1" {
		t.Errorf("PeerID = %q, want %q", event.PeerID, "peer-1")
	}
	if event.Type != PresenceTyping {
		t.Errorf("Type = %v, want %v", event.Type, PresenceTyping)
	}
}

func TestRawMessage_Struct(t *testing.T) {
	raw := RawMessage{
		ID:        "raw-1",
		Content:   "Test message",
		PeerID:    "peer-1",
		PeerName:  "John",
		GroupID:   "group-1",
		GroupName: "Friends",
		ReplyTo:   "original-1",
		Extra:     map[string]any{"key": "value"},
	}

	if raw.ID != "raw-1" {
		t.Errorf("ID = %q, want %q", raw.ID, "raw-1")
	}
	if raw.Content != "Test message" {
		t.Errorf("Content = %q, want %q", raw.Content, "Test message")
	}
}

func TestRawAttachment_Struct(t *testing.T) {
	att := RawAttachment{
		ID:       "att-1",
		MIMEType: "image/jpeg",
		Filename: "image.jpg",
		Size:     4096,
		URL:      "https://example.com/image.jpg",
		Data:     []byte("image data"),
	}

	if att.ID != "att-1" {
		t.Errorf("ID = %q, want %q", att.ID, "att-1")
	}
	if att.Size != 4096 {
		t.Errorf("Size = %d, want %d", att.Size, 4096)
	}
}

func TestListOptions_Struct(t *testing.T) {
	opts := ListOptions{
		Limit:   10,
		Offset:  20,
		Unread:  true,
		GroupID: "group-1",
	}

	if opts.Limit != 10 {
		t.Errorf("Limit = %d, want %d", opts.Limit, 10)
	}
	if opts.Offset != 20 {
		t.Errorf("Offset = %d, want %d", opts.Offset, 20)
	}
}

func TestBaseContactManager(t *testing.T) {
	adapter := NewBaseAdapter(models.ChannelWhatsApp, nil, nil)
	manager := NewBaseContactManager(adapter)

	ctx := context.Background()

	t.Run("Resolve returns nil for unknown", func(t *testing.T) {
		contact, err := manager.Resolve(ctx, "unknown")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if contact != nil {
			t.Error("expected nil contact for unknown")
		}
	})

	t.Run("Resolve returns cached contact", func(t *testing.T) {
		adapter.SetContact(&Contact{ID: "cached-1", Name: "Cached"})
		contact, err := manager.Resolve(ctx, "cached-1")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if contact == nil || contact.Name != "Cached" {
			t.Error("expected cached contact")
		}
	})

	t.Run("Search returns cached matches", func(t *testing.T) {
		adapter.SetContact(&Contact{ID: "alice-1", Name: "Alice"})
		results, err := manager.Search(ctx, "ali")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(results) != 1 || results[0].ID != "alice-1" {
			t.Errorf("expected alice result, got: %v", results)
		}
	})

	t.Run("Search with empty query returns all cached", func(t *testing.T) {
		results, err := manager.Search(ctx, " ")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(results) < 2 {
			t.Errorf("expected cached contacts, got: %v", results)
		}
	})

	t.Run("Sync returns not supported", func(t *testing.T) {
		if err := manager.Sync(ctx); !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
	})

	t.Run("GetByID returns cached contact", func(t *testing.T) {
		contact, err := manager.GetByID(ctx, "cached-1")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if contact == nil {
			t.Error("expected cached contact")
		}
	})
}

func TestBaseMediaHandler(t *testing.T) {
	handler := &BaseMediaHandler{}
	ctx := context.Background()

	t.Run("Download returns not supported", func(t *testing.T) {
		data, mime, err := handler.Download(ctx, "media-1")
		if !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
		if data != nil || mime != "" {
			t.Error("expected nil data and empty mime from stub")
		}
	})

	t.Run("Upload returns not supported", func(t *testing.T) {
		id, err := handler.Upload(ctx, []byte("data"), "image/png", "file.png")
		if !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
		if id != "" {
			t.Error("expected empty ID from stub")
		}
	})

	t.Run("GetURL returns not supported", func(t *testing.T) {
		url, err := handler.GetURL(ctx, "media-1")
		if !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
		if url != "" {
			t.Error("expected empty URL from stub")
		}
	})
}

func TestBasePresenceManager(t *testing.T) {
	pm := &BasePresenceManager{}
	ctx := context.Background()

	t.Run("SetTyping returns not supported", func(t *testing.T) {
		if err := pm.SetTyping(ctx, "peer-1", true); !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
	})

	t.Run("SetOnline returns not supported", func(t *testing.T) {
		if err := pm.SetOnline(ctx, true); !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
	})

	t.Run("Subscribe returns not supported", func(t *testing.T) {
		ch, err := pm.Subscribe(ctx, "peer-1")
		if !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
		if ch != nil {
			t.Error("expected nil channel from stub")
		}
	})

	t.Run("MarkRead returns not supported", func(t *testing.T) {
		if err := pm.MarkRead(ctx, "peer-1", "msg-1"); !errors.Is(err, channels.ErrNotSupported) {
			t.Errorf("expected not supported error, got: %v", err)
		}
	})
}
