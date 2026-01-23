package workspace

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// WorkspaceContext holds all loaded workspace data for runtime use.
type WorkspaceContext struct {
	// Raw file contents
	AgentsContent   string
	SoulContent     string
	UserContent     string
	IdentityContent string
	ToolsContent    string
	MemoryContent   string

	// Parsed data
	Identity *Identity
	User     *UserProfile
}

// Identity holds parsed agent identity from IDENTITY.md.
type Identity struct {
	Name     string
	Creature string
	Vibe     string
	Emoji    string
}

// UserProfile holds parsed user profile from USER.md.
type UserProfile struct {
	Name             string
	PreferredAddress string
	Pronouns         string
	Timezone         string
	Notes            string
}

// LoaderConfig configures the workspace loader.
type LoaderConfig struct {
	Root         string
	AgentsFile   string
	SoulFile     string
	UserFile     string
	IdentityFile string
	ToolsFile    string
	MemoryFile   string
}

// LoaderConfigFromConfig creates a LoaderConfig from the app config.
func LoaderConfigFromConfig(cfg *config.Config) LoaderConfig {
	lc := LoaderConfig{
		AgentsFile:   "AGENTS.md",
		SoulFile:     "SOUL.md",
		UserFile:     "USER.md",
		IdentityFile: "IDENTITY.md",
		ToolsFile:    "TOOLS.md",
		MemoryFile:   "MEMORY.md",
	}
	if cfg == nil {
		return lc
	}
	if cfg.Workspace.Path != "" {
		lc.Root = cfg.Workspace.Path
	}
	if cfg.Workspace.AgentsFile != "" {
		lc.AgentsFile = cfg.Workspace.AgentsFile
	}
	if cfg.Workspace.SoulFile != "" {
		lc.SoulFile = cfg.Workspace.SoulFile
	}
	if cfg.Workspace.UserFile != "" {
		lc.UserFile = cfg.Workspace.UserFile
	}
	if cfg.Workspace.IdentityFile != "" {
		lc.IdentityFile = cfg.Workspace.IdentityFile
	}
	if cfg.Workspace.ToolsFile != "" {
		lc.ToolsFile = cfg.Workspace.ToolsFile
	}
	if cfg.Workspace.MemoryFile != "" {
		lc.MemoryFile = cfg.Workspace.MemoryFile
	}
	return lc
}

// LoadWorkspace loads all workspace files and returns a WorkspaceContext.
func LoadWorkspace(cfg LoaderConfig) (*WorkspaceContext, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}

	// Apply defaults for empty file names
	agentsFile := cfg.AgentsFile
	if agentsFile == "" {
		agentsFile = "AGENTS.md"
	}
	soulFile := cfg.SoulFile
	if soulFile == "" {
		soulFile = "SOUL.md"
	}
	userFile := cfg.UserFile
	if userFile == "" {
		userFile = "USER.md"
	}
	identityFile := cfg.IdentityFile
	if identityFile == "" {
		identityFile = "IDENTITY.md"
	}
	toolsFile := cfg.ToolsFile
	if toolsFile == "" {
		toolsFile = "TOOLS.md"
	}
	memoryFile := cfg.MemoryFile
	if memoryFile == "" {
		memoryFile = "MEMORY.md"
	}

	ctx := &WorkspaceContext{}
	loadOptional := func(name string) (string, error) {
		return readOptionalFile(filepath.Join(root, name))
	}

	// Load raw contents (ignore errors for missing files)
	var err error
	if ctx.AgentsContent, err = loadOptional(agentsFile); err != nil {
		return nil, err
	}
	if ctx.SoulContent, err = loadOptional(soulFile); err != nil {
		return nil, err
	}
	if ctx.UserContent, err = loadOptional(userFile); err != nil {
		return nil, err
	}
	if ctx.IdentityContent, err = loadOptional(identityFile); err != nil {
		return nil, err
	}
	if ctx.ToolsContent, err = loadOptional(toolsFile); err != nil {
		return nil, err
	}
	if ctx.MemoryContent, err = loadOptional(memoryFile); err != nil {
		return nil, err
	}

	// Parse structured data
	if ctx.IdentityContent != "" {
		ctx.Identity = parseIdentity(ctx.IdentityContent)
	}
	if ctx.UserContent != "" {
		ctx.User = parseUserProfile(ctx.UserContent)
	}

	return ctx, nil
}

