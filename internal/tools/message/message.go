package message

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	sessionstore "github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Tool sends outbound messages through configured channel adapters.
type Tool struct {
	name         string
	channels     *channels.Registry
	sessions     sessionstore.Store
	defaultAgent string
}

// NewTool creates a message tool with a custom name ("message" or "send_message").
func NewTool(name string, registry *channels.Registry, store sessionstore.Store, defaultAgent string) *Tool {
	if strings.TrimSpace(defaultAgent) == "" {
		defaultAgent = "main"
	}
	if strings.TrimSpace(name) == "" {
		name = "message"
	}
	return &Tool{
		name:         name,
		channels:     registry,
		sessions:     store,
		defaultAgent: defaultAgent,
	}
}

func (t *Tool) Name() string { return t.name }

func (t *Tool) Description() string {
	return "Send a message to a channel/peer using configured adapters."
}

func (t *Tool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform (send only for now).",
				"enum":        []string{"send"},
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Channel/provider name (telegram, slack, etc).",
			},
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Recipient peer/channel id.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Message text to send.",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional session id to attach this message.",
			},
			"session_key": map[string]interface{}{
				"type":        "string",
				"description": "Optional session key to attach this message.",
			},
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "Agent id when creating a new session.",
			},
		},
		"required": []string{"channel", "to", "content"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *Tool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.channels == nil {
		return toolError("channel registry unavailable"), nil
	}
	var input struct {
		Action     string `json:"action"`
		Channel    string `json:"channel"`
		To         string `json:"to"`
		Content    string `json:"content"`
		SessionID  string `json:"session_id"`
		SessionKey string `json:"session_key"`
		AgentID    string `json:"agent_id"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(input.Action) == "" {
		input.Action = "send"
	}
	if input.Action != "send" {
		return toolError("unsupported action"), nil
	}

	channelName := strings.ToLower(strings.TrimSpace(input.Channel))
	if channelName == "" {
		return toolError("channel is required"), nil
	}
	to := strings.TrimSpace(input.To)
	if to == "" {
		return toolError("to is required"), nil
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return toolError("content is required"), nil
	}

	channelType := models.ChannelType(channelName)
	adapter, ok := t.channels.GetOutbound(channelType)
	if !ok {
		return toolError(fmt.Sprintf("channel %s not available", channelName)), nil
	}

	msg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   channelType,
		ChannelID: to,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   content,
		CreatedAt: time.Now(),
	}

	if err := adapter.Send(ctx, msg); err != nil {
		return toolError(fmt.Sprintf("send message: %v", err)), nil
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" && strings.TrimSpace(input.SessionKey) != "" && t.sessions != nil {
		session, err := t.sessions.GetByKey(ctx, strings.TrimSpace(input.SessionKey))
		if err == nil && session != nil {
			sessionID = session.ID
		}
	}
	if sessionID == "" && t.sessions != nil {
		agentID := strings.TrimSpace(input.AgentID)
		if agentID == "" {
			agentID = t.defaultAgent
		}
		key := sessionstore.SessionKey(agentID, channelType, to)
		session, err := t.sessions.GetOrCreate(ctx, key, agentID, channelType, to)
		if err == nil && session != nil {
			sessionID = session.ID
		}
	}
	if sessionID != "" && t.sessions != nil {
		msg.SessionID = sessionID
		if err := t.sessions.AppendMessage(ctx, sessionID, msg); err != nil {
			return toolError(fmt.Sprintf("store message: %v", err)), nil
		}
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"status":     "sent",
		"message_id": msg.ID,
		"session_id": sessionID,
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
