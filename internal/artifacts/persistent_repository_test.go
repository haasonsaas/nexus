package artifacts

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

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
