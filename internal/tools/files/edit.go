package files

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// EditTool implements in-place text edits on files.
type EditTool struct {
	resolver Resolver
}

// NewEditTool creates an edit tool scoped to the workspace.
func NewEditTool(cfg Config) *EditTool {
	return &EditTool{resolver: Resolver{Root: cfg.Workspace}}
}

// Name returns the tool name.
func (t *EditTool) Name() string {
	return "edit"
}

// Description returns the tool description.
func (t *EditTool) Description() string {
	return "Apply one or more find/replace edits to a file in the workspace."
}

// Schema returns the JSON schema for the tool parameters.
func (t *EditTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to edit (relative to workspace).",
			},
			"edits": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"old_text": map[string]interface{}{
							"type":        "string",
							"description": "Text to replace.",
						},
						"new_text": map[string]interface{}{
							"type":        "string",
							"description": "Replacement text.",
						},
						"replace_all": map[string]interface{}{
							"type":        "boolean",
							"description": "Replace all occurrences (default: false).",
						},
					},
					"required": []string{"old_text", "new_text"},
				},
			},
		},
		"required": []string{"path", "edits"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

// Execute applies edits to the file.
func (t *EditTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	_ = ctx
	var input struct {
		Path  string `json:"path"`
		Edits []struct {
			OldText    string `json:"old_text"`
			NewText    string `json:"new_text"`
			ReplaceAll bool   `json:"replace_all"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(input.Path) == "" {
		return toolError("path is required"), nil
	}
	if len(input.Edits) == 0 {
		return toolError("edits are required"), nil
	}

	resolved, err := t.resolver.Resolve(input.Path)
	if err != nil {
		return toolError(err.Error()), nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return toolError(fmt.Sprintf("read file: %v", err)), nil
	}

	content := string(data)
	replacements := 0
	for _, edit := range input.Edits {
		if edit.OldText == "" {
			return toolError("old_text is required"), nil
		}
		if !strings.Contains(content, edit.OldText) {
			return toolError("old_text not found"), nil
		}
		if edit.ReplaceAll {
			count := strings.Count(content, edit.OldText)
			content = strings.ReplaceAll(content, edit.OldText, edit.NewText)
			replacements += count
		} else {
			content = strings.Replace(content, edit.OldText, edit.NewText, 1)
			replacements++
		}
	}

	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return toolError(fmt.Sprintf("write file: %v", err)), nil
	}

	result := map[string]interface{}{
		"path":         input.Path,
		"replacements": replacements,
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}
