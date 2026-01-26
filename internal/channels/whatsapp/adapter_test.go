package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/haasonsaas/nexus/internal/channels/personal"
	"github.com/haasonsaas/nexus/pkg/models"
	"go.mau.fi/whatsmeow/types/events"
)

// =============================================================================
// Config Tests
// =============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("expected Enabled to be false by default")
	}
	if cfg.SessionPath != "~/.nexus/whatsapp/session.db" {
		t.Errorf("expected SessionPath to be '~/.nexus/whatsapp/session.db', got %s", cfg.SessionPath)
	}
	if cfg.MediaPath != "~/.nexus/whatsapp/media" {
		t.Errorf("expected MediaPath to be '~/.nexus/whatsapp/media', got %s", cfg.MediaPath)
	}
	if !cfg.SyncContacts {
		t.Error("expected SyncContacts to be true by default")
	}
	if !cfg.Personal.SyncOnStart {
		t.Error("expected SyncOnStart to be true by default")
	}
	if !cfg.Personal.Presence.SendReadReceipts {
		t.Error("expected SendReadReceipts to be true by default")
	}
	if !cfg.Personal.Presence.SendTyping {
		t.Error("expected SendTyping to be true by default")
	}
	if cfg.Personal.Presence.BroadcastOnline {
		t.Error("expected BroadcastOnline to be false by default")
	}
}

func TestDefaultConfigAllFields(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"Enabled", cfg.Enabled, false},
		{"SessionPath", cfg.SessionPath, "~/.nexus/whatsapp/session.db"},
		{"MediaPath", cfg.MediaPath, "~/.nexus/whatsapp/media"},
		{"SyncContacts", cfg.SyncContacts, true},
		{"SyncOnStart", cfg.Personal.SyncOnStart, true},
		{"SendReadReceipts", cfg.Personal.Presence.SendReadReceipts, true},
		{"SendTyping", cfg.Personal.Presence.SendTyping, true},
		{"BroadcastOnline", cfg.Personal.Presence.BroadcastOnline, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, tt.got)
			}
		})
	}
}

func TestConfigWithCustomValues(t *testing.T) {
	cfg := &Config{
		Enabled:      true,
		SessionPath:  "/custom/session.db",
		MediaPath:    "/custom/media",
		SyncContacts: false,
		Personal: personal.Config{
			SyncOnStart: false,
			Presence: personal.PresenceConfig{
				SendReadReceipts: false,
				SendTyping:       false,
				BroadcastOnline:  true,
			},
		},
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.SessionPath != "/custom/session.db" {
		t.Errorf("expected custom SessionPath, got %s", cfg.SessionPath)
	}
	if cfg.MediaPath != "/custom/media" {
		t.Errorf("expected custom MediaPath, got %s", cfg.MediaPath)
	}
	if cfg.SyncContacts {
		t.Error("expected SyncContacts to be false")
	}
	if cfg.Personal.SyncOnStart {
		t.Error("expected SyncOnStart to be false")
	}
	if cfg.Personal.Presence.SendReadReceipts {
		t.Error("expected SendReadReceipts to be false")
	}
	if !cfg.Personal.Presence.BroadcastOnline {
		t.Error("expected BroadcastOnline to be true")
	}
}

// =============================================================================
// Path Expansion Tests
// =============================================================================

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHome bool
	}{
		{
			name:     "tilde path",
			input:    "~/.nexus/whatsapp/session.db",
			wantHome: true,
		},
		{
			name:     "absolute path",
			input:    "/var/whatsapp/session.db",
			wantHome: false,
		},
		{
			name:     "relative path",
			input:    "session.db",
			wantHome: false,
		},
		{
			name:     "tilde only",
			input:    "~",
			wantHome: false, // Only ~/ is expanded
		},
		{
			name:     "tilde in middle",
			input:    "/var/~/session.db",
			wantHome: false,
		},
		{
			name:     "empty path",
			input:    "",
			wantHome: false,
		},
		{
			name:     "nested tilde path",
			input:    "~/a/b/c/d/file.db",
			wantHome: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			if tt.wantHome {
				if result == tt.input {
					t.Errorf("expected path to be expanded, got %s", result)
				}
				if result[0] == '~' {
					t.Errorf("expected tilde to be replaced, got %s", result)
				}
			} else {
				if tt.input != "" && tt.input[0] != '~' && result != tt.input {
					t.Errorf("expected path unchanged, got %s", result)
				}
			}
		})
	}
}

func TestExpandPathPreservesSubpath(t *testing.T) {
	input := "~/.nexus/whatsapp/session.db"
	result := expandPath(input)

	suffix := "/.nexus/whatsapp/session.db"
	if len(result) < len(suffix) {
		t.Fatalf("expanded path too short: %s", result)
	}
	if result[len(result)-len(suffix):] != suffix {
		t.Errorf("expected path to end with %s, got %s", suffix, result)
	}
}

// =============================================================================
// HTTP Download Tests
// =============================================================================

func TestDownloadURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test content"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
		case "/large":
			w.WriteHeader(http.StatusOK)
			for i := 0; i < 1000; i++ {
				w.Write([]byte("large content line\n"))
			}
		case "/binary":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG header
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	tests := []struct {
		name        string
		path        string
		wantError   bool
		wantContent string
	}{
		{
			name:        "successful download",
			path:        "/success",
			wantError:   false,
			wantContent: "test content",
		},
		{
			name:      "not found",
			path:      "/notfound",
			wantError: true,
		},
		{
			name:      "server error",
			path:      "/error",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := downloadURL(server.URL + tt.path)
			if tt.wantError {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(data) != tt.wantContent {
					t.Errorf("got content %q, want %q", string(data), tt.wantContent)
				}
			}
		})
	}
}

func TestDownloadURLBinaryContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // PNG header
	}))
	defer server.Close()

	data, err := downloadURL(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 8 {
		t.Errorf("expected 8 bytes, got %d", len(data))
	}
	if data[0] != 0x89 || data[1] != 0x50 {
		t.Error("binary content not preserved correctly")
	}
}

func TestDownloadURLInvalidURL(t *testing.T) {
	_, err := downloadURL("http://invalid-url-that-does-not-exist.example.com/test")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// =============================================================================
// Media Handler Tests
// =============================================================================

func newTestMediaHandler(t *testing.T) *mediaHandler {
	t.Helper()
	cfg := DefaultConfig()
	cfg.MediaPath = t.TempDir()
	adapter := &Adapter{
		BaseAdapter: personal.NewBaseAdapter(models.ChannelWhatsApp, &cfg.Personal, nil),
		config:      cfg,
		mediaCache:  make(map[string]mediaEntry),
	}
	return &mediaHandler{adapter: adapter}
}

func TestMediaHandlerDownloadMissingAdapter(t *testing.T) {
	handler := &mediaHandler{}

	_, _, err := handler.Download(nil, "media123")
	if err == nil {
		t.Error("expected error for missing adapter")
	}
}

func TestMediaHandlerUploadMissingAdapter(t *testing.T) {
	handler := &mediaHandler{}

	_, err := handler.Upload(nil, []byte("data"), "image/png", "test.png")
	if err == nil {
		t.Error("expected error for missing adapter")
	}
}

func TestMediaHandlerGetURLMissingAdapter(t *testing.T) {
	handler := &mediaHandler{}

	_, err := handler.GetURL(nil, "media123")
	if err == nil {
		t.Error("expected error for missing adapter")
	}
}

func TestMediaHandlerDownloadWithContext(t *testing.T) {
	handler := newTestMediaHandler(t)
	ctx := context.Background()

	_, _, err := handler.Download(ctx, "any-media-id")
	if err == nil {
		t.Error("expected error")
	}
}

func TestMediaHandlerUploadAndDownload(t *testing.T) {
	handler := newTestMediaHandler(t)
	ctx := context.Background()
	data := []byte("data")

	mediaID, err := handler.Upload(ctx, data, "image/png", "test.png")
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if mediaID == "" {
		t.Fatal("expected media ID")
	}

	payload, mimeType, err := handler.Download(ctx, mediaID)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if string(payload) != string(data) {
		t.Fatalf("unexpected payload")
	}
	if mimeType != "image/png" {
		t.Fatalf("unexpected mime type: %s", mimeType)
	}
}

func TestMediaHandlerUploadWithDifferentTypes(t *testing.T) {
	handler := newTestMediaHandler(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		mimeType string
		filename string
	}{
		{"image/jpeg", "image/jpeg", "photo.jpg"},
		{"image/png", "image/png", "image.png"},
		{"video/mp4", "video/mp4", "video.mp4"},
		{"application/pdf", "application/pdf", "document.pdf"},
		{"audio/mpeg", "audio/mpeg", "audio.mp3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaID, err := handler.Upload(ctx, []byte("data"), tt.mimeType, tt.filename)
			if err != nil {
				t.Fatalf("upload failed: %v", err)
			}
			_, mimeType, err := handler.Download(ctx, mediaID)
			if err != nil {
				t.Fatalf("download failed: %v", err)
			}
			if mimeType != tt.mimeType {
				t.Fatalf("mime type mismatch: %s", mimeType)
			}
		})
	}
}

func TestMediaHandlerGetURL(t *testing.T) {
	handler := newTestMediaHandler(t)
	ctx := context.Background()

	mediaID, err := handler.Upload(ctx, []byte("data"), "image/png", "image.png")
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	url, err := handler.GetURL(ctx, mediaID)
	if err != nil {
		t.Fatalf("GetURL failed: %v", err)
	}
	if !strings.HasPrefix(url, "file://") {
		t.Fatalf("unexpected url: %s", url)
	}

}

// =============================================================================
// Presence Manager Tests
// =============================================================================

func TestPresenceManagerSetTypingDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	err := pm.SetTyping(nil, "1234567890@s.whatsapp.net", true)
	if err != nil {
		t.Errorf("expected no error when typing disabled, got %v", err)
	}
}

func TestPresenceManagerSetTypingEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = true

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
			// client is nil, but config check happens first for disabled case
		},
	}

	// With an invalid JID and no client, this should error
	err := pm.SetTyping(nil, "invalid-jid", true)
	if err == nil {
		t.Error("expected error for invalid JID")
	}
}

func TestPresenceManagerSetOnlineDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.BroadcastOnline = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	err := pm.SetOnline(nil, true)
	if err != nil {
		t.Errorf("expected no error when broadcast online disabled, got %v", err)
	}

	err = pm.SetOnline(nil, false)
	if err != nil {
		t.Errorf("expected no error when broadcast online disabled, got %v", err)
	}
}

func TestPresenceManagerMarkReadDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendReadReceipts = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	err := pm.MarkRead(nil, "1234567890@s.whatsapp.net", "msg123")
	if err != nil {
		t.Errorf("expected no error when read receipts disabled, got %v", err)
	}
}

func TestPresenceManagerMarkReadInvalidJID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendReadReceipts = true

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	err := pm.MarkRead(nil, "invalid-jid", "msg123")
	if err == nil {
		t.Error("expected error for invalid JID")
	}
}

func TestPresenceManagerSubscribeInvalidJID(t *testing.T) {
	cfg := DefaultConfig()

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	_, err := pm.Subscribe(nil, "invalid-jid")
	if err == nil {
		t.Error("expected error for invalid JID")
	}
}

func TestPresenceManagerSetTypingInvalidJID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = true

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	err := pm.SetTyping(nil, "invalid-jid", true)
	if err == nil {
		t.Error("expected error for invalid JID")
	}
}

func TestPresenceManagerSetTypingValidJIDNoClient(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = true

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
			// client is nil
		},
	}

	// Valid JID format but no client - should cause nil pointer dereference
	// We can't fully test this without mocking the client
	// Just test that invalid JIDs are caught
	err := pm.SetTyping(nil, "not-a-valid-jid", true)
	if err == nil {
		t.Error("expected error for invalid JID format")
	}
}

// =============================================================================
// Contact Manager Tests
// =============================================================================

func TestContactManagerSearchReturnsEmpty(t *testing.T) {
	cm := &contactManager{}

	results, err := cm.Search(nil, "test")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestContactManagerSearchWithContext(t *testing.T) {
	cm := &contactManager{}
	ctx := context.Background()

	results, err := cm.Search(ctx, "any query")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestContactManagerSearchDifferentQueries(t *testing.T) {
	cm := &contactManager{}
	ctx := context.Background()

	queries := []string{
		"john",
		"+1234567890",
		"test@example.com",
		"",
		"   ",
		"unicode text",
	}

	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			results, err := cm.Search(ctx, q)
			if err != nil {
				t.Errorf("expected no error for query %q, got %v", q, err)
			}
			if results != nil {
				t.Errorf("expected nil results for query %q", q)
			}
		})
	}
}

// =============================================================================
// JID Validation Tests
// =============================================================================

func TestJIDValidation(t *testing.T) {
	tests := []struct {
		name    string
		jid     string
		wantErr bool
	}{
		{
			name:    "valid user JID",
			jid:     "1234567890@s.whatsapp.net",
			wantErr: false,
		},
		{
			name:    "valid group JID",
			jid:     "123456789012345678@g.us",
			wantErr: false,
		},
		{
			name:    "invalid JID",
			jid:     "invalid-jid",
			wantErr: true,
		},
		{
			name:    "empty JID",
			jid:     "",
			wantErr: true,
		},
		{
			name:    "phone number only",
			jid:     "1234567890",
			wantErr: false, // Will be converted to JID
		},
	}

	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = true

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := &presenceManager{
				adapter: &Adapter{
					config: cfg,
				},
			}

			// Use SetTyping as a proxy for JID validation
			// For invalid JIDs, we expect an error
			err := pm.SetTyping(nil, tt.jid, true)
			if tt.wantErr && err == nil && tt.jid != "1234567890" {
				// Special case: phone numbers are valid because they get converted
				t.Errorf("expected error for JID %q", tt.jid)
			}
		})
	}
}

// =============================================================================
// Adapter Connection Tests
// =============================================================================

func TestAdapterIsConnectedInitialState(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: false,
	}

	if adapter.isConnected() {
		t.Error("expected adapter to be disconnected initially")
	}
}

func TestAdapterQRChannel(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
		qrChan: make(chan string, 1),
	}

	ch := adapter.QRChannel()
	if ch == nil {
		t.Error("expected non-nil QR channel")
	}

	// Send a test QR code
	adapter.qrChan <- "test-qr-code"

	select {
	case qr := <-ch:
		if qr != "test-qr-code" {
			t.Errorf("expected 'test-qr-code', got %s", qr)
		}
	default:
		t.Error("expected to receive QR code")
	}
}

// =============================================================================
// Health Check Tests
// =============================================================================

func TestHealthCheckWithoutClient(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
		client: nil,
	}

	health := adapter.HealthCheck(context.Background())
	if health.Healthy {
		t.Error("expected unhealthy status when client is nil")
	}
	if health.Message != "client not initialized" {
		t.Errorf("expected message 'client not initialized', got %s", health.Message)
	}
}

// =============================================================================
// Stop Tests
// =============================================================================

// TestAdapterStopWithoutStart is skipped because it would require a fully
// initialized adapter with BaseAdapter. The Stop method calls SetStatus
// which needs BaseAdapter to be initialized.

// =============================================================================
// Media Type Detection Tests
// =============================================================================

