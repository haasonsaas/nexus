package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// LaunchdManager manages macOS LaunchAgent services.
type LaunchdManager struct{}

// Label returns "LaunchAgent".
func (m *LaunchdManager) Label() string {
	return "LaunchAgent"
}

// Install installs and starts a LaunchAgent.
func (m *LaunchdManager) Install(opts InstallOptions) (*InstallResult, error) {
	return InstallLaunchAgent(opts)
}

// Uninstall removes a LaunchAgent.
func (m *LaunchdManager) Uninstall(env map[string]string) error {
	return UninstallLaunchAgent(env)
}

// Stop stops a LaunchAgent.
func (m *LaunchdManager) Stop(env map[string]string) error {
	return StopLaunchAgent(env)
}

// Restart restarts a LaunchAgent.
func (m *LaunchdManager) Restart(env map[string]string) error {
	return RestartLaunchAgent(env)
}

// IsInstalled checks if a LaunchAgent is loaded.
func (m *LaunchdManager) IsInstalled(env map[string]string) (bool, error) {
	return IsLaunchAgentLoaded(env)
}

// Runtime returns the runtime status of a LaunchAgent.
func (m *LaunchdManager) Runtime(env map[string]string) (*ServiceRuntime, error) {
	return ReadLaunchAgentRuntime(env)
}

// resolveLaunchdLabel returns the launchd label from environment or default.
func resolveLaunchdLabel(env map[string]string) string {
	if label := strings.TrimSpace(env[EnvNexusLaunchdLabel]); label != "" {
		return label
	}
	profile := resolveProfile(env)
	if profile != "" {
		return "com.haasonsaas.nexus." + profile
	}
	return DefaultLaunchdLabel
}

