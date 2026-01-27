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

// SystemdManager manages Linux systemd user services.
type SystemdManager struct{}

// Label returns "systemd".
func (m *SystemdManager) Label() string {
	return "systemd"
}

// Install installs and starts a systemd user service.
func (m *SystemdManager) Install(opts InstallOptions) (*InstallResult, error) {
	return InstallSystemdService(opts)
}

// Uninstall removes a systemd user service.
func (m *SystemdManager) Uninstall(env map[string]string) error {
	return UninstallSystemdService(env)
}

// Stop stops a systemd user service.
func (m *SystemdManager) Stop(env map[string]string) error {
	return StopSystemdService(env)
}

// Restart restarts a systemd user service.
func (m *SystemdManager) Restart(env map[string]string) error {
	return RestartSystemdService(env)
}

// IsInstalled checks if a systemd user service is enabled.
func (m *SystemdManager) IsInstalled(env map[string]string) (bool, error) {
	return IsSystemdServiceEnabled(env)
}

// Runtime returns the runtime status of a systemd user service.
func (m *SystemdManager) Runtime(env map[string]string) (*ServiceRuntime, error) {
	return ReadSystemdServiceRuntime(env)
}

// resolveSystemdServiceName returns the systemd service name from environment or default.
func resolveSystemdServiceName(env map[string]string) string {
	if override := strings.TrimSpace(env[EnvNexusSystemdUnit]); override != "" {
		// Remove .service suffix if present
		if strings.HasSuffix(override, ".service") {
			return override[:len(override)-8]
		}
		return override
	}
	profile := resolveProfile(env)
	if profile != "" {
		return DefaultSystemdServiceName + "-" + profile
	}
	return DefaultSystemdServiceName
}

// resolveSystemdUnitPath returns the path to the systemd unit file.
func resolveSystemdUnitPath(env map[string]string) string {
	home := resolveHomeDir(env)
	if home == "" {
		home = "."
	}
	serviceName := resolveSystemdServiceName(env)
	return filepath.Join(home, ".config", "systemd", "user", serviceName+".service")
}

// execSystemctl runs systemctl with the given arguments.
func execSystemctl(args []string) (stdout, stderr string, code int) {
	cmd := exec.Command("systemctl", args...)
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

// IsSystemdUserServiceAvailable checks if systemd user services are available.
func IsSystemdUserServiceAvailable() bool {
	_, stderr, code := execSystemctl([]string{"--user", "status"})
	if code == 0 {
		return true
	}
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "not found") ||
		strings.Contains(lower, "failed to connect") ||
		strings.Contains(lower, "not been booted") ||
		strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "not supported") {
		return false
	}
	return false
}

// assertSystemdAvailable checks if systemctl is available and returns an error if not.
func assertSystemdAvailable() error {
	_, stderr, code := execSystemctl([]string{"--user", "status"})
	if code == 0 {
		return nil
	}
	if strings.Contains(strings.ToLower(stderr), "not found") {
		return fmt.Errorf("systemctl not available; systemd user services are required on Linux")
	}
	return fmt.Errorf("systemctl --user unavailable: %s", strings.TrimSpace(stderr))
}

