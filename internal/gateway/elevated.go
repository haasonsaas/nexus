package gateway

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

type elevatedDirectiveScope int

const (
	elevatedScopeNone elevatedDirectiveScope = iota
	elevatedScopeInline
	elevatedScopeSession
	elevatedScopeStatus
)

type elevatedDirective struct {
	Mode    agent.ElevatedMode
	Scope   elevatedDirectiveScope
	Cleaned string
}

var elevatedDirectiveRe = regexp.MustCompile(`(?i)/(?:elevated|elev)(?:\s+|:)?(on|off|ask|full)?`)

func parseElevatedDirective(content string) (elevatedDirective, bool) {
	loc := elevatedDirectiveRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return elevatedDirective{}, false
	}

	matched := content[loc[0]:loc[1]]
	arg := ""
	if len(loc) >= 4 && loc[2] != -1 && loc[3] != -1 {
		arg = content[loc[2]:loc[3]]
	}

	trimmed := strings.TrimSpace(content)
	if strings.EqualFold(trimmed, matched) {
		if strings.TrimSpace(arg) == "" {
			return elevatedDirective{Scope: elevatedScopeStatus}, true
		}
		mode, ok := agent.ParseElevatedMode(arg)
		if !ok {
			return elevatedDirective{}, false
		}
		return elevatedDirective{Mode: mode, Scope: elevatedScopeSession}, true
	}

	if strings.TrimSpace(arg) == "" {
		return elevatedDirective{}, false
	}
	mode, ok := agent.ParseElevatedMode(arg)
	if !ok {
		return elevatedDirective{}, false
	}
	cleaned := strings.TrimSpace(content[:loc[0]] + content[loc[1]:])
	return elevatedDirective{Mode: mode, Scope: elevatedScopeInline, Cleaned: cleaned}, true
}

func extractSenderID(msg *models.Message) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	if sender, ok := msg.Metadata["sender_id"].(string); ok && sender != "" {
		return sender
	}
	switch msg.Channel {
	case models.ChannelTelegram:
		if userID, ok := msg.Metadata["user_id"]; ok {
			switch v := userID.(type) {
			case int64:
				return strconv.FormatInt(v, 10)
			case int:
				return strconv.Itoa(v)
			case string:
				return v
			}
		}
	case models.ChannelSlack:
		if userID, ok := msg.Metadata["slack_user_id"].(string); ok && userID != "" {
			return userID
		}
	case models.ChannelDiscord:
		if userID, ok := msg.Metadata["discord_user_id"].(string); ok && userID != "" {
			return userID
		}
	case models.ChannelWhatsApp, models.ChannelSignal, models.ChannelIMessage, models.ChannelMatrix:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelAPI:
		if userID, ok := msg.Metadata["user_id"].(string); ok && userID != "" {
			return userID
		}
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	}
	return ""
}

func allowFromMatches(cfg config.ElevatedConfig, channel models.ChannelType, senderID string) bool {
	if senderID == "" {
		return false
	}
	if len(cfg.AllowFrom) == 0 {
		return false
	}
	channelKey := strings.ToLower(string(channel))
	allow := cfg.AllowFrom[channelKey]
	if len(allow) == 0 {
		allow = cfg.AllowFrom["default"]
	}
	if len(allow) == 0 {
		return false
	}
	return senderMatchesAllowlist(senderID, allow)
}

func senderMatchesAllowlist(senderID string, allow []string) bool {
	normalizedSender := normalizeAllowToken(senderID)
	if normalizedSender == "" {
		return false
	}
	for _, entry := range allow {
		token := normalizeAllowToken(entry)
		if token == "" {
			continue
		}
		if token == "*" {
			return true
		}
		if token == normalizedSender {
			return true
		}
	}
	return false
}

func normalizeAllowToken(value string) string {
	token := strings.TrimSpace(value)
	if token == "" {
		return ""
	}
	token = strings.TrimPrefix(token, "@")
	token = strings.TrimPrefix(token, "#")
	if idx := strings.Index(token, ":"); idx >= 0 {
		token = token[idx+1:]
	}
	return strings.ToLower(strings.TrimSpace(token))
}

func resolveElevatedPermission(global config.ElevatedConfig, agentCfg *config.ElevatedConfig, msg *models.Message) (bool, string) {
	if global.Enabled == nil || !*global.Enabled {
		return false, "tools.elevated.enabled"
	}
	if agentCfg != nil && agentCfg.Enabled != nil && !*agentCfg.Enabled {
		return false, "agents.list[].tools.elevated.enabled"
	}
	if !allowFromMatches(global, msg.Channel, extractSenderID(msg)) {
		return false, "tools.elevated.allow_from." + strings.ToLower(string(msg.Channel))
	}
	if agentCfg != nil && len(agentCfg.AllowFrom) > 0 && !allowFromMatches(*agentCfg, msg.Channel, extractSenderID(msg)) {
		return false, "agents.list[].tools.elevated.allow_from." + strings.ToLower(string(msg.Channel))
	}
	return true, ""
}

func effectiveElevatedTools(global config.ElevatedConfig, agentCfg *config.ElevatedConfig) []string {
	tools := global.Tools
	if len(tools) == 0 {
		tools = []string{"execute_code"}
	}
	if agentCfg != nil && len(agentCfg.Tools) > 0 {
		tools = agentCfg.Tools
	}
	return tools
}

func elevatedModeFromSession(session *models.Session) agent.ElevatedMode {
	if session == nil || session.Metadata == nil {
		return agent.ElevatedOff
	}
	if raw, ok := session.Metadata["elevated_mode"].(string); ok {
		if mode, ok := agent.ParseElevatedMode(raw); ok {
			return mode
		}
	}
	return agent.ElevatedOff
}

func setSessionElevatedMode(session *models.Session, mode agent.ElevatedMode) {
	if session == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	session.Metadata["elevated_mode"] = string(mode)
}

func formatElevatedStatus(mode agent.ElevatedMode, allowed bool, reason string) string {
	if !allowed {
		if reason != "" {
			return "Elevated mode is unavailable (" + reason + ")."
		}
		return "Elevated mode is unavailable."
	}
	return "Elevated mode is " + string(mode) + "."
}

func formatElevatedSet(mode agent.ElevatedMode) string {
	if mode == agent.ElevatedOff {
		return "Elevated mode disabled."
	}
	return "Elevated mode set to " + string(mode) + "."
}

func formatElevatedUnavailable(reason string) string {
	if reason == "" {
		return "Elevated mode is unavailable."
	}
	return "Elevated mode is unavailable (" + reason + ")."
}
