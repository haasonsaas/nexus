package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// startMemoryConsolidation launches the background consolidation worker.
func (s *Server) startMemoryConsolidation(ctx context.Context) {
	if s == nil || s.config == nil || s.vectorMemory == nil {
		return
	}
	cfg := s.config.VectorMemory.Consolidation
	if !cfg.Enabled {
		return
	}

	s.runtimeMu.Lock()
	if s.sessions == nil {
		store, err := s.newSessionStore()
		if err != nil {
			s.logger.Warn("memory consolidation disabled (session store init failed)", "error", err)
			s.runtimeMu.Unlock()
			return
		}
		s.sessions = store
	}
	s.runtimeMu.Unlock()

	interval := cfg.Interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.runMemoryConsolidation(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runMemoryConsolidation(ctx)
			}
		}
	}()
}

func (s *Server) runMemoryConsolidation(ctx context.Context) {
	if s.vectorMemory == nil || s.sessions == nil {
		return
	}
	cfg := s.config.VectorMemory.Consolidation
	if !cfg.Enabled {
		return
	}

	sessionList, err := s.sessions.List(ctx, "", sessions.ListOptions{
		Limit: cfg.MaxSessions,
	})
	if err != nil {
		s.logger.Warn("memory consolidation: list sessions failed", "error", err)
		return
	}

	for _, sess := range sessionList {
		if sess == nil {
			continue
		}
		if s.shouldSkipConsolidation(sess, cfg.Interval) {
			continue
		}

		history, err := s.sessions.GetHistory(ctx, sess.ID, cfg.MaxMessages)
		if err != nil {
			s.logger.Debug("memory consolidation: get history failed", "session", sess.ID, "error", err)
			continue
		}
		if len(history) < cfg.MinMessages {
			continue
		}

		summary, err := s.summarizeSession(ctx, history, cfg, s.defaultModel)
		if err != nil {
			s.logger.Warn("memory consolidation: summarize failed", "session", sess.ID, "error", err)
			continue
		}
		if strings.TrimSpace(summary) == "" {
			continue
		}

		entry := &models.MemoryEntry{
			ID:        uuid.New().String(),
			SessionID: sess.ID,
			ChannelID: sess.ChannelID,
			AgentID:   sess.AgentID,
			Content:   summary,
			Metadata: models.MemoryMetadata{
				Source: "consolidation",
				Role:   string(models.RoleSystem),
				Tags:   []string{"summary"},
				Extra: map[string]any{
					"consolidated_at": time.Now().Format(time.RFC3339),
				},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := s.vectorMemory.Index(ctx, []*models.MemoryEntry{entry}); err != nil {
			s.logger.Warn("memory consolidation: index failed", "session", sess.ID, "error", err)
			continue
		}

		s.markConsolidated(ctx, sess)
	}
}

func (s *Server) summarizeSession(ctx context.Context, history []*models.Message, cfg memory.ConsolidationConfig, model string) (string, error) {
	if len(history) == 0 {
		return "", nil
	}

	// Use LLM if available
	if s.llmProvider != nil {
		prompt := buildConsolidationPrompt(history, cfg.SummaryMaxChars)
		req := &agent.CompletionRequest{
			Model:     model,
			System:    "Summarize the conversation into durable facts, preferences, and decisions. Output concise bullet points.",
			Messages:  []agent.CompletionMessage{{Role: "user", Content: prompt}},
			MaxTokens: cfg.SummaryMaxTokens,
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		text, err := collectCompletion(ctx, s.llmProvider, req)
		if err == nil {
			return strings.TrimSpace(text), nil
		}
		s.logger.Debug("memory consolidation: LLM summary failed, falling back", "error", err)
	}

	return heuristicSummary(history, cfg.SummaryMaxChars), nil
}

func buildConsolidationPrompt(history []*models.Message, maxChars int) string {
	var sb strings.Builder
	sb.WriteString("Conversation:\n")
	for _, msg := range history {
		if msg == nil {
			continue
		}
		if msg.Role != models.RoleUser && msg.Role != models.RoleAssistant {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, content))
		if maxChars > 0 && sb.Len() > maxChars {
			break
		}
	}
	return sb.String()
}

func heuristicSummary(history []*models.Message, maxChars int) string {
	var lines []string
	for _, msg := range history {
		if msg == nil {
			continue
		}
		if msg.Role != models.RoleUser && msg.Role != models.RoleAssistant {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		line := fmt.Sprintf("%s: %s", msg.Role, truncateContent(content, 200))
		lines = append(lines, line)
		if maxChars > 0 && len(strings.Join(lines, "\n")) >= maxChars {
			break
		}
	}
	summary := strings.Join(lines, "\n")
	if maxChars > 0 && len(summary) > maxChars {
		return summary[:maxChars]
	}
	return summary
}

func collectCompletion(ctx context.Context, provider agent.LLMProvider, req *agent.CompletionRequest) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("llm provider unavailable")
	}
	ch, err := provider.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for chunk := range ch {
		if chunk == nil {
			continue
		}
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.Text != "" {
			sb.WriteString(chunk.Text)
		}
		if chunk.Done {
			break
		}
	}
	return sb.String(), nil
}

func (s *Server) shouldSkipConsolidation(sess *models.Session, interval time.Duration) bool {
	if sess == nil || sess.Metadata == nil {
		return false
	}
	raw, ok := sess.Metadata["memory_consolidated_at"].(string)
	if !ok || raw == "" {
		return false
	}
	last, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	if interval <= 0 {
		return false
	}
	return time.Since(last) < interval
}

func (s *Server) markConsolidated(ctx context.Context, sess *models.Session) {
	if sess == nil || s.sessions == nil {
		return
	}
	if sess.Metadata == nil {
		sess.Metadata = map[string]any{}
	}
	sess.Metadata["memory_consolidated_at"] = time.Now().Format(time.RFC3339)
	if err := s.sessions.Update(ctx, sess); err != nil {
		s.logger.Debug("memory consolidation: update session metadata failed", "session", sess.ID, "error", err)
	}
}
