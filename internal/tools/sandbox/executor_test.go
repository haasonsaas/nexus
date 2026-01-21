package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
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

	executor, err := newDockerExecutor("python", 1000, 512)
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
