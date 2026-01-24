package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	edgeLaunchdLabel    = "com.haasonsaas.nexus-edge"
	edgeSystemdUnitName = "nexus-edge.service"
)

func buildInitCmd(flagConfig *Config, configPath *string, pairToken *string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write an edge config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := resolveConfigPath(*configPath)

			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("config already exists: %s", path)
				}
			}

			config := DefaultConfig()
			merged, err := applyFlagOverrides(cmd, config, *flagConfig, *pairToken)
			if err != nil {
				return err
			}
			config = normalizeConfig(merged)

			if err := writeConfig(path, config); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Wrote config to %s\n", path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config file")
	return cmd
}

func buildInstallCmd(flagConfig *Config, configPath *string, pairToken *string) *cobra.Command {
	var force bool
	var start bool
	var initConfig bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the edge daemon as a user service",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := resolveConfigPath(*configPath)

			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) && initConfig {
					config := DefaultConfig()
					merged, err := applyFlagOverrides(cmd, config, *flagConfig, *pairToken)
					if err != nil {
						return err
					}
					config = normalizeConfig(merged)
					if err := writeConfig(path, config); err != nil {
						return err
					}
				} else if os.IsNotExist(err) {
					return fmt.Errorf("config not found: %s (run `nexus-edge init` or pass --init-config)", path)
				} else {
					return err
				}
			}

			execPath, err := os.Executable()
			if err != nil || strings.TrimSpace(execPath) == "" {
				execPath = "nexus-edge"
			}

			return installEdgeService(cmd.Context(), execPath, path, force, start)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing service file")
	cmd.Flags().BoolVar(&start, "start", true, "Start/enable the service after install")
	cmd.Flags().BoolVar(&initConfig, "init-config", false, "Create a config file if missing")
	return cmd
}

func buildUninstallCmd() *cobra.Command {
	var keepConfig bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the edge daemon user service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallEdgeService(cmd.Context(), keepConfig)
		},
	}

	cmd.Flags().BoolVar(&keepConfig, "keep-config", true, "Keep the edge config file on disk")
	return cmd
}

func installEdgeService(ctx context.Context, execPath, configPath string, overwrite bool, start bool) error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(ctx, execPath, configPath, overwrite, start)
	case "linux":
		return installSystemd(ctx, execPath, configPath, overwrite, start)
	default:
		return fmt.Errorf("edge service install not supported on %s", runtime.GOOS)
	}
}

func uninstallEdgeService(ctx context.Context, keepConfig bool) error {
	switch runtime.GOOS {
	case "darwin":
		if err := uninstallLaunchd(ctx); err != nil {
			return err
		}
	case "linux":
		if err := uninstallSystemd(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("edge service uninstall not supported on %s", runtime.GOOS)
	}

	if !keepConfig {
		if path, ok := resolveConfigPath(""); ok {
			_ = os.Remove(path) //nolint:errcheck // best-effort cleanup
		}
	}
	return nil
}

func installLaunchd(ctx context.Context, execPath, configPath string, overwrite bool, start bool) error {
	plistPath := launchdPlistPath()
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}

	if overwrite || !fileExists(plistPath) {
		content := generateLaunchdPlist(execPath, configPath)
		if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
			return err
		}
	}

	if start {
		_ = runCommand(ctx, "launchctl", "unload", plistPath) // best-effort
		if err := runCommand(ctx, "launchctl", "load", "-w", plistPath); err != nil {
			return err
		}
	}

	return nil
}

func uninstallLaunchd(ctx context.Context) error {
	plistPath := launchdPlistPath()
	if fileExists(plistPath) {
		_ = runCommand(ctx, "launchctl", "unload", plistPath) // best-effort
		if err := os.Remove(plistPath); err != nil {
			return err
		}
	}
	return nil
}

func installSystemd(ctx context.Context, execPath, configPath string, overwrite bool, start bool) error {
	unitPath := systemdUnitPath()
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return err
	}

	if overwrite || !fileExists(unitPath) {
		content := generateSystemdUnit(execPath, configPath)
		if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
			return err
		}
	}

	if start {
		if err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if err := runCommand(ctx, "systemctl", "--user", "enable", "--now", "nexus-edge"); err != nil {
			return err
		}
	}
	return nil
}

func uninstallSystemd(ctx context.Context) error {
	unitPath := systemdUnitPath()
	if fileExists(unitPath) {
		_ = runCommand(ctx, "systemctl", "--user", "disable", "--now", "nexus-edge") // best-effort
		if err := os.Remove(unitPath); err != nil {
			return err
		}
		_ = runCommand(ctx, "systemctl", "--user", "daemon-reload") // best-effort
	}
	return nil
}

func generateLaunchdPlist(execPath, configPath string) string {
	logDir := filepath.Join(userHomeDir(), "Library", "Logs")
	logPath := filepath.Join(logDir, "nexus-edge.log")
	errPath := filepath.Join(logDir, "nexus-edge.err.log")

	args := []string{execPath}
	if strings.TrimSpace(configPath) != "" {
		args = append(args, "--config", configPath)
	}

	argumentLines := make([]string, 0, len(args))
	for _, arg := range args {
		argumentLines = append(argumentLines, fmt.Sprintf("      <string>%s</string>", xmlEscape(arg)))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
%s
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
  </dict>
</plist>
`, edgeLaunchdLabel, strings.Join(argumentLines, "\n"), logPath, errPath)
}

func generateSystemdUnit(execPath, configPath string) string {
	args := execPath
	if strings.TrimSpace(configPath) != "" {
		args = fmt.Sprintf("%s --config %s", execPath, configPath)
	}
	return fmt.Sprintf(`[Unit]
Description=Nexus Edge Daemon
After=network.target

[Service]
ExecStart=%s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, args)
}

func launchdPlistPath() string {
	return filepath.Join(userHomeDir(), "Library", "LaunchAgents", edgeLaunchdLabel+".plist")
}

func systemdUnitPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if strings.TrimSpace(base) == "" {
		base = filepath.Join(userHomeDir(), ".config")
	}
	return filepath.Join(base, "systemd", "user", edgeSystemdUnitName)
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "."
	}
	return home
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}
