package openai

import (
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("missing API key returns error", func(t *testing.T) {
		_, err := New(Config{})
		if err == nil {
			t.Error("expected error for missing API key")
		}
	})

	t.Run("API key provided succeeds", func(t *testing.T) {
		p, err := New(Config{APIKey: "test-key"})
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if p.client == nil {
			t.Error("client should not be nil")
		}
		if p.model != "text-embedding-3-small" {
			t.Errorf("model = %q, want %q", p.model, "text-embedding-3-small")
		}
	})

	t.Run("custom model", func(t *testing.T) {
		p, err := New(Config{
			APIKey: "test-key",
			Model:  "text-embedding-3-large",
		})
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if p.model != "text-embedding-3-large" {
			t.Errorf("model = %q, want %q", p.model, "text-embedding-3-large")
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		p, err := New(Config{
			APIKey:  "test-key",
			BaseURL: "http://custom-endpoint.com",
		})
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if p.client == nil {
			t.Error("client should not be nil")
		}
	})
}

func TestProvider_Name(t *testing.T) {
	p, _ := New(Config{APIKey: "test-key"})
	if name := p.Name(); name != "openai" {
		t.Errorf("Name() = %q, want %q", name, "openai")
	}
}

func TestProvider_Dimension(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"text-embedding-3-small", 1536},
		{"text-embedding-3-large", 3072},
		{"text-embedding-ada-002", 1536},
		{"unknown-model", 1536}, // default
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p, err := New(Config{
				APIKey: "test-key",
				Model:  tt.model,
			})
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			if dim := p.Dimension(); dim != tt.expected {
				t.Errorf("Dimension() = %d, want %d", dim, tt.expected)
			}
		})
	}
}

func TestProvider_MaxBatchSize(t *testing.T) {
	p, _ := New(Config{APIKey: "test-key"})
	if max := p.MaxBatchSize(); max != 2048 {
		t.Errorf("MaxBatchSize() = %d, want %d", max, 2048)
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		APIKey:  "my-api-key",
		BaseURL: "http://example.com",
		Model:   "test-model",
	}
	if cfg.APIKey != "my-api-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "my-api-key")
	}
	if cfg.BaseURL != "http://example.com" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "http://example.com")
	}
	if cfg.Model != "test-model" {
		t.Errorf("Model = %q, want %q", cfg.Model, "test-model")
	}
}

// Note: Testing Embed and EmbedBatch would require mocking the OpenAI client,
// which is more complex since it uses the go-openai SDK. The basic functionality
// tests above provide coverage for the constructor, getters, and config handling.
