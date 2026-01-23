// Package main provides the nexus-edge daemon.
//
// node_tools.go implements node-specific tools for device access:
// - camera_snap: Take photos with the device camera
// - screen_capture: Capture screenshots
// - location_get: Get current GPS location
// - shell_run: Execute shell commands
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// RegisterNodeTools registers all node-specific tools with the daemon.
func RegisterNodeTools(daemon *EdgeDaemon) {
	daemon.RegisterTool(cameraSnapTool())
	daemon.RegisterTool(screenCaptureTool())
	daemon.RegisterTool(locationGetTool())
	daemon.RegisterTool(shellRunTool())
}

// cameraSnapTool takes a photo using the device camera.
func cameraSnapTool() *Tool {
	return &Tool{
		Name:        "nodes.camera_snap",
		Description: "Take a photo using the device camera. On macOS, uses imagesnap or ffmpeg. Returns the captured image.",
		InputSchema: `{
			"type": "object",
			"properties": {
				"device": {
					"type": "string",
					"description": "Camera device name (optional, uses default if not specified)"
				},
				"warmup_seconds": {
					"type": "number",
					"description": "Seconds to wait for camera warmup (default: 1)",
					"default": 1
				}
			}
		}`,
		RequiresApproval:  true,
		TimeoutSeconds:    30,
		ProducesArtifacts: true,
		Handler:           handleCameraSnap,
	}
}

func handleCameraSnap(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		Device        string  `json:"device"`
		WarmupSeconds float64 `json:"warmup_seconds"`
	}
	params.WarmupSeconds = 1.0 // default
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Create temp file for output
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("nexus_camera_%s.jpg", uuid.NewString()[:8]))
	defer os.Remove(tmpFile)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// Try imagesnap first (brew install imagesnap)
		if _, err := exec.LookPath("imagesnap"); err == nil {
			args := []string{"-w", fmt.Sprintf("%.1f", params.WarmupSeconds), tmpFile}
			if params.Device != "" {
				args = append([]string{"-d", params.Device}, args...)
			}
			cmd = exec.CommandContext(ctx, "imagesnap", args...)
		} else if _, err := exec.LookPath("ffmpeg"); err == nil {
			// Fallback to ffmpeg
			device := params.Device
			if device == "" {
				device = "0" // default device index
			}
			cmd = exec.CommandContext(ctx, "ffmpeg",
				"-f", "avfoundation",
				"-framerate", "30",
				"-i", device,
				"-frames:v", "1",
				"-y", tmpFile,
			)
		} else {
			return &ToolResult{
				Content: "Camera capture requires 'imagesnap' or 'ffmpeg'. Install with: brew install imagesnap",
				IsError: true,
			}, nil
		}

	case "linux":
		// Try fswebcam or ffmpeg
		if _, err := exec.LookPath("fswebcam"); err == nil {
			args := []string{"-r", "1280x720", "--jpeg", "85", "-D", fmt.Sprintf("%.0f", params.WarmupSeconds), tmpFile}
			if params.Device != "" {
				args = append([]string{"-d", params.Device}, args...)
			}
			cmd = exec.CommandContext(ctx, "fswebcam", args...)
		} else if _, err := exec.LookPath("ffmpeg"); err == nil {
			device := params.Device
			if device == "" {
				device = "/dev/video0"
			}
			cmd = exec.CommandContext(ctx, "ffmpeg",
				"-f", "v4l2",
				"-i", device,
				"-frames:v", "1",
				"-y", tmpFile,
			)
		} else {
			return &ToolResult{
				Content: "Camera capture requires 'fswebcam' or 'ffmpeg'. Install with: sudo apt install fswebcam",
				IsError: true,
			}, nil
		}

	default:
		return &ToolResult{
			Content: fmt.Sprintf("Camera capture not supported on %s", runtime.GOOS),
			IsError: true,
		}, nil
	}

	// Execute capture command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Camera capture failed: %v\nOutput: %s", err, string(output)),
			IsError: true,
		}, nil
	}

	// Read captured image
	imageData, err := os.ReadFile(tmpFile)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to read captured image: %v", err),
			IsError: true,
		}, nil
	}

	filename := fmt.Sprintf("camera_%s.jpg", time.Now().Format("20060102_150405"))

	return &ToolResult{
		Content: fmt.Sprintf("Photo captured: %s (%d bytes)", filename, len(imageData)),
		Artifacts: []*pb.Artifact{
			{
				Id:         uuid.NewString(),
				Type:       "screenshot",
				MimeType:   "image/jpeg",
				Filename:   filename,
				Size:       int64(len(imageData)),
				Data:       imageData,
				TtlSeconds: 86400, // 24 hours
			},
		},
	}, nil
}

