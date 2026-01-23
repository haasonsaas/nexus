package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChannelType_Constants(t *testing.T) {
	tests := []struct {
		constant ChannelType
		expected string
	}{
		{ChannelTelegram, "telegram"},
		{ChannelDiscord, "discord"},
		{ChannelSlack, "slack"},
		{ChannelAPI, "api"},
		{ChannelWhatsApp, "whatsapp"},
		{ChannelSignal, "signal"},
		{ChannelIMessage, "imessage"},
		{ChannelMatrix, "matrix"},
		{ChannelTeams, "teams"},
		{ChannelEmail, "email"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestDirection_Constants(t *testing.T) {
	if string(DirectionInbound) != "inbound" {
		t.Errorf("DirectionInbound = %q, want %q", DirectionInbound, "inbound")
	}
	if string(DirectionOutbound) != "outbound" {
		t.Errorf("DirectionOutbound = %q, want %q", DirectionOutbound, "outbound")
	}
}

func TestRole_Constants(t *testing.T) {
	tests := []struct {
		constant Role
		expected string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleSystem, "system"},
		{RoleTool, "tool"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestMessage_Struct(t *testing.T) {
	now := time.Now()
	msg := Message{
		ID:          "msg-123",
		SessionID:   "session-456",
		BranchID:    "branch-789",
		SequenceNum: 5,
		Channel:     ChannelSlack,
		ChannelID:   "slack-channel-id",
		Direction:   DirectionInbound,
		Role:        RoleUser,
		Content:     "Hello, world!",
		Metadata:    map[string]any{"key": "value"},
		CreatedAt:   now,
	}

	if msg.ID != "msg-123" {
		t.Errorf("ID = %q, want %q", msg.ID, "msg-123")
	}
	if msg.Channel != ChannelSlack {
		t.Errorf("Channel = %v, want %v", msg.Channel, ChannelSlack)
	}
	if msg.Direction != DirectionInbound {
		t.Errorf("Direction = %v, want %v", msg.Direction, DirectionInbound)
	}
	if msg.Role != RoleUser {
		t.Errorf("Role = %v, want %v", msg.Role, RoleUser)
	}
	if msg.SequenceNum != 5 {
		t.Errorf("SequenceNum = %d, want 5", msg.SequenceNum)
	}
}

func TestMessage_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := Message{
		ID:          "msg-123",
		SessionID:   "session-456",
		Channel:     ChannelTelegram,
		ChannelID:   "tg-123",
		Direction:   DirectionOutbound,
		Role:        RoleAssistant,
		Content:     "Hello!",
		Attachments: []Attachment{{ID: "att-1", Type: "image", URL: "http://example.com/img.png"}},
		ToolCalls:   []ToolCall{{ID: "tc-1", Name: "search", Input: json.RawMessage(`{"q":"test"}`)}},
		ToolResults: []ToolResult{{ToolCallID: "tc-1", Content: "result", IsError: false}},
		Metadata:    map[string]any{"source": "test"},
		CreatedAt:   now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Channel != original.Channel {
		t.Errorf("Channel = %v, want %v", decoded.Channel, original.Channel)
	}
	if len(decoded.Attachments) != 1 {
		t.Errorf("Attachments length = %d, want 1", len(decoded.Attachments))
	}
	if len(decoded.ToolCalls) != 1 {
		t.Errorf("ToolCalls length = %d, want 1", len(decoded.ToolCalls))
	}
	if len(decoded.ToolResults) != 1 {
		t.Errorf("ToolResults length = %d, want 1", len(decoded.ToolResults))
	}
}

func TestAttachment_Struct(t *testing.T) {
	att := Attachment{
		ID:       "att-123",
		Type:     "image",
		URL:      "http://example.com/image.png",
		Filename: "image.png",
		MimeType: "image/png",
		Size:     1024,
	}

	if att.ID != "att-123" {
		t.Errorf("ID = %q, want %q", att.ID, "att-123")
	}
	if att.Type != "image" {
		t.Errorf("Type = %q, want %q", att.Type, "image")
	}
	if att.Size != 1024 {
		t.Errorf("Size = %d, want 1024", att.Size)
	}
}

func TestToolCall_Struct(t *testing.T) {
	tc := ToolCall{
		ID:    "tc-123",
		Name:  "web_search",
		Input: json.RawMessage(`{"query": "test query"}`),
	}

	if tc.ID != "tc-123" {
		t.Errorf("ID = %q, want %q", tc.ID, "tc-123")
	}
	if tc.Name != "web_search" {
		t.Errorf("Name = %q, want %q", tc.Name, "web_search")
	}
}

func TestToolResult_Struct(t *testing.T) {
	tr := ToolResult{
		ToolCallID: "tc-123",
		Content:    "Search results here",
		IsError:    false,
	}

	if tr.ToolCallID != "tc-123" {
		t.Errorf("ToolCallID = %q, want %q", tr.ToolCallID, "tc-123")
	}
	if tr.IsError {
		t.Error("IsError should be false")
	}

	trError := ToolResult{
		ToolCallID: "tc-456",
		Content:    "Error occurred",
		IsError:    true,
	}
	if !trError.IsError {
		t.Error("IsError should be true")
	}
}

func TestSession_Struct(t *testing.T) {
	now := time.Now()
	session := Session{
		ID:        "session-123",
		AgentID:   "agent-456",
		Channel:   ChannelDiscord,
		ChannelID: "discord-channel",
		Key:       "unique-key",
		Title:     "Test Session",
		Metadata:  map[string]any{"test": true},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if session.ID != "session-123" {
		t.Errorf("ID = %q, want %q", session.ID, "session-123")
	}
	if session.Channel != ChannelDiscord {
		t.Errorf("Channel = %v, want %v", session.Channel, ChannelDiscord)
	}
}

func TestUser_Struct(t *testing.T) {
	now := time.Now()
	user := User{
		ID:         "user-123",
		Email:      "test@example.com",
		Name:       "Test User",
		AvatarURL:  "http://example.com/avatar.png",
		Provider:   "google",
		ProviderID: "google-123",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if user.ID != "user-123" {
		t.Errorf("ID = %q, want %q", user.ID, "user-123")
	}
	if user.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", user.Email, "test@example.com")
	}
}

func TestAgent_Struct(t *testing.T) {
	now := time.Now()
	agent := Agent{
		ID:           "agent-123",
		UserID:       "user-456",
		Name:         "Test Agent",
		SystemPrompt: "You are a helpful assistant.",
		Model:        "gpt-4",
		Provider:     "openai",
		Tools:        []string{"web_search", "calculator"},
		Config:       map[string]any{"temperature": 0.7},
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if agent.ID != "agent-123" {
		t.Errorf("ID = %q, want %q", agent.ID, "agent-123")
	}
	if agent.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", agent.Model, "gpt-4")
	}
	if len(agent.Tools) != 2 {
		t.Errorf("Tools length = %d, want 2", len(agent.Tools))
	}
}

func TestAPIKey_Struct(t *testing.T) {
	now := time.Now()
	apiKey := APIKey{
		ID:         "key-123",
		UserID:     "user-456",
		Name:       "Test API Key",
		Prefix:     "nxs_1234",
		Scopes:     []string{"read", "write"},
		LastUsedAt: now,
		ExpiresAt:  now.Add(24 * time.Hour),
		CreatedAt:  now,
	}

	if apiKey.ID != "key-123" {
		t.Errorf("ID = %q, want %q", apiKey.ID, "key-123")
	}
	if apiKey.Prefix != "nxs_1234" {
		t.Errorf("Prefix = %q, want %q", apiKey.Prefix, "nxs_1234")
	}
	if len(apiKey.Scopes) != 2 {
		t.Errorf("Scopes length = %d, want 2", len(apiKey.Scopes))
	}
}
