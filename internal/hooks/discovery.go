package hooks

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// HookFilename is the expected filename for hook definitions.
	HookFilename = "HOOK.md"

	// FrontmatterDelimiter marks the beginning and end of YAML frontmatter.
	FrontmatterDelimiter = "---"
)

// HookConfig represents hook metadata from HOOK.md frontmatter.
type HookConfig struct {
	// Name is the unique identifier for this hook.
	Name string `json:"name" yaml:"name"`

	// Description explains what the hook does.
	Description string `json:"description" yaml:"description"`

	// Events lists the event types this hook listens for.
	// Format: "type" or "type:action" (e.g., "gateway:startup", "command:new")
	Events []string `json:"events" yaml:"events"`

	// Requires defines eligibility requirements.
	Requires *HookRequirements `json:"requires,omitempty" yaml:"requires"`

	// Enabled controls whether the hook is active (default: true).
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled"`

	// Priority determines call order (lower = earlier, default: PriorityNormal).
	Priority Priority `json:"priority,omitempty" yaml:"priority"`

	// Always skips eligibility checks if true.
	Always bool `json:"always,omitempty" yaml:"always"`
}

// HookRequirements defines eligibility checks for a hook.
type HookRequirements struct {
	// Bins requires all listed binaries to exist on PATH.
	Bins []string `json:"bins,omitempty" yaml:"bins"`

	// AnyBins requires at least one of the listed binaries to exist.
	AnyBins []string `json:"anyBins,omitempty" yaml:"anyBins"`

	// Env requires all listed environment variables to be set.
	Env []string `json:"env,omitempty" yaml:"env"`

	// Config requires all listed config paths to be truthy.
	Config []string `json:"config,omitempty" yaml:"config"`

	// OS restricts the hook to specific platforms (darwin, linux, windows).
	OS []string `json:"os,omitempty" yaml:"os"`
}

// HookEntry represents a discovered hook with its metadata and content.
type HookEntry struct {
	// Config contains the parsed frontmatter.
	Config HookConfig

	// Content is the markdown body (lazy loaded).
	Content string

	// Path is the directory path where the hook was discovered.
	Path string

	// Source indicates where the hook was discovered from.
	Source SourceType

	// SourcePriority is used for conflict resolution (higher wins).
	SourcePriority int
}

// SourceType indicates where a hook was discovered from.
type SourceType string

const (
	SourceBundled   SourceType = "bundled"   // Shipped with nexus binary
	SourceLocal     SourceType = "local"     // ~/.nexus/hooks/
	SourceWorkspace SourceType = "workspace" // <workspace>/hooks/
	SourceExtra     SourceType = "extra"     // hooks.load.extraDirs
)

// DiscoverySource discovers hooks from a specific source.
type DiscoverySource interface {
	// Type returns the source type identifier.
	Type() SourceType

	// Priority returns the source priority (higher wins in conflicts).
	Priority() int

	// Discover scans for hooks and returns found entries.
	Discover(ctx context.Context) ([]*HookEntry, error)
}

// WatchableSource exposes paths for file watching.
type WatchableSource interface {
	WatchPaths() []string
}

// LocalSource discovers hooks from a local directory.
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
		logger:     slog.Default().With("component", "hooks", "source", string(sourceType)),
	}
}

func (s *LocalSource) Type() SourceType {
	return s.sourceType
}

func (s *LocalSource) Priority() int {
	return s.priority
}

func (s *LocalSource) Discover(ctx context.Context) ([]*HookEntry, error) {
	// Check if directory exists
	info, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		s.logger.Debug("hooks directory does not exist", "path", s.path)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", s.path)
	}

	// List subdirectories (each is a potential hook)
	entries, err := os.ReadDir(s.path)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var hooks []*HookEntry
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return hooks, ctx.Err()
		default:
		}

		if !entry.IsDir() {
			continue
		}

		hookPath := filepath.Join(s.path, entry.Name())
		hookFile := filepath.Join(hookPath, HookFilename)

		// Check if HOOK.md exists
		if _, err := os.Stat(hookFile); os.IsNotExist(err) {
			continue
		}

		// Parse hook file
		hook, err := ParseHookFile(hookFile)
		if err != nil {
			s.logger.Warn("failed to parse hook",
				"path", hookPath,
				"error", err)
			continue
		}

		// Set source metadata
		hook.Source = s.sourceType
		hook.SourcePriority = s.priority

		// Validate
		if err := ValidateHook(hook); err != nil {
			s.logger.Warn("invalid hook",
				"path", hookPath,
				"error", err)
			continue
		}

		hooks = append(hooks, hook)
		s.logger.Debug("discovered hook",
			"name", hook.Config.Name,
			"path", hookPath,
			"events", hook.Config.Events)
	}

	s.logger.Info("discovered hooks",
		"count", len(hooks),
		"path", s.path)

	return hooks, nil
}

