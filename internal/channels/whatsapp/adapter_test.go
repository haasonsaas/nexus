package whatsapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
				if tt.input[0] != '~' && result != tt.input {
					t.Errorf("expected path unchanged, got %s", result)
				}
			}
		})
	}
}

func TestDownloadURL(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test content"))
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
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

func TestMediaHandlerDownloadNotImplemented(t *testing.T) {
	handler := &mediaHandler{}

	_, _, err := handler.Download(nil, "media123")
	if err == nil {
		t.Error("expected error for unimplemented download")
	}
}

func TestMediaHandlerUploadNotSupported(t *testing.T) {
	handler := &mediaHandler{}

	_, err := handler.Upload(nil, []byte("data"), "image/png", "test.png")
	if err == nil {
		t.Error("expected error for unsupported upload")
	}
}

func TestMediaHandlerGetURLNotAvailable(t *testing.T) {
	handler := &mediaHandler{}

	_, err := handler.GetURL(nil, "media123")
	if err == nil {
		t.Error("expected error for unavailable URL")
	}
}

func TestPresenceManagerSetTypingDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// When typing is disabled, should be no-op and not error
	err := pm.SetTyping(nil, "1234567890@s.whatsapp.net", true)
	if err != nil {
		t.Errorf("expected no error when typing disabled, got %v", err)
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

	// When broadcast online is disabled, should be no-op
	err := pm.SetOnline(nil, true)
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

	// When read receipts disabled, should be no-op
	err := pm.MarkRead(nil, "1234567890@s.whatsapp.net", "msg123")
	if err != nil {
		t.Errorf("expected no error when read receipts disabled, got %v", err)
	}
}

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

func TestPresenceManagerSetTypingInvalidJID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Personal.Presence.SendTyping = true

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
			// client is nil, so any call requiring it will fail
		},
	}

	// Invalid JID should return error
	err := pm.SetTyping(nil, "invalid-jid", true)
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

// TestContactManagerResolveInvalidJID is skipped because it requires
// internal structures that are not easily testable without a connected client.
// The logic is tested through integration tests instead.

// TestContactManagerGetByIDDelegates is skipped for the same reason.
