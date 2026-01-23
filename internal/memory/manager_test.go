package memory

import (
	"testing"
	"time"
)

func TestNewEmbeddingCache(t *testing.T) {
	cache := newEmbeddingCache(10)
	if cache == nil {
		t.Fatal("newEmbeddingCache returned nil")
	}
	if cache.capacity != 10 {
		t.Errorf("capacity = %d, want 10", cache.capacity)
	}
	if cache.items == nil {
		t.Error("items map should be initialized")
	}
}

func TestEmbeddingCache_SetAndGet(t *testing.T) {
	cache := newEmbeddingCache(10)

	embedding := []float32{0.1, 0.2, 0.3}
	cache.set("key1", embedding)

	got, ok := cache.get("key1")
	if !ok {
		t.Error("expected key1 to be found")
	}
	if len(got) != len(embedding) {
		t.Errorf("got embedding length %d, want %d", len(got), len(embedding))
	}
	for i, v := range got {
		if v != embedding[i] {
			t.Errorf("got[%d] = %f, want %f", i, v, embedding[i])
		}
	}
}

func TestEmbeddingCache_GetMiss(t *testing.T) {
	cache := newEmbeddingCache(10)

	_, ok := cache.get("nonexistent")
	if ok {
		t.Error("expected miss for nonexistent key")
	}
}

func TestEmbeddingCache_Update(t *testing.T) {
	cache := newEmbeddingCache(10)

	cache.set("key1", []float32{0.1})
	cache.set("key1", []float32{0.2, 0.3})

	got, ok := cache.get("key1")
	if !ok {
		t.Error("expected key1 to be found after update")
	}
	if len(got) != 2 {
		t.Errorf("got embedding length %d, want 2", len(got))
	}
	if got[0] != 0.2 {
		t.Errorf("got[0] = %f, want 0.2", got[0])
	}
}

func TestEmbeddingCache_Eviction(t *testing.T) {
	cache := newEmbeddingCache(3)

	// Fill cache
	cache.set("key1", []float32{1.0})
	cache.set("key2", []float32{2.0})
	cache.set("key3", []float32{3.0})

	// Add one more, should evict key1 (LRU)
	cache.set("key4", []float32{4.0})

	// key1 should be evicted
	_, ok := cache.get("key1")
	if ok {
		t.Error("key1 should have been evicted")
	}

	// key2, key3, key4 should still exist
	if _, ok := cache.get("key2"); !ok {
		t.Error("key2 should still exist")
	}
	if _, ok := cache.get("key3"); !ok {
		t.Error("key3 should still exist")
	}
	if _, ok := cache.get("key4"); !ok {
		t.Error("key4 should still exist")
	}
}

func TestEmbeddingCache_LRUOrder(t *testing.T) {
	cache := newEmbeddingCache(3)

	// Fill cache
	cache.set("key1", []float32{1.0})
	cache.set("key2", []float32{2.0})
	cache.set("key3", []float32{3.0})

	// Access key1, making it most recently used
	cache.get("key1")

	// Add new key, should evict key2 (now LRU)
	cache.set("key4", []float32{4.0})

	// key2 should be evicted (it was LRU after key1 was accessed)
	if _, ok := cache.get("key2"); ok {
		t.Error("key2 should have been evicted")
	}

	// key1 should still exist
	if _, ok := cache.get("key1"); !ok {
		t.Error("key1 should still exist after access")
	}
}

func TestEmbeddingCache_EmptyCapacity(t *testing.T) {
	// Cache with 0 capacity should still work but evict immediately
	cache := newEmbeddingCache(0)
	cache.set("key1", []float32{1.0})

	// With 0 capacity, the item should be evicted immediately
	if len(cache.items) > 0 {
		t.Error("cache with 0 capacity should evict immediately")
	}
}

func TestEmbeddingCache_SingleElement(t *testing.T) {
	cache := newEmbeddingCache(1)

	cache.set("key1", []float32{1.0})
	cache.set("key2", []float32{2.0})

	// Only key2 should remain
	if _, ok := cache.get("key1"); ok {
		t.Error("key1 should have been evicted")
	}
	if _, ok := cache.get("key2"); !ok {
		t.Error("key2 should exist")
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Backend:   "sqlite-vec",
		Dimension: 1536,
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Backend != "sqlite-vec" {
		t.Errorf("Backend = %q, want %q", cfg.Backend, "sqlite-vec")
	}
	if cfg.Dimension != 1536 {
		t.Errorf("Dimension = %d, want 1536", cfg.Dimension)
	}
}

func TestSQLiteVecConfig_Struct(t *testing.T) {
	cfg := SQLiteVecConfig{
		Path: "/path/to/db.sqlite",
	}

	if cfg.Path != "/path/to/db.sqlite" {
		t.Errorf("Path = %q, want %q", cfg.Path, "/path/to/db.sqlite")
	}
}

func TestPgvectorConfig_Struct(t *testing.T) {
	runMigrations := true
	cfg := PgvectorConfig{
		DSN:            "postgres://localhost/test",
		UseCockroachDB: true,
		RunMigrations:  &runMigrations,
	}

	if cfg.DSN != "postgres://localhost/test" {
		t.Errorf("DSN = %q, want %q", cfg.DSN, "postgres://localhost/test")
	}
	if !cfg.UseCockroachDB {
		t.Error("UseCockroachDB should be true")
	}
	if cfg.RunMigrations == nil || !*cfg.RunMigrations {
		t.Error("RunMigrations should be true")
	}
}

func TestLanceDBConfig_Struct(t *testing.T) {
	cfg := LanceDBConfig{
		Path:         "/path/to/lancedb",
		IndexType:    "ivf_pq",
		MetricType:   "cosine",
		NProbes:      10,
		EF:           100,
		RefineFactor: 5,
	}

	if cfg.Path != "/path/to/lancedb" {
		t.Errorf("Path = %q, want %q", cfg.Path, "/path/to/lancedb")
	}
	if cfg.IndexType != "ivf_pq" {
		t.Errorf("IndexType = %q, want %q", cfg.IndexType, "ivf_pq")
	}
	if cfg.MetricType != "cosine" {
		t.Errorf("MetricType = %q, want %q", cfg.MetricType, "cosine")
	}
	if cfg.NProbes != 10 {
		t.Errorf("NProbes = %d, want 10", cfg.NProbes)
	}
	if cfg.EF != 100 {
		t.Errorf("EF = %d, want 100", cfg.EF)
	}
	if cfg.RefineFactor != 5 {
		t.Errorf("RefineFactor = %d, want 5", cfg.RefineFactor)
	}
}

func TestEmbeddingsConfig_Struct(t *testing.T) {
	cfg := EmbeddingsConfig{
		Provider:  "openai",
		APIKey:    "sk-test-key",
		BaseURL:   "https://api.openai.com",
		Model:     "text-embedding-ada-002",
		OllamaURL: "http://localhost:11434",
		ProjectID: "project-123",
		Location:  "us-central1",
	}

	if cfg.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "openai")
	}
	if cfg.Model != "text-embedding-ada-002" {
		t.Errorf("Model = %q, want %q", cfg.Model, "text-embedding-ada-002")
	}
	if cfg.OllamaURL != "http://localhost:11434" {
		t.Errorf("OllamaURL = %q, want %q", cfg.OllamaURL, "http://localhost:11434")
	}
}

