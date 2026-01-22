// Package rag provides RAG (Retrieval-Augmented Generation) tools for agents.
package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/pkg/models"
)

// SearchTool implements agent.Tool for semantic document search.
// It uses vector embeddings to find relevant document chunks from indexed documents.
type SearchTool struct {
	manager *index.Manager
	config  SearchToolConfig
}

// SearchToolConfig configures the document search tool behavior including
// result limits, similarity thresholds, and content formatting.
type SearchToolConfig struct {
	// DefaultLimit is the default number of results to return.
	// Default: 5
	DefaultLimit int

	// MaxLimit is the maximum number of results allowed.
	// Default: 20
	MaxLimit int

	// DefaultThreshold is the default similarity threshold.
	// Default: 0.7
	DefaultThreshold float32

	// IncludeContent includes full chunk content in results.
	// Default: true
	IncludeContent bool

	// MaxContentLength truncates content to this length.
	// 0 means no truncation.
	// Default: 500
	MaxContentLength int
}

// DefaultSearchToolConfig returns sensible defaults for the search tool
// with 5 results, 0.7 threshold, and 500 character content limit.
func DefaultSearchToolConfig() SearchToolConfig {
	return SearchToolConfig{
		DefaultLimit:     5,
		MaxLimit:         20,
		DefaultThreshold: 0.7,
		IncludeContent:   true,
		MaxContentLength: 500,
	}
}

// NewSearchTool creates a new document search tool with the given index manager
// and configuration, applying defaults for any unset values.
func NewSearchTool(manager *index.Manager, cfg *SearchToolConfig) *SearchTool {
	config := DefaultSearchToolConfig()
	if cfg != nil {
		if cfg.DefaultLimit > 0 {
			config.DefaultLimit = cfg.DefaultLimit
		}
		if cfg.MaxLimit > 0 {
			config.MaxLimit = cfg.MaxLimit
		}
		if cfg.DefaultThreshold > 0 {
			config.DefaultThreshold = cfg.DefaultThreshold
		}
		if cfg.MaxContentLength > 0 {
			config.MaxContentLength = cfg.MaxContentLength
		}
		config.IncludeContent = cfg.IncludeContent
	}

	return &SearchTool{
		manager: manager,
		config:  config,
	}
}

// Name returns the tool name.
func (t *SearchTool) Name() string {
	return "document_search"
}

// Description returns the tool description.
func (t *SearchTool) Description() string {
	return "Searches indexed documents for relevant information using semantic similarity. Use this to find information from uploaded documents, knowledge bases, or reference materials."
}

// Schema returns the JSON schema for tool parameters.
func (t *SearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query to find relevant documents"
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of results to return (default: 5, max: 20)"
    },
    "threshold": {
      "type": "number",
      "description": "Minimum similarity score from 0 to 1 (default: 0.7)"
    },
    "tags": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Filter results to documents with these tags"
    },
    "scope": {
      "type": "string",
      "enum": ["global", "agent", "session", "channel"],
      "description": "Limit search to a specific scope (default: global)"
    },
    "scope_id": {
      "type": "string",
      "description": "Scope identifier (agent_id, session_id, or channel_id). If omitted, uses the current session context when available."
    }
  },
  "required": ["query"]
}`)
}

// searchInput represents the tool input parameters.
type searchInput struct {
	Query     string   `json:"query"`
	Limit     int      `json:"limit,omitempty"`
	Threshold float32  `json:"threshold,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	ScopeID   string   `json:"scope_id,omitempty"`
}

// searchOutput represents a single search result.
type searchOutput struct {
	DocumentName string  `json:"document_name"`
	Source       string  `json:"source,omitempty"`
	Section      string  `json:"section,omitempty"`
	Content      string  `json:"content"`
	Score        float32 `json:"score"`
}

// Execute runs the document search with the given query parameters,
// returning matching chunks with their similarity scores.
func (t *SearchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input searchInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Validate query
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &agent.ToolResult{
			Content: "Query is required",
			IsError: true,
		}, nil
	}

	// Apply defaults and limits
	limit := input.Limit
	if limit <= 0 {
		limit = t.config.DefaultLimit
	}
	if limit > t.config.MaxLimit {
		limit = t.config.MaxLimit
	}

	threshold := input.Threshold
	if threshold <= 0 {
		threshold = t.config.DefaultThreshold
	}
	if threshold > 1 {
		threshold = 1
	}

	// Parse scope
	scope := models.DocumentScopeGlobal
	switch strings.ToLower(input.Scope) {
	case "agent":
		scope = models.DocumentScopeAgent
	case "session":
		scope = models.DocumentScopeSession
	case "channel":
		scope = models.DocumentScopeChannel
	}

	scopeID := strings.TrimSpace(input.ScopeID)
	if scope != models.DocumentScopeGlobal && scopeID == "" {
		if session := agent.SessionFromContext(ctx); session != nil {
			switch scope {
			case models.DocumentScopeAgent:
				scopeID = session.AgentID
			case models.DocumentScopeSession:
				scopeID = session.ID
			case models.DocumentScopeChannel:
				scopeID = session.ChannelID
			}
		}
	}
	if scope != models.DocumentScopeGlobal && scopeID == "" {
		return &agent.ToolResult{
			Content: "Scope requires scope_id or active session context",
			IsError: true,
		}, nil
	}

	// Perform search
	req := &models.DocumentSearchRequest{
		Query:     query,
		Scope:     scope,
		ScopeID:   scopeID,
		Limit:     limit,
		Threshold: threshold,
		Tags:      input.Tags,
	}

	resp, err := t.manager.Search(ctx, req)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Search failed: %v", err),
			IsError: true,
		}, nil
	}

	// Ensure deterministic ordering if the store response is unordered.
	if len(resp.Results) > 1 {
		sort.SliceStable(resp.Results, func(i, j int) bool {
			if resp.Results[i] == nil && resp.Results[j] == nil {
				return false
			}
			if resp.Results[i] == nil {
				return false
			}
			if resp.Results[j] == nil {
				return true
			}
			if resp.Results[i].Score == resp.Results[j].Score {
				if resp.Results[i].Chunk == nil || resp.Results[j].Chunk == nil {
					return resp.Results[i].Chunk != nil
				}
				return resp.Results[i].Chunk.ID < resp.Results[j].Chunk.ID
			}
			return resp.Results[i].Score > resp.Results[j].Score
		})
	}

	// Format results
	if len(resp.Results) == 0 {
		return &agent.ToolResult{
			Content: fmt.Sprintf("No relevant documents found for query: %q", query),
		}, nil
	}

	results := make([]searchOutput, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r == nil || r.Chunk == nil {
			continue
		}
		output := searchOutput{
			DocumentName: r.Chunk.Metadata.DocumentName,
			Source:       r.Chunk.Metadata.DocumentSource,
			Section:      r.Chunk.Metadata.Section,
			Score:        r.Score,
		}

		if t.config.IncludeContent {
			content := r.Chunk.Content
			if t.config.MaxContentLength > 0 && len(content) > t.config.MaxContentLength {
				content = content[:t.config.MaxContentLength] + "..."
			}
			output.Content = content
		}

		results = append(results, output)
	}

	if len(results) == 0 {
		return &agent.ToolResult{
			Content: fmt.Sprintf("No relevant documents found for query: %q", query),
		}, nil
	}

	// Format output
	outputJSON, err := json.MarshalIndent(struct {
		Query   string         `json:"query"`
		Count   int            `json:"count"`
		Results []searchOutput `json:"results"`
	}{
		Query:   query,
		Count:   len(results),
		Results: results,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to format results: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(outputJSON),
	}, nil
}
