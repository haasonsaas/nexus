package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Executor implements the agent.Tool interface for secure sandboxed code execution.
// It supports Python, Node.js, Go, and Bash with configurable resource limits.
type Executor struct {
	pool            *Pool
	useFirecracker  bool
	workspaceRoot   string
	workspaceAccess WorkspaceAccessMode
}

// WorkspaceAccessMode controls how the workspace is mounted in the sandbox.
type WorkspaceAccessMode string

const (
	// WorkspaceNone means no workspace is mounted (most secure).
	WorkspaceNone WorkspaceAccessMode = "none"

	// WorkspaceReadOnly mounts the workspace as read-only (default).
	WorkspaceReadOnly WorkspaceAccessMode = "ro"

	// WorkspaceReadWrite mounts the workspace with read-write access.
	WorkspaceReadWrite WorkspaceAccessMode = "rw"
)

// ExecuteParams defines the input parameters for code execution including
// the code, language, optional input, additional files, and resource limits.
type ExecuteParams struct {
	Language        string              `json:"language"` // python, nodejs, go, bash
	Code            string              `json:"code"`
	Stdin           string              `json:"stdin,omitempty"`
	Files           map[string]string   `json:"files,omitempty"`            // filename -> content
	Timeout         int                 `json:"timeout,omitempty"`          // seconds, default 30
	CPULimit        int                 `json:"cpu_limit,omitempty"`        // millicores, default 1000
	MemLimit        int                 `json:"mem_limit,omitempty"`        // MB, default 512
	WorkspaceAccess WorkspaceAccessMode `json:"workspace_access,omitempty"` // none, ro, rw - default ro
}

// ExecuteResult contains the execution output including stdout, stderr,
// exit code, and any error or timeout information.
type ExecuteResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
	Timeout  bool   `json:"timeout,omitempty"`
}

// NewExecutor creates a new sandbox executor with the given options.
// It initializes the executor pool and configures the backend (Docker, Firecracker, or Daytona).
func NewExecutor(opts ...Option) (*Executor, error) {
	config := &Config{
		Backend:         BackendDocker,
		PoolSize:        3,
		MaxPoolSize:     10,
		DefaultTimeout:  30 * time.Second,
		DefaultCPU:      1000, // 1 core
		DefaultMemory:   512,  // 512 MB
		NetworkEnabled:  false,
		WorkspaceAccess: WorkspaceReadOnly,
	}

	for _, opt := range opts {
		opt(config)
	}

	if config.Backend == BackendDaytona {
		resolved, err := resolveDaytonaConfig(config.Daytona)
		if err != nil {
			return nil, err
		}
		config.Daytona = resolved
		client, err := newDaytonaClient(resolved)
		if err != nil {
			return nil, err
		}
		config.daytonaClient = client
	}

	// Check if Firecracker is available
	useFirecracker := false
	if config.Backend == BackendFirecracker {
		if _, err := exec.LookPath("firecracker"); err == nil {
			useFirecracker = true
		} else {
			// Fall back to Docker
			config.Backend = BackendDocker
		}
	}

	pool, err := NewPool(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	return &Executor{
		pool:            pool,
		useFirecracker:  useFirecracker,
		workspaceRoot:   config.WorkspaceRoot,
		workspaceAccess: config.WorkspaceAccess,
	}, nil
}

// Name returns the tool name.
func (e *Executor) Name() string {
	return "execute_code"
}

// Description returns the tool description.
func (e *Executor) Description() string {
	return "Execute code in a secure sandboxed environment. Supports Python 3, Node.js, Go, and Bash. Code runs isolated with no network access and resource limits."
}

// Schema returns the JSON schema for the tool parameters.
func (e *Executor) Schema() json.RawMessage {
	schema := `{
		"type": "object",
		"properties": {
			"language": {
				"type": "string",
				"enum": ["python", "nodejs", "go", "bash"],
				"description": "Programming language to execute"
			},
			"code": {
				"type": "string",
				"description": "The code to execute"
			},
			"stdin": {
				"type": "string",
				"description": "Optional standard input to provide to the program"
			},
			"files": {
				"type": "object",
				"additionalProperties": {
					"type": "string"
				},
				"description": "Optional additional files to mount (filename -> content)"
			},
			"timeout": {
				"type": "integer",
				"description": "Execution timeout in seconds (default: 30, max: 300)",
				"minimum": 1,
				"maximum": 300
			},
			"cpu_limit": {
				"type": "integer",
				"description": "CPU limit in millicores (default: 1000 = 1 core)"
			},
			"mem_limit": {
				"type": "integer",
				"description": "Memory limit in MB (default: 512)"
			}
		},
		"required": ["language", "code"]
	}`
	return json.RawMessage(schema)
}

// Execute runs the code in a sandboxed environment.
func (e *Executor) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var execParams ExecuteParams
	if err := json.Unmarshal(params, &execParams); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Validate language
	if !isValidLanguage(execParams.Language) {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Unsupported language: %s. Supported: python, nodejs, go, bash", execParams.Language),
			IsError: true,
		}, nil
	}

	// Set defaults
	if execParams.Timeout == 0 {
		execParams.Timeout = 30
	}
	if execParams.Timeout > 300 {
		execParams.Timeout = 300
	}
	if execParams.CPULimit == 0 {
		execParams.CPULimit = 1000
	}
	if execParams.MemLimit == 0 {
		execParams.MemLimit = 512
	}
	if execParams.WorkspaceAccess == "" {
		execParams.WorkspaceAccess = e.workspaceAccess
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(execParams.Timeout)*time.Second)
	defer cancel()

	result, err := e.executeCode(execCtx, &execParams)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Execution failed: %v", err),
			IsError: true,
		}, nil
	}

	// Format result
	output := formatExecutionResult(result)
	return &agent.ToolResult{
		Content: output,
		IsError: result.ExitCode != 0 || result.Error != "",
	}, nil
}

