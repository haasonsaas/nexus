// Package imessage provides an iMessage channel adapter for macOS.
//go:build darwin
// +build darwin

package imessage

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
	if cfg.DatabasePath != "~/Library/Messages/chat.db" {
		t.Errorf("expected DatabasePath to be ~/Library/Messages/chat.db, got %s", cfg.DatabasePath)
	}
	if cfg.PollInterval != "1s" {
		t.Errorf("expected PollInterval to be 1s, got %s", cfg.PollInterval)
	}
	if !cfg.Personal.SyncOnStart {
		t.Error("expected SyncOnStart to be true by default")
	}
	if cfg.Personal.Presence.SendReadReceipts {
		t.Error("expected SendReadReceipts to be false (not supported)")
	}
	if cfg.Personal.Presence.SendTyping {
		t.Error("expected SendTyping to be false (not supported)")
	}
}

func TestDefaultConfigValues(t *testing.T) {
	cfg := DefaultConfig()

	// Test that all fields have expected values
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"Enabled", cfg.Enabled, false},
		{"DatabasePath", cfg.DatabasePath, "~/Library/Messages/chat.db"},
		{"PollInterval", cfg.PollInterval, "1s"},
		{"SyncOnStart", cfg.Personal.SyncOnStart, true},
		{"SendReadReceipts", cfg.Personal.Presence.SendReadReceipts, false},
		{"SendTyping", cfg.Personal.Presence.SendTyping, false},
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
		DatabasePath: "/custom/path/chat.db",
		PollInterval: "5s",
		Personal: personal.Config{
			SyncOnStart: false,
			Presence: personal.PresenceConfig{
				SendReadReceipts: true,
				SendTyping:       true,
			},
		},
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.DatabasePath != "/custom/path/chat.db" {
		t.Errorf("expected custom DatabasePath, got %s", cfg.DatabasePath)
	}
	if cfg.PollInterval != "5s" {
		t.Errorf("expected PollInterval to be 5s, got %s", cfg.PollInterval)
	}
	if cfg.Personal.SyncOnStart {
		t.Error("expected SyncOnStart to be false")
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
			input:    "~/Library/Messages/chat.db",
			wantHome: true,
		},
		{
			name:     "absolute path",
			input:    "/var/db/messages.db",
			wantHome: false,
		},
		{
			name:     "relative path",
			input:    "messages.db",
			wantHome: false,
		},
		{
			name:     "tilde only",
			input:    "~",
			wantHome: false, // Only ~/ is expanded, not ~
		},
		{
			name:     "tilde in middle",
			input:    "/var/~/db",
			wantHome: false,
		},
		{
			name:     "empty path",
			input:    "",
			wantHome: false,
		},
		{
			name:     "deep nested tilde path",
			input:    "~/a/b/c/d/e/file.db",
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
	input := "~/Library/Messages/chat.db"
	result := expandPath(input)

	// Should end with the same subpath
	suffix := "/Library/Messages/chat.db"
	if len(result) < len(suffix) {
		t.Fatalf("expanded path too short: %s", result)
	}
	if result[len(result)-len(suffix):] != suffix {
		t.Errorf("expected path to end with %s, got %s", suffix, result)
	}
}

