// Package chunker provides text chunking implementations.
package chunker

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/pkg/models"
)

// RecursiveCharacterTextSplitter implements a recursive chunking strategy.
// It tries to split on larger separators first, then falls back to smaller ones.
// This is similar to LangChain's RecursiveCharacterTextSplitter.
type RecursiveCharacterTextSplitter struct {
	config       Config
	separators   []string
	tokenCounter TokenCounter
}

// DefaultSeparators returns the default separator hierarchy.
// Splits are attempted in order, from largest semantic units to smallest.
var DefaultSeparators = []string{
	"\n\n",  // Paragraph break
	"\n",    // Line break
	". ",    // Sentence end
	"? ",    // Question end
	"! ",    // Exclamation end
	"; ",    // Semicolon
	": ",    // Colon
	", ",    // Comma
	" ",     // Space
	"",      // Character (last resort)
}

// MarkdownSeparators are separators optimized for Markdown documents.
var MarkdownSeparators = []string{
	"\n## ",  // H2 heading
	"\n### ", // H3 heading
	"\n#### ",// H4 heading
	"\n\n",   // Paragraph break
	"\n",     // Line break
	". ",     // Sentence end
	" ",      // Space
	"",       // Character
}

// NewRecursiveCharacterTextSplitter creates a new recursive text splitter.
func NewRecursiveCharacterTextSplitter(cfg Config) *RecursiveCharacterTextSplitter {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = DefaultConfig().ChunkSize
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = DefaultConfig().ChunkOverlap
	}
	if cfg.ChunkOverlap >= cfg.ChunkSize {
		cfg.ChunkOverlap = cfg.ChunkSize / 5
	}
	if cfg.MinChunkSize <= 0 {
		cfg.MinChunkSize = DefaultConfig().MinChunkSize
	}

	return &RecursiveCharacterTextSplitter{
		config:       cfg,
		separators:   DefaultSeparators,
		tokenCounter: &SimpleTokenCounter{CharsPerToken: 4},
	}
}

// WithSeparators sets custom separators.
func (s *RecursiveCharacterTextSplitter) WithSeparators(seps []string) *RecursiveCharacterTextSplitter {
	s.separators = seps
	return s
}

// WithTokenCounter sets a custom token counter.
func (s *RecursiveCharacterTextSplitter) WithTokenCounter(tc TokenCounter) *RecursiveCharacterTextSplitter {
	s.tokenCounter = tc
	return s
}

// Name returns the chunker name.
func (s *RecursiveCharacterTextSplitter) Name() string {
	return "recursive_character"
}

// Chunk splits a document into chunks using recursive character splitting.
func (s *RecursiveCharacterTextSplitter) Chunk(doc *models.Document, parseResult *parser.ParseResult) ([]*models.DocumentChunk, error) {
	content := parseResult.Content
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	// Split the text
	rawChunks := s.splitText(content, s.separators)

	// Merge chunks with overlap
	mergedChunks := s.mergeChunksWithOverlap(rawChunks, content)

	// Convert to DocumentChunk models
	chunks := make([]*models.DocumentChunk, 0, len(mergedChunks))
	now := time.Now()

	for i, chunk := range mergedChunks {
		// Find the section this chunk belongs to
		section := findSection(parseResult.Sections, chunk.StartOffset)

		docChunk := &models.DocumentChunk{
			ID:          uuid.New().String(),
			DocumentID:  doc.ID,
			Index:       i,
			Content:     chunk.Content,
			StartOffset: chunk.StartOffset,
			EndOffset:   chunk.EndOffset,
			Metadata:    BuildChunkMetadata(doc, section),
			TokenCount:  s.tokenCounter.Count(chunk.Content),
			CreatedAt:   now,
		}

		chunks = append(chunks, docChunk)
	}

	return chunks, nil
}

