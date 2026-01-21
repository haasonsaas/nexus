// Package embeddings provides interfaces and implementations for embedding providers.
package embeddings

import (
	"context"
)

// Provider defines the interface for embedding providers.
type Provider interface {
	// Embed generates an embedding for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts (more efficient).
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Name returns the provider name.
	Name() string

	// Dimension returns the embedding dimension.
	Dimension() int

	// MaxBatchSize returns the maximum number of texts per batch.
	MaxBatchSize() int
}

// Config contains common configuration for embedding providers.
type Config struct {
	Provider string `yaml:"provider"` // openai, gemini, ollama
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url"`
	Model    string `yaml:"model"`

	// Ollama-specific
	OllamaURL string `yaml:"ollama_url"`

	// Gemini-specific
	ProjectID string `yaml:"project_id"`
	Location  string `yaml:"location"`
}
