package memorysearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Config configures memory search including file paths, result limits,
// search mode (lexical, vector, or hybrid), and optional embeddings.
type Config struct {
	Directory     string
	MemoryFile    string
	WorkspacePath string
	MaxResults    int
	MaxSnippetLen int
	Mode          string
	Embeddings    EmbeddingsConfig
}

// MemorySearchTool implements agent.Tool for searching memory files.
// It supports lexical, vector (TF-IDF or embeddings), and hybrid search modes.
type MemorySearchTool struct {
	config   Config
	embedder embedder
}

// NewMemorySearchTool creates a new memory search tool with the given configuration.
// It initializes the embedder if embeddings are configured.
func NewMemorySearchTool(cfg *Config) *MemorySearchTool {
	config := Config{}
	if cfg != nil {
		config = *cfg
	}
	if config.MaxResults == 0 {
		config.MaxResults = 5
	}
	if config.MaxSnippetLen == 0 {
		config.MaxSnippetLen = 200
	}
	if strings.TrimSpace(config.Mode) == "" {
		config.Mode = "hybrid"
	}
	tool := &MemorySearchTool{config: config}
	if config.Embeddings.enabled() {
		emb, err := newRemoteEmbedder(config.Embeddings)
		if err != nil {
			slog.Warn("memory search embeddings disabled", "error", err)
		} else {
			tool.embedder = emb
		}
	}
	return tool
}

// Name returns the tool name.
func (t *MemorySearchTool) Name() string {
	return "memory_search"
}

// Description explains the tool.
func (t *MemorySearchTool) Description() string {
	return "Searches local memory files (MEMORY.md and memory logs) for a query."
}