// splitText recursively splits text using the separator hierarchy.
func (s *RecursiveCharacterTextSplitter) splitText(text string, separators []string) []Chunk {
	if len(text) == 0 {
		return nil
	}

	// Find the first separator that exists in the text
	separator := ""
	for _, sep := range separators {
		if sep == "" || strings.Contains(text, sep) {
			separator = sep
			break
		}
	}

	// Split by the separator
	var splits []string
	if separator == "" {
		// Split into individual characters as last resort
		splits = make([]string, 0, len(text))
		for _, r := range text {
			splits = append(splits, string(r))
		}
	} else {
		splits = strings.Split(text, separator)
	}

	// Recursively process splits and merge small pieces
	var result []Chunk
	var currentChunk strings.Builder
	startOffset := 0

	for i, split := range splits {
		// Add separator back if configured and not the last split
		piece := split
		if s.config.KeepSeparators && separator != "" && i < len(splits)-1 {
			piece = split + separator
		}

		// If adding this piece would exceed chunk size
		if currentChunk.Len() > 0 && currentChunk.Len()+len(piece) > s.config.ChunkSize {
			// Save current chunk
			chunkContent := currentChunk.String()
			if !s.config.PreserveWhitespace {
				chunkContent = strings.TrimSpace(chunkContent)
			}
			if len(chunkContent) >= s.config.MinChunkSize {
				result = append(result, Chunk{
					Content:     chunkContent,
					StartOffset: startOffset,
					EndOffset:   startOffset + len(chunkContent),
				})
			}

			// Reset for next chunk
			currentChunk.Reset()
			startOffset += len(chunkContent)
		}

		// If single piece is too large, recursively split it
		if len(piece) > s.config.ChunkSize && len(separators) > 1 {
			// First save any accumulated content
			if currentChunk.Len() > 0 {
				chunkContent := currentChunk.String()
				if !s.config.PreserveWhitespace {
					chunkContent = strings.TrimSpace(chunkContent)
				}
				if len(chunkContent) >= s.config.MinChunkSize {
					result = append(result, Chunk{
						Content:     chunkContent,
						StartOffset: startOffset,
						EndOffset:   startOffset + len(chunkContent),
					})
				}
				startOffset += len(chunkContent)
				currentChunk.Reset()
			}

			// Recursively split the large piece
			subChunks := s.splitText(piece, separators[1:])
			for _, sub := range subChunks {
				sub.StartOffset += startOffset
				sub.EndOffset += startOffset
				result = append(result, sub)
			}
			startOffset += len(piece)
		} else {
			currentChunk.WriteString(piece)
		}
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunkContent := currentChunk.String()
		if !s.config.PreserveWhitespace {
			chunkContent = strings.TrimSpace(chunkContent)
		}
		if len(chunkContent) >= s.config.MinChunkSize {
			result = append(result, Chunk{
				Content:     chunkContent,
				StartOffset: startOffset,
				EndOffset:   startOffset + len(chunkContent),
			})
		}
	}

	return result
}

// mergeChunksWithOverlap adds overlap between consecutive chunks.
func (s *RecursiveCharacterTextSplitter) mergeChunksWithOverlap(chunks []Chunk, originalText string) []Chunk {
	if len(chunks) <= 1 || s.config.ChunkOverlap <= 0 {
		return chunks
	}

	result := make([]Chunk, len(chunks))

	for i, chunk := range chunks {
		if i == 0 {
			// First chunk: no prefix overlap
			result[i] = chunk
			continue
		}

		// Get overlap from previous chunk
		prevChunk := chunks[i-1]
		overlap := s.config.ChunkOverlap
		if overlap > len(prevChunk.Content) {
			overlap = len(prevChunk.Content)
		}

		// Add overlap prefix
		overlapText := prevChunk.Content[len(prevChunk.Content)-overlap:]
		newContent := overlapText + chunk.Content

		// Adjust offsets
		result[i] = Chunk{
			Content:     newContent,
			StartOffset: chunk.StartOffset - overlap,
			EndOffset:   chunk.EndOffset,
		}
	}

	return result
}

// findSection finds the section title for a given offset.
func findSection(sections []parser.Section, offset int) string {
	for i := len(sections) - 1; i >= 0; i-- {
		if offset >= sections[i].StartOffset {
			return sections[i].Title
		}
	}
	return ""
}

// NewMarkdownSplitter creates a splitter optimized for Markdown documents.
func NewMarkdownSplitter(cfg Config) *RecursiveCharacterTextSplitter {
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	splitter.separators = MarkdownSeparators
	return splitter
}