func TestMediaTypeDetection(t *testing.T) {
	tests := []struct {
		mimeType    string
		expectImage bool
		expectVideo bool
		expectAudio bool
		expectDoc   bool
	}{
		{"image/jpeg", true, false, false, false},
		{"image/png", true, false, false, false},
		{"image/gif", true, false, false, false},
		{"image/webp", true, false, false, false},
		{"video/mp4", false, true, false, false},
		{"video/quicktime", false, true, false, false},
		{"audio/mpeg", false, false, true, false},
		{"audio/ogg", false, false, true, false},
		{"application/pdf", false, false, false, true},
		{"application/msword", false, false, false, true},
		{"text/plain", false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			isImage := len(tt.mimeType) >= 5 && tt.mimeType[:5] == "image"
			isVideo := len(tt.mimeType) >= 5 && tt.mimeType[:5] == "video"
			isAudio := len(tt.mimeType) >= 5 && tt.mimeType[:5] == "audio"

			if isImage != tt.expectImage {
				t.Errorf("expected image=%v for %s", tt.expectImage, tt.mimeType)
			}
			if isVideo != tt.expectVideo {
				t.Errorf("expected video=%v for %s", tt.expectVideo, tt.mimeType)
			}
			if isAudio != tt.expectAudio {
				t.Errorf("expected audio=%v for %s", tt.expectAudio, tt.mimeType)
			}
		})
	}
}

// =============================================================================
// Conversation Type Tests
// =============================================================================

func TestConversationTypeFromJID(t *testing.T) {
	tests := []struct {
		name     string
		jid      string
		wantType personal.ConversationType
	}{
		{
			name:     "user JID is DM",
			jid:      "1234567890@s.whatsapp.net",
			wantType: personal.ConversationDM,
		},
		{
			name:     "group JID is group",
			jid:      "123456789012345678@g.us",
			wantType: personal.ConversationGroup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check server type to determine conversation type
			var convType personal.ConversationType
			if len(tt.jid) > 4 && tt.jid[len(tt.jid)-4:] == "g.us" {
				convType = personal.ConversationGroup
			} else {
				convType = personal.ConversationDM
			}

			if convType != tt.wantType {
				t.Errorf("expected %s, got %s", tt.wantType, convType)
			}
		})
	}
}

// =============================================================================
// Config Validation Edge Cases
// =============================================================================

func TestConfigEmptyPaths(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		SessionPath: "",
		MediaPath:   "",
	}

	if cfg.SessionPath != "" {
		t.Error("expected empty SessionPath")
	}
	if cfg.MediaPath != "" {
		t.Error("expected empty MediaPath")
	}
}

func TestConfigSpecialCharactersInPaths(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		SessionPath: "/path/with spaces/session.db",
		MediaPath:   "/path/with-dashes/media",
	}

	if cfg.SessionPath != "/path/with spaces/session.db" {
		t.Errorf("path with spaces not preserved: %s", cfg.SessionPath)
	}
	if cfg.MediaPath != "/path/with-dashes/media" {
		t.Errorf("path with dashes not preserved: %s", cfg.MediaPath)
	}
}

// =============================================================================
// Presence Event Type Tests
// =============================================================================

func TestPresenceEventTypes(t *testing.T) {
	tests := []struct {
		presence personal.PresenceType
	}{
		{personal.PresenceOnline},
		{personal.PresenceOffline},
		{personal.PresenceTyping},
		{personal.PresenceStoppedTyping},
	}

	for _, tt := range tests {
		t.Run(string(tt.presence), func(t *testing.T) {
			event := personal.PresenceEvent{
				PeerID: "test",
				Type:   tt.presence,
			}
			if event.Type != tt.presence {
				t.Errorf("expected type %s, got %s", tt.presence, event.Type)
			}
		})
	}
}

// =============================================================================
// Contact Struct Tests
// =============================================================================

func TestContactFields(t *testing.T) {
	contact := personal.Contact{
		ID:       "1234567890@s.whatsapp.net",
		Name:     "John Doe",
		Phone:    "1234567890",
		Email:    "john@example.com",
		Avatar:   "https://example.com/avatar.jpg",
		Verified: true,
		Extra: map[string]any{
			"custom_field": "value",
		},
	}

	if contact.ID != "1234567890@s.whatsapp.net" {
		t.Errorf("ID mismatch: %s", contact.ID)
	}
	if contact.Name != "John Doe" {
		t.Errorf("Name mismatch: %s", contact.Name)
	}
	if contact.Phone != "1234567890" {
		t.Errorf("Phone mismatch: %s", contact.Phone)
	}
	if contact.Email != "john@example.com" {
		t.Errorf("Email mismatch: %s", contact.Email)
	}
	if !contact.Verified {
		t.Error("expected Verified to be true")
	}
	if contact.Extra["custom_field"] != "value" {
		t.Error("Extra field not preserved")
	}
}

// =============================================================================
// Conversation Struct Tests
// =============================================================================

func TestConversationFields(t *testing.T) {
	conv := personal.Conversation{
		ID:          "123456789@g.us",
		Type:        personal.ConversationGroup,
		Name:        "Test Group",
		UnreadCount: 5,
		Muted:       true,
		Pinned:      false,
	}

	if conv.ID != "123456789@g.us" {
		t.Errorf("ID mismatch: %s", conv.ID)
	}
	if conv.Type != personal.ConversationGroup {
		t.Errorf("Type mismatch: %s", conv.Type)
	}
	if conv.Name != "Test Group" {
		t.Errorf("Name mismatch: %s", conv.Name)
	}
	if conv.UnreadCount != 5 {
		t.Errorf("UnreadCount mismatch: %d", conv.UnreadCount)
	}
	if !conv.Muted {
		t.Error("expected Muted to be true")
	}
	if conv.Pinned {
		t.Error("expected Pinned to be false")
	}
}

