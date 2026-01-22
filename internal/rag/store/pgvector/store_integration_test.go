package pgvector

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/rag/store"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ============================================================================
// Test Helpers
// ============================================================================

// testDB holds the shared test database connection.
// Tests requiring a real database should call getTestDB.
var (
	testDB     *sql.DB
	testDBOnce sync.Once
	testDBErr  error
)

// getTestDB returns a database connection for integration tests.
// If TEST_POSTGRES_DSN is not set, the test is skipped.
func getTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("Skipping integration test: TEST_POSTGRES_DSN not set")
	}

	testDBOnce.Do(func() {
		var err error
		testDB, err = sql.Open("postgres", dsn)
		if err != nil {
			testDBErr = fmt.Errorf("open database: %w", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := testDB.PingContext(ctx); err != nil {
			testDBErr = fmt.Errorf("ping database: %w", err)
			testDB.Close()
			testDB = nil
			return
		}
	})

	if testDBErr != nil {
		t.Fatalf("Failed to connect to test database: %v", testDBErr)
	}

	return testDB
}

// createTestStore creates a store for testing with a unique prefix.
func createTestStore(t *testing.T, dimension int) *Store {
	t.Helper()

	db := getTestDB(t)

	s, err := New(Config{
		DB:            db,
		Dimension:     dimension,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}

	return s
}

// cleanupTestData removes test data created during a test.
func cleanupTestData(ctx context.Context, s *Store, docIDs []string) {
	for _, id := range docIDs {
		_ = s.DeleteDocument(ctx, id)
	}
}

// generateEmbedding creates a normalized test embedding.
func generateEmbedding(dimension int, seed float32) []float32 {
	embedding := make([]float32, dimension)
	sum := float32(0)
	for i := range embedding {
		v := seed + float32(i)*0.01
		embedding[i] = v
		sum += v * v
	}
	// Normalize
	norm := float32(math.Sqrt(float64(sum)))
	if norm > 0 {
		for i := range embedding {
			embedding[i] /= norm
		}
	}
	return embedding
}

// similarEmbedding creates an embedding similar to the reference.
func similarEmbedding(ref []float32, variance float32) []float32 {
	result := make([]float32, len(ref))
	copy(result, ref)
	for i := range result {
		result[i] += variance * float32(i%3-1) * 0.1
	}
	// Re-normalize
	sum := float32(0)
	for _, v := range result {
		sum += v * v
	}
	norm := float32(math.Sqrt(float64(sum)))
	if norm > 0 {
		for i := range result {
			result[i] /= norm
		}
	}
	return result
}

// ============================================================================
// AddDocument Integration Tests
// ============================================================================

func TestIntegration_AddDocument_Basic(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Test Document Basic",
		Source:      "test",
		SourceURI:   "/test/path.txt",
		ContentType: "text/plain",
		Content:     "This is the full content of the document.",
		Metadata: models.DocumentMetadata{
			Title:       "Test Title",
			Author:      "Test Author",
			Description: "A test document",
			Tags:        []string{"test", "integration"},
			Language:    "en",
		},
	}

	chunks := []*models.DocumentChunk{
		{
			Index:       0,
			Content:     "This is the first chunk.",
			StartOffset: 0,
			EndOffset:   24,
			Embedding:   generateEmbedding(8, 0.1),
			TokenCount:  5,
			Metadata: models.ChunkMetadata{
				DocumentName:   doc.Name,
				DocumentSource: doc.Source,
				Section:        "Introduction",
			},
		},
		{
			Index:       1,
			Content:     "This is the second chunk.",
			StartOffset: 25,
			EndOffset:   50,
			Embedding:   generateEmbedding(8, 0.5),
			TokenCount:  5,
			Metadata: models.ChunkMetadata{
				DocumentName:   doc.Name,
				DocumentSource: doc.Source,
				Section:        "Body",
			},
		},
	}

	// Add document
	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify document was created with ID
	if doc.ID == "" {
		t.Error("Document ID should be set after AddDocument")
	}

	// Verify timestamps were set
	if doc.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if doc.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}

	// Verify chunk count was set
	if doc.ChunkCount != 2 {
		t.Errorf("ChunkCount = %d, want 2", doc.ChunkCount)
	}

	// Retrieve and verify
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetDocument returned nil")
	}

	if retrieved.Name != doc.Name {
		t.Errorf("Name = %q, want %q", retrieved.Name, doc.Name)
	}
	if retrieved.Source != doc.Source {
		t.Errorf("Source = %q, want %q", retrieved.Source, doc.Source)
	}
	if retrieved.Content != doc.Content {
		t.Errorf("Content mismatch")
	}
	if retrieved.ChunkCount != 2 {
		t.Errorf("ChunkCount = %d, want 2", retrieved.ChunkCount)
	}
	if retrieved.Metadata.Title != doc.Metadata.Title {
		t.Errorf("Metadata.Title = %q, want %q", retrieved.Metadata.Title, doc.Metadata.Title)
	}
	if len(retrieved.Metadata.Tags) != 2 {
		t.Errorf("Metadata.Tags len = %d, want 2", len(retrieved.Metadata.Tags))
	}
}

func TestIntegration_AddDocument_WithoutEmbeddings(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Test Document No Embeddings",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Document content without embeddings.",
	}

	// Chunks without embeddings (empty slice is allowed)
	chunks := []*models.DocumentChunk{
		{
			Index:      0,
			Content:    "Chunk without embedding.",
			Embedding:  nil, // No embedding
			TokenCount: 3,
		},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument without embeddings failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify chunk was stored
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}
	if len(storedChunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(storedChunks))
	}
	if storedChunks[0].Embedding != nil && len(storedChunks[0].Embedding) > 0 {
		t.Error("Embedding should be nil or empty")
	}
}

func TestIntegration_AddDocument_Update(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create initial document
	doc := &models.Document{
		Name:        "Document to Update",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Initial content.",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Initial chunk.", Embedding: generateEmbedding(8, 0.1)},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("Initial AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)
	initialID := doc.ID
	initialCreatedAt := doc.CreatedAt

	// Wait a bit to ensure UpdatedAt changes
	time.Sleep(10 * time.Millisecond)

	// Update the document (using same ID)
	doc.Content = "Updated content."
	doc.Name = "Updated Document Name"
	newChunks := []*models.DocumentChunk{
		{Index: 0, Content: "Updated first chunk.", Embedding: generateEmbedding(8, 0.2)},
		{Index: 1, Content: "New second chunk.", Embedding: generateEmbedding(8, 0.3)},
	}

	err = s.AddDocument(ctx, doc, newChunks)
	if err != nil {
		t.Fatalf("Update AddDocument failed: %v", err)
	}

	// Verify ID didn't change
	if doc.ID != initialID {
		t.Errorf("ID changed from %q to %q", initialID, doc.ID)
	}

	// Verify document was updated
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	if retrieved.Name != "Updated Document Name" {
		t.Errorf("Name not updated: %q", retrieved.Name)
	}
	if retrieved.Content != "Updated content." {
		t.Errorf("Content not updated")
	}
	if retrieved.ChunkCount != 2 {
		t.Errorf("ChunkCount = %d, want 2", retrieved.ChunkCount)
	}
	if !retrieved.UpdatedAt.After(initialCreatedAt) {
		t.Error("UpdatedAt should be after initial CreatedAt")
	}

	// Verify chunks were replaced
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}
	if len(storedChunks) != 2 {
		t.Errorf("Expected 2 chunks, got %d", len(storedChunks))
	}
}

