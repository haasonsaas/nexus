package memorysearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemorySearchToolExecute(t *testing.T) {
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("Remember alpha bravo"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "2026-01-21.md"), []byte("alpha log entry"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewMemorySearchTool(&Config{
		Directory:     "memory",
		MemoryFile:    "MEMORY.md",
		WorkspacePath: dir,
		MaxResults:    5,
		MaxSnippetLen: 120,
	})

	params, _ := json.Marshal(map[string]any{"query": "alpha"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if result.Content == "" {
		t.Fatalf("expected content")
	}
}

func TestMemorySearchToolVectorMode(t *testing.T) {
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "2026-01-21.md"), []byte("vector search content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := NewMemorySearchTool(&Config{
		Directory:     "memory",
		WorkspacePath: dir,
		Mode:          "vector",
	})

	params, _ := json.Marshal(map[string]any{"query": "vector"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if result.Content == "" {
		t.Fatalf("expected content")
	}
}

func TestMemorySearchToolRemoteEmbeddingsCache(t *testing.T) {
	dir := t.TempDir()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "2026-01-21.md"), []byte("alpha remote embed test"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request error: %v", err)
		}
		if req.Model == "" {
			t.Errorf("expected model in request")
		}
		if len(req.Input) == 0 {
			t.Errorf("expected input in request")
		}
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"embedding": []float64{1, 1, 1}, "index": i}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	tool := NewMemorySearchTool(&Config{
		Directory:     "memory",
		WorkspacePath: dir,
		Mode:          "vector",
		Embeddings: EmbeddingsConfig{
			BaseURL:  server.URL,
			Model:    "test-emb",
			CacheDir: filepath.Join(dir, "cache"),
			CacheTTL: time.Hour,
		},
	})

	params, _ := json.Marshal(map[string]any{"query": "alpha"})
	for i := 0; i < 2; i++ {
		result, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.Content)
		}
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 embeddings request, got %d", got)
	}
}
