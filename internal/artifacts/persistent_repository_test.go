package artifacts

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

func TestPersistentRepository_PersistsMetadata(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	payload := []byte("artifact-data")
	artifact := &pb.Artifact{
		Id:       "artifact-1",
		Type:     "screenshot",
		MimeType: "text/plain",
		Filename: "note.txt",
		Size:     int64(len(payload)),
	}
	if err := repo.StoreArtifact(context.Background(), artifact, bytes.NewReader(payload)); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	storeReloaded, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore (reload): %v", err)
	}
	repoReloaded, err := NewPersistentRepository(storeReloaded, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository (reload): %v", err)
	}

	got, reader, err := repoReloaded.GetArtifact(context.Background(), artifact.Id)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("artifact payload mismatch: got %q want %q", string(data), string(payload))
	}
	if got.Reference == "" {
		t.Fatal("expected reference to be set")
	}
}

func TestNewPersistentRepository_ValidationErrors(t *testing.T) {
	t.Run("nil store", func(t *testing.T) {
		_, err := NewPersistentRepository(nil, "/tmp/meta.json", nil)
		if err == nil {
			t.Error("expected error for nil store")
		}
	})

	t.Run("empty metadata path", func(t *testing.T) {
		dir := t.TempDir()
		store, _ := NewLocalStore(dir)
		_, err := NewPersistentRepository(store, "", nil)
		if err == nil {
			t.Error("expected error for empty metadata path")
		}
	})

	t.Run("whitespace metadata path", func(t *testing.T) {
		dir := t.TempDir()
		store, _ := NewLocalStore(dir)
		_, err := NewPersistentRepository(store, "   ", nil)
		if err == nil {
			t.Error("expected error for whitespace metadata path")
		}
	})
}

func TestPersistentRepository_Close(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	err = repo.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestPersistentRepository_ListArtifacts(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	// Store some artifacts
	for i := 0; i < 3; i++ {
		artifact := &pb.Artifact{
			Type:     "screenshot",
			MimeType: "image/png",
		}
		if err := repo.StoreArtifact(context.Background(), artifact, bytes.NewReader([]byte("data"))); err != nil {
			t.Fatalf("StoreArtifact: %v", err)
		}
	}

	t.Run("list all", func(t *testing.T) {
		list, err := repo.ListArtifacts(context.Background(), Filter{})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 3 {
			t.Errorf("expected 3 artifacts, got %d", len(list))
		}
	})

	t.Run("list with type filter", func(t *testing.T) {
		list, err := repo.ListArtifacts(context.Background(), Filter{Type: "screenshot"})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 3 {
			t.Errorf("expected 3 artifacts, got %d", len(list))
		}

		list, err = repo.ListArtifacts(context.Background(), Filter{Type: "pdf"})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 artifacts, got %d", len(list))
		}
	})

	t.Run("list with limit", func(t *testing.T) {
		list, err := repo.ListArtifacts(context.Background(), Filter{Limit: 2})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 artifacts (limited), got %d", len(list))
		}
	})
}

func TestPersistentRepository_DeleteArtifact(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	// Store artifact
	artifact := &pb.Artifact{
		Id:       "delete-me",
		Type:     "screenshot",
		MimeType: "image/png",
	}
	if err := repo.StoreArtifact(context.Background(), artifact, bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	// Verify exists
	_, _, err = repo.GetArtifact(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("GetArtifact before delete: %v", err)
	}

	// Delete
	err = repo.DeleteArtifact(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	// Verify deleted
	_, _, err = repo.GetArtifact(context.Background(), "delete-me")
	if err == nil {
		t.Error("expected error after delete")
	}

	// Delete non-existent should not error
	err = repo.DeleteArtifact(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("DeleteArtifact nonexistent should not error: %v", err)
	}
}

func TestPersistentRepository_PruneExpired(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	// Store artifact with very short TTL
	artifact := &pb.Artifact{
		Id:         "expiring",
		Type:       "screenshot",
		MimeType:   "image/png",
		TtlSeconds: 1, // 1 second TTL
	}
	if err := repo.StoreArtifact(context.Background(), artifact, bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	// Wait for expiration
	time.Sleep(2 * time.Second)

	// Prune
	count, err := repo.PruneExpired(context.Background())
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pruned, got %d", count)
	}

	// Verify deleted
	_, _, err = repo.GetArtifact(context.Background(), "expiring")
	if err == nil {
		t.Error("expected error after prune")
	}
}

func TestPersistentRepository_StoreRedacted(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	// Store redacted artifact
	artifact := &pb.Artifact{
		Id:        "redacted-artifact",
		Type:      "screenshot",
		Reference: "redacted://screenshot-1234",
	}
	if err := repo.StoreArtifact(context.Background(), artifact, nil); err != nil {
		t.Fatalf("StoreArtifact redacted: %v", err)
	}

	// Get redacted artifact
	got, reader, err := repo.GetArtifact(context.Background(), "redacted-artifact")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	defer reader.Close()

	if got.Reference != "redacted://screenshot-1234" {
		t.Errorf("reference = %q, want %q", got.Reference, "redacted://screenshot-1234")
	}

	// Read should return empty data
	data, _ := io.ReadAll(reader)
	if len(data) != 0 {
		t.Errorf("expected empty data for redacted artifact, got %d bytes", len(data))
	}
}

func TestPersistentRepository_GetArtifactNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	_, _, err = repo.GetArtifact(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent artifact")
	}
}

func TestPersistentRepository_StoreNilArtifact(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	err = repo.StoreArtifact(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for nil artifact")
	}
}

func TestPersistentRepository_ListWithFilters(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	metaPath := filepath.Join(dir, "metadata.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := NewPersistentRepository(store, metaPath, logger)
	if err != nil {
		t.Fatalf("NewPersistentRepository: %v", err)
	}

	// Store some artifacts
	artifact := &pb.Artifact{
		Id:       "test-artifact",
		Type:     "screenshot",
		MimeType: "image/png",
	}
	if err := repo.StoreArtifact(context.Background(), artifact, bytes.NewReader([]byte("data"))); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	t.Run("filter by session ID", func(t *testing.T) {
		list, err := repo.ListArtifacts(context.Background(), Filter{SessionID: "nonexistent-session"})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 artifacts for wrong session, got %d", len(list))
		}
	})

	t.Run("filter by edge ID", func(t *testing.T) {
		list, err := repo.ListArtifacts(context.Background(), Filter{EdgeID: "nonexistent-edge"})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 artifacts for wrong edge, got %d", len(list))
		}
	})

	t.Run("filter by created after", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		list, err := repo.ListArtifacts(context.Background(), Filter{CreatedAfter: future})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 artifacts created in future, got %d", len(list))
		}
	})

	t.Run("filter by created before", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		list, err := repo.ListArtifacts(context.Background(), Filter{CreatedBefore: past})
		if err != nil {
			t.Fatalf("ListArtifacts: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 artifacts created in past hour, got %d", len(list))
		}
	})
}
