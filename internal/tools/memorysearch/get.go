package memorysearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// MemoryGetTool reads snippets from memory files.
type MemoryGetTool struct {
	config *Config
}

// NewMemoryGetTool creates a new memory_get tool.
func NewMemoryGetTool(config *Config) *MemoryGetTool {
	return &MemoryGetTool{config: config}
}

// Name returns the tool name.
func (t *MemoryGetTool) Name() string {
	return "memory_get"
}

// Description returns the tool description.
func (t *MemoryGetTool) Description() string {
	return "Read a snippet from MEMORY.md or memory/*.md by line range."
}

// Schema returns the JSON schema for tool parameters.
func (t *MemoryGetTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Memory file path (relative to workspace).",
			},
			"from": map[string]interface{}{
				"type":        "integer",
				"description": "1-based start line (default: 1).",
				"minimum":     1,
			},
			"lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of lines to return (default: 50).",
				"minimum":     1,
			},
		},
		"required": []string{"path"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

// Execute reads the requested memory snippet.
func (t *MemoryGetTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	_ = ctx
	var input struct {
		Path  string `json:"path"`
		From  int    `json:"from"`
		Lines int    `json:"lines"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("Invalid parameters: %v", err), IsError: true}, nil
	}
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return &agent.ToolResult{Content: "path is required", IsError: true}, nil
	}
	from := input.From
	if from <= 0 {
		from = 1
	}
	lines := input.Lines
	if lines <= 0 {
		lines = 50
	}

	resolved, err := t.resolveMemoryPath(path)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("read file: %v", err), IsError: true}, nil
	}

	all := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := from - 1
	if start >= len(all) {
		return &agent.ToolResult{Content: "", IsError: false}, nil
	}
	end := start + lines
	if end > len(all) {
		end = len(all)
	}

	result := map[string]interface{}{
		"path":  path,
		"from":  from,
		"lines": lines,
		"text":  strings.Join(all[start:end], "\n"),
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("encode result: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func (t *MemoryGetTool) resolveMemoryPath(path string) (string, error) {
	if t.config == nil {
		return "", fmt.Errorf("memory search config not available")
	}
	root := strings.TrimSpace(t.config.WorkspacePath)
	if root == "" {
		root = "."
	}
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Join(root, path)
	}
	resolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	allowed := []string{}
	if t.config.MemoryFile != "" {
		allowed = append(allowed, filepath.Join(root, t.config.MemoryFile))
	}
	if t.config.Directory != "" {
		allowed = append(allowed, filepath.Join(root, t.config.Directory))
	}
	for _, base := range allowed {
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(baseAbs, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("path is outside memory directories")
}
