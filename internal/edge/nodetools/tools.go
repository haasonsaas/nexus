// Package nodetools provides edge-executable tools for node capabilities.
//
// These tools enable privileged actions on the local device:
//   - camera_snap: Capture a photo
//   - screen_record: Record screen activity
//   - location_get: Get GPS coordinates
//   - run: Execute shell commands
//
// Each tool is designed to work with the edge daemon's tool execution
// framework and supports artifacts, timeouts, and cancellation.
package nodetools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Tool represents an executable node tool.
type Tool interface {
	// Name returns the tool name (e.g., "camera_snap").
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Schema returns the JSON Schema for parameters.
	Schema() json.RawMessage

	// Execute runs the tool with the given parameters.
	Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)

	// RequiresApproval returns true if this tool needs user approval.
	RequiresApproval() bool

	// ProducesArtifacts returns true if this tool produces artifacts.
	ProducesArtifacts() bool
}

// ToolResult contains the result of a tool execution.
type ToolResult struct {
	// Content is the textual result.
	Content string

	// IsError indicates if the execution failed.
	IsError bool

	// Artifacts produced by the tool.
	Artifacts []Artifact
}

// Artifact represents a file produced by a tool.
type Artifact struct {
	// Type of artifact (screenshot, recording, file).
	Type string

	// MimeType of the content.
	MimeType string

	// Filename suggested.
	Filename string

	// Data is the artifact content.
	Data []byte
}

// Provider manages and provides node tools.
type Provider struct {
	tools  map[string]Tool
	logger *slog.Logger
}

// NewProvider creates a new node tools provider.
// Only enables tools supported on the current platform.
func NewProvider(logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}

	p := &Provider{
		tools:  make(map[string]Tool),
		logger: logger.With("component", "nodetools"),
	}

	// Register platform-appropriate tools
	p.registerPlatformTools()

	return p
}

// registerPlatformTools registers tools available on this platform.
func (p *Provider) registerPlatformTools() {
	// Shell execution is universal
	p.Register(&RunTool{logger: p.logger})

	switch runtime.GOOS {
	case "darwin":
		p.registerMacTools()
	case "linux":
		p.registerLinuxTools()
	case "windows":
		p.registerWindowsTools()
	}
}

func (p *Provider) registerMacTools() {
	p.Register(&CameraSnapTool{logger: p.logger, platform: "darwin"})
	p.Register(&ScreenRecordTool{logger: p.logger, platform: "darwin"})
	p.Register(&LocationGetTool{logger: p.logger, platform: "darwin"})
}

func (p *Provider) registerLinuxTools() {
	p.Register(&CameraSnapTool{logger: p.logger, platform: "linux"})
	p.Register(&ScreenRecordTool{logger: p.logger, platform: "linux"})
	// Location typically not available on Linux desktop
}

func (p *Provider) registerWindowsTools() {
	p.Register(&CameraSnapTool{logger: p.logger, platform: "windows"})
	p.Register(&ScreenRecordTool{logger: p.logger, platform: "windows"})
	// Location via Windows API
}

// Register adds a tool to the provider.
func (p *Provider) Register(tool Tool) {
	p.tools[tool.Name()] = tool
	p.logger.Debug("registered node tool", "name", tool.Name())
}

// GetTools returns all registered tools.
func (p *Provider) GetTools() []Tool {
	result := make([]Tool, 0, len(p.tools))
	for _, t := range p.tools {
		result = append(result, t)
	}
	return result
}

// GetTool returns a specific tool by name.
func (p *Provider) GetTool(name string) (Tool, bool) {
	t, ok := p.tools[name]
	return t, ok
}

// Execute runs a tool by name.
func (p *Provider) Execute(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	tool, ok := p.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool.Execute(ctx, params)
}

// =============================================================================
// Camera Snap Tool
// =============================================================================

// CameraSnapParams are the parameters for camera_snap.
type CameraSnapParams struct {
	// Device is the camera device to use (default: front camera).
	Device string `json:"device,omitempty"`

	// Quality is the image quality (low, medium, high).
	Quality string `json:"quality,omitempty"`
}

// CameraSnapTool takes a photo using the device camera.
type CameraSnapTool struct {
	logger   *slog.Logger
	platform string
}

func (t *CameraSnapTool) Name() string            { return "camera_snap" }
func (t *CameraSnapTool) RequiresApproval() bool  { return true }
func (t *CameraSnapTool) ProducesArtifacts() bool { return true }

func (t *CameraSnapTool) Description() string {
	return "Take a photo using the device camera. Returns the captured image."
}

