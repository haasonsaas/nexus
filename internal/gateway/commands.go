package gateway

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/commands"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) maybeHandleCommand(ctx context.Context, session *models.Session, msg *models.Message) bool {
	if s.commandParser == nil || s.commandRegistry == nil || session == nil || msg == nil {
		return false
	}
	if !s.commandsEnabled() {
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

	if !s.commandAllowlistAllows(msg) {
		return true
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
	rawText := strings.TrimSpace(msg.Content)
	if parsed != nil && parsed.StartPos >= 0 && parsed.EndPos > parsed.StartPos && parsed.EndPos <= len(msg.Content) {
		rawText = strings.TrimSpace(msg.Content[parsed.StartPos:parsed.EndPos])
	}

	inv := &commands.Invocation{
		Name:       parsed.Name,
		Args:       parsed.Args,
		RawText:    rawText,
		SessionKey: session.Key,
		ChannelID:  session.ChannelID,
		UserID:     extractSenderID(msg),
		IsAdmin:    isAdminMessage(msg),
		Context: map[string]any{
			"session_id":     session.ID,
			"agent_id":       session.AgentID,
			"channel":        string(session.Channel),
			"channel_id":     session.ChannelID,
			"user_id":        extractSenderID(msg),
			"has_active_run": s.hasActiveRun(session.ID),
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
	case "abort":
		s.cancelActiveRun(session.ID)
	case "new_session":
		s.cancelActiveRun(session.ID)
		if err := s.sessions.Delete(ctx, session.ID); err != nil {
			s.logger.Error("failed to reset session", "error", err)
		}
		newSession, err := s.sessions.GetOrCreate(ctx, session.Key, session.AgentID, session.Channel, session.ChannelID)
		if err != nil {
			s.logger.Error("failed to create new session", "error", err)
			return
		}
		if model, ok := result.Data["model"].(string); ok {
			model = strings.TrimSpace(model)
			if model != "" {
				if newSession.Metadata == nil {
					newSession.Metadata = map[string]any{}
				}
				newSession.Metadata["model"] = model
				if err := s.sessions.Update(ctx, newSession); err != nil {
					s.logger.Error("failed to set model for new session", "error", err)
				}
			}
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

type activeRun struct {
	token  string
	cancel context.CancelFunc
}

func (s *Server) registerActiveRun(sessionID string, cancel context.CancelFunc) string {
	if s == nil || sessionID == "" || cancel == nil {
		return ""
	}
	token := uuid.NewString()
	s.activeRunsMu.Lock()
	defer s.activeRunsMu.Unlock()
	if s.activeRuns == nil {
		s.activeRuns = make(map[string]activeRun)
	}
	if existing, ok := s.activeRuns[sessionID]; ok && existing.cancel != nil {
		existing.cancel()
	}
	s.activeRuns[sessionID] = activeRun{token: token, cancel: cancel}
	return token
}

func (s *Server) finishActiveRun(sessionID, token string) {
	if s == nil || sessionID == "" || token == "" {
		return
	}
	s.activeRunsMu.Lock()
	defer s.activeRunsMu.Unlock()
	if s.activeRuns == nil {
		return
	}
	if current, ok := s.activeRuns[sessionID]; ok && current.token == token {
		delete(s.activeRuns, sessionID)
	}
}

func (s *Server) cancelActiveRun(sessionID string) bool {
	if s == nil || sessionID == "" {
		return false
	}
	s.activeRunsMu.Lock()
	if s.activeRuns == nil {
		s.activeRunsMu.Unlock()
		return false
	}
	run, ok := s.activeRuns[sessionID]
	if ok {
		delete(s.activeRuns, sessionID)
	}
	s.activeRunsMu.Unlock()
	if !ok || run.cancel == nil {
		return false
	}
	run.cancel()
	return true
}

func (s *Server) hasActiveRun(sessionID string) bool {
	if s == nil || sessionID == "" {
		return false
	}
	s.activeRunsMu.Lock()
	if s.activeRuns == nil {
		s.activeRunsMu.Unlock()
		return false
	}
	_, ok := s.activeRuns[sessionID]
	s.activeRunsMu.Unlock()
	return ok
}

func (s *Server) maybeHandleInlineCommands(ctx context.Context, session *models.Session, msg *models.Message) bool {
	if s.commandParser == nil || s.commandRegistry == nil || session == nil || msg == nil {
		return false
	}
	if !s.commandsEnabled() {
		return false
	}
	if !s.inlineAllowlistAllows(msg) {
		return false
	}

	detection := s.commandParser.Parse(msg.Content)
	if detection == nil || !detection.HasCommand || len(detection.Commands) == 0 {
		return false
	}

	var inline []commands.ParsedCommand
	for _, cmd := range detection.Commands {
		if !cmd.Inline {
			continue
		}
		if !s.isInlineCommandAllowed(cmd.Name) {
			continue
		}
		inline = append(inline, cmd)
	}

	if len(inline) == 0 {
		return false
	}

	for _, cmd := range inline {
		inlineCmd := cmd
		inlineCmd.Args = ""
		inv := s.buildCommandInvocation(session, msg, &inlineCmd)
		result, err := s.commandRegistry.Execute(ctx, inv)
		if err != nil {
			s.sendImmediateReply(ctx, session, msg, "Command failed: "+err.Error())
			continue
		}
		if result == nil {
			continue
		}
		if result.Error != "" {
			s.sendImmediateReply(ctx, session, msg, result.Error)
			continue
		}
		if !result.Suppress && strings.TrimSpace(result.Text) != "" {
			s.sendImmediateReply(ctx, session, msg, result.Text)
		}
		s.applyCommandActions(ctx, session, result)
	}

	msg.Content = stripInlineCommands(msg.Content, inline)
	return true
}

func (s *Server) commandsEnabled() bool {
	if s == nil || s.config == nil {
		return true
	}
	if s.config.Commands.Enabled == nil {
		return true
	}
	return *s.config.Commands.Enabled
}

func (s *Server) commandAllowlistAllows(msg *models.Message) bool {
	if s == nil || s.config == nil {
		return true
	}
	if len(s.config.Commands.AllowFrom) == 0 {
		return true
	}
	return allowlistMatches(s.config.Commands.AllowFrom, msg.Channel, extractSenderID(msg))
}

func (s *Server) inlineAllowlistAllows(msg *models.Message) bool {
	if s == nil || s.config == nil {
		return false
	}
	if len(s.config.Commands.InlineAllowFrom) == 0 {
		return false
	}
	return allowlistMatches(s.config.Commands.InlineAllowFrom, msg.Channel, extractSenderID(msg))
}

func (s *Server) isInlineCommandAllowed(name string) bool {
	name = normalizeCommandName(name)
	if name == "" {
		return false
	}
	allowed := s.inlineCommandsAllowlist()
	if _, ok := allowed[name]; ok {
		return true
	}
	if s.commandRegistry == nil {
		return false
	}
	if cmd, ok := s.commandRegistry.Get(name); ok && cmd != nil {
		if _, ok := allowed[normalizeCommandName(cmd.Name)]; ok {
			return true
		}
	}
	return false
}

func (s *Server) inlineCommandsAllowlist() map[string]struct{} {
	allowed := make(map[string]struct{})
	if s == nil || s.config == nil {
		return allowed
	}
	entries := s.config.Commands.InlineCommands
	if len(entries) == 0 {
		entries = []string{"help", "commands", "status", "whoami", "id"}
	}
	for _, entry := range entries {
		name := normalizeCommandName(entry)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return allowed
}

func normalizeCommandName(value string) string {
	name := strings.TrimSpace(value)
	if name == "" {
		return ""
	}
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimPrefix(name, "!")
	return strings.ToLower(strings.TrimSpace(name))
}

func stripInlineCommands(content string, inline []commands.ParsedCommand) string {
	if len(inline) == 0 || content == "" {
		return content
	}
	commandsCopy := append([]commands.ParsedCommand(nil), inline...)
	sort.Slice(commandsCopy, func(i, j int) bool {
		return commandsCopy[i].StartPos < commandsCopy[j].StartPos
	})

	cursor := 0
	var out strings.Builder
	for _, cmd := range commandsCopy {
		start := cmd.StartPos
		end := cmd.EndPos
		if cmd.Inline {
			end = cmd.StartPos + len(cmd.Prefix) + len(cmd.Name)
			if end < len(content) && content[end] == ':' {
				end++
			}
		}
		if start < cursor || start < 0 || end > len(content) || end <= start {
			continue
		}
		removedLeading := false
		if start > 0 && content[start-1] == ' ' {
			start--
			removedLeading = true
		}
		if !removedLeading && end < len(content) && content[end] == ' ' {
			end++
		}
		if start < cursor {
			start = cursor
		}
		out.WriteString(content[cursor:start])
		cursor = end
	}
	if cursor < len(content) {
		out.WriteString(content[cursor:])
	}
	return strings.TrimSpace(out.String())
}
