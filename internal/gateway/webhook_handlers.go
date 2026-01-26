package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) initWebhookHooks() error {
	if s == nil || s.config == nil {
		return nil
	}
	hooks, err := NewWebhookHooks(&s.config.Gateway.WebhookHooks)
	if err != nil {
		return err
	}
	if hooks == nil {
		return nil
	}
	hooks.RegisterHandler(webhookHandlerAgent, WebhookHandlerFunc(s.handleWebhookAgent))
	hooks.RegisterHandler(webhookHandlerWake, WebhookHandlerFunc(s.handleWebhookWake))
	hooks.RegisterHandler(webhookHandlerCustom, WebhookHandlerFunc(s.handleWebhookCustom))
	s.webhookHooks = hooks
	return nil
}

// RegisterWebhookHandler registers a custom webhook handler by name.
func (s *Server) RegisterWebhookHandler(name string, handler WebhookHandler) {
	if s == nil || handler == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	s.webhookMu.Lock()
	if s.webhookHandlers == nil {
		s.webhookHandlers = make(map[string]WebhookHandler)
	}
	s.webhookHandlers[name] = handler
	s.webhookMu.Unlock()
}

func (s *Server) handleWebhookAgent(ctx context.Context, payload *WebhookPayload, mapping *config.WebhookHookMapping) (*WebhookResponse, error) {
	msg, agentID, channelType, channelID, err := s.webhookMessageFromPayload(payload, mapping, "")
	if err != nil {
		return &WebhookResponse{OK: false, Error: err.Error()}, nil
	}
	s.handleMessage(ctx, msg)

	return &WebhookResponse{
		OK:        true,
		RequestID: msg.ID,
		Message:   "message queued",
		Data: map[string]any{
			"agent_id":    agentID,
			"channel":     string(channelType),
			"channel_id":  channelID,
			"session_key": webhookSessionKey(payload, channelID),
		},
	}, nil
}

func (s *Server) handleWebhookWake(ctx context.Context, payload *WebhookPayload, mapping *config.WebhookHookMapping) (*WebhookResponse, error) {
	wakeMode := strings.ToLower(strings.TrimSpace(payload.WakeMode))
	if wakeMode == "" {
		wakeMode = "now"
	}
	msg, agentID, channelType, channelID, err := s.webhookMessageFromPayload(payload, mapping, wakeMode)
	if err != nil {
		return &WebhookResponse{OK: false, Error: err.Error()}, nil
	}
	s.handleMessage(ctx, msg)

	return &WebhookResponse{
		OK:        true,
		RequestID: msg.ID,
		Message:   "wake queued",
		Data: map[string]any{
			"agent_id":    agentID,
			"channel":     string(channelType),
			"channel_id":  channelID,
			"session_key": webhookSessionKey(payload, channelID),
			"mode":        wakeMode,
		},
	}, nil
}

func (s *Server) handleWebhookCustom(ctx context.Context, payload *WebhookPayload, mapping *config.WebhookHookMapping) (*WebhookResponse, error) {
	if mapping == nil {
		return &WebhookResponse{OK: false, Error: "mapping required"}, nil
	}
	key := webhookHandlerKey(mapping)
	if key == "" {
		return &WebhookResponse{OK: false, Error: "custom handler name required"}, nil
	}
	s.webhookMu.RLock()
	handler := s.webhookHandlers[key]
	s.webhookMu.RUnlock()
	if handler == nil {
		return &WebhookResponse{OK: false, Error: fmt.Sprintf("custom handler not registered: %s", key)}, nil
	}
	return handler.Handle(ctx, payload, mapping)
}

func (s *Server) webhookMessageFromPayload(payload *WebhookPayload, mapping *config.WebhookHookMapping, wakeMode string) (*models.Message, string, models.ChannelType, string, error) {
	if payload == nil {
		return nil, "", "", "", errors.New("payload required")
	}
	content := strings.TrimSpace(payload.Message)
	if content == "" {
		return nil, "", "", "", errors.New("message required")
	}
	agentID := webhookAgentID(s, mapping)
	channelType, channelID, err := resolveWebhookChannel(payload, mapping)
	if err != nil {
		return nil, "", "", "", err
	}
	metadata := make(map[string]any)
	for k, v := range payload.Metadata {
		metadata[k] = v
	}
	metadata["agent_id"] = agentID
	if mapping != nil {
		if mapping.Name != "" {
			metadata["webhook_name"] = mapping.Name
		}
		if mapping.Path != "" {
			metadata["webhook_path"] = mapping.Path
		}
		if mapping.Handler != "" {
			metadata["webhook_handler"] = mapping.Handler
		}
	}
	if payload.Model != "" {
		metadata["model"] = payload.Model
	}
	if payload.Thinking != "" {
		metadata["thinking"] = payload.Thinking
	}
	if wakeMode == "next-heartbeat" {
		metadata["heartbeat"] = true
	}
	applyWebhookChannelMetadata(metadata, channelType, channelID)

	msg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   channelType,
		ChannelID: channelID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   content,
		Metadata:  metadata,
		CreatedAt: time.Now(),
	}
	return msg, agentID, channelType, channelID, nil
}

