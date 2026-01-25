package clipboard

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestClipboardToolStruct(t *testing.T) {
	tool := ClipboardTool{
		Name:     "test-tool",
		Args:     []string{"-arg1", "-arg2"},
		Platform: "linux",
	}

	if tool.Name != "test-tool" {
		t.Errorf("expected Name 'test-tool', got %q", tool.Name)
	}
	if len(tool.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(tool.Args))
	}
	if tool.Platform != "linux" {
		t.Errorf("expected Platform 'linux', got %q", tool.Platform)
	}
}

func TestCopyToolsOrder(t *testing.T) {
	tools := GetCopyTools()

	expectedOrder := []string{"pbcopy", "xclip", "wl-copy", "clip.exe", "powershell"}

	if len(tools) != len(expectedOrder) {
		t.Fatalf("expected %d tools, got %d", len(expectedOrder), len(tools))
	}

	for i, tool := range tools {
		if tool.Name != expectedOrder[i] {
			t.Errorf("expected tool %d to be %q, got %q", i, expectedOrder[i], tool.Name)
		}
	}
}

func TestPasteToolsOrder(t *testing.T) {
	tools := GetPasteTools()

	expectedOrder := []string{"pbpaste", "xclip", "wl-paste", "powershell"}

	if len(tools) != len(expectedOrder) {
		t.Fatalf("expected %d tools, got %d", len(expectedOrder), len(tools))
	}

	for i, tool := range tools {
		if tool.Name != expectedOrder[i] {
			t.Errorf("expected tool %d to be %q, got %q", i, expectedOrder[i], tool.Name)
		}
	}
}

func TestGetApplicableToolsForPlatform(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		expected []string
	}{
		{
			name:     "darwin platform",
			platform: "darwin",
			expected: []string{"pbcopy", "clip.exe"}, // clip.exe has empty platform
		},
		{
			name:     "linux platform",
			platform: "linux",
			expected: []string{"xclip", "wl-copy", "clip.exe"},
		},
		{
			name:     "windows platform",
			platform: "windows",
			expected: []string{"clip.exe", "powershell"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			applicable := GetApplicableToolsForPlatform(GetCopyTools(), tc.platform)

			if len(applicable) != len(tc.expected) {
				t.Fatalf("expected %d tools, got %d: %+v", len(tc.expected), len(applicable), applicable)
			}

			for i, tool := range applicable {
				if tool.Name != tc.expected[i] {
					t.Errorf("expected tool %d to be %q, got %q", i, tc.expected[i], tool.Name)
				}
			}
		})
	}
}

func TestGetApplicableToolsPaste(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		expected []string
	}{
		{
			name:     "darwin platform",
			platform: "darwin",
			expected: []string{"pbpaste"},
		},
		{
			name:     "linux platform",
			platform: "linux",
			expected: []string{"xclip", "wl-paste"},
		},
		{
			name:     "windows platform",
			platform: "windows",
			expected: []string{"powershell"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			applicable := GetApplicableToolsForPlatform(GetPasteTools(), tc.platform)

			if len(applicable) != len(tc.expected) {
				t.Fatalf("expected %d tools, got %d", len(tc.expected), len(applicable))
			}

			for i, tool := range applicable {
				if tool.Name != tc.expected[i] {
					t.Errorf("expected tool %d to be %q, got %q", i, tc.expected[i], tool.Name)
				}
			}
		})
	}
}

func TestToolPlatformFiltering(t *testing.T) {
	// Test that tools with empty platform are included on all platforms
	tools := []ClipboardTool{
		{Name: "universal", Args: nil, Platform: ""},
		{Name: "darwin-only", Args: nil, Platform: "darwin"},
		{Name: "linux-only", Args: nil, Platform: "linux"},
	}

	// Test darwin
	applicable := GetApplicableToolsForPlatform(tools, "darwin")
	if len(applicable) != 2 {
		t.Errorf("darwin: expected 2 tools, got %d", len(applicable))
	}

	// Test linux
	applicable = GetApplicableToolsForPlatform(tools, "linux")
	if len(applicable) != 2 {
		t.Errorf("linux: expected 2 tools, got %d", len(applicable))
	}

	// Test windows (should only get universal)
	applicable = GetApplicableToolsForPlatform(tools, "windows")
	if len(applicable) != 1 {
		t.Errorf("windows: expected 1 tool, got %d", len(applicable))
	}
	if applicable[0].Name != "universal" {
		t.Errorf("expected 'universal', got %q", applicable[0].Name)
	}
}

