package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
)

// ExecTool runs shell commands.
type ExecTool struct {
	name    string
	manager *Manager
}

// NewExecTool creates an exec tool with the given name.
func NewExecTool(name string, manager *Manager) *ExecTool {
	if strings.TrimSpace(name) == "" {
		name = "exec"
	}
	return &ExecTool{name: name, manager: manager}
}

func (t *ExecTool) Name() string { return t.name }

func (t *ExecTool) Description() string {
	return "Run a shell command in the workspace (supports optional background execution)."
}

func (t *ExecTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Working directory (relative to workspace).",
			},
			"env": map[string]interface{}{
				"type":        "object",
				"description": "Environment overrides (string values).",
			},
			"input": map[string]interface{}{
				"type":        "string",
				"description": "Stdin content to pass to the command.",
			},
			"timeout_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (0 = no timeout).",
				"minimum":     0,
			},
			"background": map[string]interface{}{
				"type":        "boolean",
				"description": "Run in background and return a process id.",
			},
		},
		"required": []string{"command"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *ExecTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.manager == nil {
		return toolError("exec manager unavailable"), nil
	}
	var input struct {
		Command        string            `json:"command"`
		Cwd            string            `json:"cwd"`
		Env            map[string]string `json:"env"`
		Input          string            `json:"input"`
		TimeoutSeconds int               `json:"timeout_seconds"`
		Background     bool              `json:"background"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return toolError("command is required"), nil
	}

	timeout := time.Duration(input.TimeoutSeconds) * time.Second

	if input.Background {
		proc, err := t.manager.startBackground(ctx, command, input.Cwd, input.Env, input.Input, timeout)
		if err != nil {
			return toolError(err.Error()), nil
		}
		payload, _ := json.MarshalIndent(map[string]interface{}{
			"status":     "running",
			"process_id": proc.id,
		}, "", "  ")
		return &agent.ToolResult{Content: string(payload)}, nil
	}

	result, err := t.manager.runSync(ctx, command, input.Cwd, input.Env, input.Input, timeout)
	if err != nil {
		return toolError(err.Error()), nil
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

// ProcessTool inspects and manages background exec processes.
type ProcessTool struct {
	manager *Manager
}

// NewProcessTool creates a process tool.
func NewProcessTool(manager *Manager) *ProcessTool {
	return &ProcessTool{manager: manager}
}

func (t *ProcessTool) Name() string { return "process" }

func (t *ProcessTool) Description() string {
	return "Manage background exec processes (list, status, log, write, kill, remove)."
}

func (t *ProcessTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: list, status, log, write, kill, remove.",
			},
			"process_id": map[string]interface{}{
				"type":        "string",
				"description": "Process id for actions that target a process.",
			},
			"input": map[string]interface{}{
				"type":        "string",
				"description": "Input for write action.",
			},
		},
		"required": []string{"action"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *ProcessTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	_ = ctx
	if t.manager == nil {
		return toolError("process manager unavailable"), nil
	}
	var input struct {
		Action    string `json:"action"`
		ProcessID string `json:"process_id"`
		Input     string `json:"input"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return toolError("action is required"), nil
	}

	switch action {
	case "list":
		payload, _ := json.MarshalIndent(map[string]interface{}{"processes": t.manager.list()}, "", "  ")
		return &agent.ToolResult{Content: string(payload)}, nil
	case "status", "log", "write", "kill", "remove":
		if strings.TrimSpace(input.ProcessID) == "" {
			return toolError("process_id is required"), nil
		}
		proc, ok := t.manager.get(strings.TrimSpace(input.ProcessID))
		if !ok {
			return toolError("process not found"), nil
		}
		switch action {
		case "status":
			payload, _ := json.MarshalIndent(proc.info(), "", "  ")
			return &agent.ToolResult{Content: string(payload)}, nil
		case "log":
			payload, _ := json.MarshalIndent(map[string]interface{}{
				"stdout": proc.stdout.String(),
				"stderr": proc.stderr.String(),
				"status": proc.status(),
			}, "", "  ")
			return &agent.ToolResult{Content: string(payload)}, nil
		case "write":
			if proc.stdin == nil {
				return toolError("process stdin unavailable"), nil
			}
			if input.Input == "" {
				return toolError("input is required"), nil
			}
			if _, err := proc.stdin.Write([]byte(input.Input)); err != nil {
				return toolError(fmt.Sprintf("write stdin: %v", err)), nil
			}
			payload, _ := json.MarshalIndent(map[string]interface{}{
				"status": "written",
			}, "", "  ")
			return &agent.ToolResult{Content: string(payload)}, nil
		case "kill":
			if proc.cmd.Process == nil {
				return toolError("process not running"), nil
			}
			if err := proc.cmd.Process.Kill(); err != nil {
				return toolError(fmt.Sprintf("kill process: %v", err)), nil
			}
			payload, _ := json.MarshalIndent(map[string]interface{}{
				"status": "killed",
			}, "", "  ")
			return &agent.ToolResult{Content: string(payload)}, nil
		case "remove":
			if proc.status() == "running" {
				return toolError("process still running"), nil
			}
			if !t.manager.remove(proc.id) {
				return toolError("remove failed"), nil
			}
			payload, _ := json.MarshalIndent(map[string]interface{}{
				"status": "removed",
			}, "", "  ")
			return &agent.ToolResult{Content: string(payload)}, nil
		}
	}
	return toolError("unsupported action"), nil
}

func toolError(message string) *agent.ToolResult {
	payload, _ := json.Marshal(map[string]string{"error": message})
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
