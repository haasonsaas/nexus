package infra

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// OSSummary contains information about the operating system.
type OSSummary struct {
	// Platform is the operating system type (darwin, linux, windows).
	Platform string
	// Arch is the architecture (amd64, arm64, etc).
	Arch string
	// Release is the OS release/version string.
	Release string
	// Label is a human-readable label like "macos 14.0 (arm64)".
	Label string
}

var (
	cachedOSSummary *OSSummary
	osSummaryOnce   sync.Once
)

// ResolveOSSummary returns a summary of the current operating system.
// The result is cached after the first call.
func ResolveOSSummary() OSSummary {
	osSummaryOnce.Do(func() {
		cachedOSSummary = resolveOSSummaryInternal()
	})
	return *cachedOSSummary
}

// resolveOSSummaryInternal builds the OS summary without caching.
// This is exported for testing purposes.
func resolveOSSummaryInternal() *OSSummary {
	platform := runtime.GOOS
	arch := runtime.GOARCH
	release := ""
	label := ""

	switch platform {
	case "darwin":
		release = getMacOSVersion()
		label = "macos " + release + " (" + arch + ")"
	case "linux":
		distro, version := getLinuxDistro()
		release = version
		if distro != "" {
			label = distro + " " + version + " (" + arch + ")"
		} else {
			label = "linux " + version + " (" + arch + ")"
		}
	case "windows":
		release = getWindowsVersion()
		label = "windows " + release + " (" + arch + ")"
	default:
		release = "unknown"
		label = platform + " " + release + " (" + arch + ")"
	}

	return &OSSummary{
		Platform: platform,
		Arch:     arch,
		Release:  release,
		Label:    label,
	}
}

// getMacOSVersion returns the macOS version by running sw_vers.
func getMacOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "unknown"
	}
	return version
}

// getLinuxDistro reads /etc/os-release to get the distribution name and version.
// Returns (distro, version) where distro is lowercased (e.g., "ubuntu", "debian").
func getLinuxDistro() (string, string) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "", "unknown"
	}
	defer file.Close()

	var distroID, versionID string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			distroID = strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			versionID = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`)
		}
	}

	if distroID == "" {
		distroID = ""
	}
	if versionID == "" {
		versionID = "unknown"
	}

	return strings.ToLower(distroID), versionID
}

// getWindowsVersion returns the Windows version.
// It uses systeminfo or falls back to a generic version string.
func getWindowsVersion() string {
	// Try using wmic to get OS version
	cmd := exec.Command("wmic", "os", "get", "Version", "/value")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Version=") {
				version := strings.TrimPrefix(line, "Version=")
				// Extract major version (e.g., "10" from "10.0.19041")
				parts := strings.Split(version, ".")
				if len(parts) >= 1 && parts[0] != "" {
					return parts[0]
				}
			}
		}
	}

	// Try using ver command
	cmd = exec.Command("cmd", "/c", "ver")
	output, err = cmd.Output()
	if err == nil {
		outStr := strings.TrimSpace(string(output))
		// Parse something like "Microsoft Windows [Version 10.0.19041.1]"
		if idx := strings.Index(outStr, "Version "); idx >= 0 {
			version := outStr[idx+8:]
			if endIdx := strings.Index(version, "]"); endIdx >= 0 {
				version = version[:endIdx]
			}
			parts := strings.Split(version, ".")
			if len(parts) >= 1 && parts[0] != "" {
				return parts[0]
			}
		}
	}

	return "unknown"
}

// ResetOSSummaryCache resets the cached OS summary.
// This is primarily for testing purposes.
func ResetOSSummaryCache() {
	osSummaryOnce = sync.Once{}
	cachedOSSummary = nil
}
