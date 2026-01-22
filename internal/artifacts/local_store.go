package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LocalStore stores artifacts on the local filesystem.
type LocalStore struct {
	mu        sync.RWMutex
	basePath  string
	indexPath string
	index     map[string]string // artifactID -> relative path
}

// NewLocalStore creates a local disk store.
func NewLocalStore(basePath string) (*LocalStore, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	store := &LocalStore{
		basePath:  basePath,
		indexPath: filepath.Join(basePath, "index.json"),
		index:     make(map[string]string),
	}
	if err := store.loadIndex(); err != nil {
		return nil, err
	}
	return store, nil
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
	err = s.persistIndexLocked()
	s.mu.Unlock()
	if err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("persist artifact index: %w", err)
	}

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
	s.mu.RLock()
	relPath, ok := s.index[artifactID]
	s.mu.RUnlock()

	if !ok {
		return nil // Already deleted
	}

	filePath := filepath.Join(s.basePath, relPath)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	s.mu.Lock()
	delete(s.index, artifactID)
	err := s.persistIndexLocked()
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("persist artifact index: %w", err)
	}
	return nil
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

func (s *LocalStore) loadIndex() error {
	data, err := os.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read artifact index: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var stored map[string]string
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("parse artifact index: %w", err)
	}
	if stored != nil {
		s.index = stored
	}
	return nil
}

func (s *LocalStore) persistIndexLocked() error {
	data, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(s.indexPath); err == nil {
		mode = info.Mode().Perm()
	}
	tmpPath := s.indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.indexPath)
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