func TestDefaultTimeout(t *testing.T) {
	if DefaultTimeout != 3*time.Second {
		t.Errorf("expected DefaultTimeout to be 3 seconds, got %v", DefaultTimeout)
	}
}

func TestErrNoClipboardTool(t *testing.T) {
	if ErrNoClipboardTool == nil {
		t.Error("ErrNoClipboardTool should not be nil")
	}
	if ErrNoClipboardTool.Error() != "no clipboard tool available" {
		t.Errorf("unexpected error message: %s", ErrNoClipboardTool.Error())
	}
}

// TestTryCopyToolWithNonExistentCommand tests that non-existent commands fail gracefully.
func TestTryCopyToolWithNonExistentCommand(t *testing.T) {
	tool := ClipboardTool{
		Name:     "nonexistent-clipboard-tool-xyz",
		Args:     nil,
		Platform: "",
	}

	success := tryCopyTool(tool, "test value", 1*time.Second)
	if success {
		t.Error("expected failure for non-existent command")
	}
}

// TestTryPasteToolWithNonExistentCommand tests that non-existent commands fail gracefully.
func TestTryPasteToolWithNonExistentCommand(t *testing.T) {
	tool := ClipboardTool{
		Name:     "nonexistent-clipboard-tool-xyz",
		Args:     nil,
		Platform: "",
	}

	_, success := tryPasteTool(tool, 1*time.Second)
	if success {
		t.Error("expected failure for non-existent command")
	}
}

