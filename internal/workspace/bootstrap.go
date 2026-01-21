package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// BootstrapFile represents a file to seed in a workspace.
type BootstrapFile struct {
	Name    string
	Content string
}

// BootstrapResult captures the files created or skipped.
type BootstrapResult struct {
	Created []string
	Skipped []string
}

// DefaultBootstrapFiles returns the default bootstrap file set.
func DefaultBootstrapFiles() []BootstrapFile {
	return []BootstrapFile{
		{
			Name: "AGENTS.md",
			Content: "# AGENTS.md - Workspace Instructions\n\n" +
				"This workspace is the assistant's working directory.\n\n" +
				"## Safety\n" +
				"- Do not exfiltrate secrets or private data.\n" +
				"- Avoid destructive actions unless explicitly requested.\n\n" +
				"## Workflow\n" +
				"- Be concise in chat; put longer output in files.\n" +
				"- Ask clarifying questions when requirements are unclear.\n" +
				"- If you keep memory, append day notes in memory/YYYY-MM-DD.md.\n",
		},
		{
			Name: "SOUL.md",
			Content: "# SOUL.md - Persona & Boundaries\n\n" +
				"- Tone: concise, direct, and friendly.\n" +
				"- Ask clarifying questions when needed.\n" +
				"- Never send partial/streaming replies to external messaging surfaces.\n",
		},
		{
			Name: "USER.md",
			Content: "# USER.md - User Profile\n\n" +
				"- Name:\n" +
				"- Preferred address:\n" +
				"- Pronouns (optional):\n" +
				"- Timezone (optional):\n" +
				"- Notes:\n",
		},
		{
			Name: "IDENTITY.md",
			Content: "# IDENTITY.md - Agent Identity\n\n" +
				"- Name:\n" +
				"- Creature:\n" +
				"- Vibe:\n" +
				"- Emoji:\n",
		},
		{
			Name: "TOOLS.md",
			Content: "# TOOLS.md - User Tool Notes (editable)\n\n" +
				"Add notes about local tools, conventions, or shortcuts here.\n",
		},
		{
			Name: "HEARTBEAT.md",
			Content: "# HEARTBEAT.md\n\n" +
				"- Only report items that are new or changed.\n" +
				"- If nothing needs attention, reply HEARTBEAT_OK.\n",
		},
		{
			Name: "MEMORY.md",
			Content: "# MEMORY.md - Long-Term Memory\n\n" +
				"Capture durable facts, preferences, and decisions here.\n",
		},
	}
}

// BootstrapFilesForConfig maps workspace config file names to bootstrap content.
func BootstrapFilesForConfig(cfg *config.Config) []BootstrapFile {
	defaults := DefaultBootstrapFiles()
	if cfg == nil {
		return defaults
	}
	nameOverrides := map[string]string{}
	workspace := cfg.Workspace
	if workspace.AgentsFile != "" {
		nameOverrides["AGENTS.md"] = workspace.AgentsFile
	}
	if workspace.SoulFile != "" {
		nameOverrides["SOUL.md"] = workspace.SoulFile
	}
	if workspace.UserFile != "" {
		nameOverrides["USER.md"] = workspace.UserFile
	}
	if workspace.IdentityFile != "" {
		nameOverrides["IDENTITY.md"] = workspace.IdentityFile
	}
	if workspace.ToolsFile != "" {
		nameOverrides["TOOLS.md"] = workspace.ToolsFile
	}
	if workspace.MemoryFile != "" {
		nameOverrides["MEMORY.md"] = workspace.MemoryFile
	}
	files := make([]BootstrapFile, 0, len(defaults))
	for _, entry := range defaults {
		name := entry.Name
		if override, ok := nameOverrides[entry.Name]; ok {
			name = override
		}
		files = append(files, BootstrapFile{Name: name, Content: entry.Content})
	}
	return files
}

// EnsureWorkspaceFiles creates missing files in the workspace root.
func EnsureWorkspaceFiles(root string, files []BootstrapFile, overwrite bool) (BootstrapResult, error) {
	result := BootstrapResult{}
	base := strings.TrimSpace(root)
	if base == "" {
		base = "."
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return result, fmt.Errorf("create workspace dir: %w", err)
	}

	for _, file := range files {
		name := strings.TrimSpace(file.Name)
		if name == "" {
			continue
		}
		path := filepath.Join(base, name)
		if !overwrite {
			if _, err := os.Stat(path); err == nil {
				result.Skipped = append(result.Skipped, path)
				continue
			} else if !os.IsNotExist(err) {
				return result, fmt.Errorf("stat %s: %w", path, err)
			}
		}
		if err := os.WriteFile(path, []byte(file.Content), 0o644); err != nil {
			return result, fmt.Errorf("write %s: %w", path, err)
		}
		result.Created = append(result.Created, path)
	}

	return result, nil
}
