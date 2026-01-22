// Package chunker provides text chunking interfaces and implementations
// for the RAG (Retrieval-Augmented Generation) system.
package chunker

import (
	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Chunker defines the interface for text chunking strategies.
// Chunkers split documents into smaller pieces suitable for embedding and retrieval.
type Chunker interface {
	// Chunk splits a document into chunks.
	// The parseResult provides content and structural information from parsing.
	Chunk(doc *models.Document, parseResult *parser.ParseResult) ([]*models.DocumentChunk, error)

	// Name returns the chunker name for logging and debugging.
	Name() string
}

// Config contains common configuration for chunkers.
type Config struct {
	// ChunkSize is the target size of each chunk in characters.
	// Default: 1000
	ChunkSize int `yaml:"chunk_size"`

	// ChunkOverlap is the number of characters to overlap between chunks.
	// Default: 200
	ChunkOverlap int `yaml:"chunk_overlap"`

	// MinChunkSize is the minimum chunk size to keep.
	// Chunks smaller than this are merged with the previous chunk.
	// Default: 100
	MinChunkSize int `yaml:"min_chunk_size"`

	// PreserveWhitespace keeps leading/trailing whitespace in chunks.
	// Default: false
	PreserveWhitespace bool `yaml:"preserve_whitespace"`

	// KeepSeparators includes separators at the end of chunks.
	// Default: true
	KeepSeparators bool `yaml:"keep_separators"`
}

// DefaultConfig returns the default chunker configuration.
func DefaultConfig() Config {
	return Config{
		ChunkSize:          1000,
		ChunkOverlap:       200,
		MinChunkSize:       100,
		PreserveWhitespace: false,
		KeepSeparators:     true,
	}
}

// Chunk represents a piece of text with position information.
type Chunk struct {
	// Content is the chunk text.
	Content string

	// StartOffset is the character offset in the original document.
	StartOffset int

	// EndOffset is the ending character offset.
	EndOffset int

	// Section is the section this chunk belongs to (if structure-aware).
	Section string
}

// TokenCounter estimates token count for text.
// Used for chunk size validation and metadata.
type TokenCounter interface {
	// Count returns the estimated token count for text.
	Count(text string) int
}

// SimpleTokenCounter estimates tokens by dividing character count by average chars per token.
type SimpleTokenCounter struct {
	// CharsPerToken is the average characters per token (default: 4).
	CharsPerToken int
}

// Count returns the estimated token count.
func (c *SimpleTokenCounter) Count(text string) int {
	cpt := c.CharsPerToken
	if cpt <= 0 {
		cpt = 4 // Default: ~4 chars per token for English
	}
	return (len(text) + cpt - 1) / cpt
}

// BuildChunkMetadata creates chunk metadata from document metadata.
func BuildChunkMetadata(doc *models.Document, section string) models.ChunkMetadata {
	meta := models.ChunkMetadata{
		DocumentName:   doc.Name,
		DocumentSource: doc.Source,
		Section:        section,
		AgentID:        doc.Metadata.AgentID,
		SessionID:      doc.Metadata.SessionID,
		ChannelID:      doc.Metadata.ChannelID,
		Tags:           doc.Metadata.Tags,
	}

	// Copy custom fields
	if doc.Metadata.Custom != nil {
		meta.Extra = make(map[string]any)
		for k, v := range doc.Metadata.Custom {
			meta.Extra[k] = v
		}
	}

	return meta
}
