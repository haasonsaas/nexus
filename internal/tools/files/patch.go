package files

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// ApplyPatchTool applies unified diffs to workspace files.
type ApplyPatchTool struct {
	resolver Resolver
}

// NewApplyPatchTool creates an apply_patch tool scoped to the workspace.
func NewApplyPatchTool(cfg Config) *ApplyPatchTool {
	return &ApplyPatchTool{resolver: Resolver{Root: cfg.Workspace}}
}

// Name returns the tool name.
func (t *ApplyPatchTool) Name() string {
	return "apply_patch"
}

// Description returns the tool description.
func (t *ApplyPatchTool) Description() string {
	return "Apply a unified diff patch to one or more files in the workspace."
}

// Schema returns the JSON schema for tool parameters.
func (t *ApplyPatchTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"patch": map[string]interface{}{
				"type":        "string",
				"description": "Unified diff patch (---/+++ headers required).",
			},
		},
		"required": []string{"patch"},
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return payload
}

// Execute applies a unified diff patch.
func (t *ApplyPatchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	_ = ctx
	var input struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return toolError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}
	if strings.TrimSpace(input.Patch) == "" {
		return toolError("patch is required"), nil
	}

	patches, err := parseUnifiedDiff(input.Patch)
	if err != nil {
		return toolError(err.Error()), nil
	}

	results := make([]map[string]interface{}, 0, len(patches))
	for _, patch := range patches {
		resolved, err := t.resolver.Resolve(patch.Path)
		if err != nil {
			return toolError(err.Error()), nil
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return toolError(fmt.Sprintf("read file: %v", err)), nil
		}
		updated, err := applyFilePatch(string(data), patch)
		if err != nil {
			return toolError(fmt.Sprintf("apply patch: %v", err)), nil
		}
		if err := os.WriteFile(resolved, []byte(updated.Content), 0o644); err != nil {
			return toolError(fmt.Sprintf("write file: %v", err)), nil
		}
		results = append(results, map[string]interface{}{
			"path":          patch.Path,
			"hunks":         len(patch.Hunks),
			"lines_added":   updated.Added,
			"lines_removed": updated.Removed,
		})
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"applied": results,
	}, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("encode result: %v", err)), nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

type filePatch struct {
	Path  string
	Hunks []hunk
}

type hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []string
}

type patchResult struct {
	Content string
	Added   int
	Removed int
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseUnifiedDiff(patch string) ([]filePatch, error) {
	lines := strings.Split(patch, "\n")
	var patches []filePatch
	var current *filePatch
	var currentHunk *hunk

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			continue
		case strings.HasPrefix(line, "--- "):
			oldPath := strings.TrimSpace(strings.TrimPrefix(line, "--- "))
			_ = oldPath
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
				return nil, fmt.Errorf("invalid patch: missing +++ header")
			}
			newPath := strings.TrimSpace(strings.TrimPrefix(lines[i+1], "+++ "))
			newPath = strings.TrimPrefix(strings.TrimPrefix(newPath, "b/"), "a/")
			patches = append(patches, filePatch{Path: newPath})
			current = &patches[len(patches)-1]
			currentHunk = nil
			i++
		case strings.HasPrefix(line, "@@ "):
			if current == nil {
				return nil, fmt.Errorf("invalid patch: hunk without file header")
			}
			match := hunkHeader.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("invalid patch: malformed hunk header")
			}
			oldStart := atoi(match[1])
			oldLines := atoiDefault(match[2], 1)
			newStart := atoi(match[3])
			newLines := atoiDefault(match[4], 1)
			h := hunk{
				OldStart: oldStart,
				OldLines: oldLines,
				NewStart: newStart,
				NewLines: newLines,
			}
			current.Hunks = append(current.Hunks, h)
			currentHunk = &current.Hunks[len(current.Hunks)-1]
		default:
			if currentHunk == nil {
				continue
			}
			if line == "\\ No newline at end of file" {
				continue
			}
			if line == "" {
				continue
			}
			prefix := line[:1]
			if prefix != " " && prefix != "+" && prefix != "-" {
				return nil, fmt.Errorf("invalid patch line: %s", line)
			}
			currentHunk.Lines = append(currentHunk.Lines, line)
		}
	}

	if len(patches) == 0 {
		return nil, fmt.Errorf("invalid patch: no file headers found")
	}
	return patches, nil
}

func applyFilePatch(content string, patch filePatch) (patchResult, error) {
	hadTrailing := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimSuffix(content, "\n")
	var lines []string
	if trimmed == "" {
		lines = []string{}
	} else {
		lines = strings.Split(trimmed, "\n")
	}

	added := 0
	removed := 0

	for _, h := range patch.Hunks {
		idx := h.OldStart - 1
		if idx < 0 {
			idx = 0
		}
		for _, line := range h.Lines {
			if line == "" {
				continue
			}
			prefix := line[:1]
			text := ""
			if len(line) > 1 {
				text = line[1:]
			}
			switch prefix {
			case " ":
				if idx >= len(lines) || lines[idx] != text {
					return patchResult{}, fmt.Errorf("context mismatch")
				}
				idx++
			case "-":
				if idx >= len(lines) || lines[idx] != text {
					return patchResult{}, fmt.Errorf("delete mismatch")
				}
				lines = append(lines[:idx], lines[idx+1:]...)
				removed++
			case "+":
				lines = append(lines[:idx], append([]string{text}, lines[idx:]...)...)
				idx++
				added++
			}
		}
	}

	result := strings.Join(lines, "\n")
	if hadTrailing {
		result += "\n"
	}
	return patchResult{Content: result, Added: added, Removed: removed}, nil
}

func atoi(value string) int {
	if value == "" {
		return 0
	}
	var out int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + int(r-'0')
	}
	return out
}

func atoiDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed := atoi(value)
	if parsed == 0 {
		return fallback
	}
	return parsed
}