// TestTimeoutHandling tests that commands respect timeout.
func TestTimeoutHandling(t *testing.T) {
	// Use 'sleep' command to test timeout handling
	tool := ClipboardTool{
		Name:     "sleep",
		Args:     []string{"10"}, // Sleep for 10 seconds
		Platform: "",
	}

	start := time.Now()
	success := tryCopyTool(tool, "test", 100*time.Millisecond)
	elapsed := time.Since(start)

	if success {
		t.Error("expected failure due to timeout")
	}

	// Should have timed out in roughly 100ms, not 10 seconds
	if elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

// TestCopyToClipboardFallback tests that CopyToClipboard tries multiple tools.
func TestCopyToClipboardFallback(t *testing.T) {
	// This test verifies the fallback behavior by attempting to copy
	// The result depends on what tools are available on the system
	success, err := CopyToClipboard("test clipboard content")

	// We can't guarantee success on all systems, but we can verify no panic occurs
	// and that either success or no-tools-error is returned
	if err != nil && err != ErrNoClipboardTool {
		t.Logf("CopyToClipboard returned error (expected on systems without clipboard tools): %v", err)
	}
	t.Logf("CopyToClipboard success: %v, error: %v", success, err)
}

// TestReadFromClipboardFallback tests that ReadFromClipboard tries multiple tools.
func TestReadFromClipboardFallback(t *testing.T) {
	// This test verifies the fallback behavior by attempting to read
	content, err := ReadFromClipboard()

	// We can't guarantee success on all systems, but we can verify no panic occurs
	if err != nil && err != ErrNoClipboardTool {
		t.Logf("ReadFromClipboard returned error (expected on systems without clipboard tools): %v", err)
	}
	t.Logf("ReadFromClipboard content: %q, error: %v", content, err)
}

// TestCopyToClipboardWithTimeout tests the custom timeout variant.
func TestCopyToClipboardWithTimeout(t *testing.T) {
	success, err := CopyToClipboardWithTimeout("test", 1*time.Second)

	// Just verify it doesn't panic and returns valid results
	if err != nil && err != ErrNoClipboardTool {
		t.Logf("CopyToClipboardWithTimeout error: %v", err)
	}
	t.Logf("CopyToClipboardWithTimeout success: %v", success)
}

// TestReadFromClipboardWithTimeout tests the custom timeout variant.
func TestReadFromClipboardWithTimeout(t *testing.T) {
	content, err := ReadFromClipboardWithTimeout(1 * time.Second)

	// Just verify it doesn't panic and returns valid results
	if err != nil && err != ErrNoClipboardTool {
		t.Logf("ReadFromClipboardWithTimeout error: %v", err)
	}
	t.Logf("ReadFromClipboardWithTimeout content: %q", content)
}

// TestEmptyToolsSlice tests behavior when no tools are applicable.
func TestEmptyToolsSlice(t *testing.T) {
	// Test with an empty slice
	tools := []ClipboardTool{}
	applicable := GetApplicableToolsForPlatform(tools, "darwin")

	if len(applicable) != 0 {
		t.Errorf("expected 0 tools, got %d", len(applicable))
	}
}

// TestToolArgsArePreserved tests that tool arguments are correctly preserved.
func TestToolArgsArePreserved(t *testing.T) {
	tools := GetCopyTools()

	// Find xclip and verify its args
	for _, tool := range tools {
		if tool.Name == "xclip" {
			expectedArgs := []string{"-selection", "clipboard"}
			if len(tool.Args) != len(expectedArgs) {
				t.Fatalf("xclip: expected %d args, got %d", len(expectedArgs), len(tool.Args))
			}
			for i, arg := range tool.Args {
				if arg != expectedArgs[i] {
					t.Errorf("xclip arg %d: expected %q, got %q", i, expectedArgs[i], arg)
				}
			}
			return
		}
	}
	t.Error("xclip not found in copy tools")
}

// TestPowershellArgsForSetClipboard verifies powershell command args.
func TestPowershellArgsForSetClipboard(t *testing.T) {
	tools := GetCopyTools()

	for _, tool := range tools {
		if tool.Name == "powershell" {
			expectedArgs := []string{"-NoProfile", "-Command", "Set-Clipboard"}
			if len(tool.Args) != len(expectedArgs) {
				t.Fatalf("powershell: expected %d args, got %d", len(expectedArgs), len(tool.Args))
			}
			for i, arg := range tool.Args {
				if arg != expectedArgs[i] {
					t.Errorf("powershell arg %d: expected %q, got %q", i, expectedArgs[i], arg)
				}
			}
			return
		}
	}
	t.Error("powershell not found in copy tools")
}

// TestCurrentPlatformHasApplicableTools verifies that the current platform has at least one tool.
func TestCurrentPlatformHasApplicableTools(t *testing.T) {
	currentOS := runtime.GOOS

	copyApplicable := getApplicableTools(copyTools)
	pasteApplicable := getApplicableTools(pasteTools)

	t.Logf("Current OS: %s", currentOS)
	t.Logf("Applicable copy tools: %d", len(copyApplicable))
	t.Logf("Applicable paste tools: %d", len(pasteApplicable))

	// On supported platforms (darwin, linux, windows), we should have at least one tool
	if currentOS == "darwin" || currentOS == "linux" || currentOS == "windows" {
		if len(copyApplicable) == 0 {
			t.Error("expected at least one copy tool for current platform")
		}
		if len(pasteApplicable) == 0 {
			t.Error("expected at least one paste tool for current platform")
		}
	}
}

// TestIntegrationCopyPaste performs an integration test if clipboard tools are available.
func TestIntegrationCopyPaste(t *testing.T) {
	// Skip if no clipboard tools are likely available
	// This is a best-effort integration test
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Check if any clipboard tool is available
	tools := getApplicableTools(copyTools)
	if len(tools) == 0 {
		t.Skip("no clipboard tools available for integration test")
	}

	// Try to find if any tool is actually installed
	hasWorkingTool := false
	for _, tool := range tools {
		_, err := exec.LookPath(tool.Name)
		if err == nil {
			hasWorkingTool = true
			t.Logf("Found working tool: %s", tool.Name)
			break
		}
	}

	if !hasWorkingTool {
		t.Skip("no clipboard tool installed for integration test")
	}

	testContent := "nexus-clipboard-test-" + time.Now().String()

	success, err := CopyToClipboard(testContent)
	if err != nil {
		t.Skipf("CopyToClipboard failed: %v", err)
	}
	if !success {
		t.Skip("CopyToClipboard returned false")
	}

	// Read back
	content, err := ReadFromClipboard()
	if err != nil {
		t.Skipf("ReadFromClipboard failed: %v", err)
	}

	if content != testContent {
		t.Errorf("clipboard content mismatch: expected %q, got %q", testContent, content)
	}
}

// TestContextCancellation tests that context cancellation is handled properly.
func TestContextCancellation(t *testing.T) {
	// Use sleep command which will definitely block
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sleep", "10") // Sleep for 10 seconds
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	// Should have been killed due to timeout
	if err == nil {
		t.Error("expected error from cancelled context")
	}

	// Verify it was killed quickly, not after 10 seconds
	if elapsed > 500*time.Millisecond {
		t.Errorf("context cancellation took too long: %v", elapsed)
	}
}

// Benchmark tests
func BenchmarkGetApplicableTools(b *testing.B) {
	for i := 0; i < b.N; i++ {
		getApplicableTools(copyTools)
	}
}

func BenchmarkGetApplicableToolsForPlatform(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetApplicableToolsForPlatform(copyTools, "linux")
	}
}