func (t *CameraSnapTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"device": {
				"type": "string",
				"description": "Camera device (front, back, or device name)",
				"default": "front"
			},
			"quality": {
				"type": "string",
				"enum": ["low", "medium", "high"],
				"default": "medium"
			}
		}
	}`)
}

func (t *CameraSnapTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p CameraSnapParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	switch t.platform {
	case "darwin":
		return t.executeMac(ctx, p)
	case "linux":
		return t.executeLinux(ctx, p)
	case "windows":
		return t.executeWindows(ctx, p)
	default:
		return &ToolResult{Content: "camera not supported on this platform", IsError: true}, nil
	}
}

func (t *CameraSnapTool) executeMac(ctx context.Context, p CameraSnapParams) (*ToolResult, error) {
	// Use imagesnap on macOS (brew install imagesnap)
	args := []string{"-w", "1"} // 1 second warmup

	cmd := exec.CommandContext(ctx, "imagesnap", args...)
	output, err := cmd.Output()
	if err != nil {
		// Check if imagesnap is installed
		if strings.Contains(err.Error(), "executable file not found") {
			return &ToolResult{
				Content: "imagesnap not installed. Install with: brew install imagesnap",
				IsError: true,
			}, nil
		}
		return &ToolResult{Content: fmt.Sprintf("camera capture failed: %v", err), IsError: true}, nil
	}

	return &ToolResult{
		Content: "Photo captured successfully",
		Artifacts: []Artifact{{
			Type:     "screenshot",
			MimeType: "image/jpeg",
			Filename: fmt.Sprintf("camera_%s.jpg", time.Now().Format("20060102_150405")),
			Data:     output,
		}},
	}, nil
}

func (t *CameraSnapTool) executeLinux(ctx context.Context, p CameraSnapParams) (*ToolResult, error) {
	// Use fswebcam on Linux
	cmd := exec.CommandContext(ctx, "fswebcam", "-r", "1280x720", "--jpeg", "85", "-")
	output, err := cmd.Output()
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("camera capture failed: %v", err), IsError: true}, nil
	}

	return &ToolResult{
		Content: "Photo captured successfully",
		Artifacts: []Artifact{{
			Type:     "screenshot",
			MimeType: "image/jpeg",
			Filename: fmt.Sprintf("camera_%s.jpg", time.Now().Format("20060102_150405")),
			Data:     output,
		}},
	}, nil
}

func (t *CameraSnapTool) executeWindows(ctx context.Context, p CameraSnapParams) (*ToolResult, error) {
	// Windows camera capture via PowerShell
	return &ToolResult{Content: "Windows camera capture not yet implemented", IsError: true}, nil
}

// =============================================================================
// Screen Record Tool
// =============================================================================

// ScreenRecordParams are the parameters for screen_record.
type ScreenRecordParams struct {
	// Duration in seconds (default: 5, max: 60).
	Duration int `json:"duration,omitempty"`

	// Format is the output format (gif, mp4).
	Format string `json:"format,omitempty"`

	// Display is which display to capture (default: main).
	Display string `json:"display,omitempty"`
}

// ScreenRecordTool records the screen.
type ScreenRecordTool struct {
	logger   *slog.Logger
	platform string
}

func (t *ScreenRecordTool) Name() string            { return "screen_record" }
func (t *ScreenRecordTool) RequiresApproval() bool  { return true }
func (t *ScreenRecordTool) ProducesArtifacts() bool { return true }

func (t *ScreenRecordTool) Description() string {
	return "Record the screen for a specified duration. Returns the recorded video or GIF."
}

func (t *ScreenRecordTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"duration": {
				"type": "integer",
				"description": "Recording duration in seconds (1-60)",
				"minimum": 1,
				"maximum": 60,
				"default": 5
			},
			"format": {
				"type": "string",
				"enum": ["gif", "mp4"],
				"default": "gif"
			},
			"display": {
				"type": "string",
				"description": "Display to capture (main, 0, 1, etc.)",
				"default": "main"
			}
		}
	}`)
}

func (t *ScreenRecordTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p ScreenRecordParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	// Apply defaults
	if p.Duration <= 0 {
		p.Duration = 5
	}
	if p.Duration > 60 {
		p.Duration = 60
	}
	if p.Format == "" {
		p.Format = "gif"
	}

	switch t.platform {
	case "darwin":
		return t.executeMac(ctx, p)
	case "linux":
		return t.executeLinux(ctx, p)
	default:
		return &ToolResult{Content: "screen recording not supported on this platform", IsError: true}, nil
	}
}

func (t *ScreenRecordTool) executeMac(ctx context.Context, p ScreenRecordParams) (*ToolResult, error) {
	// Use screencapture on macOS for single frame, ffmpeg for video
	if p.Format == "gif" || p.Duration <= 1 {
		// Single screenshot for short durations
		cmd := exec.CommandContext(ctx, "screencapture", "-x", "-t", "png", "-")
		output, err := cmd.Output()
		if err != nil {
			return &ToolResult{Content: fmt.Sprintf("screen capture failed: %v", err), IsError: true}, nil
		}

		return &ToolResult{
			Content: "Screenshot captured",
			Artifacts: []Artifact{{
				Type:     "screenshot",
				MimeType: "image/png",
				Filename: fmt.Sprintf("screen_%s.png", time.Now().Format("20060102_150405")),
				Data:     output,
			}},
		}, nil
	}

	// For longer recordings, would need ffmpeg
	return &ToolResult{Content: "Video recording requires ffmpeg (use format=gif for screenshots)", IsError: true}, nil
}

