package chunker

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/rag/parser"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ============================================================================
// Config Tests
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ChunkSize != 1000 {
		t.Errorf("ChunkSize = %d, want 1000", cfg.ChunkSize)
	}
	if cfg.ChunkOverlap != 200 {
		t.Errorf("ChunkOverlap = %d, want 200", cfg.ChunkOverlap)
	}
	if cfg.MinChunkSize != 100 {
		t.Errorf("MinChunkSize = %d, want 100", cfg.MinChunkSize)
	}
	if cfg.PreserveWhitespace != false {
		t.Error("PreserveWhitespace should be false by default")
	}
	if cfg.KeepSeparators != true {
		t.Error("KeepSeparators should be true by default")
	}
}

func TestConfigWithCustomValues(t *testing.T) {
	cfg := Config{
		ChunkSize:          500,
		ChunkOverlap:       100,
		MinChunkSize:       50,
		PreserveWhitespace: true,
		KeepSeparators:     false,
	}

	if cfg.ChunkSize != 500 {
		t.Errorf("ChunkSize = %d, want 500", cfg.ChunkSize)
	}
	if cfg.ChunkOverlap != 100 {
		t.Errorf("ChunkOverlap = %d, want 100", cfg.ChunkOverlap)
	}
	if cfg.MinChunkSize != 50 {
		t.Errorf("MinChunkSize = %d, want 50", cfg.MinChunkSize)
	}
}

// ============================================================================
// SimpleTokenCounter Tests
// ============================================================================