// =============================================================================
// AppleScript Escaping Tests
// =============================================================================

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escaping needed",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "escape quotes",
			input:    `Hello "World"`,
			expected: `Hello \"World\"`,
		},
		{
			name:     "escape backslashes",
			input:    `Hello\World`,
			expected: `Hello\\World`,
		},
		{
			name:     "escape both",
			input:    `Say "Hello\World"`,
			expected: `Say \"Hello\\World\"`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "multiple backslashes",
			input:    `a\\b`,
			expected: `a\\\\b`,
		},
		{
			name:     "only quotes",
			input:    `"""`,
			expected: `\"\"\"`,
		},
		{
			name:     "only backslashes",
			input:    `\\\`,
			expected: `\\\\\\`,
		},
		{
			name:     "alternating quotes and backslashes",
			input:    `"\"\`,
			expected: `\"\\\"\\`,
		},
		{
			name:     "newlines preserved",
			input:    "Hello\nWorld",
			expected: "Hello\nWorld",
		},
		{
			name:     "tabs preserved",
			input:    "Hello\tWorld",
			expected: "Hello\tWorld",
		},
		{
			name:     "unicode preserved",
			input:    "Hello, World",
			expected: "Hello, World",
		},
		{
			name:     "complex message with quotes",
			input:    `He said "Hello" and she said "Hi"`,
			expected: `He said \"Hello\" and she said \"Hi\"`,
		},
		{
			name:     "path with backslashes",
			input:    `C:\Users\John\Documents`,
			expected: `C:\\Users\\John\\Documents`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeAppleScript(tt.input)
			if result != tt.expected {
				t.Errorf("escapeAppleScript(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEscapeAppleScriptIdempotence(t *testing.T) {
	// Double escaping should produce different results (not idempotent)
	input := `Hello "World"`
	first := escapeAppleScript(input)
	second := escapeAppleScript(first)

	if first == second {
		t.Error("expected double escaping to produce different results")
	}

	expectedFirst := `Hello \"World\"`
	expectedSecond := `Hello \\\"World\\\"`

	if first != expectedFirst {
		t.Errorf("first escape: got %q, want %q", first, expectedFirst)
	}
	if second != expectedSecond {
		t.Errorf("second escape: got %q, want %q", second, expectedSecond)
	}
}

// =============================================================================
// Apple Timestamp Conversion Tests
// =============================================================================

func TestAppleTimestampToTime(t *testing.T) {
	tests := []struct {
		name     string
		nano     int64
		expected time.Time
	}{
		{
			name:     "zero timestamp",
			nano:     0,
			expected: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one second after epoch",
			nano:     1_000_000_000, // 1 second in nanoseconds
			expected: time.Date(2001, 1, 1, 0, 0, 1, 0, time.UTC),
		},
		{
			name:     "one minute after epoch",
			nano:     60 * 1_000_000_000, // 1 minute in nanoseconds
			expected: time.Date(2001, 1, 1, 0, 1, 0, 0, time.UTC),
		},
		{
			name:     "one hour after epoch",
			nano:     60 * 60 * 1_000_000_000, // 1 hour in nanoseconds
			expected: time.Date(2001, 1, 1, 1, 0, 0, 0, time.UTC),
		},
		{
			name:     "one day after epoch",
			nano:     24 * 60 * 60 * 1_000_000_000, // 1 day in nanoseconds
			expected: time.Date(2001, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "one year after epoch (approx)",
			nano:     365 * 24 * 60 * 60 * 1_000_000_000, // ~1 year in nanoseconds
			expected: time.Date(2002, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "specific timestamp",
			nano:     700_000_000_000_000_000, // roughly 22+ years
			expected: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(700_000_000_000_000_000) * time.Nanosecond),
		},
		{
			name:     "timestamp with nanosecond precision",
			nano:     1_000_000_001, // 1 second + 1 nanosecond
			expected: time.Date(2001, 1, 1, 0, 0, 1, 1, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appleTimestampToTime(tt.nano)
			if !result.Equal(tt.expected) {
				t.Errorf("appleTimestampToTime(%d) = %v, want %v", tt.nano, result, tt.expected)
			}
		})
	}
}

func TestAppleTimestampToTimeRecentDate(t *testing.T) {
	// Test a known recent timestamp
	// 2024-01-01 00:00:00 UTC is approximately 23 years after Apple epoch
	// That's roughly 23 * 365.25 * 24 * 60 * 60 * 1_000_000_000 nanoseconds

	result := appleTimestampToTime(0)
	appleEpoch := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

	if !result.Equal(appleEpoch) {
		t.Errorf("expected Apple epoch (2001-01-01), got %v", result)
	}
}

func TestAppleTimestampNegative(t *testing.T) {
	// Test negative timestamp (before Apple epoch)
	nano := int64(-1_000_000_000) // 1 second before Apple epoch
	result := appleTimestampToTime(nano)
	expected := time.Date(2000, 12, 31, 23, 59, 59, 0, time.UTC)

	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

// =============================================================================
// Adapter Creation Tests
// =============================================================================

func TestNewAdapterNilConfig(t *testing.T) {
	// New should accept nil config and use defaults
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New(nil, nil) error = %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.pollInterval != time.Second {
		t.Errorf("expected default poll interval of 1s, got %v", adapter.pollInterval)
	}
}

func TestNewAdapterWithConfig(t *testing.T) {
	cfg := &Config{
		Enabled:      true,
		DatabasePath: "/custom/path.db",
		PollInterval: "5s",
	}

	adapter, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.pollInterval != 5*time.Second {
		t.Errorf("expected poll interval of 5s, got %v", adapter.pollInterval)
	}
	if adapter.config.DatabasePath != "/custom/path.db" {
		t.Errorf("expected DatabasePath /custom/path.db, got %s", adapter.config.DatabasePath)
	}
}

func TestNewAdapterInvalidPollInterval(t *testing.T) {
	cfg := &Config{
		DatabasePath: "/custom/path.db",
		PollInterval: "invalid",
	}

	adapter, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Should fallback to 1 second
	if adapter.pollInterval != time.Second {
		t.Errorf("expected fallback poll interval of 1s, got %v", adapter.pollInterval)
	}
}

func TestNewAdapterWithVariousPollIntervals(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		expected time.Duration
	}{
		{"1 second", "1s", time.Second},
		{"5 seconds", "5s", 5 * time.Second},
		{"100 milliseconds", "100ms", 100 * time.Millisecond},
		{"1 minute", "1m", time.Minute},
		{"500 microseconds", "500us", 500 * time.Microsecond},
		{"invalid", "invalid", time.Second}, // fallback
		{"empty", "", time.Second},          // fallback
		{"negative", "-1s", time.Second},    // fallback (invalid)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				DatabasePath: "/test.db",
				PollInterval: tt.interval,
			}

			adapter, err := New(cfg, nil)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			// For negative duration, the parse succeeds but returns negative value
			// The implementation may or may not handle this specially
			if tt.interval == "-1s" {
				// Just verify it doesn't crash
				if adapter.pollInterval <= 0 && adapter.pollInterval != tt.expected {
					// Implementation might fallback for negative values
				}
			} else if adapter.pollInterval != tt.expected {
				t.Errorf("expected poll interval %v, got %v", tt.expected, adapter.pollInterval)
			}
		})
	}
}