// BuildSystemdUnit builds a systemd unit file content.
func BuildSystemdUnit(opts struct {
	Description      string
	ProgramArguments []string
	WorkingDirectory string
	Environment      map[string]string
}) string {
	var lines []string

	// [Unit] section
	lines = append(lines, "[Unit]")
	description := opts.Description
	if description == "" {
		description = "Nexus Gateway"
	}
	lines = append(lines, "Description="+description)
	lines = append(lines, "After=network-online.target")
	lines = append(lines, "Wants=network-online.target")
	lines = append(lines, "")

	// [Service] section
	lines = append(lines, "[Service]")
	execStart := systemdQuoteArgs(opts.ProgramArguments)
	lines = append(lines, "ExecStart="+execStart)
	lines = append(lines, "Restart=always")
	lines = append(lines, "RestartSec=5")
	// KillMode=process ensures systemd only waits for the main process to exit.
	// Without this, child processes block shutdown.
	lines = append(lines, "KillMode=process")

	if opts.WorkingDirectory != "" {
		lines = append(lines, "WorkingDirectory="+systemdEscapeArg(opts.WorkingDirectory))
	}

	for k, v := range opts.Environment {
		if v != "" {
			lines = append(lines, "Environment="+systemdEscapeArg(k+"="+v))
		}
	}
	lines = append(lines, "")

	// [Install] section
	lines = append(lines, "[Install]")
	lines = append(lines, "WantedBy=default.target")
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

// systemdEscapeArg escapes a single argument for systemd unit files.
func systemdEscapeArg(value string) string {
	if !strings.ContainsAny(value, " \t\"\\") {
		return value
	}
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return "\"" + escaped + "\""
}

// systemdQuoteArgs joins arguments with proper escaping for ExecStart.
func systemdQuoteArgs(args []string) string {
	var parts []string
	for _, arg := range args {
		parts = append(parts, systemdEscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

// InstallSystemdService installs and starts a systemd user service.
func InstallSystemdService(opts InstallOptions) (*InstallResult, error) {
	if err := assertSystemdAvailable(); err != nil {
		return nil, err
	}

	env := opts.Env
	if env == nil {
		env = make(map[string]string)
	}

	unitPath := resolveSystemdUnitPath(env)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	// Build and write unit file
	description := opts.Description
	if description == "" {
		description = formatServiceDescription(env)
	}

	unit := BuildSystemdUnit(struct {
		Description      string
		ProgramArguments []string
		WorkingDirectory string
		Environment      map[string]string
	}{
		Description:      description,
		ProgramArguments: opts.ProgramArguments,
		WorkingDirectory: opts.WorkingDirectory,
		Environment:      opts.Environment,
	})

	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write unit file: %w", err)
	}

	serviceName := resolveSystemdServiceName(env)
	unitName := serviceName + ".service"

	// Reload systemd
	_, stderr, code := execSystemctl([]string{"--user", "daemon-reload"})
	if code != 0 {
		return nil, fmt.Errorf("systemctl daemon-reload failed: %s", strings.TrimSpace(stderr))
	}

	// Enable the service
	_, stderr, code = execSystemctl([]string{"--user", "enable", unitName})
	if code != 0 {
		return nil, fmt.Errorf("systemctl enable failed: %s", strings.TrimSpace(stderr))
	}

	// Start/restart the service
	_, stderr, code = execSystemctl([]string{"--user", "restart", unitName})
	if code != 0 {
		return nil, fmt.Errorf("systemctl restart failed: %s", strings.TrimSpace(stderr))
	}

	return &InstallResult{Path: unitPath}, nil
}

// UninstallSystemdService stops and removes a systemd user service.
func UninstallSystemdService(env map[string]string) error {
	if err := assertSystemdAvailable(); err != nil {
		return err
	}

	if env == nil {
		env = make(map[string]string)
	}

	serviceName := resolveSystemdServiceName(env)
	unitName := serviceName + ".service"

	// Disable and stop
	execSystemctl([]string{"--user", "disable", "--now", unitName})

	// Remove unit file
	unitPath := resolveSystemdUnitPath(env)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	return nil
}

// StopSystemdService stops a systemd user service.
func StopSystemdService(env map[string]string) error {
	if err := assertSystemdAvailable(); err != nil {
		return err
	}

	if env == nil {
		env = make(map[string]string)
	}

	serviceName := resolveSystemdServiceName(env)
	unitName := serviceName + ".service"

	_, stderr, code := execSystemctl([]string{"--user", "stop", unitName})
	if code != 0 {
		return fmt.Errorf("systemctl stop failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// RestartSystemdService restarts a systemd user service.
func RestartSystemdService(env map[string]string) error {
	if err := assertSystemdAvailable(); err != nil {
		return err
	}

	if env == nil {
		env = make(map[string]string)
	}

	serviceName := resolveSystemdServiceName(env)
	unitName := serviceName + ".service"

	_, stderr, code := execSystemctl([]string{"--user", "restart", unitName})
	if code != 0 {
		return fmt.Errorf("systemctl restart failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// IsSystemdServiceEnabled checks if a systemd user service is enabled.
func IsSystemdServiceEnabled(env map[string]string) (bool, error) {
	if err := assertSystemdAvailable(); err != nil {
		return false, err
	}

	if env == nil {
		env = make(map[string]string)
	}

	serviceName := resolveSystemdServiceName(env)
	unitName := serviceName + ".service"

	_, _, code := execSystemctl([]string{"--user", "is-enabled", unitName})
	return code == 0, nil
}

// ReadSystemdServiceRuntime returns the runtime status of a systemd user service.
func ReadSystemdServiceRuntime(env map[string]string) (*ServiceRuntime, error) {
	if err := assertSystemdAvailable(); err != nil {
		return &ServiceRuntime{
			Status: "unknown",
			Detail: err.Error(),
		}, nil
	}

	if env == nil {
		env = make(map[string]string)
	}

	serviceName := resolveSystemdServiceName(env)
	unitName := serviceName + ".service"

	stdout, stderr, code := execSystemctl([]string{
		"--user", "show", unitName,
		"--no-page",
		"--property", "ActiveState,SubState,MainPID,ExecMainStatus,ExecMainCode",
	})

	if code != 0 {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		missing := strings.Contains(strings.ToLower(detail), "not found")
		return &ServiceRuntime{
			Status:      "stopped",
			Detail:      detail,
			MissingUnit: missing,
		}, nil
	}

	info := parseSystemdShow(stdout)
	activeState := strings.ToLower(info.ActiveState)
	status := "unknown"
	if activeState == "active" {
		status = "running"
	} else if activeState != "" {
		status = "stopped"
	}

	return &ServiceRuntime{
		Status:         status,
		State:          info.ActiveState,
		SubState:       info.SubState,
		PID:            info.MainPID,
		LastExitStatus: info.ExecMainStatus,
		LastExitReason: info.ExecMainCode,
	}, nil
}

// SystemdShowInfo contains parsed systemctl show output.
type SystemdShowInfo struct {
	ActiveState    string
	SubState       string
	MainPID        int
	ExecMainStatus int
	ExecMainCode   string
}

// parseSystemdShow parses the output of systemctl show.
func parseSystemdShow(output string) SystemdShowInfo {
	entries := parseKeyValueOutput(output, "=")
	info := SystemdShowInfo{}

	if state := entries["activestate"]; state != "" {
		info.ActiveState = state
	}

	if subState := entries["substate"]; subState != "" {
		info.SubState = subState
	}

	if pidStr := entries["mainpid"]; pidStr != "" {
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			info.MainPID = pid
		}
	}

	if statusStr := entries["execmainstatus"]; statusStr != "" {
		if status, err := strconv.Atoi(statusStr); err == nil {
			info.ExecMainStatus = status
		}
	}

	if code := entries["execmaincode"]; code != "" {
		info.ExecMainCode = code
	}

	return info
}

// LegacySystemdUnit represents a prior systemd unit that may need cleanup.
type LegacySystemdUnit struct {
	Name     string
	UnitPath string
	Enabled  bool
	Exists   bool
}

// FindLegacySystemdUnits finds prior systemd units that may need cleanup.
func FindLegacySystemdUnits(env map[string]string) ([]LegacySystemdUnit, error) {
	if env == nil {
		env = make(map[string]string)
	}

	var results []LegacySystemdUnit

	// No prior names defined yet for Nexus
	legacyNames := []string{}

	home := resolveHomeDir(env)
	if home == "" {
		return results, nil
	}

	systemctlAvailable := IsSystemdUserServiceAvailable()

	for _, name := range legacyNames {
		unitPath := filepath.Join(home, ".config", "systemd", "user", name+".service")

		// Check if file exists
		exists := false
		if _, err := os.Stat(unitPath); err == nil {
			exists = true
		}

		// Check if enabled
		enabled := false
		if systemctlAvailable {
			_, _, code := execSystemctl([]string{"--user", "is-enabled", name + ".service"})
			enabled = code == 0
		}

		if exists || enabled {
			results = append(results, LegacySystemdUnit{
				Name:     name,
				UnitPath: unitPath,
				Enabled:  enabled,
				Exists:   exists,
			})
		}
	}

	return results, nil
}

// UninstallLegacySystemdUnits removes prior systemd units.
func UninstallLegacySystemdUnits(env map[string]string) ([]LegacySystemdUnit, error) {
	units, err := FindLegacySystemdUnits(env)
	if err != nil {
		return nil, err
	}

	if len(units) == 0 {
		return units, nil
	}

	systemctlAvailable := IsSystemdUserServiceAvailable()

	for _, unit := range units {
		if systemctlAvailable {
			execSystemctl([]string{"--user", "disable", "--now", unit.Name + ".service"})
		}

		if unit.Exists {
			os.Remove(unit.UnitPath)
		}
	}

	return units, nil
}

// ParseSystemdExecStart parses an ExecStart value into arguments.
func ParseSystemdExecStart(value string) []string {
	var args []string
	var current string
	inQuotes := false
	escapeNext := false

	for _, char := range value {
		if escapeNext {
			current += string(char)
			escapeNext = false
			continue
		}
		if char == '\\' {
			escapeNext = true
			continue
		}
		if char == '"' {
			inQuotes = !inQuotes
			continue
		}
		if !inQuotes && (char == ' ' || char == '\t') {
			if current != "" {
				args = append(args, current)
				current = ""
			}
			continue
		}
		current += string(char)
	}
	if current != "" {
		args = append(args, current)
	}
	return args
}

// ParseSystemdEnvAssignment parses an Environment= line value.
func ParseSystemdEnvAssignment(raw string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}

	// Handle quoted form
	if strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
		unquoted := ""
		escapeNext := false
		for _, ch := range trimmed[1 : len(trimmed)-1] {
			if escapeNext {
				unquoted += string(ch)
				escapeNext = false
				continue
			}
			if ch == '\\' {
				escapeNext = true
				continue
			}
			unquoted += string(ch)
		}
		trimmed = unquoted
	}

	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", "", false
	}

	key = strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", "", false
	}
	value = trimmed[idx+1:]
	return key, value, true
}

// ReadSystemdServiceExecStart reads the ExecStart from a unit file.
func ReadSystemdServiceExecStart(env map[string]string) (programArguments []string, workingDirectory string, environment map[string]string, err error) {
	if env == nil {
		env = make(map[string]string)
	}

	unitPath := resolveSystemdUnitPath(env)
	content, readErr := os.ReadFile(unitPath)
	if readErr != nil {
		return nil, "", nil, readErr
	}

	environment = make(map[string]string)
	var execStart string

	for _, line := range splitLines(string(content)) {
		line = trimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "ExecStart=") {
			execStart = line[len("ExecStart="):]
		} else if strings.HasPrefix(line, "WorkingDirectory=") {
			workingDirectory = line[len("WorkingDirectory="):]
		} else if strings.HasPrefix(line, "Environment=") {
			raw := line[len("Environment="):]
			if k, v, ok := ParseSystemdEnvAssignment(raw); ok {
				environment[k] = v
			}
		}
	}

	if execStart == "" {
		return nil, "", nil, fmt.Errorf("ExecStart not found in unit file")
	}

	programArguments = ParseSystemdExecStart(execStart)
	return programArguments, workingDirectory, environment, nil
}
