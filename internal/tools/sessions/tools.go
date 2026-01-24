package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/agent"
	sessionstore "github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ListTool lists sessions from the store.
type ListTool struct {
	store        sessionstore.Store
	defaultAgent string
}

// NewListTool creates a sessions_list tool.
func NewListTool(store sessionstore.Store, defaultAgent string) *ListTool {
	if strings.TrimSpace(defaultAgent) == "" {
		defaultAgent = "main"
	}
	return &ListTool{store: store, defaultAgent: defaultAgent}
}

func (t *ListTool) Name() string { return "sessions_list" }

func (t *ListTool) Description() string {
	return "List recent sessions with optional agent/channel filters."
}

func (t *ListTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "Filter by agent id.",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Filter by channel type (telegram, slack, etc).",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max sessions to return (default: 25).",
				"minimum":     1,
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Offset for pagination (default: 0).",
				"minimum":     0,
			},
			"active_minutes": map[string]interface{}{
				"type":        "integer",
				"description": "Only sessions updated within N minutes.",
				"minimum":     1,
			},
		},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *ListTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return toolError("session store unavailable"), nil
	}
	var input struct {
		AgentID       string `json:"agent_id"`
		Channel       string `json:"channel"`
		Limit         int    `json:"limit"`
		Offset        int    `json:"offset"`
		ActiveMinutes int    `json:"active_minutes"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		agentID = t.defaultAgent
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 500 {
		limit = 500
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	channel := models.ChannelType(strings.ToLower(strings.TrimSpace(input.Channel)))

	list, err := t.store.List(ctx, agentID, sessionstore.ListOptions{Channel: channel, Limit: limit, Offset: offset})
	if err != nil {
		return toolError(fmt.Sprintf("list sessions: %v", err)), nil
	}

	filtered := list
	if input.ActiveMinutes > 0 {
		cutoff := time.Now().Add(-time.Duration(input.ActiveMinutes) * time.Minute)
		filtered = filtered[:0]
		for _, session := range list {
			updated := session.UpdatedAt
			if updated.IsZero() {
				updated = session.CreatedAt
			}
			if updated.After(cutoff) {
				filtered = append(filtered, session)
			}
		}
	}

	out := make([]map[string]interface{}, 0, len(filtered))
	for _, session := range filtered {
		out = append(out, map[string]interface{}{
			"id":         session.ID,
			"key":        session.Key,
			"agent_id":   session.AgentID,
			"channel":    session.Channel,
			"channel_id": session.ChannelID,
			"title":      session.Title,
			"metadata":   session.Metadata,
			"created_at": session.CreatedAt,
			"updated_at": session.UpdatedAt,
		})
	}

	nextOffset := 0
	if len(list) == limit {
		nextOffset = offset + limit
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"sessions":    out,
		"count":       len(out),
		"next_offset": nextOffset,
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

// HistoryTool returns session history.
type HistoryTool struct {
	store sessionstore.Store
}

// NewHistoryTool creates a sessions_history tool.
func NewHistoryTool(store sessionstore.Store) *HistoryTool {
	return &HistoryTool{store: store}
}

func (t *HistoryTool) Name() string { return "sessions_history" }

func (t *HistoryTool) Description() string {
	return "Fetch recent messages from a session by id or key."
}

func (t *HistoryTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session ID.",
			},
			"session_key": map[string]interface{}{
				"type":        "string",
				"description": "Session key.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max messages to return (default: 50).",
				"minimum":     1,
			},
			"include_tools": map[string]interface{}{
				"type":        "boolean",
				"description": "Include tool messages (default: false).",
			},
		},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *HistoryTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return toolError("session store unavailable"), nil
	}
	var input struct {
		SessionID    string `json:"session_id"`
		SessionKey   string `json:"session_key"`
		Limit        int    `json:"limit"`
		IncludeTools bool   `json:"include_tools"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	session, err := resolveSession(ctx, t.store, input.SessionID, input.SessionKey)
	if err != nil {
		return toolError(err.Error()), nil
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	history, err := t.store.GetHistory(ctx, session.ID, limit)
	if err != nil {
		return toolError(fmt.Sprintf("get history: %v", err)), nil
	}

	messages := make([]map[string]interface{}, 0, len(history))
	for _, msg := range history {
		if !input.IncludeTools && msg.Role == models.RoleTool {
			continue
		}
		messages = append(messages, map[string]interface{}{
			"id":           msg.ID,
			"role":         msg.Role,
			"content":      msg.Content,
			"created_at":   msg.CreatedAt,
			"tool_calls":   msg.ToolCalls,
			"tool_results": msg.ToolResults,
		})
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"session_id": session.ID,
		"messages":   messages,
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

// StatusTool reports session metadata.
type StatusTool struct {
	store sessionstore.Store
}

// NewStatusTool creates a session_status tool.
func NewStatusTool(store sessionstore.Store) *StatusTool {
	return &StatusTool{store: store}
}

func (t *StatusTool) Name() string { return "session_status" }

func (t *StatusTool) Description() string {
	return "Return basic status metadata for a session by id or key."
}

func (t *StatusTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session ID.",
			},
			"session_key": map[string]interface{}{
				"type":        "string",
				"description": "Session key.",
			},
		},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *StatusTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil {
		return toolError("session store unavailable"), nil
	}
	var input struct {
		SessionID  string `json:"session_id"`
		SessionKey string `json:"session_key"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	session, err := resolveSession(ctx, t.store, input.SessionID, input.SessionKey)
	if err != nil {
		return toolError(err.Error()), nil
	}
	payload, err := json.MarshalIndent(map[string]interface{}{
		"id":         session.ID,
		"key":        session.Key,
		"agent_id":   session.AgentID,
		"channel":    session.Channel,
		"channel_id": session.ChannelID,
		"title":      session.Title,
		"metadata":   session.Metadata,
		"created_at": session.CreatedAt,
		"updated_at": session.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

// SendTool sends a message into another session.
type SendTool struct {
	store   sessionstore.Store
	runtime *agent.Runtime
}

// NewSendTool creates a sessions_send tool.
func NewSendTool(store sessionstore.Store, runtime *agent.Runtime) *SendTool {
	return &SendTool{store: store, runtime: runtime}
}

func (t *SendTool) Name() string { return "sessions_send" }

func (t *SendTool) Description() string {
	return "Send a message to another session and optionally wait for the reply."
}

func (t *SendTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session ID.",
			},
			"session_key": map[string]interface{}{
				"type":        "string",
				"description": "Target session key.",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Message to send.",
			},
			"wait": map[string]interface{}{
				"type":        "boolean",
				"description": "Wait for completion (default: true).",
			},
			"timeout_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "Optional timeout in seconds.",
				"minimum":     0,
			},
		},
		"required": []string{"message"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *SendTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.store == nil || t.runtime == nil {
		return toolError("session runtime unavailable"), nil
	}
	var input struct {
		SessionID      string `json:"session_id"`
		SessionKey     string `json:"session_key"`
		Message        string `json:"message"`
		Wait           *bool  `json:"wait"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(input.Message) == "" {
		return toolError("message is required"), nil
	}

	session, err := resolveSession(ctx, t.store, input.SessionID, input.SessionKey)
	if err != nil {
		return toolError(err.Error()), nil
	}

	msg := &models.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Channel:   session.Channel,
		ChannelID: session.ChannelID,
		Role:      models.RoleUser,
		Content:   input.Message,
		CreatedAt: time.Now(),
	}

	wait := true
	if input.Wait != nil {
		wait = *input.Wait
	}

	runCtx := ctx
	if input.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	if !wait {
		go func() {
			chunks, err := t.runtime.Process(runCtx, session, msg)
			if err != nil {
				return
			}
			for range chunks {
				// Drain
			}
		}()
		payload, err := json.MarshalIndent(map[string]interface{}{
			"status":     "queued",
			"session_id": session.ID,
		}, "", "  ")
		if err != nil {
			return toolError(fmt.Sprintf("encode result: %v", err)), nil
		}
		return &agent.ToolResult{Content: string(payload)}, nil
	}

	chunks, err := t.runtime.Process(runCtx, session, msg)
	if err != nil {
		return toolError(fmt.Sprintf("run session: %v", err)), nil
	}

	var builder strings.Builder
	for chunk := range chunks {
		if chunk.Error != nil {
			return toolError(fmt.Sprintf("session error: %v", chunk.Error)), nil
		}
		if chunk.Text != "" {
			builder.WriteString(chunk.Text)
		}
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"status":     "completed",
		"session_id": session.ID,
		"response":   builder.String(),
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

func resolveSession(ctx context.Context, store sessionstore.Store, id, key string) (*models.Session, error) {
	id = strings.TrimSpace(id)
	key = strings.TrimSpace(key)
	if id == "" && key == "" {
		return nil, fmt.Errorf("session_id or session_key is required")
	}
	if id != "" {
		session, err := store.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("session not found")
		}
		return session, nil
	}
	session, err := store.GetByKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("session not found")
	}
	return session, nil
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
