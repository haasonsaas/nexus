package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// GitSource discovers skills from a git repository.
type GitSource struct {
	URL             string
	Branch          string
	SubPath         string
	CacheDir        string
	RefreshInterval time.Duration

	priority   int
	logger     *slog.Logger
	lastPull   time.Time
	lastPullMu sync.Mutex
}

// NewGitSource creates a git repository discovery source.
func NewGitSource(url, branch, subPath, cacheDir string, refreshInterval time.Duration, priority int) *GitSource {
	if branch == "" {
		branch = "main"
	}
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(homeDir) == "" {
			homeDir = "."
		}
		cacheDir = filepath.Join(homeDir, ".nexus", "cache", "skills", "git")
	}
	return &GitSource{
		URL:             url,
		Branch:          branch,
		SubPath:         subPath,
		CacheDir:        cacheDir,
		RefreshInterval: refreshInterval,
		priority:        priority,
		logger:          slog.Default().With("component", "skills", "source", "git", "url", url),
	}
}

func (s *GitSource) Type() SourceType {
	return SourceGit
}

func (s *GitSource) Priority() int {
	return s.priority
}

// repoPath returns the local path where the repository is cached.
func (s *GitSource) repoPath() string {
	// Create a unique directory name based on URL hash
	hash := sha256.Sum256([]byte(s.URL))
	dirName := hex.EncodeToString(hash[:8])
	return filepath.Join(s.CacheDir, dirName)
}

// needsRefresh checks if the repository should be refreshed.
func (s *GitSource) needsRefresh() bool {
	if s.RefreshInterval <= 0 {
		return false
	}
	s.lastPullMu.Lock()
	defer s.lastPullMu.Unlock()
	return time.Since(s.lastPull) > s.RefreshInterval
}

// updateLastPull records the last successful pull time.
func (s *GitSource) updateLastPull() {
	s.lastPullMu.Lock()
	s.lastPull = time.Now()
	s.lastPullMu.Unlock()
}

func (s *GitSource) Discover(ctx context.Context) ([]*SkillEntry, error) {
	repoPath := s.repoPath()

	// Check if repo exists
	gitDir := filepath.Join(repoPath, ".git")
	repoExists := false
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		repoExists = true
	}

	if repoExists {
		// Pull if refresh interval has elapsed
		if s.needsRefresh() {
			if err := s.pull(ctx, repoPath); err != nil {
				s.logger.Warn("git pull failed, using cached version", "error", err)
			} else {
				s.updateLastPull()
			}
		}
	} else {
		// Clone the repository
		if err := s.clone(ctx, repoPath); err != nil {
			return nil, fmt.Errorf("git clone failed: %w", err)
		}
		s.updateLastPull()
	}

	// Scan for skills
	skillsPath := repoPath
	if s.SubPath != "" {
		skillsPath = filepath.Join(repoPath, s.SubPath)
	}

	return s.scanForSkills(ctx, skillsPath)
}

func (s *GitSource) clone(ctx context.Context, repoPath string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	args := []string{"clone", "--depth", "1", "--branch", s.Branch, s.URL, repoPath}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w: %s", err, string(output))
	}

	s.logger.Info("cloned git repository", "path", repoPath)
	return nil
}

func (s *GitSource) pull(ctx context.Context, repoPath string) error {
	// Fetch and reset to origin/branch for a clean pull
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "--depth", "1", "origin", s.Branch)
	fetchCmd.Dir = repoPath
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %w: %s", err, string(output))
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", "origin/"+s.Branch)
	resetCmd.Dir = repoPath

	if output, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset: %w: %s", err, string(output))
	}

	s.logger.Debug("pulled git repository", "path", repoPath)
	return nil
}

func (s *GitSource) scanForSkills(ctx context.Context, skillsPath string) ([]*SkillEntry, error) {
	info, err := os.Stat(skillsPath)
	if os.IsNotExist(err) {
		s.logger.Debug("skills directory does not exist in repo", "path", skillsPath)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat skills path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", skillsPath)
	}

	entries, err := os.ReadDir(skillsPath)
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

		skillPath := filepath.Join(skillsPath, entry.Name())
		skillFile := filepath.Join(skillPath, SkillFilename)

		if _, err := os.Stat(skillFile); os.IsNotExist(err) {
			continue
		}

		skill, err := ParseSkillFile(skillFile)
		if err != nil {
			s.logger.Warn("failed to parse skill", "path", skillPath, "error", err)
			continue
		}

		skill.Source = SourceGit
		skill.SourcePriority = s.priority

		if err := ValidateSkill(skill); err != nil {
			s.logger.Warn("invalid skill", "path", skillPath, "error", err)
			continue
		}

		skills = append(skills, skill)
		s.logger.Debug("discovered skill", "name", skill.Name, "path", skillPath)
	}

	s.logger.Info("discovered skills from git", "count", len(skills), "url", s.URL)
	return skills, nil
}