// screenCaptureTool captures a screenshot.
func screenCaptureTool() *Tool {
	return &Tool{
		Name:        "nodes.screen_capture",
		Description: "Capture a screenshot of the screen. On macOS uses screencapture, on Linux uses scrot or gnome-screenshot.",
		InputSchema: `{
			"type": "object",
			"properties": {
				"display": {
					"type": "integer",
					"description": "Display number to capture (optional, captures main display if not specified)"
				},
				"region": {
					"type": "object",
					"description": "Capture a specific region",
					"properties": {
						"x": {"type": "integer"},
						"y": {"type": "integer"},
						"width": {"type": "integer"},
						"height": {"type": "integer"}
					}
				},
				"window_name": {
					"type": "string",
					"description": "Capture a specific window by name (macOS only)"
				}
			}
		}`,
		RequiresApproval:  true,
		TimeoutSeconds:    15,
		ProducesArtifacts: true,
		Handler:           handleScreenCapture,
	}
}

func handleScreenCapture(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		Display    int    `json:"display"`
		WindowName string `json:"window_name"`
		Region     *struct {
			X      int `json:"x"`
			Y      int `json:"y"`
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"region"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("nexus_screen_%s.png", uuid.NewString()[:8]))
	defer os.Remove(tmpFile)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		args := []string{"-x", tmpFile} // -x: no sound
		if params.Display > 0 {
			args = append(args, "-D", fmt.Sprintf("%d", params.Display))
		}
		if params.Region != nil {
			args = append(args, "-R",
				fmt.Sprintf("%d,%d,%d,%d", params.Region.X, params.Region.Y, params.Region.Width, params.Region.Height))
		}
		// Window-name capture is not supported yet; fall back to full screen capture.
		cmd = exec.CommandContext(ctx, "screencapture", args...)

	case "linux":
		if _, err := exec.LookPath("scrot"); err == nil {
			args := []string{tmpFile}
			if params.Region != nil {
				args = append(args, "-a",
					fmt.Sprintf("%d,%d,%d,%d", params.Region.X, params.Region.Y, params.Region.Width, params.Region.Height))
			}
			cmd = exec.CommandContext(ctx, "scrot", args...)
		} else if _, err := exec.LookPath("gnome-screenshot"); err == nil {
			args := []string{"-f", tmpFile}
			cmd = exec.CommandContext(ctx, "gnome-screenshot", args...)
		} else if _, err := exec.LookPath("import"); err == nil {
			// ImageMagick import
			args := []string{"-window", "root", tmpFile}
			cmd = exec.CommandContext(ctx, "import", args...)
		} else {
			return &ToolResult{
				Content: "Screenshot requires 'scrot', 'gnome-screenshot', or 'imagemagick'. Install with: sudo apt install scrot",
				IsError: true,
			}, nil
		}

	default:
		return &ToolResult{
			Content: fmt.Sprintf("Screenshot not supported on %s", runtime.GOOS),
			IsError: true,
		}, nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Screenshot failed: %v\nOutput: %s", err, string(output)),
			IsError: true,
		}, nil
	}

	imageData, err := os.ReadFile(tmpFile)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to read screenshot: %v", err),
			IsError: true,
		}, nil
	}

	filename := fmt.Sprintf("screenshot_%s.png", time.Now().Format("20060102_150405"))

	return &ToolResult{
		Content: fmt.Sprintf("Screenshot captured: %s (%d bytes)", filename, len(imageData)),
		Artifacts: []*pb.Artifact{
			{
				Id:         uuid.NewString(),
				Type:       "screenshot",
				MimeType:   "image/png",
				Filename:   filename,
				Size:       int64(len(imageData)),
				Data:       imageData,
				TtlSeconds: 86400,
			},
		},
	}, nil
}

// locationGetTool gets the current GPS location.
func locationGetTool() *Tool {
	return &Tool{
		Name:        "nodes.location_get",
		Description: "Get the current GPS location of the device. On macOS uses CoreLocation, on Linux may require gpsd.",
		InputSchema: `{
			"type": "object",
			"properties": {
				"timeout_seconds": {
					"type": "integer",
					"description": "Timeout for location acquisition in seconds (default: 10)",
					"default": 10
				},
				"accuracy": {
					"type": "string",
					"description": "Desired accuracy: 'best', 'high', 'medium', 'low'",
					"default": "high"
				}
			}
		}`,
		RequiresApproval: true,
		TimeoutSeconds:   30,
		Handler:          handleLocationGet,
	}
}

