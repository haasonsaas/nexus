package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/pkg/models"
)

// HierarchyRequest defines inputs for hierarchical memory search.
type HierarchyRequest struct {
	Query     string
	Limit     int
	Threshold float32
	Filters   map[string]any

	SessionID string
	ChannelID string
	AgentID   string
}

// SearchHierarchical searches across scopes and merges results by weighted score.
func (m *Manager) SearchHierarchical(ctx context.Context, req *HierarchyRequest) (*models.SearchResponse, error) {
	if m == nil || m.backend == nil {
		return nil, fmt.Errorf("memory manager not initialized (set vector_memory.enabled)")
	}
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	start := time.Now()

	limit := req.Limit
	if limit <= 0 {
		limit = m.config.Search.Hierarchy.MaxResults
		if limit <= 0 {
			limit = m.config.Search.DefaultLimit
		}
	}
	threshold := req.Threshold
	if threshold <= 0 {
		threshold = m.config.Search.DefaultThreshold
	}

	cacheKey := fmt.Sprintf("hierarchy:%s", req.Query)
	queryEmbed, ok := m.cache.get(cacheKey)
	if !ok {
		embed, err := m.embedder.Embed(ctx, req.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to embed query: %w", err)
		}
		queryEmbed = embed
		m.cache.set(cacheKey, embed)
	}

	scopes := m.config.Search.Hierarchy.Scopes
	if len(scopes) == 0 {
		scopes = []string{"session", "agent", "channel", "global"}
	}

	type scored struct {
		entry *models.MemoryEntry
		score float32
	}
	results := make(map[string]scored)

	for _, scopeName := range scopes {
		scope := models.MemoryScope(strings.ToLower(strings.TrimSpace(scopeName)))
		scopeID := ""
		switch scope {
		case models.ScopeSession:
			scopeID = req.SessionID
		case models.ScopeChannel:
			scopeID = req.ChannelID
		case models.ScopeAgent:
			scopeID = req.AgentID
		case models.ScopeGlobal:
			scopeID = ""
		case models.ScopeAll:
			scopeID = ""
		default:
			continue
		}
		if scope != models.ScopeGlobal && scope != models.ScopeAll && scopeID == "" {
			continue
		}

		opts := &backend.SearchOptions{
			Scope:     scope,
			ScopeID:   scopeID,
			Limit:     limit,
			Threshold: threshold,
			Filters:   req.Filters,
		}
		found, err := m.backend.Search(ctx, queryEmbed, opts)
		if err != nil {
			return nil, fmt.Errorf("search failed for scope %s: %w", scope, err)
		}
		weight := float32(1.0)
		if w, ok := m.config.Search.Hierarchy.Weights[string(scope)]; ok {
			weight = w
		}
		for _, res := range found {
			if res == nil || res.Entry == nil {
				continue
			}
			score := res.Score * weight
			if existing, ok := results[res.Entry.ID]; ok {
				if score > existing.score {
					results[res.Entry.ID] = scored{entry: res.Entry, score: score}
				}
				continue
			}
			results[res.Entry.ID] = scored{entry: res.Entry, score: score}
		}
	}

	merged := make([]*models.SearchResult, 0, len(results))
	for _, s := range results {
		merged = append(merged, &models.SearchResult{
			Entry: s.entry,
			Score: s.score,
		})
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}

	return &models.SearchResponse{
		Results:    merged,
		TotalCount: len(merged),
		QueryTime:  time.Since(start),
	}, nil
}
