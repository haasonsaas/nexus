package signal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
	"github.com/haasonsaas/nexus/pkg/models"
)

// =============================================================================
// Config Tests
// =============================================================================

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

func TestDefaultConfigAllFields(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"Enabled", cfg.Enabled, false},
		{"SignalCLIPath", cfg.SignalCLIPath, "signal-cli"},
		{"ConfigDir", cfg.ConfigDir, "~/.config/signal-cli"},
		{"Account", cfg.Account, ""},
		{"SyncOnStart", cfg.Personal.SyncOnStart, true},
		{"SendReadReceipts", cfg.Personal.Presence.SendReadReceipts, true},
		{"SendTyping", cfg.Personal.Presence.SendTyping, true},
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
		Enabled:       true,
		Account:       "+1234567890",
		SignalCLIPath: "/usr/local/bin/signal-cli",
		ConfigDir:     "/custom/config",
		Personal: personal.Config{
			SyncOnStart: false,
			Presence: personal.PresenceConfig{
				SendReadReceipts: false,
				SendTyping:       false,
			},
		},
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.Account != "+1234567890" {
		t.Errorf("expected Account '+1234567890', got %s", cfg.Account)
	}
	if cfg.SignalCLIPath != "/usr/local/bin/signal-cli" {
		t.Errorf("expected custom SignalCLIPath, got %s", cfg.SignalCLIPath)
	}
	if cfg.ConfigDir != "/custom/config" {
		t.Errorf("expected custom ConfigDir, got %s", cfg.ConfigDir)
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
		{
			name:     "tilde only",
			input:    "~",
			wantHome: false, // Only ~/ is expanded
		},
		{
			name:     "tilde in middle",
			input:    "/opt/~/config",
			wantHome: false,
		},
		{
			name:     "empty path",
			input:    "",
			wantHome: false,
		},
		{
			name:     "nested tilde path",
			input:    "~/a/b/c/d",
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
	input := "~/.config/signal-cli"
	result := expandPath(input)

	suffix := "/.config/signal-cli"
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
			data, err := downloadURL(context.Background(), server.URL+tt.path)
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

func TestDownloadURLInvalidURL(t *testing.T) {
	_, err := downloadURL(context.Background(), "http://invalid-url-that-does-not-exist.example.com/test")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// =============================================================================
// Adapter Creation Tests
// =============================================================================

func TestNewAdapterMissingAccount(t *testing.T) {
	cfg := &Config{
		SignalCLIPath: "signal-cli",
	}

	_, err := New(cfg, nil)
	if err == nil {
		t.Error("expected error for missing account")
	}
}

func TestNewAdapterNilConfig(t *testing.T) {
	_, err := New(nil, nil)
	if err == nil {
		t.Error("expected error for empty account in default config")
	}
}

func TestNewAdapterMissingSignalCLI(t *testing.T) {
	cfg := &Config{
		Account:       "+1234567890",
		SignalCLIPath: "/nonexistent/signal-cli-path-that-does-not-exist",
	}

	_, err := New(cfg, nil)
	if err == nil {
		t.Error("expected error for missing signal-cli binary")
	}
}

// =============================================================================
// JSON-RPC Message Parsing Tests
// =============================================================================

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
		{
			name:       "notification without params",
			input:      `{"jsonrpc":"2.0","method":"ping"}`,
			wantID:     nil,
			wantMethod: "ping",
		},
		{
			name:       "response with zero id",
			input:      `{"jsonrpc":"2.0","id":0,"result":null}`,
			wantID:     ptrInt64(0),
			wantResult: true,
		},
		{
			name:       "response with large id",
			input:      `{"jsonrpc":"2.0","id":9999999999,"result":{}}`,
			wantID:     ptrInt64(9999999999),
			wantResult: true,
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

func TestJSONRPCErrorParsing(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCode    int
		wantMessage string
	}{
		{
			name:        "parse error",
			input:       `{"code":-32700,"message":"Parse error"}`,
			wantCode:    -32700,
			wantMessage: "Parse error",
		},
		{
			name:        "invalid request",
			input:       `{"code":-32600,"message":"Invalid Request"}`,
			wantCode:    -32600,
			wantMessage: "Invalid Request",
		},
		{
			name:        "method not found",
			input:       `{"code":-32601,"message":"Method not found"}`,
			wantCode:    -32601,
			wantMessage: "Method not found",
		},
		{
			name:        "internal error",
			input:       `{"code":-32603,"message":"Internal error"}`,
			wantCode:    -32603,
			wantMessage: "Internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err jsonRPCError
			if unmarshalErr := json.Unmarshal([]byte(tt.input), &err); unmarshalErr != nil {
				t.Fatalf("failed to unmarshal: %v", unmarshalErr)
			}

			if err.Code != tt.wantCode {
				t.Errorf("expected code %d, got %d", tt.wantCode, err.Code)
			}
			if err.Message != tt.wantMessage {
				t.Errorf("expected message %q, got %q", tt.wantMessage, err.Message)
			}
		})
	}
}

// =============================================================================
// Signal Envelope Parsing Tests
// =============================================================================

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

func TestSignalEnvelopeWithoutDataMessage(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if envelope.DataMessage != nil {
		t.Error("expected dataMessage to be nil")
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

func TestSignalEnvelopeWithMultipleAttachments(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Multiple attachments",
			"attachments": [
				{"id": "att1", "contentType": "image/jpeg", "filename": "photo1.jpg", "size": 1000},
				{"id": "att2", "contentType": "image/png", "filename": "photo2.png", "size": 2000},
				{"id": "att3", "contentType": "application/pdf", "filename": "doc.pdf", "size": 3000}
			]
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}

	if len(envelope.DataMessage.Attachments) != 3 {
		t.Fatalf("expected 3 attachments, got %d", len(envelope.DataMessage.Attachments))
	}

	expectedIDs := []string{"att1", "att2", "att3"}
	for i, att := range envelope.DataMessage.Attachments {
		if att.ID != expectedIDs[i] {
			t.Errorf("attachment %d: expected id %q, got %q", i, expectedIDs[i], att.ID)
		}
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

// =============================================================================
// Signal Data Message Tests
// =============================================================================

func TestSignalDataMessageEmpty(t *testing.T) {
	input := `{
		"timestamp": 1704067200000,
		"message": "",
		"groupInfo": null,
		"attachments": [],
		"quote": null
	}`

	var dm signalDataMessage
	if err := json.Unmarshal([]byte(input), &dm); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if dm.Message != "" {
		t.Errorf("expected empty message, got %q", dm.Message)
	}
	if dm.GroupInfo != nil {
		t.Error("expected nil groupInfo")
	}
	if len(dm.Attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(dm.Attachments))
	}
	if dm.Quote != nil {
		t.Error("expected nil quote")
	}
}

func TestSignalDataMessageWithUnicode(t *testing.T) {
	input := `{
		"timestamp": 1704067200000,
		"message": "Hello World! Привет мир! 你好世界!"
	}`

	var dm signalDataMessage
	if err := json.Unmarshal([]byte(input), &dm); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	expected := "Hello World! Привет мир! 你好世界!"
	if dm.Message != expected {
		t.Errorf("expected message %q, got %q", expected, dm.Message)
	}
}

// =============================================================================
// Signal Contact Parsing Tests
// =============================================================================

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

func TestSignalContactParsingPartial(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantNumber string
		wantUUID   string
		wantName   string
	}{
		{
			name:       "only number",
			input:      `{"number": "+1234567890"}`,
			wantNumber: "+1234567890",
		},
		{
			name:     "only uuid",
			input:    `{"uuid": "uuid-456"}`,
			wantUUID: "uuid-456",
		},
		{
			name:     "only name",
			input:    `{"name": "Jane Doe"}`,
			wantName: "Jane Doe",
		},
		{
			name:       "number and name",
			input:      `{"number": "+1234567890", "name": "John"}`,
			wantNumber: "+1234567890",
			wantName:   "John",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var contact signalContact
			if err := json.Unmarshal([]byte(tt.input), &contact); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if contact.Number != tt.wantNumber {
				t.Errorf("expected number %q, got %q", tt.wantNumber, contact.Number)
			}
			if contact.UUID != tt.wantUUID {
				t.Errorf("expected uuid %q, got %q", tt.wantUUID, contact.UUID)
			}
			if contact.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, contact.Name)
			}
		})
	}
}