// executeCode runs the code using the pool.
func (e *Executor) executeCode(ctx context.Context, params *ExecuteParams) (*ExecuteResult, error) {
	// Get an executor from the pool
	executor, err := e.pool.Get(ctx, params.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to get executor: %w", err)
	}
	defer e.pool.Put(executor)

	// Prepare workspace
	workspace, err := prepareWorkspace(params, e.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	// Execute the code
	result, err := executor.Run(ctx, params, workspace)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &ExecuteResult{
				Error:   "Execution timeout",
				Timeout: true,
			}, nil
		}
		return nil, err
	}

	return result, nil
}

// prepareWorkspace creates a scratch directory with code and files.
func prepareWorkspace(params *ExecuteParams, workspaceRoot string) (string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
			return "", err
		}
	}

	workspace, err := os.MkdirTemp(workspaceRoot, "sandbox-*")
	if err != nil {
		return "", err
	}

	// Write main code file
	mainFile := getMainFilename(params.Language)
	if err := os.WriteFile(filepath.Join(workspace, mainFile), []byte(params.Code), 0644); err != nil {
		os.RemoveAll(workspace)
		return "", err
	}

	// Write additional files
	for filename, content := range params.Files {
		// Sanitize filename to prevent directory traversal
		filename = filepath.Base(filename)
		if err := os.WriteFile(filepath.Join(workspace, filename), []byte(content), 0644); err != nil {
			os.RemoveAll(workspace)
			return "", err
		}
	}

	// Write stdin if provided
	if params.Stdin != "" {
		if err := os.WriteFile(filepath.Join(workspace, "stdin.txt"), []byte(params.Stdin), 0644); err != nil {
			os.RemoveAll(workspace)
			return "", err
		}
	}

	return workspace, nil
}

// getMainFilename returns the filename for the code based on language.
func getMainFilename(language string) string {
	switch language {
	case "python":
		return "main.py"
	case "nodejs":
		return "main.js"
	case "go":
		return "main.go"
	case "bash":
		return "main.sh"
	default:
		return "main.txt"
	}
}

// isValidLanguage checks if the language is supported.
func isValidLanguage(language string) bool {
	switch language {
	case "python", "nodejs", "go", "bash":
		return true
	default:
		return false
	}
}

// formatExecutionResult formats the execution result for display.
func formatExecutionResult(result *ExecuteResult) string {
	var sb strings.Builder

	if result.Error != "" {
		sb.WriteString("Error: ")
		sb.WriteString(result.Error)
		sb.WriteString("\n")
	}

	if result.Timeout {
		sb.WriteString("Execution timed out\n")
	}

	if result.Stdout != "" {
		sb.WriteString("STDOUT:\n")
		sb.WriteString(result.Stdout)
		if !strings.HasSuffix(result.Stdout, "\n") {
			sb.WriteString("\n")
		}
	}

	if result.Stderr != "" {
		sb.WriteString("STDERR:\n")
		sb.WriteString(result.Stderr)
		if !strings.HasSuffix(result.Stderr, "\n") {
			sb.WriteString("\n")
		}
	}

	sb.WriteString(fmt.Sprintf("Exit code: %d", result.ExitCode))

	return sb.String()
}