func handleLocationGet(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		TimeoutSeconds int    `json:"timeout_seconds"`
		Accuracy       string `json:"accuracy"`
	}
	params.TimeoutSeconds = 10
	params.Accuracy = "high"
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		// Use Swift/CoreLocation via osascript or a helper tool
		// This is a simplified approach using whereami if available
		if _, err := exec.LookPath("whereami"); err == nil {
			cmd := exec.CommandContext(ctx, "whereami")
			output, err := cmd.Output()
			if err != nil {
				return &ToolResult{
					Content: fmt.Sprintf("Location query failed: %v", err),
					IsError: true,
				}, nil
			}

			// Parse whereami output
			lines := strings.Split(string(output), "\n")
			result := make(map[string]string)
			for _, line := range lines {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					result[key] = val
				}
			}

			jsonResult, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return &ToolResult{
					Content: fmt.Sprintf("Failed to encode location result: %v", err),
					IsError: true,
				}, nil
			}
			return &ToolResult{
				Content: string(jsonResult),
			}, nil
		}

		// Fallback: Use CoreLocation via swift script
		swiftScript := `
import CoreLocation
import Foundation

class LocationDelegate: NSObject, CLLocationManagerDelegate {
    var location: CLLocation?
    let semaphore = DispatchSemaphore(value: 0)

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        location = locations.last
        semaphore.signal()
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        semaphore.signal()
    }
}

let manager = CLLocationManager()
let delegate = LocationDelegate()
manager.delegate = delegate
manager.requestWhenInUseAuthorization()
manager.requestLocation()

_ = delegate.semaphore.wait(timeout: .now() + 10)

if let loc = delegate.location {
    print("{\"latitude\":\(loc.coordinate.latitude),\"longitude\":\(loc.coordinate.longitude),\"accuracy\":\(loc.horizontalAccuracy)}")
} else {
    print("{\"error\":\"Location unavailable\"}")
}
`
		tmpFile := filepath.Join(os.TempDir(), "nexus_location.swift")
		if err := os.WriteFile(tmpFile, []byte(swiftScript), 0600); err != nil {
			return &ToolResult{
				Content: "Failed to create location script",
				IsError: true,
			}, nil
		}
		defer os.Remove(tmpFile)

		cmd := exec.CommandContext(ctx, "swift", tmpFile)
		output, err := cmd.Output()
		if err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Location query failed: %v. Install whereami: brew install whereami", err),
				IsError: true,
			}, nil
		}

		return &ToolResult{
			Content: string(output),
		}, nil

	case "linux":
		// Try gpsd
		if _, err := exec.LookPath("gpspipe"); err == nil {
			cmd := exec.CommandContext(ctx, "gpspipe", "-w", "-n", "1")
			output, err := cmd.Output()
			if err != nil {
				return &ToolResult{
					Content: fmt.Sprintf("GPS query failed: %v", err),
					IsError: true,
				}, nil
			}
			return &ToolResult{
				Content: string(output),
			}, nil
		}

		return &ToolResult{
			Content: "Location requires gpsd. Install with: sudo apt install gpsd gpsd-clients",
			IsError: true,
		}, nil

	default:
		return &ToolResult{
			Content: fmt.Sprintf("Location not supported on %s", runtime.GOOS),
			IsError: true,
		}, nil
	}
}

// shellRunTool executes shell commands.
func shellRunTool() *Tool {
	return &Tool{
		Name:        "nodes.shell_run",
		Description: "Execute a shell command on the device. Returns stdout, stderr, and exit code. Use with caution.",
		InputSchema: `{
			"type": "object",
			"properties": {
				"command": {
					"type": "string",
					"description": "The command to execute"
				},
				"args": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Command arguments"
				},
				"working_dir": {
					"type": "string",
					"description": "Working directory for command execution"
				},
				"env": {
					"type": "object",
					"description": "Environment variables to set",
					"additionalProperties": {"type": "string"}
				},
				"timeout_seconds": {
					"type": "integer",
					"description": "Command timeout in seconds (default: 60)",
					"default": 60
				},
				"capture_output": {
					"type": "boolean",
					"description": "Whether to capture and return output (default: true)",
					"default": true
				}
			},
			"required": ["command"]
		}`,
		RequiresApproval: true,
		TimeoutSeconds:   120,
		Handler:          handleShellRun,
	}
}

func handleShellRun(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		Command        string            `json:"command"`
		Args           []string          `json:"args"`
		WorkingDir     string            `json:"working_dir"`
		Env            map[string]string `json:"env"`
		TimeoutSeconds int               `json:"timeout_seconds"`
		CaptureOutput  *bool             `json:"capture_output"`
	}
	params.TimeoutSeconds = 60
	captureOutput := true
	params.CaptureOutput = &captureOutput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if params.Command == "" {
		return &ToolResult{
			Content: "command is required",
			IsError: true,
		}, nil
	}

	// Create command with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(params.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, params.Command, params.Args...)

	if params.WorkingDir != "" {
		cmd.Dir = params.WorkingDir
	}

	if len(params.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range params.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return &ToolResult{
				Content: fmt.Sprintf("Command execution failed: %v", err),
				IsError: true,
			}, nil
		}
	}

	result := map[string]interface{}{
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
	}

	if *params.CaptureOutput {
		result["stdout"] = stdout.String()
		result["stderr"] = stderr.String()

		// Truncate if too large
		const maxOutputSize = 100000
		if len(stdout.String()) > maxOutputSize {
			result["stdout"] = stdout.String()[:maxOutputSize] + "\n... (truncated)"
			result["stdout_truncated"] = true
		}
		if len(stderr.String()) > maxOutputSize {
			result["stderr"] = stderr.String()[:maxOutputSize] + "\n... (truncated)"
			result["stderr_truncated"] = true
		}
	}

	jsonResult, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to encode command result: %v", err),
			IsError: true,
		}, nil
	}

	isError := exitCode != 0
	return &ToolResult{
		Content: string(jsonResult),
		IsError: isError,
	}, nil
}