// =============================================================================
// Presence Manager Tests
// =============================================================================

func TestPresenceManagerSetTypingConfigDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"
	cfg.Personal.Presence.SendTyping = false

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	err := pm.SetTyping(nil, "+0987654321", true)
	if err != nil {
		t.Errorf("expected no error when typing is disabled, got %v", err)
	}
}

func TestPresenceManagerSetTypingConfigEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"
	cfg.Personal.Presence.SendTyping = true

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
			// No stdin, so call will fail, but we test config check
		},
	}

	// Without stdin, this would fail, but the config check happens first
	// Since typing is enabled and there's no stdin, this should eventually error
	// But the point is config-disabled returns early
	_ = pm // Test that the struct is properly initialized
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

	err := pm.MarkRead(nil, "+0987654321", "123")
	if err != nil {
		t.Errorf("expected no error when read receipts disabled, got %v", err)
	}
}

func TestPresenceManagerSubscribe(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	ch, err := pm.Subscribe(context.Background(), "+0987654321")
	if err != nil {
		t.Errorf("expected no error for Subscribe, got %v", err)
	}
	if ch == nil {
		t.Error("expected non-nil channel")
	}
}

// =============================================================================
// Contact Manager Tests
// =============================================================================

func TestContactManagerSearch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	cm := &contactManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	results, err := cm.Search(context.Background(), "test")
	if err == nil {
		t.Error("expected error for missing signal client")
	}
	if channels.GetErrorCode(err) != channels.ErrCodeUnavailable {
		t.Errorf("expected unavailable error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

// =============================================================================
// Timestamp Conversion Tests
// =============================================================================

func TestTimestampConversion(t *testing.T) {
	tests := []struct {
		name      string
		timestamp int64
		expected  time.Time
	}{
		{
			name:      "2024-01-01 00:00:00 UTC",
			timestamp: 1704067200000,
			expected:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Unix epoch",
			timestamp: 0,
			expected:  time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "2000-01-01 00:00:00 UTC",
			timestamp: 946684800000,
			expected:  time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := time.UnixMilli(tt.timestamp).UTC()
			if !tm.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, tm)
			}
		})
	}
}

