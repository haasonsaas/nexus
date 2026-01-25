// Package clipboard provides cross-platform clipboard utilities.
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DefaultTimeout is the timeout for each clipboard tool attempt.
const DefaultTimeout = 3 * time.Second

// ErrNoClipboardTool is returned when no clipboard tool is available.
var ErrNoClipboardTool = errors.New("no clipboard tool available")

// ClipboardTool represents a clipboard command with its arguments.
type ClipboardTool struct {
	Name     string   // Name of the command (e.g., "pbcopy")
	Args     []string // Arguments to pass to the command
	Platform string   // Platform restriction: "darwin", "linux", "windows", or "" for any
}

// copyTools contains clipboard tools for writing, in priority order.
// Based on clawdbot's clipboard.ts implementation.
var copyTools = []ClipboardTool{
	{Name: "pbcopy", Args: nil, Platform: "darwin"},
	{Name: "xclip", Args: []string{"-selection", "clipboard"}, Platform: "linux"},
	{Name: "wl-copy", Args: nil, Platform: "linux"},
	{Name: "clip.exe", Args: nil, Platform: ""}, // WSL / Windows - works on both
	{Name: "powershell", Args: []string{"-NoProfile", "-Command", "Set-Clipboard"}, Platform: "windows"},
}

// pasteTools contains clipboard tools for reading, in priority order.
var pasteTools = []ClipboardTool{
	{Name: "pbpaste", Args: nil, Platform: "darwin"},
	{Name: "xclip", Args: []string{"-selection", "clipboard", "-o"}, Platform: "linux"},
	{Name: "wl-paste", Args: nil, Platform: "linux"},
	{Name: "powershell", Args: []string{"-NoProfile", "-Command", "Get-Clipboard"}, Platform: "windows"},
}

// CopyToClipboard copies the given value to the system clipboard.
// It tries multiple clipboard tools in order until one succeeds.
// Returns true if the copy was successful, false if all tools failed.
func CopyToClipboard(value string) (bool, error) {
	return CopyToClipboardWithTimeout(value, DefaultTimeout)
}

// CopyToClipboardWithTimeout copies the given value to the system clipboard with a custom timeout.
func CopyToClipboardWithTimeout(value string, timeout time.Duration) (bool, error) {
	tools := getApplicableTools(copyTools)
	if len(tools) == 0 {
		return false, ErrNoClipboardTool
	}

	for _, tool := range tools {
		if tryCopyTool(tool, value, timeout) {
			return true, nil
		}
	}

	return false, nil
}

// ReadFromClipboard reads the current content from the system clipboard.
// It tries multiple clipboard tools in order until one succeeds.
func ReadFromClipboard() (string, error) {
	return ReadFromClipboardWithTimeout(DefaultTimeout)
}

// ReadFromClipboardWithTimeout reads from the clipboard with a custom timeout.
func ReadFromClipboardWithTimeout(timeout time.Duration) (string, error) {
	tools := getApplicableTools(pasteTools)
	if len(tools) == 0 {
		return "", ErrNoClipboardTool
	}

	for _, tool := range tools {
		content, ok := tryPasteTool(tool, timeout)
		if ok {
			return content, nil
		}
	}

	return "", ErrNoClipboardTool
}

// getApplicableTools filters tools based on the current platform.
func getApplicableTools(tools []ClipboardTool) []ClipboardTool {
	var applicable []ClipboardTool
	currentOS := runtime.GOOS

	for _, tool := range tools {
		if tool.Platform == "" || tool.Platform == currentOS {
			applicable = append(applicable, tool)
		}
	}

	return applicable
}

// tryCopyTool attempts to copy value using the given tool.
// Returns true if successful, false otherwise.
func tryCopyTool(tool ClipboardTool, value string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool.Name, tool.Args...)
	cmd.Stdin = strings.NewReader(value)

	err := cmd.Run()
	if err != nil {
		return false
	}

	// Check if the context was cancelled (timeout)
	if ctx.Err() != nil {
		return false
	}

	return true
}

// tryPasteTool attempts to read from clipboard using the given tool.
// Returns the content and true if successful, empty string and false otherwise.
func tryPasteTool(tool ClipboardTool, timeout time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool.Name, tool.Args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return "", false
	}

	// Check if the context was cancelled (timeout)
	if ctx.Err() != nil {
		return "", false
	}

	return strings.TrimSuffix(stdout.String(), "\n"), true
}

// GetCopyTools returns the list of copy tools (for testing).
func GetCopyTools() []ClipboardTool {
	return copyTools
}

// GetPasteTools returns the list of paste tools (for testing).
func GetPasteTools() []ClipboardTool {
	return pasteTools
}

// GetApplicableToolsForPlatform returns tools applicable to a specific platform (for testing).
func GetApplicableToolsForPlatform(tools []ClipboardTool, platform string) []ClipboardTool {
	var applicable []ClipboardTool
	for _, tool := range tools {
		if tool.Platform == "" || tool.Platform == platform {
			applicable = append(applicable, tool)
		}
	}
	return applicable
}
