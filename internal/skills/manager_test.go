package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerStartWatchingTracksSkillDirs(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}

	skillPath := filepath.Join(skillsDir, "alpha")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skillFile := filepath.Join(skillPath, SkillFilename)
	if err := os.WriteFile(skillFile, []byte("---\nname: alpha\ndescription: test skill\n---\n# Alpha\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	cfg := &SkillsConfig{
		Load: &LoadConfig{
			Watch:           true,
			WatchDebounceMs: 10,
		},
	}
	manager, err := NewManager(cfg, workspace, nil)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if err := manager.Discover(context.Background()); err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if err := manager.StartWatching(context.Background()); err != nil {
		t.Fatalf("StartWatching error: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		manager.watchMu.Lock()
		_, ok := manager.watchPaths[skillPath]
		manager.watchMu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected watcher to include %s", skillPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSkillEntry_ConfigKey(t *testing.T) {
	tests := []struct {
		name     string
		skill    *SkillEntry
		expected string
	}{
		{
			name:     "uses skill name when no metadata",
			skill:    &SkillEntry{Name: "my-skill"},
			expected: "my-skill",
		},
		{
			name:     "uses skill name when metadata is nil",
			skill:    &SkillEntry{Name: "another-skill", Metadata: nil},
			expected: "another-skill",
		},
		{
			name:     "uses skill name when skillKey is empty",
			skill:    &SkillEntry{Name: "test-skill", Metadata: &SkillMetadata{SkillKey: ""}},
			expected: "test-skill",
		},
		{
			name:     "uses skillKey when set",
			skill:    &SkillEntry{Name: "original", Metadata: &SkillMetadata{SkillKey: "custom-key"}},
			expected: "custom-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.skill.ConfigKey(); got != tt.expected {
				t.Errorf("ConfigKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSkillEntry_IsEnabled(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name      string
		skill     *SkillEntry
		overrides map[string]*SkillConfig
		expected  bool
	}{
		{
			name:      "enabled by default with nil overrides",
			skill:     &SkillEntry{Name: "test"},
			overrides: nil,
			expected:  true,
		},
		{
			name:      "enabled by default with empty overrides",
			skill:     &SkillEntry{Name: "test"},
			overrides: map[string]*SkillConfig{},
			expected:  true,
		},
		{
			name:      "enabled by default when skill not in overrides",
			skill:     &SkillEntry{Name: "test"},
			overrides: map[string]*SkillConfig{"other": {}},
			expected:  true,
		},
		{
			name:      "enabled when nil Enabled field",
			skill:     &SkillEntry{Name: "test"},
			overrides: map[string]*SkillConfig{"test": {}},
			expected:  true,
		},
		{
			name:      "explicitly enabled",
			skill:     &SkillEntry{Name: "test"},
			overrides: map[string]*SkillConfig{"test": {Enabled: &enabled}},
			expected:  true,
		},
		{
			name:      "explicitly disabled",
			skill:     &SkillEntry{Name: "test"},
			overrides: map[string]*SkillConfig{"test": {Enabled: &disabled}},
			expected:  false,
		},
		{
			name:      "uses skillKey for lookup",
			skill:     &SkillEntry{Name: "original", Metadata: &SkillMetadata{SkillKey: "custom"}},
			overrides: map[string]*SkillConfig{"custom": {Enabled: &disabled}},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.skill.IsEnabled(tt.overrides); got != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSkillEntry_ToSnapshot(t *testing.T) {
	skill := &SkillEntry{
		Name:        "test-skill",
		Description: "A test skill",
		Path:        "/path/to/skill",
		Content:     "This should not be in snapshot",
		Source:      SourceLocal,
	}

	snapshot := skill.ToSnapshot()

	if snapshot.Name != skill.Name {
		t.Errorf("Name = %q, want %q", snapshot.Name, skill.Name)
	}
	if snapshot.Description != skill.Description {
		t.Errorf("Description = %q, want %q", snapshot.Description, skill.Description)
	}
	if snapshot.Path != skill.Path {
		t.Errorf("Path = %q, want %q", snapshot.Path, skill.Path)
	}
}

func TestSourceType_Constants(t *testing.T) {
	tests := []struct {
		source   SourceType
		expected string
	}{
		{SourceBundled, "bundled"},
		{SourceLocal, "local"},
		{SourceWorkspace, "workspace"},
		{SourceExtra, "extra"},
		{SourceGit, "git"},
		{SourceRegistry, "registry"},
	}

	for _, tt := range tests {
		if string(tt.source) != tt.expected {
			t.Errorf("SourceType = %q, want %q", tt.source, tt.expected)
		}
	}
}

func TestExecutionLocation_Constants(t *testing.T) {
	tests := []struct {
		loc      ExecutionLocation
		expected string
	}{
		{ExecCore, "core"},
		{ExecEdge, "edge"},
		{ExecAny, "any"},
	}

	for _, tt := range tests {
		if string(tt.loc) != tt.expected {
			t.Errorf("ExecutionLocation = %q, want %q", tt.loc, tt.expected)
		}
	}
}

func TestSkillMetadata_Struct(t *testing.T) {
	metadata := &SkillMetadata{
		Emoji:      "🔧",
		Always:     true,
		OS:         []string{"darwin", "linux"},
		PrimaryEnv: "API_KEY",
		SkillKey:   "custom-key",
		Execution:  ExecCore,
		ToolGroups: []string{"browser"},
		Requires: &SkillRequires{
			Bins:    []string{"git", "curl"},
			AnyBins: []string{"vim", "nvim"},
			Env:     []string{"HOME"},
			Config:  []string{"api.enabled"},
		},
		Install: []InstallSpec{
			{ID: "brew", Kind: "brew", Formula: "git"},
		},
	}

	if metadata.Emoji != "🔧" {
		t.Errorf("Emoji = %q", metadata.Emoji)
	}
	if !metadata.Always {
		t.Error("Always should be true")
	}
	if len(metadata.OS) != 2 {
		t.Errorf("OS length = %d, want 2", len(metadata.OS))
	}
	if metadata.Requires == nil {
		t.Fatal("Requires should not be nil")
	}
	if len(metadata.Requires.Bins) != 2 {
		t.Errorf("Requires.Bins length = %d, want 2", len(metadata.Requires.Bins))
	}
}

func TestSkillConfig_Struct(t *testing.T) {
	enabled := true
	cfg := &SkillConfig{
		Enabled: &enabled,
		APIKey:  "sk-test-key",
		Env:     map[string]string{"DEBUG": "true"},
		Config:  map[string]any{"timeout": 30},
	}

	if cfg.Enabled == nil || !*cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.APIKey != "sk-test-key" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.Env["DEBUG"] != "true" {
		t.Errorf("Env[DEBUG] = %q", cfg.Env["DEBUG"])
	}
}

func TestSourceConfig_Struct(t *testing.T) {
	cfg := &SourceConfig{
		Type:    SourceGit,
		Path:    "/path/to/skills",
		URL:     "https://github.com/user/skills",
		Branch:  "main",
		SubPath: "skills/",
		Refresh: 1 * time.Hour,
		Auth:    "token123",
	}

	if cfg.Type != SourceGit {
		t.Errorf("Type = %q, want %q", cfg.Type, SourceGit)
	}
	if cfg.URL != "https://github.com/user/skills" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Refresh != 1*time.Hour {
		t.Errorf("Refresh = %v", cfg.Refresh)
	}
}

func TestLoadConfig_Struct(t *testing.T) {
	cfg := &LoadConfig{
		ExtraDirs:       []string{"/extra/dir1", "/extra/dir2"},
		Watch:           true,
		WatchDebounceMs: 100,
	}

	if len(cfg.ExtraDirs) != 2 {
		t.Errorf("ExtraDirs length = %d, want 2", len(cfg.ExtraDirs))
	}
	if !cfg.Watch {
		t.Error("Watch should be true")
	}
	if cfg.WatchDebounceMs != 100 {
		t.Errorf("WatchDebounceMs = %d, want 100", cfg.WatchDebounceMs)
	}
}

func TestInstallSpec_Struct(t *testing.T) {
	spec := &InstallSpec{
		ID:      "homebrew-git",
		Kind:    "brew",
		Formula: "git",
		Package: "",
		Module:  "",
		URL:     "",
		Bins:    []string{"git"},
		Label:   "Install with Homebrew",
		OS:      []string{"darwin"},
	}

	if spec.ID != "homebrew-git" {
		t.Errorf("ID = %q", spec.ID)
	}
	if spec.Kind != "brew" {
		t.Errorf("Kind = %q", spec.Kind)
	}
	if spec.Formula != "git" {
		t.Errorf("Formula = %q", spec.Formula)
	}
}

func TestSkillsConfig_Struct(t *testing.T) {
	enabled := true
	cfg := &SkillsConfig{
		Sources: []SourceConfig{
			{Type: SourceGit, URL: "https://github.com/user/skills"},
		},
		Load: &LoadConfig{
			Watch: true,
		},
		Entries: map[string]*SkillConfig{
			"browser": {Enabled: &enabled},
		},
	}

	if len(cfg.Sources) != 1 {
		t.Errorf("Sources length = %d, want 1", len(cfg.Sources))
	}
	if cfg.Load == nil || !cfg.Load.Watch {
		t.Error("Load.Watch should be true")
	}
	if cfg.Entries["browser"] == nil {
		t.Error("Entries[browser] should not be nil")
	}
}