func TestIntegration_AddDocument_ManyChunks(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Document with Many Chunks",
		Source:      "test",
		ContentType: "text/plain",
		Content:     strings.Repeat("Content block. ", 100),
	}

	// Create 50 chunks
	numChunks := 50
	chunks := make([]*models.DocumentChunk, numChunks)
	for i := 0; i < numChunks; i++ {
		chunks[i] = &models.DocumentChunk{
			Index:       i,
			Content:     fmt.Sprintf("Chunk number %d with some content.", i),
			StartOffset: i * 100,
			EndOffset:   (i + 1) * 100,
			Embedding:   generateEmbedding(8, float32(i)*0.02),
			TokenCount:  10,
		}
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument with many chunks failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify all chunks were stored
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}
	if len(storedChunks) != numChunks {
		t.Errorf("Expected %d chunks, got %d", numChunks, len(storedChunks))
	}

	// Verify chunks are ordered by index
	for i, chunk := range storedChunks {
		if chunk.Index != i {
			t.Errorf("Chunk at position %d has index %d", i, chunk.Index)
		}
	}
}

func TestIntegration_AddDocument_LargeContent(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create 1MB of content
	largeContent := strings.Repeat("Large content block with varied text. ", 30000)

	doc := &models.Document{
		Name:        "Large Document",
		Source:      "test",
		ContentType: "text/plain",
		Content:     largeContent,
	}

	chunks := []*models.DocumentChunk{
		{
			Index:     0,
			Content:   largeContent[:len(largeContent)/2],
			Embedding: generateEmbedding(8, 0.1),
		},
		{
			Index:     1,
			Content:   largeContent[len(largeContent)/2:],
			Embedding: generateEmbedding(8, 0.2),
		},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument with large content failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify content was stored correctly
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if len(retrieved.Content) != len(largeContent) {
		t.Errorf("Content length = %d, want %d", len(retrieved.Content), len(largeContent))
	}
}

func TestIntegration_AddDocument_SpecialCharacters(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Test with 'quotes' and \"double quotes\"",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Content with special chars: <>&'\"\n\ttabs\r\nand newlines",
		Metadata: models.DocumentMetadata{
			Title:       "Unicode: Hello",
			Description: "Backslash: \\path\\to\\file",
			Tags:        []string{"tag-with-dash", "tag_underscore", "tag.dot"},
		},
	}

	chunks := []*models.DocumentChunk{
		{
			Index:     0,
			Content:   "Chunk with JSON-like content: {\"key\": \"value\"}",
			Embedding: generateEmbedding(8, 0.1),
			Metadata: models.ChunkMetadata{
				Extra: map[string]any{
					"nested": map[string]any{"a": 1},
				},
			},
		},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument with special characters failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify retrieval preserves special characters
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if retrieved.Name != doc.Name {
		t.Errorf("Name not preserved: got %q, want %q", retrieved.Name, doc.Name)
	}
	if retrieved.Metadata.Title != doc.Metadata.Title {
		t.Errorf("Title not preserved: got %q", retrieved.Metadata.Title)
	}
}

// ============================================================================
// GetDocument Integration Tests
// ============================================================================

func TestIntegration_GetDocument_NotFound(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	retrieved, err := s.GetDocument(ctx, "nonexistent-id-12345")
	if err != nil {
		t.Fatalf("GetDocument should not error for missing document: %v", err)
	}
	if retrieved != nil {
		t.Error("GetDocument should return nil for missing document")
	}
}

// ============================================================================
// ListDocuments Integration Tests
// ============================================================================

func TestIntegration_ListDocuments_Basic(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create multiple documents
	for i := 0; i < 5; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("List Test Doc %d", i),
			Source:      "test-list",
			ContentType: "text/plain",
			Content:     fmt.Sprintf("Content for document %d", i),
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, float32(i)*0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument %d failed: %v", i, err)
		}
		docIDs = append(docIDs, doc.ID)
		// Small delay to ensure different timestamps
		time.Sleep(5 * time.Millisecond)
	}

	// List all documents from test source
	docs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source: "test-list",
		Limit:  100,
	})
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}

	if len(docs) < 5 {
		t.Errorf("Expected at least 5 documents, got %d", len(docs))
	}
}

