package pgvector

import "testing"

func TestValidateEmbeddingDimension(t *testing.T) {
	store := &Store{dimension: 3}

	if err := store.validateEmbedding([]float32{1, 2, 3}, false); err != nil {
		t.Fatalf("expected valid embedding, got %v", err)
	}
	if err := store.validateEmbedding([]float32{1, 2}, false); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestValidateEmbeddingAllowsEmptyWhenConfigured(t *testing.T) {
	store := &Store{dimension: 3}

	if err := store.validateEmbedding(nil, true); err != nil {
		t.Fatalf("expected empty embedding allowed, got %v", err)
	}
	if err := store.validateEmbedding([]float32{}, true); err != nil {
		t.Fatalf("expected empty embedding allowed, got %v", err)
	}
	if err := store.validateEmbedding([]float32{}, false); err == nil {
		t.Fatal("expected empty embedding error when not allowed")
	}
}