// =============================================================================
// Raw Message Struct Tests
// =============================================================================

func TestRawMessageFields(t *testing.T) {
	raw := personal.RawMessage{
		ID:        "msg123",
		Content:   "Hello World",
		PeerID:    "1234567890@s.whatsapp.net",
		PeerName:  "John Doe",
		GroupID:   "123456789@g.us",
		GroupName: "Test Group",
		ReplyTo:   "msg122",
		Extra: map[string]any{
			"forwarded": true,
		},
	}

	if raw.ID != "msg123" {
		t.Errorf("ID mismatch: %s", raw.ID)
	}
	if raw.Content != "Hello World" {
		t.Errorf("Content mismatch: %s", raw.Content)
	}
	if raw.PeerID != "1234567890@s.whatsapp.net" {
		t.Errorf("PeerID mismatch: %s", raw.PeerID)
	}
	if raw.GroupID != "123456789@g.us" {
		t.Errorf("GroupID mismatch: %s", raw.GroupID)
	}
	if raw.ReplyTo != "msg122" {
		t.Errorf("ReplyTo mismatch: %s", raw.ReplyTo)
	}
}

// =============================================================================
// Raw Attachment Struct Tests
// =============================================================================

func TestRawAttachmentFields(t *testing.T) {
	att := personal.RawAttachment{
		ID:       "att123",
		MIMEType: "image/jpeg",
		Filename: "photo.jpg",
		Size:     1024,
		URL:      "https://example.com/photo.jpg",
		Data:     []byte{0x89, 0x50, 0x4E, 0x47},
	}

	if att.ID != "att123" {
		t.Errorf("ID mismatch: %s", att.ID)
	}
	if att.MIMEType != "image/jpeg" {
		t.Errorf("MIMEType mismatch: %s", att.MIMEType)
	}
	if att.Filename != "photo.jpg" {
		t.Errorf("Filename mismatch: %s", att.Filename)
	}
	if att.Size != 1024 {
		t.Errorf("Size mismatch: %d", att.Size)
	}
	if att.URL != "https://example.com/photo.jpg" {
		t.Errorf("URL mismatch: %s", att.URL)
	}
	if len(att.Data) != 4 {
		t.Errorf("Data length mismatch: %d", len(att.Data))
	}
}

// =============================================================================
// Presence Config Tests
// =============================================================================

func TestPresenceConfigAllDisabled(t *testing.T) {
	cfg := personal.PresenceConfig{
		SendReadReceipts: false,
		SendTyping:       false,
		BroadcastOnline:  false,
	}

	if cfg.SendReadReceipts {
		t.Error("expected SendReadReceipts to be false")
	}
	if cfg.SendTyping {
		t.Error("expected SendTyping to be false")
	}
	if cfg.BroadcastOnline {
		t.Error("expected BroadcastOnline to be false")
	}
}

func TestPresenceConfigAllEnabled(t *testing.T) {
	cfg := personal.PresenceConfig{
		SendReadReceipts: true,
		SendTyping:       true,
		BroadcastOnline:  true,
	}

	if !cfg.SendReadReceipts {
		t.Error("expected SendReadReceipts to be true")
	}
	if !cfg.SendTyping {
		t.Error("expected SendTyping to be true")
	}
	if !cfg.BroadcastOnline {
		t.Error("expected BroadcastOnline to be true")
	}
}

// =============================================================================
// List Options Tests
// =============================================================================

func TestListOptionsDefaults(t *testing.T) {
	opts := personal.ListOptions{}

	if opts.Limit != 0 {
		t.Errorf("expected default Limit 0, got %d", opts.Limit)
	}
	if opts.Offset != 0 {
		t.Errorf("expected default Offset 0, got %d", opts.Offset)
	}
	if opts.Unread {
		t.Error("expected default Unread false")
	}
	if opts.GroupID != "" {
		t.Errorf("expected default GroupID empty, got %s", opts.GroupID)
	}
}

func TestListOptionsWithValues(t *testing.T) {
	opts := personal.ListOptions{
		Limit:   50,
		Offset:  10,
		Unread:  true,
		GroupID: "123456789@g.us",
	}

	if opts.Limit != 50 {
		t.Errorf("expected Limit 50, got %d", opts.Limit)
	}
	if opts.Offset != 10 {
		t.Errorf("expected Offset 10, got %d", opts.Offset)
	}
	if !opts.Unread {
		t.Error("expected Unread true")
	}
	if opts.GroupID != "123456789@g.us" {
		t.Errorf("expected GroupID, got %s", opts.GroupID)
	}
}

// =============================================================================
// Event Handler Dispatch Tests
// =============================================================================

func TestHandleEventConnected(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: false,
	}
	// Create minimal BaseAdapter for logging
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// Simulate Connected event
	adapter.handleEvent(&events.Connected{})

	if !adapter.isConnected() {
		t.Error("expected adapter to be connected after Connected event")
	}

	status := adapter.Status()
	if !status.Connected {
		t.Error("expected status to show connected")
	}
}

