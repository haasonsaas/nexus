package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

var dockerCheck struct {
	once sync.Once
	err  error
}

func requireDocker(t *testing.T) {
	t.Helper()
	force := os.Getenv("NEXUS_DOCKER_TESTS") == "1"
	allowPull := os.Getenv("NEXUS_DOCKER_PULL") == "1"
	if testing.Short() && !force {
		t.Skip("Skipping integration test in short mode")
	}

	dockerCheck.once.Do(func() {
		if _, err := exec.LookPath("docker"); err != nil {
			dockerCheck.err = err
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
			dockerCheck.err = err
			return
		}

		images := []string{"python:3.11-alpine", "node:20-alpine", "golang:1.22-alpine", "bash:5-alpine"}
		for _, image := range images {
			if err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Run(); err != nil {
				if !allowPull {
					dockerCheck.err = err
					return
				}
				pullCtx, pullCancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if pullErr := exec.CommandContext(pullCtx, "docker", "pull", image).Run(); pullErr != nil {
					pullCancel()
					dockerCheck.err = pullErr
					return
				}
				pullCancel()
			}
		}

		runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer runCancel()
		if err := exec.CommandContext(runCtx, "docker", "run", "--rm", "--pull=never", "python:3.11-alpine", "true").Run(); err != nil {
			dockerCheck.err = err
			return
		}
	})

	if dockerCheck.err != nil {
		if errors.Is(dockerCheck.err, exec.ErrNotFound) {
			if force {
				t.Fatalf("Docker required but not installed")
			}
			t.Skip("Docker not installed")
		}
		if force {
			t.Fatalf("Docker required but unavailable: %v", dockerCheck.err)
		}
		t.Skipf("Docker not available for tests: %v", dockerCheck.err)
	}
}

func TestExecutor_Name(t *testing.T) {
	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	if name := executor.Name(); name != "execute_code" {
		t.Errorf("Expected name 'execute_code', got '%s'", name)
	}
}

func TestExecutor_Description(t *testing.T) {
	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	desc := executor.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
	if !strings.Contains(desc, "sandbox") {
		t.Error("Description should mention sandbox")
	}
}

func TestExecutor_Schema(t *testing.T) {
	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	schema := executor.Schema()
	if len(schema) == 0 {
		t.Error("Schema should not be empty")
	}

	// Validate it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("Schema is not valid JSON: %v", err)
	}

	// Check for required fields
	if props, ok := parsed["properties"].(map[string]interface{}); ok {
		if _, ok := props["language"]; !ok {
			t.Error("Schema should have 'language' property")
		}
		if _, ok := props["code"]; !ok {
			t.Error("Schema should have 'code' property")
		}
	}
}

func TestExecutor_PythonExecution(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	tests := []struct {
		name     string
		code     string
		stdin    string
		wantOut  string
		wantCode int
	}{
		{
			name:     "Hello World",
			code:     `print("Hello, World!")`,
			wantOut:  "Hello, World!",
			wantCode: 0,
		},
		{
			name:     "Math Operation",
			code:     `print(2 + 2)`,
			wantOut:  "4",
			wantCode: 0,
		},
		{
			name:     "Read Stdin",
			code:     `import sys; print(sys.stdin.read().strip())`,
			stdin:    "test input",
			wantOut:  "test input",
			wantCode: 0,
		},
		{
			name:     "Syntax Error",
			code:     `print("unclosed string`,
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := ExecuteParams{
				Language: "python",
				Code:     tt.code,
				Stdin:    tt.stdin,
				Timeout:  5,
			}

			paramsJSON, _ := json.Marshal(params)
			ctx := context.Background()

			result, err := executor.Execute(ctx, paramsJSON)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			// Parse result to check exit code
			if tt.wantCode == 0 && result.IsError {
				t.Errorf("Expected success but got error: %s", result.Content)
			}

			if tt.wantOut != "" && !strings.Contains(result.Content, tt.wantOut) {
				t.Errorf("Expected output to contain '%s', got: %s", tt.wantOut, result.Content)
			}
		})
	}
}