func (t *ScreenRecordTool) executeLinux(ctx context.Context, p ScreenRecordParams) (*ToolResult, error) {
	// Use import (ImageMagick) or scrot for screenshots
	cmd := exec.CommandContext(ctx, "import", "-window", "root", "png:-")
	output, err := cmd.Output()
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("screen capture failed: %v", err), IsError: true}, nil
	}

	return &ToolResult{
		Content: "Screenshot captured",
		Artifacts: []Artifact{{
			Type:     "screenshot",
			MimeType: "image/png",
			Filename: fmt.Sprintf("screen_%s.png", time.Now().Format("20060102_150405")),
			Data:     output,
		}},
	}, nil
}

// =============================================================================
// Location Get Tool
// =============================================================================

// LocationGetParams are the parameters for location_get.
type LocationGetParams struct {
	// HighAccuracy requests GPS-level accuracy (slower).
	HighAccuracy bool `json:"high_accuracy,omitempty"`
}

// LocationGetTool gets the device location.
type LocationGetTool struct {
	logger   *slog.Logger
	platform string
}

func (t *LocationGetTool) Name() string            { return "location_get" }
func (t *LocationGetTool) RequiresApproval() bool  { return true }
func (t *LocationGetTool) ProducesArtifacts() bool { return false }

func (t *LocationGetTool) Description() string {
	return "Get the current GPS coordinates of the device."
}

func (t *LocationGetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"high_accuracy": {
				"type": "boolean",
				"description": "Request GPS-level accuracy (may take longer)",
				"default": false
			}
		}
	}`)
}

func (t *LocationGetTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p LocationGetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	switch t.platform {
	case "darwin":
		return t.executeMac(ctx, p)
	default:
		return &ToolResult{Content: "location not supported on this platform", IsError: true}, nil
	}
}

func (t *LocationGetTool) executeMac(ctx context.Context, p LocationGetParams) (*ToolResult, error) {
	// Use CoreLocation via a small Swift helper or AppleScript
	// For now, return a placeholder that explains how to get location
	script := `
tell application "System Events"
    -- macOS doesn't expose location via AppleScript directly
    -- Need to use CoreLocation framework
end tell
`
	_ = script

	// In production, this would use a small helper binary that accesses CoreLocation
	return &ToolResult{
		Content: "Location services require a native helper. Location: unavailable (helper not installed)",
		IsError: false,
	}, nil
}

// =============================================================================
// Run Tool
// =============================================================================

// RunParams are the parameters for run.
type RunParams struct {
	// Command is the shell command to execute.
	Command string `json:"command"`

	// WorkingDir is the working directory (default: home).
	WorkingDir string `json:"working_dir,omitempty"`

	// Timeout in seconds (default: 30, max: 300).
	Timeout int `json:"timeout,omitempty"`
}

// RunTool executes shell commands.
type RunTool struct {
	logger *slog.Logger
}

func (t *RunTool) Name() string            { return "run" }
func (t *RunTool) RequiresApproval() bool  { return true }
func (t *RunTool) ProducesArtifacts() bool { return false }

func (t *RunTool) Description() string {
	return "Execute a shell command on the device. Returns stdout/stderr."
}

func (t *RunTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["command"],
		"properties": {
			"command": {
				"type": "string",
				"description": "Shell command to execute"
			},
			"working_dir": {
				"type": "string",
				"description": "Working directory for command execution"
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in seconds (1-300)",
				"minimum": 1,
				"maximum": 300,
				"default": 30
			}
		}
	}`)
}

func (t *RunTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p RunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	if p.Command == "" {
		return &ToolResult{Content: "command is required", IsError: true}, nil
	}

	// Apply timeout
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 300 {
		timeout = 300
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Determine shell
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, shell, "/c", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", p.Command)
	}

	if p.WorkingDir != "" {
		cmd.Dir = p.WorkingDir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check for timeout
		if ctx.Err() == context.DeadlineExceeded {
			return &ToolResult{
				Content: fmt.Sprintf("command timed out after %d seconds\nPartial output:\n%s", timeout, string(output)),
				IsError: true,
			}, nil
		}

		// Command failed but produced output
		return &ToolResult{
			Content: fmt.Sprintf("command failed: %v\nOutput:\n%s", err, string(output)),
			IsError: true,
		}, nil
	}

	return &ToolResult{Content: string(output)}, nil
}