// Close shuts down the executor pool and releases all resources.
func (e *Executor) Close() error {
	return e.pool.Close()
}

// RuntimeExecutor is the interface for language-specific code executors.
// Implementations handle running code in isolated environments for specific languages.
type RuntimeExecutor interface {
	Run(ctx context.Context, params *ExecuteParams, workspace string) (*ExecuteResult, error)
	Language() string
	Close() error
}

// dockerExecutor implements RuntimeExecutor using Docker.
type dockerExecutor struct {
	language       string
	image          string
	cpuLimit       int
	memLimit       int
	networkEnabled bool
}

// newDockerExecutor creates a new Docker-based executor.
func newDockerExecutor(language string, cpuLimit, memLimit int, networkEnabled bool) (*dockerExecutor, error) {
	image := getDockerImage(language)
	return &dockerExecutor{
		language:       language,
		image:          image,
		cpuLimit:       cpuLimit,
		memLimit:       memLimit,
		networkEnabled: networkEnabled,
	}, nil
}

// Run executes code in a Docker container.
func (d *dockerExecutor) Run(ctx context.Context, params *ExecuteParams, workspace string) (*ExecuteResult, error) {
	if params.WorkspaceAccess == WorkspaceNone {
		return d.runWithCopiedWorkspace(ctx, params, workspace)
	}

	// Build Docker command
	args := []string{"run", "--rm"}
	args = append(args, d.baseDockerArgs(params)...)

	// Mount workspace based on access mode
	switch params.WorkspaceAccess {
	case WorkspaceReadWrite:
		args = append(args, "-v", fmt.Sprintf("%s:/workspace:rw", workspace))
	case WorkspaceReadOnly, "":
		// Default to read-only for security
		args = append(args, "-v", fmt.Sprintf("%s:/workspace:ro", workspace))
	default:
		// Unknown mode - fall back to read-only
		args = append(args, "-v", fmt.Sprintf("%s:/workspace:ro", workspace))
	}
	args = append(args, "-w", "/workspace")

	// Add image and command
	args = append(args, d.image)
	args = append(args, getRunCommand(params.Language)...)

	return d.runDockerCommand(ctx, args, params.Stdin)
}

func (d *dockerExecutor) baseDockerArgs(params *ExecuteParams) []string {
	args := []string{}
	if !d.networkEnabled {
		args = append(args, "--network", "none")
	}
	args = append(args,
		"--cpus", fmt.Sprintf("%.2f", float64(params.CPULimit)/1000.0),
		"--memory", fmt.Sprintf("%dm", params.MemLimit),
		"--memory-swap", fmt.Sprintf("%dm", params.MemLimit), // No swap
		"--pids-limit", "100",
		"--ulimit", "nofile=1024:1024",
	)
	if params.Stdin != "" {
		args = append(args, "-i")
	}
	return args
}

func (d *dockerExecutor) runWithCopiedWorkspace(ctx context.Context, params *ExecuteParams, workspace string) (result *ExecuteResult, runErr error) {
	createArgs := []string{"create"}
	createArgs = append(createArgs, d.baseDockerArgs(params)...)
	createArgs = append(createArgs, "--tmpfs", "/workspace:rw", "-w", "/workspace")
	createArgs = append(createArgs, d.image)
	createArgs = append(createArgs, getRunCommand(params.Language)...)

	var createOut, createErr strings.Builder
	createCmd := exec.CommandContext(ctx, "docker", createArgs...)
	createCmd.Stdout = &createOut
	createCmd.Stderr = &createErr
	if err := createCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker create: %w: %s", err, strings.TrimSpace(createErr.String()))
	}

	containerID := strings.TrimSpace(createOut.String())
	if containerID == "" {
		return nil, errors.New("docker create returned empty container id")
	}

	defer func() {
		if err := exec.CommandContext(context.Background(), "docker", "rm", "-f", containerID).Run(); err != nil {
			if result == nil {
				return
			}
			if result.Stderr != "" {
				result.Stderr += "\n"
			}
			result.Stderr += fmt.Sprintf("docker cleanup error: %v", err)
		}
	}()

	copySrc := filepath.Join(workspace, ".")
	copyCmd := exec.CommandContext(ctx, "docker", "cp", copySrc, containerID+":/workspace")
	var copyErr strings.Builder
	copyCmd.Stderr = &copyErr
	if err := copyCmd.Run(); err != nil {
		return nil, fmt.Errorf("docker cp workspace: %w: %s", err, strings.TrimSpace(copyErr.String()))
	}

	startArgs := []string{"start", "-a"}
	if params.Stdin != "" {
		startArgs = append(startArgs, "-i")
	}
	startArgs = append(startArgs, containerID)

	result, runErr = d.runDockerCommand(ctx, startArgs, params.Stdin)
	return result, runErr
}

