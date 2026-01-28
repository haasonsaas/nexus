package memorysearch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type EmbeddingsConfig struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	CacheDir string
	CacheTTL time.Duration
	Timeout  time.Duration
}

func (c EmbeddingsConfig) enabled() bool {
	return strings.TrimSpace(c.Model) != "" && strings.TrimSpace(c.BaseURL) != ""
}

type embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float64, error)
}

type remoteEmbedder struct {
	cfg    EmbeddingsConfig
	client *http.Client
	cache  *embeddingCache
	url    string
}

func newRemoteEmbedder(cfg EmbeddingsConfig) (*remoteEmbedder, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("embeddings model is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("embeddings base_url is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	cacheDir := expandPath(cfg.CacheDir)
	cache := newEmbeddingCache(cacheDir, cfg.CacheTTL)
	return &remoteEmbedder{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		cache:  cache,
		url:    resolveEmbeddingsURL(cfg.BaseURL),
	}, nil
}

func (e *remoteEmbedder) Embed(ctx context.Context, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	results := make([][]float64, len(inputs))
	missingInputs := make([]string, 0, len(inputs))
	missingIndexes := make([]int, 0, len(inputs))

	for i, input := range inputs {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			continue
		}
		key := cacheKey(e.cfg.Model, trimmed)
		if e.cache != nil {
			if cached, ok := e.cache.Get(key); ok {
				results[i] = cached
				continue
			}
		}
		missingInputs = append(missingInputs, trimmed)
		missingIndexes = append(missingIndexes, i)
	}

	if len(missingInputs) > 0 {
		vectors, err := e.embedRemote(ctx, missingInputs)
		if err != nil {
			return nil, err
		}
		if len(vectors) != len(missingInputs) {
			return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(vectors), len(missingInputs))
		}
		for i, idx := range missingIndexes {
			vec := vectors[i]
			results[idx] = vec
			if e.cache != nil {
				if err := e.cache.Set(cacheKey(e.cfg.Model, missingInputs[i]), vec); err != nil {
					// Best-effort cache population.
					_ = err
				}
			}
		}
	}

	for i := range results {
		if results[i] == nil {
			results[i] = []float64{}
		}
	}

	return results, nil
}

func (e *remoteEmbedder) embedRemote(ctx context.Context, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	provider := strings.TrimSpace(e.cfg.Provider)
	if provider == "" {
		provider = "unknown"
	}
	endpoint := strings.TrimSpace(e.url)

	payload := struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{
		Model: e.cfg.Model,
		Input: inputs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embeddings request build failed (provider=%s url=%s): %w", provider, endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := strings.TrimSpace(e.cfg.APIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request failed (provider=%s url=%s): %w", provider, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
		if readErr != nil {
			return nil, fmt.Errorf("embeddings request failed (provider=%s url=%s) with status %d and unreadable body: %w", provider, endpoint, resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("embeddings request failed (provider=%s url=%s): %s", provider, endpoint, strings.TrimSpace(string(data)))
	}

	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embeddings response decode failed (provider=%s url=%s): %w", provider, endpoint, err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("embeddings response missing data (provider=%s url=%s)", provider, endpoint)
	}

	vectors := make([][]float64, len(inputs))
	for i, entry := range parsed.Data {
		idx := entry.Index
		if idx < 0 || idx >= len(inputs) {
			idx = i
		}
		vectors[idx] = entry.Embedding
	}
	for i := range vectors {
		if vectors[i] == nil {
			vectors[i] = []float64{}
		}
	}
	return vectors, nil
}

type embeddingCache struct {
	dir string
	ttl time.Duration
}

func newEmbeddingCache(dir string, ttl time.Duration) *embeddingCache {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if ttl < 0 {
		ttl = 0
	}
	return &embeddingCache{dir: dir, ttl: ttl}
}

func (c *embeddingCache) Get(key string) ([]float64, bool) {
	if c == nil {
		return nil, false
	}
	path := filepath.Join(c.dir, key+".json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if c.ttl > 0 && time.Since(info.ModTime()) > c.ttl {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}
	if len(entry.Embedding) == 0 {
		return nil, false
	}
	return entry.Embedding, true
}

func (c *embeddingCache) Set(key string, embedding []float64) error {
	if c == nil {
		return nil
	}
	if len(embedding) == 0 {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(c.dir, key+".json")
	payload, err := json.Marshal(struct {
		Embedding []float64 `json:"embedding"`
	}{Embedding: embedding})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cacheKey(model string, text string) string {
	payload := strings.TrimSpace(model) + "\n" + strings.TrimSpace(text)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}

func resolveEmbeddingsURL(base string) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimRight(trimmed, "/")
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, "/embeddings") {
		return trimmed
	}
	if strings.HasSuffix(lower, "/v1") {
		return trimmed + "/embeddings"
	}
	return trimmed + "/v1/embeddings"
}

func expandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
