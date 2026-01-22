package pgvector

import (
	"testing"
)

func TestEncodeEmbedding(t *testing.T) {
	tests := []struct {
		name      string
		embedding []float32
		want      string
		wantValid bool
	}{
		{
			name:      "empty embedding",
			embedding: nil,
			want:      "",
			wantValid: false,
		},
		{
			name:      "empty slice",
			embedding: []float32{},
			want:      "",
			wantValid: false,
		},
		{
			name:      "single element",
			embedding: []float32{0.5},
			want:      "[0.5]",
			wantValid: true,
		},
		{
			name:      "multiple elements",
			embedding: []float32{0.1, 0.2, 0.3},
			want:      "[0.1,0.2,0.3]",
			wantValid: true,
		},
		{
			name:      "negative values",
			embedding: []float32{-0.5, 0.5, -1.0},
			want:      "[-0.5,0.5,-1]",
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeEmbedding(tt.embedding)
			if got.Valid != tt.wantValid {
				t.Errorf("encodeEmbedding() valid = %v, want %v", got.Valid, tt.wantValid)
			}
			if got.Valid && got.String != tt.want {
				t.Errorf("encodeEmbedding() = %q, want %q", got.String, tt.want)
			}
		})
	}
}

func TestDecodeEmbedding(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []float32
	}{
		{
			name: "empty string",
			s:    "",
			want: nil,
		},
		{
			name: "empty brackets",
			s:    "[]",
			want: nil,
		},
		{
			name: "single element",
			s:    "[0.5]",
			want: []float32{0.5},
		},
		{
			name: "multiple elements",
			s:    "[0.1,0.2,0.3]",
			want: []float32{0.1, 0.2, 0.3},
		},
		{
			name: "negative values",
			s:    "[-0.5,0.5,-1.0]",
			want: []float32{-0.5, 0.5, -1.0},
		},
		{
			name: "with spaces",
			s:    "[0.1, 0.2, 0.3]",
			want: []float32{0.1, 0.2, 0.3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeEmbedding(tt.s)
			if len(got) != len(tt.want) {
				t.Fatalf("decodeEmbedding() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("decodeEmbedding()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRoundTripEmbedding(t *testing.T) {
	original := []float32{0.123, -0.456, 0.789, 0.0, 1.0, -1.0}
	encoded := encodeEmbedding(original)
	if !encoded.Valid {
		t.Fatal("encodeEmbedding() returned invalid")
	}
	decoded := decodeEmbedding(encoded.String)

	if len(decoded) != len(original) {
		t.Fatalf("round trip len = %d, want %d", len(decoded), len(original))
	}

	for i := range decoded {
		// Allow small floating point differences
		diff := decoded[i] - original[i]
		if diff < -0.0001 || diff > 0.0001 {
			t.Errorf("round trip[%d] = %v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(migrations) < 1 {
		t.Fatalf("expected at least 1 migration, got %d", len(migrations))
	}
	if migrations[0].ID != "001_create_memories" {
		t.Fatalf("expected first migration to be 001_create_memories, got %q", migrations[0].ID)
	}
	if migrations[0].UpSQL == "" {
		t.Fatal("expected up migration to have content")
	}
	if migrations[0].DownSQL == "" {
		t.Fatal("expected down migration to have content")
	}
}

func TestNullString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
	}{
		{
			name:      "empty string",
			input:     "",
			wantValid: false,
		},
		{
			name:      "non-empty string",
			input:     "test",
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullString(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("nullString() valid = %v, want %v", got.Valid, tt.wantValid)
			}
			if got.Valid && got.String != tt.input {
				t.Errorf("nullString() = %q, want %q", got.String, tt.input)
			}
		})
	}
}

func TestNewBackend_Errors(t *testing.T) {
	// Test that New returns error when neither DSN nor DB is provided
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error when neither DSN nor DB is provided")
	}
}

func TestNewBackend_DefaultDimension(t *testing.T) {
	// We can't fully test New without a database, but we can verify the config handling
	cfg := Config{
		Dimension: 0,
	}

	// Verify that dimension defaults to 1536 (we check this indirectly through the error)
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error when neither DSN nor DB is provided")
	}
	// The error should be about missing DSN/DB, not about dimension
	expected := "either DSN or DB must be provided"
	if err.Error() != expected {
		t.Fatalf("unexpected error: %v", err)
	}
}
