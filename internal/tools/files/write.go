package files

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// WriteTool implements file writes within the workspace.
type WriteTool struct {
	resolver Resolver
}

// NewWriteTool creates a write tool scoped to the workspace.
func NewWriteTool(cfg Config) *WriteTool {
	return &WriteTool{resolver: Resolver{Root: cfg.Workspace}}
}

// Name returns the tool name.
func (t *WriteTool) Name() string {
	return "write"
}

// Description returns the tool description.
func (t *WriteTool) Description() string {
	return "Write content to a file in the workspace (overwrites by default)."
}

// Schema returns the JSON schema for the tool parameters.
func (t *WriteTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to write (relative to workspace).",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "File contents to write.",
			},
			"append": map[string]interface{}{
				"type":        "boolean",
				"description": "Append instead of overwrite (default: false).",
			},
		},
		"required": []string{"path", "content"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

// Execute writes file contents.
func (t *WriteTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	_ = ctx
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(input.Path) == "" {
		return toolError("path is required"), nil
	}

	resolved, err := t.resolver.Resolve(input.Path)
	if err != nil {
		return toolError(err.Error()), nil
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return toolError(fmt.Sprintf("create directory: %v", err)), nil
	}

	flags := os.O_CREATE | os.O_WRONLY
	if input.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(resolved, flags, 0o644)
	if err != nil {
		return toolError(fmt.Sprintf("open file: %v", err)), nil
	}
	defer file.Close()

	n, err := file.WriteString(input.Content)
	if err != nil {
		return toolError(fmt.Sprintf("write file: %v", err)), nil
	}

	result := map[string]interface{}{
		"path":          input.Path,
		"bytes_written": n,
		"append":        input.Append,
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}
