package signal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("expected Enabled to be false by default")
	}
	if cfg.SignalCLIPath != "signal-cli" {
		t.Errorf("expected SignalCLIPath to be 'signal-cli', got %s", cfg.SignalCLIPath)
	}
	if cfg.ConfigDir != "~/.config/signal-cli" {
		t.Errorf("expected ConfigDir to be '~/.config/signal-cli', got %s", cfg.ConfigDir)
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
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHome bool
	}{
		{
			name:     "tilde path",
			input:    "~/.config/signal-cli",
			wantHome: true,
		},
		{
			name:     "absolute path",
			input:    "/opt/signal-cli",
			wantHome: false,
		},
		{
			name:     "relative path",
			input:    "signal-cli",
			wantHome: false,
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

func TestNewAdapterMissingAccount(t *testing.T) {
	cfg := &Config{
		SignalCLIPath: "signal-cli", // This will fail LookPath anyway
	}

	_, err := New(cfg, nil)
	if err == nil {
		t.Error("expected error for missing account")
	}
}

func TestNewAdapterNilConfig(t *testing.T) {
	// With nil config, should use defaults which have empty account
	_, err := New(nil, nil)
	if err == nil {
		t.Error("expected error for empty account in default config")
	}
}

func TestJSONRPCMessageParsing(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantID     *int64
		wantMethod string
		wantResult bool
		wantError  bool
	}{
		{
			name:       "notification",
			input:      `{"jsonrpc":"2.0","method":"receive","params":{"source":"+1234567890"}}`,
			wantID:     nil,
			wantMethod: "receive",
		},
		{
			name:       "response with result",
			input:      `{"jsonrpc":"2.0","id":1,"result":{"success":true}}`,
			wantID:     ptrInt64(1),
			wantResult: true,
		},
		{
			name:      "response with error",
			input:     `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid Request"}}`,
			wantID:    ptrInt64(2),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg jsonRPCMessage
			if err := json.Unmarshal([]byte(tt.input), &msg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if tt.wantID != nil {
				if msg.ID == nil {
					t.Error("expected ID to be present")
				} else if *msg.ID != *tt.wantID {
					t.Errorf("expected ID %d, got %d", *tt.wantID, *msg.ID)
				}
			} else if msg.ID != nil {
				t.Errorf("expected ID to be nil, got %d", *msg.ID)
			}

			if msg.Method != tt.wantMethod {
				t.Errorf("expected method %q, got %q", tt.wantMethod, msg.Method)
			}

			if tt.wantResult && len(msg.Result) == 0 {
				t.Error("expected result to be present")
			}

			if tt.wantError && msg.Error == nil {
				t.Error("expected error to be present")
			}
		})
	}
}

func TestSignalEnvelopeParsing(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Hello World",
			"groupInfo": null,
			"attachments": [],
			"quote": null
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if envelope.Source != "+1234567890" {
		t.Errorf("expected source +1234567890, got %s", envelope.Source)
	}
	if envelope.SourceName != "John Doe" {
		t.Errorf("expected sourceName 'John Doe', got %s", envelope.SourceName)
	}
	if envelope.Timestamp != 1704067200000 {
		t.Errorf("expected timestamp 1704067200000, got %d", envelope.Timestamp)
	}
	if envelope.DataMessage == nil {
		t.Fatal("expected dataMessage to be present")
	}
	if envelope.DataMessage.Message != "Hello World" {
		t.Errorf("expected message 'Hello World', got %s", envelope.DataMessage.Message)
	}
}

func TestSignalEnvelopeWithGroup(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Hello Group",
			"groupInfo": {
				"groupId": "abc123",
				"groupName": "Test Group"
			}
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if envelope.DataMessage.GroupInfo == nil {
		t.Fatal("expected groupInfo to be present")
	}
	if envelope.DataMessage.GroupInfo.GroupID != "abc123" {
		t.Errorf("expected groupId 'abc123', got %s", envelope.DataMessage.GroupInfo.GroupID)
	}
	if envelope.DataMessage.GroupInfo.GroupName != "Test Group" {
		t.Errorf("expected groupName 'Test Group', got %s", envelope.DataMessage.GroupInfo.GroupName)
	}
}

func TestSignalEnvelopeWithAttachments(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "",
			"attachments": [
				{
					"id": "att123",
					"contentType": "image/jpeg",
					"filename": "photo.jpg",
					"size": 12345
				}
			]
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if len(envelope.DataMessage.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(envelope.DataMessage.Attachments))
	}

	att := envelope.DataMessage.Attachments[0]
	if att.ID != "att123" {
		t.Errorf("expected attachment id 'att123', got %s", att.ID)
	}
	if att.ContentType != "image/jpeg" {
		t.Errorf("expected contentType 'image/jpeg', got %s", att.ContentType)
	}
	if att.Filename != "photo.jpg" {
		t.Errorf("expected filename 'photo.jpg', got %s", att.Filename)
	}
	if att.Size != 12345 {
		t.Errorf("expected size 12345, got %d", att.Size)
	}
}

