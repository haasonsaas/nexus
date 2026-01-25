// Package daemon provides service configuration auditing for nexus gateway services.
// Inspired by Clawdbot's service-audit.ts pattern for comprehensive configuration validation.
package daemon

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// ServiceConfigIssue represents a detected configuration problem.
type ServiceConfigIssue struct {
	Code    string // e.g., "gateway-runtime-bun", "systemd-restart-sec"
	Message string
	Detail  string
	Level   string // "recommended" or "aggressive"
}

// ServiceConfigAudit is the result of auditing service configuration.
type ServiceConfigAudit struct {
	OK     bool
	Issues []ServiceConfigIssue
}

// Audit codes for various configuration issues.
const (
	AuditCodeGatewayCommandMissing        = "gateway-command-missing"
	AuditCodeGatewayEntrypointMismatch    = "gateway-entrypoint-mismatch"
	AuditCodeGatewayPathMissing           = "gateway-path-missing"
	AuditCodeGatewayPathMissingDirs       = "gateway-path-missing-dirs"
	AuditCodeGatewayPathNonMinimal        = "gateway-path-nonminimal"
	AuditCodeGatewayRuntimeBun            = "gateway-runtime-bun"
	AuditCodeGatewayRuntimeVersionManager = "gateway-runtime-node-version-manager"
	AuditCodeGatewayRuntimeNodeMissing    = "gateway-runtime-node-system-missing"
	AuditCodeLaunchdKeepAlive             = "launchd-keep-alive"
	AuditCodeLaunchdRunAtLoad             = "launchd-run-at-load"
	AuditCodeSystemdAfterNetwork          = "systemd-after-network-online"
	AuditCodeSystemdRestartSec            = "systemd-restart-sec"
	AuditCodeSystemdWantsNetwork          = "systemd-wants-network-online"
)

// Issue severity levels.
const (
	LevelRecommended = "recommended"
	LevelAggressive  = "aggressive"
)

// AuditParams contains parameters for service configuration auditing.
type AuditParams struct {
	Env      map[string]string
	Command  *ServiceCommand
	Platform string // "darwin", "linux", "windows"
}

// ServiceCommand represents the service execution configuration.
type ServiceCommand struct {
	ProgramArguments []string
	WorkingDirectory string
	Environment      map[string]string
	SourcePath       string // path to the service file (plist/unit file)
}

// Version manager path patterns to detect.
var versionManagerPatterns = []string{
	".nvm",
	".fnm",
	".volta",
	".asdf",
	".n",
	".nodenv",
	"pnpm",
}

// isNodeRuntime checks if the given executable path is a Node.js runtime.
func isNodeRuntime(execPath string) bool {
	base := pathBase(execPath)
	return base == "node" || base == "node.exe"
}

// isBunRuntime checks if the given executable path is a Bun runtime.
func isBunRuntime(execPath string) bool {
	base := pathBase(execPath)
	return base == "bun" || base == "bun.exe"
}

// pathBase extracts the base name from a path, handling both Unix and Windows paths.
func pathBase(path string) string {
	// Handle Windows paths on any platform
	if idx := strings.LastIndex(path, "\\"); idx >= 0 {
		return path[idx+1:]
	}
	return filepath.Base(path)
}

// isVersionManagedPath checks if the path contains a version manager directory.
func isVersionManagedPath(execPath string) bool {
	normalizedPath := filepath.ToSlash(execPath)
	for _, pattern := range versionManagerPatterns {
		if strings.Contains(normalizedPath, "/"+pattern+"/") ||
			strings.Contains(normalizedPath, "/"+pattern) ||
			strings.HasPrefix(normalizedPath, pattern+"/") {
			return true
		}
	}
	return false
}

// getMinimalServicePathParts returns the minimal required PATH entries for the service.
func getMinimalServicePathParts(env map[string]string, platform string) []string {
	home := env["HOME"]
	if home == "" {
		home = os.Getenv("HOME")
	}

	if platform == "" {
		platform = runtime.GOOS
	}

	switch platform {
	case "darwin":
		parts := []string{
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
		}
		// Add Homebrew paths for ARM and Intel Macs
		if home != "" {
			parts = append([]string{
				filepath.Join(home, ".local", "bin"),
			}, parts...)
		}
		parts = append([]string{"/opt/homebrew/bin"}, parts...)
		return parts
	case "linux":
		parts := []string{
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
		}
		if home != "" {
			parts = append([]string{
				filepath.Join(home, ".local", "bin"),
			}, parts...)
		}
		return parts
	default:
		return []string{
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
		}
	}
}

// NeedsRuntimeMigration returns true if issues indicate runtime migration is needed.
func NeedsRuntimeMigration(issues []ServiceConfigIssue) bool {
	for _, issue := range issues {
		switch issue.Code {
		case AuditCodeGatewayRuntimeBun,
			AuditCodeGatewayRuntimeVersionManager,
			AuditCodeGatewayRuntimeNodeMissing:
			return true
		}
	}
	return false
}