func TestExecutor_NodeJSExecution(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	tests := []struct {
		name     string
		code     string
		wantOut  string
		wantCode int
	}{
		{
			name:     "Console Log",
			code:     `console.log("Hello from Node!");`,
			wantOut:  "Hello from Node!",
			wantCode: 0,
		},
		{
			name:     "Array Operations",
			code:     `const arr = [1, 2, 3]; console.log(arr.reduce((a, b) => a + b));`,
			wantOut:  "6",
			wantCode: 0,
		},
		{
			name:     "Async Code",
			code:     `(async () => { await Promise.resolve(); console.log("async works"); })();`,
			wantOut:  "async works",
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := ExecuteParams{
				Language: "nodejs",
				Code:     tt.code,
				Timeout:  5,
			}

			paramsJSON, _ := json.Marshal(params)
			ctx := context.Background()

			result, err := executor.Execute(ctx, paramsJSON)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if tt.wantCode == 0 && result.IsError {
				t.Errorf("Expected success but got error: %s", result.Content)
			}

			if tt.wantOut != "" && !strings.Contains(result.Content, tt.wantOut) {
				t.Errorf("Expected output to contain '%s', got: %s", tt.wantOut, result.Content)
			}
		})
	}
}

func TestExecutor_GoExecution(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	tests := []struct {
		name     string
		code     string
		wantOut  string
		wantCode int
	}{
		{
			name: "Hello World",
			code: `package main
import "fmt"
func main() {
	fmt.Println("Hello, Go!")
}`,
			wantOut:  "Hello, Go!",
			wantCode: 0,
		},
		{
			name: "Math",
			code: `package main
import "fmt"
func main() {
	fmt.Println(10 * 5)
}`,
			wantOut:  "50",
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := ExecuteParams{
				Language: "go",
				Code:     tt.code,
				Timeout:  10, // Go compilation takes longer
			}

			paramsJSON, _ := json.Marshal(params)
			ctx := context.Background()

			result, err := executor.Execute(ctx, paramsJSON)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if tt.wantCode == 0 && result.IsError {
				t.Errorf("Expected success but got error: %s", result.Content)
			}

			if tt.wantOut != "" && !strings.Contains(result.Content, tt.wantOut) {
				t.Errorf("Expected output to contain '%s', got: %s", tt.wantOut, result.Content)
			}
		})
	}
}

func TestExecutor_BashExecution(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	tests := []struct {
		name     string
		code     string
		wantOut  string
		wantCode int
	}{
		{
			name:     "Echo",
			code:     `echo "Hello from Bash"`,
			wantOut:  "Hello from Bash",
			wantCode: 0,
		},
		{
			name:     "Variables",
			code:     `NAME="World"; echo "Hello, $NAME"`,
			wantOut:  "Hello, World",
			wantCode: 0,
		},
		{
			name:     "Command Substitution",
			code:     `echo "Result: $((5 + 3))"`,
			wantOut:  "Result: 8",
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := ExecuteParams{
				Language: "bash",
				Code:     tt.code,
				Timeout:  5,
			}

			paramsJSON, _ := json.Marshal(params)
			ctx := context.Background()

			result, err := executor.Execute(ctx, paramsJSON)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if tt.wantCode == 0 && result.IsError {
				t.Errorf("Expected success but got error: %s", result.Content)
			}

			if tt.wantOut != "" && !strings.Contains(result.Content, tt.wantOut) {
				t.Errorf("Expected output to contain '%s', got: %s", tt.wantOut, result.Content)
			}
		})
	}
}

func TestExecutor_TimeoutHandling(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	params := ExecuteParams{
		Language: "python",
		Code:     `import time; time.sleep(10)`,
		Timeout:  1, // 1 second timeout
	}

	paramsJSON, _ := json.Marshal(params)
	ctx := context.Background()

	start := time.Now()
	result, err := executor.Execute(ctx, paramsJSON)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should timeout around 1 second, not wait 10 seconds
	if elapsed > 3*time.Second {
		t.Errorf("Timeout took too long: %v", elapsed)
	}

	if !result.IsError {
		t.Error("Expected timeout error")
	}

	if !strings.Contains(result.Content, "timeout") && !strings.Contains(result.Content, "Timeout") {
		t.Errorf("Expected timeout message, got: %s", result.Content)
	}
}

