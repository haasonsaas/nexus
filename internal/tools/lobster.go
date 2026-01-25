package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LobsterTool provides integration with the Lobster workflow runtime.
// Lobster is a local-first workflow engine that supports typed JSON envelopes
// and resumable approval workflows.
//
// The tool executes the lobster CLI as a subprocess and returns structured output.
//
// Example usage:
//
//	tool := NewLobsterTool(LobsterConfig{})
//	result, err := tool.Execute(ctx, "run", LobsterParams{Pipeline: "my-pipeline"})
type LobsterTool struct {
	execPath       string
	workDir        string
	timeoutMs      int
	maxStdoutBytes int
}

// LobsterConfig holds configuration for the Lobster tool.
type LobsterConfig struct {
	// ExecPath is the path to the lobster executable (default: "lobster" in PATH)
	ExecPath string

	// WorkDir is the working directory for lobster commands (default: current directory)
	WorkDir string

	// TimeoutMs is the command timeout in milliseconds (default: 20000)
	TimeoutMs int

	// MaxStdoutBytes is the maximum stdout size in bytes (default: 512000)
	MaxStdoutBytes int
}

// LobsterParams holds parameters for a lobster command.
type LobsterParams struct {
	// Action is the lobster action: "run" or "resume"
	Action string `json:"action"`

	// Pipeline is the pipeline name for "run" action
	Pipeline string `json:"pipeline,omitempty"`

	// Token is the resume token for "resume" action
	Token string `json:"token,omitempty"`

	// Approve indicates approval decision for "resume" action
	Approve *bool `json:"approve,omitempty"`

	// Custom execution parameters
	LobsterPath    string `json:"lobsterPath,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	TimeoutMs      int    `json:"timeoutMs,omitempty"`
	MaxStdoutBytes int    `json:"maxStdoutBytes,omitempty"`
}

// LobsterEnvelope represents a lobster response envelope.
type LobsterEnvelope struct {
	OK               bool                    `json:"ok"`
	Status           string                  `json:"status,omitempty"` // "ok", "needs_approval", "cancelled"
	Output           []interface{}           `json:"output,omitempty"`
	RequiresApproval *LobsterApprovalRequest `json:"requiresApproval,omitempty"`
	Error            *LobsterError           `json:"error,omitempty"`
}

// LobsterApprovalRequest represents an approval request from a pipeline.
type LobsterApprovalRequest struct {
	Type        string        `json:"type"`
	Prompt      string        `json:"prompt"`
	Items       []interface{} `json:"items,omitempty"`
	ResumeToken string        `json:"resumeToken,omitempty"`
}

// LobsterError represents an error from lobster.
type LobsterError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
}

// LobsterResult contains the tool execution result.
type LobsterResult struct {
	Content  string           `json:"content"`
	Envelope *LobsterEnvelope `json:"envelope"`
}

// NewLobsterTool creates a new Lobster tool instance.
func NewLobsterTool(cfg LobsterConfig) *LobsterTool {
	execPath := cfg.ExecPath
	if execPath == "" {
		execPath = "lobster"
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			wd = "."
		}
		workDir = wd
	}

	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 20000
	}

	maxStdoutBytes := cfg.MaxStdoutBytes
	if maxStdoutBytes <= 0 {
		maxStdoutBytes = 512000
	}

	return &LobsterTool{
		execPath:       execPath,
		workDir:        workDir,
		timeoutMs:      timeoutMs,
		maxStdoutBytes: maxStdoutBytes,
	}
}

// Name returns the tool name.
func (t *LobsterTool) Name() string {
	return "lobster"
}

// Description returns the tool description.
func (t *LobsterTool) Description() string {
	return "Run Lobster pipelines as a local-first workflow runtime (typed JSON envelope + resumable approvals)."
}

// Schema returns the JSON schema for tool parameters.
func (t *LobsterTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"run", "resume"},
				"description": "The action to perform: 'run' to execute a pipeline, 'resume' to continue an approval workflow",
			},
			"pipeline": map[string]interface{}{
				"type":        "string",
				"description": "Pipeline name for 'run' action",
			},
			"token": map[string]interface{}{
				"type":        "string",
				"description": "Resume token for 'resume' action",
			},
			"approve": map[string]interface{}{
				"type":        "boolean",
				"description": "Approval decision for 'resume' action",
			},
			"lobsterPath": map[string]interface{}{
				"type":        "string",
				"description": "Custom path to lobster executable (must be absolute)",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for the command",
			},
			"timeoutMs": map[string]interface{}{
				"type":        "number",
				"description": "Command timeout in milliseconds",
			},
			"maxStdoutBytes": map[string]interface{}{
				"type":        "number",
				"description": "Maximum stdout size in bytes",
			},
		},
		"required": []string{"action"},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// Execute runs a lobster command.
func (t *LobsterTool) Execute(ctx context.Context, id string, params json.RawMessage) (string, error) {
	var p LobsterParams
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("lobster: invalid parameters: %w", err)
	}

	if p.Action == "" {
		return "", errors.New("lobster: action is required")
	}

	// Resolve execution parameters
	execPath := t.execPath
	if p.LobsterPath != "" {
		if !filepath.IsAbs(p.LobsterPath) {
			return "", errors.New("lobster: lobsterPath must be an absolute path")
		}
		execPath = p.LobsterPath
	}

	cwd := t.workDir
	if p.Cwd != "" {
		cwd = p.Cwd
	}

	timeoutMs := t.timeoutMs
	if p.TimeoutMs > 0 {
		timeoutMs = p.TimeoutMs
	}

	maxStdoutBytes := t.maxStdoutBytes
	if p.MaxStdoutBytes > 0 {
		maxStdoutBytes = p.MaxStdoutBytes
	}

	// Build command arguments
	var args []string
	switch p.Action {
	case "run":
		if p.Pipeline == "" {
			return "", errors.New("lobster: pipeline is required for 'run' action")
		}
		args = []string{"run", "--mode", "tool", p.Pipeline}

	case "resume":
		if p.Token == "" {
			return "", errors.New("lobster: token is required for 'resume' action")
		}
		if p.Approve == nil {
			return "", errors.New("lobster: approve is required for 'resume' action")
		}
		approveStr := "no"
		if *p.Approve {
			approveStr = "yes"
		}
		args = []string{"resume", "--token", p.Token, "--approve", approveStr}

	default:
		return "", fmt.Errorf("lobster: unknown action: %s", p.Action)
	}

	// Execute subprocess
	stdout, err := t.runSubprocess(ctx, execPath, args, cwd, timeoutMs, maxStdoutBytes)
	if err != nil {
		return "", err
	}

	// Parse envelope
	envelope, err := t.parseEnvelope(stdout)
	if err != nil {
		return "", err
	}

	// Format result
	result := LobsterResult{
		Content:  string(stdout),
		Envelope: envelope,
	}

	formatted, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("lobster: failed to format result: %w", err)
	}
	return string(formatted), nil
}

// runSubprocess executes lobster as a subprocess.
func (t *LobsterTool) runSubprocess(ctx context.Context, execPath string, args []string, cwd string, timeoutMs, maxStdoutBytes int) ([]byte, error) {
	// Create timeout context
	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath, args...)
	cmd.Dir = cwd

	// Set environment
	env := os.Environ()
	env = append(env, "LOBSTER_MODE=tool")

	// Remove NODE_OPTIONS with --inspect (conflicts with subprocess)
	filteredEnv := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "NODE_OPTIONS=") && strings.Contains(e, "--inspect") {
			continue
		}
		filteredEnv = append(filteredEnv, e)
	}
	cmd.Env = filteredEnv

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lobster: failed to start: %w", err)
	}

	// Monitor stdout size
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Wait for completion or timeout
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return nil, fmt.Errorf("lobster: subprocess timed out (kill failed: %w)", err)
			}
		}
		return nil, fmt.Errorf("lobster: subprocess timed out")
	case err := <-done:
		if stdout.Len() > maxStdoutBytes {
			return nil, fmt.Errorf("lobster: output exceeded maxStdoutBytes (%d)", maxStdoutBytes)
		}

		if err != nil {
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = stdout.String()
			}
			return nil, fmt.Errorf("lobster: failed (%v): %s", err, strings.TrimSpace(errMsg))
		}

		return stdout.Bytes(), nil
	}
}

// parseEnvelope parses the lobster JSON envelope.
func (t *LobsterTool) parseEnvelope(data []byte) (*LobsterEnvelope, error) {
	var envelope LobsterEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("lobster: invalid JSON response: %w", err)
	}
	return &envelope, nil
}