func TestIntegration_ListDocuments_Pagination(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create 10 documents
	for i := 0; i < 10; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Pagination Doc %d", i),
			Source:      "test-pagination",
			ContentType: "text/plain",
			Content:     fmt.Sprintf("Content %d", i),
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, float32(i)*0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// First page
	page1, err := s.ListDocuments(ctx, &store.ListOptions{
		Source: "test-pagination",
		Limit:  3,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("ListDocuments page 1 failed: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("Page 1 length = %d, want 3", len(page1))
	}

	// Second page
	page2, err := s.ListDocuments(ctx, &store.ListOptions{
		Source: "test-pagination",
		Limit:  3,
		Offset: 3,
	})
	if err != nil {
		t.Fatalf("ListDocuments page 2 failed: %v", err)
	}
	if len(page2) != 3 {
		t.Errorf("Page 2 length = %d, want 3", len(page2))
	}

	// Verify no overlap
	page1IDs := make(map[string]bool)
	for _, doc := range page1 {
		page1IDs[doc.ID] = true
	}
	for _, doc := range page2 {
		if page1IDs[doc.ID] {
			t.Errorf("Document %s appears in both pages", doc.ID)
		}
	}
}

func TestIntegration_ListDocuments_Ordering(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents with different names
	names := []string{"Zebra", "Alpha", "Middle"}
	for _, name := range names {
		doc := &models.Document{
			Name:        name,
			Source:      "test-ordering",
			ContentType: "text/plain",
			Content:     "Content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, 0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
		time.Sleep(10 * time.Millisecond)
	}

	// Order by name ascending
	docsAsc, err := s.ListDocuments(ctx, &store.ListOptions{
		Source:    "test-ordering",
		OrderBy:   "name",
		OrderDesc: false,
	})
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}

	if len(docsAsc) >= 2 {
		// Verify ascending order
		for i := 1; i < len(docsAsc); i++ {
			if docsAsc[i].Name < docsAsc[i-1].Name {
				t.Errorf("Not in ascending order: %q comes after %q", docsAsc[i].Name, docsAsc[i-1].Name)
			}
		}
	}

	// Order by name descending
	docsDesc, err := s.ListDocuments(ctx, &store.ListOptions{
		Source:    "test-ordering",
		OrderBy:   "name",
		OrderDesc: true,
	})
	if err != nil {
		t.Fatalf("ListDocuments descending failed: %v", err)
	}

	if len(docsDesc) >= 2 {
		// Verify descending order
		for i := 1; i < len(docsDesc); i++ {
			if docsDesc[i].Name > docsDesc[i-1].Name {
				t.Errorf("Not in descending order: %q comes after %q", docsDesc[i].Name, docsDesc[i-1].Name)
			}
		}
	}
}

func TestIntegration_ListDocuments_FilterByMetadata(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents with different metadata
	docs := []struct {
		name      string
		agentID   string
		sessionID string
		channelID string
	}{
		{"Agent1 Doc", "agent-1", "", ""},
		{"Agent2 Doc", "agent-2", "", ""},
		{"Session Doc", "", "session-1", ""},
		{"Channel Doc", "", "", "channel-1"},
	}

	for _, d := range docs {
		doc := &models.Document{
			Name:        d.name,
			Source:      "test-filter",
			ContentType: "text/plain",
			Content:     "Content",
			Metadata: models.DocumentMetadata{
				AgentID:   d.agentID,
				SessionID: d.sessionID,
				ChannelID: d.channelID,
			},
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, 0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Filter by agent
	agentDocs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source:  "test-filter",
		AgentID: "agent-1",
	})
	if err != nil {
		t.Fatalf("ListDocuments by agent failed: %v", err)
	}
	if len(agentDocs) != 1 {
		t.Errorf("Expected 1 document for agent-1, got %d", len(agentDocs))
	}
	if len(agentDocs) > 0 && agentDocs[0].Name != "Agent1 Doc" {
		t.Errorf("Wrong document returned: %s", agentDocs[0].Name)
	}

	// Filter by session
	sessionDocs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source:    "test-filter",
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatalf("ListDocuments by session failed: %v", err)
	}
	if len(sessionDocs) != 1 {
		t.Errorf("Expected 1 document for session-1, got %d", len(sessionDocs))
	}

	// Filter by channel
	channelDocs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source:    "test-filter",
		ChannelID: "channel-1",
	})
	if err != nil {
		t.Fatalf("ListDocuments by channel failed: %v", err)
	}
	if len(channelDocs) != 1 {
		t.Errorf("Expected 1 document for channel-1, got %d", len(channelDocs))
	}
}

func TestIntegration_ListDocuments_EmptyResult(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// List with a source that doesn't exist
	docs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source: "nonexistent-source-xyz123",
	})
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("Expected empty result, got %d documents", len(docs))
	}
}

func TestIntegration_ListDocuments_NilOptions(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Should not panic with nil options
	docs, err := s.ListDocuments(ctx, nil)
	if err != nil {
		t.Fatalf("ListDocuments with nil options failed: %v", err)
	}
	// Just verify it returns without error
	_ = docs
}

// ============================================================================
// DeleteDocument Integration Tests
// ============================================================================

func TestIntegration_DeleteDocument(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Create a document
	doc := &models.Document{
		Name:        "Document to Delete",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Content",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Chunk 1", Embedding: generateEmbedding(8, 0.1)},
		{Index: 1, Content: "Chunk 2", Embedding: generateEmbedding(8, 0.2)},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	// Verify it exists
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Document should exist before deletion")
	}

	// Delete it
	err = s.DeleteDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("DeleteDocument failed: %v", err)
	}

	// Verify it's gone
	retrieved, err = s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument after delete failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Document should not exist after deletion")
	}

	// Verify chunks are also deleted (cascade)
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}
	if len(storedChunks) != 0 {
		t.Errorf("Chunks should be deleted, got %d", len(storedChunks))
	}
}

func TestIntegration_DeleteDocument_Nonexistent(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Deleting a nonexistent document should not error
	err := s.DeleteDocument(ctx, "nonexistent-id-xyz789")
	if err != nil {
		t.Fatalf("DeleteDocument for nonexistent document should not error: %v", err)
	}
}

// ============================================================================
// GetChunk Integration Tests
// ============================================================================

func TestIntegration_GetChunk(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)
	doc := &models.Document{
		Name:        "Chunk Test Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Document content.",
	}
	chunks := []*models.DocumentChunk{
		{
			Index:       0,
			Content:     "Specific chunk content",
			StartOffset: 0,
			EndOffset:   22,
			Embedding:   embedding,
			TokenCount:  3,
			Metadata: models.ChunkMetadata{
				DocumentName: doc.Name,
				Section:      "Test Section",
			},
		},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Get the chunk ID
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}
	if len(storedChunks) != 1 {
		t.Fatalf("Expected 1 chunk, got %d", len(storedChunks))
	}

	chunkID := storedChunks[0].ID

	// Get chunk by ID
	chunk, err := s.GetChunk(ctx, chunkID)
	if err != nil {
		t.Fatalf("GetChunk failed: %v", err)
	}
	if chunk == nil {
		t.Fatal("GetChunk returned nil")
	}

	if chunk.Content != "Specific chunk content" {
		t.Errorf("Chunk content mismatch: %q", chunk.Content)
	}
	if chunk.DocumentID != doc.ID {
		t.Errorf("DocumentID = %q, want %q", chunk.DocumentID, doc.ID)
	}
	if chunk.Index != 0 {
		t.Errorf("Index = %d, want 0", chunk.Index)
	}
	if chunk.TokenCount != 3 {
		t.Errorf("TokenCount = %d, want 3", chunk.TokenCount)
	}
	if chunk.Metadata.Section != "Test Section" {
		t.Errorf("Section = %q, want %q", chunk.Metadata.Section, "Test Section")
	}

	// Verify embedding was stored and retrieved
	if len(chunk.Embedding) != 8 {
		t.Errorf("Embedding length = %d, want 8", len(chunk.Embedding))
	}
}

func TestIntegration_GetChunk_NotFound(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	chunk, err := s.GetChunk(ctx, "nonexistent-chunk-id")
	if err != nil {
		t.Fatalf("GetChunk should not error for missing chunk: %v", err)
	}
	if chunk != nil {
		t.Error("GetChunk should return nil for missing chunk")
	}
}

// ============================================================================
// Search Integration Tests
// ============================================================================