func TestIndexingConfig_Struct(t *testing.T) {
	cfg := IndexingConfig{
		AutoIndexMessages: true,
		MinContentLength:  20,
		BatchSize:         50,
	}

	if !cfg.AutoIndexMessages {
		t.Error("AutoIndexMessages should be true")
	}
	if cfg.MinContentLength != 20 {
		t.Errorf("MinContentLength = %d, want 20", cfg.MinContentLength)
	}
	if cfg.BatchSize != 50 {
		t.Errorf("BatchSize = %d, want 50", cfg.BatchSize)
	}
}

func TestSearchConfig_Struct(t *testing.T) {
	cfg := SearchConfig{
		DefaultLimit:     15,
		DefaultThreshold: 0.8,
		DefaultScope:     "global",
	}

	if cfg.DefaultLimit != 15 {
		t.Errorf("DefaultLimit = %d, want 15", cfg.DefaultLimit)
	}
	if cfg.DefaultThreshold != 0.8 {
		t.Errorf("DefaultThreshold = %f, want 0.8", cfg.DefaultThreshold)
	}
	if cfg.DefaultScope != "global" {
		t.Errorf("DefaultScope = %q, want %q", cfg.DefaultScope, "global")
	}
}

func TestStats_Struct(t *testing.T) {
	stats := Stats{
		TotalEntries:      1000,
		Backend:           "pgvector",
		EmbeddingProvider: "openai",
		EmbeddingModel:    "text-embedding-ada-002",
		Dimension:         1536,
	}

	if stats.TotalEntries != 1000 {
		t.Errorf("TotalEntries = %d, want 1000", stats.TotalEntries)
	}
	if stats.Backend != "pgvector" {
		t.Errorf("Backend = %q, want %q", stats.Backend, "pgvector")
	}
	if stats.EmbeddingProvider != "openai" {
		t.Errorf("EmbeddingProvider = %q, want %q", stats.EmbeddingProvider, "openai")
	}
	if stats.Dimension != 1536 {
		t.Errorf("Dimension = %d, want 1536", stats.Dimension)
	}
}

func TestNewManager_Nil(t *testing.T) {
	mgr, err := NewManager(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if mgr != nil {
		t.Error("expected nil manager for nil config")
	}
}

func TestNewManager_Disabled(t *testing.T) {
	mgr, err := NewManager(&Config{Enabled: false})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if mgr != nil {
		t.Error("expected nil manager for disabled config")
	}
}

func TestNewManager_UnknownBackend(t *testing.T) {
	cfg := &Config{
		Enabled: true,
		Backend: "unknown-backend",
	}

	_, err := NewManager(cfg)
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestLruNode_Struct(t *testing.T) {
	node := lruNode{
		key:   "test-key",
		value: []float32{1.0, 2.0, 3.0},
	}

	if node.key != "test-key" {
		t.Errorf("key = %q, want %q", node.key, "test-key")
	}
	if len(node.value) != 3 {
		t.Errorf("value length = %d, want 3", len(node.value))
	}
}

func TestEmbeddingCache_MoveToFront_AlreadyAtFront(t *testing.T) {
	cache := newEmbeddingCache(5)

	cache.set("key1", []float32{1.0})
	cache.set("key2", []float32{2.0})

	// key2 is already at front, access it again
	cache.get("key2")

	// Should still work correctly
	if cache.head.key != "key2" {
		t.Errorf("head.key = %q, want %q", cache.head.key, "key2")
	}
}

func TestEmbeddingCache_ConcurrentAccess(t *testing.T) {
	cache := newEmbeddingCache(100)

	// Simple concurrent test to ensure no deadlocks
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			cache.set("key-a", []float32{float32(i)})
			cache.get("key-a")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			cache.set("key-b", []float32{float32(i)})
			cache.get("key-b")
		}
		done <- true
	}()

	// Wait with timeout
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent access test timed out")
		}
	}
}