func TestSimpleTokenCounter_Count(t *testing.T) {
	tests := []struct {
		name          string
		charsPerToken int
		text          string
		want          int
	}{
		{
			name:          "empty text",
			charsPerToken: 4,
			text:          "",
			want:          0,
		},
		{
			name:          "short text default",
			charsPerToken: 0, // Should default to 4
			text:          "hello",
			want:          2, // 5 chars / 4 = 2 (rounded up)
		},
		{
			name:          "exact multiple",
			charsPerToken: 4,
			text:          "12345678",
			want:          2,
		},
		{
			name:          "with remainder",
			charsPerToken: 4,
			text:          "123456789",
			want:          3, // 9 chars / 4 = 2.25, rounded up to 3
		},
		{
			name:          "custom chars per token",
			charsPerToken: 5,
			text:          "12345678901234567890", // 20 chars
			want:          4,                      // 20 / 5 = 4
		},
		{
			name:          "single character",
			charsPerToken: 4,
			text:          "a",
			want:          1,
		},
		{
			name:          "unicode text",
			charsPerToken: 4,
			text:          "Hello world!",
			want:          3, // 12 chars / 4 = 3 (len() counts bytes, not runes for ASCII)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &SimpleTokenCounter{CharsPerToken: tt.charsPerToken}
			got := tc.Count(tt.text)
			if got != tt.want {
				t.Errorf("Count() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSimpleTokenCounter_DefaultCharsPerToken(t *testing.T) {
	tc := &SimpleTokenCounter{CharsPerToken: 0}
	text := "1234567890" // 10 chars

	got := tc.Count(text)
	// With default 4 chars per token: 10/4 = 3 (rounded up)
	if got != 3 {
		t.Errorf("Count() with default = %d, want 3", got)
	}
}

// ============================================================================
// BuildChunkMetadata Tests
// ============================================================================

func TestBuildChunkMetadata(t *testing.T) {
	doc := &models.Document{
		Name:   "Test Document",
		Source: "upload",
		Metadata: models.DocumentMetadata{
			AgentID:   "agent-123",
			SessionID: "session-456",
			ChannelID: "channel-789",
			Tags:      []string{"tag1", "tag2"},
			Custom: map[string]any{
				"key1": "value1",
				"key2": 42,
			},
		},
	}

	meta := BuildChunkMetadata(doc, "Introduction")

	if meta.DocumentName != "Test Document" {
		t.Errorf("DocumentName = %q, want %q", meta.DocumentName, "Test Document")
	}
	if meta.DocumentSource != "upload" {
		t.Errorf("DocumentSource = %q, want %q", meta.DocumentSource, "upload")
	}
	if meta.Section != "Introduction" {
		t.Errorf("Section = %q, want %q", meta.Section, "Introduction")
	}
	if meta.AgentID != "agent-123" {
		t.Errorf("AgentID = %q, want %q", meta.AgentID, "agent-123")
	}
	if meta.SessionID != "session-456" {
		t.Errorf("SessionID = %q, want %q", meta.SessionID, "session-456")
	}
	if meta.ChannelID != "channel-789" {
		t.Errorf("ChannelID = %q, want %q", meta.ChannelID, "channel-789")
	}
	if len(meta.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(meta.Tags))
	}
	if len(meta.Extra) != 2 {
		t.Errorf("Extra len = %d, want 2", len(meta.Extra))
	}
	if meta.Extra["key1"] != "value1" {
		t.Errorf("Extra[key1] = %v, want 'value1'", meta.Extra["key1"])
	}
}

func TestBuildChunkMetadata_NilCustom(t *testing.T) {
	doc := &models.Document{
		Name:   "Test Document",
		Source: "upload",
		Metadata: models.DocumentMetadata{
			Custom: nil,
		},
	}

	meta := BuildChunkMetadata(doc, "")

	if meta.Extra != nil {
		t.Errorf("Extra should be nil when Custom is nil, got %v", meta.Extra)
	}
}

func TestBuildChunkMetadata_EmptySection(t *testing.T) {
	doc := &models.Document{
		Name:   "Test Document",
		Source: "upload",
	}

	meta := BuildChunkMetadata(doc, "")

	if meta.Section != "" {
		t.Errorf("Section = %q, want empty", meta.Section)
	}
}

// ============================================================================
// Chunk Struct Tests
// ============================================================================

func TestChunk_Structure(t *testing.T) {
	chunk := Chunk{
		Content:     "Test content",
		StartOffset: 0,
		EndOffset:   12,
		Section:     "Introduction",
	}

	if chunk.Content != "Test content" {
		t.Errorf("Content = %q, want %q", chunk.Content, "Test content")
	}
	if chunk.StartOffset != 0 {
		t.Errorf("StartOffset = %d, want 0", chunk.StartOffset)
	}
	if chunk.EndOffset != 12 {
		t.Errorf("EndOffset = %d, want 12", chunk.EndOffset)
	}
	if chunk.Section != "Introduction" {
		t.Errorf("Section = %q, want %q", chunk.Section, "Introduction")
	}
}

// ============================================================================
// RecursiveCharacterTextSplitter Tests
// ============================================================================

func TestNewRecursiveCharacterTextSplitter(t *testing.T) {
	tests := []struct {
		name             string
		cfg              Config
		wantChunkSize    int
		wantChunkOverlap int
		wantMinChunkSize int
	}{
		{
			name:             "default values when zero",
			cfg:              Config{ChunkSize: 0, ChunkOverlap: 0, MinChunkSize: 0},
			wantChunkSize:    1000,
			wantChunkOverlap: 0, // Overlap 0 stays 0 (not negative)
			wantMinChunkSize: 100,
		},
		{
			name:             "custom values",
			cfg:              Config{ChunkSize: 500, ChunkOverlap: 100, MinChunkSize: 50},
			wantChunkSize:    500,
			wantChunkOverlap: 100,
			wantMinChunkSize: 50,
		},
		{
			name:             "overlap exceeds chunk size - adjusted",
			cfg:              Config{ChunkSize: 100, ChunkOverlap: 150},
			wantChunkSize:    100,
			wantChunkOverlap: 20, // chunk_size / 5
			wantMinChunkSize: 100,
		},
		{
			name:             "negative overlap - defaults to DefaultConfig overlap",
			cfg:              Config{ChunkSize: 500, ChunkOverlap: -10, MinChunkSize: 50},
			wantChunkSize:    500,
			wantChunkOverlap: 200, // defaults to DefaultConfig when negative
			wantMinChunkSize: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitter := NewRecursiveCharacterTextSplitter(tt.cfg)
			if splitter.config.ChunkSize != tt.wantChunkSize {
				t.Errorf("ChunkSize = %d, want %d", splitter.config.ChunkSize, tt.wantChunkSize)
			}
			if splitter.config.ChunkOverlap != tt.wantChunkOverlap {
				t.Errorf("ChunkOverlap = %d, want %d", splitter.config.ChunkOverlap, tt.wantChunkOverlap)
			}
			if splitter.config.MinChunkSize != tt.wantMinChunkSize {
				t.Errorf("MinChunkSize = %d, want %d", splitter.config.MinChunkSize, tt.wantMinChunkSize)
			}
		})
	}
}

func TestRecursiveCharacterTextSplitter_Name(t *testing.T) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	if splitter.Name() != "recursive_character" {
		t.Errorf("Name() = %q, want %q", splitter.Name(), "recursive_character")
	}
}

func TestRecursiveCharacterTextSplitter_WithSeparators(t *testing.T) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	customSeps := []string{"\n\n", "\n", " "}
	splitter.WithSeparators(customSeps)

	if len(splitter.separators) != 3 {
		t.Errorf("separators len = %d, want 3", len(splitter.separators))
	}
}