// resolveLaunchdPlistPath returns the path to the plist file.
func resolveLaunchdPlistPath(env map[string]string) string {
	home := resolveHomeDir(env)
	if home == "" {
		home = "."
	}
	label := resolveLaunchdLabel(env)
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// resolveLogPaths returns paths for stdout and stderr logs.
func resolveLogPaths(env map[string]string) (logDir, stdoutPath, stderrPath string) {
	stateDir := resolveStateDir(env)
	if stateDir == "" {
		stateDir = "."
	}
	logDir = filepath.Join(stateDir, "logs")
	prefix := env[EnvNexusLogPrefix]
	if prefix == "" {
		prefix = "gateway"
	}
	stdoutPath = filepath.Join(logDir, prefix+".log")
	stderrPath = filepath.Join(logDir, prefix+".err.log")
	return
}

// resolveGUIDomain returns the launchd GUI domain for the current user.
func resolveGUIDomain() string {
	uid := os.Getuid()
	if uid < 0 {
		uid = 501 // default macOS UID
	}
	return fmt.Sprintf("gui/%d", uid)
}

// execLaunchctl runs launchctl with the given arguments.
func execLaunchctl(args []string) (stdout, stderr string, code int) {
	cmd := exec.Command("launchctl", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
		if stderr == "" {
			stderr = err.Error()
		}
	}
	return
}

// BuildLaunchAgentPlist builds a plist XML string for a LaunchAgent.
func BuildLaunchAgentPlist(opts struct {
	Label            string
	Comment          string
	ProgramArguments []string
	WorkingDirectory string
	StdoutPath       string
	StderrPath       string
	Environment      map[string]string
}) string {
	var buf bytes.Buffer

	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>`)
	buf.WriteString(plistEscape(opts.Label))
	buf.WriteString(`</string>
`)

	if opts.Comment != "" {
		buf.WriteString(`    <key>Comment</key>
    <string>`)
		buf.WriteString(plistEscape(opts.Comment))
		buf.WriteString(`</string>
`)
	}

	buf.WriteString(`    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ProgramArguments</key>
    <array>
`)
	for _, arg := range opts.ProgramArguments {
		buf.WriteString(`      <string>`)
		buf.WriteString(plistEscape(arg))
		buf.WriteString(`</string>
`)
	}
	buf.WriteString(`    </array>
`)

	if opts.WorkingDirectory != "" {
		buf.WriteString(`    <key>WorkingDirectory</key>
    <string>`)
		buf.WriteString(plistEscape(opts.WorkingDirectory))
		buf.WriteString(`</string>
`)
	}

	buf.WriteString(`    <key>StandardOutPath</key>
    <string>`)
	buf.WriteString(plistEscape(opts.StdoutPath))
	buf.WriteString(`</string>
    <key>StandardErrorPath</key>
    <string>`)
	buf.WriteString(plistEscape(opts.StderrPath))
	buf.WriteString(`</string>
`)

	if len(opts.Environment) > 0 {
		buf.WriteString(`    <key>EnvironmentVariables</key>
    <dict>
`)
		for k, v := range opts.Environment {
			buf.WriteString(`      <key>`)
			buf.WriteString(plistEscape(k))
			buf.WriteString(`</key>
      <string>`)
			buf.WriteString(plistEscape(v))
			buf.WriteString(`</string>
`)
		}
		buf.WriteString(`    </dict>
`)
	}

	buf.WriteString(`  </dict>
</plist>
`)
	return buf.String()
}

// plistEscape escapes special characters for plist XML.
func plistEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// InstallLaunchAgent installs and starts a LaunchAgent.
func InstallLaunchAgent(opts InstallOptions) (*InstallResult, error) {
	env := opts.Env
	if env == nil {
		env = make(map[string]string)
	}

	logDir, stdoutPath, stderrPath := resolveLogPaths(env)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	domain := resolveGUIDomain()
	label := resolveLaunchdLabel(env)
	plistPath := resolveLaunchdPlistPath(env)

	// Clean up legacy services
	for _, legacyLabel := range []string{LegacyLaunchdLabel} {
		legacyPath := filepath.Join(filepath.Dir(plistPath), legacyLabel+".plist")
		execLaunchctl([]string{"bootout", domain, legacyPath})
		execLaunchctl([]string{"unload", legacyPath})
		os.Remove(legacyPath)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	// Build and write plist
	description := opts.Description
	if description == "" {
		description = formatServiceDescription(env)
	}

	plist := BuildLaunchAgentPlist(struct {
		Label            string
		Comment          string
		ProgramArguments []string
		WorkingDirectory string
		StdoutPath       string
		StderrPath       string
		Environment      map[string]string
	}{
		Label:            label,
		Comment:          description,
		ProgramArguments: opts.ProgramArguments,
		WorkingDirectory: opts.WorkingDirectory,
		StdoutPath:       stdoutPath,
		StderrPath:       stderrPath,
		Environment:      opts.Environment,
	})

	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write plist: %w", err)
	}

	// Unload any existing service
	execLaunchctl([]string{"bootout", domain, plistPath})
	execLaunchctl([]string{"unload", plistPath})

	// Clear any persisted disabled state
	execLaunchctl([]string{"enable", fmt.Sprintf("%s/%s", domain, label)})

	// Bootstrap the service
	_, stderr, code := execLaunchctl([]string{"bootstrap", domain, plistPath})
	if code != 0 {
		return nil, fmt.Errorf("launchctl bootstrap failed: %s", strings.TrimSpace(stderr))
	}

	// Kickstart the service
	execLaunchctl([]string{"kickstart", "-k", fmt.Sprintf("%s/%s", domain, label)})

	return &InstallResult{Path: plistPath}, nil
}

// UninstallLaunchAgent stops and removes a LaunchAgent.
func UninstallLaunchAgent(env map[string]string) error {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	plistPath := resolveLaunchdPlistPath(env)

	// Stop and unload
	execLaunchctl([]string{"bootout", domain, plistPath})
	execLaunchctl([]string{"unload", plistPath})

	// Remove plist file
	if _, err := os.Stat(plistPath); err == nil {
		// Move to Trash instead of deleting
		home := resolveHomeDir(env)
		if home != "" {
			trashDir := filepath.Join(home, ".Trash")
			label := resolveLaunchdLabel(env)
			destPath := filepath.Join(trashDir, label+".plist")
			if err := os.MkdirAll(trashDir, 0o755); err == nil {
				if err := os.Rename(plistPath, destPath); err == nil {
					return nil
				}
			}

			// Fall back to deletion
			if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else {
			if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

// StopLaunchAgent stops a running LaunchAgent.
func StopLaunchAgent(env map[string]string) error {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	label := resolveLaunchdLabel(env)
	serviceID := fmt.Sprintf("%s/%s", domain, label)

	_, stderr, code := execLaunchctl([]string{"bootout", serviceID})
	if code != 0 && !isLaunchctlNotLoaded(stderr) {
		return fmt.Errorf("launchctl bootout failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// RestartLaunchAgent restarts a LaunchAgent.
func RestartLaunchAgent(env map[string]string) error {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	label := resolveLaunchdLabel(env)
	serviceID := fmt.Sprintf("%s/%s", domain, label)

	_, stderr, code := execLaunchctl([]string{"kickstart", "-k", serviceID})
	if code != 0 {
		return fmt.Errorf("launchctl kickstart failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// IsLaunchAgentLoaded checks if a LaunchAgent is loaded.
func IsLaunchAgentLoaded(env map[string]string) (bool, error) {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	label := resolveLaunchdLabel(env)
	serviceID := fmt.Sprintf("%s/%s", domain, label)

	_, _, code := execLaunchctl([]string{"print", serviceID})
	return code == 0, nil
}

// ReadLaunchAgentRuntime returns the runtime status of a LaunchAgent.
func ReadLaunchAgentRuntime(env map[string]string) (*ServiceRuntime, error) {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	label := resolveLaunchdLabel(env)
	serviceID := fmt.Sprintf("%s/%s", domain, label)

	stdout, stderr, code := execLaunchctl([]string{"print", serviceID})
	if code != 0 {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		return &ServiceRuntime{
			Status:      "unknown",
			Detail:      detail,
			MissingUnit: true,
		}, nil
	}

	info := parseLaunchctlPrint(stdout)

	// Check if plist file exists
	plistPath := resolveLaunchdPlistPath(env)
	plistExists := true
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		plistExists = false
	}

	state := strings.ToLower(info.State)
	status := "unknown"
	if state == "running" || info.PID > 0 {
		status = "running"
	} else if state != "" {
		status = "stopped"
	}

	return &ServiceRuntime{
		Status:         status,
		State:          info.State,
		PID:            info.PID,
		LastExitStatus: info.LastExitStatus,
		LastExitReason: info.LastExitReason,
		CachedLabel:    !plistExists,
	}, nil
}

// LaunchctlPrintInfo contains parsed launchctl print output.
type LaunchctlPrintInfo struct {
	State          string
	PID            int
	LastExitStatus int
	LastExitReason string
}

// parseLaunchctlPrint parses the output of launchctl print.
func parseLaunchctlPrint(output string) LaunchctlPrintInfo {
	entries := parseKeyValueOutput(output, "=")
	info := LaunchctlPrintInfo{}

	if state := entries["state"]; state != "" {
		info.State = state
	}

	if pidStr := entries["pid"]; pidStr != "" {
		if pid, err := strconv.Atoi(pidStr); err == nil {
			info.PID = pid
		}
	}

	if statusStr := entries["last exit status"]; statusStr != "" {
		if status, err := strconv.Atoi(statusStr); err == nil {
			info.LastExitStatus = status
		}
	}

	if reason := entries["last exit reason"]; reason != "" {
		info.LastExitReason = reason
	}

	return info
}

// isLaunchctlNotLoaded checks if the error indicates the service is not loaded.
func isLaunchctlNotLoaded(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no such process") ||
		strings.Contains(lower, "could not find service") ||
		strings.Contains(lower, "not found")
}

// LegacyLaunchAgent represents a legacy LaunchAgent that may need cleanup.
type LegacyLaunchAgent struct {
	Label     string
	PlistPath string
	Loaded    bool
	Exists    bool
}

// FindLegacyLaunchAgents finds legacy LaunchAgents that may need cleanup.
func FindLegacyLaunchAgents(env map[string]string) ([]LegacyLaunchAgent, error) {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	var results []LegacyLaunchAgent

	legacyLabels := []string{LegacyLaunchdLabel}
	home := resolveHomeDir(env)
	if home == "" {
		return results, nil
	}

	for _, label := range legacyLabels {
		plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")

		// Check if loaded
		serviceID := fmt.Sprintf("%s/%s", domain, label)
		_, _, code := execLaunchctl([]string{"print", serviceID})
		loaded := code == 0

		// Check if file exists
		exists := false
		if _, err := os.Stat(plistPath); err == nil {
			exists = true
		}

		if loaded || exists {
			results = append(results, LegacyLaunchAgent{
				Label:     label,
				PlistPath: plistPath,
				Loaded:    loaded,
				Exists:    exists,
			})
		}
	}

	return results, nil
}

// UninstallLegacyLaunchAgents removes legacy LaunchAgents.
func UninstallLegacyLaunchAgents(env map[string]string) ([]LegacyLaunchAgent, error) {
	agents, err := FindLegacyLaunchAgents(env)
	if err != nil {
		return nil, err
	}

	if len(agents) == 0 {
		return agents, nil
	}

	domain := resolveGUIDomain()
	home := resolveHomeDir(env)
	trashDir := ""
	if home != "" {
		trashDir = filepath.Join(home, ".Trash")
		if err := os.MkdirAll(trashDir, 0o755); err != nil {
			trashDir = ""
		}
	}

	var firstErr error
	for _, agent := range agents {
		execLaunchctl([]string{"bootout", domain, agent.PlistPath})
		execLaunchctl([]string{"unload", agent.PlistPath})

		if agent.Exists {
			if trashDir != "" {
				destPath := filepath.Join(trashDir, agent.Label+".plist")
				if err := os.Rename(agent.PlistPath, destPath); err != nil {
					if err := os.Remove(agent.PlistPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
						firstErr = err
					}
				}
			} else {
				if err := os.Remove(agent.PlistPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
					firstErr = err
				}
			}
		}
	}

	return agents, firstErr
}

// RepairLaunchAgentBootstrap attempts to repair a LaunchAgent bootstrap.
func RepairLaunchAgentBootstrap(env map[string]string) error {
	if env == nil {
		env = make(map[string]string)
	}

	domain := resolveGUIDomain()
	label := resolveLaunchdLabel(env)
	plistPath := resolveLaunchdPlistPath(env)

	_, stderr, code := execLaunchctl([]string{"bootstrap", domain, plistPath})
	if code != 0 {
		return fmt.Errorf("launchctl bootstrap failed: %s", strings.TrimSpace(stderr))
	}

	serviceID := fmt.Sprintf("%s/%s", domain, label)
	_, stderr, code = execLaunchctl([]string{"kickstart", "-k", serviceID})
	if code != 0 {
		return fmt.Errorf("launchctl kickstart failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}
