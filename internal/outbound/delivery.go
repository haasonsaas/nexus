// Package outbound provides utilities for formatting outbound delivery results.
package outbound

import "fmt"

// DeliveryVia indicates how a message was delivered.
type DeliveryVia string

const (
	// DeliveryViaDirect indicates direct delivery to the channel.
	DeliveryViaDirect DeliveryVia = "direct"
	// DeliveryViaGateway indicates delivery through a gateway.
	DeliveryViaGateway DeliveryVia = "gateway"
)

// OutboundDeliveryJSON represents the JSON structure of a delivery result.
type OutboundDeliveryJSON struct {
	Channel        string         `json:"channel"`
	Via            DeliveryVia    `json:"via"`
	To             string         `json:"to"`
	MessageID      string         `json:"messageId"`
	MediaURL       *string        `json:"mediaUrl"`
	ChatID         string         `json:"chatId,omitempty"`
	ChannelID      string         `json:"channelId,omitempty"`
	RoomID         string         `json:"roomId,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	Timestamp      *int64         `json:"timestamp,omitempty"`
	ToJid          string         `json:"toJid,omitempty"`
	Meta           map[string]any `json:"meta,omitempty"`
}

// DeliveryResult contains the result of a message delivery.
type DeliveryResult struct {
	MessageID      string
	ChatID         string
	ChannelID      string
	RoomID         string
	ConversationID string
	Timestamp      *int64
	ToJid          string
	Meta           map[string]any
}

// FormatDeliverySummary formats a delivery summary with the channel and result.
// Returns a string like "Sent via {channel}. Message ID: {id}" with optional context.
func FormatDeliverySummary(channel string, result *DeliveryResult) string {
	if result == nil {
		return fmt.Sprintf("Sent via %s. Message ID: unknown", channel)
	}

	messageID := result.MessageID
	if messageID == "" {
		messageID = "unknown"
	}

	base := fmt.Sprintf("Sent via %s. Message ID: %s", channel, messageID)

	if result.ChatID != "" {
		return fmt.Sprintf("%s (chat %s)", base, result.ChatID)
	}
	if result.ChannelID != "" {
		return fmt.Sprintf("%s (channel %s)", base, result.ChannelID)
	}
	if result.RoomID != "" {
		return fmt.Sprintf("%s (room %s)", base, result.RoomID)
	}
	if result.ConversationID != "" {
		return fmt.Sprintf("%s (conversation %s)", base, result.ConversationID)
	}
	return base
}

// BuildDeliveryJSONParams contains parameters for building a delivery JSON.
type BuildDeliveryJSONParams struct {
	Channel  string
	To       string
	Result   *DeliveryResult
	Via      DeliveryVia
	MediaURL *string
}

// BuildDeliveryJSON constructs an OutboundDeliveryJSON from the given parameters.
func BuildDeliveryJSON(params BuildDeliveryJSONParams) OutboundDeliveryJSON {
	messageID := "unknown"
	if params.Result != nil && params.Result.MessageID != "" {
		messageID = params.Result.MessageID
	}

	via := params.Via
	if via == "" {
		via = DeliveryViaDirect
	}

	payload := OutboundDeliveryJSON{
		Channel:   params.Channel,
		Via:       via,
		To:        params.To,
		MessageID: messageID,
		MediaURL:  params.MediaURL,
	}

	if params.Result != nil {
		if params.Result.ChatID != "" {
			payload.ChatID = params.Result.ChatID
		}
		if params.Result.ChannelID != "" {
			payload.ChannelID = params.Result.ChannelID
		}
		if params.Result.RoomID != "" {
			payload.RoomID = params.Result.RoomID
		}
		if params.Result.ConversationID != "" {
			payload.ConversationID = params.Result.ConversationID
		}
		if params.Result.Timestamp != nil {
			payload.Timestamp = params.Result.Timestamp
		}
		if params.Result.ToJid != "" {
			payload.ToJid = params.Result.ToJid
		}
		if params.Result.Meta != nil {
			payload.Meta = params.Result.Meta
		}
	}

	return payload
}

// FormatGatewaySummaryParams contains parameters for formatting a gateway summary.
type FormatGatewaySummaryParams struct {
	Action    string
	Channel   string
	MessageID string
}

// FormatGatewaySummary formats a gateway delivery summary.
// Returns a string like "{action} via gateway. Message ID: {id}".
func FormatGatewaySummary(params FormatGatewaySummaryParams) string {
	action := params.Action
	if action == "" {
		action = "Sent"
	}

	channelSuffix := ""
	if params.Channel != "" {
		channelSuffix = fmt.Sprintf(" (%s)", params.Channel)
	}

	messageID := params.MessageID
	if messageID == "" {
		messageID = "unknown"
	}

	return fmt.Sprintf("%s via gateway%s. Message ID: %s", action, channelSuffix, messageID)
}
