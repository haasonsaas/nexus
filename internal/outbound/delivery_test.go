package outbound

import (
	"testing"
)

func TestFormatDeliverySummary(t *testing.T) {
	tests := []struct {
		name     string
		channel  string
		result   *DeliveryResult
		expected string
	}{
		{
			name:     "nil result",
			channel:  "slack",
			result:   nil,
			expected: "Sent via slack. Message ID: unknown",
		},
		{
			name:    "basic result with message ID",
			channel: "telegram",
			result: &DeliveryResult{
				MessageID: "msg-123",
			},
			expected: "Sent via telegram. Message ID: msg-123",
		},
		{
			name:    "result with empty message ID",
			channel: "discord",
			result: &DeliveryResult{
				MessageID: "",
			},
			expected: "Sent via discord. Message ID: unknown",
		},
		{
			name:    "result with chat ID context",
			channel: "telegram",
			result: &DeliveryResult{
				MessageID: "msg-456",
				ChatID:    "chat-789",
			},
			expected: "Sent via telegram. Message ID: msg-456 (chat chat-789)",
		},
		{
			name:    "result with channel ID context",
			channel: "slack",
			result: &DeliveryResult{
				MessageID: "msg-101",
				ChannelID: "C1234567",
			},
			expected: "Sent via slack. Message ID: msg-101 (channel C1234567)",
		},
		{
			name:    "result with room ID context",
			channel: "matrix",
			result: &DeliveryResult{
				MessageID: "event-202",
				RoomID:    "!room:matrix.org",
			},
			expected: "Sent via matrix. Message ID: event-202 (room !room:matrix.org)",
		},
		{
			name:    "result with conversation ID context",
			channel: "teams",
			result: &DeliveryResult{
				MessageID:      "msg-303",
				ConversationID: "conv-404",
			},
			expected: "Sent via teams. Message ID: msg-303 (conversation conv-404)",
		},
		{
			name:    "priority: chat ID over channel ID",
			channel: "multi",
			result: &DeliveryResult{
				MessageID: "msg-500",
				ChatID:    "chat-first",
				ChannelID: "channel-second",
			},
			expected: "Sent via multi. Message ID: msg-500 (chat chat-first)",
		},
		{
			name:    "priority: channel ID over room ID",
			channel: "multi",
			result: &DeliveryResult{
				MessageID: "msg-600",
				ChannelID: "channel-first",
				RoomID:    "room-second",
			},
			expected: "Sent via multi. Message ID: msg-600 (channel channel-first)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDeliverySummary(tt.channel, tt.result)
			if got != tt.expected {
				t.Errorf("FormatDeliverySummary() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestBuildDeliveryJSON(t *testing.T) {
	tests := []struct {
		name   string
		params BuildDeliveryJSONParams
		check  func(t *testing.T, got OutboundDeliveryJSON)
	}{
		{
			name: "minimal params",
			params: BuildDeliveryJSONParams{
				Channel: "slack",
				To:      "user@example.com",
			},
			check: func(t *testing.T, got OutboundDeliveryJSON) {
				if got.Channel != "slack" {
					t.Errorf("Channel = %q, want %q", got.Channel, "slack")
				}
				if got.To != "user@example.com" {
					t.Errorf("To = %q, want %q", got.To, "user@example.com")
				}
				if got.Via != DeliveryViaDirect {
					t.Errorf("Via = %q, want %q", got.Via, DeliveryViaDirect)
				}
				if got.MessageID != "unknown" {
					t.Errorf("MessageID = %q, want %q", got.MessageID, "unknown")
				}
				if got.MediaURL != nil {
					t.Errorf("MediaURL = %v, want nil", got.MediaURL)
				}
			},
		},
		{
			name: "with via gateway",
			params: BuildDeliveryJSONParams{
				Channel: "telegram",
				To:      "+1234567890",
				Via:     DeliveryViaGateway,
			},
			check: func(t *testing.T, got OutboundDeliveryJSON) {
				if got.Via != DeliveryViaGateway {
					t.Errorf("Via = %q, want %q", got.Via, DeliveryViaGateway)
				}
			},
		},
		{
			name: "with media URL",
			params: BuildDeliveryJSONParams{
				Channel:  "discord",
				To:       "user123",
				MediaURL: strPtr("https://example.com/image.png"),
			},
			check: func(t *testing.T, got OutboundDeliveryJSON) {
				if got.MediaURL == nil {
					t.Error("MediaURL is nil, want non-nil")
				} else if *got.MediaURL != "https://example.com/image.png" {
					t.Errorf("MediaURL = %q, want %q", *got.MediaURL, "https://example.com/image.png")
				}
			},
		},
		{
			name: "with full result",
			params: BuildDeliveryJSONParams{
				Channel: "slack",
				To:      "user@slack.com",
				Result: &DeliveryResult{
					MessageID:      "msg-999",
					ChatID:         "chat-111",
					ChannelID:      "C222",
					RoomID:         "room-333",
					ConversationID: "conv-444",
					Timestamp:      int64Ptr(1699999999),
					ToJid:          "user@xmpp.local",
					Meta: map[string]any{
						"custom": "value",
					},
				},
			},
			check: func(t *testing.T, got OutboundDeliveryJSON) {
				if got.MessageID != "msg-999" {
					t.Errorf("MessageID = %q, want %q", got.MessageID, "msg-999")
				}
				if got.ChatID != "chat-111" {
					t.Errorf("ChatID = %q, want %q", got.ChatID, "chat-111")
				}
				if got.ChannelID != "C222" {
					t.Errorf("ChannelID = %q, want %q", got.ChannelID, "C222")
				}
				if got.RoomID != "room-333" {
					t.Errorf("RoomID = %q, want %q", got.RoomID, "room-333")
				}
				if got.ConversationID != "conv-444" {
					t.Errorf("ConversationID = %q, want %q", got.ConversationID, "conv-444")
				}
				if got.Timestamp == nil || *got.Timestamp != 1699999999 {
					t.Errorf("Timestamp = %v, want 1699999999", got.Timestamp)
				}
				if got.ToJid != "user@xmpp.local" {
					t.Errorf("ToJid = %q, want %q", got.ToJid, "user@xmpp.local")
				}
				if got.Meta == nil || got.Meta["custom"] != "value" {
					t.Errorf("Meta = %v, want map with custom=value", got.Meta)
				}
			},
		},
		{
			name: "result with empty message ID uses unknown",
			params: BuildDeliveryJSONParams{
				Channel: "test",
				To:      "test",
				Result: &DeliveryResult{
					MessageID: "",
				},
			},
			check: func(t *testing.T, got OutboundDeliveryJSON) {
				if got.MessageID != "unknown" {
					t.Errorf("MessageID = %q, want %q", got.MessageID, "unknown")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDeliveryJSON(tt.params)
			tt.check(t, got)
		})
	}
}

func TestFormatGatewaySummary(t *testing.T) {
	tests := []struct {
		name     string
		params   FormatGatewaySummaryParams
		expected string
	}{
		{
			name:     "empty params uses defaults",
			params:   FormatGatewaySummaryParams{},
			expected: "Sent via gateway. Message ID: unknown",
		},
		{
			name: "with action",
			params: FormatGatewaySummaryParams{
				Action: "Forwarded",
			},
			expected: "Forwarded via gateway. Message ID: unknown",
		},
		{
			name: "with channel",
			params: FormatGatewaySummaryParams{
				Channel: "telegram",
			},
			expected: "Sent via gateway (telegram). Message ID: unknown",
		},
		{
			name: "with message ID",
			params: FormatGatewaySummaryParams{
				MessageID: "gw-msg-123",
			},
			expected: "Sent via gateway. Message ID: gw-msg-123",
		},
		{
			name: "all params",
			params: FormatGatewaySummaryParams{
				Action:    "Delivered",
				Channel:   "whatsapp",
				MessageID: "wa-456",
			},
			expected: "Delivered via gateway (whatsapp). Message ID: wa-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatGatewaySummary(tt.params)
			if got != tt.expected {
				t.Errorf("FormatGatewaySummary() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDeliveryViaConstants(t *testing.T) {
	if DeliveryViaDirect != "direct" {
		t.Errorf("DeliveryViaDirect = %q, want %q", DeliveryViaDirect, "direct")
	}
	if DeliveryViaGateway != "gateway" {
		t.Errorf("DeliveryViaGateway = %q, want %q", DeliveryViaGateway, "gateway")
	}
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}