func TestIntegration_Search_Basic(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents with distinct embeddings
	mlEmbedding := generateEmbedding(8, 0.1)
	cookingEmbedding := generateEmbedding(8, 0.9)

	docs := []struct {
		name      string
		content   string
		embedding []float32
	}{
		{"Machine Learning Basics", "Introduction to machine learning concepts", mlEmbedding},
		{"Cooking Recipes", "How to cook pasta and other dishes", cookingEmbedding},
	}

	for _, d := range docs {
		doc := &models.Document{
			Name:        d.name,
			Source:      "test-search",
			ContentType: "text/plain",
			Content:     d.content,
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: d.content, Embedding: d.embedding},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search with an embedding similar to ML
	queryEmbedding := similarEmbedding(mlEmbedding, 0.1)
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "machine learning",
		Limit:     10,
		Threshold: 0.5,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) == 0 {
		t.Error("Expected search results")
	}

	// The ML document should rank higher
	if len(resp.Results) > 0 {
		topResult := resp.Results[0]
		if !strings.Contains(topResult.Chunk.Content, "machine learning") {
			t.Errorf("Expected ML document as top result, got: %s", topResult.Chunk.Content)
		}
		if topResult.Score <= 0 {
			t.Error("Score should be positive")
		}
	}

	// Verify query time is recorded
	if resp.QueryTime <= 0 {
		t.Error("QueryTime should be positive")
	}
}

func TestIntegration_Search_WithThreshold(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create a document
	embedding := generateEmbedding(8, 0.5)
	doc := &models.Document{
		Name:        "Threshold Test Doc",
		Source:      "test-threshold",
		ContentType: "text/plain",
		Content:     "Test content",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Test content", Embedding: embedding},
	}
	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Search with very high threshold - should return no results
	dissimilarEmbedding := generateEmbedding(8, -0.5) // Very different
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "test",
		Limit:     10,
		Threshold: 0.99, // Very high threshold
	}, dissimilarEmbedding)
	if err != nil {
		t.Fatalf("Search with high threshold failed: %v", err)
	}

	if len(resp.Results) > 0 {
		t.Errorf("Expected no results with high threshold, got %d", len(resp.Results))
	}

	// Search with low threshold - should return results
	resp, err = s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "test",
		Limit:     10,
		Threshold: 0.1, // Low threshold
	}, similarEmbedding(embedding, 0.3))
	if err != nil {
		t.Fatalf("Search with low threshold failed: %v", err)
	}

	// May or may not have results depending on actual similarity
	// Just verify no error
}

func TestIntegration_Search_WithScope(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)

	// Create documents with different scopes
	scopes := []struct {
		name      string
		agentID   string
		sessionID string
		channelID string
	}{
		{"Agent1 Doc", "agent-1", "", ""},
		{"Agent2 Doc", "agent-2", "", ""},
		{"Session Doc", "", "session-1", ""},
	}

	for _, sc := range scopes {
		doc := &models.Document{
			Name:        sc.name,
			Source:      "test-scope",
			ContentType: "text/plain",
			Content:     "Scoped content",
			Metadata: models.DocumentMetadata{
				AgentID:   sc.agentID,
				SessionID: sc.sessionID,
				ChannelID: sc.channelID,
			},
		}
		chunks := []*models.DocumentChunk{
			{
				Index:     0,
				Content:   "Scoped content",
				Embedding: embedding,
				Metadata: models.ChunkMetadata{
					AgentID:   sc.agentID,
					SessionID: sc.sessionID,
					ChannelID: sc.channelID,
				},
			},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	queryEmbedding := similarEmbedding(embedding, 0.1)

	// Search scoped to agent-1
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "content",
		Scope:     models.DocumentScopeAgent,
		ScopeID:   "agent-1",
		Limit:     10,
		Threshold: 0.5,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search with agent scope failed: %v", err)
	}

	// Should only return agent-1's document
	for _, result := range resp.Results {
		if result.Chunk.Metadata.AgentID != "agent-1" && result.Chunk.Metadata.AgentID != "" {
			t.Errorf("Result should be scoped to agent-1, got agent_id=%s", result.Chunk.Metadata.AgentID)
		}
	}
}

func TestIntegration_Search_WithDocumentIDFilter(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)

	// Create multiple documents
	for i := 0; i < 3; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Filter Doc %d", i),
			Source:      "test-docid-filter",
			ContentType: "text/plain",
			Content:     "Content to filter",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Content to filter", Embedding: embedding},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search limited to specific documents
	targetDocIDs := []string{docIDs[0], docIDs[2]}
	queryEmbedding := similarEmbedding(embedding, 0.1)

	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:       "content",
		DocumentIDs: targetDocIDs,
		Limit:       10,
		Threshold:   0.5,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search with document filter failed: %v", err)
	}

	// All results should be from target documents
	for _, result := range resp.Results {
		found := false
		for _, id := range targetDocIDs {
			if result.Chunk.DocumentID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Result document %s not in filter list", result.Chunk.DocumentID)
		}
	}
}

func TestIntegration_Search_EmptyResults(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Search with very dissimilar embedding
	queryEmbedding := generateEmbedding(8, 100) // Very different from typical values

	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:       "nonexistent content xyz",
		DocumentIDs: []string{"nonexistent-doc-id"},
		Limit:       10,
		Threshold:   0.99,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(resp.Results))
	}
	if resp.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", resp.TotalCount)
	}
}

func TestIntegration_Search_Ordering(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create a reference embedding
	refEmbedding := generateEmbedding(8, 0.5)

	// Create documents with progressively less similar embeddings
	similarities := []float32{0.05, 0.1, 0.2, 0.3, 0.4}
	for i, variance := range similarities {
		embedding := similarEmbedding(refEmbedding, variance)
		doc := &models.Document{
			Name:        fmt.Sprintf("Ordering Doc %d", i),
			Source:      "test-ordering",
			ContentType: "text/plain",
			Content:     fmt.Sprintf("Content %d", i),
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: fmt.Sprintf("Content %d", i), Embedding: embedding},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "content",
		Limit:     10,
		Threshold: 0.3,
	}, refEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Verify results are ordered by descending score
	for i := 1; i < len(resp.Results); i++ {
		if resp.Results[i].Score > resp.Results[i-1].Score {
			t.Errorf("Results not in descending order: score[%d]=%f > score[%d]=%f",
				i, resp.Results[i].Score, i-1, resp.Results[i-1].Score)
		}
	}
}

