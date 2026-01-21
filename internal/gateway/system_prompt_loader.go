package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) systemPromptForMessage(ctx context.Context, session *models.Session, msg *models.Message) string {
	if s.config == nil {
		return ""
	}

	opts := SystemPromptOptions{
		ToolNotes:         s.loadToolNotes(),
		Heartbeat:         s.loadHeartbeat(msg),
		WorkspaceSections: s.loadWorkspaceSections(),
		MemoryFlush:       s.memoryFlushPrompt(ctx, session),
		SkillContent:      s.loadSkillSections(ctx),
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

func (s *Server) loadWorkspaceSections() []PromptSection {
	sections, err := loadWorkspaceSectionsFromConfig(s.config)
	if err != nil {
		s.logger.Error("failed to read workspace files", "error", err)
		return nil
	}
	return sections
}

func (s *Server) loadHeartbeat(msg *models.Message) string {
	content, err := loadHeartbeatFromConfig(s.config, msg)
	if err != nil {
		s.logger.Error("failed to read heartbeat file", "error", err)
		return ""
	}
	return content
}

func (s *Server) loadSkillSections(ctx context.Context) []SkillSection {
	if s.skillsManager == nil {
		return nil
	}

	eligible := s.skillsManager.ListEligible()
	if len(eligible) == 0 {
		return nil
	}

	sections := make([]SkillSection, 0, len(eligible))
	for _, skill := range eligible {
		content, err := s.skillsManager.LoadContent(skill.Name)
		if err != nil {
			s.logger.Error("failed to load skill content",
				"skill", skill.Name,
				"error", err)
			continue
		}
		if content == "" {
			continue
		}
		sections = append(sections, SkillSection{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     content,
		})
	}

	return sections
}

func (s *Server) memoryFlushPrompt(ctx context.Context, session *models.Session) string {
	if s.config == nil || !s.config.Session.MemoryFlush.Enabled {
		return ""
	}
	if session == nil || s.sessions == nil {
		return ""
	}
	threshold := s.config.Session.MemoryFlush.Threshold
	if threshold <= 0 {
		return ""
	}

	today := time.Now().Format("2006-01-02")
	if session.Metadata != nil {
		if value, ok := session.Metadata["memory_flush_date"].(string); ok && value == today {
			return ""
		}
	}

	history, err := s.sessions.GetHistory(ctx, session.ID, threshold)
	if err != nil {
		s.logger.Error("failed to read session history", "error", err)
		return ""
	}
	if len(history) < threshold {
		return ""
	}

	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	session.Metadata["memory_flush_date"] = today
	session.Metadata["memory_flush_pending"] = true
	if err := s.sessions.Update(ctx, session); err != nil {
		s.logger.Error("failed to update session metadata", "error", err)
	}

	return s.config.Session.MemoryFlush.Prompt
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

	sections, err := loadWorkspaceSectionsFromConfig(cfg)
	if err != nil {
		return "", err
	}
	opts.WorkspaceSections = sections

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

	// Load skill content
	skillSections, err := loadSkillSectionsFromConfig(cfg)
	if err != nil {
		return "", err
	}
	opts.SkillContent = skillSections

	return buildSystemPrompt(cfg, opts), nil
}

// loadSkillSectionsFromConfig loads skill content from the skills configuration.
func loadSkillSectionsFromConfig(cfg *config.Config) ([]SkillSection, error) {
	if cfg == nil {
		return nil, nil
	}

	mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Discover(ctx); err != nil {
		return nil, err
	}

	eligible := mgr.ListEligible()
	if len(eligible) == 0 {
		return nil, nil
	}

	sections := make([]SkillSection, 0, len(eligible))
	for _, skill := range eligible {
		content, err := mgr.LoadContent(skill.Name)
		if err != nil {
			continue // Skip skills that fail to load
		}
		if content == "" {
			continue
		}
		sections = append(sections, SkillSection{
			Name:        skill.Name,
			Description: skill.Description,
			Content:     content,
		})
	}

	return sections, nil
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
		workspaceFile := resolveWorkspaceFile(cfg, strings.TrimSpace(cfg.Workspace.ToolsFile))
		if cfg.Workspace.Enabled && workspaceFile != "" {
			content, err := readPromptFileLimited(workspaceFile, cfg.Workspace.MaxChars)
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
	return readPromptFileLimited(path, 0)
}

func readPromptFileLimited(path string, maxChars int) (string, error) {
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
	content := strings.TrimSpace(string(data))
	if maxChars <= 0 {
		return content, nil
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content, nil
	}
	truncated := strings.TrimSpace(string(runes[:maxChars]))
	if truncated == "" {
		return "", nil
	}
	return truncated + "\n...(truncated)", nil
}

func loadWorkspaceSectionsFromConfig(cfg *config.Config) ([]PromptSection, error) {
	if cfg == nil || !cfg.Workspace.Enabled {
		return nil, nil
	}

	sections := make([]PromptSection, 0, 5)
	add := func(label, filename string) error {
		path := resolveWorkspaceFile(cfg, filename)
		if path == "" {
			return nil
		}
		content, err := readPromptFileLimited(path, cfg.Workspace.MaxChars)
		if err != nil {
			return err
		}
		if strings.TrimSpace(content) == "" {
			return nil
		}
		sections = append(sections, PromptSection{Label: label, Content: content})
		return nil
	}

	if err := add("Workspace instructions", cfg.Workspace.AgentsFile); err != nil {
		return nil, err
	}
	if err := add("Persona and boundaries", cfg.Workspace.SoulFile); err != nil {
		return nil, err
	}
	if err := add("Workspace user profile", cfg.Workspace.UserFile); err != nil {
		return nil, err
	}
	if err := add("Workspace identity", cfg.Workspace.IdentityFile); err != nil {
		return nil, err
	}
	if err := add("Workspace memory", cfg.Workspace.MemoryFile); err != nil {
		return nil, err
	}

	return sections, nil
}

func resolveWorkspaceFile(cfg *config.Config, filename string) string {
	if cfg == nil {
		return ""
	}
	name := strings.TrimSpace(filename)
	if name == "" {
		return ""
	}
	if filepath.IsAbs(name) {
		return name
	}
	base := strings.TrimSpace(cfg.Workspace.Path)
	if base == "" {
		return name
	}
	return filepath.Join(base, name)
}
