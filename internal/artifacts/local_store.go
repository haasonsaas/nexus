package artifacts

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LocalStore stores artifacts on the local filesystem.
type LocalStore struct {
	mu       sync.RWMutex
	basePath string
	index    map[string]string // artifactID -> relative path
}

// NewLocalStore creates a local disk store.
func NewLocalStore(basePath string) (*LocalStore, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	return &LocalStore{
		basePath: basePath,
		index:    make(map[string]string),
	}, nil
}

// Put stores artifact data on disk.
func (s *LocalStore) Put(ctx context.Context, artifactID string, data io.Reader, opts PutOptions) (string, error) {
	artifactType := "unknown"
	if t, ok := opts.Metadata["type"]; ok {
		artifactType = t
	}

	// Create path: base/type/YYYY/MM/DD/
	now := time.Now()
	dir := filepath.Join(s.basePath, artifactType,
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		fmt.Sprintf("%02d", now.Day()))

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}

	// Determine extension from MIME type
	ext := extensionForMime(opts.MimeType)
	filename := artifactID + ext
	filePath := filepath.Join(dir, filename)

	// Write to temp file first, then atomic rename
	tmpPath := filePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, data); err != nil {
		f.Close()
		os.Remove(tmpPath) //nolint:errcheck
		return "", fmt.Errorf("write artifact: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return "", fmt.Errorf("rename artifact: %w", err)
	}

	// Track in index
	relPath := filepath.Join(artifactType,
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		fmt.Sprintf("%02d", now.Day()),
		filename)

	s.mu.Lock()
	s.index[artifactID] = relPath
	s.mu.Unlock()

	// Reference format: file:///path/to/artifact
	reference := fmt.Sprintf("file://%s", filePath)
	return reference, nil
}

// Get retrieves artifact data by ID.
func (s *LocalStore) Get(ctx context.Context, artifactID string) (io.ReadCloser, error) {
	s.mu.RLock()
	relPath, ok := s.index[artifactID]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("artifact not found: %s", artifactID)
	}

	filePath := filepath.Join(s.basePath, relPath)
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	return f, nil
}

// Delete removes an artifact from disk.
func (s *LocalStore) Delete(ctx context.Context, artifactID string) error {
	s.mu.Lock()
	relPath, ok := s.index[artifactID]
	if ok {
		delete(s.index, artifactID)
	}
	s.mu.Unlock()

	if !ok {
		return nil // Already deleted
	}

	filePath := filepath.Join(s.basePath, relPath)
	return os.Remove(filePath)
}

// Exists checks if an artifact exists.
func (s *LocalStore) Exists(ctx context.Context, artifactID string) (bool, error) {
	s.mu.RLock()
	relPath, ok := s.index[artifactID]
	s.mu.RUnlock()

	if !ok {
		return false, nil
	}

	filePath := filepath.Join(s.basePath, relPath)
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Close releases resources.
func (s *LocalStore) Close() error {
	return nil
}

// extensionForMime returns a file extension for a MIME type.
func extensionForMime(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "application/json":
		return ".json"
	default:
		return ".dat"
	}
}