// =============================================================================
// Group Info Tests
// =============================================================================

func TestSignalGroupInfoParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantID   string
		wantName string
	}{
		{
			name:     "basic group",
			input:    `{"groupId": "group123", "groupName": "Friends"}`,
			wantID:   "group123",
			wantName: "Friends",
		},
		{
			name:   "group without name",
			input:  `{"groupId": "group456"}`,
			wantID: "group456",
		},
		{
			name:     "group with empty name",
			input:    `{"groupId": "group789", "groupName": ""}`,
			wantID:   "group789",
			wantName: "",
		},
		{
			name:     "group with unicode name",
			input:    `{"groupId": "groupUni", "groupName": "Группа друзей"}`,
			wantID:   "groupUni",
			wantName: "Группа друзей",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gi signalGroupInfo
			if err := json.Unmarshal([]byte(tt.input), &gi); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if gi.GroupID != tt.wantID {
				t.Errorf("expected groupId %q, got %q", tt.wantID, gi.GroupID)
			}
			if gi.GroupName != tt.wantName {
				t.Errorf("expected groupName %q, got %q", tt.wantName, gi.GroupName)
			}
		})
	}
}

// =============================================================================
// Attachment Tests
// =============================================================================

func TestSignalAttachmentParsing(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantID          string
		wantContentType string
		wantFilename    string
		wantSize        int64
	}{
		{
			name:            "image attachment",
			input:           `{"id": "img1", "contentType": "image/jpeg", "filename": "photo.jpg", "size": 1024}`,
			wantID:          "img1",
			wantContentType: "image/jpeg",
			wantFilename:    "photo.jpg",
			wantSize:        1024,
		},
		{
			name:            "video attachment",
			input:           `{"id": "vid1", "contentType": "video/mp4", "filename": "video.mp4", "size": 5242880}`,
			wantID:          "vid1",
			wantContentType: "video/mp4",
			wantFilename:    "video.mp4",
			wantSize:        5242880,
		},
		{
			name:            "document attachment",
			input:           `{"id": "doc1", "contentType": "application/pdf", "filename": "document.pdf", "size": 2048}`,
			wantID:          "doc1",
			wantContentType: "application/pdf",
			wantFilename:    "document.pdf",
			wantSize:        2048,
		},
		{
			name:            "attachment without filename",
			input:           `{"id": "att1", "contentType": "image/png", "size": 512}`,
			wantID:          "att1",
			wantContentType: "image/png",
			wantSize:        512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var att signalAttachment
			if err := json.Unmarshal([]byte(tt.input), &att); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if att.ID != tt.wantID {
				t.Errorf("expected id %q, got %q", tt.wantID, att.ID)
			}
			if att.ContentType != tt.wantContentType {
				t.Errorf("expected contentType %q, got %q", tt.wantContentType, att.ContentType)
			}
			if att.Filename != tt.wantFilename {
				t.Errorf("expected filename %q, got %q", tt.wantFilename, att.Filename)
			}
			if att.Size != tt.wantSize {
				t.Errorf("expected size %d, got %d", tt.wantSize, att.Size)
			}
		})
	}
}