// AuditGatewayServiceConfig performs a comprehensive audit of service configuration.
func AuditGatewayServiceConfig(params AuditParams) (*ServiceConfigAudit, error) {
	var issues []ServiceConfigIssue

	// Run all audit checks
	issues = append(issues, auditGatewayCommand(params)...)

	if params.Platform != "windows" {
		issues = append(issues, auditGatewayServicePath(params)...)
	}

	issues = append(issues, auditGatewayRuntime(params)...)

	// Platform-specific audits
	switch params.Platform {
	case "linux":
		if params.Command != nil && params.Command.SourcePath != "" {
			unitIssues, err := auditSystemdUnit(params.Command.SourcePath)
			if err == nil {
				issues = append(issues, unitIssues...)
			}
		}
	case "darwin":
		if params.Command != nil && params.Command.SourcePath != "" {
			plistIssues, err := auditLaunchdPlist(params.Command.SourcePath)
			if err == nil {
				issues = append(issues, plistIssues...)
			}
		}
	}

	return &ServiceConfigAudit{
		OK:     len(issues) == 0,
		Issues: issues,
	}, nil
}

// auditGatewayCommand checks if the service command includes the gateway subcommand.
func auditGatewayCommand(params AuditParams) []ServiceConfigIssue {
	var issues []ServiceConfigIssue

	if params.Command == nil || len(params.Command.ProgramArguments) == 0 {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeGatewayCommandMissing,
			Message: "Service command is not configured",
			Detail:  "No program arguments found in service configuration",
			Level:   LevelRecommended,
		})
		return issues
	}

	hasGatewayArg := false
	for _, arg := range params.Command.ProgramArguments {
		if arg == "gateway" || arg == "serve" {
			hasGatewayArg = true
			break
		}
	}

	if !hasGatewayArg {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeGatewayCommandMissing,
			Message: "Gateway subcommand not found in service arguments",
			Detail:  "Expected 'gateway' or 'serve' subcommand in: " + strings.Join(params.Command.ProgramArguments, " "),
			Level:   LevelRecommended,
		})
	}

	return issues
}

// auditGatewayServicePath checks the PATH environment configuration.
func auditGatewayServicePath(params AuditParams) []ServiceConfigIssue {
	var issues []ServiceConfigIssue

	// Check for PATH in service environment
	servicePath := ""
	if params.Command != nil && params.Command.Environment != nil {
		servicePath = params.Command.Environment["PATH"]
	}
	if servicePath == "" && params.Env != nil {
		servicePath = params.Env["PATH"]
	}

	if servicePath == "" {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeGatewayPathMissing,
			Message: "PATH environment variable is not set",
			Detail:  "Service may not find required executables without PATH configuration",
			Level:   LevelRecommended,
		})
		return issues
	}

	pathParts := filepath.SplitList(servicePath)
	minimalParts := getMinimalServicePathParts(params.Env, params.Platform)

	// Check for missing required directories
	var missingDirs []string
	for _, required := range minimalParts {
		found := false
		for _, part := range pathParts {
			if part == required {
				found = true
				break
			}
		}
		if !found {
			// Check if directory exists before flagging
			if _, err := os.Stat(required); err == nil {
				missingDirs = append(missingDirs, required)
			}
		}
	}

	if len(missingDirs) > 0 {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeGatewayPathMissingDirs,
			Message: "PATH is missing recommended directories",
			Detail:  "Missing: " + strings.Join(missingDirs, ", "),
			Level:   LevelRecommended,
		})
	}

	// Check for version manager paths (non-minimal)
	for _, part := range pathParts {
		if isVersionManagedPath(part) {
			issues = append(issues, ServiceConfigIssue{
				Code:    AuditCodeGatewayPathNonMinimal,
				Message: "PATH contains version manager directory",
				Detail:  "Found version manager path: " + part + ". This may cause issues with service stability.",
				Level:   LevelAggressive,
			})
		}
	}

	return issues
}

// auditGatewayRuntime checks the runtime configuration for potential issues.
func auditGatewayRuntime(params AuditParams) []ServiceConfigIssue {
	var issues []ServiceConfigIssue

	if params.Command == nil || len(params.Command.ProgramArguments) == 0 {
		return issues
	}

	execPath := params.Command.ProgramArguments[0]

	// Check for Bun runtime
	if isBunRuntime(execPath) {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeGatewayRuntimeBun,
			Message: "Service is using Bun runtime",
			Detail:  "Bun may not be compatible with all channels. Consider using Node.js instead.",
			Level:   LevelRecommended,
		})
	}

	// Check for version-managed Node
	if isNodeRuntime(execPath) && isVersionManagedPath(execPath) {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeGatewayRuntimeVersionManager,
			Message: "Service is using version-managed Node.js",
			Detail:  "Path: " + execPath + ". Version managers may not work reliably in service context.",
			Level:   LevelRecommended,
		})
	}

	// Check if system Node is available when using Node runtime
	if isNodeRuntime(execPath) && !isVersionManagedPath(execPath) {
		// Check if the Node executable exists
		if _, err := os.Stat(execPath); os.IsNotExist(err) {
			issues = append(issues, ServiceConfigIssue{
				Code:    AuditCodeGatewayRuntimeNodeMissing,
				Message: "System Node.js not found",
				Detail:  "Expected Node.js at: " + execPath,
				Level:   LevelRecommended,
			})
		}
	}

	return issues
}