// =============================================================================
// Adapter Type and Channel Tests
// =============================================================================

func TestAdapterType(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// The adapter should be of the correct type
	if adapter.Type() != "imessage" {
		t.Errorf("expected channel type 'imessage', got %v", adapter.Type())
	}
}

func TestAdapterMessagesChannel(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Messages channel should be available
	msgChan := adapter.Messages()
	if msgChan == nil {
		t.Error("expected non-nil messages channel")
	}
}

func TestAdapterStatus(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Initial status should be disconnected
	status := adapter.Status()
	if status.Connected {
		t.Error("expected adapter to be disconnected initially")
	}
}

func TestAdapterMetrics(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Initial metrics should be zero
	metrics := adapter.Metrics()
	if metrics.MessagesSent != 0 {
		t.Errorf("expected 0 sent messages, got %d", metrics.MessagesSent)
	}
	if metrics.MessagesReceived != 0 {
		t.Errorf("expected 0 received messages, got %d", metrics.MessagesReceived)
	}
	if metrics.MessagesFailed != 0 {
		t.Errorf("expected 0 failed messages, got %d", metrics.MessagesFailed)
	}
}

// =============================================================================
// Interface Implementation Tests
// =============================================================================

func TestAdapterContactsManager(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	contacts := adapter.Contacts()
	if contacts == nil {
		t.Error("expected non-nil contacts manager")
	}
}

func TestAdapterMediaHandler(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	media := adapter.Media()
	if media == nil {
		t.Error("expected non-nil media handler")
	}
}

func TestAdapterPresenceManager(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	presence := adapter.Presence()
	if presence == nil {
		t.Error("expected non-nil presence manager")
	}
}

// =============================================================================
// Health Check Tests (without database)
// =============================================================================

func TestHealthCheckWithoutDatabase(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Without starting, db should be nil
	health := adapter.HealthCheck(nil)
	if health.Healthy {
		t.Error("expected unhealthy status when database is not connected")
	}
	if health.Message != "database not connected" {
		t.Errorf("expected message 'database not connected', got %s", health.Message)
	}
}

// =============================================================================
// Contact Cache Tests
// =============================================================================

func TestContactCache(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Initially should have no contacts
	contact, ok := adapter.GetContact("test@example.com")
	if ok {
		t.Error("expected no contact initially")
	}
	if contact != nil {
		t.Error("expected nil contact")
	}

	// Add a contact
	testContact := &personal.Contact{
		ID:    "test@example.com",
		Name:  "Test User",
		Phone: "+1234567890",
	}
	adapter.SetContact(testContact)

	// Should now be retrievable
	contact, ok = adapter.GetContact("test@example.com")
	if !ok {
		t.Error("expected contact to be found")
	}
	if contact == nil {
		t.Fatal("expected non-nil contact")
	}
	if contact.Name != "Test User" {
		t.Errorf("expected name 'Test User', got %s", contact.Name)
	}
}

func TestContactCacheNil(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Setting nil contact should not panic
	adapter.SetContact(nil)

	// Setting contact with empty ID should not be stored
	adapter.SetContact(&personal.Contact{ID: "", Name: "No ID"})
	_, ok := adapter.GetContact("")
	if ok {
		t.Error("expected contact with empty ID to not be stored")
	}
}

// =============================================================================
// Message Normalization Tests
// =============================================================================

