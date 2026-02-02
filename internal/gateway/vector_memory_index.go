package gateway

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

const vectorMemoryIndexTimeout = 30 * time.Second

func (s *Server) maybeIndexVectorMemory(_ context.Context, session *models.Session, msg *models.Message) {
	if s == nil || s.vectorMemory == nil || s.config == nil {
		return
	}
	cfg := s.config.VectorMemory
	if !cfg.Enabled || !cfg.Indexing.AutoIndexMessages {
		return
	}
	if session == nil || msg == nil {
		return
	}
	if msg.Role != models.RoleUser && msg.Role != models.RoleAssistant {
		return
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return
	}
	if cfg.Indexing.MinContentLength > 0 && len(content) < cfg.Indexing.MinContentLength {
		return
	}

	createdAt := msg.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	metadata := models.MemoryMetadata{
		Source: "auto-index",
		Role:   string(msg.Role),
		Tags:   []string{string(msg.Role)},
		Extra: map[string]any{
			"session_id": session.ID,
			"channel_id": session.ChannelID,
			"agent_id":   session.AgentID,
			"channel":    string(msg.Channel),
			"direction":  string(msg.Direction),
			"scope":      "session",
		},
	}
	if msg.ID != "" {
		metadata.Extra["message_id"] = msg.ID
	}

	entry := &models.MemoryEntry{
		ID:        uuid.New().String(),
		SessionID: session.ID,
		Content:   content,
		Metadata:  metadata,
		CreatedAt: createdAt,
		UpdatedAt: time.Now(),
	}

	go func(entry *models.MemoryEntry) {
		ctx, cancel := context.WithTimeout(context.Background(), vectorMemoryIndexTimeout)
		defer cancel()
		if err := s.vectorMemory.Index(ctx, []*models.MemoryEntry{entry}); err != nil {
			s.logger.Warn("vector memory auto-index failed", "error", err)
		}
	}(entry)
}
