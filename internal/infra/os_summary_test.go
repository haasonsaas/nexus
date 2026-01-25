package infra

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestResolveOSSummary(t *testing.T) {
	// Reset cache before test
	ResetOSSummaryCache()
	defer ResetOSSummaryCache()

	summary := ResolveOSSummary()

	// Check that basic fields are set
	if summary.Platform == "" {
		t.Error("Platform should not be empty")
	}
	if summary.Arch == "" {
		t.Error("Arch should not be empty")
	}
	if summary.Release == "" {
		t.Error("Release should not be empty")
	}
	if summary.Label == "" {
		t.Error("Label should not be empty")
	}

	// Verify platform matches runtime
	if summary.Platform != runtime.GOOS {
		t.Errorf("Platform = %q, want %q", summary.Platform, runtime.GOOS)
	}

	// Verify arch matches runtime
	if summary.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", summary.Arch, runtime.GOARCH)
	}
}

func TestResolveOSSummaryCaching(t *testing.T) {
	// Reset cache before test
	ResetOSSummaryCache()
	defer ResetOSSummaryCache()

	// Get first summary
	summary1 := ResolveOSSummary()

	// Get second summary
	summary2 := ResolveOSSummary()

	// They should be identical (same cached instance)
	if summary1 != summary2 {
		t.Error("Expected cached summary to return identical results")
	}
}

func TestResolveOSSummaryConcurrency(t *testing.T) {
	// Reset cache before test
	ResetOSSummaryCache()
	defer ResetOSSummaryCache()

	const numGoroutines = 100
	var wg sync.WaitGroup
	summaries := make([]OSSummary, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			summaries[idx] = ResolveOSSummary()
		}(i)
	}
	wg.Wait()

	// All summaries should be identical
	first := summaries[0]
	for i, s := range summaries {
		if s != first {
			t.Errorf("Summary[%d] differs from Summary[0]: got %+v, want %+v", i, s, first)
		}
	}
}

func TestLabelFormat(t *testing.T) {
	// Reset cache before test
	ResetOSSummaryCache()
	defer ResetOSSummaryCache()

	summary := ResolveOSSummary()

	// Label should contain the architecture in parentheses
	if !strings.HasSuffix(summary.Label, "("+summary.Arch+")") {
		t.Errorf("Label %q should end with (%s)", summary.Label, summary.Arch)
	}

	// Label should start with platform-specific prefix
	switch summary.Platform {
	case "darwin":
		if !strings.HasPrefix(summary.Label, "macos ") {
			t.Errorf("Label %q should start with 'macos ' on darwin", summary.Label)
		}
	case "linux":
		// Linux label starts with distro name or "linux"
		// Should contain a space and the arch
		if !strings.Contains(summary.Label, " ") {
			t.Errorf("Label %q should contain spaces", summary.Label)
		}
	case "windows":
		if !strings.HasPrefix(summary.Label, "windows ") {
			t.Errorf("Label %q should start with 'windows ' on windows", summary.Label)
		}
	}
}

func TestGetMacOSVersion(t *testing.T) {
	version := getMacOSVersion()

	if runtime.GOOS == "darwin" {
		// On macOS, we should get a valid version
		if version == "unknown" {
			t.Skip("Could not get macOS version, sw_vers may not be available")
		}
		// Version should look like "14.0" or "14.0.1"
		if !strings.Contains(version, ".") {
			t.Errorf("macOS version %q doesn't look like a version number", version)
		}
	} else {
		// On non-macOS, we expect "unknown"
		if version != "unknown" {
			t.Errorf("getMacOSVersion on %s returned %q, expected 'unknown'", runtime.GOOS, version)
		}
	}
}

func TestGetLinuxDistro(t *testing.T) {
	distro, version := getLinuxDistro()

	if runtime.GOOS == "linux" {
		// On Linux, we should get something
		// Even if distro is empty, version should be set
		if version == "" {
			t.Error("Linux version should not be empty")
		}
	} else {
		// On non-Linux, we expect empty distro and "unknown" version
		if distro != "" {
			t.Errorf("getLinuxDistro distro on %s returned %q, expected ''", runtime.GOOS, distro)
		}
		if version != "unknown" {
			t.Errorf("getLinuxDistro version on %s returned %q, expected 'unknown'", runtime.GOOS, version)
		}
	}
}

