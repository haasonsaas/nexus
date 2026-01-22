// Package ollama provides an embedding provider using Ollama's local models.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/haasonsaas/nexus/internal/memory/embeddings"
)

// Provider implements embeddings.Provider using Ollama.
type Provider struct {
	baseURL string
	model   string
	client  *http.Client
}

var _ embeddings.Provider = (*Provider)(nil)

// Config contains configuration for the Ollama provider.
type Config struct {
	BaseURL string // Default: http://localhost:11434
	Model   string // nomic-embed-text, mxbai-embed-large
}

// New creates a new Ollama embedding provider.
func New(cfg Config) (*Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "nomic-embed-text"
	}

	return &Provider{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "ollama"
}

// Dimension returns the embedding dimension for the configured model.
func (p *Provider) Dimension() int {
	switch p.model {
	case "nomic-embed-text":
		return 768
	case "mxbai-embed-large":
		return 1024
	case "all-minilm":
		return 384
	default:
		return 768
	}
}

// MaxBatchSize returns the maximum number of texts per batch.
// Ollama processes one at a time via API, though batch requests are supported.
func (p *Provider) MaxBatchSize() int {
	return 100 // Process in reasonable batches
}

// Embed generates an embedding for a single text.
func (p *Provider) Embed(ctx context.Context, text string) ([]float32, error) {
	req := embeddingRequest{
		Model:  p.model,
		Prompt: text,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("ollama returned status %d and failed to read body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Embedding, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (p *Provider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	for i, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		embeddings[i] = embedding
	}

	return embeddings, nil
}

type embeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}