// auditSystemdUnit parses a systemd unit file and checks for configuration issues.
func auditSystemdUnit(unitPath string) ([]ServiceConfigIssue, error) {
	var issues []ServiceConfigIssue

	file, err := os.Open(unitPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var hasAfterNetwork, hasWantsNetwork bool
	var restartSec float64 = -1

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Parse After directive
		if strings.HasPrefix(line, "After=") {
			value := strings.TrimPrefix(line, "After=")
			targets := strings.Fields(value)
			for _, target := range targets {
				if target == "network-online.target" {
					hasAfterNetwork = true
				}
			}
		}

		// Parse Wants directive
		if strings.HasPrefix(line, "Wants=") {
			value := strings.TrimPrefix(line, "Wants=")
			targets := strings.Fields(value)
			for _, target := range targets {
				if target == "network-online.target" {
					hasWantsNetwork = true
				}
			}
		}

		// Parse RestartSec directive
		if strings.HasPrefix(line, "RestartSec=") {
			value := strings.TrimPrefix(line, "RestartSec=")
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				restartSec = parsed
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Check for After=network-online.target
	if !hasAfterNetwork {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeSystemdAfterNetwork,
			Message: "Missing After=network-online.target",
			Detail:  "Service may start before network is fully available",
			Level:   LevelRecommended,
		})
	}

	// Check for Wants=network-online.target
	if !hasWantsNetwork {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeSystemdWantsNetwork,
			Message: "Missing Wants=network-online.target",
			Detail:  "Service should declare dependency on network-online.target",
			Level:   LevelRecommended,
		})
	}

	// Check RestartSec (approximately 5 seconds is recommended)
	if restartSec < 0 {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeSystemdRestartSec,
			Message: "RestartSec not configured",
			Detail:  "Recommended to set RestartSec=5 for graceful restart handling",
			Level:   LevelRecommended,
		})
	} else if restartSec < 3 || restartSec > 10 {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeSystemdRestartSec,
			Message: "RestartSec value may not be optimal",
			Detail:  "Current value: " + strconv.FormatFloat(restartSec, 'f', -1, 64) + "s. Recommended: ~5s",
			Level:   LevelRecommended,
		})
	}

	return issues, nil
}

// auditLaunchdPlist parses a launchd plist and checks for configuration issues.
func auditLaunchdPlist(plistPath string) ([]ServiceConfigIssue, error) {
	var issues []ServiceConfigIssue

	content, err := os.ReadFile(plistPath)
	if err != nil {
		return nil, err
	}

	plistStr := string(content)

	// Check for RunAtLoad
	runAtLoadPattern := regexp.MustCompile(`<key>RunAtLoad</key>\s*<(true|false)/>`)
	runAtLoadMatch := runAtLoadPattern.FindStringSubmatch(plistStr)

	if runAtLoadMatch == nil {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeLaunchdRunAtLoad,
			Message: "RunAtLoad not configured",
			Detail:  "Service will not start automatically on login",
			Level:   LevelRecommended,
		})
	} else if runAtLoadMatch[1] == "false" {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeLaunchdRunAtLoad,
			Message: "RunAtLoad is set to false",
			Detail:  "Service will not start automatically on login",
			Level:   LevelRecommended,
		})
	}

	// Check for KeepAlive
	keepAlivePattern := regexp.MustCompile(`<key>KeepAlive</key>\s*<(true|false)/>`)
	keepAliveMatch := keepAlivePattern.FindStringSubmatch(plistStr)

	if keepAliveMatch == nil {
		// KeepAlive can also be a dict, check for that
		keepAliveDictPattern := regexp.MustCompile(`<key>KeepAlive</key>\s*<dict>`)
		if !keepAliveDictPattern.MatchString(plistStr) {
			issues = append(issues, ServiceConfigIssue{
				Code:    AuditCodeLaunchdKeepAlive,
				Message: "KeepAlive not configured",
				Detail:  "Service will not restart automatically if it crashes",
				Level:   LevelRecommended,
			})
		}
	} else if keepAliveMatch[1] == "false" {
		issues = append(issues, ServiceConfigIssue{
			Code:    AuditCodeLaunchdKeepAlive,
			Message: "KeepAlive is set to false",
			Detail:  "Service will not restart automatically if it crashes",
			Level:   LevelRecommended,
		})
	}

	return issues, nil
}
