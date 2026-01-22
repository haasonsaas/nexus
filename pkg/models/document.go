// Package models defines the core data types for Nexus.
package models

import (
	"time"
)

// Document represents a complete document in the RAG system.
// Documents are parsed and chunked before being stored and indexed.
type Document struct {
	// ID is the unique identifier for the document.
	ID string `json:"id"`

	// Name is the human-readable name or title of the document.
	Name string `json:"name"`

	// Source indicates where the document came from (e.g., "upload", "url", "api").
	Source string `json:"source"`

	// SourceURI is the original URI or path (e.g., file path, URL).
	SourceURI string `json:"source_uri,omitempty"`

	// ContentType is the MIME type of the original document.
	ContentType string `json:"content_type"`

	// Content is the raw text content of the document.
	Content string `json:"content"`

	// Metadata contains additional information about the document.
	Metadata DocumentMetadata `json:"metadata"`

	// ChunkCount is the number of chunks this document was split into.
	ChunkCount int `json:"chunk_count,omitempty"`

	// TotalTokens is the approximate token count (if computed).
	TotalTokens int `json:"total_tokens,omitempty"`

	// CreatedAt is when the document was added.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the document was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// DocumentMetadata contains additional information about a document.
type DocumentMetadata struct {
	// Title is the document title (from frontmatter or first heading).
	Title string `json:"title,omitempty"`

	// Author is the document author if available.
	Author string `json:"author,omitempty"`

	// Description is a brief description or summary.
	Description string `json:"description,omitempty"`

	// Tags are labels for categorization.
	Tags []string `json:"tags,omitempty"`

	// Language is the document language (ISO 639-1 code).
	Language string `json:"language,omitempty"`

	// AgentID limits this document to a specific agent.
	AgentID string `json:"agent_id,omitempty"`

	// SessionID limits this document to a specific session.
	SessionID string `json:"session_id,omitempty"`

	// ChannelID limits this document to a specific channel.
	ChannelID string `json:"channel_id,omitempty"`

	// Custom contains user-defined metadata fields.
	Custom map[string]any `json:"custom,omitempty"`
}

// DocumentChunk represents a portion of a document for vector indexing.
// Chunks are the unit of retrieval in the RAG system.
type DocumentChunk struct {
	// ID is the unique identifier for this chunk.
	ID string `json:"id"`

	// DocumentID links this chunk to its parent document.
	DocumentID string `json:"document_id"`

	// Index is the position of this chunk within the document (0-based).
	Index int `json:"index"`

	// Content is the text content of this chunk.
	Content string `json:"content"`

	// Embedding is the vector embedding for semantic search.
	Embedding []float32 `json:"-"`

	// StartOffset is the character offset in the original document.
	StartOffset int `json:"start_offset"`

	// EndOffset is the ending character offset.
	EndOffset int `json:"end_offset"`

	// Metadata contains chunk-specific metadata inherited from document.
	Metadata ChunkMetadata `json:"metadata"`

	// TokenCount is the approximate token count for this chunk.
	TokenCount int `json:"token_count,omitempty"`

	// CreatedAt is when the chunk was created.
	CreatedAt time.Time `json:"created_at"`
}

// ChunkMetadata contains information about a document chunk.
type ChunkMetadata struct {
	// DocumentName is the parent document's name.
	DocumentName string `json:"document_name,omitempty"`

	// DocumentSource is the parent document's source.
	DocumentSource string `json:"document_source,omitempty"`

	// Section is the section or heading this chunk belongs to.
	Section string `json:"section,omitempty"`

	// AgentID limits this chunk to a specific agent.
	AgentID string `json:"agent_id,omitempty"`

	// SessionID limits this chunk to a specific session.
	SessionID string `json:"session_id,omitempty"`

	// ChannelID limits this chunk to a specific channel.
	ChannelID string `json:"channel_id,omitempty"`

	// Tags are inherited from the document.
	Tags []string `json:"tags,omitempty"`

	// Extra contains additional metadata.
	Extra map[string]any `json:"extra,omitempty"`
}

// DocumentScope defines the scope for document retrieval.
type DocumentScope string

const (
	// DocumentScopeGlobal searches all documents.
	DocumentScopeGlobal DocumentScope = "global"
	// DocumentScopeAgent limits search to documents for a specific agent.
	DocumentScopeAgent DocumentScope = "agent"
	// DocumentScopeSession limits search to documents for a specific session.
	DocumentScopeSession DocumentScope = "session"
	// DocumentScopeChannel limits search to documents for a specific channel.
	DocumentScopeChannel DocumentScope = "channel"
)

// DocumentSearchRequest defines parameters for document search.
type DocumentSearchRequest struct {
	// Query is the search query text.
	Query string `json:"query"`

	// Scope limits the search to a specific scope.
	Scope DocumentScope `json:"scope,omitempty"`

	// ScopeID is the ID for the scope (agent_id, session_id, etc.).
	ScopeID string `json:"scope_id,omitempty"`

	// Limit is the maximum number of results to return.
	Limit int `json:"limit,omitempty"`

	// Threshold is the minimum similarity score (0-1).
	Threshold float32 `json:"threshold,omitempty"`

	// Tags filters results to chunks with these tags.
	Tags []string `json:"tags,omitempty"`

	// DocumentIDs limits search to specific documents.
	DocumentIDs []string `json:"document_ids,omitempty"`

	// IncludeMetadata includes full metadata in results.
	IncludeMetadata bool `json:"include_metadata,omitempty"`
}

// DocumentSearchResult represents a single search result.
type DocumentSearchResult struct {
	// Chunk is the matching chunk.
	Chunk *DocumentChunk `json:"chunk"`

	// Score is the similarity score (0-1).
	Score float32 `json:"score"`

	// Highlights are matched snippets for display.
	Highlights []string `json:"highlights,omitempty"`
}

// DocumentSearchResponse contains the results of a document search.
type DocumentSearchResponse struct {
	// Results are the matching chunks ordered by relevance.
	Results []*DocumentSearchResult `json:"results"`

	// TotalCount is the total number of matching results.
	TotalCount int `json:"total_count"`

	// QueryTime is how long the search took.
	QueryTime time.Duration `json:"query_time"`
}