// WatchPaths returns the directory to watch for hook changes.
func (s *LocalSource) WatchPaths() []string {
	return []string{s.path}
}

// ParseHookFile parses a HOOK.md file and returns a HookEntry.
func ParseHookFile(path string) (*HookEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return ParseHook(data, filepath.Dir(path))
}

// ParseHook parses HOOK.md content and returns a HookEntry.
func ParseHook(data []byte, hookPath string) (*HookEntry, error) {
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("split frontmatter: %w", err)
	}

	var config HookConfig
	if err := yaml.Unmarshal(frontmatter, &config); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	entry := &HookEntry{
		Config:  config,
		Content: strings.TrimSpace(string(body)),
		Path:    hookPath,
	}

	return entry, nil
}

// splitFrontmatter separates YAML frontmatter from markdown body.
func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	// Find opening delimiter
	if !scanner.Scan() {
		return nil, nil, fmt.Errorf("empty file")
	}
	firstLine := strings.TrimSpace(scanner.Text())
	if firstLine != FrontmatterDelimiter {
		return nil, nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	// Read frontmatter until closing delimiter
	var frontmatterLines []string
	foundClosing := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == FrontmatterDelimiter {
			foundClosing = true
			break
		}
		frontmatterLines = append(frontmatterLines, line)
	}

	if !foundClosing {
		return nil, nil, fmt.Errorf("missing closing frontmatter delimiter")
	}

	// Read remaining content as body
	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scanner error: %w", err)
	}

	frontmatter := []byte(strings.Join(frontmatterLines, "\n"))
	body := []byte(strings.Join(bodyLines, "\n"))

	return frontmatter, body, nil
}

// ValidateHook checks if a hook entry is valid.
func ValidateHook(entry *HookEntry) error {
	if entry.Config.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate name format: lowercase, hyphens, no spaces
	for _, r := range entry.Config.Name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("name must be lowercase alphanumeric with hyphens: got %q", entry.Config.Name)
		}
	}

	if len(entry.Config.Events) == 0 {
		return fmt.Errorf("at least one event is required")
	}

	return nil
}

// EligibilityResult contains the result of an eligibility check.
type EligibilityResult struct {
	Eligible bool
	Reason   string
}

// GatingContext provides context for hook eligibility checks.
type GatingContext struct {
	// OS is the current operating system.
	OS string

	// PathBins caches binary lookups.
	PathBins map[string]bool

	// EnvVars caches environment variable checks.
	EnvVars map[string]bool

	// ConfigValues for config path checks.
	ConfigValues map[string]any
}

// NewGatingContext creates a GatingContext with the current environment.
func NewGatingContext(configValues map[string]any) *GatingContext {
	return &GatingContext{
		OS:           runtime.GOOS,
		PathBins:     make(map[string]bool),
		EnvVars:      make(map[string]bool),
		ConfigValues: configValues,
	}
}

// CheckBinary checks if a binary exists on PATH.
func (c *GatingContext) CheckBinary(name string) bool {
	if result, ok := c.PathBins[name]; ok {
		return result
	}

	_, err := exec.LookPath(name)
	result := err == nil
	c.PathBins[name] = result
	return result
}

// CheckEnv checks if an environment variable is set.
func (c *GatingContext) CheckEnv(name string) bool {
	if result, ok := c.EnvVars[name]; ok {
		return result
	}

	_, exists := os.LookupEnv(name)
	c.EnvVars[name] = exists
	return exists
}

// CheckConfig checks if a config path is truthy.
func (c *GatingContext) CheckConfig(path string) bool {
	if c.ConfigValues == nil {
		return false
	}

	parts := strings.Split(path, ".")
	var current any = c.ConfigValues

	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return false
		}
	}

	return isTruthy(current)
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}

	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false" && val != "0"
	case int, int8, int16, int32, int64:
		return val != 0
	case uint, uint8, uint16, uint32, uint64:
		return val != 0
	case float32, float64:
		return val != 0
	default:
		return true
	}
}

