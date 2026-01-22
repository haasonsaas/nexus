package artifacts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/observability"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// MemoryRepository is an in-memory implementation for testing and simple deployments.
type MemoryRepository struct {
	mu         sync.RWMutex
	store      Store
	metadata   map[string]*Metadata
	inlineData map[string][]byte
	logger     *slog.Logger
}

// NewMemoryRepository creates a repository backed by the given store.
func NewMemoryRepository(store Store, logger *slog.Logger) *MemoryRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryRepository{
		store:      store,
		metadata:   make(map[string]*Metadata),
		inlineData: make(map[string][]byte),
		logger:     logger,
	}
}

// StoreArtifact persists an artifact from tool execution.
func (r *MemoryRepository) StoreArtifact(ctx context.Context, artifact *pb.Artifact, data io.Reader) error {
	// Generate ID if not provided
	if artifact.Id == "" {
		artifact.Id = uuid.NewString()
	}

	now := time.Now()
	meta := &Metadata{
		ID:         artifact.Id,
		Type:       artifact.Type,
		MimeType:   artifact.MimeType,
		Filename:   artifact.Filename,
		Size:       artifact.Size,
		TTLSeconds: artifact.TtlSeconds,
		CreatedAt:  now,
	}
	if sessionID := observability.GetSessionID(ctx); sessionID != "" {
		meta.SessionID = sessionID
	}
	if edgeID := observability.GetEdgeID(ctx); edgeID != "" {
		meta.EdgeID = edgeID
	}

	// Calculate expiration
	ttl := time.Duration(artifact.TtlSeconds) * time.Second
	if ttl == 0 {
		ttl = GetDefaultTTL(artifact.Type)
	}
	meta.ExpiresAt = now.Add(ttl)

	// For small artifacts (<1MB), store inline
	const maxInlineSize = 1024 * 1024
	if artifact.Size < maxInlineSize && artifact.Size > 0 {
		buf := make([]byte, artifact.Size)
		n, err := io.ReadFull(data, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("read artifact data: %w", err)
		}
		artifact.Data = buf[:n]
		artifact.Reference = fmt.Sprintf("inline://%s", artifact.Id)
		meta.Reference = artifact.Reference

		r.mu.Lock()
		r.inlineData[artifact.Id] = buf[:n]
		r.metadata[artifact.Id] = meta
		r.mu.Unlock()
	} else {
		// Store in backend
		opts := PutOptions{
			MimeType: artifact.MimeType,
			TTL:      ttl,
			Metadata: map[string]string{
				"type": artifact.Type,
			},
		}
		ref, err := r.store.Put(ctx, artifact.Id, data, opts)
		if err != nil {
			return fmt.Errorf("store artifact: %w", err)
		}
		artifact.Reference = ref
		meta.Reference = ref

		r.mu.Lock()
		r.metadata[artifact.Id] = meta
		r.mu.Unlock()
	}

	r.logger.Info("artifact stored",
		"id", artifact.Id,
		"type", artifact.Type,
		"size", artifact.Size,
		"reference", artifact.Reference)

	return nil
}

// GetArtifact retrieves artifact metadata and data.
func (r *MemoryRepository) GetArtifact(ctx context.Context, artifactID string) (*pb.Artifact, io.ReadCloser, error) {
	r.mu.RLock()
	meta, ok := r.metadata[artifactID]
	inlineData := r.inlineData[artifactID]
	r.mu.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("artifact not found: %s", artifactID)
	}

	// Check expiration
	if !meta.ExpiresAt.IsZero() && time.Now().After(meta.ExpiresAt) {
		r.DeleteArtifact(ctx, artifactID) //nolint:errcheck
		return nil, nil, fmt.Errorf("artifact expired: %s", artifactID)
	}

	artifact := &pb.Artifact{
		Id:         meta.ID,
		Type:       meta.Type,
		MimeType:   meta.MimeType,
		Filename:   meta.Filename,
		Size:       meta.Size,
		Reference:  meta.Reference,
		TtlSeconds: meta.TTLSeconds,
	}

	// Return inline data or fetch from store
	if len(inlineData) > 0 {
		artifact.Data = inlineData
		return artifact, io.NopCloser(bytes.NewReader(inlineData)), nil
	}

	data, err := r.store.Get(ctx, artifactID)
	if err != nil {
		return nil, nil, fmt.Errorf("get artifact data: %w", err)
	}

	return artifact, data, nil
}

// ListArtifacts finds artifacts matching criteria.
func (r *MemoryRepository) ListArtifacts(ctx context.Context, filter Filter) ([]*pb.Artifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*pb.Artifact
	now := time.Now()

	for _, meta := range r.metadata {
		// Check expiration
		if !meta.ExpiresAt.IsZero() && now.After(meta.ExpiresAt) {
			continue
		}

		// Apply filters
		if filter.SessionID != "" && meta.SessionID != filter.SessionID {
			continue
		}
		if filter.EdgeID != "" && meta.EdgeID != filter.EdgeID {
			continue
		}
		if filter.Type != "" && meta.Type != filter.Type {
			continue
		}
		if !filter.CreatedAfter.IsZero() && meta.CreatedAt.Before(filter.CreatedAfter) {
			continue
		}
		if !filter.CreatedBefore.IsZero() && meta.CreatedAt.After(filter.CreatedBefore) {
			continue
		}

		results = append(results, &pb.Artifact{
			Id:         meta.ID,
			Type:       meta.Type,
			MimeType:   meta.MimeType,
			Filename:   meta.Filename,
			Size:       meta.Size,
			Reference:  meta.Reference,
			TtlSeconds: meta.TTLSeconds,
		})

		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}

	return results, nil
}

// DeleteArtifact removes an artifact and its data.
func (r *MemoryRepository) DeleteArtifact(ctx context.Context, artifactID string) error {
	r.mu.Lock()
	meta, ok := r.metadata[artifactID]
	if ok {
		delete(r.metadata, artifactID)
		delete(r.inlineData, artifactID)
	}
	r.mu.Unlock()

	if !ok {
		return nil
	}

	// Delete from store if not inline
	if meta.Reference != fmt.Sprintf("inline://%s", artifactID) {
		if err := r.store.Delete(ctx, artifactID); err != nil {
			r.logger.Warn("failed to delete artifact from store",
				"id", artifactID,
				"error", err)
		}
	}

	r.logger.Info("artifact deleted", "id", artifactID)
	return nil
}

// PruneExpired removes expired artifacts.
func (r *MemoryRepository) PruneExpired(ctx context.Context) (int, error) {
	r.mu.Lock()
	var expired []string
	now := time.Now()
	for id, meta := range r.metadata {
		if !meta.ExpiresAt.IsZero() && now.After(meta.ExpiresAt) {
			expired = append(expired, id)
		}
	}
	r.mu.Unlock()

	count := 0
	for _, id := range expired {
		if err := r.DeleteArtifact(ctx, id); err == nil {
			count++
		}
	}

	r.logger.Info("pruned expired artifacts", "count", count)
	return count, nil
}
