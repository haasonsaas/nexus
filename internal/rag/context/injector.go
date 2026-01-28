// Package context provides utilities for injecting RAG-retrieved documents
// into agent conversation context.
package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Searcher defines the search capability needed by the Injector.
// This interface is implemented by index.Manager and enables testing.
type Searcher interface {
	Search(ctx context.Context, req *models.DocumentSearchRequest) (*models.DocumentSearchResponse, error)
}

// Injector injects retrieved document chunks into agent context.
type Injector struct {
	manager  *index.Manager
	searcher Searcher
	config   *InjectorConfig
}

// InjectorConfig configures context injection behavior.
type InjectorConfig struct {
	// Enabled controls whether RAG injection is active.
	Enabled bool `yaml:"enabled"`

	// MaxChunks is the maximum number of chunks to inject.
	// Default: 5
	MaxChunks int `yaml:"max_chunks"`

	// MaxTokens is the maximum total tokens to inject.
	// Default: 2000
	MaxTokens int `yaml:"max_tokens"`

	// MinScore is the minimum similarity score for inclusion.
	// Default: 0.7
	MinScore float32 `yaml:"min_score"`

	// AutoQuery automatically queries RAG based on user messages.
	// Default: true
	AutoQuery bool `yaml:"auto_query"`

	// Scope limits retrieval to a specific scope.
	// Options: "global", "agent", "session", "channel"
	// Default: "global"
	Scope string `yaml:"scope"`

	// HeaderTemplate is the template for the context header.
	// Default: "## Relevant Context\n\nThe following information may be relevant:\n\n"
	HeaderTemplate string `yaml:"header_template"`

	// ChunkTemplate is the template for each chunk.
	// Available variables: {{.Content}}, {{.Source}}, {{.Score}}
	// Default: "### {{.Source}}\n{{.Content}}\n\n"
	ChunkTemplate string `yaml:"chunk_template"`

	// FooterTemplate is the template for the context footer.
	// Default: "---\n\n"
	FooterTemplate string `yaml:"footer_template"`
}

// DefaultInjectorConfig returns the default injector configuration.
func DefaultInjectorConfig() *InjectorConfig {
	return &InjectorConfig{
		Enabled:        true,
		MaxChunks:      5,
		MaxTokens:      2000,
		MinScore:       0.7,
		AutoQuery:      true,
		Scope:          "global",
		HeaderTemplate: "## Relevant Context\n\nThe following information may be relevant:\n\n",
		ChunkTemplate:  "### {{.Source}}\n{{.Content}}\n\n",
		FooterTemplate: "---\n\n",
	}
}

// NewInjector creates a new context injector.
func NewInjector(manager *index.Manager, cfg *InjectorConfig) *Injector {
	if cfg == nil {
		cfg = DefaultInjectorConfig()
	}
	i := &Injector{
		manager: manager,
		config:  cfg,
	}
	if manager != nil {
		i.searcher = manager
	}
	return i
}

// NewInjectorWithSearcher creates a new context injector with a custom searcher.
// This is primarily used for testing.
func NewInjectorWithSearcher(searcher Searcher, cfg *InjectorConfig) *Injector {
	if cfg == nil {
		cfg = DefaultInjectorConfig()
	}
	return &Injector{
		searcher: searcher,
		config:   cfg,
	}
}

// InjectionResult contains the result of context injection.
type InjectionResult struct {
	// Context is the formatted context string to inject.
	Context string

	// ChunksUsed is the number of chunks included.
	ChunksUsed int

	// TokensUsed is the approximate token count of injected context.
	TokensUsed int

	// Chunks are the source chunks used.
	Chunks []*models.DocumentChunk
}