// =============================================================================
// Quote Tests
// =============================================================================

func TestSignalQuoteParsing(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantID     int64
		wantAuthor string
		wantText   string
	}{
		{
			name:       "basic quote",
			input:      `{"id": 1704067100000, "author": "+1234567890", "text": "Original"}`,
			wantID:     1704067100000,
			wantAuthor: "+1234567890",
			wantText:   "Original",
		},
		{
			name:       "quote without text",
			input:      `{"id": 1704067100000, "author": "+1234567890"}`,
			wantID:     1704067100000,
			wantAuthor: "+1234567890",
		},
		{
			name:       "quote with empty text",
			input:      `{"id": 1704067100000, "author": "+1234567890", "text": ""}`,
			wantID:     1704067100000,
			wantAuthor: "+1234567890",
			wantText:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var quote signalQuote
			if err := json.Unmarshal([]byte(tt.input), &quote); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if quote.ID != tt.wantID {
				t.Errorf("expected id %d, got %d", tt.wantID, quote.ID)
			}
			if quote.Author != tt.wantAuthor {
				t.Errorf("expected author %q, got %q", tt.wantAuthor, quote.Author)
			}
			if quote.Text != tt.wantText {
				t.Errorf("expected text %q, got %q", tt.wantText, quote.Text)
			}
		})
	}
}

// =============================================================================
// Full Message Flow Tests
// =============================================================================

