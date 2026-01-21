package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/workspace"
)

// RepairWorkspace ensures workspace bootstrap files exist.
func RepairWorkspace(cfg *config.Config) (workspace.BootstrapResult, error) {
	if cfg == nil || !cfg.Workspace.Enabled {
		return workspace.BootstrapResult{}, nil
	}
	files := workspace.BootstrapFilesForConfig(cfg)
	return workspace.EnsureWorkspaceFiles(cfg.Workspace.Path, files, false)
}

// RepairHeartbeat ensures the heartbeat file exists when enabled.
func RepairHeartbeat(cfg *config.Config, configPath string) (string, bool, error) {
	if cfg == nil || !cfg.Session.Heartbeat.Enabled {
		return "", false, nil
	}
	path := resolveConfigPath(configPath, cfg.Workspace.Path, cfg.Session.Heartbeat.File)
	if strings.TrimSpace(path) == "" {
		return "", false, fmt.Errorf("heartbeat file path is empty")
	}
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}

	content := defaultHeartbeatContent()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", false, err
	}

	return path, true, nil
}

func resolveConfigPath(configPath, workspacePath, filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		return ""
	}
	if filepath.IsAbs(name) {
		return name
	}
	base := strings.TrimSpace(workspacePath)
	if base != "" {
		return filepath.Join(base, name)
	}
	if configPath != "" {
		return filepath.Join(filepath.Dir(configPath), name)
	}
	return name
}

func defaultHeartbeatContent() string {
	files := workspace.DefaultBootstrapFiles()
	for _, file := range files {
		if file.Name == "HEARTBEAT.md" {
			return file.Content
		}
	}
	return "# HEARTBEAT.md\n\n- Only report items that are new or changed.\n- If nothing needs attention, reply HEARTBEAT_OK.\n"
}