func (d *dockerExecutor) runDockerCommand(ctx context.Context, args []string, stdin string) (*ExecuteResult, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &ExecuteResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			result.Timeout = true
			result.Error = "Execution timeout"
		} else {
			result.Error = err.Error()
		}
	}

	return result, nil
}

// Language returns the language this executor handles.
func (d *dockerExecutor) Language() string {
	return d.language
}

// Close cleans up resources.
func (d *dockerExecutor) Close() error {
	return nil
}

// getDockerImage returns the Docker image for a language.
func getDockerImage(language string) string {
	switch language {
	case "python":
		return "python:3.11-alpine"
	case "nodejs":
		return "node:20-alpine"
	case "go":
		return "golang:1.24-alpine"
	case "bash":
		return "bash:5-alpine"
	default:
		return "alpine:latest"
	}
}

// getRunCommand returns the command to run code for a language.
func getRunCommand(language string) []string {
	switch language {
	case "python":
		return []string{"python", "main.py"}
	case "nodejs":
		return []string{"node", "main.js"}
	case "go":
		return []string{"sh", "-c", "go run main.go"}
	case "bash":
		return []string{"bash", "main.sh"}
	default:
		return []string{"cat", "main.txt"}
	}
}

// Config holds executor configuration including backend type, pool sizing,
// resource limits, and network access settings.
type Config struct {
	Backend         Backend
	PoolSize        int
	MaxPoolSize     int
	DefaultTimeout  time.Duration
	DefaultCPU      int
	DefaultMemory   int
	NetworkEnabled  bool
	Daytona         *DaytonaConfig
	WorkspaceRoot   string
	WorkspaceAccess WorkspaceAccessMode

	daytonaClient *daytonaClient
}

// Backend represents the sandbox backend technology (Docker, Firecracker, Daytona).
type Backend string

const (
	BackendFirecracker Backend = "firecracker"
	BackendDocker      Backend = "docker"
	BackendDaytona     Backend = "daytona"
)

// Option is a functional option for configuring the executor at creation time.
type Option func(*Config)

// WithBackend sets the sandbox backend.
func WithBackend(backend Backend) Option {
	return func(c *Config) {
		c.Backend = backend
	}
}

// WithPoolSize sets the initial pool size.
func WithPoolSize(size int) Option {
	return func(c *Config) {
		c.PoolSize = size
	}
}

// WithMaxPoolSize sets the maximum pool size.
func WithMaxPoolSize(size int) Option {
	return func(c *Config) {
		c.MaxPoolSize = size
	}
}

// WithDefaultTimeout sets the default execution timeout.
func WithDefaultTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.DefaultTimeout = timeout
	}
}

// WithDefaultCPU sets the default CPU limit in millicores.
func WithDefaultCPU(millicores int) Option {
	return func(c *Config) {
		c.DefaultCPU = millicores
	}
}

// WithDefaultMemory sets the default memory limit in MB.
func WithDefaultMemory(megabytes int) Option {
	return func(c *Config) {
		c.DefaultMemory = megabytes
	}
}

// WithNetworkEnabled enables network access in sandboxes.
func WithNetworkEnabled(enabled bool) Option {
	return func(c *Config) {
		c.NetworkEnabled = enabled
	}
}

// WithDaytonaConfig sets Daytona-specific backend configuration.
func WithDaytonaConfig(cfg DaytonaConfig) Option {
	return func(c *Config) {
		c.Daytona = &cfg
	}
}

// WithWorkspaceRoot sets the root directory for sandbox workspaces.
func WithWorkspaceRoot(root string) Option {
	return func(c *Config) {
		c.WorkspaceRoot = root
	}
}

// WithDefaultWorkspaceAccess sets the default workspace access mode.
func WithDefaultWorkspaceAccess(mode WorkspaceAccessMode) Option {
	return func(c *Config) {
		c.WorkspaceAccess = mode
	}
}
