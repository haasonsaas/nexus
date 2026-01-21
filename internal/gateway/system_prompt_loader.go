package gateway

import (
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) systemPromptForMessage(session *models.Session, msg *models.Message) string {
	if s.config == nil {
		return ""
	}

	opts := SystemPromptOptions{
		ToolNotes: s.loadToolNotes(),
		Heartbeat: s.loadHeartbeat(msg),
	}

	if s.config.Session.Memory.Enabled && s.memoryLogger != nil {
		channelID := msg.Channel
		sessionID := session.ID
		switch strings.ToLower(strings.TrimSpace(s.config.Session.Memory.Scope)) {
		case "channel":
			sessionID = ""
		case "global":
			channelID = ""
			sessionID = ""
		}
		days := s.config.Session.Memory.Days
		lines, err := s.memoryLogger.ReadRecentAt(time.Now(), channelID, sessionID, days, s.config.Session.Memory.MaxLines)
		if err != nil {
			s.logger.Error("failed to read memory log", "error", err)
		} else {
			opts.MemoryLines = lines
		}
	}

	return buildSystemPrompt(s.config, opts)
}

func (s *Server) loadToolNotes() string {
	notes, err := loadToolNotesFromConfig(s.config)
	if err != nil {
		s.logger.Error("failed to read tool notes file", "error", err)
		return strings.TrimSpace(s.config.Tools.Notes)
	}
	return notes
}

func (s *Server) loadHeartbeat(msg *models.Message) string {
	content, err := loadHeartbeatFromConfig(s.config, msg)
	if err != nil {
		s.logger.Error("failed to read heartbeat file", "error", err)
		return ""
	}
	return content
}

// BuildSystemPrompt assembles the system prompt using the provided config, session, and message context.
func BuildSystemPrompt(cfg *config.Config, sessionID string, msg *models.Message) (string, error) {
	if cfg == nil {
		return "", nil
	}
	if msg == nil {
		msg = &models.Message{}
	}

	opts := SystemPromptOptions{}
	notes, err := loadToolNotesFromConfig(cfg)
	if err != nil {
		return "", err
	}
	opts.ToolNotes = notes

	heartbeat, err := loadHeartbeatFromConfig(cfg, msg)
	if err != nil {
		return "", err
	}
	opts.Heartbeat = heartbeat

	if cfg.Session.Memory.Enabled {
		logger := sessions.NewMemoryLogger(cfg.Session.Memory.Directory)
		channelID := msg.Channel
		session := sessionID
		switch strings.ToLower(strings.TrimSpace(cfg.Session.Memory.Scope)) {
		case "channel":
			session = ""
		case "global":
			channelID = ""
			session = ""
		}
		lines, err := logger.ReadRecentAt(time.Now(), channelID, session, cfg.Session.Memory.Days, cfg.Session.Memory.MaxLines)
		if err != nil {
			return "", err
		}
		opts.MemoryLines = lines
	}

	return buildSystemPrompt(cfg, opts), nil
}

func isHeartbeatMessage(msg *models.Message) bool {
	if msg == nil {
		return false
	}
	if msg.Metadata != nil {
		if flag, ok := msg.Metadata["heartbeat"].(bool); ok && flag {
			return true
		}
	}
	content := strings.TrimSpace(strings.ToLower(msg.Content))
	if content == "heartbeat" {
		return true
	}
	return strings.HasPrefix(content, "heartbeat ")
}

func loadToolNotesFromConfig(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", nil
	}
	inline := strings.TrimSpace(cfg.Tools.Notes)
	filePath := strings.TrimSpace(cfg.Tools.NotesFile)
	if filePath == "" {
		return inline, nil
	}

	content, err := readPromptFile(filePath)
	if err != nil {
		return inline, err
	}
	if content == "" {
		return inline, nil
	}
	if inline == "" {
		return content, nil
	}
	return inline + "\n" + content, nil
}

func loadHeartbeatFromConfig(cfg *config.Config, msg *models.Message) (string, error) {
	if cfg == nil || !cfg.Session.Heartbeat.Enabled {
		return "", nil
	}
	if strings.EqualFold(cfg.Session.Heartbeat.Mode, "on_demand") && !isHeartbeatMessage(msg) {
		return "", nil
	}
	path := strings.TrimSpace(cfg.Session.Heartbeat.File)
	if path == "" {
		return "", nil
	}
	content, err := readPromptFile(path)
	if err != nil {
		return "", err
	}
	return content, nil
}

func readPromptFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
