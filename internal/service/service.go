package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	SystemdUnitName = "nexus.service"
	LaunchdLabel    = "com.haasonsaas.nexus"
)

// InstallResult captures the service file write and next steps.
type InstallResult struct {
	Path         string
	Instructions []string
}

// InstallUserService writes a user-level service file for the current OS.
func InstallUserService(configPath string, overwrite bool) (InstallResult, error) {
	execPath, err := os.Executable()
	if err != nil {
		execPath = "nexus"
	}
	configPath = normalizeConfigPath(configPath)

	switch runtime.GOOS {
	case "linux":
		return installSystemdUser(execPath, configPath, overwrite)
	case "darwin":
		return installLaunchdUser(execPath, configPath, overwrite)
	default:
		return InstallResult{}, fmt.Errorf("service install not supported on %s", runtime.GOOS)
	}
}

func normalizeConfigPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "nexus.yaml"
	}
	return path
}

func installSystemdUser(execPath, configPath string, overwrite bool) (InstallResult, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if strings.TrimSpace(base) == "" {
		home, _ := os.UserHomeDir()
		if strings.TrimSpace(home) == "" {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}

	path := filepath.Join(base, "systemd", "user", SystemdUnitName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return InstallResult{}, err
	}

	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return InstallResult{Path: path, Instructions: []string{"systemctl --user daemon-reload", "systemctl --user enable --now nexus"}}, nil
		} else if !os.IsNotExist(err) {
			return InstallResult{}, err
		}
	}

	content := GenerateSystemdUnit(execPath, configPath)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return InstallResult{}, err
	}

	return InstallResult{
		Path: path,
		Instructions: []string{
			"systemctl --user daemon-reload",
			"systemctl --user enable --now nexus",
		},
	}, nil
}

func installLaunchdUser(execPath, configPath string, overwrite bool) (InstallResult, error) {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		home = "."
	}
	path := filepath.Join(home, "Library", "LaunchAgents", LaunchdLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return InstallResult{}, err
	}

	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return InstallResult{Path: path, Instructions: []string{"launchctl unload " + path, "launchctl load -w " + path}}, nil
		} else if !os.IsNotExist(err) {
			return InstallResult{}, err
		}
	}

	content := GenerateLaunchdPlist(execPath, configPath)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return InstallResult{}, err
	}

	return InstallResult{
		Path: path,
		Instructions: []string{
			"launchctl unload " + path,
			"launchctl load -w " + path,
		},
	}, nil
}

// GenerateSystemdUnit returns a systemd unit file for Nexus.
func GenerateSystemdUnit(execPath, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Nexus Gateway
After=network.target

[Service]
ExecStart=%s serve --config %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, execPath, configPath)
}

// GenerateLaunchdPlist returns a launchd plist for Nexus.
func GenerateLaunchdPlist(execPath, configPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
      <string>serve</string>
      <string>--config</string>
      <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
  </dict>
</plist>
`, LaunchdLabel, execPath, configPath)
}