func TestSignalEnvelopeWithQuote(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Reply to this",
			"quote": {
				"id": 1704067100000,
				"author": "+0987654321",
				"text": "Original message"
			}
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if envelope.DataMessage.Quote == nil {
		t.Fatal("expected quote to be present")
	}
	if envelope.DataMessage.Quote.ID != 1704067100000 {
		t.Errorf("expected quote id 1704067100000, got %d", envelope.DataMessage.Quote.ID)
	}
	if envelope.DataMessage.Quote.Author != "+0987654321" {
		t.Errorf("expected quote author '+0987654321', got %s", envelope.DataMessage.Quote.Author)
	}
	if envelope.DataMessage.Quote.Text != "Original message" {
		t.Errorf("expected quote text 'Original message', got %s", envelope.DataMessage.Quote.Text)
	}
}

func TestSignalContactParsing(t *testing.T) {
	input := `{
		"number": "+1234567890",
		"uuid": "uuid-123",
		"name": "John Doe"
	}`

	var contact signalContact
	if err := json.Unmarshal([]byte(input), &contact); err != nil {
		t.Fatalf("failed to unmarshal contact: %v", err)
	}

	if contact.Number != "+1234567890" {
		t.Errorf("expected number '+1234567890', got %s", contact.Number)
	}
	if contact.UUID != "uuid-123" {
		t.Errorf("expected uuid 'uuid-123', got %s", contact.UUID)
	}
	if contact.Name != "John Doe" {
		t.Errorf("expected name 'John Doe', got %s", contact.Name)
	}
}

func TestPresenceManagerSetTypingConfigDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"
	cfg.Personal.Presence.SendTyping = false

	// Create a mock presence manager
	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// When typing is disabled, SetTyping should be a no-op and not error
	err := pm.SetTyping(nil, "+0987654321", true)
	if err != nil {
		t.Errorf("expected no error when typing is disabled, got %v", err)
	}
}

func TestPresenceManagerSetOnline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// Signal doesn't support explicit online status, should be no-op
	err := pm.SetOnline(nil, true)
	if err != nil {
		t.Errorf("expected no error for SetOnline, got %v", err)
	}

	err = pm.SetOnline(nil, false)
	if err != nil {
		t.Errorf("expected no error for SetOnline, got %v", err)
	}
}

func TestPresenceManagerMarkReadConfigDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"
	cfg.Personal.Presence.SendReadReceipts = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// When read receipts are disabled, MarkRead should be a no-op
	err := pm.MarkRead(nil, "+0987654321", "123")
	if err != nil {
		t.Errorf("expected no error when read receipts disabled, got %v", err)
	}
}

func TestTimestampConversion(t *testing.T) {
	// Signal uses milliseconds since Unix epoch
	timestamp := int64(1704067200000) // 2024-01-01 00:00:00 UTC in milliseconds

	tm := time.UnixMilli(timestamp)

	expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if !tm.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, tm)
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}
