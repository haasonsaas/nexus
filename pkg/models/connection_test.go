package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestConnectionStatus_Constants(t *testing.T) {
	tests := []struct {
		constant ConnectionStatus
		expected string
	}{
		{ConnectionStatusUnspecified, "unspecified"},
		{ConnectionStatusConnected, "connected"},
		{ConnectionStatusDisconnected, "disconnected"},
		{ConnectionStatusError, "error"},
		{ConnectionStatusConnecting, "connecting"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestChannelConnection_Struct(t *testing.T) {
	now := time.Now()
	conn := ChannelConnection{
		ID:             "conn-123",
		UserID:         "user-456",
		ChannelType:    ChannelSlack,
		ChannelID:      "slack-workspace-1",
		Status:         ConnectionStatusConnected,
		Config:         map[string]any{"token": "secret"},
		ConnectedAt:    now,
		LastActivityAt: now,
	}

	if conn.ID != "conn-123" {
		t.Errorf("ID = %q, want %q", conn.ID, "conn-123")
	}
	if conn.ChannelType != ChannelSlack {
		t.Errorf("ChannelType = %v, want %v", conn.ChannelType, ChannelSlack)
	}
	if conn.Status != ConnectionStatusConnected {
		t.Errorf("Status = %v, want %v", conn.Status, ConnectionStatusConnected)
	}
}

func TestChannelConnection_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := ChannelConnection{
		ID:             "conn-123",
		UserID:         "user-456",
		ChannelType:    ChannelDiscord,
		ChannelID:      "discord-server-1",
		Status:         ConnectionStatusConnecting,
		Config:         map[string]any{"guild_id": "12345"},
		ConnectedAt:    now,
		LastActivityAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded ChannelConnection
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.ChannelType != original.ChannelType {
		t.Errorf("ChannelType = %v, want %v", decoded.ChannelType, original.ChannelType)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %v, want %v", decoded.Status, original.Status)
	}
	if decoded.Config["guild_id"] != "12345" {
		t.Errorf("Config[guild_id] = %v, want %q", decoded.Config["guild_id"], "12345")
	}
}

func TestChannelConnection_DifferentStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status ConnectionStatus
	}{
		{"unspecified", ConnectionStatusUnspecified},
		{"connected", ConnectionStatusConnected},
		{"disconnected", ConnectionStatusDisconnected},
		{"error", ConnectionStatusError},
		{"connecting", ConnectionStatusConnecting},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := ChannelConnection{
				ID:          "conn-" + tt.name,
				ChannelType: ChannelAPI,
				Status:      tt.status,
			}
			if conn.Status != tt.status {
				t.Errorf("Status = %v, want %v", conn.Status, tt.status)
			}
		})
	}
}