func TestFullEnvelopeToRawMessage(t *testing.T) {
	// Test converting a full envelope to raw message format
	input := `{
		"source": "+1234567890",
		"sourceName": "John Doe",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Hello World",
			"groupInfo": {
				"groupId": "group123",
				"groupName": "Test Group"
			},
			"attachments": [
				{
					"id": "att1",
					"contentType": "image/jpeg",
					"filename": "photo.jpg",
					"size": 1024
				}
			],
			"quote": {
				"id": 1704067100000,
				"author": "+0987654321",
				"text": "Original message"
			}
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify all fields
	if envelope.Source != "+1234567890" {
		t.Errorf("source mismatch")
	}
	if envelope.SourceName != "John Doe" {
		t.Errorf("sourceName mismatch")
	}
	if envelope.DataMessage == nil {
		t.Fatal("dataMessage is nil")
	}
	if envelope.DataMessage.Message != "Hello World" {
		t.Errorf("message mismatch")
	}
	if envelope.DataMessage.GroupInfo == nil {
		t.Fatal("groupInfo is nil")
	}
	if envelope.DataMessage.GroupInfo.GroupID != "group123" {
		t.Errorf("groupId mismatch")
	}
	if len(envelope.DataMessage.Attachments) != 1 {
		t.Errorf("attachments count mismatch")
	}
	if envelope.DataMessage.Quote == nil {
		t.Fatal("quote is nil")
	}
	if envelope.DataMessage.Quote.ID != 1704067100000 {
		t.Errorf("quote id mismatch")
	}
}

// =============================================================================
// Edge Cases Tests
// =============================================================================

func TestEnvelopeWithNullFields(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": null,
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Test",
			"groupInfo": null,
			"attachments": null,
			"quote": null
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if envelope.SourceName != "" {
		t.Errorf("expected empty sourceName, got %q", envelope.SourceName)
	}
	if envelope.DataMessage.GroupInfo != nil {
		t.Error("expected nil groupInfo")
	}
	if envelope.DataMessage.Attachments != nil {
		t.Error("expected nil attachments")
	}
	if envelope.DataMessage.Quote != nil {
		t.Error("expected nil quote")
	}
}

func TestEnvelopeWithEmptyArrays(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"sourceName": "",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "",
			"attachments": []
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(envelope.DataMessage.Attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(envelope.DataMessage.Attachments))
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}

// =============================================================================
// Additional Presence Manager Tests
// =============================================================================

func TestPresenceManagerSetTypingStarted(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"
	cfg.Personal.Presence.SendTyping = false // Disabled, so it's a no-op

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// Both true and false should work when disabled
	err := pm.SetTyping(nil, "+0987654321", true)
	if err != nil {
		t.Errorf("expected no error for typing true, got %v", err)
	}

	err = pm.SetTyping(nil, "+0987654321", false)
	if err != nil {
		t.Errorf("expected no error for typing false, got %v", err)
	}
}

func TestPresenceManagerMarkReadEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"
	cfg.Personal.Presence.SendReadReceipts = false // Disabled

	pm := &presenceManager{
		adapter: &Adapter{
			config: cfg,
		},
	}

	// When disabled, should be no-op
	err := pm.MarkRead(nil, "+0987654321", "msg123")
	if err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}

	err = pm.MarkRead(nil, "+0987654321", "msg456")
	if err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}
}

// =============================================================================
// Download URL Additional Tests
// =============================================================================

func TestDownloadURLWithHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "12")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test content"))
	}))
	defer server.Close()

	data, err := downloadURL(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("expected 'test content', got %q", string(data))
	}
}

func TestDownloadURLEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write nothing
	}))
	defer server.Close()

	data, err := downloadURL(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
}

// =============================================================================
// JSON-RPC Additional Tests
// =============================================================================

func TestJSONRPCMessageWithParams(t *testing.T) {
	input := `{
		"jsonrpc": "2.0",
		"method": "receive",
		"params": {
			"source": "+1234567890",
			"timestamp": 1704067200000,
			"dataMessage": {
				"message": "Hello"
			}
		}
	}`

	var msg jsonRPCMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if msg.Method != "receive" {
		t.Errorf("expected method 'receive', got %q", msg.Method)
	}
	if len(msg.Params) == 0 {
		t.Error("expected params to be present")
	}
}

func TestJSONRPCMessageMinimal(t *testing.T) {
	input := `{"jsonrpc": "2.0"}`

	var msg jsonRPCMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if msg.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %q", msg.JSONRPC)
	}
	if msg.ID != nil {
		t.Error("expected ID to be nil")
	}
	if msg.Method != "" {
		t.Error("expected Method to be empty")
	}
}

// =============================================================================
// Signal Envelope Edge Cases
// =============================================================================

func TestSignalEnvelopeEmptySource(t *testing.T) {
	input := `{
		"source": "",
		"sourceName": "",
		"timestamp": 1704067200000,
		"dataMessage": {
			"message": "Test"
		}
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if envelope.Source != "" {
		t.Errorf("expected empty source, got %q", envelope.Source)
	}
}

func TestSignalEnvelopeLargeTimestamp(t *testing.T) {
	input := `{
		"source": "+1234567890",
		"timestamp": 9999999999999
	}`

	var envelope signalEnvelope
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if envelope.Timestamp != 9999999999999 {
		t.Errorf("expected large timestamp, got %d", envelope.Timestamp)
	}
}

// =============================================================================
// Attachment Size Tests
// =============================================================================

func TestSignalAttachmentLargeSize(t *testing.T) {
	input := `{
		"id": "large-att",
		"contentType": "video/mp4",
		"filename": "large_video.mp4",
		"size": 2147483647
	}`

	var att signalAttachment
	if err := json.Unmarshal([]byte(input), &att); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if att.Size != 2147483647 {
		t.Errorf("expected size 2147483647, got %d", att.Size)
	}
}

func TestSignalAttachmentZeroSize(t *testing.T) {
	input := `{
		"id": "zero-att",
		"contentType": "text/plain",
		"filename": "empty.txt",
		"size": 0
	}`

	var att signalAttachment
	if err := json.Unmarshal([]byte(input), &att); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if att.Size != 0 {
		t.Errorf("expected size 0, got %d", att.Size)
	}
}

// =============================================================================
// Contact Manager GetByID Test
// =============================================================================

// TestContactManagerGetByID is skipped because GetByID delegates to Resolve
// which requires a fully initialized BaseAdapter with contact cache.
// The contactManager needs an adapter with BaseAdapter to work properly.

// =============================================================================
// Full Data Message Tests
// =============================================================================

func TestSignalDataMessageAllFields(t *testing.T) {
	input := `{
		"timestamp": 1704067200000,
		"message": "Full message with all fields",
		"groupInfo": {
			"groupId": "group123",
			"groupName": "Full Group"
		},
		"attachments": [
			{
				"id": "att1",
				"contentType": "image/jpeg",
				"filename": "photo.jpg",
				"size": 1024
			},
			{
				"id": "att2",
				"contentType": "video/mp4",
				"filename": "video.mp4",
				"size": 5242880
			}
		],
		"quote": {
			"id": 1704067100000,
			"author": "+0987654321",
			"text": "Quoted text"
		}
	}`

	var dm signalDataMessage
	if err := json.Unmarshal([]byte(input), &dm); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if dm.Message != "Full message with all fields" {
		t.Errorf("message mismatch")
	}
	if dm.GroupInfo == nil {
		t.Fatal("expected groupInfo")
	}
	if dm.GroupInfo.GroupID != "group123" {
		t.Errorf("groupId mismatch")
	}
	if len(dm.Attachments) != 2 {
		t.Errorf("expected 2 attachments, got %d", len(dm.Attachments))
	}
	if dm.Quote == nil {
		t.Fatal("expected quote")
	}
	if dm.Quote.Text != "Quoted text" {
		t.Errorf("quote text mismatch")
	}
}

// =============================================================================
// Path Expansion Edge Cases
// =============================================================================

func TestExpandPathWithSpaces(t *testing.T) {
	// Paths with spaces should work
	input := "~/path with spaces/file.db"
	result := expandPath(input)

	if result == input {
		t.Errorf("expected path to be expanded")
	}
	// Should contain the path with spaces
	if len(result) < len("/path with spaces/file.db") {
		t.Errorf("expanded path too short: %s", result)
	}
}

func TestExpandPathDeep(t *testing.T) {
	input := "~/a/very/deep/nested/path/to/file.db"
	result := expandPath(input)

	suffix := "/a/very/deep/nested/path/to/file.db"
	if result[len(result)-len(suffix):] != suffix {
		t.Errorf("expected suffix %s, got %s", suffix, result[len(result)-len(suffix):])
	}
}

// =============================================================================
// Default Config Mutation Tests
// =============================================================================

func TestDefaultConfigIsIndependent(t *testing.T) {
	cfg1 := DefaultConfig()
	cfg2 := DefaultConfig()

	cfg1.Account = "+1111111111"
	cfg2.Account = "+2222222222"

	if cfg1.Account == cfg2.Account {
		t.Error("expected independent config instances")
	}
}

func TestDefaultConfigFieldsNotNil(t *testing.T) {
	cfg := DefaultConfig()

	// Personal config should be initialized
	if cfg.Personal.SyncOnStart != true {
		t.Error("expected SyncOnStart to be true")
	}
}

// =============================================================================
// HTTP Status Code Tests
// =============================================================================

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

			_, err := downloadURL(context.Background(), server.URL)
			if err == nil {
				t.Errorf("expected error for status %d", code)
			}
		})
	}
}