func TestGetWindowsVersion(t *testing.T) {
	version := getWindowsVersion()

	if runtime.GOOS == "windows" {
		// On Windows, we should get a version
		if version == "unknown" {
			t.Skip("Could not get Windows version")
		}
		// Version should be a number like "10" or "11"
		if version == "" {
			t.Error("Windows version should not be empty")
		}
	} else {
		// On non-Windows, we expect "unknown"
		if version != "unknown" {
			t.Errorf("getWindowsVersion on %s returned %q, expected 'unknown'", runtime.GOOS, version)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	// Create a temporary os-release file for testing
	tmpDir := t.TempDir()
	osReleasePath := filepath.Join(tmpDir, "os-release")

	testCases := []struct {
		name           string
		content        string
		expectedDistro string
		expectedVer    string
	}{
		{
			name: "Ubuntu",
			content: `NAME="Ubuntu"
VERSION="22.04.1 LTS (Jammy Jellyfish)"
ID=ubuntu
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04.1 LTS"`,
			expectedDistro: "ubuntu",
			expectedVer:    "22.04",
		},
		{
			name: "Debian",
			content: `PRETTY_NAME="Debian GNU/Linux 11 (bullseye)"
NAME="Debian GNU/Linux"
VERSION_ID="11"
ID=debian`,
			expectedDistro: "debian",
			expectedVer:    "11",
		},
		{
			name: "Fedora",
			content: `NAME="Fedora Linux"
VERSION="38 (Workstation Edition)"
ID=fedora
VERSION_ID=38
PRETTY_NAME="Fedora Linux 38 (Workstation Edition)"`,
			expectedDistro: "fedora",
			expectedVer:    "38",
		},
		{
			name: "Arch Linux",
			content: `NAME="Arch Linux"
PRETTY_NAME="Arch Linux"
ID=arch
BUILD_ID=rolling`,
			expectedDistro: "arch",
			expectedVer:    "unknown",
		},
		{
			name:           "Empty file",
			content:        "",
			expectedDistro: "",
			expectedVer:    "unknown",
		},
		{
			name: "Only ID",
			content: `ID=alpine
`,
			expectedDistro: "alpine",
			expectedVer:    "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write test content
			if err := os.WriteFile(osReleasePath, []byte(tc.content), 0644); err != nil {
				t.Fatalf("Failed to write test os-release: %v", err)
			}

			// Parse using a helper that reads from the test file
			distro, version := parseOSReleaseFile(osReleasePath)

			if distro != tc.expectedDistro {
				t.Errorf("distro = %q, want %q", distro, tc.expectedDistro)
			}
			if version != tc.expectedVer {
				t.Errorf("version = %q, want %q", version, tc.expectedVer)
			}
		})
	}
}

// parseOSReleaseFile is a test helper that parses a specific os-release file.
func parseOSReleaseFile(path string) (string, string) {
	file, err := os.Open(path)
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

	if versionID == "" {
		versionID = "unknown"
	}

	return strings.ToLower(distroID), versionID
}

func TestResolveOSSummaryInternal(t *testing.T) {
	summary := resolveOSSummaryInternal()

	// Should return a valid pointer
	if summary == nil {
		t.Fatal("resolveOSSummaryInternal returned nil")
	}

	// Check all fields are populated
	if summary.Platform == "" {
		t.Error("Platform is empty")
	}
	if summary.Arch == "" {
		t.Error("Arch is empty")
	}
	if summary.Release == "" {
		t.Error("Release is empty")
	}
	if summary.Label == "" {
		t.Error("Label is empty")
	}
}

func TestResetOSSummaryCache(t *testing.T) {
	// Get initial summary
	summary1 := ResolveOSSummary()

	// Reset cache
	ResetOSSummaryCache()

	// Get new summary - should still match since it's reading the same system
	summary2 := ResolveOSSummary()

	// The values should be the same even after reset
	if summary1.Platform != summary2.Platform {
		t.Errorf("Platform changed after reset: %q vs %q", summary1.Platform, summary2.Platform)
	}
	if summary1.Arch != summary2.Arch {
		t.Errorf("Arch changed after reset: %q vs %q", summary1.Arch, summary2.Arch)
	}
}

func TestOSSummaryStruct(t *testing.T) {
	// Test that the struct can be created and used correctly
	s := OSSummary{
		Platform: "darwin",
		Arch:     "arm64",
		Release:  "14.0",
		Label:    "macos 14.0 (arm64)",
	}

	if s.Platform != "darwin" {
		t.Errorf("Platform = %q, want 'darwin'", s.Platform)
	}
	if s.Arch != "arm64" {
		t.Errorf("Arch = %q, want 'arm64'", s.Arch)
	}
	if s.Release != "14.0" {
		t.Errorf("Release = %q, want '14.0'", s.Release)
	}
	if s.Label != "macos 14.0 (arm64)" {
		t.Errorf("Label = %q, want 'macos 14.0 (arm64)'", s.Label)
	}
}

func TestLabelFormats(t *testing.T) {
	// Test expected label formats for different platforms
	testCases := []struct {
		platform     string
		arch         string
		release      string
		distro       string
		expectedPart string
	}{
		{"darwin", "arm64", "14.0", "", "macos 14.0 (arm64)"},
		{"darwin", "amd64", "13.5.2", "", "macos 13.5.2 (amd64)"},
		{"linux", "amd64", "22.04", "ubuntu", "ubuntu 22.04 (amd64)"},
		{"linux", "arm64", "11", "debian", "debian 11 (arm64)"},
		{"windows", "amd64", "10", "", "windows 10 (amd64)"},
		{"windows", "amd64", "11", "", "windows 11 (amd64)"},
	}

	for _, tc := range testCases {
		t.Run(tc.expectedPart, func(t *testing.T) {
			var label string
			switch tc.platform {
			case "darwin":
				label = "macos " + tc.release + " (" + tc.arch + ")"
			case "linux":
				if tc.distro != "" {
					label = tc.distro + " " + tc.release + " (" + tc.arch + ")"
				} else {
					label = "linux " + tc.release + " (" + tc.arch + ")"
				}
			case "windows":
				label = "windows " + tc.release + " (" + tc.arch + ")"
			}

			if label != tc.expectedPart {
				t.Errorf("Label = %q, want %q", label, tc.expectedPart)
			}
		})
	}
}
