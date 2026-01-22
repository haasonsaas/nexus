package gateway

import (
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

func allowlistForChannel(allowFrom map[string][]string, channel models.ChannelType) []string {
	if len(allowFrom) == 0 {
		return nil
	}
	channelKey := strings.ToLower(string(channel))
	if allow := allowFrom[channelKey]; len(allow) > 0 {
		return allow
	}
	return allowFrom["default"]
}

func allowlistMatches(allowFrom map[string][]string, channel models.ChannelType, senderID string) bool {
	if senderID == "" {
		return false
	}
	allow := allowlistForChannel(allowFrom, channel)
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
		if token == "*" || token == normalizedSender {
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
