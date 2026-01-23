package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDocument_Struct(t *testing.T) {
	now := time.Now()
	doc := Document{
		ID:          "doc-123",
		Name:        "Test Document",
		Source:      "upload",
		SourceURI:   "/path/to/file.txt",
		ContentType: "text/plain",
		Content:     "Document content here",
		ChunkCount:  5,
		TotalTokens: 1000,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if doc.ID != "doc-123" {
		t.Errorf("ID = %q, want %q", doc.ID, "doc-123")
	}
	if doc.Source != "upload" {
		t.Errorf("Source = %q, want %q", doc.Source, "upload")
	}
	if doc.ChunkCount != 5 {
		t.Errorf("ChunkCount = %d, want 5", doc.ChunkCount)
	}
}

func TestDocument_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := Document{
		ID:          "doc-123",
		Name:        "Test Doc",
		Source:      "api",
		ContentType: "text/markdown",
		Content:     "# Hello",
		Metadata: DocumentMetadata{
			Title:       "Test Title",
			Author:      "Test Author",
			Description: "A test document",
			Tags:        []string{"test", "demo"},
		},
		ChunkCount: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Document
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Metadata.Title != original.Metadata.Title {
		t.Errorf("Metadata.Title = %q, want %q", decoded.Metadata.Title, original.Metadata.Title)
	}
	if len(decoded.Metadata.Tags) != len(original.Metadata.Tags) {
		t.Errorf("Metadata.Tags length = %d, want %d", len(decoded.Metadata.Tags), len(original.Metadata.Tags))
	}
}

func TestDocumentMetadata_Struct(t *testing.T) {
	meta := DocumentMetadata{
		Title:       "Document Title",
		Author:      "Author Name",
		Description: "Description text",
		Tags:        []string{"tag1", "tag2"},
		Language:    "en",
		AgentID:     "agent-123",
		SessionID:   "session-456",
		ChannelID:   "channel-789",
		Custom:      map[string]any{"key": "value"},
	}

	if meta.Title != "Document Title" {
		t.Errorf("Title = %q, want %q", meta.Title, "Document Title")
	}
	if meta.Language != "en" {
		t.Errorf("Language = %q, want %q", meta.Language, "en")
	}
	if len(meta.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(meta.Tags))
	}
}

func TestDocumentChunk_Struct(t *testing.T) {
	now := time.Now()
	chunk := DocumentChunk{
		ID:          "chunk-123",
		DocumentID:  "doc-456",
		Index:       2,
		Content:     "Chunk content",
		Embedding:   []float32{0.1, 0.2, 0.3},
		StartOffset: 100,
		EndOffset:   200,
		TokenCount:  50,
		CreatedAt:   now,
	}

	if chunk.ID != "chunk-123" {
		t.Errorf("ID = %q, want %q", chunk.ID, "chunk-123")
	}
	if chunk.Index != 2 {
		t.Errorf("Index = %d, want 2", chunk.Index)
	}
	if len(chunk.Embedding) != 3 {
		t.Errorf("Embedding length = %d, want 3", len(chunk.Embedding))
	}
	if chunk.StartOffset != 100 {
		t.Errorf("StartOffset = %d, want 100", chunk.StartOffset)
	}
}

func TestChunkMetadata_Struct(t *testing.T) {
	meta := ChunkMetadata{
		DocumentName:   "Test Doc",
		DocumentSource: "upload",
		Section:        "Introduction",
		AgentID:        "agent-123",
		SessionID:      "session-456",
		ChannelID:      "channel-789",
		Tags:           []string{"tag1"},
		Extra:          map[string]any{"key": "value"},
	}

	if meta.DocumentName != "Test Doc" {
		t.Errorf("DocumentName = %q, want %q", meta.DocumentName, "Test Doc")
	}
	if meta.Section != "Introduction" {
		t.Errorf("Section = %q, want %q", meta.Section, "Introduction")
	}
}

func TestDocumentScope_Constants(t *testing.T) {
	tests := []struct {
		constant DocumentScope
		expected string
	}{
		{DocumentScopeGlobal, "global"},
		{DocumentScopeAgent, "agent"},
		{DocumentScopeSession, "session"},
		{DocumentScopeChannel, "channel"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestDocumentSearchRequest_Struct(t *testing.T) {
	req := DocumentSearchRequest{
		Query:           "test query",
		Scope:           DocumentScopeAgent,
		ScopeID:         "agent-123",
		Limit:           10,
		Threshold:       0.7,
		Tags:            []string{"important"},
		DocumentIDs:     []string{"doc-1", "doc-2"},
		IncludeMetadata: true,
	}

	if req.Query != "test query" {
		t.Errorf("Query = %q, want %q", req.Query, "test query")
	}
	if req.Scope != DocumentScopeAgent {
		t.Errorf("Scope = %v, want %v", req.Scope, DocumentScopeAgent)
	}
	if req.Threshold != 0.7 {
		t.Errorf("Threshold = %v, want 0.7", req.Threshold)
	}
	if !req.IncludeMetadata {
		t.Error("IncludeMetadata should be true")
	}
}

func TestDocumentSearchResult_Struct(t *testing.T) {
	chunk := &DocumentChunk{ID: "chunk-123", Content: "test"}
	result := DocumentSearchResult{
		Chunk:      chunk,
		Score:      0.95,
		Highlights: []string{"matched text"},
	}

	if result.Chunk == nil {
		t.Fatal("Chunk is nil")
	}
	if result.Score != 0.95 {
		t.Errorf("Score = %v, want 0.95", result.Score)
	}
	if len(result.Highlights) != 1 {
		t.Errorf("Highlights length = %d, want 1", len(result.Highlights))
	}
}

func TestDocumentSearchResponse_Struct(t *testing.T) {
	response := DocumentSearchResponse{
		Results: []*DocumentSearchResult{
			{Score: 0.9},
			{Score: 0.8},
		},
		TotalCount: 100,
		QueryTime:  50 * time.Millisecond,
	}

	if len(response.Results) != 2 {
		t.Errorf("Results length = %d, want 2", len(response.Results))
	}
	if response.TotalCount != 100 {
		t.Errorf("TotalCount = %d, want 100", response.TotalCount)
	}
	if response.QueryTime != 50*time.Millisecond {
		t.Errorf("QueryTime = %v, want 50ms", response.QueryTime)
	}
}
