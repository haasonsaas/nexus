package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SchtasksManager manages Windows Scheduled Task services.
type SchtasksManager struct{}

// Label returns "Scheduled Task".
func (m *SchtasksManager) Label() string {
	return "Scheduled Task"
}

// Install installs and starts a scheduled task.
func (m *SchtasksManager) Install(opts InstallOptions) (*InstallResult, error) {
	return InstallScheduledTask(opts)
}

// Uninstall removes a scheduled task.
func (m *SchtasksManager) Uninstall(env map[string]string) error {
	return UninstallScheduledTask(env)
}

// Stop stops a scheduled task.
func (m *SchtasksManager) Stop(env map[string]string) error {
	return StopScheduledTask(env)
}

// Restart restarts a scheduled task.
func (m *SchtasksManager) Restart(env map[string]string) error {
	return RestartScheduledTask(env)
}

// IsInstalled checks if a scheduled task is installed.
func (m *SchtasksManager) IsInstalled(env map[string]string) (bool, error) {
	return IsScheduledTaskInstalled(env)
}

// Runtime returns the runtime status of a scheduled task.
func (m *SchtasksManager) Runtime(env map[string]string) (*ServiceRuntime, error) {
	return ReadScheduledTaskRuntime(env)
}

// resolveWindowsTaskName returns the task name from environment or default.
func resolveWindowsTaskName(env map[string]string) string {
	if override := strings.TrimSpace(env[EnvNexusWindowsTask]); override != "" {
		return override
	}
	profile := resolveProfile(env)
	if profile != "" {
		return DefaultWindowsTaskName + " (" + profile + ")"
	}
	return DefaultWindowsTaskName
}

// resolveTaskScriptPath returns the path to the task script.
func resolveTaskScriptPath(env map[string]string) string {
	if override := strings.TrimSpace(env["NEXUS_TASK_SCRIPT"]); override != "" {
		return override
	}
	scriptName := env["NEXUS_TASK_SCRIPT_NAME"]
	if scriptName == "" {
		scriptName = "gateway.cmd"
	}
	stateDir := resolveStateDir(env)
	if stateDir == "" {
		stateDir = "."
	}
	return filepath.Join(stateDir, scriptName)
}

// resolveTaskUser returns the current user for task scheduling.
func resolveTaskUser(env map[string]string) string {
	username := env["USERNAME"]
	if username == "" {
		username = env["USER"]
	}
	if username == "" {
		username = env["LOGNAME"]
	}
	if username == "" {
		return ""
	}
	if strings.Contains(username, "\\") {
		return username
	}
	domain := env["USERDOMAIN"]
	if domain != "" {
		return domain + "\\" + username
	}
	return username
}

