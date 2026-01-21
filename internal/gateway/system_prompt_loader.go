package gateway

import (
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) systemPromptForMessage(session *models.Session, msg *models.Message) string {
	if s.config == nil {
		return ""
	}

	opts := SystemPromptOptions{
		ToolNotes: s.loadToolNotes(),
		Heartbeat: s.loadHeartbeat(),
	}

	if s.config.Session.Memory.Enabled && s.memoryLogger != nil {
		lines, err := s.memoryLogger.ReadRecentAt(time.Now(), msg.Channel, session.ID, 2, s.config.Session.Memory.MaxLines)
		if err != nil {
			s.logger.Error("failed to read memory log", "error", err)
		} else {
			opts.MemoryLines = lines
		}
	}

	return buildSystemPrompt(s.config, opts)
}

func (s *Server) loadToolNotes() string {
	if s.config == nil {
		return ""
	}

	inline := strings.TrimSpace(s.config.Tools.Notes)
	filePath := strings.TrimSpace(s.config.Tools.NotesFile)
	if filePath == "" {
		return inline
	}

	content, err := readPromptFile(filePath)
	if err != nil {
		s.logger.Error("failed to read tool notes file", "error", err)
		return inline
	}
	if content == "" {
		return inline
	}
	if inline == "" {
		return content
	}
	return inline + "\n" + content
}

func (s *Server) loadHeartbeat() string {
	if s.config == nil || !s.config.Session.Heartbeat.Enabled {
		return ""
	}
	path := strings.TrimSpace(s.config.Session.Heartbeat.File)
	if path == "" {
		return ""
	}
	content, err := readPromptFile(path)
	if err != nil {
		s.logger.Error("failed to read heartbeat file", "error", err)
		return ""
	}
	return content
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