func webhookAgentID(s *Server, mapping *config.WebhookHookMapping) string {
	if mapping != nil {
		if agentID := strings.TrimSpace(mapping.AgentID); agentID != "" {
			return agentID
		}
	}
	if s != nil && s.config != nil {
		if agentID := strings.TrimSpace(s.config.Session.DefaultAgentID); agentID != "" {
			return agentID
		}
	}
	return defaultAgentID
}

func resolveWebhookChannel(payload *WebhookPayload, mapping *config.WebhookHookMapping) (models.ChannelType, string, error) {
	channelRaw := strings.TrimSpace(payload.Channel)
	if strings.EqualFold(channelRaw, "last") {
		channelRaw = ""
	}
	to := strings.TrimSpace(payload.To)
	channelID := ""
	if mapping != nil {
		channelID = strings.TrimSpace(mapping.ChannelID)
	}
	sessionKey := strings.TrimSpace(payload.SessionKey)

	if channelRaw != "" {
		if channelType, ok := parseWebhookChannelType(channelRaw); ok {
			resolvedID := to
			if resolvedID == "" {
				resolvedID = channelID
			}
			if resolvedID == "" {
				resolvedID = sessionKey
			}
			if resolvedID == "" && channelType != models.ChannelAPI {
				return channelType, "", fmt.Errorf("channel_id required for channel %s", channelType)
			}
			if resolvedID == "" {
				resolvedID = webhookFallbackChannelID(mapping)
			}
			return channelType, resolvedID, nil
		}
		if to == "" {
			to = channelRaw
		}
	}

	resolvedID := to
	if resolvedID == "" {
		resolvedID = channelID
	}
	if resolvedID == "" {
		resolvedID = sessionKey
	}
	if resolvedID == "" {
		resolvedID = webhookFallbackChannelID(mapping)
	}
	return models.ChannelAPI, resolvedID, nil
}

func parseWebhookChannelType(raw string) (models.ChannelType, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "telegram":
		return models.ChannelTelegram, true
	case "discord":
		return models.ChannelDiscord, true
	case "slack":
		return models.ChannelSlack, true
	case "api":
		return models.ChannelAPI, true
	case "whatsapp":
		return models.ChannelWhatsApp, true
	case "signal":
		return models.ChannelSignal, true
	case "imessage":
		return models.ChannelIMessage, true
	case "matrix":
		return models.ChannelMatrix, true
	case "teams":
		return models.ChannelTeams, true
	case "email":
		return models.ChannelEmail, true
	case "mattermost":
		return models.ChannelMattermost, true
	case "nextcloud-talk", "nextcloudtalk":
		return models.ChannelNextcloudTalk, true
	case "nostr":
		return models.ChannelNostr, true
	case "zalo":
		return models.ChannelZalo, true
	case "bluebubbles":
		return models.ChannelBlueBubbles, true
	default:
		return "", false
	}
}

func webhookFallbackChannelID(mapping *config.WebhookHookMapping) string {
	if mapping == nil {
		return "webhook"
	}
	path := strings.Trim(strings.TrimSpace(mapping.Path), "/")
	if path == "" {
		return "webhook"
	}
	return "webhook:" + path
}

func applyWebhookChannelMetadata(metadata map[string]any, channelType models.ChannelType, channelID string) {
	if metadata == nil || channelID == "" {
		return
	}
	switch channelType {
	case models.ChannelTelegram:
		metadata[MetaChatID] = channelID
	case models.ChannelSlack:
		metadata["slack_channel"] = channelID
	case models.ChannelDiscord:
		metadata["discord_channel_id"] = channelID
	case models.ChannelWhatsApp, models.ChannelSignal, models.ChannelIMessage, models.ChannelMatrix:
		metadata[MetaPeerID] = channelID
	}
}

func webhookHandlerKey(mapping *config.WebhookHookMapping) string {
	if mapping == nil {
		return ""
	}
	if name := strings.TrimSpace(mapping.Name); name != "" {
		return strings.ToLower(name)
	}
	path := strings.Trim(strings.TrimSpace(mapping.Path), "/")
	return strings.ToLower(path)
}

func webhookSessionKey(payload *WebhookPayload, channelID string) string {
	if payload == nil {
		return channelID
	}
	key := strings.TrimSpace(payload.SessionKey)
	if key != "" {
		return key
	}
	return channelID
}