// execSchtasks runs schtasks with the given arguments.
func execSchtasks(args []string) (stdout, stderr string, code int) {
	cmd := exec.Command("schtasks", args...)
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

// assertSchtasksAvailable checks if schtasks is available.
func assertSchtasksAvailable() error {
	_, stderr, code := execSchtasks([]string{"/Query"})
	if code == 0 {
		return nil
	}
	return fmt.Errorf("schtasks unavailable: %s", strings.TrimSpace(stderr))
}

// quoteCmdArg quotes an argument for Windows cmd.exe.
func quoteCmdArg(value string) string {
	if !strings.ContainsAny(value, " \t\"") {
		return value
	}
	escaped := strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + escaped + "\""
}

// BuildTaskScript builds a .cmd script for the scheduled task.
func BuildTaskScript(opts struct {
	Description      string
	ProgramArguments []string
	WorkingDirectory string
	Environment      map[string]string
}) string {
	lines := []string{"@echo off"}

	if opts.Description != "" {
		lines = append(lines, "rem "+opts.Description)
	}

	if opts.WorkingDirectory != "" {
		lines = append(lines, "cd /d "+quoteCmdArg(opts.WorkingDirectory))
	}

	for k, v := range opts.Environment {
		if v != "" {
			lines = append(lines, "set "+k+"="+v)
		}
	}

	var cmdParts []string
	for _, arg := range opts.ProgramArguments {
		cmdParts = append(cmdParts, quoteCmdArg(arg))
	}
	lines = append(lines, strings.Join(cmdParts, " "))

	return strings.Join(lines, "\r\n") + "\r\n"
}

// InstallScheduledTask installs and starts a scheduled task.
func InstallScheduledTask(opts InstallOptions) (*InstallResult, error) {
	if err := assertSchtasksAvailable(); err != nil {
		return nil, err
	}

	env := opts.Env
	if env == nil {
		env = make(map[string]string)
	}

	scriptPath := resolveTaskScriptPath(env)
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create script directory: %w", err)
	}

	// Build and write script
	description := opts.Description
	if description == "" {
		description = formatServiceDescription(env)
	}

	script := BuildTaskScript(struct {
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

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write task script: %w", err)
	}

	taskName := resolveWindowsTaskName(env)
	quotedScript := quoteCmdArg(scriptPath)

	baseArgs := []string{
		"/Create",
		"/F",             // force overwrite
		"/SC", "ONLOGON", // run at logon
		"/RL", "LIMITED", // run with limited privileges
		"/TN", taskName,
		"/TR", quotedScript,
	}

	taskUser := resolveTaskUser(env)
	var createCode int
	var createStderr string

	if taskUser != "" {
		// Try with user-specific options first
		args := append(baseArgs, "/RU", taskUser, "/NP", "/IT")
		_, createStderr, createCode = execSchtasks(args)
		if createCode != 0 {
			// Fall back to basic options
			_, createStderr, createCode = execSchtasks(baseArgs)
		}
	} else {
		_, createStderr, createCode = execSchtasks(baseArgs)
	}

	if createCode != 0 {
		detail := strings.TrimSpace(createStderr)
		hint := ""
		if strings.Contains(strings.ToLower(detail), "access is denied") {
			hint = " Run PowerShell as Administrator or rerun without installing the daemon."
		}
		return nil, fmt.Errorf("schtasks create failed: %s%s", detail, hint)
	}

	// Start the task
	execSchtasks([]string{"/Run", "/TN", taskName})

	return &InstallResult{Path: scriptPath}, nil
}

// UninstallScheduledTask removes a scheduled task.
func UninstallScheduledTask(env map[string]string) error {
	if err := assertSchtasksAvailable(); err != nil {
		return err
	}

	if env == nil {
		env = make(map[string]string)
	}

	taskName := resolveWindowsTaskName(env)

	// Delete the task
	execSchtasks([]string{"/Delete", "/F", "/TN", taskName})

	// Remove the script
	scriptPath := resolveTaskScriptPath(env)
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove task script: %w", err)
	}

	return nil
}

// isTaskNotRunning checks if the error indicates the task is not running.
func isTaskNotRunning(output string) bool {
	return strings.Contains(strings.ToLower(output), "not running")
}

