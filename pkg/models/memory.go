// Package models defines the core data types for Nexus.
package models

import (
	"time"
)

// MemoryEntry represents a memory item stored in the vector database for semantic search.
type MemoryEntry struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`

	Content  string         `json:"content"`
	Metadata MemoryMetadata `json:"metadata"`

	Embedding []float32 `json:"-"` // Not serialized to JSON
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryMetadata contains additional information about a memory entry.
type MemoryMetadata struct {
	Source string         `json:"source"` // "message", "document", "note"
	Role   string         `json:"role"`   // "user", "assistant"
	Tags   []string       `json:"tags"`
	Extra  map[string]any `json:"extra"`
}

// MemoryScope defines the scope for memory search/indexing.
type MemoryScope string

const (
	// ScopeSession limits memory to the current session.
	ScopeSession MemoryScope = "session"
	// ScopeChannel limits memory to the current channel.
	ScopeChannel MemoryScope = "channel"
	// ScopeAgent limits memory to the current agent.
	ScopeAgent MemoryScope = "agent"
	// ScopeGlobal searches all memories.
	ScopeGlobal MemoryScope = "global"
)

// SearchRequest defines parameters for semantic memory search.
type SearchRequest struct {
	Query     string         `json:"query"`
	Scope     MemoryScope    `json:"scope"`
	ScopeID   string         `json:"scope_id"`
	Limit     int            `json:"limit"`
	Threshold float32        `json:"threshold"` // Min similarity (0-1)
	Filters   map[string]any `json:"filters"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	Entry      *MemoryEntry `json:"entry"`
	Score      float32      `json:"score"`      // Similarity score (0-1)
	Highlights []string     `json:"highlights"` // Matched snippets
}

// SearchResponse contains the results of a memory search.
type SearchResponse struct {
	Results    []*SearchResult `json:"results"`
	TotalCount int             `json:"total_count"`
	QueryTime  time.Duration   `json:"query_time"`
}