func TestExecutor_ResourceLimits(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	// Test memory limit by trying to allocate lots of memory
	params := ExecuteParams{
		Language: "python",
		Code: `
data = []
try:
    for i in range(1000):
        data.append([0] * 1000000)  # Try to use lots of memory
    print("No memory limit")
except MemoryError:
    print("Memory limit hit")
`,
		Timeout:  10,
		MemLimit: 128, // 128 MB limit
	}

	paramsJSON, _ := json.Marshal(params)
	ctx := context.Background()

	result, err := executor.Execute(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should either hit memory limit or be killed by container
	t.Logf("Result: %s", result.Content)
}

func TestExecutor_StderrCapture(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	params := ExecuteParams{
		Language: "python",
		Code: `
import sys
print("stdout message")
print("stderr message", file=sys.stderr)
`,
		Timeout: 5,
	}

	paramsJSON, _ := json.Marshal(params)
	ctx := context.Background()

	result, err := executor.Execute(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result.Content, "stdout message") {
		t.Error("Expected stdout message in output")
	}

	if !strings.Contains(result.Content, "stderr message") {
		t.Error("Expected stderr message in output")
	}
}

func TestExecutor_InvalidLanguage(t *testing.T) {
	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	params := ExecuteParams{
		Language: "invalid",
		Code:     `print("test")`,
	}

	paramsJSON, _ := json.Marshal(params)
	ctx := context.Background()

	result, err := executor.Execute(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.IsError {
		t.Error("Expected error for invalid language")
	}

	if !strings.Contains(result.Content, "Unsupported language") {
		t.Errorf("Expected unsupported language error, got: %s", result.Content)
	}
}

func TestExecutor_InvalidJSON(t *testing.T) {
	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	ctx := context.Background()
	result, err := executor.Execute(ctx, json.RawMessage(`{invalid json}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.IsError {
		t.Error("Expected error for invalid JSON")
	}
}

func TestExecutor_FileMounting(t *testing.T) {
	requireDocker(t)

	executor, err := NewExecutor()
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}
	defer executor.Close()

	params := ExecuteParams{
		Language: "python",
		Code: `
with open('data.txt', 'r') as f:
    content = f.read()
print(f"Read: {content}")
`,
		Files: map[string]string{
			"data.txt": "Hello from file!",
		},
		Timeout: 5,
	}

	paramsJSON, _ := json.Marshal(params)
	ctx := context.Background()

	result, err := executor.Execute(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected success but got error: %s", result.Content)
	}

	if !strings.Contains(result.Content, "Hello from file!") {
		t.Errorf("Expected file content in output, got: %s", result.Content)
	}
}

func TestPool_GetAndPut(t *testing.T) {
	requireDocker(t)

	config := &Config{
		Backend:        BackendDocker,
		PoolSize:       2,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Get an executor
	exec1, err := pool.Get(ctx, "python")
	if err != nil {
		t.Fatalf("Failed to get executor: %v", err)
	}

	if exec1.Language() != "python" {
		t.Errorf("Expected python executor, got: %s", exec1.Language())
	}

	// Return it
	pool.Put(exec1)

	// Get it again - should get the same one from the pool
	exec2, err := pool.Get(ctx, "python")
	if err != nil {
		t.Fatalf("Failed to get executor: %v", err)
	}

	pool.Put(exec2)
}

func TestPool_Stats(t *testing.T) {
	config := &Config{
		Backend:        BackendDocker,
		PoolSize:       2,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	stats := pool.Stats()
	if len(stats) != 4 { // python, nodejs, go, bash
		t.Errorf("Expected stats for 4 languages, got: %d", len(stats))
	}

	if pythonStats, ok := stats["python"]; ok {
		if pythonStats.Language != "python" {
			t.Errorf("Expected python stats, got: %s", pythonStats.Language)
		}
	} else {
		t.Error("Expected python stats")
	}
}

func TestPool_Warmup(t *testing.T) {
	requireDocker(t)

	config := &Config{
		Backend:        BackendDocker,
		PoolSize:       0, // Start with empty pool
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Warmup python pool
	if err := pool.Warmup(ctx, "python", 2); err != nil {
		t.Fatalf("Failed to warmup pool: %v", err)
	}

	stats := pool.Stats()
	if pythonStats, ok := stats["python"]; ok {
		if pythonStats.Available < 1 {
			t.Errorf("Expected at least 1 available python executor after warmup, got: %d", pythonStats.Available)
		}
	}
}

func TestPool_Close(t *testing.T) {
	config := &Config{
		Backend:        BackendDocker,
		PoolSize:       1,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	if err := pool.Close(); err != nil {
		t.Fatalf("Failed to close pool: %v", err)
	}

	// Try to get after close
	ctx := context.Background()
	_, err = pool.Get(ctx, "python")
	if err == nil {
		t.Error("Expected error when getting from closed pool")
	}
}

func TestDockerExecutor_Run(t *testing.T) {
	requireDocker(t)

	executor, err := newDockerExecutor("python", 1000, 512, false)
	if err != nil {
		t.Fatalf("Failed to create docker executor: %v", err)
	}
	defer executor.Close()

	params := &ExecuteParams{
		Language: "python",
		Code:     `print("test")`,
		CPULimit: 1000,
		MemLimit: 512,
		Timeout:  5,
	}

	workspace, err := prepareWorkspace(params)
	if err != nil {
		t.Fatalf("Failed to prepare workspace: %v", err)
	}
	defer os.RemoveAll(workspace)

	ctx := context.Background()
	result, err := executor.Run(ctx, params, workspace)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(result.Stdout, "test") {
		t.Errorf("Expected 'test' in stdout, got: %s", result.Stdout)
	}

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got: %d", result.ExitCode)
	}
}

// Test modes.go functionality without Docker

func TestSandboxModeConstants(t *testing.T) {
	if ModeOff != "off" {
		t.Errorf("ModeOff = %q, want %q", ModeOff, "off")
	}
	if ModeAll != "all" {
		t.Errorf("ModeAll = %q, want %q", ModeAll, "all")
	}
	if ModeNonMain != "non-main" {
		t.Errorf("ModeNonMain = %q, want %q", ModeNonMain, "non-main")
	}
}

func TestSandboxScopeConstants(t *testing.T) {
	if ScopeAgent != "agent" {
		t.Errorf("ScopeAgent = %q, want %q", ScopeAgent, "agent")
	}
	if ScopeSession != "session" {
		t.Errorf("ScopeSession = %q, want %q", ScopeSession, "session")
	}
	if ScopeShared != "shared" {
		t.Errorf("ScopeShared = %q, want %q", ScopeShared, "shared")
	}
}

func TestResolveModeConfig(t *testing.T) {
	tests := []struct {
		name          string
		cfg           config.SandboxConfig
		expectedMode  SandboxMode
		expectedScope SandboxScope
	}{
		{
			name:          "disabled config",
			cfg:           config.SandboxConfig{Enabled: false},
			expectedMode:  ModeOff,
			expectedScope: ScopeAgent,
		},
		{
			name:          "enabled with all mode",
			cfg:           config.SandboxConfig{Enabled: true, Mode: "all"},
			expectedMode:  ModeAll,
			expectedScope: ScopeAgent,
		},
		{
			name:          "enabled with non-main mode",
			cfg:           config.SandboxConfig{Enabled: true, Mode: "non-main"},
			expectedMode:  ModeNonMain,
			expectedScope: ScopeAgent,
		},
		{
			name:          "enabled with session scope",
			cfg:           config.SandboxConfig{Enabled: true, Mode: "all", Scope: "session"},
			expectedMode:  ModeAll,
			expectedScope: ScopeSession,
		},
		{
			name:          "enabled with shared scope",
			cfg:           config.SandboxConfig{Enabled: true, Mode: "all", Scope: "shared"},
			expectedMode:  ModeAll,
			expectedScope: ScopeShared,
		},
		{
			name:          "enabled with invalid mode defaults to all",
			cfg:           config.SandboxConfig{Enabled: true, Mode: "invalid"},
			expectedMode:  ModeAll,
			expectedScope: ScopeAgent,
		},
		{
			name:          "enabled with invalid scope defaults to agent",
			cfg:           config.SandboxConfig{Enabled: true, Mode: "all", Scope: "invalid"},
			expectedMode:  ModeAll,
			expectedScope: ScopeAgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveModeConfig(tt.cfg)
			if result.Mode != tt.expectedMode {
				t.Errorf("Mode = %q, want %q", result.Mode, tt.expectedMode)
			}
			if result.Scope != tt.expectedScope {
				t.Errorf("Scope = %q, want %q", result.Scope, tt.expectedScope)
			}
		})
	}
}

func TestModeConfig_ShouldSandbox(t *testing.T) {
	tests := []struct {
		name        string
		mode        SandboxMode
		isMainAgent bool
		expected    bool
	}{
		{"off mode main agent", ModeOff, true, false},
		{"off mode non-main agent", ModeOff, false, false},
		{"all mode main agent", ModeAll, true, true},
		{"all mode non-main agent", ModeAll, false, true},
		{"non-main mode main agent", ModeNonMain, true, false},
		{"non-main mode non-main agent", ModeNonMain, false, true},
		{"invalid mode", SandboxMode("invalid"), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := ModeConfig{Mode: tt.mode, Scope: ScopeAgent}
			result := mc.ShouldSandbox("agent-123", tt.isMainAgent)
			if result != tt.expected {
				t.Errorf("ShouldSandbox() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestModeConfig_SandboxKey(t *testing.T) {
	tests := []struct {
		name      string
		scope     SandboxScope
		agentID   string
		sessionID string
		expected  string
	}{
		{"agent scope", ScopeAgent, "agent-123", "session-456", "agent:agent-123"},
		{"session scope", ScopeSession, "agent-123", "session-456", "session:session-456"},
		{"shared scope", ScopeShared, "agent-123", "session-456", "shared"},
		{"default scope", SandboxScope("invalid"), "agent-123", "session-456", "agent:agent-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := ModeConfig{Mode: ModeAll, Scope: tt.scope}
			result := mc.SandboxKey(tt.agentID, tt.sessionID)
			if result != tt.expected {
				t.Errorf("SandboxKey() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestModeConfig_Struct(t *testing.T) {
	mc := ModeConfig{
		Mode:  ModeAll,
		Scope: ScopeSession,
	}

	if mc.Mode != ModeAll {
		t.Errorf("Mode = %q, want %q", mc.Mode, ModeAll)
	}
	if mc.Scope != ScopeSession {
		t.Errorf("Scope = %q, want %q", mc.Scope, ScopeSession)
	}
}

func TestGetMainFilename(t *testing.T) {
	tests := []struct {
		language string
		expected string
	}{
		{"python", "main.py"},
		{"nodejs", "main.js"},
		{"go", "main.go"},
		{"bash", "main.sh"},
		{"unknown", "main.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			result := getMainFilename(tt.language)
			if result != tt.expected {
				t.Errorf("getMainFilename(%q) = %q, want %q", tt.language, result, tt.expected)
			}
		})
	}
}

func TestIsValidLanguage(t *testing.T) {
	tests := []struct {
		language string
		valid    bool
	}{
		{"python", true},
		{"nodejs", true},
		{"go", true},
		{"bash", true},
		{"ruby", false},
		{"java", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			result := isValidLanguage(tt.language)
			if result != tt.valid {
				t.Errorf("isValidLanguage(%q) = %v, want %v", tt.language, result, tt.valid)
			}
		})
	}
}

func TestGetDockerImage(t *testing.T) {
	tests := []struct {
		language string
		expected string
	}{
		{"python", "python:3.11-alpine"},
		{"nodejs", "node:20-alpine"},
		{"go", "golang:1.22-alpine"},
		{"bash", "bash:5-alpine"},
		{"unknown", "alpine:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			result := getDockerImage(tt.language)
			if result != tt.expected {
				t.Errorf("getDockerImage(%q) = %q, want %q", tt.language, result, tt.expected)
			}
		})
	}
}

func TestGetRunCommand(t *testing.T) {
	tests := []struct {
		language string
		expected []string
	}{
		{"python", []string{"python", "main.py"}},
		{"nodejs", []string{"node", "main.js"}},
		{"go", []string{"sh", "-c", "go run main.go"}},
		{"bash", []string{"bash", "main.sh"}},
		{"unknown", []string{"cat", "main.txt"}},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			result := getRunCommand(tt.language)
			if len(result) != len(tt.expected) {
				t.Errorf("getRunCommand(%q) len = %d, want %d", tt.language, len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getRunCommand(%q)[%d] = %q, want %q", tt.language, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestFormatExecutionResult(t *testing.T) {
	t.Run("success result", func(t *testing.T) {
		result := &ExecuteResult{
			Stdout:   "Hello World\n",
			Stderr:   "",
			ExitCode: 0,
		}
		output := formatExecutionResult(result)
		if !strings.Contains(output, "Hello World") {
			t.Error("expected stdout in output")
		}
		if !strings.Contains(output, "Exit code: 0") {
			t.Error("expected exit code in output")
		}
	})

	t.Run("error result", func(t *testing.T) {
		result := &ExecuteResult{
			Stdout:   "",
			Stderr:   "Error: file not found",
			ExitCode: 1,
			Error:    "execution failed",
		}
		output := formatExecutionResult(result)
		if !strings.Contains(output, "Error: execution failed") {
			t.Error("expected error message in output")
		}
		if !strings.Contains(output, "file not found") {
			t.Error("expected stderr in output")
		}
	})

	t.Run("timeout result", func(t *testing.T) {
		result := &ExecuteResult{
			Timeout: true,
			Error:   "timeout",
		}
		output := formatExecutionResult(result)
		if !strings.Contains(output, "timed out") {
			t.Error("expected timeout message in output")
		}
	})

	t.Run("stdout without newline", func(t *testing.T) {
		result := &ExecuteResult{
			Stdout:   "no newline",
			ExitCode: 0,
		}
		output := formatExecutionResult(result)
		// Should have newline added
		if !strings.Contains(output, "no newline\n") {
			t.Error("expected newline to be added after stdout")
		}
	})
}

func TestExecuteParams_Struct(t *testing.T) {
	params := ExecuteParams{
		Language:        "python",
		Code:            "print('hello')",
		Stdin:           "input data",
		Files:           map[string]string{"data.txt": "content"},
		Timeout:         30,
		CPULimit:        1000,
		MemLimit:        512,
		WorkspaceAccess: WorkspaceReadOnly,
	}

	if params.Language != "python" {
		t.Errorf("Language = %q, want %q", params.Language, "python")
	}
	if params.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", params.Timeout)
	}
	if params.WorkspaceAccess != WorkspaceReadOnly {
		t.Errorf("WorkspaceAccess = %q, want %q", params.WorkspaceAccess, WorkspaceReadOnly)
	}
}

func TestExecuteResult_Struct(t *testing.T) {
	result := ExecuteResult{
		Stdout:   "output",
		Stderr:   "error",
		ExitCode: 1,
		Error:    "failed",
		Timeout:  true,
	}

	if result.Stdout != "output" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "output")
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if !result.Timeout {
		t.Error("Timeout should be true")
	}
}

func TestWorkspaceAccessModeConstants(t *testing.T) {
	if WorkspaceNone != "none" {
		t.Errorf("WorkspaceNone = %q, want %q", WorkspaceNone, "none")
	}
	if WorkspaceReadOnly != "ro" {
		t.Errorf("WorkspaceReadOnly = %q, want %q", WorkspaceReadOnly, "ro")
	}
	if WorkspaceReadWrite != "rw" {
		t.Errorf("WorkspaceReadWrite = %q, want %q", WorkspaceReadWrite, "rw")
	}
}

func TestBackendConstants(t *testing.T) {
	if BackendDocker != "docker" {
		t.Errorf("BackendDocker = %q, want %q", BackendDocker, "docker")
	}
	if BackendFirecracker != "firecracker" {
		t.Errorf("BackendFirecracker = %q, want %q", BackendFirecracker, "firecracker")
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		Backend:        BackendDocker,
		PoolSize:       5,
		MaxPoolSize:    20,
		DefaultTimeout: 60 * time.Second,
		DefaultCPU:     2000,
		DefaultMemory:  1024,
		NetworkEnabled: true,
	}

	if cfg.Backend != BackendDocker {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendDocker)
	}
	if cfg.PoolSize != 5 {
		t.Errorf("PoolSize = %d, want 5", cfg.PoolSize)
	}
	if cfg.MaxPoolSize != 20 {
		t.Errorf("MaxPoolSize = %d, want 20", cfg.MaxPoolSize)
	}
	if !cfg.NetworkEnabled {
		t.Error("NetworkEnabled should be true")
	}
}

func TestConfigOptions(t *testing.T) {
	cfg := &Config{}

	WithBackend(BackendFirecracker)(cfg)
	if cfg.Backend != BackendFirecracker {
		t.Errorf("WithBackend: Backend = %q, want %q", cfg.Backend, BackendFirecracker)
	}

	WithPoolSize(10)(cfg)
	if cfg.PoolSize != 10 {
		t.Errorf("WithPoolSize: PoolSize = %d, want 10", cfg.PoolSize)
	}

	WithMaxPoolSize(50)(cfg)
	if cfg.MaxPoolSize != 50 {
		t.Errorf("WithMaxPoolSize: MaxPoolSize = %d, want 50", cfg.MaxPoolSize)
	}

	WithDefaultTimeout(2 * time.Minute)(cfg)
	if cfg.DefaultTimeout != 2*time.Minute {
		t.Errorf("WithDefaultTimeout: DefaultTimeout = %v, want %v", cfg.DefaultTimeout, 2*time.Minute)
	}

	WithDefaultCPU(3000)(cfg)
	if cfg.DefaultCPU != 3000 {
		t.Errorf("WithDefaultCPU: DefaultCPU = %d, want 3000", cfg.DefaultCPU)
	}

	WithDefaultMemory(2048)(cfg)
	if cfg.DefaultMemory != 2048 {
		t.Errorf("WithDefaultMemory: DefaultMemory = %d, want 2048", cfg.DefaultMemory)
	}

	WithNetworkEnabled(true)(cfg)
	if !cfg.NetworkEnabled {
		t.Error("WithNetworkEnabled: NetworkEnabled should be true")
	}
}

func TestPoolStats_Struct(t *testing.T) {
	stats := PoolStats{
		Language:  "python",
		Available: 3,
		Active:    2,
		MaxSize:   10,
	}

	if stats.Language != "python" {
		t.Errorf("Language = %q, want %q", stats.Language, "python")
	}
	if stats.Available != 3 {
		t.Errorf("Available = %d, want 3", stats.Available)
	}
	if stats.Active != 2 {
		t.Errorf("Active = %d, want 2", stats.Active)
	}
	if stats.MaxSize != 10 {
		t.Errorf("MaxSize = %d, want 10", stats.MaxSize)
	}
}

func TestNewPool_NilConfig(t *testing.T) {
	_, err := NewPool(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestPrepareWorkspace(t *testing.T) {
	params := &ExecuteParams{
		Language: "python",
		Code:     "print('hello')",
		Files: map[string]string{
			"data.txt":      "some data",
			"../escape.txt": "should be sanitized",
		},
		Stdin: "input",
	}

	workspace, err := prepareWorkspace(params)
	if err != nil {
		t.Fatalf("prepareWorkspace failed: %v", err)
	}
	defer os.RemoveAll(workspace)

	// Check main file exists
	mainPath := filepath.Join(workspace, "main.py")
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("main file not found: %v", err)
	}

	// Check additional file exists (should be sanitized)
	dataPath := filepath.Join(workspace, "data.txt")
	if _, err := os.Stat(dataPath); err != nil {
		t.Errorf("data file not found: %v", err)
	}

	// Check stdin file exists
	stdinPath := filepath.Join(workspace, "stdin.txt")
	if _, err := os.Stat(stdinPath); err != nil {
		t.Errorf("stdin file not found: %v", err)
	}

	// Check that the traversal attempt was sanitized
	escapePath := filepath.Join(workspace, "escape.txt")
	if _, err := os.Stat(escapePath); err != nil {
		t.Errorf("escape file should exist (sanitized): %v", err)
	}
}

func TestPool_Get_UnsupportedLanguage(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	_, err = pool.Get(context.Background(), "ruby")
	if err == nil {
		t.Error("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %q, want to contain 'unsupported'", err.Error())
	}
}

func TestPool_Shrink_UnsupportedLanguage(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	err = pool.Shrink("ruby", 1)
	if err == nil {
		t.Error("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %q, want to contain 'unsupported'", err.Error())
	}
}

func TestPool_Shrink_ClosedPool(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	pool.Close()

	err = pool.Shrink("python", 1)
	if err == nil {
		t.Error("expected error for closed pool")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error = %q, want to contain 'closed'", err.Error())
	}
}

func TestPool_Health_ClosedPool(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	pool.Close()

	err = pool.Health()
	if err == nil {
		t.Error("expected error for closed pool")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error = %q, want to contain 'closed'", err.Error())
	}
}

func TestPool_Warmup_UnsupportedLanguage(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	err = pool.Warmup(context.Background(), "ruby", 1)
	if err == nil {
		t.Error("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %q, want to contain 'unsupported'", err.Error())
	}
}

func TestPool_Warmup_ClosedPool(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	pool.Close()

	err = pool.Warmup(context.Background(), "python", 1)
	if err == nil {
		t.Error("expected error for closed pool")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error = %q, want to contain 'closed'", err.Error())
	}
}

func TestPool_Put_NilExecutor(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	// Should not panic
	pool.Put(nil)
}

func TestPool_DoubleClose(t *testing.T) {
	cfg := &Config{
		Backend:        BackendDocker,
		PoolSize:       0,
		MaxPoolSize:    5,
		DefaultTimeout: 30 * time.Second,
		DefaultCPU:     1000,
		DefaultMemory:  512,
	}

	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	// First close should succeed
	err = pool.Close()
	if err != nil {
		t.Errorf("first Close() error = %v", err)
	}

	// Second close should also succeed (idempotent)
	err = pool.Close()
	if err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestFirecrackerExecutorWrapper_NilBackend(t *testing.T) {
	wrapper := &firecrackerExecutorWrapper{
		language: "python",
		cpuLimit: 1000,
		memLimit: 512,
		backend:  nil,
	}

	// Language should still work
	if lang := wrapper.Language(); lang != "python" {
		t.Errorf("Language() = %q, want %q", lang, "python")
	}

	// Close should not panic
	err := wrapper.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Run should return error
	_, err = wrapper.Run(context.Background(), &ExecuteParams{}, "/tmp")
	if err == nil {
		t.Error("expected error for nil backend")
	}
}

func TestPrepareWorkspace_NoStdin(t *testing.T) {
	params := &ExecuteParams{
		Language: "python",
		Code:     "print('hello')",
		Stdin:    "",
	}

	workspace, err := prepareWorkspace(params)
	if err != nil {
		t.Fatalf("prepareWorkspace failed: %v", err)
	}
	defer os.RemoveAll(workspace)

	// Check main file exists
	mainPath := filepath.Join(workspace, "main.py")
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("main file not found: %v", err)
	}

	// Check stdin file should NOT exist when empty
	stdinPath := filepath.Join(workspace, "stdin.txt")
	if _, err := os.Stat(stdinPath); !os.IsNotExist(err) {
		t.Error("stdin file should not exist when stdin is empty")
	}
}

func TestPrepareWorkspace_AllLanguages(t *testing.T) {
	languages := []struct {
		lang     string
		filename string
	}{
		{"python", "main.py"},
		{"nodejs", "main.js"},
		{"go", "main.go"},
		{"bash", "main.sh"},
		{"unknown", "main.txt"},
	}

	for _, tc := range languages {
		t.Run(tc.lang, func(t *testing.T) {
			params := &ExecuteParams{
				Language: tc.lang,
				Code:     "test code",
			}

			workspace, err := prepareWorkspace(params)
			if err != nil {
				t.Fatalf("prepareWorkspace failed: %v", err)
			}
			defer os.RemoveAll(workspace)

			mainPath := filepath.Join(workspace, tc.filename)
			if _, err := os.Stat(mainPath); err != nil {
				t.Errorf("main file %s not found: %v", tc.filename, err)
			}
		})
	}
}
