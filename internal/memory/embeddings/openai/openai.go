// Package openai provides an embedding provider using OpenAI's embedding models.
package openai

import (
	"context"
	"fmt"

	"github.com/haasonsaas/nexus/internal/memory/embeddings"
	"github.com/sashabaranov/go-openai"
)

// Provider implements embeddings.Provider using OpenAI.
type Provider struct {
	client *openai.Client
	model  string
}

var _ embeddings.Provider = (*Provider)(nil)

// Config contains configuration for the OpenAI provider.
type Config struct {
	APIKey  string
	BaseURL string // Optional custom base URL
	Model   string // text-embedding-3-small or text-embedding-3-large
}

// New creates a new OpenAI embedding provider.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}

	config := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}

	return &Provider{
		client: openai.NewClientWithConfig(config),
		model:  cfg.Model,
	}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "openai"
}

// Dimension returns the embedding dimension for the configured model.
func (p *Provider) Dimension() int {
	switch p.model {
	case "text-embedding-3-small":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	default:
		return 1536
	}
}

// MaxBatchSize returns the maximum number of texts per batch.
func (p *Provider) MaxBatchSize() int {
	return 2048 // OpenAI supports up to 2048 inputs per request
}

// Embed generates an embedding for a single text.
func (p *Provider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
func (p *Provider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: texts,
		Model: openai.EmbeddingModel(p.model),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embeddings: %w", err)
	}

	results := make([][]float32, len(resp.Data))
	for _, data := range resp.Data {
		results[data.Index] = data.Embedding
	}

	return results, nil
}