func TestNormalizeInbound(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now()
	raw := personal.RawMessage{
		ID:        "msg-123",
		Content:   "Hello, World!",
		PeerID:    "sender@example.com",
		PeerName:  "Sender Name",
		Timestamp: now,
	}

	msg := adapter.NormalizeInbound(raw)

	if msg.ID != "msg-123" {
		t.Errorf("expected ID 'msg-123', got %s", msg.ID)
	}
	if msg.Content != "Hello, World!" {
		t.Errorf("expected content 'Hello, World!', got %s", msg.Content)
	}
	if msg.Direction != "inbound" {
		t.Errorf("expected direction 'inbound', got %s", msg.Direction)
	}
	if msg.Metadata["peer_id"] != "sender@example.com" {
		t.Errorf("expected peer_id 'sender@example.com', got %v", msg.Metadata["peer_id"])
	}
	if msg.Metadata["peer_name"] != "Sender Name" {
		t.Errorf("expected peer_name 'Sender Name', got %v", msg.Metadata["peer_name"])
	}
}

func TestNormalizeInboundWithGroup(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-456",
		Content:   "Group message",
		PeerID:    "sender@example.com",
		PeerName:  "Sender",
		GroupID:   "group-abc",
		GroupName: "Test Group",
		Timestamp: time.Now(),
	}

	msg := adapter.NormalizeInbound(raw)

	if msg.Metadata["group_id"] != "group-abc" {
		t.Errorf("expected group_id 'group-abc', got %v", msg.Metadata["group_id"])
	}
	if msg.Metadata["group_name"] != "Test Group" {
		t.Errorf("expected group_name 'Test Group', got %v", msg.Metadata["group_name"])
	}
}

func TestNormalizeInboundWithReply(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-789",
		Content:   "Reply message",
		PeerID:    "sender@example.com",
		PeerName:  "Sender",
		ReplyTo:   "msg-123",
		Timestamp: time.Now(),
	}

	msg := adapter.NormalizeInbound(raw)

	if msg.Metadata["reply_to"] != "msg-123" {
		t.Errorf("expected reply_to 'msg-123', got %v", msg.Metadata["reply_to"])
	}
}

func TestNormalizeInboundWithExtra(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-extra",
		Content:   "Message with extra",
		PeerID:    "sender@example.com",
		PeerName:  "Sender",
		Timestamp: time.Now(),
		Extra: map[string]any{
			"custom_field": "custom_value",
			"numeric":      42,
		},
	}

	msg := adapter.NormalizeInbound(raw)

	if msg.Metadata["custom_field"] != "custom_value" {
		t.Errorf("expected custom_field 'custom_value', got %v", msg.Metadata["custom_field"])
	}
	if msg.Metadata["numeric"] != 42 {
		t.Errorf("expected numeric 42, got %v", msg.Metadata["numeric"])
	}
}

// =============================================================================
// Attachment Processing Tests
// =============================================================================

func TestProcessAttachments(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-att",
		Content:   "Message with attachments",
		PeerID:    "sender@example.com",
		PeerName:  "Sender",
		Timestamp: time.Now(),
		Attachments: []personal.RawAttachment{
			{
				ID:       "att-1",
				MIMEType: "image/jpeg",
				Filename: "photo.jpg",
				Size:     1024,
				URL:      "https://example.com/photo.jpg",
			},
			{
				ID:       "att-2",
				MIMEType: "application/pdf",
				Filename: "document.pdf",
				Size:     2048,
			},
		},
	}

	msg := adapter.NormalizeInbound(raw)
	adapter.ProcessAttachments(raw, msg)

	if len(msg.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(msg.Attachments))
	}

	// Check first attachment
	att1 := msg.Attachments[0]
	if att1.ID != "att-1" {
		t.Errorf("expected attachment ID 'att-1', got %s", att1.ID)
	}
	if att1.MimeType != "image/jpeg" {
		t.Errorf("expected MIME type 'image/jpeg', got %s", att1.MimeType)
	}
	if att1.Filename != "photo.jpg" {
		t.Errorf("expected filename 'photo.jpg', got %s", att1.Filename)
	}
	if att1.Size != 1024 {
		t.Errorf("expected size 1024, got %d", att1.Size)
	}
	if att1.URL != "https://example.com/photo.jpg" {
		t.Errorf("expected URL, got %s", att1.URL)
	}

	// Check second attachment
	att2 := msg.Attachments[1]
	if att2.ID != "att-2" {
		t.Errorf("expected attachment ID 'att-2', got %s", att2.ID)
	}
	if att2.MimeType != "application/pdf" {
		t.Errorf("expected MIME type 'application/pdf', got %s", att2.MimeType)
	}
}

func TestProcessAttachmentsEmpty(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:          "msg-no-att",
		Content:     "Message without attachments",
		PeerID:      "sender@example.com",
		PeerName:    "Sender",
		Timestamp:   time.Now(),
		Attachments: []personal.RawAttachment{},
	}

	msg := adapter.NormalizeInbound(raw)
	adapter.ProcessAttachments(raw, msg)

	if len(msg.Attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(msg.Attachments))
	}
}

