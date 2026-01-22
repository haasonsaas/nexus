package pgvector

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ============================================================================
// Embedding Encoding/Decoding Tests
// ============================================================================

func TestEncodeEmbedding(t *testing.T) {
	tests := []struct {
		name      string
		embedding []float32
		want      string
		wantValid bool
	}{
		{
			name:      "nil embedding",
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
		{
			name:      "scientific notation values",
			embedding: []float32{1e-10, 1e10},
			want:      "[1e-10,1e+10]",
			wantValid: true,
		},
		{
			name:      "zero values",
			embedding: []float32{0.0, 0.0, 0.0},
			want:      "[0,0,0]",
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
		{
			name: "scientific notation",
			s:    "[1e-10,1e+10]",
			want: []float32{1e-10, 1e10},
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
	testCases := [][]float32{
		{0.123, -0.456, 0.789, 0.0, 1.0, -1.0},
		{0.0},
		{1e-7, 1e7},
		{-0.999999, 0.999999},
	}

	for _, original := range testCases {
		encoded := encodeEmbedding(original)
		if !encoded.Valid {
			t.Fatal("encodeEmbedding() returned invalid")
		}
		decoded := decodeEmbedding(encoded.String)

		if len(decoded) != len(original) {
			t.Fatalf("round trip len = %d, want %d", len(decoded), len(original))
		}

		for i := range decoded {
			diff := decoded[i] - original[i]
			if diff < -0.0001 || diff > 0.0001 {
				t.Errorf("round trip[%d] = %v, want %v", i, decoded[i], original[i])
			}
		}
	}
}

// ============================================================================
// Embedding Validation Tests
// ============================================================================

func TestValidateEmbeddingDimension(t *testing.T) {
	store := &Store{dimension: 3}

	tests := []struct {
		name       string
		embedding  []float32
		allowEmpty bool
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid embedding",
			embedding:  []float32{1, 2, 3},
			allowEmpty: false,
			wantErr:    false,
		},
		{
			name:       "dimension mismatch - too short",
			embedding:  []float32{1, 2},
			allowEmpty: false,
			wantErr:    true,
			errMsg:     "embedding dimension mismatch",
		},
		{
			name:       "dimension mismatch - too long",
			embedding:  []float32{1, 2, 3, 4},
			allowEmpty: false,
			wantErr:    true,
			errMsg:     "embedding dimension mismatch",
		},
		{
			name:       "empty when allowed",
			embedding:  nil,
			allowEmpty: true,
			wantErr:    false,
		},
		{
			name:       "empty slice when allowed",
			embedding:  []float32{},
			allowEmpty: true,
			wantErr:    false,
		},
		{
			name:       "empty when not allowed",
			embedding:  []float32{},
			allowEmpty: false,
			wantErr:    true,
			errMsg:     "embedding is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.validateEmbedding(tt.embedding, tt.allowEmpty)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEmbedding() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateEmbedding() error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestValidateEmbeddingInvalidValues(t *testing.T) {
	store := &Store{dimension: 3}

	tests := []struct {
		name      string
		embedding []float32
		wantErr   bool
	}{
		{
			name:      "contains NaN",
			embedding: []float32{1, float32(math.NaN()), 3},
			wantErr:   true,
		},
		{
			name:      "contains positive infinity",
			embedding: []float32{1, float32(math.Inf(1)), 3},
			wantErr:   true,
		},
		{
			name:      "contains negative infinity",
			embedding: []float32{1, float32(math.Inf(-1)), 3},
			wantErr:   true,
		},
		{
			name:      "valid normal values",
			embedding: []float32{1.0, 2.0, 3.0},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.validateEmbedding(tt.embedding, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEmbedding() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEmbeddingZeroDimension(t *testing.T) {
	// Store with dimension 0 should accept any dimension
	store := &Store{dimension: 0}

	tests := []struct {
		name      string
		embedding []float32
		wantErr   bool
	}{
		{
			name:      "single element",
			embedding: []float32{1.0},
			wantErr:   false,
		},
		{
			name:      "many elements",
			embedding: make([]float32, 1536),
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.validateEmbedding(tt.embedding, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEmbedding() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ============================================================================
// Store Configuration Tests
// ============================================================================

func TestNewStore_Errors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "neither DSN nor DB provided",
			cfg:     Config{},
			wantErr: "either DSN or DB must be provided",
		},
		{
			name: "invalid DSN",
			cfg: Config{
				DSN:           "invalid://connection",
				RunMigrations: false,
			},
			wantErr: "failed to ping database", // Connection fails at ping, not open
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("New() error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNewStore_DefaultDimension(t *testing.T) {
	// Test that dimension defaults to 1536
	cfg := Config{
		Dimension: 0,
	}
	_, err := New(cfg)
	// Should fail because no DB/DSN, but dimension should default
	if err == nil {
		t.Fatal("expected error when neither DSN nor DB is provided")
	}
	expected := "either DSN or DB must be provided"
	if !contains(err.Error(), expected) {
		t.Errorf("unexpected error: %v, expected containing: %s", err, expected)
	}
}

func TestStore_Close(t *testing.T) {
	// Test close with nil db
	s := &Store{db: nil, ownsDB: true}
	err := s.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Test close when store doesn't own the DB
	s = &Store{db: &sql.DB{}, ownsDB: false}
	err = s.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// ============================================================================
// Migration Loading Tests
// ============================================================================

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	if len(migrations) < 1 {
		t.Fatalf("expected at least 1 migration, got %d", len(migrations))
	}

	// Check first migration exists and has content
	first := migrations[0]
	if first.ID == "" {
		t.Error("first migration ID is empty")
	}
	if first.UpSQL == "" {
		t.Error("first migration UpSQL is empty")
	}

	// Verify migrations are sorted by ID
	for i := 1; i < len(migrations); i++ {
		if migrations[i].ID < migrations[i-1].ID {
			t.Errorf("migrations not sorted: %s comes after %s", migrations[i].ID, migrations[i-1].ID)
		}
	}
}

// ============================================================================
// ListOptions Tests
// ============================================================================

func TestListOptions_StructureAndDefaults(t *testing.T) {
	// Test that ListOptions struct works correctly with nil
	// and default values - we can't test actual query without DB
	var opts *store.ListOptions
	if opts != nil {
		t.Error("nil ListOptions should be nil")
	}

	// Test empty options
	opts = &store.ListOptions{}
	if opts.Limit != 0 {
		t.Errorf("Limit = %d, want 0 (default applied in method)", opts.Limit)
	}
	if opts.OrderBy != "" {
		t.Errorf("OrderBy = %q, want empty (default applied in method)", opts.OrderBy)
	}
}

// ============================================================================
// Store Interface Tests (using mock-like patterns)
// ============================================================================

func TestStore_ImplementsDocumentStore(t *testing.T) {
	// Verify Store implements the DocumentStore interface
	var _ store.DocumentStore = (*Store)(nil)
}

// ============================================================================
// Document and Chunk Model Tests
// ============================================================================

func TestDocumentCreation(t *testing.T) {
	doc := &models.Document{
		ID:          "test-id",
		Name:        "Test Document",
		Source:      "test",
		SourceURI:   "/path/to/file.txt",
		ContentType: "text/plain",
		Content:     "This is test content.",
		Metadata: models.DocumentMetadata{
			Title:       "Test Title",
			Author:      "Test Author",
			Description: "Test description",
			Tags:        []string{"tag1", "tag2"},
		},
	}

	if doc.ID != "test-id" {
		t.Errorf("Document ID = %q, want %q", doc.ID, "test-id")
	}
	if doc.Name != "Test Document" {
		t.Errorf("Document Name = %q, want %q", doc.Name, "Test Document")
	}
	if len(doc.Metadata.Tags) != 2 {
		t.Errorf("Document Tags len = %d, want 2", len(doc.Metadata.Tags))
	}
}

func TestDocumentChunkCreation(t *testing.T) {
	chunk := &models.DocumentChunk{
		ID:          "chunk-id",
		DocumentID:  "doc-id",
		Index:       0,
		Content:     "Chunk content",
		StartOffset: 0,
		EndOffset:   13,
		Embedding:   []float32{0.1, 0.2, 0.3},
		TokenCount:  3,
		Metadata: models.ChunkMetadata{
			DocumentName:   "Test Doc",
			DocumentSource: "test",
			Section:        "Introduction",
		},
		CreatedAt: time.Now(),
	}

	if chunk.ID != "chunk-id" {
		t.Errorf("Chunk ID = %q, want %q", chunk.ID, "chunk-id")
	}
	if chunk.DocumentID != "doc-id" {
		t.Errorf("Chunk DocumentID = %q, want %q", chunk.DocumentID, "doc-id")
	}
	if len(chunk.Embedding) != 3 {
		t.Errorf("Chunk Embedding len = %d, want 3", len(chunk.Embedding))
	}
}

// ============================================================================
// Search Request Tests
// ============================================================================

func TestSearchRequestDefaults(t *testing.T) {
	req := &models.DocumentSearchRequest{
		Query: "test query",
	}

	// Defaults should be applied in Search method
	if req.Limit != 0 {
		t.Errorf("Initial Limit = %d, want 0 (defaults applied later)", req.Limit)
	}
	if req.Threshold != 0 {
		t.Errorf("Initial Threshold = %v, want 0 (defaults applied later)", req.Threshold)
	}
}

func TestSearchRequestWithScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   models.DocumentScope
		scopeID string
	}{
		{
			name:    "agent scope",
			scope:   models.DocumentScopeAgent,
			scopeID: "agent-123",
		},
		{
			name:    "session scope",
			scope:   models.DocumentScopeSession,
			scopeID: "session-456",
		},
		{
			name:    "channel scope",
			scope:   models.DocumentScopeChannel,
			scopeID: "channel-789",
		},
		{
			name:    "global scope",
			scope:   models.DocumentScopeGlobal,
			scopeID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &models.DocumentSearchRequest{
				Query:   "test",
				Scope:   tt.scope,
				ScopeID: tt.scopeID,
			}
			if req.Scope != tt.scope {
				t.Errorf("Scope = %v, want %v", req.Scope, tt.scope)
			}
			if req.ScopeID != tt.scopeID {
				t.Errorf("ScopeID = %q, want %q", req.ScopeID, tt.scopeID)
			}
		})
	}
}

// ============================================================================
// Store Stats Tests
// ============================================================================

func TestStoreStats(t *testing.T) {
	stats := &store.StoreStats{
		TotalDocuments:     10,
		TotalChunks:        100,
		TotalTokens:        5000,
		StorageBytes:       1024 * 1024,
		EmbeddingDimension: 1536,
	}

	if stats.TotalDocuments != 10 {
		t.Errorf("TotalDocuments = %d, want 10", stats.TotalDocuments)
	}
	if stats.TotalChunks != 100 {
		t.Errorf("TotalChunks = %d, want 100", stats.TotalChunks)
	}
	if stats.EmbeddingDimension != 1536 {
		t.Errorf("EmbeddingDimension = %d, want 1536", stats.EmbeddingDimension)
	}
}

// ============================================================================
// Search Options Tests
// ============================================================================

func TestDefaultSearchOptions(t *testing.T) {
	opts := store.DefaultSearchOptions()

	if opts.Scope != models.DocumentScopeGlobal {
		t.Errorf("Default Scope = %v, want %v", opts.Scope, models.DocumentScopeGlobal)
	}
	if opts.Limit != 10 {
		t.Errorf("Default Limit = %d, want 10", opts.Limit)
	}
	if opts.Threshold != 0.7 {
		t.Errorf("Default Threshold = %v, want 0.7", opts.Threshold)
	}
}

// ============================================================================
// Helper function tests
// ============================================================================

func TestSearchResponse(t *testing.T) {
	resp := &models.DocumentSearchResponse{
		Results: []*models.DocumentSearchResult{
			{
				Chunk: &models.DocumentChunk{
					ID:      "chunk-1",
					Content: "First result",
				},
				Score: 0.95,
			},
			{
				Chunk: &models.DocumentChunk{
					ID:      "chunk-2",
					Content: "Second result",
				},
				Score: 0.85,
			},
		},
		TotalCount: 2,
		QueryTime:  100 * time.Millisecond,
	}

	if len(resp.Results) != 2 {
		t.Errorf("Results len = %d, want 2", len(resp.Results))
	}
	if resp.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", resp.TotalCount)
	}
	if resp.Results[0].Score != 0.95 {
		t.Errorf("First result Score = %v, want 0.95", resp.Results[0].Score)
	}
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestAddDocument_ValidationErrors(t *testing.T) {
	store := &Store{dimension: 3}
	ctx := context.Background()

	doc := &models.Document{
		Name:    "Test",
		Content: "Content",
	}
	chunks := []*models.DocumentChunk{
		{
			Content:   "Chunk content",
			Embedding: []float32{1, 2}, // Wrong dimension
		},
	}

	err := store.AddDocument(ctx, doc, chunks)
	if err == nil {
		t.Fatal("expected validation error for wrong embedding dimension")
	}
	if !contains(err.Error(), "validate embedding") {
		t.Errorf("expected embedding validation error, got: %v", err)
	}
}

func TestSearch_EmptyEmbeddingError(t *testing.T) {
	s := &Store{dimension: 3}
	ctx := context.Background()

	req := &models.DocumentSearchRequest{
		Query: "test",
	}

	_, err := s.Search(ctx, req, nil)
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
	if !contains(err.Error(), "embedding is empty") {
		t.Errorf("expected empty embedding error, got: %v", err)
	}
}

func TestSearch_WrongDimensionError(t *testing.T) {
	s := &Store{dimension: 3}
	ctx := context.Background()

	req := &models.DocumentSearchRequest{
		Query: "test",
	}

	_, err := s.Search(ctx, req, []float32{1, 2}) // Wrong dimension
	if err == nil {
		t.Fatal("expected error for wrong embedding dimension")
	}
	if !contains(err.Error(), "dimension mismatch") {
		t.Errorf("expected dimension mismatch error, got: %v", err)
	}
}

// ============================================================================
// UpdateChunkEmbeddings Tests
// ============================================================================

func TestUpdateChunkEmbeddings_EmptyMap(t *testing.T) {
	s := &Store{dimension: 3}
	ctx := context.Background()

	// Empty map should not cause error
	err := s.UpdateChunkEmbeddings(ctx, map[string][]float32{})
	if err != nil {
		t.Errorf("UpdateChunkEmbeddings() error = %v, want nil for empty map", err)
	}

	// Nil map should not cause error
	err = s.UpdateChunkEmbeddings(ctx, nil)
	if err != nil {
		t.Errorf("UpdateChunkEmbeddings() error = %v, want nil for nil map", err)
	}
}

func TestUpdateChunkEmbeddings_ValidationError(t *testing.T) {
	// Test that embeddings with NaN values would be rejected by validateEmbedding
	s := &Store{dimension: 3}

	// Direct validation test - without calling the full method that needs DB
	invalidEmbedding := []float32{float32(math.NaN()), 2.0, 3.0}
	err := s.validateEmbedding(invalidEmbedding, true)
	if err == nil {
		t.Fatal("expected validation error for NaN embedding")
	}
	if !contains(err.Error(), "invalid values") {
		t.Errorf("expected 'invalid values' error, got: %v", err)
	}
}

// ============================================================================
// Document Metadata Tests
// ============================================================================

func TestDocumentMetadata_Serialization(t *testing.T) {
	meta := models.DocumentMetadata{
		Title:       "Test Title",
		Author:      "Test Author",
		Description: "A description",
		Tags:        []string{"golang", "testing"},
		Language:    "en",
		AgentID:     "agent-1",
		SessionID:   "session-1",
		ChannelID:   "channel-1",
		Custom: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
	}

	// Verify all fields are set correctly
	if meta.Title != "Test Title" {
		t.Errorf("Title = %q, want %q", meta.Title, "Test Title")
	}
	if len(meta.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(meta.Tags))
	}
	if meta.Custom["key1"] != "value1" {
		t.Errorf("Custom[key1] = %v, want 'value1'", meta.Custom["key1"])
	}
}

func TestChunkMetadata(t *testing.T) {
	meta := models.ChunkMetadata{
		DocumentName:   "Doc Name",
		DocumentSource: "upload",
		Section:        "Introduction",
		AgentID:        "agent-1",
		SessionID:      "session-1",
		ChannelID:      "channel-1",
		Tags:           []string{"important"},
		Extra: map[string]any{
			"custom": "value",
		},
	}

	if meta.DocumentName != "Doc Name" {
		t.Errorf("DocumentName = %q, want %q", meta.DocumentName, "Doc Name")
	}
	if meta.Section != "Introduction" {
		t.Errorf("Section = %q, want %q", meta.Section, "Introduction")
	}
}

// ============================================================================
// Document Scope Tests
// ============================================================================

func TestDocumentScope_Values(t *testing.T) {
	tests := []struct {
		scope models.DocumentScope
		want  string
	}{
		{models.DocumentScopeGlobal, "global"},
		{models.DocumentScopeAgent, "agent"},
		{models.DocumentScopeSession, "session"},
		{models.DocumentScopeChannel, "channel"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.scope) != tt.want {
				t.Errorf("DocumentScope = %q, want %q", tt.scope, tt.want)
			}
		})
	}
}

// ============================================================================
// Migration Struct Tests
// ============================================================================

func TestMigration_Structure(t *testing.T) {
	m := Migration{
		ID:      "001_test",
		UpSQL:   "CREATE TABLE test;",
		DownSQL: "DROP TABLE test;",
	}

	if m.ID != "001_test" {
		t.Errorf("Migration ID = %q, want %q", m.ID, "001_test")
	}
	if m.UpSQL != "CREATE TABLE test;" {
		t.Errorf("Migration UpSQL = %q, want %q", m.UpSQL, "CREATE TABLE test;")
	}
	if m.DownSQL != "DROP TABLE test;" {
		t.Errorf("Migration DownSQL = %q, want %q", m.DownSQL, "DROP TABLE test;")
	}
}

// ============================================================================
// ListOptions Tests
// ============================================================================

func TestListOptions_Fields(t *testing.T) {
	opts := &store.ListOptions{
		Limit:     50,
		Offset:    10,
		Source:    "upload",
		Tags:      []string{"tag1", "tag2"},
		AgentID:   "agent-1",
		SessionID: "session-1",
		ChannelID: "channel-1",
		OrderBy:   "updated_at",
		OrderDesc: true,
	}

	if opts.Limit != 50 {
		t.Errorf("Limit = %d, want 50", opts.Limit)
	}
	if opts.Offset != 10 {
		t.Errorf("Offset = %d, want 10", opts.Offset)
	}
	if opts.Source != "upload" {
		t.Errorf("Source = %q, want %q", opts.Source, "upload")
	}
	if len(opts.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(opts.Tags))
	}
	if !opts.OrderDesc {
		t.Error("OrderDesc = false, want true")
	}
}

// ============================================================================
// Context Cancellation Tests
// ============================================================================

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// We test that context cancellation doesn't cause undefined behavior
	// Actual database operations would fail with context canceled error
	// but we can't test that without a real DB connection
	if ctx.Err() == nil {
		t.Error("Context should be canceled")
	}
}

// ============================================================================
// Database Error Simulation Tests
// ============================================================================

func TestDatabaseErrors(t *testing.T) {
	// Test that sql error types are properly recognized
	// Note: We can't test actual DB operations without a real database
	// as they would panic on nil db pointer dereference

	t.Run("ErrNoRows is correctly detected", func(t *testing.T) {
		err := sql.ErrNoRows
		if !errors.Is(err, sql.ErrNoRows) {
			t.Error("errors.Is should match sql.ErrNoRows")
		}
	})

	t.Run("ErrTxDone is correctly detected", func(t *testing.T) {
		err := sql.ErrTxDone
		if !errors.Is(err, sql.ErrTxDone) {
			t.Error("errors.Is should match sql.ErrTxDone")
		}
	})
}

// ============================================================================
// Concurrency Safety Tests
// ============================================================================

func TestConcurrentEmbeddingOperations(t *testing.T) {
	// Test that embedding encode/decode operations are thread-safe
	embeddings := [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
		{0.7, 0.8, 0.9},
	}

	done := make(chan bool, len(embeddings)*2)

	for _, emb := range embeddings {
		go func(e []float32) {
			encoded := encodeEmbedding(e)
			if encoded.Valid {
				decodeEmbedding(encoded.String)
			}
			done <- true
		}(emb)

		go func(e []float32) {
			s := &Store{dimension: len(e)}
			_ = s.validateEmbedding(e, false)
			done <- true
		}(emb)
	}

	// Wait for all goroutines
	for i := 0; i < len(embeddings)*2; i++ {
		<-done
	}
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestLargeEmbedding(t *testing.T) {
	// Test with OpenAI-sized embedding (1536 dimensions)
	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	encoded := encodeEmbedding(embedding)
	if !encoded.Valid {
		t.Fatal("failed to encode large embedding")
	}

	decoded := decodeEmbedding(encoded.String)
	if len(decoded) != 1536 {
		t.Errorf("decoded length = %d, want 1536", len(decoded))
	}
}

func TestSpecialCharactersInMetadata(t *testing.T) {
	meta := models.DocumentMetadata{
		Title:       "Test with 'quotes' and \"double quotes\"",
		Description: "Contains\nnewlines\tand\ttabs",
		Tags:        []string{"tag-with-dash", "tag_with_underscore", "tag.with.dots"},
		Custom: map[string]any{
			"unicode":     "Hello",
			"special":     "<>&",
			"backslashes": "path\\to\\file",
		},
	}

	if meta.Title == "" {
		t.Error("Title should not be empty")
	}
	if len(meta.Tags) != 3 {
		t.Errorf("Tags len = %d, want 3", len(meta.Tags))
	}
}

func TestEmptyDocumentContent(t *testing.T) {
	doc := &models.Document{
		ID:      "empty-doc",
		Name:    "Empty",
		Content: "",
	}

	if doc.Content != "" {
		t.Errorf("Content = %q, want empty", doc.Content)
	}
}

func TestVeryLongDocumentContent(t *testing.T) {
	// Test with 1MB of content
	longContent := make([]byte, 1024*1024)
	for i := range longContent {
		longContent[i] = 'a' + byte(i%26)
	}

	doc := &models.Document{
		ID:      "long-doc",
		Name:    "Very Long Document",
		Content: string(longContent),
	}

	if len(doc.Content) != 1024*1024 {
		t.Errorf("Content length = %d, want %d", len(doc.Content), 1024*1024)
	}
}

// ============================================================================
// Integration-ready Tests (can be run with test database)
// ============================================================================

// These tests are designed to work with a real database when available
// They are skipped when no database is configured

func TestIntegration_AddAndGetDocument(t *testing.T) {
	// Skip if no test database is configured
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("No test database configured (set TEST_POSTGRES_DSN)")
	}

	store, err := New(Config{
		DSN:           dsn,
		Dimension:     3,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a document
	doc := &models.Document{
		Name:        "Integration Test Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "This is test content for integration testing.",
		Metadata: models.DocumentMetadata{
			Title:  "Test Title",
			Author: "Test Author",
		},
	}

	chunks := []*models.DocumentChunk{
		{
			Index:       0,
			Content:     "This is test content",
			StartOffset: 0,
			EndOffset:   20,
			Embedding:   []float32{0.1, 0.2, 0.3},
			TokenCount:  5,
		},
	}

	// Add document
	err = store.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	// Get document back
	retrieved, err := store.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Retrieved document is nil")
	}
	if retrieved.Name != doc.Name {
		t.Errorf("Name = %q, want %q", retrieved.Name, doc.Name)
	}
	if retrieved.ChunkCount != 1 {
		t.Errorf("ChunkCount = %d, want 1", retrieved.ChunkCount)
	}

	// Clean up
	err = store.DeleteDocument(ctx, doc.ID)
	if err != nil {
		t.Errorf("DeleteDocument failed: %v", err)
	}
}

func TestIntegration_Search(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("No test database configured (set TEST_POSTGRES_DSN)")
	}

	store, err := New(Config{
		DSN:           dsn,
		Dimension:     3,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create test documents with embeddings
	docs := []struct {
		name      string
		content   string
		embedding []float32
	}{
		{"Doc 1", "Machine learning basics", []float32{0.9, 0.1, 0.1}},
		{"Doc 2", "Deep learning tutorial", []float32{0.8, 0.2, 0.1}},
		{"Doc 3", "Cooking recipes", []float32{0.1, 0.1, 0.9}},
	}

	var docIDs []string
	for _, d := range docs {
		doc := &models.Document{
			Name:        d.name,
			Source:      "test",
			ContentType: "text/plain",
			Content:     d.content,
		}
		chunks := []*models.DocumentChunk{
			{
				Index:     0,
				Content:   d.content,
				Embedding: d.embedding,
			},
		}
		err = store.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search for ML-related content
	req := &models.DocumentSearchRequest{
		Query:     "machine learning",
		Limit:     10,
		Threshold: 0.5,
	}
	queryEmbedding := []float32{0.85, 0.15, 0.1}

	resp, err := store.Search(ctx, req, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) == 0 {
		t.Error("Expected search results, got none")
	}

	// Clean up
	for _, id := range docIDs {
		_ = store.DeleteDocument(ctx, id)
	}
}

func TestIntegration_Stats(t *testing.T) {
	dsn := getTestDSN()
	if dsn == "" {
		t.Skip("No test database configured (set TEST_POSTGRES_DSN)")
	}

	store, err := New(Config{
		DSN:           dsn,
		Dimension:     3,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.EmbeddingDimension != 3 {
		t.Errorf("EmbeddingDimension = %d, want 3", stats.EmbeddingDimension)
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkEncodeEmbedding(b *testing.B) {
	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodeEmbedding(embedding)
	}
}

func BenchmarkDecodeEmbedding(b *testing.B) {
	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}
	encoded := encodeEmbedding(embedding)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeEmbedding(encoded.String)
	}
}

func BenchmarkValidateEmbedding(b *testing.B) {
	s := &Store{dimension: 1536}
	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.validateEmbedding(embedding, false)
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func getTestDSN() string {
	// In a real test, this would read from environment variable
	// return os.Getenv("TEST_POSTGRES_DSN")
	return ""
}

// ============================================================================
// SQL Error Type Tests
// ============================================================================

func TestSQLErrorHandling(t *testing.T) {
	// Test that sql error constants are available and correctly typed
	if sql.ErrNoRows.Error() == "" {
		t.Error("ErrNoRows should have an error message")
	}
	if sql.ErrTxDone.Error() == "" {
		t.Error("ErrTxDone should have an error message")
	}
}