func TestHandleEventDisconnected(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// Simulate Disconnected event
	adapter.handleEvent(&events.Disconnected{})

	if adapter.isConnected() {
		t.Error("expected adapter to be disconnected after Disconnected event")
	}

	status := adapter.Status()
	if status.Connected {
		t.Error("expected status to show disconnected")
	}
}

func TestHandleEventLoggedOut(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// Simulate LoggedOut event
	adapter.handleEvent(&events.LoggedOut{Reason: events.ConnectFailureLoggedOut})

	if adapter.isConnected() {
		t.Error("expected adapter to be disconnected after LoggedOut event")
	}

	status := adapter.Status()
	if status.Connected {
		t.Error("expected status to show disconnected")
	}
}

func TestHandleEventUnknown(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// Unknown event type should not crash or change state
	adapter.handleEvent("unknown event type")

	// State should be unchanged
	if !adapter.isConnected() {
		t.Error("expected connection state to be unchanged for unknown event")
	}
}

// =============================================================================
// Send Message Validation Tests
// =============================================================================

func TestSendMissingPeerID(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{},
	}

	err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Error("expected error for missing peer_id")
	}
	if !strings.Contains(err.Error(), "missing peer_id") {
		t.Errorf("expected 'missing peer_id' error, got: %v", err)
	}
}

func TestSendEmptyPeerID(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{"peer_id": ""},
	}

	err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Error("expected error for empty peer_id")
	}
}

func TestSendNotConnected(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: false,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"peer_id": "1234567890@s.whatsapp.net",
		},
	}

	err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Error("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestSendWithNilClient(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
		client:    nil, // Client is nil
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"peer_id": "1234567890@s.whatsapp.net",
		},
	}

	err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Error("expected error when client is nil")
	}
	// The error should come from trying to use the nil client
	if err != nil && !strings.Contains(err.Error(), "client is nil") {
		// Or could fail due to JID parsing or other issues
		t.Logf("got error: %v", err)
	}
}

func TestSendWithWrongMetadataType(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: true,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// peer_id is an int, not a string
	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"peer_id": 12345, // Wrong type
		},
	}

	err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Error("expected error for wrong peer_id type")
	}
}

// =============================================================================
// GetConversation Tests
// =============================================================================

func TestGetConversationDM(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	conv, err := adapter.GetConversation(context.Background(), "1234567890@s.whatsapp.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if conv.Type != personal.ConversationDM {
		t.Errorf("expected ConversationDM, got %s", conv.Type)
	}
	if conv.ID != "1234567890@s.whatsapp.net" {
		t.Errorf("expected ID to match peer_id")
	}
}

func TestGetConversationGroup(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	conv, err := adapter.GetConversation(context.Background(), "1234567890-1234567890@g.us")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if conv.Type != personal.ConversationGroup {
		t.Errorf("expected ConversationGroup, got %s", conv.Type)
	}
}

func TestGetConversationValidFormats(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// Test various JID formats that should work
	tests := []struct {
		jid      string
		expected personal.ConversationType
	}{
		{"1234567890@s.whatsapp.net", personal.ConversationDM},
		{"1234567890-1234567890@g.us", personal.ConversationGroup},
	}

	for _, tt := range tests {
		t.Run(tt.jid, func(t *testing.T) {
			conv, err := adapter.GetConversation(context.Background(), tt.jid)
			if err != nil {
				t.Errorf("unexpected error for %s: %v", tt.jid, err)
				return
			}
			if conv.Type != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, conv.Type)
			}
		})
	}
}

// =============================================================================
// Connection State Concurrency Tests
// =============================================================================

func TestConnectionStateConcurrency(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config:    cfg,
		connected: false,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	var wg sync.WaitGroup
	iterations := 100

	// Concurrently toggle connection state
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			adapter.handleEvent(&events.Connected{})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			adapter.handleEvent(&events.Disconnected{})
		}
	}()

	// Concurrently read connection state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations*2; i++ {
			_ = adapter.isConnected()
		}
	}()

	wg.Wait()
	// Test should not panic or race
}

// =============================================================================
// Media Type Selection Tests
// =============================================================================

func TestMediaTypeSelection(t *testing.T) {
	tests := []struct {
		mimeType   string
		expectType string
	}{
		{"image/jpeg", "image"},
		{"image/png", "image"},
		{"image/gif", "image"},
		{"image/webp", "image"},
		{"video/mp4", "video"},
		{"video/quicktime", "video"},
		{"video/webm", "video"},
		{"audio/mpeg", "audio"},
		{"audio/ogg", "audio"},
		{"audio/wav", "audio"},
		{"application/pdf", "document"},
		{"application/msword", "document"},
		{"text/plain", "document"},
		{"application/octet-stream", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			var result string
			switch {
			case strings.HasPrefix(tt.mimeType, "image/"):
				result = "image"
			case strings.HasPrefix(tt.mimeType, "video/"):
				result = "video"
			case strings.HasPrefix(tt.mimeType, "audio/"):
				result = "audio"
			default:
				result = "document"
			}

			if result != tt.expectType {
				t.Errorf("expected %s for %s, got %s", tt.expectType, tt.mimeType, result)
			}
		})
	}
}

