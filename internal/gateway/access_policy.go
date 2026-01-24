package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/pairing"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) enforceAccessPolicy(ctx context.Context, msg *models.Message) bool {
	if s == nil || s.config == nil || msg == nil {
		return false
	}

	convType := conversationTypeForMessage(msg)
	policyCfg, ok := s.channelPolicyConfig(msg.Channel, convType)
	if !ok {
		return false
	}

	policy := strings.ToLower(strings.TrimSpace(policyCfg.Policy))
	switch policy {
	case "", "open":
		return false
	case "disabled":
		s.logger.Info("message blocked by policy",
			"channel", msg.Channel,
			"conversation_type", convType,
			"policy", policy,
		)
		return true
	case "allowlist":
		targetID := s.policyTargetID(msg, convType)
		useStore := strings.EqualFold(convType, "dm")
		if targetID != "" && s.isAllowedTarget(msg.Channel, targetID, policyCfg, useStore) {
			return false
		}
		s.logger.Info("message blocked by allowlist",
			"channel", msg.Channel,
			"conversation_type", convType,
			"policy", policy,
		)
		return true
	case "pairing":
		if convType != "dm" {
			s.logger.Info("pairing policy blocked non-dm message",
				"channel", msg.Channel,
				"conversation_type", convType,
			)
			return true
		}
		targetID := s.policyTargetID(msg, convType)
		if targetID != "" && s.isAllowedTarget(msg.Channel, targetID, policyCfg, true) {
			return false
		}
		if err := s.handlePairingRequest(ctx, msg, targetID); err != nil {
			s.logger.Warn("pairing request failed",
				"channel", msg.Channel,
				"error", err,
			)
		}
		return true
	default:
		return false
	}
}

func (s *Server) channelPolicyConfig(channel models.ChannelType, convType string) (config.ChannelPolicyConfig, bool) {
	if s == nil || s.config == nil {
		return config.ChannelPolicyConfig{}, false
	}

	isGroup := strings.EqualFold(convType, "group")
	switch channel {
	case models.ChannelTelegram:
		if isGroup {
			return s.config.Channels.Telegram.Group, true
		}
		return s.config.Channels.Telegram.DM, true
	case models.ChannelDiscord:
		if isGroup {
			return s.config.Channels.Discord.Group, true
		}
		return s.config.Channels.Discord.DM, true
	case models.ChannelSlack:
		if isGroup {
			return s.config.Channels.Slack.Group, true
		}
		return s.config.Channels.Slack.DM, true
	case models.ChannelWhatsApp:
		if isGroup {
			return s.config.Channels.WhatsApp.Group, true
		}
		return s.config.Channels.WhatsApp.DM, true
	case models.ChannelSignal:
		if isGroup {
			return s.config.Channels.Signal.Group, true
		}
		return s.config.Channels.Signal.DM, true
	case models.ChannelIMessage:
		if isGroup {
			return s.config.Channels.IMessage.Group, true
		}
		return s.config.Channels.IMessage.DM, true
	case models.ChannelMatrix:
		if isGroup {
			return s.config.Channels.Matrix.Group, true
		}
		return s.config.Channels.Matrix.DM, true
	case models.ChannelTeams:
		if isGroup {
			return s.config.Channels.Teams.Group, true
		}
		return s.config.Channels.Teams.DM, true
	default:
		return config.ChannelPolicyConfig{}, false
	}
}

func (s *Server) policyTargetID(msg *models.Message, convType string) string {
	if strings.EqualFold(convType, "group") {
		return extractGroupID(msg)
	}
	return extractSenderID(msg)
}

func (s *Server) isAllowedTarget(channel models.ChannelType, targetID string, policyCfg config.ChannelPolicyConfig, includeStore bool) bool {
	if targetID == "" {
		return false
	}
	if senderMatchesAllowlist(targetID, policyCfg.AllowFrom) {
		return true
	}
	if !includeStore {
		return false
	}
	provider := strings.ToLower(string(channel))
	store := pairing.NewStore(provider)
	allowlist, err := store.GetAllowlist(provider)
	if err != nil {
		s.logger.Warn("failed to load pairing allowlist",
			"channel", channel,
			"error", err,
		)
		return false
	}
	return senderMatchesAllowlist(targetID, allowlist)
}