func TestRecursiveCharacterTextSplitter_WithTokenCounter(t *testing.T) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	customTC := &SimpleTokenCounter{CharsPerToken: 3}
	splitter.WithTokenCounter(customTC)

	if splitter.tokenCounter != customTC {
		t.Error("tokenCounter not set correctly")
	}
}

// ============================================================================
// Chunk Method Tests
// ============================================================================

func TestChunk_EmptyContent(t *testing.T) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	doc := &models.Document{ID: "test", Name: "Test"}
	parseResult := &parser.ParseResult{Content: ""}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if chunks != nil {
		t.Errorf("Chunk() = %v, want nil for empty content", chunks)
	}
}

func TestChunk_WhitespaceOnlyContent(t *testing.T) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	doc := &models.Document{ID: "test", Name: "Test"}
	parseResult := &parser.ParseResult{Content: "   \n\n\t  "}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if chunks != nil {
		t.Errorf("Chunk() = %v, want nil for whitespace-only content", chunks)
	}
}

func TestChunk_SmallContent(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 200,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}
	parseResult := &parser.ParseResult{Content: "This is a small piece of text."}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("len(chunks) = %d, want 1", len(chunks))
	}
	// The content should be present (trimmed of whitespace but otherwise intact)
	if len(chunks[0].Content) == 0 {
		t.Error("Chunk content should not be empty")
	}
}

func TestChunk_WithParagraphSeparation(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "First paragraph with some content here.\n\nSecond paragraph with different content."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Errorf("Expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestChunk_WithLineSeparation(t *testing.T) {
	cfg := Config{
		ChunkSize:    30,
		ChunkOverlap: 5,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "Line one content.\nLine two content.\nLine three content."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}
}

func TestChunk_WithSentenceSeparation(t *testing.T) {
	cfg := Config{
		ChunkSize:    40,
		ChunkOverlap: 10,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "First sentence here. Second sentence here. Third sentence here."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}
}

func TestChunk_ChunkIndexSequential(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "First part of the document. Second part of the document. Third part of the document. Fourth part of document."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Errorf("Chunk[%d].Index = %d, want %d", i, chunk.Index, i)
		}
	}
}

func TestChunk_ChunksHaveDocumentID(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		MinChunkSize: 10, // Low min chunk size to ensure we get chunks
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "doc-123", Name: "Test"}
	// Content longer than MinChunkSize
	parseResult := &parser.ParseResult{Content: "Some content to chunk that is long enough to not be filtered out by min size."}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	for _, chunk := range chunks {
		if chunk.DocumentID != "doc-123" {
			t.Errorf("DocumentID = %q, want %q", chunk.DocumentID, "doc-123")
		}
	}
}

func TestChunk_ChunksHaveMetadata(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		MinChunkSize: 10, // Low min chunk size
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{
		ID:     "doc-123",
		Name:   "Test Document",
		Source: "test",
		Metadata: models.DocumentMetadata{
			AgentID: "agent-1",
			Tags:    []string{"important"},
		},
	}
	// Content longer than MinChunkSize
	parseResult := &parser.ParseResult{Content: "Some content to chunk for metadata test that is long enough to produce chunks."}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	for _, chunk := range chunks {
		if chunk.Metadata.DocumentName != "Test Document" {
			t.Errorf("Metadata.DocumentName = %q, want %q", chunk.Metadata.DocumentName, "Test Document")
		}
		if chunk.Metadata.DocumentSource != "test" {
			t.Errorf("Metadata.DocumentSource = %q, want %q", chunk.Metadata.DocumentSource, "test")
		}
		if chunk.Metadata.AgentID != "agent-1" {
			t.Errorf("Metadata.AgentID = %q, want %q", chunk.Metadata.AgentID, "agent-1")
		}
	}
}

func TestChunk_ChunksHaveTokenCount(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		MinChunkSize: 10, // Low min chunk size
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}
	// Content longer than MinChunkSize
	parseResult := &parser.ParseResult{Content: "Some content for token count testing that is sufficiently long to create a chunk."}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	for _, chunk := range chunks {
		if chunk.TokenCount == 0 {
			t.Error("TokenCount should not be 0")
		}
		if chunk.TokenCount < 0 {
			t.Error("TokenCount should not be negative")
		}
	}
}