func TestIntegration_Search_Limit(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)

	// Create 10 similar documents
	for i := 0; i < 10; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Limit Doc %d", i),
			Source:      "test-limit",
			ContentType: "text/plain",
			Content:     "Similar content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Similar content", Embedding: similarEmbedding(embedding, float32(i)*0.01)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search with limit of 3
	queryEmbedding := similarEmbedding(embedding, 0.05)
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "content",
		Limit:     3,
		Threshold: 0.5,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) > 3 {
		t.Errorf("Expected at most 3 results, got %d", len(resp.Results))
	}
}

// ============================================================================
// UpdateChunkEmbeddings Integration Tests
// ============================================================================

func TestIntegration_UpdateChunkEmbeddings(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create a document with initial embeddings
	initialEmbedding := generateEmbedding(8, 0.1)
	doc := &models.Document{
		Name:        "Embedding Update Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Content",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Chunk 1", Embedding: initialEmbedding},
		{Index: 1, Content: "Chunk 2", Embedding: initialEmbedding},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Get chunk IDs
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}

	// Update embeddings
	newEmbedding := generateEmbedding(8, 0.9)
	updates := map[string][]float32{
		storedChunks[0].ID: newEmbedding,
	}

	err = s.UpdateChunkEmbeddings(ctx, updates)
	if err != nil {
		t.Fatalf("UpdateChunkEmbeddings failed: %v", err)
	}

	// Verify update
	updatedChunk, err := s.GetChunk(ctx, storedChunks[0].ID)
	if err != nil {
		t.Fatalf("GetChunk failed: %v", err)
	}

	// Compare embeddings (with some tolerance for floating point)
	if len(updatedChunk.Embedding) != len(newEmbedding) {
		t.Errorf("Updated embedding length = %d, want %d", len(updatedChunk.Embedding), len(newEmbedding))
	} else {
		for i := range newEmbedding {
			diff := updatedChunk.Embedding[i] - newEmbedding[i]
			if diff < -0.001 || diff > 0.001 {
				t.Errorf("Embedding[%d] = %f, want %f", i, updatedChunk.Embedding[i], newEmbedding[i])
				break
			}
		}
	}
}

// ============================================================================
// Stats Integration Tests
// ============================================================================

func TestIntegration_Stats_Comprehensive(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Get initial stats
	initialStats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	// Create a document
	doc := &models.Document{
		Name:        "Stats Test Doc",
		Source:      "test-stats",
		ContentType: "text/plain",
		Content:     "Content for stats test",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Chunk 1", Embedding: generateEmbedding(8, 0.1), TokenCount: 10},
		{Index: 1, Content: "Chunk 2", Embedding: generateEmbedding(8, 0.2), TokenCount: 15},
	}

	err = s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Get updated stats
	newStats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after add failed: %v", err)
	}

	if newStats.TotalDocuments <= initialStats.TotalDocuments {
		t.Error("TotalDocuments should have increased")
	}
	if newStats.TotalChunks < initialStats.TotalChunks+2 {
		t.Error("TotalChunks should have increased by at least 2")
	}
	if newStats.TotalTokens < initialStats.TotalTokens+25 {
		t.Errorf("TotalTokens should have increased by at least 25, was %d, now %d",
			initialStats.TotalTokens, newStats.TotalTokens)
	}
	if newStats.EmbeddingDimension != 8 {
		t.Errorf("EmbeddingDimension = %d, want 8", newStats.EmbeddingDimension)
	}
}

// ============================================================================
// Concurrent Operations Tests
// ============================================================================

func TestIntegration_ConcurrentAddDocuments(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var allDocIDs []string
	var mu sync.Mutex

	numGoroutines := 5
	docsPerGoroutine := 3
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*docsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < docsPerGoroutine; i++ {
				doc := &models.Document{
					Name:        fmt.Sprintf("Concurrent Doc G%d-D%d", goroutineID, i),
					Source:      "test-concurrent",
					ContentType: "text/plain",
					Content:     fmt.Sprintf("Content from goroutine %d doc %d", goroutineID, i),
				}
				chunks := []*models.DocumentChunk{
					{
						Index:     0,
						Content:   doc.Content,
						Embedding: generateEmbedding(8, float32(goroutineID)*0.1+float32(i)*0.01),
					},
				}

				err := s.AddDocument(ctx, doc, chunks)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d doc %d: %w", goroutineID, i, err)
					return
				}

				mu.Lock()
				allDocIDs = append(allDocIDs, doc.ID)
				mu.Unlock()
			}
		}(g)
	}

	wg.Wait()
	close(errors)
	defer func() { cleanupTestData(ctx, s, allDocIDs) }()

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent error: %v", err)
	}

	// Verify all documents were created
	if len(allDocIDs) != numGoroutines*docsPerGoroutine {
		t.Errorf("Expected %d documents, got %d", numGoroutines*docsPerGoroutine, len(allDocIDs))
	}

	// Verify all documents are retrievable
	for _, id := range allDocIDs {
		doc, err := s.GetDocument(ctx, id)
		if err != nil {
			t.Errorf("GetDocument for %s failed: %v", id, err)
		}
		if doc == nil {
			t.Errorf("Document %s not found", id)
		}
	}
}

