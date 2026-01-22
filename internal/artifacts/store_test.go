package artifacts

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

func TestLocalStore(t *testing.T) {
	// Create temp directory
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	artifactID := "test-artifact-123"
	data := []byte("hello world")

	// Test Put
	ref, err := store.Put(ctx, artifactID, bytes.NewReader(data), PutOptions{
		MimeType: "text/plain",
		Metadata: map[string]string{"type": "file"},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ref == "" {
		t.Error("Put returned empty reference")
	}

	// Test Exists
	exists, err := store.Exists(ctx, artifactID)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("Exists returned false for stored artifact")
	}

	// Test Get
	reader, err := store.Get(ctx, artifactID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer reader.Close()

	retrieved, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(retrieved, data) {
		t.Errorf("Get returned %q, want %q", retrieved, data)
	}

	// Test Delete
	if err := store.Delete(ctx, artifactID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, err = store.Exists(ctx, artifactID)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("Exists returned true after delete")
	}
}

func TestLocalStore_DirectoryStructure(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Store a screenshot
	_, err = store.Put(ctx, "screenshot-1", bytes.NewReader([]byte("png data")), PutOptions{
		MimeType: "image/png",
		Metadata: map[string]string{"type": "screenshot"},
	})
	if err != nil {
		t.Fatalf("Put screenshot: %v", err)
	}

	// Check directory structure exists
	now := time.Now()
	expectedDir := filepath.Join(dir, "screenshot",
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"))

	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Expected directory %s does not exist", expectedDir)
	}
}

func TestMemoryRepository(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	repo := NewMemoryRepository(store, nil)
	ctx := context.Background()

	// Test StoreArtifact with small data (inline)
	artifact := &pb.Artifact{
		Type:     "screenshot",
		MimeType: "image/png",
		Filename: "test.png",
		Size:     100,
	}
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}

	err = repo.StoreArtifact(ctx, artifact, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}
	if artifact.Id == "" {
		t.Error("StoreArtifact did not set ID")
	}
	if artifact.Reference == "" {
		t.Error("StoreArtifact did not set Reference")
	}

	// Test GetArtifact
	retrieved, reader, err := repo.GetArtifact(ctx, artifact.Id)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	defer reader.Close()

	if retrieved.Type != artifact.Type {
		t.Errorf("Type = %q, want %q", retrieved.Type, artifact.Type)
	}

	readData, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(readData, data) {
		t.Error("Retrieved data does not match stored data")
	}

	// Test ListArtifacts
	artifacts, err := repo.ListArtifacts(ctx, Filter{Type: "screenshot"})
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Errorf("ListArtifacts returned %d artifacts, want 1", len(artifacts))
	}

	// Test DeleteArtifact
	err = repo.DeleteArtifact(ctx, artifact.Id)
	if err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	_, _, err = repo.GetArtifact(ctx, artifact.Id)
	if err == nil {
		t.Error("GetArtifact should fail after delete")
	}
}

func TestMemoryRepository_LargeArtifact(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	repo := NewMemoryRepository(store, nil)
	ctx := context.Background()

	// Create artifact larger than inline threshold (1MB)
	largeData := make([]byte, 2*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	artifact := &pb.Artifact{
		Type:     "recording",
		MimeType: "video/mp4",
		Filename: "large.mp4",
		Size:     int64(len(largeData)),
	}

	err = repo.StoreArtifact(ctx, artifact, bytes.NewReader(largeData))
	if err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	// Should be stored in backend, not inline
	if artifact.Reference == "" || artifact.Reference[:7] == "inline:" {
		t.Error("Large artifact should not be stored inline")
	}

	// Retrieve and verify
	_, reader, err := repo.GetArtifact(ctx, artifact.Id)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	defer reader.Close()

	readData, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(readData, largeData) {
		t.Error("Retrieved data does not match stored data")
	}
}

func TestGetDefaultTTL(t *testing.T) {
	tests := []struct {
		artifactType string
		wantDays     int
	}{
		{"screenshot", 7},
		{"recording", 30},
		{"file", 14},
		{"unknown", 1},
		{"", 1},
	}

	for _, tt := range tests {
		got := GetDefaultTTL(tt.artifactType)
		want := time.Duration(tt.wantDays) * 24 * time.Hour
		if got != want {
			t.Errorf("GetDefaultTTL(%q) = %v, want %v", tt.artifactType, got, want)
		}
	}
}
