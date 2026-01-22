package models

import "time"

// ConnectionStatus represents channel connection status.
type ConnectionStatus string

const (
	ConnectionStatusUnspecified  ConnectionStatus = "unspecified"
	ConnectionStatusConnected    ConnectionStatus = "connected"
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"
	ConnectionStatusError        ConnectionStatus = "error"
	ConnectionStatusConnecting   ConnectionStatus = "connecting"
)

// ChannelConnection tracks a user's connection to a messaging channel.
type ChannelConnection struct {
	ID             string           `json:"id"`
	UserID         string           `json:"user_id"`
	ChannelType    ChannelType      `json:"channel_type"`
	ChannelID      string           `json:"channel_id"`
	Status         ConnectionStatus `json:"status"`
	Config         map[string]any   `json:"config,omitempty"`
	ConnectedAt    time.Time        `json:"connected_at"`
	LastActivityAt time.Time        `json:"last_activity_at"`
}