// WatchPaths returns the cached repository path for watching.
func (s *GitSource) WatchPaths() []string {
	repoPath := s.repoPath()
	if s.SubPath != "" {
		return []string{filepath.Join(repoPath, s.SubPath)}
	}
	return []string{repoPath}
}

// RegistrySource discovers skills from an HTTP registry.
type RegistrySource struct {
	URL  string
	Auth string

	priority int
	cacheDir string
	logger   *slog.Logger
}

// RegistrySkillInfo represents skill metadata from a registry API.
type RegistrySkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Checksum    string `json:"checksum,omitempty"`
}

// RegistryResponse represents the registry API response.
type RegistryResponse struct {
	Skills []RegistrySkillInfo `json:"skills"`
}

// NewRegistrySource creates an HTTP registry discovery source.
func NewRegistrySource(url, auth string, priority int) *RegistrySource {
	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		homeDir = "."
	}
	cacheDir := filepath.Join(homeDir, ".nexus", "cache", "skills", "registry")

	return &RegistrySource{
		URL:      strings.TrimSuffix(url, "/"),
		Auth:     auth,
		priority: priority,
		cacheDir: cacheDir,
		logger:   slog.Default().With("component", "skills", "source", "registry", "url", url),
	}
}

func (s *RegistrySource) Type() SourceType {
	return SourceRegistry
}

func (s *RegistrySource) Priority() int {
	return s.priority
}

func (s *RegistrySource) Discover(ctx context.Context) ([]*SkillEntry, error) {
	// Fetch skill metadata from registry
	skillInfos, err := s.fetchSkillList(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch skill list: %w", err)
	}

	// Download each skill
	var skills []*SkillEntry
	for _, info := range skillInfos {
		select {
		case <-ctx.Done():
			return skills, ctx.Err()
		default:
		}

		skill, err := s.downloadSkill(ctx, info)
		if err != nil {
			s.logger.Warn("failed to download skill", "name", info.Name, "error", err)
			continue
		}

		skills = append(skills, skill)
		s.logger.Debug("discovered skill", "name", skill.Name)
	}

	s.logger.Info("discovered skills from registry", "count", len(skills), "url", s.URL)
	return skills, nil
}

func (s *RegistrySource) fetchSkillList(ctx context.Context) ([]RegistrySkillInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/skills", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if s.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+s.Auth)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			return nil, fmt.Errorf("registry returned %d (failed reading body: %v)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var response RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return response.Skills, nil
}

func (s *RegistrySource) downloadSkill(ctx context.Context, info RegistrySkillInfo) (*SkillEntry, error) {
	// Create skill cache directory
	skillDir := filepath.Join(s.cacheDir, info.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	skillFile := filepath.Join(skillDir, SkillFilename)

	// Check if we need to download (based on checksum if available)
	needsDownload := true
	if info.Checksum != "" {
		if existingChecksum, err := s.fileChecksum(skillFile); err == nil {
			needsDownload = existingChecksum != info.Checksum
		}
	}

	if needsDownload {
		downloadURL := info.DownloadURL
		if downloadURL == "" {
			downloadURL = fmt.Sprintf("%s/skills/%s/SKILL.md", s.URL, info.Name)
		}

		if err := s.downloadFile(ctx, downloadURL, skillFile); err != nil {
			return nil, fmt.Errorf("download skill file: %w", err)
		}
	}

	// Parse the downloaded skill
	skill, err := ParseSkillFile(skillFile)
	if err != nil {
		return nil, fmt.Errorf("parse skill: %w", err)
	}

	skill.Source = SourceRegistry
	skill.SourcePriority = s.priority

	if err := ValidateSkill(skill); err != nil {
		return nil, fmt.Errorf("validate skill: %w", err)
	}

	return skill, nil
}

func (s *RegistrySource) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if s.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+s.Auth)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			return fmt.Errorf("download returned %d (failed reading body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("download returned %d: %s", resp.StatusCode, string(body))
	}

	// Write to temp file first
	tmpFile := destPath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("write file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, destPath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rename file: %w", err)
	}

	return nil
}

func (s *RegistrySource) fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// WatchPaths returns the cache directory for watching.
func (s *RegistrySource) WatchPaths() []string {
	return []string{s.cacheDir}
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
	PriorityExtra     = 10 // skills.load.extraDirs
	PriorityBundled   = 20 // Shipped with binary
	PriorityLocal     = 30 // ~/.nexus/skills/
	PriorityWorkspace = 40 // <workspace>/skills/
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