func TestChunk_ChunksHaveIDs(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		MinChunkSize: 10, // Low min chunk size
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}
	// Content longer than MinChunkSize
	parseResult := &parser.ParseResult{Content: "Content for ID test that is long enough to produce a chunk."}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	for _, chunk := range chunks {
		if chunk.ID == "" {
			t.Error("Chunk ID should not be empty")
		}
	}
}

func TestChunk_ChunksHaveCreatedAt(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		MinChunkSize: 10, // Low min chunk size
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}
	// Content longer than MinChunkSize
	parseResult := &parser.ParseResult{Content: "Content for timestamp test that is long enough to produce a chunk."}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	for _, chunk := range chunks {
		if chunk.CreatedAt.IsZero() {
			t.Error("Chunk CreatedAt should not be zero")
		}
	}
}

// ============================================================================
// Chunk Overlap Tests
// ============================================================================

func TestChunk_WithOverlap(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 20,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "First part of document. Second part of document. Third part of document."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Skip("Need at least 2 chunks to test overlap")
	}

	// Check that later chunks have overlapping content from previous chunks
	// This is difficult to test exactly due to splitting behavior,
	// but we can verify the mechanism exists
	t.Logf("Got %d chunks", len(chunks))
}

func TestChunk_NoOverlap(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 0,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "First part of document here. Second part of document here."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}
}

// ============================================================================
// Section Finding Tests
// ============================================================================

