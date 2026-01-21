// Package skills provides a skill system for extending agent capabilities
// with specialized knowledge, workflows, and tools.
package skills

import (
	"time"
)

// SkillEntry represents a discovered skill with its metadata and content.
type SkillEntry struct {
	// Name is the unique skill identifier (lowercase, hyphens allowed).
	Name string `json:"name" yaml:"name"`

	// Description explains what the skill does and when to use it.
	Description string `json:"description" yaml:"description"`

	// Homepage is an optional URL to skill documentation.
	Homepage string `json:"homepage,omitempty" yaml:"homepage"`

	// Metadata contains gating, install, and UI hints.
	Metadata *SkillMetadata `json:"metadata,omitempty" yaml:"metadata"`

	// Content is the markdown body (lazy loaded).
	Content string `json:"-"`

	// Path is the directory path where the skill was discovered.
	Path string `json:"path"`

	// Source indicates where the skill was discovered from.
	Source SourceType `json:"source"`

	// SourcePriority is used for conflict resolution (higher wins).
	SourcePriority int `json:"-"`
}

// SourceType indicates where a skill was discovered from.
type SourceType string

const (
	SourceBundled   SourceType = "bundled"   // Shipped with nexus binary
	SourceLocal     SourceType = "local"     // ~/.nexus/skills/
	SourceWorkspace SourceType = "workspace" // <workspace>/skills/
	SourceExtra     SourceType = "extra"     // skills.load.extraDirs
	SourceGit       SourceType = "git"       // Git repository
	SourceRegistry  SourceType = "registry"  // HTTP registry
)

// SkillMetadata contains gating rules and installation hints.
type SkillMetadata struct {
	// Emoji is displayed in UIs next to the skill name.
	Emoji string `json:"emoji,omitempty" yaml:"emoji"`

	// Always skips all gating checks if true.
	Always bool `json:"always,omitempty" yaml:"always"`

	// OS restricts the skill to specific platforms (darwin, linux, windows).
	OS []string `json:"os,omitempty" yaml:"os"`

	// Requires defines gating requirements.
	Requires *SkillRequires `json:"requires,omitempty" yaml:"requires"`

	// PrimaryEnv is the main API key environment variable for this skill.
	PrimaryEnv string `json:"primaryEnv,omitempty" yaml:"primaryEnv"`

	// SkillKey overrides the config key (defaults to skill name).
	SkillKey string `json:"skillKey,omitempty" yaml:"skillKey"`

	// Install provides installation instructions for different package managers.
	Install []InstallSpec `json:"install,omitempty" yaml:"install"`
}

// SkillRequires defines gating requirements for a skill.
type SkillRequires struct {
	// Bins requires all listed binaries to exist on PATH.
	Bins []string `json:"bins,omitempty" yaml:"bins"`

	// AnyBins requires at least one of the listed binaries to exist.
	AnyBins []string `json:"anyBins,omitempty" yaml:"anyBins"`

	// Env requires all listed environment variables to be set (or in config).
	Env []string `json:"env,omitempty" yaml:"env"`

	// Config requires all listed config paths to be truthy.
	Config []string `json:"config,omitempty" yaml:"config"`
}

// InstallSpec describes how to install a skill dependency.
type InstallSpec struct {
	// ID is a unique identifier for this install option.
	ID string `json:"id" yaml:"id"`

	// Kind is the installer type: brew, apt, npm, go, download.
	Kind string `json:"kind" yaml:"kind"`

	// Formula is the Homebrew formula name.
	Formula string `json:"formula,omitempty" yaml:"formula"`

	// Package is the npm/apt package name.
	Package string `json:"package,omitempty" yaml:"package"`

	// Module is the Go module path.
	Module string `json:"module,omitempty" yaml:"module"`

	// URL is the download URL for download kind.
	URL string `json:"url,omitempty" yaml:"url"`

	// Bins lists the binaries provided by this installer.
	Bins []string `json:"bins,omitempty" yaml:"bins"`

	// Label is a human-readable description.
	Label string `json:"label,omitempty" yaml:"label"`

	// OS restricts this installer to specific platforms.
	OS []string `json:"os,omitempty" yaml:"os"`
}

// SkillConfig provides per-skill configuration overrides.
type SkillConfig struct {
	// Enabled controls whether the skill is active.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled"`

	// APIKey is a convenience for skills with PrimaryEnv.
	APIKey string `json:"apiKey,omitempty" yaml:"apiKey"`

	// Env provides environment variable overrides.
	Env map[string]string `json:"env,omitempty" yaml:"env"`

	// Config provides custom skill configuration.
	Config map[string]any `json:"config,omitempty" yaml:"config"`
}

// SkillSnapshot is a lightweight representation for session storage.
type SkillSnapshot struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

// SourceConfig configures a skill discovery source.
type SourceConfig struct {
	// Type is the source type: local, git, registry.
	Type SourceType `json:"type" yaml:"type"`

	// Path is the directory path for local sources.
	Path string `json:"path,omitempty" yaml:"path"`

	// URL is the repository/registry URL for git/registry sources.
	URL string `json:"url,omitempty" yaml:"url"`

	// Branch is the git branch to use.
	Branch string `json:"branch,omitempty" yaml:"branch"`

	// SubPath is a subdirectory within a git repository.
	SubPath string `json:"subPath,omitempty" yaml:"subPath"`

	// Refresh is the auto-pull interval for git sources.
	Refresh time.Duration `json:"refresh,omitempty" yaml:"refresh"`

	// Auth is the authentication token for registry sources.
	Auth string `json:"auth,omitempty" yaml:"auth"`
}

// LoadConfig configures skill loading behavior.
type LoadConfig struct {
	// ExtraDirs are additional directories to scan for skills.
	ExtraDirs []string `json:"extraDirs,omitempty" yaml:"extraDirs"`

	// Watch enables file watching for skill changes.
	Watch bool `json:"watch,omitempty" yaml:"watch"`

	// WatchDebounceMs is the debounce delay for the watcher.
	WatchDebounceMs int `json:"watchDebounceMs,omitempty" yaml:"watchDebounceMs"`
}

// SkillsConfig is the top-level skills configuration.
type SkillsConfig struct {
	// Sources are additional discovery sources beyond defaults.
	Sources []SourceConfig `json:"sources,omitempty" yaml:"sources"`

	// Load configures loading behavior.
	Load *LoadConfig `json:"load,omitempty" yaml:"load"`

	// Entries provides per-skill configuration.
	Entries map[string]*SkillConfig `json:"entries,omitempty" yaml:"entries"`
}

// ConfigKey returns the configuration key for this skill.
func (s *SkillEntry) ConfigKey() string {
	if s.Metadata != nil && s.Metadata.SkillKey != "" {
		return s.Metadata.SkillKey
	}
	return s.Name
}

// IsEnabled checks if the skill is enabled based on config overrides.
func (s *SkillEntry) IsEnabled(overrides map[string]*SkillConfig) bool {
	cfg, ok := overrides[s.ConfigKey()]
	if !ok || cfg.Enabled == nil {
		return true // Enabled by default
	}
	return *cfg.Enabled
}

// ToSnapshot creates a lightweight snapshot for session storage.
func (s *SkillEntry) ToSnapshot() *SkillSnapshot {
	return &SkillSnapshot{
		Name:        s.Name,
		Description: s.Description,
		Path:        s.Path,
	}
}
