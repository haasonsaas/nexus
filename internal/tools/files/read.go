package files

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Config controls filesystem tool defaults.
type Config struct {
	Workspace    string
	MaxReadBytes int
}

// ReadTool implements a safe file reader.
type ReadTool struct {
	resolver   Resolver
	maxReadLen int
}

// NewReadTool creates a read tool scoped to the workspace.
func NewReadTool(cfg Config) *ReadTool {
	limit := cfg.MaxReadBytes
	if limit <= 0 {
		limit = 200000
	}
	return &ReadTool{
		resolver:   Resolver{Root: cfg.Workspace},
		maxReadLen: limit,
	}
}

// Name returns the tool name.
func (t *ReadTool) Name() string {
	return "read"
}

// Description returns the tool description.
func (t *ReadTool) Description() string {
	return "Read a file from the workspace with optional offset and byte limit."
}

// Schema returns the JSON schema for the tool parameters.
func (t *ReadTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file (relative to workspace).",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Byte offset to start reading from (default: 0).",
				"minimum":     0,
			},
			"max_bytes": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum bytes to read (capped by tool default).",
				"minimum":     0,
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

// Execute reads a file with safety limits.
func (t *ReadTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		Path     string `json:"path"`
		Offset   int64  `json:"offset"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(input.Path) == "" {
		return toolError("path is required"), nil
	}
	if input.Offset < 0 {
		return toolError("offset must be >= 0"), nil
	}

	resolved, err := t.resolver.Resolve(input.Path)
	if err != nil {
		return toolError(err.Error()), nil
	}

	file, err := os.Open(resolved)
	if err != nil {
		return toolError(fmt.Sprintf("open file: %v", err)), nil
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return toolError(fmt.Sprintf("stat file: %v", err)), nil
	}

	if input.Offset > 0 {
		if _, err := file.Seek(input.Offset, io.SeekStart); err != nil {
			return toolError(fmt.Sprintf("seek file: %v", err)), nil
		}
	}

	limit := t.maxReadLen
	if input.MaxBytes > 0 && input.MaxBytes < limit {
		limit = input.MaxBytes
	}

	remaining := int64(limit)
	if size := info.Size(); size > 0 {
		remaining = size - input.Offset
		if remaining < 0 {
			remaining = 0
		}
		if remaining > int64(limit) {
			remaining = int64(limit)
		}
	}

	buf, err := io.ReadAll(io.LimitReader(file, remaining))
	if err != nil {
		return toolError(fmt.Sprintf("read file: %v", err)), nil
	}

	truncated := false
	if info.Size() > 0 && input.Offset+int64(len(buf)) < info.Size() {
		truncated = true
	}

	result := map[string]interface{}{
		"path":      input.Path,
		"content":   string(buf),
		"offset":    input.Offset,
		"bytes":     len(buf),
		"truncated": truncated,
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func toolError(message string) *agent.ToolResult {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return &agent.ToolResult{Content: message, IsError: true}
	}
	return &agent.ToolResult{Content: string(payload), IsError: true}
}