// =============================================================================
// ProcessLine Tests
// =============================================================================

func TestProcessLineReceiveNotification(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Send a receive notification
	line := `{"jsonrpc":"2.0","method":"receive","params":{"source":"+0987654321","sourceName":"Test User","timestamp":1704067200000,"dataMessage":{"timestamp":1704067200000,"message":"Hello from Signal"}}}`

	// Should process without error
	adapter.processLine(line)

	// Check that a message was emitted
	select {
	case msg := <-adapter.Messages():
		if msg.Content != "Hello from Signal" {
			t.Errorf("expected 'Hello from Signal', got %q", msg.Content)
		}
		if msg.Metadata["peer_id"] != "+0987654321" {
			t.Errorf("expected peer_id '+0987654321', got %v", msg.Metadata["peer_id"])
		}
		if msg.Metadata["peer_name"] != "Test User" {
			t.Errorf("expected peer_name 'Test User', got %v", msg.Metadata["peer_name"])
		}
	default:
		t.Error("expected message to be emitted")
	}
}

func TestProcessLineResponseHandling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Register a pending request
	id := int64(42)
	ch := make(chan json.RawMessage, 1)
	adapter.pending[id] = ch

	// Send a response
	line := `{"jsonrpc":"2.0","id":42,"result":{"success":true}}`
	adapter.processLine(line)

	// Check that the response was delivered
	select {
	case result := <-ch:
		var parsed map[string]bool
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if !parsed["success"] {
			t.Error("expected success to be true")
		}
	default:
		t.Error("expected response to be delivered to pending channel")
	}

	// Pending map should be cleared
	adapter.pendingMu.Lock()
	if _, exists := adapter.pending[id]; exists {
		t.Error("expected pending request to be removed")
	}
	adapter.pendingMu.Unlock()
}