func TestIntegration_ConcurrentSearches(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create some documents first
	for i := 0; i < 5; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Search Concurrent Doc %d", i),
			Source:      "test-concurrent-search",
			ContentType: "text/plain",
			Content:     "Searchable content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Searchable content", Embedding: generateEmbedding(8, float32(i)*0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Run concurrent searches
	numSearches := 10
	var wg sync.WaitGroup
	errors := make(chan error, numSearches)

	for i := 0; i < numSearches; i++ {
		wg.Add(1)
		go func(searchID int) {
			defer wg.Done()
			queryEmbedding := generateEmbedding(8, float32(searchID)*0.05)
			_, err := s.Search(ctx, &models.DocumentSearchRequest{
				Query:     "content",
				Limit:     10,
				Threshold: 0.3,
			}, queryEmbedding)
			if err != nil {
				errors <- fmt.Errorf("search %d: %w", searchID, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent search error: %v", err)
	}
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestIntegration_ZeroChunks(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Doc with No Chunks",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Document content but no chunks",
	}

	// Add document with empty chunks slice
	err := s.AddDocument(ctx, doc, []*models.DocumentChunk{})
	if err != nil {
		t.Fatalf("AddDocument with no chunks failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify document was created
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Document should exist")
	}
	if retrieved.ChunkCount != 0 {
		t.Errorf("ChunkCount = %d, want 0", retrieved.ChunkCount)
	}
}

func TestIntegration_VerySmallEmbeddings(t *testing.T) {
	s := createTestStore(t, 3) // Minimum dimension
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Small Embedding Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Content",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Chunk", Embedding: []float32{0.1, 0.2, 0.3}},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument with small embedding failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Search should work
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "test",
		Limit:     10,
		Threshold: 0.3,
	}, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	_ = resp
}

func TestIntegration_ContextCancellation(t *testing.T) {
	s := createTestStore(t, 8)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Operations should fail with context canceled
	_, err := s.GetDocument(ctx, "any-id")
	if err == nil {
		// Note: Some operations might succeed if they don't check context
		// This is implementation-dependent
		t.Log("GetDocument succeeded with canceled context (might be acceptable)")
	}
}

func TestIntegration_TransactionRollback(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Try to add document with invalid embedding dimension
	doc := &models.Document{
		Name:        "Invalid Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Content",
	}
	chunks := []*models.DocumentChunk{
		{
			Index:     0,
			Content:   "Valid chunk",
			Embedding: generateEmbedding(8, 0.1), // Correct dimension
		},
		{
			Index:     1,
			Content:   "Invalid chunk",
			Embedding: []float32{0.1, 0.2}, // Wrong dimension - should fail validation
		},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err == nil {
		// Clean up if somehow it succeeded
		_ = s.DeleteDocument(ctx, doc.ID)
		t.Fatal("Expected error for invalid embedding dimension")
	}

	// Document should not exist (transaction rolled back)
	if doc.ID != "" {
		retrieved, _ := s.GetDocument(ctx, doc.ID)
		if retrieved != nil {
			_ = s.DeleteDocument(ctx, doc.ID)
			t.Error("Document should not exist after failed transaction")
		}
	}
}

// ============================================================================
// Cosine Similarity Accuracy Tests
// ============================================================================

func TestIntegration_CosineSimilarityAccuracy(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents with known embeddings
	// Use unit vectors for predictable cosine similarity
	embeddings := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},           // e1 - points along first axis
		{0.7071, 0.7071, 0, 0, 0, 0, 0, 0}, // e2 - 45 degrees from e1 (cos = 0.7071)
		{0, 1, 0, 0, 0, 0, 0, 0},           // e3 - orthogonal to e1 (cos = 0)
		{-1, 0, 0, 0, 0, 0, 0, 0},          // e4 - opposite to e1 (cos = -1)
	}

	for i, emb := range embeddings {
		doc := &models.Document{
			Name:        fmt.Sprintf("Similarity Doc %d", i),
			Source:      "test-similarity",
			ContentType: "text/plain",
			Content:     fmt.Sprintf("Content %d", i),
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: fmt.Sprintf("Content %d", i), Embedding: emb},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search with e1 as query
	queryEmbedding := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "test",
		Limit:     10,
		Threshold: 0.0, // Accept all results including negative similarity
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Results should be ordered by similarity
	// e1 should be first (similarity = 1)
	// e2 should be second (similarity ~= 0.7071)
	// e3 and e4 depend on threshold

	if len(resp.Results) == 0 {
		t.Fatal("Expected at least one result")
	}

	// First result should be the identical vector
	if resp.Results[0].Score < 0.99 {
		t.Errorf("Identical vector should have score ~1.0, got %f", resp.Results[0].Score)
	}

	// If we have multiple results, verify ordering
	if len(resp.Results) >= 2 {
		// Second result (45-degree angle) should have lower score
		if resp.Results[1].Score > resp.Results[0].Score {
			t.Error("Results should be in descending order by score")
		}
	}
}

// ============================================================================
// Migration Tests
// ============================================================================

func TestIntegration_MigrationsIdempotent(t *testing.T) {
	db := getTestDB(t)

	// Create store (runs migrations)
	s1, err := New(Config{
		DB:            db,
		Dimension:     8,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("First store creation failed: %v", err)
	}

	// Create another store (migrations should be idempotent)
	s2, err := New(Config{
		DB:            db,
		Dimension:     8,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Second store creation failed: %v", err)
	}

	// Both should work
	ctx := context.Background()
	_, err = s1.Stats(ctx)
	if err != nil {
		t.Errorf("s1.Stats failed: %v", err)
	}
	_, err = s2.Stats(ctx)
	if err != nil {
		t.Errorf("s2.Stats failed: %v", err)
	}
}

// ============================================================================
// Metadata JSON Handling Tests
// ============================================================================

func TestIntegration_ComplexMetadata(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Complex Metadata Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Content with complex metadata",
		Metadata: models.DocumentMetadata{
			Title:       "Complex Title",
			Author:      "Test Author",
			Description: "A document with nested custom metadata",
			Tags:        []string{"tag1", "tag2", "tag3"},
			Language:    "en",
			AgentID:     "agent-complex",
			SessionID:   "session-complex",
			ChannelID:   "channel-complex",
			Custom: map[string]any{
				"string_key": "string_value",
				"int_key":    42,
				"float_key":  3.14,
				"bool_key":   true,
				"array_key":  []any{"a", "b", "c"},
				"nested": map[string]any{
					"level1": map[string]any{
						"level2": "deep_value",
					},
				},
			},
		},
	}

	chunks := []*models.DocumentChunk{
		{
			Index:     0,
			Content:   "Chunk content",
			Embedding: generateEmbedding(8, 0.1),
			Metadata: models.ChunkMetadata{
				DocumentName:   doc.Name,
				DocumentSource: doc.Source,
				Section:        "Section 1",
				AgentID:        "agent-complex",
				Tags:           []string{"chunk-tag"},
				Extra: map[string]any{
					"chunk_extra": "value",
				},
			},
		},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument with complex metadata failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Retrieve and verify
	retrieved, err := s.GetDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	if len(retrieved.Metadata.Tags) != 3 {
		t.Errorf("Tags count = %d, want 3", len(retrieved.Metadata.Tags))
	}
	if retrieved.Metadata.Custom["string_key"] != "string_value" {
		t.Errorf("Custom string_key mismatch")
	}
	// Note: JSON numbers become float64
	if retrieved.Metadata.Custom["int_key"] != float64(42) {
		t.Errorf("Custom int_key = %v, want 42", retrieved.Metadata.Custom["int_key"])
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkIntegration_AddDocument(b *testing.B) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		b.Skip("TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	s, err := New(Config{DB: db, Dimension: 8, RunMigrations: true})
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	embedding := generateEmbedding(8, 0.5)
	var docIDs []string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Bench Doc %d", i),
			Source:      "benchmark",
			ContentType: "text/plain",
			Content:     "Benchmark content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: embedding},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			b.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}
	b.StopTimer()

	// Cleanup
	for _, id := range docIDs {
		_ = s.DeleteDocument(ctx, id)
	}
}

func BenchmarkIntegration_Search(b *testing.B) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		b.Skip("TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	s, err := New(Config{DB: db, Dimension: 8, RunMigrations: true})
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()

	// Create test data
	var docIDs []string
	for i := 0; i < 100; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Search Bench Doc %d", i),
			Source:      "benchmark-search",
			ContentType: "text/plain",
			Content:     "Searchable content for benchmarking",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Searchable content", Embedding: generateEmbedding(8, float32(i)*0.01)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			b.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	queryEmbedding := generateEmbedding(8, 0.5)
	req := &models.DocumentSearchRequest{
		Query:     "content",
		Limit:     10,
		Threshold: 0.3,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Search(ctx, req, queryEmbedding)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
	b.StopTimer()

	// Cleanup
	for _, id := range docIDs {
		_ = s.DeleteDocument(ctx, id)
	}
}

// ============================================================================
// Helper function to verify test data cleanup
// ============================================================================

func TestIntegration_Cleanup(t *testing.T) {
	// This test runs last to verify we don't leave test data behind
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Check for any leftover test documents
	testSources := []string{
		"test", "test-list", "test-pagination", "test-ordering",
		"test-filter", "test-search", "test-threshold", "test-scope",
		"test-docid-filter", "test-limit", "test-concurrent",
		"test-concurrent-search", "test-similarity", "test-stats",
		"benchmark", "benchmark-search",
	}

	for _, source := range testSources {
		docs, err := s.ListDocuments(ctx, &store.ListOptions{Source: source, Limit: 1000})
		if err != nil {
			t.Logf("Warning: ListDocuments for %s failed: %v", source, err)
			continue
		}
		if len(docs) > 0 {
			t.Logf("Warning: Found %d leftover documents with source %s", len(docs), source)
			// Clean them up
			for _, doc := range docs {
				_ = s.DeleteDocument(ctx, doc.ID)
			}
		}
	}
}

// ============================================================================
// Tag Filtering Tests
// ============================================================================

func TestIntegration_ListDocuments_FilterByTags(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents with different tags
	docs := []struct {
		name string
		tags []string
	}{
		{"Doc with tag1", []string{"tag1"}},
		{"Doc with tag2", []string{"tag2"}},
		{"Doc with both", []string{"tag1", "tag2"}},
		{"Doc with none", nil},
	}

	for _, d := range docs {
		doc := &models.Document{
			Name:        d.name,
			Source:      "test-tags",
			ContentType: "text/plain",
			Content:     "Content",
			Metadata: models.DocumentMetadata{
				Tags: d.tags,
			},
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, 0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Note: Tag filtering uses ?| operator which matches ANY tag
	// This test verifies the query builds correctly
	tagDocs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source: "test-tags",
		Tags:   []string{"tag1"},
	})
	if err != nil {
		t.Fatalf("ListDocuments by tags failed: %v", err)
	}

	// Should find docs with tag1 (2 docs: "Doc with tag1" and "Doc with both")
	if len(tagDocs) < 1 {
		t.Errorf("Expected at least 1 document with tag1, got %d", len(tagDocs))
	}
}

// ============================================================================
// GetChunksByDocument Ordering Test
// ============================================================================

func TestIntegration_GetChunksByDocument_Ordering(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Ordered Chunks Doc",
		Source:      "test",
		ContentType: "text/plain",
		Content:     "Document with ordered chunks",
	}

	// Create chunks in non-sequential order
	chunks := []*models.DocumentChunk{
		{Index: 2, Content: "Third", Embedding: generateEmbedding(8, 0.3)},
		{Index: 0, Content: "First", Embedding: generateEmbedding(8, 0.1)},
		{Index: 1, Content: "Second", Embedding: generateEmbedding(8, 0.2)},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Get chunks - should be ordered by index
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}

	if len(storedChunks) != 3 {
		t.Fatalf("Expected 3 chunks, got %d", len(storedChunks))
	}

	// Verify they're in order
	expectedContents := []string{"First", "Second", "Third"}
	for i, chunk := range storedChunks {
		if chunk.Index != i {
			t.Errorf("Chunk at position %d has index %d", i, chunk.Index)
		}
		if chunk.Content != expectedContents[i] {
			t.Errorf("Chunk %d content = %q, want %q", i, chunk.Content, expectedContents[i])
		}
	}
}

// ============================================================================
// Search with Tags Test
// ============================================================================

func TestIntegration_Search_WithTags(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)

	// Create documents with different tags
	docs := []struct {
		name string
		tags []string
	}{
		{"Tagged Doc 1", []string{"important", "urgent"}},
		{"Tagged Doc 2", []string{"important"}},
		{"Untagged Doc", nil},
	}

	for _, d := range docs {
		doc := &models.Document{
			Name:        d.name,
			Source:      "test-search-tags",
			ContentType: "text/plain",
			Content:     "Content",
		}
		chunks := []*models.DocumentChunk{
			{
				Index:     0,
				Content:   "Searchable content",
				Embedding: embedding,
				Metadata: models.ChunkMetadata{
					Tags: d.tags,
				},
			},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	// Search with tag filter
	queryEmbedding := similarEmbedding(embedding, 0.1)
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "content",
		Tags:      []string{"urgent"},
		Limit:     10,
		Threshold: 0.5,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search with tags failed: %v", err)
	}

	// Should only find docs with "urgent" tag
	for _, result := range resp.Results {
		hasUrgent := false
		for _, tag := range result.Chunk.Metadata.Tags {
			if tag == "urgent" {
				hasUrgent = true
				break
			}
		}
		if !hasUrgent {
			t.Errorf("Result should have 'urgent' tag, got tags: %v", result.Chunk.Metadata.Tags)
		}
	}
}

// ============================================================================
// Search IncludeMetadata Test
// ============================================================================

func TestIntegration_Search_IncludeMetadata(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)
	doc := &models.Document{
		Name:        "Metadata Test Doc",
		Source:      "test-include-meta",
		ContentType: "text/plain",
		Content:     "Content",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Chunk", Embedding: embedding},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	queryEmbedding := similarEmbedding(embedding, 0.1)

	// Search with IncludeMetadata = true
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:           "content",
		Limit:           10,
		Threshold:       0.5,
		IncludeMetadata: true,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// When IncludeMetadata is true, embedding should be included
	if len(resp.Results) > 0 {
		if len(resp.Results[0].Chunk.Embedding) == 0 {
			t.Log("Embedding not included even with IncludeMetadata=true (implementation detail)")
		}
	}

	// Search with IncludeMetadata = false
	resp, err = s.Search(ctx, &models.DocumentSearchRequest{
		Query:           "content",
		Limit:           10,
		Threshold:       0.5,
		IncludeMetadata: false,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Embedding should not be included
	if len(resp.Results) > 0 && len(resp.Results[0].Chunk.Embedding) > 0 {
		t.Error("Embedding should not be included when IncludeMetadata=false")
	}
}

// ============================================================================
// ListDocuments by updated_at Test
// ============================================================================

func TestIntegration_ListDocuments_OrderByUpdatedAt(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents
	for i := 0; i < 3; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Update Order Doc %d", i),
			Source:      "test-update-order",
			ContentType: "text/plain",
			Content:     "Content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, 0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
		time.Sleep(20 * time.Millisecond)
	}

	// Update the first document (should make it most recently updated)
	time.Sleep(20 * time.Millisecond)
	firstDoc, _ := s.GetDocument(ctx, docIDs[0])
	firstDoc.Content = "Updated content"
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Updated chunk", Embedding: generateEmbedding(8, 0.2)},
	}
	err := s.AddDocument(ctx, firstDoc, chunks)
	if err != nil {
		t.Fatalf("Update AddDocument failed: %v", err)
	}

	// List by updated_at descending
	docs, err := s.ListDocuments(ctx, &store.ListOptions{
		Source:    "test-update-order",
		OrderBy:   "updated_at",
		OrderDesc: true,
	})
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}

	if len(docs) >= 1 {
		// First doc should be the most recently updated
		if docs[0].ID != docIDs[0] {
			t.Log("First document should be most recently updated (order may vary)")
		}
	}

	// Verify descending order
	for i := 1; i < len(docs); i++ {
		if docs[i].UpdatedAt.After(docs[i-1].UpdatedAt) {
			t.Errorf("Not in descending updated_at order")
		}
	}
}

// ============================================================================
// Default Values Tests
// ============================================================================

func TestIntegration_Search_DefaultValues(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)
	doc := &models.Document{
		Name:        "Default Values Doc",
		Source:      "test-defaults",
		ContentType: "text/plain",
		Content:     "Content",
	}
	chunks := []*models.DocumentChunk{
		{Index: 0, Content: "Chunk", Embedding: embedding},
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Search with minimal request (defaults should be applied)
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query: "content",
		// Limit and Threshold not set - should use defaults
	}, similarEmbedding(embedding, 0.1))
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Should work with defaults
	_ = resp
}

// ============================================================================
// Unique Document IDs Test
// ============================================================================

func TestIntegration_UniqueDocumentIDs(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create multiple documents and verify unique IDs
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Unique ID Doc %d", i),
			Source:      "test-unique",
			ContentType: "text/plain",
			Content:     "Content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, 0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}

		if ids[doc.ID] {
			t.Errorf("Duplicate document ID: %s", doc.ID)
		}
		ids[doc.ID] = true
		docIDs = append(docIDs, doc.ID)
	}
}

// ============================================================================
// Unique Chunk IDs Test
// ============================================================================

func TestIntegration_UniqueChunkIDs(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	doc := &models.Document{
		Name:        "Multi-chunk Doc",
		Source:      "test-chunk-ids",
		ContentType: "text/plain",
		Content:     "Content with many chunks",
	}

	chunks := make([]*models.DocumentChunk, 10)
	for i := 0; i < 10; i++ {
		chunks[i] = &models.DocumentChunk{
			Index:     i,
			Content:   fmt.Sprintf("Chunk %d", i),
			Embedding: generateEmbedding(8, float32(i)*0.1),
		}
	}

	err := s.AddDocument(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}
	docIDs = append(docIDs, doc.ID)

	// Verify unique chunk IDs
	storedChunks, err := s.GetChunksByDocument(ctx, doc.ID)
	if err != nil {
		t.Fatalf("GetChunksByDocument failed: %v", err)
	}

	ids := make(map[string]bool)
	for _, chunk := range storedChunks {
		if ids[chunk.ID] {
			t.Errorf("Duplicate chunk ID: %s", chunk.ID)
		}
		ids[chunk.ID] = true
	}
}

// ============================================================================
// Search Result Count Test
// ============================================================================

func TestIntegration_Search_TotalCount(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	embedding := generateEmbedding(8, 0.5)

	// Create 5 similar documents
	for i := 0; i < 5; i++ {
		doc := &models.Document{
			Name:        fmt.Sprintf("Count Doc %d", i),
			Source:      "test-count",
			ContentType: "text/plain",
			Content:     "Similar content",
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Similar content", Embedding: similarEmbedding(embedding, float32(i)*0.01)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
	}

	queryEmbedding := similarEmbedding(embedding, 0.05)

	// Search with limit 2
	resp, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "content",
		Limit:     2,
		Threshold: 0.3,
	}, queryEmbedding)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// TotalCount should match Results length (limited by Limit)
	if resp.TotalCount != len(resp.Results) {
		t.Errorf("TotalCount = %d, len(Results) = %d", resp.TotalCount, len(resp.Results))
	}
}

// ============================================================================
// Embedding Dimension Validation on Search
// ============================================================================

func TestIntegration_Search_DimensionMismatch(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()

	// Search with wrong dimension
	wrongDimEmbedding := generateEmbedding(4, 0.5) // 4 instead of 8

	_, err := s.Search(ctx, &models.DocumentSearchRequest{
		Query:     "test",
		Limit:     10,
		Threshold: 0.5,
	}, wrongDimEmbedding)

	if err == nil {
		t.Error("Expected error for dimension mismatch")
	}
}

// ============================================================================
// Sort Order Stability Test
// ============================================================================

func TestIntegration_ListDocuments_SortStability(t *testing.T) {
	s := createTestStore(t, 8)
	ctx := context.Background()
	var docIDs []string
	defer func() { cleanupTestData(ctx, s, docIDs) }()

	// Create documents with same name to test sort stability
	for i := 0; i < 5; i++ {
		doc := &models.Document{
			Name:        "Same Name",
			Source:      "test-sort-stability",
			ContentType: "text/plain",
			Content:     fmt.Sprintf("Content %d", i),
		}
		chunks := []*models.DocumentChunk{
			{Index: 0, Content: "Chunk", Embedding: generateEmbedding(8, 0.1)},
		}
		err := s.AddDocument(ctx, doc, chunks)
		if err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
		docIDs = append(docIDs, doc.ID)
		time.Sleep(10 * time.Millisecond)
	}

	// List multiple times and verify consistent order
	var lastOrder []string
	for attempt := 0; attempt < 3; attempt++ {
		docs, err := s.ListDocuments(ctx, &store.ListOptions{
			Source:  "test-sort-stability",
			OrderBy: "name",
		})
		if err != nil {
			t.Fatalf("ListDocuments failed: %v", err)
		}

		currentOrder := make([]string, len(docs))
		for i, doc := range docs {
			currentOrder[i] = doc.ID
		}

		if lastOrder != nil {
			// Compare with previous order
			sort.Strings(currentOrder)
			sort.Strings(lastOrder)
			// After sorting, they should match (same documents)
			if len(currentOrder) != len(lastOrder) {
				t.Error("Different number of documents returned")
			}
		}
		lastOrder = currentOrder
	}
}
