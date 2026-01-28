package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/pkg/models"
)

type haConversationRequest struct {
	Text           string         `json:"text"`
	Query          string         `json:"query"`
	Utterance      string         `json:"utterance"`
	SessionID      string         `json:"session_id"`
	ConversationID string         `json:"conversation_id"`
	AgentID        string         `json:"agent_id"`
	UserID         string         `json:"user_id"`
	Metadata       map[string]any `json:"metadata"`
}

type haConversationResponse struct {
	Response        string `json:"response"`
	SessionID       string `json:"session_id"`
	MessageID       string `json:"message_id"`
	ConversationID  string `json:"conversation_id,omitempty"`
	ResponseCreated string `json:"response_created_at,omitempty"`
}

func (s *Server) handleHomeAssistantConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if s == nil || s.config == nil || !s.config.Channels.HomeAssistant.Enabled {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "homeassistant integration disabled"})
		return
	}

	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxInputSize)
	defer r.Body.Close()

	var req haConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}

	content := strings.TrimSpace(req.Text)
	if content == "" {
		content = strings.TrimSpace(req.Query)
	}
	if content == "" {
		content = strings.TrimSpace(req.Utterance)
	}
	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "text is required"})
		return
	}

	runtime, err := s.ensureRuntime(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "runtime unavailable"})
		return
	}

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(s.config.Session.DefaultAgentID)
	}
	if agentID == "" {
		agentID = defaultAgentID
	}

	subject := strings.TrimSpace(req.UserID)
	if subject == "" {
		if user, ok := auth.UserFromContext(ctx); ok && user != nil {
			subject = strings.TrimSpace(user.ID)
		}
	}
	if subject == "" {
		subject = strings.TrimSpace(req.ConversationID)
	}
	if subject == "" {
		subject = "homeassistant"
	}

	channelID := strings.TrimSpace(req.ConversationID)
	if channelID == "" {
		channelID = subject
	}
	channelID = "homeassistant:" + channelID

	var session *models.Session
	if strings.TrimSpace(req.SessionID) != "" {
		session, err = s.sessions.Get(ctx, strings.TrimSpace(req.SessionID))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
			return
		}
	} else {
		key := s.buildSessionKeyForPeer(agentID, models.ChannelAPI, channelID)
		session, err = s.sessions.GetOrCreate(ctx, key, agentID, models.ChannelAPI, channelID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to create session"})
			return
		}
	}

	updatedSession := false
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
		updatedSession = true
	}
	if subject != "" {
		if existing, ok := session.Metadata["user_id"].(string); !ok || strings.TrimSpace(existing) == "" {
			session.Metadata["user_id"] = subject
			updatedSession = true
		}
	}
	if req.ConversationID != "" {
		if existing, ok := session.Metadata["conversation_id"].(string); !ok || existing != req.ConversationID {
			session.Metadata["conversation_id"] = req.ConversationID
			updatedSession = true
		}
	}
	if existing, ok := session.Metadata["source"].(string); !ok || existing != "homeassistant" {
		session.Metadata["source"] = "homeassistant"
		updatedSession = true
	}
	if updatedSession {
		if err := s.sessions.Update(ctx, session); err != nil && s.logger != nil {
			s.logger.Warn("failed to update homeassistant session metadata", "error", err)
		}
	}

	msg := &models.Message{
		SessionID:   session.ID,
		Channel:     session.Channel,
		ChannelID:   session.ChannelID,
		Direction:   models.DirectionInbound,
		Role:        models.RoleUser,
		Content:     content,
		Metadata:    req.Metadata,
		CreatedAt:   time.Now(),
		ToolCalls:   nil,
		ToolResults: nil,
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}
	msg.Metadata["source"] = "homeassistant"
	msg.Metadata["channel_id"] = channelID
	if subject != "" {
		msg.Metadata["user_id"] = subject
	}
	if req.ConversationID != "" {
		msg.Metadata["conversation_id"] = req.ConversationID
	}

	if err := s.sessions.AppendMessage(ctx, session.ID, msg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to persist message"})
		return
	}
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(msg); err != nil && s.logger != nil {
			s.logger.Warn("failed to append memory log", "error", err)
		}
	}

	promptCtx := ctx
	var agentModel *models.Agent
	if s.stores.Agents != nil && session != nil {
		if model, err := s.stores.Agents.Get(ctx, session.AgentID); err == nil {
			agentModel = model
		}
	}
	toolPolicy := s.resolveToolPolicy(agentModel, msg)
	systemPrompt, _ := s.systemPromptForMessage(ctx, session, msg, toolPolicy)
	if systemPrompt != "" {
		promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
	}
	if s.toolPolicyResolver != nil && toolPolicy != nil {
		promptCtx = agent.WithToolPolicy(promptCtx, s.toolPolicyResolver, toolPolicy)
	}
	if overrides := s.experimentOverrides(session, msg); overrides.Model != "" {
		promptCtx = agent.WithModel(promptCtx, overrides.Model)
	}
	if model := sessionModelOverride(session); model != "" {
		promptCtx = agent.WithModel(promptCtx, model)
	}

	runCtx, cancel := context.WithTimeout(promptCtx, maxProcessingTime)
	runToken := s.registerActiveRun(session.ID, cancel)
	defer func() {
		cancel()
		s.finishActiveRun(session.ID, runToken)
	}()

	chunks, err := runtime.Process(runCtx, session, msg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "runtime error"})
		return
	}

	messageID := uuid.NewString()
	var response strings.Builder
	var toolResults []models.ToolResult

	for chunk := range chunks {
		if chunk == nil {
			continue
		}
		if chunk.Error != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": chunk.Error.Error()})
			return
		}
		if chunk.Text != "" {
			if response.Len()+len(chunk.Text) > maxResponseSize {
				response.WriteString(chunk.Text[:max(0, maxResponseSize-response.Len())])
				cancel()
				break
			}
			response.WriteString(chunk.Text)
		}
		if chunk.ToolResult != nil {
			toolResults = append(toolResults, *chunk.ToolResult)
		}
	}

	content, _, _ = normalizeReplyContent(response.String())
	outbound := &models.Message{
		ID:          messageID,
		SessionID:   session.ID,
		Channel:     session.Channel,
		ChannelID:   session.ChannelID,
		Direction:   models.DirectionOutbound,
		Role:        models.RoleAssistant,
		Content:     content,
		ToolResults: toolResults,
		CreatedAt:   time.Now(),
	}
	if err := s.sessions.AppendMessage(ctx, session.ID, outbound); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to persist response"})
		return
	}
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(outbound); err != nil && s.logger != nil {
			s.logger.Warn("failed to append memory log", "error", err)
		}
	}

	s.confirmMemoryFlush(ctx, session)

	writeJSON(w, http.StatusOK, haConversationResponse{
		Response:        outbound.Content,
		SessionID:       session.ID,
		MessageID:       outbound.ID,
		ConversationID:  req.ConversationID,
		ResponseCreated: outbound.CreatedAt.Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if w == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(payload); err != nil {
		// Best-effort: the client may have disconnected or the response stream may be broken.
		return
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
