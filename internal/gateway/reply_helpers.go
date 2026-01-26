package gateway

import (
	"context"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/reply"
	"github.com/haasonsaas/nexus/pkg/models"
)

func normalizeReplyContent(content string) (string, bool, string) {
	reason := ""
	if reply.IsSilentReplyText(content) {
		reason = "silent_reply"
		content = reply.StripSilentToken(content)
		if strings.TrimSpace(content) == "" {
			return "", true, reason
		}
	}
	if reply.HasHeartbeatToken(content) {
		reason = "heartbeat"
		content = reply.StripHeartbeatToken(content)
		if strings.TrimSpace(content) == "" {
			return "", true, reason
		}
	}
	return content, false, ""
}

func (s *Server) confirmMemoryFlush(ctx context.Context, session *models.Session) {
	if s == nil || s.sessions == nil || session == nil || session.Metadata == nil {
		return
	}
	if pending, ok := session.Metadata["memory_flush_pending"].(bool); ok && pending {
		session.Metadata["memory_flush_pending"] = false
		session.Metadata["memory_flush_confirmed_at"] = time.Now().Format(time.RFC3339)
		if err := s.sessions.Update(ctx, session); err != nil && s.logger != nil {
			s.logger.Warn("failed to update session metadata", "error", err)
		}
	}
}