// Inject retrieves relevant documents and formats them for context injection.
func (i *Injector) Inject(ctx context.Context, query string, scopeID string) (*InjectionResult, error) {
	if !i.config.Enabled || i.searcher == nil {
		return &InjectionResult{}, nil
	}

	// Build search request
	req := &models.DocumentSearchRequest{
		Query:     query,
		Scope:     parseScope(i.config.Scope),
		ScopeID:   scopeID,
		Limit:     i.config.MaxChunks * 2, // Get extra in case we hit token limit
		Threshold: i.config.MinScore,
	}

	// Search for relevant chunks
	resp, err := i.searcher.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(resp.Results) == 0 {
		return &InjectionResult{}, nil
	}

	// Select chunks within limits
	var selectedChunks []*models.DocumentChunk
	totalTokens := 0

	for _, result := range resp.Results {
		if len(selectedChunks) >= i.config.MaxChunks {
			break
		}

		chunkTokens := result.Chunk.TokenCount
		if chunkTokens == 0 {
			// Estimate if not available
			chunkTokens = len(result.Chunk.Content) / 4
		}

		if i.config.MaxTokens > 0 && totalTokens+chunkTokens > i.config.MaxTokens {
			continue // Skip if would exceed token limit
		}

		selectedChunks = append(selectedChunks, result.Chunk)
		totalTokens += chunkTokens
	}

	if len(selectedChunks) == 0 {
		return &InjectionResult{}, nil
	}

	// Format context
	contextStr := i.formatContext(selectedChunks)

	return &InjectionResult{
		Context:    contextStr,
		ChunksUsed: len(selectedChunks),
		TokensUsed: totalTokens,
		Chunks:     selectedChunks,
	}, nil
}

// InjectForMessage injects context for a user message.
// This is a convenience method that extracts the query from the message.
func (i *Injector) InjectForMessage(ctx context.Context, msg *models.Message, session *models.Session) (*InjectionResult, error) {
	if msg == nil || msg.Content == "" {
		return &InjectionResult{}, nil
	}

	// Use the message content as the query
	query := msg.Content

	// Determine scope ID based on configuration
	scopeID := ""
	switch i.config.Scope {
	case "session":
		if session != nil {
			scopeID = session.ID
		}
	case "channel":
		if session != nil && session.ChannelID != "" {
			scopeID = session.ChannelID
		} else {
			scopeID = msg.ChannelID
		}
	case "agent":
		if session != nil {
			scopeID = session.AgentID
		}
	}

	return i.Inject(ctx, query, scopeID)
}

// formatContext formats selected chunks into a context string.
func (i *Injector) formatContext(chunks []*models.DocumentChunk) string {
	if len(chunks) == 0 {
		return ""
	}

	var sb strings.Builder

	// Header
	sb.WriteString(i.config.HeaderTemplate)

	// Chunks
	for _, chunk := range chunks {
		source := chunk.Metadata.DocumentName
		if source == "" {
			source = "Document"
		}

		formatted := i.config.ChunkTemplate
		formatted = strings.ReplaceAll(formatted, "{{.Content}}", chunk.Content)
		formatted = strings.ReplaceAll(formatted, "{{.Source}}", source)
		if chunk.Metadata.Section != "" {
			formatted = strings.ReplaceAll(formatted, "{{.Section}}", chunk.Metadata.Section)
		}

		sb.WriteString(formatted)
	}

	// Footer
	sb.WriteString(i.config.FooterTemplate)

	return sb.String()
}

// FormatContextBlock creates a formatted context block from search results.
// This is useful for manual context injection in tools.
func FormatContextBlock(results []*models.DocumentSearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Retrieved Context\n\n")

	for _, result := range results {
		source := result.Chunk.Metadata.DocumentName
		if source == "" {
			source = "Document"
		}

		sb.WriteString(fmt.Sprintf("### %s (score: %.2f)\n", source, result.Score))
		sb.WriteString(result.Chunk.Content)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// parseScope converts a string scope to DocumentScope.
func parseScope(scope string) models.DocumentScope {
	switch strings.ToLower(scope) {
	case "agent":
		return models.DocumentScopeAgent
	case "session":
		return models.DocumentScopeSession
	case "channel":
		return models.DocumentScopeChannel
	default:
		return models.DocumentScopeGlobal
	}
}

// ContextKey is the context key for injected RAG context.
type contextKey struct{}

// WithRAGContext adds RAG context to a Go context.
func WithRAGContext(ctx context.Context, ragContext string) context.Context {
	return context.WithValue(ctx, contextKey{}, ragContext)
}

// RAGContextFromContext retrieves RAG context from a Go context.
func RAGContextFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(contextKey{}).(string)
	return v, ok && v != ""
}
