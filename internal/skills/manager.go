package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Manager manages skill discovery, loading, and eligibility.
type Manager struct {
	sources  []DiscoverySource
	config   *SkillsConfig
	logger   *slog.Logger

	// All discovered skills
	skills   map[string]*SkillEntry
	skillsMu sync.RWMutex

	// Eligible skills (after gating)
	eligible   map[string]*SkillEntry
	eligibleMu sync.RWMutex

	// Gating context
	gatingCtx *GatingContext
}

// NewManager creates a new skill manager.
func NewManager(cfg *SkillsConfig, workspacePath string, configValues map[string]any) (*Manager, error) {
	if cfg == nil {
		cfg = &SkillsConfig{}
	}

	// Build default sources
	homeDir, _ := os.UserHomeDir()
	localPath := filepath.Join(homeDir, ".nexus", "skills")

	var extraDirs []string
	if cfg.Load != nil {
		extraDirs = cfg.Load.ExtraDirs
	}

	sources := BuildDefaultSources(workspacePath, localPath, "", extraDirs)

	// Add configured sources
	for _, srcCfg := range cfg.Sources {
		switch srcCfg.Type {
		case SourceLocal, SourceExtra:
			sources = append(sources, NewLocalSource(srcCfg.Path, srcCfg.Type, PriorityExtra))
		// TODO: Add git and registry sources
		}
	}

	// Create gating context
	gatingCtx := NewGatingContext(cfg.Entries, configValues)

	return &Manager{
		sources:   sources,
		config:    cfg,
		logger:    slog.Default().With("component", "skills"),
		skills:    make(map[string]*SkillEntry),
		eligible:  make(map[string]*SkillEntry),
		gatingCtx: gatingCtx,
	}, nil
}

// Discover scans all sources for skills.
func (m *Manager) Discover(ctx context.Context) error {
	skills, err := DiscoverAll(ctx, m.sources)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	m.skillsMu.Lock()
	m.skills = make(map[string]*SkillEntry)
	for _, skill := range skills {
		m.skills[skill.Name] = skill
	}
	m.skillsMu.Unlock()

	m.logger.Info("discovered skills", "count", len(skills))

	// Refresh eligible list
	return m.RefreshEligible()
}

// RefreshEligible updates the list of eligible skills based on gating.
func (m *Manager) RefreshEligible() error {
	m.skillsMu.RLock()
	allSkills := make([]*SkillEntry, 0, len(m.skills))
	for _, skill := range m.skills {
		allSkills = append(allSkills, skill)
	}
	m.skillsMu.RUnlock()

	eligible := FilterEligible(allSkills, m.gatingCtx)

	m.eligibleMu.Lock()
	m.eligible = make(map[string]*SkillEntry)
	for _, skill := range eligible {
		m.eligible[skill.Name] = skill
	}
	m.eligibleMu.Unlock()

	m.logger.Info("eligible skills",
		"eligible", len(eligible),
		"total", len(allSkills))

	return nil
}

// GetSkill returns a skill by name (from all discovered skills).
func (m *Manager) GetSkill(name string) (*SkillEntry, bool) {
	m.skillsMu.RLock()
	defer m.skillsMu.RUnlock()
	skill, ok := m.skills[name]
	return skill, ok
}

// GetEligible returns an eligible skill by name.
func (m *Manager) GetEligible(name string) (*SkillEntry, bool) {
	m.eligibleMu.RLock()
	defer m.eligibleMu.RUnlock()
	skill, ok := m.eligible[name]
	return skill, ok
}

// ListAll returns all discovered skills.
func (m *Manager) ListAll() []*SkillEntry {
	m.skillsMu.RLock()
	defer m.skillsMu.RUnlock()

	result := make([]*SkillEntry, 0, len(m.skills))
	for _, skill := range m.skills {
		result = append(result, skill)
	}
	sortSkills(result)
	return result
}

// ListEligible returns all eligible skills.
func (m *Manager) ListEligible() []*SkillEntry {
	m.eligibleMu.RLock()
	defer m.eligibleMu.RUnlock()

	result := make([]*SkillEntry, 0, len(m.eligible))
	for _, skill := range m.eligible {
		result = append(result, skill)
	}
	sortSkills(result)
	return result
}

// ListSnapshots returns lightweight snapshots of eligible skills.
func (m *Manager) ListSnapshots() []*SkillSnapshot {
	eligible := m.ListEligible()
	snapshots := make([]*SkillSnapshot, len(eligible))
	for i, skill := range eligible {
		snapshots[i] = skill.ToSnapshot()
	}
	return snapshots
}

// LoadContent loads the full content of a skill (lazy loading).
func (m *Manager) LoadContent(name string) (string, error) {
	skill, ok := m.GetSkill(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}

	// Already loaded
	if skill.Content != "" {
		return skill.Content, nil
	}

	// Read from file
	skillFile := filepath.Join(skill.Path, SkillFilename)
	parsed, err := ParseSkillFile(skillFile)
	if err != nil {
		return "", fmt.Errorf("parse skill file: %w", err)
	}

	// Update cached content
	m.skillsMu.Lock()
	skill.Content = parsed.Content
	m.skillsMu.Unlock()

	return skill.Content, nil
}

// CheckEligibility checks if a skill is eligible and returns the reason if not.
func (m *Manager) CheckEligibility(name string) (*EligibilityResult, error) {
	skill, ok := m.GetSkill(name)
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	result := skill.CheckEligibility(m.gatingCtx)
	return &result, nil
}

// GetIneligibleReasons returns reasons for all ineligible skills.
func (m *Manager) GetIneligibleReasons() map[string]string {
	allSkills := m.ListAll()
	return GetIneligibleReasons(allSkills, m.gatingCtx)
}

// InjectEnv injects skill environment variables for a session.
// Returns a function to restore the original environment.
func (m *Manager) InjectEnv(skillNames []string) (restore func()) {
	originalEnv := make(map[string]string)
	injected := make(map[string]string)

	for _, name := range skillNames {
		skill, ok := m.GetEligible(name)
		if !ok {
			continue
		}

		cfg, ok := m.config.Entries[skill.ConfigKey()]
		if !ok {
			continue
		}

		// API key shorthand
		if cfg.APIKey != "" && skill.Metadata != nil && skill.Metadata.PrimaryEnv != "" {
			envVar := skill.Metadata.PrimaryEnv
			if _, exists := os.LookupEnv(envVar); !exists {
				originalEnv[envVar] = ""
				injected[envVar] = cfg.APIKey
			}
		}

		// Explicit env overrides
		for k, v := range cfg.Env {
			if _, exists := os.LookupEnv(k); !exists {
				originalEnv[k] = ""
				injected[k] = v
			}
		}
	}

	// Apply injections
	for k, v := range injected {
		os.Setenv(k, v)
	}

	// Return restore function
	return func() {
		for k := range injected {
			if original, ok := originalEnv[k]; ok && original == "" {
				os.Unsetenv(k)
			}
		}
	}
}

// sortSkills sorts skills alphabetically by name.
func sortSkills(skills []*SkillEntry) {
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
}

// Close cleans up manager resources.
func (m *Manager) Close() error {
	// TODO: Stop file watcher if enabled
	return nil
}
