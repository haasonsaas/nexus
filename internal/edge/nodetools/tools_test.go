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

func TestCameraSnapTool_Description(t *testing.T) {
	tool := &CameraSnapTool{platform: "darwin"}
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "camera") {
		t.Error("description should mention camera")
	}
}

func TestScreenRecordTool_Description(t *testing.T) {
	tool := &ScreenRecordTool{platform: "darwin"}
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "screen") || !strings.Contains(desc, "Record") {
		t.Error("description should mention screen recording")
	}
}

func TestScreenRecordTool_Schema(t *testing.T) {
	tool := &ScreenRecordTool{platform: "darwin"}
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("expected non-empty schema")
	}

	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Errorf("invalid schema JSON: %v", err)
	}
}

func TestLocationGetTool_Description(t *testing.T) {
	tool := &LocationGetTool{platform: "darwin"}
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "GPS") || !strings.Contains(desc, "coordinates") {
		t.Error("description should mention GPS coordinates")
	}
}

func TestLocationGetTool_Schema(t *testing.T) {
	tool := &LocationGetTool{platform: "darwin"}
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("expected non-empty schema")
	}

	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Errorf("invalid schema JSON: %v", err)
	}
}

func TestLocationGetTool_Execute(t *testing.T) {
	tool := &LocationGetTool{platform: "darwin"}
	ctx := context.Background()

	params, _ := json.Marshal(LocationGetParams{HighAccuracy: true})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// The mac implementation returns placeholder content
	if result.Content == "" {
		t.Error("expected non-empty content")
	}
	if !result.IsError {
		t.Error("expected error when helper is unavailable")
	}
}

func TestLocationGetTool_Execute_UnsupportedPlatform(t *testing.T) {
	tool := &LocationGetTool{platform: "linux"}
	ctx := context.Background()

	params, _ := json.Marshal(LocationGetParams{})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unsupported platform")
	}
	if !strings.Contains(result.Content, "not supported") {
		t.Errorf("expected 'not supported' message, got: %s", result.Content)
	}
}

func TestLocationGetTool_Execute_InvalidParams(t *testing.T) {
	tool := &LocationGetTool{platform: "darwin"}
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid json"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestCameraSnapTool_Execute_UnsupportedPlatform(t *testing.T) {
	tool := &CameraSnapTool{platform: "unsupported"}
	ctx := context.Background()

	params, _ := json.Marshal(CameraSnapParams{})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unsupported platform")
	}
	if !strings.Contains(result.Content, "not supported") {
		t.Errorf("expected 'not supported' message, got: %s", result.Content)
	}
}

func TestCameraSnapTool_Execute_InvalidParams(t *testing.T) {
	tool := &CameraSnapTool{platform: "darwin"}
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid json"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestScreenRecordTool_Execute_UnsupportedPlatform(t *testing.T) {
	tool := &ScreenRecordTool{platform: "unsupported"}
	ctx := context.Background()

	params, _ := json.Marshal(ScreenRecordParams{Duration: 5})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unsupported platform")
	}
	if !strings.Contains(result.Content, "not supported") {
		t.Errorf("expected 'not supported' message, got: %s", result.Content)
	}
}

func TestScreenRecordTool_Execute_InvalidParams(t *testing.T) {
	tool := &ScreenRecordTool{platform: "darwin"}
	ctx := context.Background()

	result, err := tool.Execute(ctx, []byte("invalid json"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid params")
	}
}

func TestScreenRecordTool_Defaults(t *testing.T) {
	tool := &ScreenRecordTool{platform: "darwin"}
	ctx := context.Background()

	// Test with zero duration (should use default)
	params, _ := json.Marshal(ScreenRecordParams{Duration: 0})
	_, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// Can't verify defaults without mocking, but at least it runs
}

func TestRunTool_Description(t *testing.T) {
	tool := &RunTool{}
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "shell") || !strings.Contains(desc, "command") {
		t.Error("description should mention shell command")
	}
}

func TestRunTool_TimeoutClamping(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	// Test with timeout > 300 (should be clamped)
	params, _ := json.Marshal(RunParams{
		Command: "echo test",
		Timeout: 500, // Above max
	})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestRunTool_FailingCommand(t *testing.T) {
	tool := &RunTool{}
	ctx := context.Background()

	params, _ := json.Marshal(RunParams{
		Command: "exit 1",
	})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for failing command")
	}
}

func TestProviderRegister(t *testing.T) {
	provider := NewProvider(nil)

	// Register a custom tool
	customTool := &RunTool{}
	provider.Register(&customToolWrapper{customTool, "custom_run"})

	tool, ok := provider.GetTool("custom_run")
	if !ok {
		t.Error("expected custom tool to be registered")
	}
	if tool.Name() != "custom_run" {
		t.Errorf("name = %s, want custom_run", tool.Name())
	}
}

// customToolWrapper wraps a tool with a custom name for testing
type customToolWrapper struct {
	Tool
	name string
}

func (w *customToolWrapper) Name() string { return w.name }

func TestToolResult_Struct(t *testing.T) {
	result := ToolResult{
		Content: "test content",
		IsError: false,
		Artifacts: []Artifact{
			{
				Type:     "screenshot",
				MimeType: "image/png",
				Filename: "test.png",
				Data:     []byte("data"),
			},
		},
	}

	if result.Content != "test content" {
		t.Errorf("Content = %q", result.Content)
	}
	if result.IsError {
		t.Error("IsError should be false")
	}
	if len(result.Artifacts) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(result.Artifacts))
	}
}

func TestArtifact_Struct(t *testing.T) {
	artifact := Artifact{
		Type:     "screenshot",
		MimeType: "image/png",
		Filename: "test.png",
		Data:     []byte("test data"),
	}

	if artifact.Type != "screenshot" {
		t.Errorf("Type = %q", artifact.Type)
	}
	if artifact.MimeType != "image/png" {
		t.Errorf("MimeType = %q", artifact.MimeType)
	}
	if artifact.Filename != "test.png" {
		t.Errorf("Filename = %q", artifact.Filename)
	}
}

func TestCameraSnapParams_Struct(t *testing.T) {
	params := CameraSnapParams{
		Device:  "front",
		Quality: "high",
	}

	if params.Device != "front" {
		t.Errorf("Device = %q", params.Device)
	}
	if params.Quality != "high" {
		t.Errorf("Quality = %q", params.Quality)
	}
}

func TestScreenRecordParams_Struct(t *testing.T) {
	params := ScreenRecordParams{
		Duration: 10,
		Format:   "gif",
		Display:  "main",
	}

	if params.Duration != 10 {
		t.Errorf("Duration = %d", params.Duration)
	}
	if params.Format != "gif" {
		t.Errorf("Format = %q", params.Format)
	}
	if params.Display != "main" {
		t.Errorf("Display = %q", params.Display)
	}
}

func TestLocationGetParams_Struct(t *testing.T) {
	params := LocationGetParams{
		HighAccuracy: true,
	}

	if !params.HighAccuracy {
		t.Error("HighAccuracy should be true")
	}
}

func TestRunParams_Struct(t *testing.T) {
	params := RunParams{
		Command:    "ls -la",
		WorkingDir: "/tmp",
		Timeout:    30,
	}

	if params.Command != "ls -la" {
		t.Errorf("Command = %q", params.Command)
	}
	if params.WorkingDir != "/tmp" {
		t.Errorf("WorkingDir = %q", params.WorkingDir)
	}
	if params.Timeout != 30 {
		t.Errorf("Timeout = %d", params.Timeout)
	}
}