// Schema defines the parameters for the tool.
func (t *MemorySearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "max_results": {"type": "integer", "description": "Max results to return"}
  },
  "required": ["query"]
}`)
}

// Execute runs the search.
func (t *MemorySearchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}, nil
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &agent.ToolResult{Content: "query is required", IsError: true}, nil
	}

	maxResults := t.config.MaxResults
	if input.MaxResults > 0 {
		maxResults = input.MaxResults
	}
	if maxResults <= 0 {
		maxResults = 5
	}

	files := t.resolveFiles()
	results := searchFiles(ctx, files, query, maxResults, t.config.MaxSnippetLen, t.config.Mode, t.embedder)

	payload, err := json.MarshalIndent(struct {
		Query   string         `json:"query"`
		Results []SearchResult `json:"results"`
	}{
		Query:   query,
		Results: results,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("failed to encode results: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func (t *MemorySearchTool) resolveFiles() []string {
	var files []string

	if memoryFile := strings.TrimSpace(t.config.MemoryFile); memoryFile != "" {
		files = append(files, resolvePath(t.config.WorkspacePath, memoryFile))
	}

	if dir := strings.TrimSpace(t.config.Directory); dir != "" {
		resolved := resolvePath(t.config.WorkspacePath, dir)
		entries, err := os.ReadDir(resolved)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
					files = append(files, filepath.Join(resolved, entry.Name()))
				}
			}
		}
	}

	return dedupe(files)
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) || strings.TrimSpace(base) == "" {
		return path
	}
	return filepath.Join(base, path)
}

// SearchResult represents a single memory search result with file path,
// matched snippet, match count, and relevance score.
type SearchResult struct {
	File    string  `json:"file"`
	Snippet string  `json:"snippet"`
	Matches int     `json:"matches"`
	Score   float64 `json:"score"`
}

func searchFiles(ctx context.Context, files []string, query string, maxResults int, maxSnippetLen int, mode string, embedder embedder) []SearchResult {
	chunks := loadChunks(files)
	if len(chunks) == 0 {
		return nil
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "hybrid"
	}
	switch mode {
	case "vector":
		return rankVector(ctx, chunks, query, maxResults, maxSnippetLen, embedder)
	case "lexical":
		return rankLexical(chunks, query, maxResults, maxSnippetLen)
	default:
		return rankHybrid(ctx, chunks, query, maxResults, maxSnippetLen, embedder)
	}
}

func findMatches(content string, needle string, maxSnippetLen int) (int, string) {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, needle)
	if idx == -1 {
		return 0, ""
	}
	count := strings.Count(lower, needle)
	runeIndex := utf8.RuneCountInString(lower[:idx])

	runeContent := []rune(content)
	start := runeIndex - 40
	if start < 0 {
		start = 0
	}
	end := runeIndex + len([]rune(needle)) + 40
	if end > len(runeContent) {
		end = len(runeContent)
	}
	snippet := strings.TrimSpace(string(runeContent[start:end]))
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runeContent) {
		snippet = snippet + "..."
	}
	if maxSnippetLen > 0 && len([]rune(snippet)) > maxSnippetLen {
		snippet = string([]rune(snippet)[:maxSnippetLen]) + "..."
	}
	return count, snippet
}

type chunk struct {
	file   string
	text   string
	tokens []string
}

func loadChunks(files []string) []chunk {
	var chunks []chunk
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, part := range splitChunks(string(content)) {
			tokens := tokenize(part)
			if len(tokens) == 0 {
				continue
			}
			chunks = append(chunks, chunk{file: path, text: part, tokens: tokens})
		}
	}
	return chunks
}

func splitChunks(content string) []string {
	parts := strings.Split(content, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func tokenize(content string) []string {
	content = strings.ToLower(content)
	fields := strings.FieldsFunc(content, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 2 {
			continue
		}
		tokens = append(tokens, field)
	}
	return tokens
}

func rankLexical(chunks []chunk, query string, maxResults int, maxSnippetLen int) []SearchResult {
	needle := strings.ToLower(query)
	var results []SearchResult
	for _, chunk := range chunks {
		matches, snippet := findMatches(chunk.text, needle, maxSnippetLen)
		if matches == 0 {
			continue
		}
		results = append(results, SearchResult{File: chunk.file, Snippet: snippet, Matches: matches, Score: float64(matches)})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].File < results[j].File
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func rankVector(ctx context.Context, chunks []chunk, query string, maxResults int, maxSnippetLen int, embedder embedder) []SearchResult {
	if embedder != nil {
		results, err := rankVectorRemote(ctx, chunks, query, maxResults, maxSnippetLen, embedder)
		if err != nil {
			slog.Warn("memory search embeddings failed; falling back to local vectors", "error", err)
		} else if len(results) > 0 {
			return results
		}
	}
	return rankVectorTFIDF(chunks, query, maxResults, maxSnippetLen)
}

func rankVectorTFIDF(chunks []chunk, query string, maxResults int, maxSnippetLen int) []SearchResult {
	tfidf := buildTFIDF(chunks)
	queryVec := tfidf.Vectorize(tokenize(query))
	var results []SearchResult
	for _, chunk := range chunks {
		score := cosine(queryVec, tfidf.Vectorize(chunk.tokens))
		if score <= 0 {
			continue
		}
		snippet := clampSnippet(chunk.text, maxSnippetLen)
		results = append(results, SearchResult{File: chunk.file, Snippet: snippet, Matches: 0, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].File < results[j].File
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func rankHybrid(ctx context.Context, chunks []chunk, query string, maxResults int, maxSnippetLen int, embedder embedder) []SearchResult {
	if embedder != nil {
		results, err := rankHybridRemote(ctx, chunks, query, maxResults, maxSnippetLen, embedder)
		if err != nil {
			slog.Warn("memory search embeddings failed; falling back to local hybrid", "error", err)
		} else if len(results) > 0 {
			return results
		}
	}
	return rankHybridTFIDF(chunks, query, maxResults, maxSnippetLen)
}

func rankHybridTFIDF(chunks []chunk, query string, maxResults int, maxSnippetLen int) []SearchResult {
	lexical := rankLexical(chunks, query, len(chunks), maxSnippetLen)
	vector := rankVectorTFIDF(chunks, query, len(chunks), maxSnippetLen)
	score := map[string]float64{}
	snippet := map[string]string{}
	for _, entry := range lexical {
		key := entry.File + "::" + entry.Snippet
		score[key] += entry.Score
		snippet[key] = entry.Snippet
	}
	for _, entry := range vector {
		key := entry.File + "::" + entry.Snippet
		score[key] += entry.Score
		if _, ok := snippet[key]; !ok {
			snippet[key] = entry.Snippet
		}
	}
	results := make([]SearchResult, 0, len(score))
	for key, value := range score {
		parts := strings.SplitN(key, "::", 2)
		file := parts[0]
		text := ""
		if len(parts) > 1 {
			text = parts[1]
		}
		results = append(results, SearchResult{File: file, Snippet: text, Score: value})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].File < results[j].File
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

const (
	hybridVectorWeight  = 0.7
	hybridLexicalWeight = 0.3
)

func rankVectorRemote(ctx context.Context, chunks []chunk, query string, maxResults int, maxSnippetLen int, embedder embedder) ([]SearchResult, error) {
	if embedder == nil || len(chunks) == 0 {
		return nil, nil
	}
	inputs := make([]string, 0, len(chunks)+1)
	inputs = append(inputs, query)
	for _, chunk := range chunks {
		inputs = append(inputs, chunk.text)
	}
	vectors, err := embedder.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(inputs) {
		return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(vectors), len(inputs))
	}
	queryVec := vectors[0]
	results := make([]SearchResult, 0, len(chunks))
	for i, chunk := range chunks {
		score := cosineDense(queryVec, vectors[i+1])
		if score <= 0 {
			continue
		}
		snippet := clampSnippet(chunk.text, maxSnippetLen)
		results = append(results, SearchResult{File: chunk.file, Snippet: snippet, Matches: 0, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].File < results[j].File
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

func rankHybridRemote(ctx context.Context, chunks []chunk, query string, maxResults int, maxSnippetLen int, embedder embedder) ([]SearchResult, error) {
	if embedder == nil || len(chunks) == 0 {
		return nil, nil
	}
	inputs := make([]string, 0, len(chunks)+1)
	inputs = append(inputs, query)
	for _, chunk := range chunks {
		inputs = append(inputs, chunk.text)
	}
	vectors, err := embedder.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(inputs) {
		return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(vectors), len(inputs))
	}
	queryVec := vectors[0]
	needle := strings.ToLower(query)
	maxMatches := 0
	matches := make([]int, len(chunks))
	snippets := make([]string, len(chunks))
	for i, chunk := range chunks {
		count, snippet := findMatches(chunk.text, needle, maxSnippetLen)
		matches[i] = count
		if count > maxMatches {
			maxMatches = count
		}
		if count > 0 {
			snippets[i] = snippet
		} else {
			snippets[i] = clampSnippet(chunk.text, maxSnippetLen)
		}
	}

	results := make([]SearchResult, 0, len(chunks))
	for i, chunk := range chunks {
		vectorScore := cosineDense(queryVec, vectors[i+1])
		lexicalScore := 0.0
		if maxMatches > 0 {
			lexicalScore = float64(matches[i]) / float64(maxMatches)
		}
		score := hybridVectorWeight*vectorScore + hybridLexicalWeight*lexicalScore
		if score <= 0 {
			continue
		}
		results = append(results, SearchResult{
			File:    chunk.file,
			Snippet: snippets[i],
			Matches: matches[i],
			Score:   score,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].File < results[j].File
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

type tfidfIndex struct {
	df    map[string]int
	total int
}

func buildTFIDF(chunks []chunk) *tfidfIndex {
	df := map[string]int{}
	for _, chunk := range chunks {
		seen := map[string]struct{}{}
		for _, token := range chunk.tokens {
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			df[token]++
		}
	}
	return &tfidfIndex{df: df, total: len(chunks)}
}

func (t *tfidfIndex) Vectorize(tokens []string) map[string]float64 {
	tf := map[string]int{}
	for _, token := range tokens {
		tf[token]++
	}
	vec := map[string]float64{}
	for token, count := range tf {
		df := t.df[token]
		if df == 0 || t.total == 0 {
			continue
		}
		idf := 1.0 + math.Log(float64(t.total)/float64(df))
		vec[token] = float64(count) * idf
	}
	return vec
}

func cosine(a map[string]float64, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for k, v := range a {
		normA += v * v
		if bv, ok := b[k]; ok {
			dot += v * bv
		}
	}
	for _, v := range b {
		normB += v * v
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func cosineDense(a []float64, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot float64
	var normA float64
	var normB float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
	}
	for _, v := range a {
		normA += v * v
	}
	for _, v := range b {
		normB += v * v
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func clampSnippet(text string, maxSnippetLen int) string {
	text = strings.TrimSpace(text)
	if maxSnippetLen <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxSnippetLen {
		return text
	}
	return string(runes[:maxSnippetLen]) + "..."
}

func dedupe(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
