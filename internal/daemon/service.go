// Package daemon provides cross-platform daemon/service management for Nexus.
// It supports macOS LaunchAgent, Linux systemd user services, and Windows Task Scheduler.
package daemon

import (
	"runtime"
)

// ServiceRuntime contains runtime status information for a daemon service.
type ServiceRuntime struct {
	Status         string // "running", "stopped", "unknown"
	State          string // platform-specific state string
	SubState       string // systemd sub-state (Linux only)
	PID            int    // process ID if running
	LastExitStatus int    // last exit code
	LastExitReason string // exit reason description
	LastRunTime    string // last run time (Windows only)
	LastRunResult  string // last run result (Windows only)
	Detail         string // error detail message
	CachedLabel    bool   // plist exists but is cached (macOS only)
	MissingUnit    bool   // unit/plist/task is missing
}

// InstallOptions contains configuration for installing a daemon service.
type InstallOptions struct {
	Env              map[string]string // environment variable overrides for path resolution
	ProgramArguments []string          // command and arguments to execute
	WorkingDirectory string            // working directory for the service
	Environment      map[string]string // environment variables to set in the service
	Description      string            // service description
}

// InstallResult contains the result of installing a daemon service.
type InstallResult struct {
	Path string // path to the installed service file (plist, unit, or script)
}

// ServiceManager defines the interface for cross-platform service management.
type ServiceManager interface {
	// Label returns a human-readable name for the service type (e.g., "LaunchAgent", "systemd", "Scheduled Task")
	Label() string

	// Install installs and starts the daemon service.
	Install(opts InstallOptions) (*InstallResult, error)

	// Uninstall stops and removes the daemon service.
	Uninstall(env map[string]string) error

	// Stop stops the running daemon service.
	Stop(env map[string]string) error

	// Restart restarts the daemon service.
	Restart(env map[string]string) error

	// IsInstalled checks if the service is installed and enabled.
	IsInstalled(env map[string]string) (bool, error)

	// Runtime returns the current runtime status of the service.
	Runtime(env map[string]string) (*ServiceRuntime, error)
}

// GetServiceManager returns the appropriate ServiceManager for the current platform.
// It returns nil if the current platform is not supported.
func GetServiceManager() ServiceManager {
	switch runtime.GOOS {
	case "darwin":
		return &LaunchdManager{}
	case "linux":
		return &SystemdManager{}
	case "windows":
		return &SchtasksManager{}
	default:
		return nil
	}
}

// Constants for service names and labels.
const (
	// DefaultLaunchdLabel is the default label for macOS LaunchAgent.
	DefaultLaunchdLabel = "com.haasonsaas.nexus.gateway"

	// DefaultSystemdServiceName is the default name for Linux systemd user service.
	DefaultSystemdServiceName = "nexus-gateway"

	// DefaultWindowsTaskName is the default name for Windows scheduled task.
	DefaultWindowsTaskName = "Nexus Gateway"

	// ServiceMarker is used to identify nexus services.
	ServiceMarker = "nexus"

	// LegacyLaunchdLabel is the old label that may need cleanup.
	LegacyLaunchdLabel = "com.haasonsaas.nexus"
)

// Environment variable names for overriding defaults.
const (
	EnvNexusProfile        = "NEXUS_PROFILE"
	EnvNexusStateDir       = "NEXUS_STATE_DIR"
	EnvNexusLaunchdLabel   = "NEXUS_LAUNCHD_LABEL"
	EnvNexusSystemdUnit    = "NEXUS_SYSTEMD_UNIT"
	EnvNexusWindowsTask    = "NEXUS_WINDOWS_TASK_NAME"
	EnvNexusLogPrefix      = "NEXUS_LOG_PREFIX"
	EnvNexusServiceVersion = "NEXUS_SERVICE_VERSION"
)

// resolveHomeDir returns the home directory from environment.
func resolveHomeDir(env map[string]string) string {
	if home := env["HOME"]; home != "" {
		return home
	}
	if home := env["USERPROFILE"]; home != "" {
		return home
	}
	return ""
}

// resolveProfile returns the normalized profile name from environment.
func resolveProfile(env map[string]string) string {
	profile := env[EnvNexusProfile]
	if profile == "" || profile == "default" || profile == "Default" || profile == "DEFAULT" {
		return ""
	}
	return profile
}

// resolveStateDir returns the state directory for storing logs and scripts.
func resolveStateDir(env map[string]string) string {
	if stateDir := env[EnvNexusStateDir]; stateDir != "" {
		return stateDir
	}
	home := resolveHomeDir(env)
	if home == "" {
		return ""
	}
	profile := resolveProfile(env)
	if profile != "" {
		return home + "/.nexus-" + profile
	}
	return home + "/.nexus"
}

// formatServiceDescription creates a service description string.
func formatServiceDescription(env map[string]string) string {
	profile := resolveProfile(env)
	version := env[EnvNexusServiceVersion]
	parts := []string{}
	if profile != "" {
		parts = append(parts, "profile: "+profile)
	}
	if version != "" {
		parts = append(parts, "v"+version)
	}
	if len(parts) == 0 {
		return "Nexus Gateway"
	}
	return "Nexus Gateway (" + join(parts, ", ") + ")"
}

// join concatenates strings with a separator.
func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

// parseKeyValueOutput parses key-value output with a separator.
func parseKeyValueOutput(output, separator string) map[string]string {
	entries := make(map[string]string)
	for _, line := range splitLines(output) {
		line = trimSpace(line)
		if line == "" {
			continue
		}
		idx := indexOf(line, separator)
		if idx <= 0 {
			continue
		}
		key := toLower(trimSpace(line[:idx]))
		if key == "" {
			continue
		}
		value := trimSpace(line[idx+len(separator):])
		entries[key] = value
	}
	return entries
}

// splitLines splits a string into lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// trimSpace removes leading and trailing whitespace.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && isSpace(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

// isSpace checks if a byte is whitespace.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// indexOf returns the index of substring or -1.
func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// toLower converts a string to lowercase.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
