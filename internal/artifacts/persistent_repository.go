package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/observability"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// PersistentRepository stores artifact metadata on disk and artifact data in a Store backend.
type PersistentRepository struct {
	mu           sync.RWMutex
	store        Store
	metadata     map[string]*Metadata
	metadataPath string
	logger       *slog.Logger
}

type persistedMetadata struct {
	Version   int                  `json:"version"`
	Artifacts map[string]*Metadata `json:"artifacts"`
}

// NewPersistentRepository creates a repository that persists metadata to disk.
func NewPersistentRepository(store Store, metadataPath string, logger *slog.Logger) (*PersistentRepository, error) {
	if store == nil {
		return nil, fmt.Errorf("artifact store is required")
	}
	if strings.TrimSpace(metadataPath) == "" {
		return nil, fmt.Errorf("metadata path is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	dir := filepath.Dir(metadataPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create metadata directory: %w", err)
	}

	repo := &PersistentRepository{
		store:        store,
		metadata:     make(map[string]*Metadata),
		metadataPath: metadataPath,
		logger:       logger,
	}
	if err := repo.loadMetadata(); err != nil {
		return nil, err
	}
	return repo, nil
}

// StoreArtifact persists an artifact from tool execution.
func (r *PersistentRepository) StoreArtifact(ctx context.Context, artifact *pb.Artifact, data io.Reader) error {
	if artifact == nil {
		return fmt.Errorf("artifact is required")
	}
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

	ttl := time.Duration(artifact.TtlSeconds) * time.Second
	if ttl == 0 {
		ttl = GetDefaultTTL(artifact.Type)
	}
	meta.ExpiresAt = now.Add(ttl)

	if strings.HasPrefix(artifact.Reference, "redacted://") {
		meta.Reference = artifact.Reference
		meta.Size = 0
		r.mu.Lock()
		r.metadata[artifact.Id] = meta
		err := r.persistLocked()
		r.mu.Unlock()
		if err != nil {
			return err
		}
		r.logger.Info("artifact redacted", "id", artifact.Id, "type", artifact.Type)
		return nil
	}

	opts := PutOptions{
		MimeType: artifact.MimeType,
		TTL:      ttl,
		Metadata: map[string]string{
			"type": artifact.Type,
		},
	}
	if meta.SessionID != "" {
		opts.Metadata["session_id"] = meta.SessionID
	}
	if meta.EdgeID != "" {
		opts.Metadata["edge_id"] = meta.EdgeID
	}

	ref, err := r.store.Put(ctx, artifact.Id, data, opts)
	if err != nil {
		return fmt.Errorf("store artifact: %w", err)
	}
	artifact.Reference = ref
	meta.Reference = ref

	r.mu.Lock()
	r.metadata[artifact.Id] = meta
	err = r.persistLocked()
	r.mu.Unlock()
	if err != nil {
		_ = r.store.Delete(ctx, artifact.Id)
		return err
	}

	r.logger.Info("artifact stored",
		"id", artifact.Id,
		"type", artifact.Type,
		"size", artifact.Size,
		"reference", artifact.Reference)

	return nil
}

// GetArtifact retrieves artifact metadata and data.
func (r *PersistentRepository) GetArtifact(ctx context.Context, artifactID string) (*pb.Artifact, io.ReadCloser, error) {
	r.mu.RLock()
	meta, ok := r.metadata[artifactID]
	r.mu.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("artifact not found: %s", artifactID)
	}

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

	if strings.HasPrefix(meta.Reference, "redacted://") {
		return artifact, io.NopCloser(bytes.NewReader(nil)), nil
	}

	data, err := r.store.Get(ctx, artifactID)
	if err != nil {
		return nil, nil, fmt.Errorf("get artifact data: %w", err)
	}

	return artifact, data, nil
}

// ListArtifacts finds artifacts matching criteria.
func (r *PersistentRepository) ListArtifacts(ctx context.Context, filter Filter) ([]*pb.Artifact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*pb.Artifact
	now := time.Now()

	for _, meta := range r.metadata {
		if !meta.ExpiresAt.IsZero() && now.After(meta.ExpiresAt) {
			continue
		}

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
func (r *PersistentRepository) DeleteArtifact(ctx context.Context, artifactID string) error {
	r.mu.Lock()
	meta, ok := r.metadata[artifactID]
	if ok {
		delete(r.metadata, artifactID)
	}
	err := r.persistLocked()
	r.mu.Unlock()
	if err != nil {
		return err
	}

	if !ok {
		return nil
	}

	if meta.Reference != "" && !strings.HasPrefix(meta.Reference, "redacted://") {
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
func (r *PersistentRepository) PruneExpired(ctx context.Context) (int, error) {
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

func (r *PersistentRepository) loadMetadata() error {
	data, err := os.ReadFile(r.metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read artifact metadata: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var stored persistedMetadata
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("parse artifact metadata: %w", err)
	}
	if stored.Artifacts != nil {
		r.metadata = stored.Artifacts
	}
	return nil
}

func (r *PersistentRepository) persistLocked() error {
	state := persistedMetadata{
		Version:   1,
		Artifacts: r.metadata,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(r.metadataPath); err == nil {
		mode = info.Mode().Perm()
	}
	tmpPath := r.metadataPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, r.metadataPath)
}
