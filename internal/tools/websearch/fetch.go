package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// FetchConfig controls web_fetch defaults.
type FetchConfig struct {
	MaxChars int
}

// WebFetchTool implements a lightweight web fetch + extraction tool.
type WebFetchTool struct {
	config    FetchConfig
	extractor *ContentExtractor
}

// WebFetchOption customizes WebFetchTool construction.
type WebFetchOption func(*WebFetchTool)

// WithExtractor overrides the default content extractor (useful for tests).
func WithExtractor(extractor *ContentExtractor) WebFetchOption {
	return func(tool *WebFetchTool) {
		if extractor != nil {
			tool.extractor = extractor
		}
	}
}

// NewWebFetchTool creates a new web_fetch tool with defaults applied.
func NewWebFetchTool(config *FetchConfig, opts ...WebFetchOption) *WebFetchTool {
	cfg := FetchConfig{MaxChars: 10000}
	if config != nil {
		if config.MaxChars > 0 {
			cfg.MaxChars = config.MaxChars
		}
	}
	tool := &WebFetchTool{
		config:    cfg,
		extractor: NewContentExtractor(),
	}
	for _, opt := range opts {
		opt(tool)
	}
	return tool
}

// Name returns the tool name for registration with the agent runtime.
func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

// Description returns the tool description.
func (t *WebFetchTool) Description() string {
	return "Fetch and extract readable content from a URL without full browser automation."
}

// Schema returns the JSON schema for tool parameters.
func (t *WebFetchTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to fetch (http/https only)",
			},
			"extract_mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"markdown", "text"},
				"description": "Extraction mode (markdown or text). Default: markdown",
			},
			"max_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters to return (default: 10000)",
				"minimum":     0,
			},
		},
		"required": []string{"url"},
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return schemaBytes
}

// Execute runs the fetch + extraction with SSRF protection.
func (t *WebFetchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(params, &raw); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}
	url := readStringParam(raw, "url")
	if url == "" {
		return &agent.ToolResult{
			Content: "Missing required parameter: url",
			IsError: true,
		}, nil
	}

	extractMode := normalizeExtractMode(readStringParam(raw, "extract_mode", "extractMode"))
	maxChars := readIntParam(raw, "max_chars", "maxChars")
	limit := t.config.MaxChars
	if maxChars > 0 && (limit == 0 || maxChars < limit) {
		limit = maxChars
	}

	content, err := t.extractor.Extract(ctx, url)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Fetch failed: %v", err),
			IsError: true,
		}, nil
	}

	truncated := false
	if limit > 0 && len(content) > limit {
		content = content[:limit] + "..."
		truncated = true
	}

	result := map[string]interface{}{
		"url":          url,
		"extract_mode": extractMode,
		"content":      content,
	}
	if truncated {
		result["truncated"] = true
	}

	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to format response: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func normalizeExtractMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "text" {
		return "text"
	}
	return "markdown"
}

func readStringParam(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if str, ok := value.(string); ok {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}

func readIntParam(raw map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch v := value.(type) {
			case float64:
				return int(v)
			case int:
				return v
			case json.Number:
				if parsed, err := v.Int64(); err == nil {
					return int(parsed)
				}
			}
		}
	}
	return 0
}