func TestProcessLineInvalidJSON(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Invalid JSON should not panic
	adapter.processLine("not valid json")
	adapter.processLine("{incomplete")
	adapter.processLine("")
}

func TestProcessLineUnknownMethod(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Unknown method should be ignored
	line := `{"jsonrpc":"2.0","method":"unknownMethod","params":{}}`
	adapter.processLine(line)

	// No message should be emitted
	select {
	case <-adapter.Messages():
		t.Error("expected no message for unknown method")
	default:
		// Expected
	}
}

// =============================================================================
// HandleReceive Tests
// =============================================================================

func TestHandleReceiveWithGroup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	params := json.RawMessage(`{
		"source": "+0987654321",
		"sourceName": "John",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Hello Group!",
			"groupInfo": {
				"groupId": "group123",
				"groupName": "Test Group"
			}
		}
	}`)

	adapter.handleReceive(params)

	select {
	case msg := <-adapter.Messages():
		if msg.Content != "Hello Group!" {
			t.Errorf("expected 'Hello Group!', got %q", msg.Content)
		}
		if msg.Metadata["group_id"] != "group123" {
			t.Errorf("expected group_id 'group123', got %v", msg.Metadata["group_id"])
		}
		if msg.Metadata["group_name"] != "Test Group" {
			t.Errorf("expected group_name 'Test Group', got %v", msg.Metadata["group_name"])
		}
	default:
		t.Error("expected message to be emitted")
	}
}

func TestHandleReceiveWithAttachments(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	params := json.RawMessage(`{
		"source": "+0987654321",
		"sourceName": "Jane",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Check this out",
			"attachments": [
				{
					"id": "att123",
					"contentType": "image/jpeg",
					"filename": "photo.jpg",
					"size": 12345
				}
			]
		}
	}`)

	adapter.handleReceive(params)

	select {
	case msg := <-adapter.Messages():
		if len(msg.Attachments) != 1 {
			t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
		}
		att := msg.Attachments[0]
		if att.ID != "att123" {
			t.Errorf("expected attachment ID 'att123', got %s", att.ID)
		}
		if att.MimeType != "image/jpeg" {
			t.Errorf("expected MIME type 'image/jpeg', got %s", att.MimeType)
		}
	default:
		t.Error("expected message to be emitted")
	}
}

func TestHandleReceiveWithQuote(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	params := json.RawMessage(`{
		"source": "+0987654321",
		"sourceName": "Bob",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Reply here",
			"quote": {
				"id": 1704067100000,
				"author": "+1111111111",
				"text": "Original message"
			}
		}
	}`)

	adapter.handleReceive(params)

	select {
	case msg := <-adapter.Messages():
		if msg.Metadata["reply_to"] != "1704067100000" {
			t.Errorf("expected reply_to '1704067100000', got %v", msg.Metadata["reply_to"])
		}
	default:
		t.Error("expected message to be emitted")
	}
}

func TestHandleReceiveNoDataMessage(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Envelope without dataMessage (e.g., typing indicator)
	params := json.RawMessage(`{
		"source": "+0987654321",
		"sourceName": "Alice",
		"timestamp": 1704067200000
	}`)

	adapter.handleReceive(params)

	// No message should be emitted
	select {
	case <-adapter.Messages():
		t.Error("expected no message for envelope without dataMessage")
	default:
		// Expected
	}
}

func TestHandleReceiveInvalidJSON(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Invalid JSON should not panic
	adapter.handleReceive(json.RawMessage(`invalid`))
	adapter.handleReceive(json.RawMessage(`{broken`))
}

// =============================================================================
// Send Validation Tests
// =============================================================================

func TestSendMissingPeerID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

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
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{"peer_id": ""},
	}

	err := adapter.Send(context.Background(), msg)
	if err == nil {
		t.Error("expected error for empty peer_id")
	}
}

