package gateway

import (
	"context"
	"strings"

	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) maybeHandleCommand(ctx context.Context, session *models.Session, msg *models.Message) bool {
	if s.commandParser == nil || s.commandRegistry == nil || session == nil || msg == nil {
		return false
	}

	detection := s.commandParser.Parse(msg.Content)
	if detection == nil || detection.Primary == nil || !detection.IsControlCommand {
		return false
	}

	trimmed := strings.TrimSpace(msg.Content)
	if detection.Primary.StartPos != 0 || detection.Primary.EndPos != len(strings.TrimSpace(trimmed)) {
		return false
	}

	inv := s.buildCommandInvocation(session, msg, detection.Primary)
	result, err := s.commandRegistry.Execute(ctx, inv)
	if err != nil {
		s.sendImmediateReply(ctx, session, msg, "Command failed: "+err.Error())
		return true
	}
	if result == nil {
		return true
	}
	if result.Error != "" {
		s.sendImmediateReply(ctx, session, msg, result.Error)
		return true
	}
	if !result.Suppress && strings.TrimSpace(result.Text) != "" {
		s.sendImmediateReply(ctx, session, msg, result.Text)
	}
	s.applyCommandActions(ctx, session, result)
	return true
}

func (s *Server) buildCommandInvocation(session *models.Session, msg *models.Message, parsed *commands.ParsedCommand) *commands.Invocation {
	inv := &commands.Invocation{
		Name:       parsed.Name,
		Args:       parsed.Args,
		RawText:    strings.TrimSpace(msg.Content),
		SessionKey: session.Key,
		ChannelID:  session.ChannelID,
		UserID:     extractSenderID(msg),
		IsAdmin:    isAdminMessage(msg),
		Context: map[string]any{
			"session_id": session.ID,
			"agent_id":   session.AgentID,
			"channel":    session.Channel,
			"channel_id": session.ChannelID,
		},
	}

	if model := sessionModelOverride(session); model != "" {
		inv.Context["model"] = model
	}
	if s.defaultModel != "" {
		inv.Context["default_model"] = s.defaultModel
	}
	return inv
}

func (s *Server) applyCommandActions(ctx context.Context, session *models.Session, result *commands.Result) {
	if result == nil || result.Data == nil || session == nil {
		return
	}
	action, _ := result.Data["action"].(string)
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "new_session":
		if err := s.sessions.Delete(ctx, session.ID); err != nil {
			s.logger.Error("failed to reset session", "error", err)
		}
	case "set_model":
		model, _ := result.Data["model"].(string)
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if session.Metadata == nil {
			session.Metadata = map[string]any{}
		}
		session.Metadata["model"] = model
		if err := s.sessions.Update(ctx, session); err != nil {
			s.logger.Error("failed to update session model", "error", err)
		}
	}
}

func sessionModelOverride(session *models.Session) string {
	if session == nil || session.Metadata == nil {
		return ""
	}
	if value, ok := session.Metadata["model"].(string); ok {
		return strings.TrimSpace(value)
	}
	if value, ok := session.Metadata["model_override"].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func isAdminMessage(msg *models.Message) bool {
	if msg == nil || msg.Metadata == nil {
		return false
	}
	for _, key := range []string{"is_admin", "admin", "owner"} {
		value, ok := msg.Metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			if typed {
				return true
			}
		case string:
			if strings.EqualFold(strings.TrimSpace(typed), "true") {
				return true
			}
		}
	}
	return false
}
