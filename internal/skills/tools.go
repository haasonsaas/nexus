package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	exectools "github.com/haasonsaas/nexus/internal/tools/exec"
)

// SkillToolSpec defines a tool provided by a skill.
type SkillToolSpec struct {
	Name           string         `json:"name" yaml:"name"`
	Description    string         `json:"description" yaml:"description"`
	Schema         map[string]any `json:"schema" yaml:"schema"`
	Command        string         `json:"command" yaml:"command"`
	Script         string         `json:"script" yaml:"script"`
	TimeoutSeconds int            `json:"timeout_seconds" yaml:"timeout_seconds"`
	WorkingDir     string         `json:"cwd" yaml:"cwd"`
}

// BuildSkillTools creates executable tools from a skill definition.
func BuildSkillTools(skill *SkillEntry, execManager *exectools.Manager) []agent.Tool {
	if skill == nil || skill.Metadata == nil || len(skill.Metadata.Tools) == 0 || execManager == nil {
		return nil
	}

	tools := make([]agent.Tool, 0, len(skill.Metadata.Tools))
	for _, spec := range skill.Metadata.Tools {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		tools = append(tools, &skillTool{
			skill:   skill,
			spec:    spec,
			manager: execManager,
		})
	}
	return tools
}

type skillTool struct {
	skill   *SkillEntry
	spec    SkillToolSpec
	manager *exectools.Manager
}

func (t *skillTool) Name() string {
	return t.spec.Name
}

func (t *skillTool) Description() string {
	if t.spec.Description != "" {
		return t.spec.Description
	}
	return "Skill tool: " + t.spec.Name
}

func (t *skillTool) Schema() json.RawMessage {
	if t.spec.Schema == nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	payload, err := json.Marshal(t.spec.Schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

func (t *skillTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.manager == nil {
		return &agent.ToolResult{Content: "exec manager unavailable", IsError: true}, nil
	}
	command := strings.TrimSpace(t.spec.Command)
	script := strings.TrimSpace(t.spec.Script)
	if command == "" {
		command = "bash"
	}

	input := string(params)
	if script != "" {
		scriptPath := filepath.Join(t.skill.Path, script)
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			return &agent.ToolResult{Content: fmt.Sprintf("read script: %v", err), IsError: true}, nil
		}
		input = string(content)
	}

	env := map[string]string{
		"NEXUS_TOOL_INPUT": string(params),
		"NEXUS_TOOL_NAME":  t.spec.Name,
	}
	if t.skill != nil {
		env["NEXUS_SKILL_NAME"] = t.skill.Name
		env["NEXUS_SKILL_DIR"] = t.skill.Path
	}

	cwd := strings.TrimSpace(t.spec.WorkingDir)
	if cwd == "" {
		cwd = t.skill.Path
	}
	timeout := time.Duration(t.spec.TimeoutSeconds) * time.Second

	result, err := t.manager.RunCommand(ctx, command, cwd, env, input, timeout)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("encode result: %v", err), IsError: true}, nil
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}