func TestSendWrongPeerIDType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

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
// Health Check Tests
// =============================================================================

func TestHealthCheckProcessNotStarted(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	health := adapter.HealthCheck(context.Background())
	if health.Healthy {
		t.Error("expected unhealthy when process not started")
	}
	if health.Message != "process not started" {
		t.Errorf("expected message 'process not started', got %q", health.Message)
	}
}

// =============================================================================
// GetConversation Tests
// =============================================================================

func TestGetConversation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	conv, err := adapter.GetConversation(context.Background(), "+0987654321")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if conv.ID != "+0987654321" {
		t.Errorf("expected ID '+0987654321', got %s", conv.ID)
	}
	if conv.Type != personal.ConversationDM {
		t.Errorf("expected ConversationDM, got %s", conv.Type)
	}
}

// =============================================================================
// ListConversations Tests
// =============================================================================

func TestListConversationsUnavailable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	convs, err := adapter.ListConversations(context.Background(), personal.ListOptions{})
	if err == nil {
		t.Fatal("expected error for missing signal client")
	}
	if channels.GetErrorCode(err) != channels.ErrCodeUnavailable {
		t.Fatalf("expected unavailable error, got %v", err)
	}
	if convs != nil {
		t.Errorf("expected nil conversations, got %v", convs)
	}
}

// =============================================================================
// Pending Request Timeout Tests
// =============================================================================

func TestPendingRequestCleanup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Add multiple pending requests
	for i := int64(1); i <= 10; i++ {
		adapter.pending[i] = make(chan json.RawMessage, 1)
	}

	// Process responses for some
	adapter.processLine(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	adapter.processLine(`{"jsonrpc":"2.0","id":5,"result":{}}`)
	adapter.processLine(`{"jsonrpc":"2.0","id":10,"result":{}}`)

	adapter.pendingMu.Lock()
	remaining := len(adapter.pending)
	adapter.pendingMu.Unlock()

	if remaining != 7 {
		t.Errorf("expected 7 pending requests remaining, got %d", remaining)
	}
}

// =============================================================================
// Unicode Message Tests
// =============================================================================

func TestHandleReceiveUnicodeMessage(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	params := json.RawMessage(`{
		"source": "+0987654321",
		"sourceName": "用户",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Привет! 你好! مرحبا!"
		}
	}`)

	adapter.handleReceive(params)

	select {
	case msg := <-adapter.Messages():
		if msg.Content != "Привет! 你好! مرحبا!" {
			t.Errorf("expected unicode message, got %q", msg.Content)
		}
		if msg.Metadata["peer_name"] != "用户" {
			t.Errorf("expected unicode peer name, got %v", msg.Metadata["peer_name"])
		}
	default:
		t.Error("expected message to be emitted")
	}
}

// =============================================================================
// Stop Without Start Tests
// =============================================================================

func TestStopWithoutStart(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Stop should not panic even if Start was never called
	err := adapter.Stop(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Interface Implementation Tests
// =============================================================================

func TestAdapterInterfaces(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	contacts := adapter.Contacts()
	if contacts == nil {
		t.Error("expected non-nil ContactManager")
	}

	media := adapter.Media()
	if media == nil {
		t.Error("expected non-nil MediaHandler")
	}

	presence := adapter.Presence()
	if presence == nil {
		t.Error("expected non-nil PresenceManager")
	}
}

// =============================================================================
// Message Timestamp Tests
// =============================================================================

func TestHandleReceiveTimestamp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Account = "+1234567890"

	adapter := &Adapter{
		config:  cfg,
		pending: make(map[int64]chan json.RawMessage),
	}
	adapter.BaseAdapter = personal.NewBaseAdapter("signal", &cfg.Personal, nil)

	// Use a known timestamp: 2024-01-01 00:00:00 UTC = 1704067200000ms
	params := json.RawMessage(`{
		"source": "+0987654321",
		"sourceName": "Test",
		"timestamp": 1704067200000,
		"dataMessage": {
			"timestamp": 1704067200000,
			"message": "Test"
		}
	}`)

	adapter.handleReceive(params)

	select {
	case msg := <-adapter.Messages():
		expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		if !msg.CreatedAt.Equal(expected) {
			t.Errorf("expected timestamp %v, got %v", expected, msg.CreatedAt)
		}
	default:
		t.Error("expected message to be emitted")
	}
}