func (s *Server) handlePairingRequest(ctx context.Context, msg *models.Message, senderID string) error {
	if senderID == "" {
		return fmt.Errorf("missing sender id for pairing")
	}
	provider := strings.ToLower(string(msg.Channel))
	store := pairing.NewStore(provider)
	meta := map[string]string{}
	if senderName := extractSenderName(msg); senderName != "" {
		meta["sender_name"] = senderName
	}
	code, created, err := store.UpsertRequest(provider, senderID, meta)
	if err != nil {
		return err
	}
	req := pairing.Request{
		ID:        senderID,
		Code:      code,
		CreatedAt: time.Now(),
	}
	if !created {
		if pending, err := store.ListRequests(provider); err == nil {
			for _, item := range pending {
				if item != nil && item.ID == senderID {
					req.CreatedAt = item.CreatedAt
					req.LastSeenAt = item.LastSeenAt
					req.Meta = item.Meta
					break
				}
			}
		}
	}

	content := buildPairingPrompt(provider, extractSenderName(msg), req, created)
	if strings.TrimSpace(content) == "" {
		return nil
	}

	adapter, ok := s.channels.GetOutbound(msg.Channel)
	if !ok {
		return fmt.Errorf("no outbound adapter for channel %s", msg.Channel)
	}

	outbound := &models.Message{
		Channel:   msg.Channel,
		ChannelID: msg.ChannelID,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   content,
		Metadata:  s.buildReplyMetadata(msg),
		CreatedAt: time.Now(),
	}

	if err := adapter.Send(ctx, outbound); err != nil {
		return err
	}
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(outbound); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}
	return nil
}

func buildPairingPrompt(provider string, senderName string, req pairing.Request, created bool) string {
	label := "Pairing request"
	if senderName != "" {
		label = fmt.Sprintf("Pairing request from %s", senderName)
	}
	status := "received"
	if !created {
		status = "already pending"
	}
	expiresAt := req.CreatedAt.Add(pairing.PendingTTL)
	expiresIn := time.Until(expiresAt).Round(time.Minute)
	if expiresIn < 0 {
		expiresIn = 0
	}

	lines := []string{
		fmt.Sprintf("%s for %s (%s).", label, provider, status),
		fmt.Sprintf("Code: %s (expires in %s).", req.Code, expiresIn),
		fmt.Sprintf("Approve: nexus pairing approve %s --provider %s", req.Code, provider),
		fmt.Sprintf("Deny: nexus pairing deny %s --provider %s", req.Code, provider),
	}
	return strings.Join(lines, "\n")
}

func conversationTypeForMessage(msg *models.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Metadata != nil {
		if raw, ok := msg.Metadata["conversation_type"].(string); ok && strings.TrimSpace(raw) != "" {
			if normalized := normalizeConversationType(raw); normalized != "" {
				return normalized
			}
		}
		if raw, ok := msg.Metadata["chat_type"].(string); ok && strings.TrimSpace(raw) != "" {
			if strings.EqualFold(raw, "private") || strings.EqualFold(raw, "oneOnOne") {
				return "dm"
			}
			return "group"
		}
		if groupID, ok := msg.Metadata["group_id"].(string); ok && groupID != "" {
			return "group"
		}
		if roomID, ok := msg.Metadata["room_id"].(string); ok && roomID != "" {
			return "group"
		}
	}

	switch msg.Channel {
	case models.ChannelSlack:
		if msg.Metadata != nil {
			if channelID, ok := msg.Metadata["slack_channel"].(string); ok && channelID != "" {
				if strings.HasPrefix(channelID, "D") {
					return "dm"
				}
				return "group"
			}
		}
	case models.ChannelDiscord:
		if msg.Metadata != nil {
			if guildID, ok := msg.Metadata["discord_guild_id"].(string); ok && guildID != "" {
				return "group"
			}
		}
	}

	return "dm"
}

func normalizeConversationType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dm", "direct", "direct_message", "private":
		return "dm"
	case "group", "channel", "thread":
		return "group"
	default:
		return ""
	}
}

func extractGroupID(msg *models.Message) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	if groupID, ok := msg.Metadata["group_id"].(string); ok && groupID != "" {
		return groupID
	}
	if roomID, ok := msg.Metadata["room_id"].(string); ok && roomID != "" {
		return roomID
	}
	switch msg.Channel {
	case models.ChannelTelegram:
		if chatID, ok := msg.Metadata["chat_id"]; ok {
			if id := stringifyID(chatID); id != "" {
				return id
			}
		}
	case models.ChannelSlack:
		if channelID, ok := msg.Metadata["slack_channel"].(string); ok && channelID != "" {
			return channelID
		}
	case models.ChannelDiscord:
		if threadID, ok := msg.Metadata["discord_thread_id"].(string); ok && threadID != "" {
			return threadID
		}
		if channelID, ok := msg.Metadata["discord_channel_id"].(string); ok && channelID != "" {
			return channelID
		}
	case models.ChannelTeams:
		if chatID, ok := msg.Metadata["chat_id"].(string); ok && chatID != "" {
			return chatID
		}
	}
	return ""
}

func extractSenderName(msg *models.Message) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	if name, ok := msg.Metadata["sender_name"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if name, ok := msg.Metadata["peer_name"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if first, ok := msg.Metadata["user_first"].(string); ok {
		last, _ := msg.Metadata["user_last"].(string) //nolint:errcheck
		combined := strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
		if combined != "" {
			return combined
		}
	}
	if name, ok := msg.Metadata["discord_username"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if name, ok := msg.Metadata["slack_user_name"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if name, ok := msg.Metadata["sender"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return ""
}

func stringifyID(value any) string {
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}