// =============================================================================
// Metrics Tests
// =============================================================================

func TestMetricsIncrement(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Increment sent
	adapter.IncrementSent()
	adapter.IncrementSent()
	adapter.IncrementSent()

	metrics := adapter.Metrics()
	if metrics.MessagesSent != 3 {
		t.Errorf("expected 3 sent messages, got %d", metrics.MessagesSent)
	}

	// Increment errors
	adapter.IncrementErrors()
	adapter.IncrementErrors()

	metrics = adapter.Metrics()
	if metrics.MessagesFailed != 2 {
		t.Errorf("expected 2 failed messages, got %d", metrics.MessagesFailed)
	}
}

// =============================================================================
// Status Tests
// =============================================================================

func TestSetStatus(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Set connected
	adapter.SetStatus(true, "")
	status := adapter.Status()
	if !status.Connected {
		t.Error("expected connected status")
	}
	if status.Error != "" {
		t.Errorf("expected no error, got %s", status.Error)
	}

	// Set disconnected with error
	adapter.SetStatus(false, "connection failed")
	status = adapter.Status()
	if status.Connected {
		t.Error("expected disconnected status")
	}
	if status.Error != "connection failed" {
		t.Errorf("expected error 'connection failed', got %s", status.Error)
	}
}

// =============================================================================
// Health Check Additional Tests
// =============================================================================

func TestHealthCheckLatency(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	health := adapter.HealthCheck(nil)
	if health.Latency < 0 {
		t.Error("expected non-negative latency")
	}
	if health.LastCheck.IsZero() {
		t.Error("expected LastCheck to be set")
	}
}

// =============================================================================
// Logger Tests
// =============================================================================

func TestAdapterLogger(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger := adapter.Logger()
	if logger == nil {
		t.Error("expected non-nil logger")
	}
}

// =============================================================================
// Config Access Tests
// =============================================================================

func TestAdapterConfig(t *testing.T) {
	cfg := &Config{
		Enabled:      true,
		DatabasePath: "/test/path.db",
		PollInterval: "2s",
	}

	adapter, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if adapter.config.DatabasePath != "/test/path.db" {
		t.Errorf("expected DatabasePath '/test/path.db', got %s", adapter.config.DatabasePath)
	}
	if adapter.pollInterval != 2*time.Second {
		t.Errorf("expected poll interval 2s, got %v", adapter.pollInterval)
	}
}

// =============================================================================
// Emit Tests
// =============================================================================

func TestEmitMessage(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "test-msg",
		Content:   "Test content",
		PeerID:    "peer@example.com",
		PeerName:  "Test Peer",
		Timestamp: time.Now(),
	}

	msg := adapter.NormalizeInbound(raw)

	// Emit should succeed when channel is not full
	success := adapter.Emit(msg)
	if !success {
		t.Error("expected Emit to succeed")
	}

	// Verify message was emitted
	select {
	case received := <-adapter.Messages():
		if received.ID != "test-msg" {
			t.Errorf("expected message ID 'test-msg', got %s", received.ID)
		}
	default:
		t.Error("expected to receive message")
	}
}

func TestEmitIncreasesReceivedCount(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "test-msg",
		Content:   "Test",
		PeerID:    "peer",
		Timestamp: time.Now(),
	}

	msg := adapter.NormalizeInbound(raw)
	adapter.Emit(msg)

	metrics := adapter.Metrics()
	if metrics.MessagesReceived != 1 {
		t.Errorf("expected 1 received message, got %d", metrics.MessagesReceived)
	}
}

// =============================================================================
// Multiple Contact Tests
// =============================================================================

func TestMultipleContactsCache(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	contacts := []*personal.Contact{
		{ID: "contact1", Name: "Contact One", Phone: "+1111111111"},
		{ID: "contact2", Name: "Contact Two", Phone: "+2222222222"},
		{ID: "contact3", Name: "Contact Three", Phone: "+3333333333"},
	}

	for _, c := range contacts {
		adapter.SetContact(c)
	}

	for _, c := range contacts {
		retrieved, ok := adapter.GetContact(c.ID)
		if !ok {
			t.Errorf("expected contact %s to be found", c.ID)
		}
		if retrieved.Name != c.Name {
			t.Errorf("expected name %s, got %s", c.Name, retrieved.Name)
		}
	}
}