// LoadSoul loads just the SOUL.md file content.
func LoadSoul(root, filename string) (string, error) {
	if filename == "" {
		filename = "SOUL.md"
	}
	return readFile(filepath.Join(root, filename))
}

// LoadUser loads and parses the USER.md file.
func LoadUser(root, filename string) (*UserProfile, error) {
	if filename == "" {
		filename = "USER.md"
	}
	content, err := readFile(filepath.Join(root, filename))
	if err != nil {
		return nil, err
	}
	return parseUserProfile(content), nil
}

// LoadIdentity loads and parses the IDENTITY.md file.
func LoadIdentity(root, filename string) (*Identity, error) {
	if filename == "" {
		filename = "IDENTITY.md"
	}
	content, err := readFile(filepath.Join(root, filename))
	if err != nil {
		return nil, err
	}
	return parseIdentity(content), nil
}

// LoadMemory loads the MEMORY.md file content.
func LoadMemory(root, filename string) (string, error) {
	if filename == "" {
		filename = "MEMORY.md"
	}
	return readFile(filepath.Join(root, filename))
}

// SystemPromptContext generates context to inject into system prompts.
func (w *WorkspaceContext) SystemPromptContext() string {
	var parts []string

	if w.SoulContent != "" {
		parts = append(parts, w.SoulContent)
	}

	if w.Identity != nil && w.Identity.Name != "" {
		parts = append(parts, fmt.Sprintf("Your name is %s.", w.Identity.Name))
		if w.Identity.Creature != "" {
			parts = append(parts, fmt.Sprintf("You are a %s.", w.Identity.Creature))
		}
		if w.Identity.Vibe != "" {
			parts = append(parts, fmt.Sprintf("Your vibe is %s.", w.Identity.Vibe))
		}
		if w.Identity.Emoji != "" {
			parts = append(parts, fmt.Sprintf("Your emoji is %s.", w.Identity.Emoji))
		}
	}

	if w.User != nil && w.User.Name != "" {
		addr := w.User.PreferredAddress
		if addr == "" {
			addr = w.User.Name
		}
		parts = append(parts, fmt.Sprintf("You are talking to %s (address them as %s).", w.User.Name, addr))
		if w.User.Timezone != "" {
			parts = append(parts, fmt.Sprintf("Their timezone is %s.", w.User.Timezone))
		}
	}

	return strings.Join(parts, "\n")
}

// Helper functions

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readOptionalFile(path string) (string, error) {
	content, err := readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return content, nil
}

// parseIdentity parses IDENTITY.md format:
// - Name: value
// - Creature: value
// etc.
func parseIdentity(content string) *Identity {
	id := &Identity{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if key, val := parseKeyValue(line); key != "" {
			switch strings.ToLower(key) {
			case "name":
				id.Name = val
			case "creature":
				id.Creature = val
			case "vibe":
				id.Vibe = val
			case "emoji":
				id.Emoji = val
			}
		}
	}
	return id
}

// parseUserProfile parses USER.md format:
// - Name: value
// - Preferred address: value
// etc.
func parseUserProfile(content string) *UserProfile {
	user := &UserProfile{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if key, val := parseKeyValue(line); key != "" {
			switch strings.ToLower(key) {
			case "name":
				user.Name = val
			case "preferred address":
				user.PreferredAddress = val
			case "pronouns", "pronouns (optional)":
				user.Pronouns = val
			case "timezone", "timezone (optional)":
				user.Timezone = val
			case "notes":
				user.Notes = val
			}
		}
	}
	return user
}

// parseKeyValue extracts key-value from lines like "- Key: Value" or "Key: Value"
func parseKeyValue(line string) (string, string) {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "-")
	line = strings.TrimSpace(line)

	idx := strings.Index(line, ":")
	if idx == -1 {
		return "", ""
	}

	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	return key, val
}