// StopScheduledTask stops a scheduled task.
func StopScheduledTask(env map[string]string) error {
	if err := assertSchtasksAvailable(); err != nil {
		return err
	}

	if env == nil {
		env = make(map[string]string)
	}

	taskName := resolveWindowsTaskName(env)

	_, stderr, code := execSchtasks([]string{"/End", "/TN", taskName})
	if code != 0 && !isTaskNotRunning(stderr) {
		return fmt.Errorf("schtasks end failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// RestartScheduledTask restarts a scheduled task.
func RestartScheduledTask(env map[string]string) error {
	if err := assertSchtasksAvailable(); err != nil {
		return err
	}

	if env == nil {
		env = make(map[string]string)
	}

	taskName := resolveWindowsTaskName(env)

	// Stop first (ignore errors)
	execSchtasks([]string{"/End", "/TN", taskName})

	// Run again
	_, stderr, code := execSchtasks([]string{"/Run", "/TN", taskName})
	if code != 0 {
		return fmt.Errorf("schtasks run failed: %s", strings.TrimSpace(stderr))
	}

	return nil
}

// IsScheduledTaskInstalled checks if a scheduled task is installed.
func IsScheduledTaskInstalled(env map[string]string) (bool, error) {
	if err := assertSchtasksAvailable(); err != nil {
		return false, err
	}

	if env == nil {
		env = make(map[string]string)
	}

	taskName := resolveWindowsTaskName(env)

	_, _, code := execSchtasks([]string{"/Query", "/TN", taskName})
	return code == 0, nil
}

// ReadScheduledTaskRuntime returns the runtime status of a scheduled task.
func ReadScheduledTaskRuntime(env map[string]string) (*ServiceRuntime, error) {
	if err := assertSchtasksAvailable(); err != nil {
		return &ServiceRuntime{
			Status: "unknown",
			Detail: err.Error(),
		}, nil
	}

	if env == nil {
		env = make(map[string]string)
	}

	taskName := resolveWindowsTaskName(env)

	stdout, stderr, code := execSchtasks([]string{"/Query", "/TN", taskName, "/V", "/FO", "LIST"})
	if code != 0 {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		missing := strings.Contains(strings.ToLower(detail), "cannot find the file")
		return &ServiceRuntime{
			Status:      "stopped",
			Detail:      detail,
			MissingUnit: missing,
		}, nil
	}

	info := parseSchtasksQuery(stdout)
	statusRaw := strings.ToLower(info.Status)
	status := "unknown"
	if statusRaw == "running" {
		status = "running"
	} else if statusRaw != "" {
		status = "stopped"
	}

	return &ServiceRuntime{
		Status:        status,
		State:         info.Status,
		LastRunTime:   info.LastRunTime,
		LastRunResult: info.LastRunResult,
	}, nil
}

// SchtasksQueryInfo contains parsed schtasks query output.
type SchtasksQueryInfo struct {
	Status        string
	LastRunTime   string
	LastRunResult string
}

// parseSchtasksQuery parses the output of schtasks /Query /V.
func parseSchtasksQuery(output string) SchtasksQueryInfo {
	entries := parseKeyValueOutput(output, ":")
	info := SchtasksQueryInfo{}

	if status := entries["status"]; status != "" {
		info.Status = status
	}

	if lastRunTime := entries["last run time"]; lastRunTime != "" {
		info.LastRunTime = lastRunTime
	}

	if lastRunResult := entries["last run result"]; lastRunResult != "" {
		info.LastRunResult = lastRunResult
	}

	return info
}

// ReadScheduledTaskCommand reads the command from the task script.
func ReadScheduledTaskCommand(env map[string]string) (programArguments []string, workingDirectory string, environment map[string]string, err error) {
	if env == nil {
		env = make(map[string]string)
	}

	scriptPath := resolveTaskScriptPath(env)
	content, readErr := os.ReadFile(scriptPath)
	if readErr != nil {
		return nil, "", nil, readErr
	}

	environment = make(map[string]string)
	var commandLine string

	for _, line := range splitLines(string(content)) {
		line = trimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "@echo") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "rem ") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "set ") {
			assignment := line[4:]
			idx := strings.Index(assignment, "=")
			if idx > 0 {
				key := strings.TrimSpace(assignment[:idx])
				value := strings.TrimSpace(assignment[idx+1:])
				if key != "" {
					environment[key] = value
				}
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "cd /d ") {
			wd := strings.TrimSpace(line[6:])
			wd = strings.Trim(wd, "\"")
			workingDirectory = wd
			continue
		}
		// First non-special line is the command
		commandLine = line
		break
	}

	if commandLine == "" {
		return nil, "", nil, fmt.Errorf("command not found in task script")
	}

	programArguments = parseWindowsCommandLine(commandLine)
	return programArguments, workingDirectory, environment, nil
}

// parseWindowsCommandLine parses a Windows command line into arguments.
// On Windows, backslashes are path separators and only \" is treated as an escaped quote.
func parseWindowsCommandLine(value string) []string {
	var args []string
	var current string
	inQuotes := false
	i := 0
	runes := []rune(value)

	for i < len(runes) {
		char := runes[i]

		// Handle escape sequence for quotes (\" inside quotes)
		if char == '\\' && i+1 < len(runes) && runes[i+1] == '"' {
			current += "\""
			i += 2
			continue
		}

		if char == '"' {
			inQuotes = !inQuotes
			i++
			continue
		}

		if !inQuotes && (char == ' ' || char == '\t') {
			if current != "" {
				args = append(args, current)
				current = ""
			}
			i++
			continue
		}

		current += string(char)
		i++
	}

	if current != "" {
		args = append(args, current)
	}
	return args
}