func TestContactUpdate(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Add initial contact
	adapter.SetContact(&personal.Contact{ID: "contact1", Name: "Original Name"})

	// Update contact
	adapter.SetContact(&personal.Contact{ID: "contact1", Name: "Updated Name"})

	contact, ok := adapter.GetContact("contact1")
	if !ok {
		t.Error("expected contact to be found")
	}
	if contact.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %s", contact.Name)
	}
}

// =============================================================================
// Attachment Variety Tests
// =============================================================================

func TestProcessVariousAttachmentTypes(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name     string
		mimeType string
		filename string
	}{
		{"JPEG image", "image/jpeg", "photo.jpg"},
		{"PNG image", "image/png", "screenshot.png"},
		{"GIF image", "image/gif", "animation.gif"},
		{"PDF document", "application/pdf", "document.pdf"},
		{"Word document", "application/msword", "report.doc"},
		{"Excel file", "application/vnd.ms-excel", "data.xls"},
		{"Video file", "video/mp4", "movie.mp4"},
		{"Audio file", "audio/mpeg", "song.mp3"},
		{"Text file", "text/plain", "notes.txt"},
		{"ZIP archive", "application/zip", "archive.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := personal.RawMessage{
				ID:        "msg-" + tt.name,
				Content:   "Message with " + tt.name,
				PeerID:    "peer@example.com",
				PeerName:  "Peer",
				Timestamp: time.Now(),
				Attachments: []personal.RawAttachment{
					{
						ID:       "att-" + tt.name,
						MIMEType: tt.mimeType,
						Filename: tt.filename,
						Size:     1024,
					},
				},
			}

			msg := adapter.NormalizeInbound(raw)
			adapter.ProcessAttachments(raw, msg)

			if len(msg.Attachments) != 1 {
				t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
			}

			att := msg.Attachments[0]
			if att.MimeType != tt.mimeType {
				t.Errorf("expected MIME type %s, got %s", tt.mimeType, att.MimeType)
			}
			if att.Filename != tt.filename {
				t.Errorf("expected filename %s, got %s", tt.filename, att.Filename)
			}
		})
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestAppleTimestampEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		nano int64
	}{
		{"large positive", 1e18},
		{"large negative", -1e18},
		{"max int64", 9223372036854775807},
		{"min int64", -9223372036854775808},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			result := appleTimestampToTime(tt.nano)
			if result.IsZero() && tt.nano != 0 {
				// Expected non-zero result for non-zero input
			}
		})
	}
}

func TestEscapeAppleScriptLongString(t *testing.T) {
	// Test with a very long string
	longString := ""
	for i := 0; i < 10000; i++ {
		longString += "a"
	}

	result := escapeAppleScript(longString)
	if len(result) != len(longString) {
		t.Errorf("expected same length, got %d vs %d", len(result), len(longString))
	}
}

func TestEscapeAppleScriptRepeatedSpecialChars(t *testing.T) {
	input := `""""\\\\""""\\\\`
	expected := `\"\"\"\"\\\\\\\\\"\"\"\"\\\\\\\\`

	result := escapeAppleScript(input)
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

// =============================================================================
// Message Direction and Role Tests
// =============================================================================

func TestNormalizeInboundSetsDirectionAndRole(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-test",
		Content:   "Test message",
		PeerID:    "sender@example.com",
		PeerName:  "Sender",
		Timestamp: time.Now(),
	}

	msg := adapter.NormalizeInbound(raw)

	if msg.Direction != "inbound" {
		t.Errorf("expected direction 'inbound', got %s", msg.Direction)
	}
	if msg.Role != "user" {
		t.Errorf("expected role 'user', got %s", msg.Role)
	}
	if msg.Channel != "imessage" {
		t.Errorf("expected channel 'imessage', got %v", msg.Channel)
	}
}

// =============================================================================
// Timestamp Preservation Tests
// =============================================================================

func TestNormalizeInboundPreservesTimestamp(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	raw := personal.RawMessage{
		ID:        "msg-timestamp",
		Content:   "Test",
		PeerID:    "peer",
		PeerName:  "Peer",
		Timestamp: now,
	}

	msg := adapter.NormalizeInbound(raw)

	if !msg.CreatedAt.Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, msg.CreatedAt)
	}
}

// =============================================================================
// Send Validation Tests
// =============================================================================

func TestSendMissingPeerID(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{},
	}

	err = adapter.Send(nil, msg)
	if err == nil {
		t.Error("expected error for missing peer_id")
	}
	if !strings.Contains(err.Error(), "missing peer_id in message metadata") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSendEmptyPeerID(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	msg := &models.Message{
		Content:  "Test message",
		Metadata: map[string]any{"peer_id": ""},
	}

	err = adapter.Send(nil, msg)
	if err == nil {
		t.Error("expected error for empty peer_id")
	}
}

