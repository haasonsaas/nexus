package models

import (
	"encoding/json"
	"time"
)

// ChannelType represents a messaging platform.
type ChannelType string

const (
	ChannelTelegram ChannelType = "telegram"
	ChannelDiscord  ChannelType = "discord"
	ChannelSlack    ChannelType = "slack"
)

// Direction indicates if a message is inbound or outbound.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// Role indicates the message author type.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// Message is the unified message format across all channels.
type Message struct {
	ID          string            `json:"id"`
	SessionID   string            `json:"session_id"`
	Channel     ChannelType       `json:"channel"`
	ChannelID   string            `json:"channel_id"`   // Platform-specific message ID
	Direction   Direction         `json:"direction"`
	Role        Role              `json:"role"`
	Content     string            `json:"content"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	ToolCalls   []ToolCall        `json:"tool_calls,omitempty"`
	ToolResults []ToolResult      `json:"tool_results,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// Attachment represents a file or media attachment.
type Attachment struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // image, audio, video, document
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// ToolCall represents an LLM's request to execute a tool.
type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// Session represents a conversation thread.
type Session struct {
	ID        string            `json:"id"`
	AgentID   string            `json:"agent_id"`
	Channel   ChannelType       `json:"channel"`
	ChannelID string            `json:"channel_id"`
	Key       string            `json:"key"`
	Title     string            `json:"title,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// User represents an authenticated user.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Agent represents a configured AI agent.
type Agent struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id"`
	Name         string         `json:"name"`
	SystemPrompt string         `json:"system_prompt,omitempty"`
	Model        string         `json:"model"`
	Provider     string         `json:"provider"`
	Tools        []string       `json:"tools,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// APIKey represents an API key for programmatic access.
type APIKey struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"` // First 8 chars for identification
	Scopes     []string  `json:"scopes,omitempty"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