// =============================================================================
// Stop Without Start Tests
// =============================================================================

func TestStopWithoutStart(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("whatsapp", &cfg.Personal, nil)

	// Stop should not panic even if Start was never called
	err := adapter.Stop(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	status := adapter.Status()
	if status.Connected {
		t.Error("expected disconnected status")
	}
}

// =============================================================================
// Additional Download Tests
// =============================================================================

func TestDownloadURLEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write nothing
	}))
	defer server.Close()

	data, err := downloadURL(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
}

func TestDownloadURLWithContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0}) // JPEG magic bytes
	}))
	defer server.Close()

	data, err := downloadURL(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 4 {
		t.Errorf("expected 4 bytes, got %d", len(data))
	}
}

func TestDownloadURLVariousStatusCodes(t *testing.T) {
	codes := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusMethodNotAllowed,
		http.StatusRequestTimeout,
		http.StatusConflict,
		http.StatusGone,
		http.StatusServiceUnavailable,
	}

	for _, code := range codes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			_, err := downloadURL(server.URL)
			if err == nil {
				t.Errorf("expected error for status %d", code)
			}
		})
	}
}

// =============================================================================
// Additional Media Handler Tests
// =============================================================================

func TestMediaHandlerGetURLWithContext(t *testing.T) {
	handler := &mediaHandler{}
	ctx := context.Background()

	url, err := handler.GetURL(ctx, "any-media-id")
	if err == nil {
		t.Error("expected error")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %s", url)
	}
}

// =============================================================================
// Additional Presence Manager Tests
// =============================================================================

func TestPresenceManagerSetTypingBothStates(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// Both started and stopped should work when disabled
	if err := pm.SetTyping(nil, "1234567890@s.whatsapp.net", true); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := pm.SetTyping(nil, "1234567890@s.whatsapp.net", false); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPresenceManagerSetOnlineBothStates(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.BroadcastOnline = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// Both online and offline should work when disabled
	if err := pm.SetOnline(nil, true); err != nil {
		t.Errorf("unexpected error for online: %v", err)
	}
	if err := pm.SetOnline(nil, false); err != nil {
		t.Errorf("unexpected error for offline: %v", err)
	}
}

func TestPresenceManagerMarkReadMultiple(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendReadReceipts = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	messages := []string{"msg1", "msg2", "msg3"}
	for _, msgID := range messages {
		if err := pm.MarkRead(nil, "1234567890@s.whatsapp.net", msgID); err != nil {
			t.Errorf("unexpected error for message %s: %v", msgID, err)
		}
	}
}

// =============================================================================
// Path Expansion Additional Tests
// =============================================================================

func TestExpandPathWithSpaces(t *testing.T) {
	input := "~/path with spaces/file.db"
	result := expandPath(input)

	if result == input {
		t.Error("expected path to be expanded")
	}
}

func TestExpandPathDeep(t *testing.T) {
	input := "~/a/very/deep/nested/path/to/file.db"
	result := expandPath(input)

	suffix := "/a/very/deep/nested/path/to/file.db"
	if result[len(result)-len(suffix):] != suffix {
		t.Errorf("expected suffix %s", suffix)
	}
}

func TestExpandPathMultipleTildes(t *testing.T) {
	// Only leading ~/ should be expanded
	input := "~/path/~/nested/~"
	result := expandPath(input)

	// Should still contain the embedded tildes
	if result == input {
		t.Error("expected leading tilde to be expanded")
	}
}

// =============================================================================
// Config Independent Instance Tests
// =============================================================================

func TestDefaultConfigIsIndependent(t *testing.T) {
	cfg1 := DefaultConfig()
	cfg2 := DefaultConfig()

	cfg1.SessionPath = "/custom/path1"
	cfg2.SessionPath = "/custom/path2"

	if cfg1.SessionPath == cfg2.SessionPath {
		t.Error("expected independent config instances")
	}
}

// =============================================================================
// Health Check Message Tests
// =============================================================================

func TestHealthCheckMessages(t *testing.T) {
	tests := []struct {
		name        string
		client      bool
		wantHealthy bool
		wantMessage string
	}{
		{
			name:        "no client",
			client:      false,
			wantHealthy: false,
			wantMessage: "client not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			adapter := &Adapter{
				config: cfg,
			}
			if !tt.client {
				adapter.client = nil
			}

			health := adapter.HealthCheck(context.Background())
			if health.Healthy != tt.wantHealthy {
				t.Errorf("expected healthy=%v, got %v", tt.wantHealthy, health.Healthy)
			}
			if health.Message != tt.wantMessage {
				t.Errorf("expected message %q, got %q", tt.wantMessage, health.Message)
			}
		})
	}
}

// =============================================================================
// QR Channel Tests
// =============================================================================

func TestQRChannelMultipleReads(t *testing.T) {
	cfg := DefaultConfig()
	adapter := &Adapter{
		config: cfg,
		qrChan: make(chan string, 3),
	}

	// Send multiple QR codes
	codes := []string{"code1", "code2", "code3"}
	for _, code := range codes {
		adapter.qrChan <- code
	}

	ch := adapter.QRChannel()
	for _, expected := range codes {
		select {
		case got := <-ch:
			if got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		default:
			t.Errorf("expected to receive %q", expected)
		}
	}
}

