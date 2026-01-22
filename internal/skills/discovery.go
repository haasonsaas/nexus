package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// DiscoverySource discovers skills from a specific source.
type DiscoverySource interface {
	// Type returns the source type identifier.
	Type() SourceType

	// Priority returns the source priority (higher wins in conflicts).
	Priority() int

	// Discover scans for skills and returns found entries.
	Discover(ctx context.Context) ([]*SkillEntry, error)
}

// WatchableSource exposes paths for file watching.
type WatchableSource interface {
	WatchPaths() []string
}

// LocalSource discovers skills from a local directory.
type LocalSource struct {
	path       string
	sourceType SourceType
	priority   int
	logger     *slog.Logger
}

// NewLocalSource creates a local directory discovery source.
func NewLocalSource(path string, sourceType SourceType, priority int) *LocalSource {
	return &LocalSource{
		path:       path,
		sourceType: sourceType,
		priority:   priority,
		logger:     slog.Default().With("component", "skills", "source", sourceType),
	}
}

func (s *LocalSource) Type() SourceType {
	return s.sourceType
}

func (s *LocalSource) Priority() int {
	return s.priority
}

func (s *LocalSource) Discover(ctx context.Context) ([]*SkillEntry, error) {
	// Check if directory exists
	info, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		s.logger.Debug("skills directory does not exist", "path", s.path)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", s.path)
	}

	// List subdirectories (each is a potential skill)
	entries, err := os.ReadDir(s.path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var skills []*SkillEntry
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return skills, ctx.Err()
		default:
		}

		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(s.path, entry.Name())
		skillFile := filepath.Join(skillPath, SkillFilename)

		// Check if SKILL.md exists
		if _, err := os.Stat(skillFile); os.IsNotExist(err) {
			continue
		}

		// Parse skill file
		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			s.logger.Warn("failed to parse skill",
				"path", skillPath,
				"error", err)
			continue
		}

		// Set source metadata
		skill.Source = s.sourceType
		skill.SourcePriority = s.priority

		// Validate
		if err := ValidateSkill(skill); err != nil {
			s.logger.Warn("invalid skill",
				"path", skillPath,
				"error", err)
			continue
		}

		skills = append(skills, skill)
		s.logger.Debug("discovered skill",
			"name", skill.Name,
			"path", skillPath)
	}

	s.logger.Info("discovered skills",
		"count", len(skills),
		"path", s.path)

	return skills, nil
}

// WatchPaths returns the directory to watch for skill changes.
func (s *LocalSource) WatchPaths() []string {
	return []string{s.path}
}

// DiscoverAll discovers skills from multiple sources with precedence.
// Higher priority sources override lower priority ones on name conflicts.
func DiscoverAll(ctx context.Context, sources []DiscoverySource) ([]*SkillEntry, error) {
	skillMap := make(map[string]*SkillEntry)

	for _, source := range sources {
		skills, err := source.Discover(ctx)
		if err != nil {
			slog.Warn("skill discovery failed",
				"source", source.Type(),
				"error", err)
			continue
		}

		for _, skill := range skills {
			existing, ok := skillMap[skill.Name]
			if !ok {
				skillMap[skill.Name] = skill
				continue
			}

			// Higher priority wins
			if skill.SourcePriority > existing.SourcePriority {
				slog.Debug("skill override",
					"name", skill.Name,
					"oldSource", existing.Source,
					"newSource", skill.Source)
				skillMap[skill.Name] = skill
			}
		}
	}

	// Convert map to slice
	result := make([]*SkillEntry, 0, len(skillMap))
	for _, skill := range skillMap {
		result = append(result, skill)
	}

	return result, nil
}

// DefaultSourcePriorities defines the default priority order.
// Higher numbers = higher priority (wins in conflicts).
const (
	PriorityExtra     = 10  // skills.load.extraDirs
	PriorityBundled   = 20  // Shipped with binary
	PriorityLocal     = 30  // ~/.nexus/skills/
	PriorityWorkspace = 40  // <workspace>/skills/
)

// BuildDefaultSources creates the default discovery sources.
func BuildDefaultSources(workspacePath, localPath, bundledPath string, extraDirs []string) []DiscoverySource {
	var sources []DiscoverySource

	// Extra directories (lowest priority)
	for _, dir := range extraDirs {
		sources = append(sources, NewLocalSource(dir, SourceExtra, PriorityExtra))
	}

	// Bundled skills
	if bundledPath != "" {
		sources = append(sources, NewLocalSource(bundledPath, SourceBundled, PriorityBundled))
	}

	// Local skills (~/.nexus/skills/)
	if localPath != "" {
		sources = append(sources, NewLocalSource(localPath, SourceLocal, PriorityLocal))
	}

	// Workspace skills (highest priority)
	if workspacePath != "" {
		wsSkills := filepath.Join(workspacePath, "skills")
		sources = append(sources, NewLocalSource(wsSkills, SourceWorkspace, PriorityWorkspace))
	}

	return sources
}