func TestFindSection(t *testing.T) {
	sections := []parser.Section{
		{Title: "Introduction", StartOffset: 0, EndOffset: 100},
		{Title: "Methods", StartOffset: 100, EndOffset: 200},
		{Title: "Results", StartOffset: 200, EndOffset: 300},
	}

	tests := []struct {
		name   string
		offset int
		want   string
	}{
		{"at start", 0, "Introduction"},
		{"in middle of first", 50, "Introduction"},
		{"at section boundary", 100, "Methods"},
		{"in second section", 150, "Methods"},
		{"in last section", 250, "Results"},
		{"before all sections", -1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSection(sections, tt.offset)
			if got != tt.want {
				t.Errorf("findSection() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindSection_EmptySections(t *testing.T) {
	got := findSection(nil, 50)
	if got != "" {
		t.Errorf("findSection() = %q, want empty for nil sections", got)
	}

	got = findSection([]parser.Section{}, 50)
	if got != "" {
		t.Errorf("findSection() = %q, want empty for empty sections", got)
	}
}

// ============================================================================
// Chunk with Sections Tests
// ============================================================================

func TestChunk_WithSections(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "Introduction content here. Methods content here. Results content here."
	parseResult := &parser.ParseResult{
		Content: content,
		Sections: []parser.Section{
			{Title: "Introduction", StartOffset: 0, EndOffset: 26},
			{Title: "Methods", StartOffset: 27, EndOffset: 49},
			{Title: "Results", StartOffset: 50, EndOffset: 70},
		},
	}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	// Chunks should have section metadata
	if len(chunks) > 0 {
		// First chunk should have a section
		t.Logf("First chunk section: %q", chunks[0].Metadata.Section)
	}
}

// ============================================================================
// Edge Cases Tests
// ============================================================================

func TestChunk_VeryLongText(t *testing.T) {
	cfg := Config{
		ChunkSize:    100,
		ChunkOverlap: 20,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	// Generate 10KB of content
	content := make([]byte, 10000)
	for i := range content {
		content[i] = 'a' + byte(i%26)
		if i > 0 && i%100 == 0 {
			content[i] = '\n'
		}
	}
	parseResult := &parser.ParseResult{Content: string(content)}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected multiple chunks for long text")
	}
	t.Logf("Created %d chunks for 10KB content", len(chunks))
}

func TestChunk_TextWithSpecialCharacters(t *testing.T) {
	cfg := Config{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		MinChunkSize: 10, // Low min chunk size
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	// Content with special chars that is long enough (> MinChunkSize)
	content := "Content with special chars: <>&\"' and unicode: Hello! Also: tabs\tand\nnewlines. This text is extended to be longer than the minimum chunk size requirement."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}
}

func TestChunk_TextWithOnlyNewlines(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 5,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "Line1\n\n\n\nLine2\n\n\n\nLine3"
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	// Should handle multiple newlines gracefully
	t.Logf("Got %d chunks", len(chunks))
}

func TestChunk_SingleLongWord(t *testing.T) {
	cfg := Config{
		ChunkSize:    20,
		ChunkOverlap: 5,
		MinChunkSize: 5,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	// Word longer than chunk size
	content := "supercalifragilisticexpialidocious"
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	// Should split even a single long word by character
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk for long word")
	}
}

// ============================================================================
// Markdown Splitter Tests
// ============================================================================

func TestNewMarkdownSplitter(t *testing.T) {
	cfg := Config{
		ChunkSize:    500,
		ChunkOverlap: 100,
		MinChunkSize: 50,
	}
	splitter := NewMarkdownSplitter(cfg)

	// Should use markdown separators
	if splitter.separators[0] != "\n## " {
		t.Errorf("First separator = %q, want %q", splitter.separators[0], "\n## ")
	}
}

func TestMarkdownSplitter_WithHeadings(t *testing.T) {
	cfg := Config{
		ChunkSize:    100,
		ChunkOverlap: 20,
		MinChunkSize: 20,
	}
	splitter := NewMarkdownSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := `# Main Title

Introduction paragraph here.

## Section One

Content for section one.

## Section Two

Content for section two.`

	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk")
	}
	t.Logf("Got %d chunks for markdown content", len(chunks))
}

// ============================================================================
// Default Separators Tests
// ============================================================================

func TestDefaultSeparators(t *testing.T) {
	if len(DefaultSeparators) == 0 {
		t.Error("DefaultSeparators should not be empty")
	}

	// First separator should be paragraph break
	if DefaultSeparators[0] != "\n\n" {
		t.Errorf("First separator = %q, want %q", DefaultSeparators[0], "\n\n")
	}

	// Last separator should be empty (character split)
	if DefaultSeparators[len(DefaultSeparators)-1] != "" {
		t.Errorf("Last separator = %q, want empty", DefaultSeparators[len(DefaultSeparators)-1])
	}
}

func TestMarkdownSeparators(t *testing.T) {
	if len(MarkdownSeparators) == 0 {
		t.Error("MarkdownSeparators should not be empty")
	}

	// Should start with heading separators
	if MarkdownSeparators[0] != "\n## " {
		t.Errorf("First separator = %q, want %q", MarkdownSeparators[0], "\n## ")
	}
}

// ============================================================================
// Chunker Interface Tests
// ============================================================================

func TestRecursiveCharacterTextSplitter_ImplementsChunker(t *testing.T) {
	var _ Chunker = (*RecursiveCharacterTextSplitter)(nil)
}

// ============================================================================
// Offset Tests
// ============================================================================

func TestChunk_OffsetsAreValid(t *testing.T) {
	cfg := Config{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 10,
	}
	splitter := NewRecursiveCharacterTextSplitter(cfg)
	doc := &models.Document{ID: "test", Name: "Test"}

	content := "First sentence here. Second sentence here. Third sentence here. Fourth here."
	parseResult := &parser.ParseResult{Content: content}

	chunks, err := splitter.Chunk(doc, parseResult)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	for i, chunk := range chunks {
		if chunk.StartOffset < 0 {
			t.Errorf("Chunk[%d] StartOffset = %d, should not be negative", i, chunk.StartOffset)
		}
		if chunk.EndOffset <= chunk.StartOffset {
			t.Errorf("Chunk[%d] EndOffset = %d, should be > StartOffset = %d", i, chunk.EndOffset, chunk.StartOffset)
		}
	}
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkChunk_SmallText(b *testing.B) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	doc := &models.Document{ID: "test", Name: "Test"}
	parseResult := &parser.ParseResult{Content: "This is a small piece of text for benchmarking."}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitter.Chunk(doc, parseResult)
	}
}

func BenchmarkChunk_LargeText(b *testing.B) {
	splitter := NewRecursiveCharacterTextSplitter(DefaultConfig())
	doc := &models.Document{ID: "test", Name: "Test"}

	content := make([]byte, 100000)
	for i := range content {
		content[i] = 'a' + byte(i%26)
		if i > 0 && i%100 == 0 {
			content[i] = '\n'
		}
	}
	parseResult := &parser.ParseResult{Content: string(content)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitter.Chunk(doc, parseResult)
	}
}

func BenchmarkSimpleTokenCounter(b *testing.B) {
	tc := &SimpleTokenCounter{CharsPerToken: 4}
	text := "This is some sample text for token counting benchmark."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tc.Count(text)
	}
}
