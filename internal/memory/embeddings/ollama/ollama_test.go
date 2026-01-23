package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		p, err := New(Config{})
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if p.baseURL != "http://localhost:11434" {
			t.Errorf("baseURL = %q, want %q", p.baseURL, "http://localhost:11434")
		}
		if p.model != "nomic-embed-text" {
			t.Errorf("model = %q, want %q", p.model, "nomic-embed-text")
		}
		if p.client == nil {
			t.Error("client should not be nil")
		}
	})

	t.Run("custom config", func(t *testing.T) {
		p, err := New(Config{
			BaseURL: "http://custom:8080",
			Model:   "mxbai-embed-large",
		})
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if p.baseURL != "http://custom:8080" {
			t.Errorf("baseURL = %q, want %q", p.baseURL, "http://custom:8080")
		}
		if p.model != "mxbai-embed-large" {
			t.Errorf("model = %q, want %q", p.model, "mxbai-embed-large")
		}
	})
}

func TestProvider_Name(t *testing.T) {
	p, _ := New(Config{})
	if name := p.Name(); name != "ollama" {
		t.Errorf("Name() = %q, want %q", name, "ollama")
	}
}

func TestProvider_Dimension(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"nomic-embed-text", 768},
		{"mxbai-embed-large", 1024},
		{"all-minilm", 384},
		{"unknown-model", 768}, // default
		{"", 768},              // empty defaults to nomic-embed-text (768)
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			cfg := Config{Model: tt.model}
			if tt.model == "" {
				cfg.Model = "" // Let New set default
			}
			p, err := New(cfg)
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			// For empty model, the constructor sets it to nomic-embed-text
			if dim := p.Dimension(); dim != tt.expected {
				t.Errorf("Dimension() = %d, want %d", dim, tt.expected)
			}
		})
	}
}

func TestProvider_MaxBatchSize(t *testing.T) {
	p, _ := New(Config{})
	if max := p.MaxBatchSize(); max != 100 {
		t.Errorf("MaxBatchSize() = %d, want %d", max, 100)
	}
}

func TestProvider_Embed(t *testing.T) {
	t.Run("successful embed", func(t *testing.T) {
		expectedEmbedding := []float32{0.1, 0.2, 0.3}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/api/embeddings" {
				t.Errorf("path = %s, want /api/embeddings", r.URL.Path)
			}

			var req embeddingRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request: %v", err)
			}
			if req.Prompt != "test text" {
				t.Errorf("prompt = %q, want %q", req.Prompt, "test text")
			}

			resp := embeddingResponse{Embedding: expectedEmbedding}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, _ := New(Config{BaseURL: server.URL})
		embedding, err := p.Embed(context.Background(), "test text")
		if err != nil {
			t.Fatalf("Embed error: %v", err)
		}

		if len(embedding) != len(expectedEmbedding) {
			t.Fatalf("embedding length = %d, want %d", len(embedding), len(expectedEmbedding))
		}
		for i, v := range embedding {
			if v != expectedEmbedding[i] {
				t.Errorf("embedding[%d] = %f, want %f", i, v, expectedEmbedding[i])
			}
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
		}))
		defer server.Close()

		p, _ := New(Config{BaseURL: server.URL})
		_, err := p.Embed(context.Background(), "test")
		if err == nil {
			t.Error("expected error for server error")
		}
	})

	t.Run("invalid response JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		p, _ := New(Config{BaseURL: server.URL})
		_, err := p.Embed(context.Background(), "test")
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Delay response
			<-r.Context().Done()
		}))
		defer server.Close()

		p, _ := New(Config{BaseURL: server.URL})
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := p.Embed(ctx, "test")
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

func TestProvider_EmbedBatch(t *testing.T) {
	t.Run("successful batch embed", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			resp := embeddingResponse{Embedding: []float32{float32(callCount) * 0.1}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, _ := New(Config{BaseURL: server.URL})
		embeddings, err := p.EmbedBatch(context.Background(), []string{"text1", "text2", "text3"})
		if err != nil {
			t.Fatalf("EmbedBatch error: %v", err)
		}

		if len(embeddings) != 3 {
			t.Fatalf("embeddings length = %d, want 3", len(embeddings))
		}
		if callCount != 3 {
			t.Errorf("callCount = %d, want 3 (one per text)", callCount)
		}
	})

	t.Run("empty batch", func(t *testing.T) {
		p, _ := New(Config{})
		embeddings, err := p.EmbedBatch(context.Background(), []string{})
		if err != nil {
			t.Fatalf("EmbedBatch error: %v", err)
		}
		if len(embeddings) != 0 {
			t.Errorf("embeddings length = %d, want 0", len(embeddings))
		}
	})

	t.Run("error in batch", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			resp := embeddingResponse{Embedding: []float32{0.1}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p, _ := New(Config{BaseURL: server.URL})
		_, err := p.EmbedBatch(context.Background(), []string{"text1", "text2", "text3"})
		if err == nil {
			t.Error("expected error when one embed fails")
		}
	})
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		BaseURL: "http://example.com",
		Model:   "test-model",
	}
	if cfg.BaseURL != "http://example.com" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "http://example.com")
	}
	if cfg.Model != "test-model" {
		t.Errorf("Model = %q, want %q", cfg.Model, "test-model")
	}
}