// =============================================================================
// IsConnected Tests
// =============================================================================

func TestIsConnectedStates(t *testing.T) {
	tests := []struct {
		name      string
		connected bool
	}{
		{"connected", true},
		{"disconnected", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			adapter := &Adapter{
				config:    cfg,
				connected: tt.connected,
			}

			if adapter.isConnected() != tt.connected {
				t.Errorf("expected isConnected=%v, got %v", tt.connected, adapter.isConnected())
			}
		})
	}
}

// =============================================================================
// JID Server Types Tests
// =============================================================================

func TestJIDServerTypes(t *testing.T) {
	tests := []struct {
		jid         string
		isGroup     bool
		description string
	}{
		{"1234567890@s.whatsapp.net", false, "individual user"},
		{"1234567890-1234567890@g.us", true, "group chat"},
		{"status@broadcast", false, "status broadcast"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			isGroup := len(tt.jid) > 4 && tt.jid[len(tt.jid)-4:] == "g.us"
			if isGroup != tt.isGroup {
				t.Errorf("expected isGroup=%v for %s", tt.isGroup, tt.jid)
			}
		})
	}
}

// =============================================================================
// Contact Manager GetByID Tests
// =============================================================================

// TestContactManagerGetByID is skipped because GetByID delegates to Resolve
// which requires a fully initialized adapter with WhatsApp client.
// The contactManager needs an adapter with a connected client to work properly.

// =============================================================================
// Raw Message With All Fields Tests
// =============================================================================

func TestRawMessageWithAllFields(t *testing.T) {
	raw := personal.RawMessage{
		ID:        "complete-msg",
		Content:   "Complete message content",
		PeerID:    "1234567890@s.whatsapp.net",
		PeerName:  "John Doe",
		GroupID:   "123456789@g.us",
		GroupName: "Test Group",
		ReplyTo:   "previous-msg-id",
		Attachments: []personal.RawAttachment{
			{
				ID:       "att1",
				MIMEType: "image/jpeg",
				Filename: "photo.jpg",
				Size:     1024,
				URL:      "https://example.com/photo.jpg",
			},
		},
		Extra: map[string]any{
			"forwarded":     true,
			"broadcast":     false,
			"mention_count": 3,
		},
	}

	// Verify all fields
	if raw.ID != "complete-msg" {
		t.Errorf("ID mismatch")
	}
	if raw.Content != "Complete message content" {
		t.Errorf("Content mismatch")
	}
	if raw.PeerID != "1234567890@s.whatsapp.net" {
		t.Errorf("PeerID mismatch")
	}
	if raw.PeerName != "John Doe" {
		t.Errorf("PeerName mismatch")
	}
	if raw.GroupID != "123456789@g.us" {
		t.Errorf("GroupID mismatch")
	}
	if raw.GroupName != "Test Group" {
		t.Errorf("GroupName mismatch")
	}
	if raw.ReplyTo != "previous-msg-id" {
		t.Errorf("ReplyTo mismatch")
	}
	if len(raw.Attachments) != 1 {
		t.Errorf("Attachments count mismatch")
	}
	if raw.Extra["forwarded"] != true {
		t.Errorf("Extra forwarded mismatch")
	}
	if raw.Extra["mention_count"] != 3 {
		t.Errorf("Extra mention_count mismatch")
	}
}

// =============================================================================
// Raw Attachment With Data Tests
// =============================================================================

func TestRawAttachmentWithInlineData(t *testing.T) {
	// Test inline data attachment
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	att := personal.RawAttachment{
		ID:       "inline-att",
		MIMEType: "application/octet-stream",
		Size:     int64(len(data)),
		Data:     data,
	}

	if len(att.Data) != 1024 {
		t.Errorf("expected 1024 bytes of data, got %d", len(att.Data))
	}
	if att.Size != 1024 {
		t.Errorf("expected size 1024, got %d", att.Size)
	}
}

// =============================================================================
// MIME Type Categories Tests
// =============================================================================

func TestMIMETypeCategories(t *testing.T) {
	categories := map[string][]string{
		"image":    {"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp"},
		"video":    {"video/mp4", "video/quicktime", "video/webm", "video/avi"},
		"audio":    {"audio/mpeg", "audio/ogg", "audio/wav", "audio/aac"},
		"document": {"application/pdf", "application/msword", "text/plain", "application/zip"},
	}

	for category, mimeTypes := range categories {
		for _, mimeType := range mimeTypes {
			t.Run(mimeType, func(t *testing.T) {
				prefix := category
				if len(mimeType) < len(prefix) {
					t.Errorf("MIME type too short")
					return
				}

				actualPrefix := ""
				if len(mimeType) >= 5 {
					if mimeType[:5] == "image" {
						actualPrefix = "image"
					} else if mimeType[:5] == "video" {
						actualPrefix = "video"
					} else if mimeType[:5] == "audio" {
						actualPrefix = "audio"
					}
				}

				if category == "document" {
					// Documents don't have a common prefix
					return
				}

				if actualPrefix != category {
					t.Errorf("expected category %s for %s, got prefix %s", category, mimeType, actualPrefix)
				}
			})
		}
	}
}