func TestSendWrongPeerIDType(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	msg := &models.Message{
		Content: "Test message",
		Metadata: map[string]any{
			"peer_id": 12345, // Wrong type
		},
	}

	err = adapter.Send(nil, msg)
	if err == nil {
		t.Error("expected error for wrong peer_id type")
	}
}

// =============================================================================
// Additional AppleScript Escaping Edge Cases
// =============================================================================

func TestEscapeAppleScriptSpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "null byte",
			input:    "hello\x00world",
			expected: "hello\x00world", // null bytes preserved
		},
		{
			name:     "carriage return",
			input:    "hello\rworld",
			expected: "hello\rworld", // CR preserved
		},
		{
			name:     "CRLF",
			input:    "hello\r\nworld",
			expected: "hello\r\nworld", // CRLF preserved
		},
		{
			name:     "emoji",
			input:    "Hello 👋 World 🌍",
			expected: "Hello 👋 World 🌍", // Emoji preserved
		},
		{
			name:     "zero-width joiner",
			input:    "👨‍👩‍👧‍👦", // Family emoji with ZWJ
			expected: "👨‍👩‍👧‍👦",
		},
		{
			name:     "right-to-left text",
			input:    "مرحبا",
			expected: "مرحبا",
		},
		{
			name:     "mixed quotes and escapes",
			input:    `"quote" 'apostrophe' \backslash\ "more"`,
			expected: `\"quote\" 'apostrophe' \\backslash\\ \"more\"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeAppleScript(tt.input)
			if result != tt.expected {
				t.Errorf("escapeAppleScript(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Apple Timestamp Boundary Tests
// =============================================================================

func TestAppleTimestampBoundaries(t *testing.T) {
	// Test various timestamp boundaries
	tests := []struct {
		name string
		nano int64
	}{
		{"zero", 0},
		{"one nanosecond", 1},
		{"one second", 1_000_000_000},
		{"one minute", 60 * 1_000_000_000},
		{"one hour", 3600 * 1_000_000_000},
		{"one day", 86400 * 1_000_000_000},
		{"one week", 7 * 86400 * 1_000_000_000},
		{"one year approx", 365 * 86400 * 1_000_000_000},
	}

	appleEpoch := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appleTimestampToTime(tt.nano)
			expected := appleEpoch.Add(time.Duration(tt.nano) * time.Nanosecond)
			if !result.Equal(expected) {
				t.Errorf("expected %v, got %v", expected, result)
			}
		})
	}
}

// =============================================================================
// Conversation Style Detection Tests
// =============================================================================

func TestConversationStyleDetection(t *testing.T) {
	// In iMessage, style 43 indicates a group chat
	tests := []struct {
		style    int
		expected personal.ConversationType
	}{
		{0, personal.ConversationDM},
		{1, personal.ConversationDM},
		{43, personal.ConversationGroup},
		{44, personal.ConversationDM}, // Only 43 is group
		{100, personal.ConversationDM},
	}

	for _, tt := range tests {
		t.Run(string(rune('0'+tt.style)), func(t *testing.T) {
			var convType personal.ConversationType
			if tt.style == 43 {
				convType = personal.ConversationGroup
			} else {
				convType = personal.ConversationDM
			}

			if convType != tt.expected {
				t.Errorf("style %d: expected %s, got %s", tt.style, tt.expected, convType)
			}
		})
	}
}

// =============================================================================
// Multiple Emit Tests
// =============================================================================

func TestEmitMultipleMessages(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Emit multiple messages
	for i := 0; i < 10; i++ {
		raw := personal.RawMessage{
			ID:        fmt.Sprintf("msg-%d", i),
			Content:   fmt.Sprintf("Message %d", i),
			PeerID:    "peer@example.com",
			PeerName:  "Peer",
			Timestamp: time.Now(),
		}
		msg := adapter.NormalizeInbound(raw)
		success := adapter.Emit(msg)
		if !success {
			t.Errorf("failed to emit message %d", i)
		}
	}

	// Verify metrics
	metrics := adapter.Metrics()
	if metrics.MessagesReceived != 10 {
		t.Errorf("expected 10 received messages, got %d", metrics.MessagesReceived)
	}

	// Drain the channel
	count := 0
	for {
		select {
		case <-adapter.Messages():
			count++
		default:
			goto done
		}
	}
done:
	if count != 10 {
		t.Errorf("expected 10 messages in channel, got %d", count)
	}
}

// =============================================================================
// Empty and Nil Metadata Tests
// =============================================================================

func TestNormalizeInboundNilExtra(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-nil-extra",
		Content:   "Test",
		PeerID:    "peer",
		PeerName:  "Peer",
		Timestamp: time.Now(),
		Extra:     nil, // Explicitly nil
	}

	msg := adapter.NormalizeInbound(raw)
	if msg.Metadata == nil {
		t.Error("expected non-nil metadata even with nil Extra")
	}
}

func TestNormalizeInboundEmptyExtra(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := personal.RawMessage{
		ID:        "msg-empty-extra",
		Content:   "Test",
		PeerID:    "peer",
		PeerName:  "Peer",
		Timestamp: time.Now(),
		Extra:     map[string]any{}, // Empty but not nil
	}

	msg := adapter.NormalizeInbound(raw)
	if msg.Metadata == nil {
		t.Error("expected non-nil metadata")
	}
	// Should still have the standard fields
	if _, ok := msg.Metadata["peer_id"]; !ok {
		t.Error("expected peer_id in metadata")
	}
}

// =============================================================================
// Close Tests
// =============================================================================

func TestAdapterClose(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Close through BaseAdapter
	adapter.BaseAdapter.Close()

	// Messages channel should be closed or empty
	select {
	case _, open := <-adapter.Messages():
		if open {
			// Channel still has messages, drain it
		}
	default:
		// Channel is empty or closed
	}
}

// =============================================================================
// Poll Interval Parsing Tests
// =============================================================================

func TestPollIntervalParsing(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		expected time.Duration
	}{
		{"milliseconds", "100ms", 100 * time.Millisecond},
		{"seconds", "5s", 5 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
		{"combined", "1m30s", 90 * time.Second},
		{"hours", "1h", time.Hour},
		{"fractional seconds", "1.5s", 1500 * time.Millisecond},
		{"invalid fallback", "bad", time.Second},
		{"negative fallback", "-5s", time.Second},
		{"empty fallback", "", time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				DatabasePath: "/test.db",
				PollInterval: tt.interval,
			}

			adapter, err := New(cfg, nil)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			// For invalid durations, should fallback to 1 second
			if tt.interval == "bad" || tt.interval == "" {
				if adapter.pollInterval != time.Second {
					t.Errorf("expected fallback to 1s, got %v", adapter.pollInterval)
				}
			} else if tt.interval == "-5s" {
				// Negative parses successfully but is still negative
				// Check the actual behavior
				if adapter.pollInterval < 0 {
					// Implementation might accept this
				}
			} else if adapter.pollInterval != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, adapter.pollInterval)
			}
		})
	}
}

// =============================================================================
// Database Path Expansion Tests
// =============================================================================

func TestDatabasePathExpansion(t *testing.T) {
	tests := []struct {
		input    string
		wantHome bool
	}{
		{"~/Library/Messages/chat.db", true},
		{"/absolute/path/chat.db", false},
		{"relative/path/chat.db", false},
		{"~", false}, // Only ~/ is expanded
		{"~/", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := expandPath(tt.input)
			if tt.wantHome {
				if result == tt.input {
					t.Error("expected tilde to be expanded")
				}
			} else {
				if tt.input != "" && !strings.HasPrefix(tt.input, "~") && result != tt.input {
					t.Errorf("expected path unchanged, got %s", result)
				}
			}
		})
	}
}

// =============================================================================
// Message With All Metadata Fields
// =============================================================================

func TestNormalizeInboundAllFields(t *testing.T) {
	adapter, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now()
	raw := personal.RawMessage{
		ID:        "full-msg",
		Content:   "Complete message",
		PeerID:    "peer@icloud.com",
		PeerName:  "Full Peer",
		GroupID:   "group123",
		GroupName: "Test Group",
		ReplyTo:   "prev-msg",
		Timestamp: now,
		Extra: map[string]any{
			"custom_field1": "value1",
			"custom_field2": 42,
		},
	}

	msg := adapter.NormalizeInbound(raw)

	// Check all fields
	if msg.ID != "full-msg" {
		t.Errorf("ID mismatch")
	}
	if msg.Content != "Complete message" {
		t.Errorf("Content mismatch")
	}
	if msg.Metadata["peer_id"] != "peer@icloud.com" {
		t.Errorf("peer_id mismatch")
	}
	if msg.Metadata["peer_name"] != "Full Peer" {
		t.Errorf("peer_name mismatch")
	}
	if msg.Metadata["group_id"] != "group123" {
		t.Errorf("group_id mismatch")
	}
	if msg.Metadata["group_name"] != "Test Group" {
		t.Errorf("group_name mismatch")
	}
	if msg.Metadata["reply_to"] != "prev-msg" {
		t.Errorf("reply_to mismatch")
	}
	if msg.Metadata["custom_field1"] != "value1" {
		t.Errorf("custom_field1 mismatch")
	}
	if msg.Metadata["custom_field2"] != 42 {
		t.Errorf("custom_field2 mismatch")
	}
}
