package vectormemory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Searcher defines the subset of memory manager behavior used by the search tool.
type Searcher interface {
	Search(ctx context.Context, req *models.SearchRequest) (*models.SearchResponse, error)
	SearchHierarchical(ctx context.Context, req *memory.HierarchyRequest) (*models.SearchResponse, error)
}

// SearchTool searches vector memory for relevant context.
type SearchTool struct {
	manager         Searcher
	config          *memory.Config
	maxContentChars int
}

// NewSearchTool creates a new vector memory search tool.
func NewSearchTool(manager Searcher, cfg *memory.Config) *SearchTool {
	return &SearchTool{
		manager:         manager,
		config:          cfg,
		maxContentChars: 500,
	}
}

// Name returns the tool name.
func (t *SearchTool) Name() string {
	return "vector_memory_search"
}

// Description describes the tool.
func (t *SearchTool) Description() string {
	return "Searches vector memory for relevant context across session, agent, channel, or global scopes."
}

// Schema defines the tool parameters.
func (t *SearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query to find relevant memories"},
    "scope": {
      "type": "string",
      "enum": ["hierarchy", "session", "channel", "agent", "global", "all"],
      "description": "Scope to search within (default: hierarchy when enabled, otherwise config default)"
    },
    "scope_id": {"type": "string", "description": "Scope identifier if required"},
    "limit": {"type": "integer", "description": "Maximum number of results"},
    "threshold": {"type": "number", "description": "Minimum similarity score from 0 to 1"},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "Filter results to entries with matching tags"}
  },
  "required": ["query"]
}`)
}

type searchInput struct {
	Query     string   `json:"query"`
	Scope     string   `json:"scope"`
	ScopeID   string   `json:"scope_id"`
	Limit     int      `json:"limit"`
	Threshold float32  `json:"threshold"`
	Tags      []string `json:"tags"`
}

type searchResult struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Score     float32   `json:"score"`
	Source    string    `json:"source,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	SessionID string    `json:"session_id,omitempty"`
	ChannelID string    `json:"channel_id,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
}

// Execute runs the vector memory search tool.
func (t *SearchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.manager == nil {
		return &agent.ToolResult{Content: "vector memory is unavailable", IsError: true}, nil
	}

	var input searchInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}, nil
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &agent.ToolResult{Content: "query is required", IsError: true}, nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultLimitFromConfig(t.config)
	}
	threshold := input.Threshold
	if threshold <= 0 {
		threshold = defaultThresholdFromConfig(t.config)
	}

	scopeRaw := strings.ToLower(strings.TrimSpace(input.Scope))
	useHierarchy := false
	if scopeRaw == "" {
		if t.config != nil && t.config.Search.Hierarchy.Enabled {
			useHierarchy = true
		} else {
			scopeRaw = defaultScopeFromConfig(t.config)
		}
	} else if scopeRaw == "hierarchy" {
		useHierarchy = true
	}

	var (
		resp *models.SearchResponse
		err  error
	)
	session := agent.SessionFromContext(ctx)
	if useHierarchy {
		req := &memory.HierarchyRequest{
			Query:     query,
			Limit:     limit,
			Threshold: threshold,
		}
		if session != nil {
			req.SessionID = session.ID
			req.ChannelID = session.ChannelID
			req.AgentID = session.AgentID
		}
		resp, err = t.manager.SearchHierarchical(ctx, req)
		scopeRaw = "hierarchy"
	} else {
		scope, scopeID, scopeErr := resolveScope(scopeRaw, input.ScopeID, session, defaultScopeFromConfig(t.config))
		if scopeErr != nil {
			return &agent.ToolResult{Content: scopeErr.Error(), IsError: true}, nil
		}
		resp, err = t.manager.Search(ctx, &models.SearchRequest{
			Query:     query,
			Scope:     scope,
			ScopeID:   scopeID,
			Limit:     limit,
			Threshold: threshold,
		})
	}
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}

	results := buildSearchResults(resp, input.Tags, t.maxContentChars)

	payload, err := json.MarshalIndent(struct {
		Query   string         `json:"query"`
		Scope   string         `json:"scope"`
		Results []searchResult `json:"results"`
	}{
		Query:   query,
		Scope:   scopeRaw,
		Results: results,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("failed to encode results: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func buildSearchResults(resp *models.SearchResponse, tags []string, maxLen int) []searchResult {
	if resp == nil || len(resp.Results) == 0 {
		return nil
	}
	filter := tagFilter(tags)
	results := make([]searchResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r == nil || r.Entry == nil {
			continue
		}
		if !filter(r.Entry.Metadata.Tags) {
			continue
		}
		content := strings.TrimSpace(r.Entry.Content)
		if maxLen > 0 && len(content) > maxLen {
			content = content[:maxLen] + "...[truncated]"
		}
		results = append(results, searchResult{
			ID:        r.Entry.ID,
			Content:   content,
			Score:     r.Score,
			Source:    r.Entry.Metadata.Source,
			Tags:      r.Entry.Metadata.Tags,
			CreatedAt: r.Entry.CreatedAt,
			SessionID: r.Entry.SessionID,
			ChannelID: r.Entry.ChannelID,
			AgentID:   r.Entry.AgentID,
		})
	}
	return results
}

func resolveScope(scopeRaw, scopeID string, session *models.Session, defaultScope string) (models.MemoryScope, string, error) {
	if scopeRaw == "" || scopeRaw == "default" {
		if strings.TrimSpace(defaultScope) == "" {
			defaultScope = "session"
		}
		scopeRaw = strings.ToLower(strings.TrimSpace(defaultScope))
	}
	switch scopeRaw {
	case "session":
		if scopeID == "" && session != nil {
			scopeID = session.ID
		}
		if scopeID == "" {
			return "", "", fmt.Errorf("scope_id is required for session scope")
		}
		return models.ScopeSession, scopeID, nil
	case "channel":
		if scopeID == "" && session != nil {
			scopeID = session.ChannelID
		}
		if scopeID == "" {
			return "", "", fmt.Errorf("scope_id is required for channel scope")
		}
		return models.ScopeChannel, scopeID, nil
	case "agent":
		if scopeID == "" && session != nil {
			scopeID = session.AgentID
		}
		if scopeID == "" {
			return "", "", fmt.Errorf("scope_id is required for agent scope")
		}
		return models.ScopeAgent, scopeID, nil
	case "global":
		return models.ScopeGlobal, "", nil
	case "all":
		return models.ScopeAll, "", nil
	default:
		return "", "", fmt.Errorf("unsupported scope %q", scopeRaw)
	}
}

func defaultLimitFromConfig(cfg *memory.Config) int {
	if cfg != nil && cfg.Search.DefaultLimit > 0 {
		return cfg.Search.DefaultLimit
	}
	return 10
}

func defaultThresholdFromConfig(cfg *memory.Config) float32 {
	if cfg != nil && cfg.Search.DefaultThreshold > 0 {
		return cfg.Search.DefaultThreshold
	}
	return 0.7
}

func defaultScopeFromConfig(cfg *memory.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.Search.DefaultScope) != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Search.DefaultScope))
	}
	return "session"
}

func tagFilter(tags []string) func([]string) bool {
	if len(tags) == 0 {
		return func(_ []string) bool { return true }
	}
	allowed := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		allowed[tag] = struct{}{}
	}
	return func(entryTags []string) bool {
		if len(allowed) == 0 {
			return true
		}
		for _, tag := range entryTags {
			if _, ok := allowed[strings.ToLower(strings.TrimSpace(tag))]; ok {
				return true
			}
		}
		return false
	}
}