// CheckEligibility checks if a hook is eligible to be loaded.
func (entry *HookEntry) CheckEligibility(ctx *GatingContext) EligibilityResult {
	config := entry.Config

	// Check explicit disable
	if config.Enabled != nil && !*config.Enabled {
		return EligibilityResult{false, "disabled in config"}
	}

	// Always flag skips all checks
	if config.Always {
		return EligibilityResult{true, "always enabled"}
	}

	reqs := config.Requires
	if reqs == nil {
		return EligibilityResult{true, ""}
	}

	// OS check
	if len(reqs.OS) > 0 {
		found := false
		for _, os := range reqs.OS {
			if os == ctx.OS {
				found = true
				break
			}
		}
		if !found {
			return EligibilityResult{
				false,
				fmt.Sprintf("requires OS %v, have %s", reqs.OS, ctx.OS),
			}
		}
	}

	// All required binaries
	for _, bin := range reqs.Bins {
		if !ctx.CheckBinary(bin) {
			return EligibilityResult{
				false,
				fmt.Sprintf("missing required binary: %s", bin),
			}
		}
	}

	// Any-of binaries
	if len(reqs.AnyBins) > 0 {
		found := false
		for _, bin := range reqs.AnyBins {
			if ctx.CheckBinary(bin) {
				found = true
				break
			}
		}
		if !found {
			return EligibilityResult{
				false,
				fmt.Sprintf("requires one of: %v", reqs.AnyBins),
			}
		}
	}

	// Environment variables
	for _, env := range reqs.Env {
		if !ctx.CheckEnv(env) {
			return EligibilityResult{
				false,
				fmt.Sprintf("missing environment variable: %s", env),
			}
		}
	}

	// Config paths
	for _, path := range reqs.Config {
		if !ctx.CheckConfig(path) {
			return EligibilityResult{
				false,
				fmt.Sprintf("config not truthy: %s", path),
			}
		}
	}

	return EligibilityResult{true, ""}
}

// FilterEligible filters hooks to only those that are eligible.
func FilterEligible(hooks []*HookEntry, ctx *GatingContext) []*HookEntry {
	var eligible []*HookEntry
	for _, hook := range hooks {
		result := hook.CheckEligibility(ctx)
		if result.Eligible {
			eligible = append(eligible, hook)
		}
	}
	return eligible
}

// DiscoverAll discovers hooks from multiple sources with precedence.
func DiscoverAll(ctx context.Context, sources []DiscoverySource) ([]*HookEntry, error) {
	hookMap := make(map[string]*HookEntry)

	for _, source := range sources {
		hooks, err := source.Discover(ctx)
		if err != nil {
			slog.Warn("hook discovery failed",
				"source", source.Type(),
				"error", err)
			continue
		}

		for _, hook := range hooks {
			existing, ok := hookMap[hook.Config.Name]
			if !ok {
				hookMap[hook.Config.Name] = hook
				continue
			}

			// Higher priority wins
			if hook.SourcePriority > existing.SourcePriority {
				slog.Debug("hook override",
					"name", hook.Config.Name,
					"oldSource", existing.Source,
					"newSource", hook.Source)
				hookMap[hook.Config.Name] = hook
			}
		}
	}

	result := make([]*HookEntry, 0, len(hookMap))
	for _, hook := range hookMap {
		result = append(result, hook)
	}

	return result, nil
}

// DefaultSourcePriorities defines the default priority order.
const (
	PriorityExtra     = 10 // hooks.load.extraDirs
	PriorityBundled   = 20 // Shipped with binary
	PriorityLocal     = 30 // ~/.nexus/hooks/
	PriorityWorkspace = 40 // <workspace>/hooks/
)

// BuildDefaultSources creates the default discovery sources.
func BuildDefaultSources(workspacePath, localPath, bundledPath string, extraDirs []string) []DiscoverySource {
	var sources []DiscoverySource

	// Extra directories (lowest priority)
	for _, dir := range extraDirs {
		sources = append(sources, NewLocalSource(dir, SourceExtra, PriorityExtra))
	}

	// Bundled hooks
	if bundledPath != "" {
		sources = append(sources, NewLocalSource(bundledPath, SourceBundled, PriorityBundled))
	}

	// Local hooks (~/.nexus/hooks/)
	if localPath != "" {
		sources = append(sources, NewLocalSource(localPath, SourceLocal, PriorityLocal))
	}

	// Workspace hooks (highest priority)
	if workspacePath != "" {
		wsHooks := filepath.Join(workspacePath, "hooks")
		sources = append(sources, NewLocalSource(wsHooks, SourceWorkspace, PriorityWorkspace))
	}

	return sources
}

// DefaultLocalPath returns the default path for local hooks.
func DefaultLocalPath() string {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		home = "."
	}
	return filepath.Join(home, ".nexus", "hooks")
}
