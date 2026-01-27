package artifacts

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/observability"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

func TestLocalStore(t *testing.T) {
	// Create a test directory
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

func TestLocalStore_PersistsIndex(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	ctx := context.Background()
	artifactID := "persisted-1"
	payload := []byte("persisted data")
	if _, err := store.Put(ctx, artifactID, bytes.NewReader(payload), PutOptions{
		MimeType: "text/plain",
		Metadata: map[string]string{"type": "file"},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	store.Close()

	reloaded, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore reload: %v", err)
	}
	reader, err := reloaded.Get(ctx, artifactID)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("data = %q, want %q", data, payload)
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

func TestMemoryRepository_FiltersByContext(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	repo := NewMemoryRepository(store, nil)
	ctx := observability.AddSessionID(context.Background(), "session-1")
	ctx = observability.AddEdgeID(ctx, "edge-1")

	artifact := &pb.Artifact{
		Type:     "file",
		MimeType: "text/plain",
		Filename: "note.txt",
		Size:     4,
	}
	if err := repo.StoreArtifact(ctx, artifact, bytes.NewReader([]byte("test"))); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	results, err := repo.ListArtifacts(context.Background(), Filter{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(results))
	}

	edgeResults, err := repo.ListArtifacts(context.Background(), Filter{EdgeID: "edge-1"})
	if err != nil {
		t.Fatalf("ListArtifacts (edge): %v", err)
	}
	if len(edgeResults) != 1 {
		t.Fatalf("expected 1 edge artifact, got %d", len(edgeResults))
	}
}

func TestMemoryRepository_RedactedArtifact(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	repo := NewMemoryRepository(store, nil)
	ctx := context.Background()

	artifact := &pb.Artifact{
		Id:        "redacted-1",
		Type:      "screenshot",
		MimeType:  "image/png",
		Filename:  "screen.png",
		Size:      128,
		Reference: "redacted://redacted-1",
	}

	if err := repo.StoreArtifact(ctx, artifact, bytes.NewReader([]byte("secret"))); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	stored, reader, err := repo.GetArtifact(ctx, artifact.Id)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	defer reader.Close()

	if stored.Reference != "redacted://redacted-1" {
		t.Errorf("Reference = %q, want redacted://redacted-1", stored.Reference)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected no data for redacted artifact, got %d bytes", len(data))
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

func TestGetDefaultTTL_CaseInsensitive(t *testing.T) {
	tests := []struct {
		artifactType string
		wantDays     int
	}{
		{"SCREENSHOT", 7},
		{"Screenshot", 7},
		{"  screenshot  ", 7},
		{"RECORDING", 30},
	}

	for _, tt := range tests {
		got := GetDefaultTTL(tt.artifactType)
		want := time.Duration(tt.wantDays) * 24 * time.Hour
		if got != want {
			t.Errorf("GetDefaultTTL(%q) = %v, want %v", tt.artifactType, got, want)
		}
	}
}

func TestSetDefaultTTLs(t *testing.T) {
	// Store original TTLs
	origScreenshot := GetDefaultTTL("screenshot")

	t.Run("nil map is ignored", func(t *testing.T) {
		SetDefaultTTLs(nil)
		// Should not panic or change anything
	})

	t.Run("merges new TTLs", func(t *testing.T) {
		SetDefaultTTLs(map[string]time.Duration{
			"custom": 48 * time.Hour,
		})
		got := GetDefaultTTL("custom")
		if got != 48*time.Hour {
			t.Errorf("GetDefaultTTL(custom) = %v, want 48h", got)
		}

		// Original should still work
		got = GetDefaultTTL("screenshot")
		if got != origScreenshot {
			t.Errorf("GetDefaultTTL(screenshot) changed to %v", got)
		}
	})

	t.Run("overwrites existing TTLs", func(t *testing.T) {
		SetDefaultTTLs(map[string]time.Duration{
			"screenshot": 3 * 24 * time.Hour,
		})
		got := GetDefaultTTL("screenshot")
		if got != 3*24*time.Hour {
			t.Errorf("GetDefaultTTL(screenshot) = %v, want 72h", got)
		}
	})

	t.Run("ignores empty keys", func(t *testing.T) {
		SetDefaultTTLs(map[string]time.Duration{
			"":    time.Hour,
			"   ": 2 * time.Hour,
		})
		// Should not add empty keys
	})

	// Restore original
	SetDefaultTTLs(map[string]time.Duration{
		"screenshot": origScreenshot,
	})
}

func TestNewCleanupService(t *testing.T) {
	t.Run("uses provided interval", func(t *testing.T) {
		svc := NewCleanupService(nil, 30*time.Minute, nil)
		if svc.interval != 30*time.Minute {
			t.Errorf("interval = %v, want 30m", svc.interval)
		}
	})

	t.Run("defaults to 1 hour interval", func(t *testing.T) {
		svc := NewCleanupService(nil, 0, nil)
		if svc.interval != time.Hour {
			t.Errorf("interval = %v, want 1h", svc.interval)
		}
	})

	t.Run("uses default logger when nil", func(t *testing.T) {
		svc := NewCleanupService(nil, time.Hour, nil)
		if svc.logger == nil {
			t.Error("logger should not be nil")
		}
	})

	t.Run("creates stop channel", func(t *testing.T) {
		svc := NewCleanupService(nil, time.Hour, nil)
		if svc.stopCh == nil {
			t.Error("stopCh should not be nil")
		}
	})
}

func TestCleanupService_Stop(t *testing.T) {
	svc := NewCleanupService(nil, time.Hour, nil)

	// Should not panic
	svc.Stop()

	// Verify channel is closed
	select {
	case _, ok := <-svc.stopCh:
		if ok {
			t.Error("stopCh should be closed")
		}
	default:
		t.Error("stopCh should be readable after Stop()")
	}
}

func TestPutOptions_Struct(t *testing.T) {
	opts := PutOptions{
		MimeType: "image/png",
		TTL:      24 * time.Hour,
		Metadata: map[string]string{
			"type":   "screenshot",
			"source": "edge-1",
		},
	}

	if opts.MimeType != "image/png" {
		t.Errorf("MimeType = %q", opts.MimeType)
	}
	if opts.TTL != 24*time.Hour {
		t.Errorf("TTL = %v", opts.TTL)
	}
	if len(opts.Metadata) != 2 {
		t.Errorf("Metadata length = %d, want 2", len(opts.Metadata))
	}
}

func TestFilter_Struct(t *testing.T) {
	now := time.Now()
	filter := Filter{
		SessionID:     "session-123",
		EdgeID:        "edge-456",
		Type:          "screenshot",
		CreatedAfter:  now.Add(-24 * time.Hour),
		CreatedBefore: now,
		Limit:         10,
	}

	if filter.SessionID != "session-123" {
		t.Errorf("SessionID = %q", filter.SessionID)
	}
	if filter.EdgeID != "edge-456" {
		t.Errorf("EdgeID = %q", filter.EdgeID)
	}
	if filter.Limit != 10 {
		t.Errorf("Limit = %d", filter.Limit)
	}
}

func TestMetadata_Struct(t *testing.T) {
	now := time.Now()
	meta := Metadata{
		ID:         "artifact-123",
		SessionID:  "session-456",
		EdgeID:     "edge-789",
		Type:       "screenshot",
		MimeType:   "image/png",
		Filename:   "screen.png",
		Size:       1024,
		Reference:  "s3://bucket/screen.png",
		TTLSeconds: 86400,
		CreatedAt:  now,
		ExpiresAt:  now.Add(24 * time.Hour),
	}

	if meta.ID != "artifact-123" {
		t.Errorf("ID = %q", meta.ID)
	}
	if meta.Size != 1024 {
		t.Errorf("Size = %d", meta.Size)
	}
	if meta.TTLSeconds != 86400 {
		t.Errorf("TTLSeconds = %d", meta.TTLSeconds)
	}
}

func TestMemoryRepository_PruneExpired(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	repo := NewMemoryRepository(store, nil)
	ctx := context.Background()

	// Create an artifact with very short TTL
	artifact := &pb.Artifact{
		Type:       "file",
		MimeType:   "text/plain",
		Filename:   "test.txt",
		Size:       4,
		TtlSeconds: 1, // 1 second TTL
	}
	if err := repo.StoreArtifact(ctx, artifact, bytes.NewReader([]byte("test"))); err != nil {
		t.Fatalf("StoreArtifact: %v", err)
	}

	// Verify artifact exists
	_, _, err = repo.GetArtifact(ctx, artifact.Id)
	if err != nil {
		t.Fatalf("GetArtifact before expiry: %v", err)
	}

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Prune expired artifacts
	count, err := repo.PruneExpired(ctx)
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if count != 1 {
		t.Errorf("PruneExpired count = %d, want 1", count)
	}

	// Verify artifact is gone
	_, _, err = repo.GetArtifact(ctx, artifact.Id)
	if err == nil {
		t.Error("GetArtifact should fail after prune")
	}
}

func TestMemoryRepository_ListArtifacts_Limit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	repo := NewMemoryRepository(store, nil)
	ctx := context.Background()

	// Create multiple artifacts
	for i := 0; i < 5; i++ {
		artifact := &pb.Artifact{
			Type:     "file",
			MimeType: "text/plain",
			Filename: "test.txt",
			Size:     4,
		}
		if err := repo.StoreArtifact(ctx, artifact, bytes.NewReader([]byte("test"))); err != nil {
			t.Fatalf("StoreArtifact: %v", err)
		}
	}

	// List with limit
	results, err := repo.ListArtifacts(ctx, Filter{Limit: 3})
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("ListArtifacts returned %d artifacts, want 3", len(results))
	}
}

func TestExtensionForMime(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"video/mp4", ".mp4"},
		{"video/webm", ".webm"},
		{"application/pdf", ".pdf"},
		{"text/plain", ".txt"},
		{"application/json", ".json"},
		{"application/octet-stream", ".dat"},
		{"unknown/type", ".dat"},
		{"", ".dat"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := extensionForMime(tt.mimeType)
			if result != tt.expected {
				t.Errorf("extensionForMime(%q) = %q, want %q", tt.mimeType, result, tt.expected)
			}
		})
	}
}

func TestLocalStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_, err = store.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent artifact")
	}
}

func TestLocalStore_DeleteNonexistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Should not return error for nonexistent artifact
	err = store.Delete(ctx, "nonexistent-id")
	if err != nil {
		t.Errorf("Delete nonexistent: %v", err)
	}
}

func TestLocalStore_ExistsNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	exists, err := store.Exists(ctx, "nonexistent-id")
	if err != nil {
		t.Errorf("Exists: %v", err)
	}
	if exists {
		t.Error("expected exists=false for nonexistent artifact")
	}
}

func TestLocalStore_PutDifferentTypes(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	types := []struct {
		mimeType     string
		metadataType string
		ext          string
	}{
		{"image/png", "screenshot", ".png"},
		{"image/jpeg", "photo", ".jpg"},
		{"video/mp4", "recording", ".mp4"},
		{"application/pdf", "document", ".pdf"},
	}

	for _, tt := range types {
		t.Run(tt.mimeType, func(t *testing.T) {
			id := "artifact-" + tt.metadataType
			ref, err := store.Put(ctx, id, bytes.NewReader([]byte("data")), PutOptions{
				MimeType: tt.mimeType,
				Metadata: map[string]string{"type": tt.metadataType},
			})
			if err != nil {
				t.Fatalf("Put: %v", err)
			}
			if ref == "" {
				t.Error("expected non-empty reference")
			}
		})
	}
}
