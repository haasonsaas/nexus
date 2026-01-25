package infra

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	machineDisplayName     string
	machineDisplayNameOnce sync.Once
	machineDisplayNameMu   sync.Mutex

	// commandExecutor allows tests to mock command execution
	commandExecutor = defaultCommandExecutor
)

// defaultCommandExecutor runs a command and returns its stdout
func defaultCommandExecutor(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getMacosComputerName attempts to get the macOS ComputerName via scutil
func getMacosComputerName() (string, error) {
	return commandExecutor("/usr/sbin/scutil", "--get", "ComputerName")
}

// getMacosLocalHostName attempts to get the macOS LocalHostName via scutil
func getMacosLocalHostName() (string, error) {
	return commandExecutor("/usr/sbin/scutil", "--get", "LocalHostName")
}

// getWindowsComputerName gets the computer name on Windows
// First tries COMPUTERNAME environment variable, then falls back to os.Hostname
func getWindowsComputerName() (string, error) {
	if name := os.Getenv("COMPUTERNAME"); name != "" {
		return name, nil
	}
	return os.Hostname()
}

// fallbackHostName returns os.Hostname with .local suffix removed
func fallbackHostName() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "nexus"
	}

	// Remove .local suffix (case-insensitive)
	hostname = strings.TrimSuffix(hostname, ".local")
	hostname = strings.TrimSuffix(hostname, ".LOCAL")
	hostname = strings.TrimSuffix(hostname, ".Local")

	// More thorough case-insensitive removal
	if strings.HasSuffix(strings.ToLower(hostname), ".local") {
		hostname = hostname[:len(hostname)-6]
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "nexus"
	}
	return hostname
}

// GetMachineDisplayName returns a human-friendly display name for this machine.
// On macOS, it tries ComputerName, then LocalHostName, then falls back to hostname.
// On Windows, it uses COMPUTERNAME env var or hostname.
// On other platforms, it uses hostname with .local suffix removed.
// The result is cached after the first call.
func GetMachineDisplayName() string {
	machineDisplayNameOnce.Do(func() {
		machineDisplayName = getMachineDisplayNameInternal()
	})
	return machineDisplayName
}

// getMachineDisplayNameInternal implements the actual name detection logic
func getMachineDisplayNameInternal() string {
	switch runtime.GOOS {
	case "darwin":
		// Try ComputerName first
		if name, err := getMacosComputerName(); err == nil && name != "" {
			return name
		}
		// Try LocalHostName second
		if name, err := getMacosLocalHostName(); err == nil && name != "" {
			return name
		}
		// Fall back to hostname
		return fallbackHostName()

	case "windows":
		if name, err := getWindowsComputerName(); err == nil && name != "" {
			return name
		}
		return fallbackHostName()

	default:
		// Linux and other platforms
		return fallbackHostName()
	}
}

// ResetMachineNameCacheForTest resets the cached machine name for testing purposes.
// This should only be used in tests.
func ResetMachineNameCacheForTest() {
	machineDisplayNameMu.Lock()
	defer machineDisplayNameMu.Unlock()
	machineDisplayName = ""
	machineDisplayNameOnce = sync.Once{}
}

// SetCommandExecutorForTest allows tests to mock command execution.
// It returns a function to restore the original executor.
func SetCommandExecutorForTest(executor func(name string, args ...string) (string, error)) func() {
	machineDisplayNameMu.Lock()
	defer machineDisplayNameMu.Unlock()
	original := commandExecutor
	commandExecutor = executor
	return func() {
		machineDisplayNameMu.Lock()
		defer machineDisplayNameMu.Unlock()
		commandExecutor = original
	}
}
