package memorysearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Config configures memory search.
type Config struct {
	Directory     string
	MemoryFile    string
	WorkspacePath string
	MaxResults    int
	MaxSnippetLen int
}

// MemorySearchTool implements agent.Tool for searching memory files.
type MemorySearchTool struct {
	config Config
}

// NewMemorySearchTool creates a new memory search tool.
func NewMemorySearchTool(cfg *Config) *MemorySearchTool {
	config := Config{}
	if cfg != nil {
		config = *cfg
	}
	if config.MaxResults == 0 {
		config.MaxResults = 5
	}
	if config.MaxSnippetLen == 0 {
		config.MaxSnippetLen = 200
	}
	return &MemorySearchTool{config: config}
}

// Name returns the tool name.
func (t *MemorySearchTool) Name() string {
	return "memory_search"
}

// Description explains the tool.
func (t *MemorySearchTool) Description() string {
	return "Searches local memory files (MEMORY.md and memory logs) for a query."
}

// Schema defines the parameters for the tool.
func (t *MemorySearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "max_results": {"type": "integer", "description": "Max results to return"}
  },
  "required": ["query"]
}`)
}

// Execute runs the search.
func (t *MemorySearchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}, nil
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &agent.ToolResult{Content: "query is required", IsError: true}, nil
	}

	maxResults := t.config.MaxResults
	if input.MaxResults > 0 {
		maxResults = input.MaxResults
	}
	if maxResults <= 0 {
		maxResults = 5
	}

	files := t.resolveFiles()
	results := searchFiles(files, query, maxResults, t.config.MaxSnippetLen)

	payload, err := json.MarshalIndent(struct {
		Query   string         `json:"query"`
		Results []SearchResult `json:"results"`
	}{
		Query:   query,
		Results: results,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("failed to encode results: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func (t *MemorySearchTool) resolveFiles() []string {
	var files []string

	if memoryFile := strings.TrimSpace(t.config.MemoryFile); memoryFile != "" {
		files = append(files, resolvePath(t.config.WorkspacePath, memoryFile))
	}

	if dir := strings.TrimSpace(t.config.Directory); dir != "" {
		resolved := resolvePath(t.config.WorkspacePath, dir)
		entries, err := os.ReadDir(resolved)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
					files = append(files, filepath.Join(resolved, entry.Name()))
				}
			}
		}
	}

	return dedupe(files)
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) || strings.TrimSpace(base) == "" {
		return path
	}
	return filepath.Join(base, path)
}

type SearchResult struct {
	File    string `json:"file"`
	Snippet string `json:"snippet"`
	Matches int    `json:"matches"`
}

func searchFiles(files []string, query string, maxResults int, maxSnippetLen int) []SearchResult {
	var results []SearchResult
	needle := strings.ToLower(query)
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		matches, snippet := findMatches(string(content), needle, maxSnippetLen)
		if matches == 0 {
			continue
		}
		results = append(results, SearchResult{File: path, Snippet: snippet, Matches: matches})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Matches == results[j].Matches {
			return results[i].File < results[j].File
		}
		return results[i].Matches > results[j].Matches
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func findMatches(content string, needle string, maxSnippetLen int) (int, string) {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, needle)
	if idx == -1 {
		return 0, ""
	}
	count := strings.Count(lower, needle)
	runeIndex := utf8.RuneCountInString(lower[:idx])

	runeContent := []rune(content)
	start := runeIndex - 40
	if start < 0 {
		start = 0
	}
	end := runeIndex + len([]rune(needle)) + 40
	if end > len(runeContent) {
		end = len(runeContent)
	}
	snippet := strings.TrimSpace(string(runeContent[start:end]))
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runeContent) {
		snippet = snippet + "..."
	}
	if maxSnippetLen > 0 && len([]rune(snippet)) > maxSnippetLen {
		snippet = string([]rune(snippet)[:maxSnippetLen]) + "..."
	}
	return count, snippet
}

func dedupe(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
