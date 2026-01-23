package nodetools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProvider(t *testing.T) {
	provider := NewProvider(nil)

	// Should have at least the run tool on all platforms
	tools := provider.GetTools()
	if len(tools) == 0 {
		t.Error("expected at least one tool")
	}

	// Check run tool is present
	runTool, ok := provider.GetTool("run")
	if !ok {
		t.Error("expected run tool to be registered")
	}
	if runTool.Name() != "run" {
		t.Errorf("expected name 'run', got %s", runTool.Name())
	}
}

func TestRunTool(t *testing.T) {
	tool := &RunTool{}

	// Check metadata
	if tool.Name() != "run" {
		t.Errorf("expected name 'run', got %s", tool.Name())
	}
	if !tool.RequiresApproval() {
		t.Error("run tool should require approval")
	}
	if tool.ProducesArtifacts() {
		t.Error("run tool should not produce artifacts")
	}

	// Check schema is valid JSON
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Errorf("invalid schema JSON: %v", err)
	}
}

func TestRunToolExecute(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	// Test simple command
	params, _ := json.Marshal(RunParams{Command: "echo hello"})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", result.Content)
	}
}

func TestRunToolMissingCommand(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	params, _ := json.Marshal(RunParams{})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing command")
	}
}

func TestRunToolTimeout(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	// Command that takes longer than timeout
	params, _ := json.Marshal(RunParams{
		Command: "sleep 5",
		Timeout: 1,
	})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected timeout error")
	}
	if !strings.Contains(result.Content, "timed out") {
		t.Errorf("expected timeout message, got: %s", result.Content)
	}
}

func TestRunToolInvalidParams(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid json"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestCameraSnapTool(t *testing.T) {
	tool := &CameraSnapTool{platform: "darwin"}

	// Check metadata
	if tool.Name() != "camera_snap" {
		t.Errorf("expected name 'camera_snap', got %s", tool.Name())
	}
	if !tool.RequiresApproval() {
		t.Error("camera tool should require approval")
	}
	if !tool.ProducesArtifacts() {
		t.Error("camera tool should produce artifacts")
	}

	// Check schema is valid JSON
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Errorf("invalid schema JSON: %v", err)
	}
}

func TestScreenRecordTool(t *testing.T) {
	tool := &ScreenRecordTool{platform: "darwin"}

	if tool.Name() != "screen_record" {
		t.Errorf("expected name 'screen_record', got %s", tool.Name())
	}
	if !tool.RequiresApproval() {
		t.Error("screen tool should require approval")
	}
	if !tool.ProducesArtifacts() {
		t.Error("screen tool should produce artifacts")
	}
}

func TestLocationGetTool(t *testing.T) {
	tool := &LocationGetTool{platform: "darwin"}

	if tool.Name() != "location_get" {
		t.Errorf("expected name 'location_get', got %s", tool.Name())
	}
	if !tool.RequiresApproval() {
		t.Error("location tool should require approval")
	}
	if tool.ProducesArtifacts() {
		t.Error("location tool should not produce artifacts")
	}
}

func TestProviderExecute(t *testing.T) {
	provider := NewProvider(nil)
	ctx := context.Background()

	// Execute run tool via provider
	params, _ := json.Marshal(RunParams{Command: "echo test"})
	result, err := provider.Execute(ctx, "run", params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestProviderExecuteUnknown(t *testing.T) {
	provider := NewProvider(nil)
	ctx := context.Background()

	_, err := provider.Execute(ctx, "unknown_tool", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestScreenRecordParams(t *testing.T) {
	tool := &ScreenRecordTool{platform: "darwin"}
	ctx := context.Background()

	// Test with max duration capping
	params, _ := json.Marshal(ScreenRecordParams{Duration: 100})
	_, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// Can't verify capping without mocking, but at least it doesn't crash
}

func TestRunToolWorkingDir(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	// Test with working directory
	params, _ := json.Marshal(RunParams{
		Command:    "pwd",
		WorkingDir: "/tmp",
	})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "tmp") {
		t.Errorf("expected /tmp in output, got: %s", result.Content)
	}
}

func TestRunToolContextCancellation(t *testing.T) {
	tool := &RunTool{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	params, _ := json.Marshal(RunParams{
		Command: "sleep 10",
		Timeout: 60, // Tool timeout is longer than context timeout
	})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error from context cancellation")
	}
}
