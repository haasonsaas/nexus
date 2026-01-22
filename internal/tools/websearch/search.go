package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
)

// SearchBackend represents the type of search backend to use for web queries.
type SearchBackend string

const (
	BackendSearXNG     SearchBackend = "searxng"
	BackendDuckDuckGo  SearchBackend = "duckduckgo"
	BackendBraveSearch SearchBackend = "brave"

	// maxCacheSize limits the number of cached search responses to prevent unbounded memory growth
	maxCacheSize = 1000
)

// SearchType represents the type of search to perform (web, image, or news).
type SearchType string

const (
	SearchTypeWeb   SearchType = "web"
	SearchTypeImage SearchType = "image"
	SearchTypeNews  SearchType = "news"
)

// Config holds configuration for the web search tool including backend
// credentials, caching settings, and default behavior.
type Config struct {
	// SearXNG configuration
	SearXNGURL string `json:"searxng_url,omitempty"`

	// Brave Search API configuration
	BraveAPIKey string `json:"brave_api_key,omitempty"`

	// Default backend to use
	DefaultBackend SearchBackend `json:"default_backend"`

	// Whether to extract full content from URLs
	ExtractContent bool `json:"extract_content"`

	// Default number of results
	DefaultResultCount int `json:"default_result_count"`

	// Cache TTL in seconds
	CacheTTL int `json:"cache_ttl"`
}

// SearchParams represents the parameters for a search query including
// the query text, search type, result count, and optional content extraction.
type SearchParams struct {
	Query          string        `json:"query"`
	Type           SearchType    `json:"type,omitempty"`
	ResultCount    int           `json:"result_count,omitempty"`
	ExtractContent bool          `json:"extract_content,omitempty"`
	Backend        SearchBackend `json:"backend,omitempty"`
}

// SearchResult represents a single search result with title, URL, snippet,
// and optional full content or image URL depending on search type.
type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Content     string `json:"content,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

// SearchResponse represents the complete search response including
// the original query, results, and which backend was used.
type SearchResponse struct {
	Query       string         `json:"query"`
	Type        SearchType     `json:"type"`
	Results     []SearchResult `json:"results"`
	ResultCount int            `json:"result_count"`
	Backend     SearchBackend  `json:"backend"`
}

// cacheEntry holds a cached search result with expiration.
type cacheEntry struct {
	response  *SearchResponse
	expiresAt time.Time
}

// WebSearchTool implements the agent.Tool interface for web searching.
// It supports multiple backends (SearXNG, DuckDuckGo, Brave) with caching
// and optional full content extraction from result URLs.
type WebSearchTool struct {
	config     *Config
	httpClient *http.Client
	extractor  *ContentExtractor
	cache      map[string]*cacheEntry
	cacheMu    sync.RWMutex
}

// NewWebSearchTool creates a new web search tool with the given configuration.
// It applies default values and initializes the content extractor and cache.
func NewWebSearchTool(config *Config) *WebSearchTool {
	if config.DefaultResultCount == 0 {
		config.DefaultResultCount = 5
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 300 // 5 minutes default
	}
	if config.DefaultBackend == "" {
		if config.SearXNGURL != "" {
			config.DefaultBackend = BackendSearXNG
		} else {
			config.DefaultBackend = BackendDuckDuckGo
		}
	}

	return &WebSearchTool{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		extractor: NewContentExtractor(),
		cache:     make(map[string]*cacheEntry),
	}
}

// Name returns the tool name for registration with the agent runtime.
func (t *WebSearchTool) Name() string {
	return "web_search"
}

// Description returns the tool description.
func (t *WebSearchTool) Description() string {
	return "Search the web for information. Supports web search, image search, and news search. Can optionally extract full content from result URLs."
}

// Schema returns the JSON schema for tool parameters used by LLMs.
func (t *WebSearchTool) Schema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query",
			},
			"type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"web", "image", "news"},
				"description": "Type of search to perform (default: web)",
			},
			"result_count": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return (default: 5, max: 20)",
				"minimum":     1,
				"maximum":     20,
			},
			"extract_content": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to extract full content from result URLs (default: false)",
			},
			"backend": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"searxng", "duckduckgo", "brave"},
				"description": "Search backend to use (default: configured default)",
			},
		},
		"required": []string{"query"},
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return schemaBytes
}

// Execute runs the search with given parameters, checking cache first
// and falling back to DuckDuckGo if the primary backend fails.
func (t *WebSearchTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var searchParams SearchParams
	if err := json.Unmarshal(params, &searchParams); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Validate and set defaults
	if searchParams.Query == "" {
		return &agent.ToolResult{
			Content: "Query parameter is required",
			IsError: true,
		}, nil
	}

	if searchParams.Type == "" {
		searchParams.Type = SearchTypeWeb
	}

	if searchParams.ResultCount == 0 {
		searchParams.ResultCount = t.config.DefaultResultCount
	} else if searchParams.ResultCount > 20 {
		searchParams.ResultCount = 20
	}

	if searchParams.Backend == "" {
		searchParams.Backend = t.config.DefaultBackend
	}

	if !searchParams.ExtractContent {
		searchParams.ExtractContent = t.config.ExtractContent
	}

	// Check cache
	cacheKey := t.getCacheKey(&searchParams)
	if cached := t.getFromCache(cacheKey); cached != nil {
		return t.formatResponse(cached), nil
	}

	// Perform search
	var response *SearchResponse
	var err error

	switch searchParams.Backend {
	case BackendSearXNG:
		response, err = t.searchSearXNG(ctx, &searchParams)
	case BackendDuckDuckGo:
		response, err = t.searchDuckDuckGo(ctx, &searchParams)
	case BackendBraveSearch:
		response, err = t.searchBrave(ctx, &searchParams)
	default:
		return &agent.ToolResult{
			Content: fmt.Sprintf("Unknown backend: %s", searchParams.Backend),
			IsError: true,
		}, nil
	}

	if err != nil {
		// Try fallback to DuckDuckGo if primary backend fails
		if searchParams.Backend != BackendDuckDuckGo {
			response, err = t.searchDuckDuckGo(ctx, &searchParams)
			if err != nil {
				return &agent.ToolResult{
					Content: fmt.Sprintf("Search failed: %v", err),
					IsError: true,
				}, nil
			}
			response.Backend = BackendDuckDuckGo
		} else {
			return &agent.ToolResult{
				Content: fmt.Sprintf("Search failed: %v", err),
				IsError: true,
			}, nil
		}
	}

	// Extract content if requested
	if searchParams.ExtractContent && searchParams.Type == SearchTypeWeb {
		t.extractContentForResults(ctx, response)
	}

	// Cache the response
	t.putInCache(cacheKey, response)

	return t.formatResponse(response), nil
}

// formatResponse converts a SearchResponse to a ToolResult.
func (t *WebSearchTool) formatResponse(response *SearchResponse) *agent.ToolResult {
	output, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to format response: %v", err),
			IsError: true,
		}
	}

	return &agent.ToolResult{
		Content: string(output),
		IsError: false,
	}
}

// getCacheKey generates a cache key from search parameters.
func (t *WebSearchTool) getCacheKey(params *SearchParams) string {
	return fmt.Sprintf("%s:%s:%d:%v:%s",
		params.Backend,
		params.Type,
		params.ResultCount,
		params.ExtractContent,
		params.Query,
	)
}

// getFromCache retrieves a cached response if it exists and hasn't expired.
func (t *WebSearchTool) getFromCache(key string) *SearchResponse {
	t.cacheMu.RLock()
	defer t.cacheMu.RUnlock()

	entry, exists := t.cache[key]
	if !exists {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		// Entry has expired
		return nil
	}

	return entry.response
}

// putInCache stores a response in the cache with TTL.
func (t *WebSearchTool) putInCache(key string, response *SearchResponse) {
	t.cacheMu.Lock()
	defer t.cacheMu.Unlock()

	now := time.Now()

	// Clean up expired entries first
	for k, v := range t.cache {
		if now.After(v.expiresAt) {
			delete(t.cache, k)
		}
	}

	// If still at capacity after cleanup, evict oldest entries
	for len(t.cache) >= maxCacheSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range t.cache {
			if oldestKey == "" || v.expiresAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.expiresAt
			}
		}
		if oldestKey != "" {
			delete(t.cache, oldestKey)
		} else {
			break
		}
	}

	t.cache[key] = &cacheEntry{
		response:  response,
		expiresAt: now.Add(time.Duration(t.config.CacheTTL) * time.Second),
	}
}

// extractContentForResults extracts full content for search results in parallel.
func (t *WebSearchTool) extractContentForResults(ctx context.Context, response *SearchResponse) {
	var wg sync.WaitGroup
	for i := range response.Results {
		wg.Add(1)
		go func(result *SearchResult) {
			defer wg.Done()
			content, err := t.extractor.Extract(ctx, result.URL)
			if err == nil && content != "" {
				result.Content = content
			}
		}(&response.Results[i])
	}
	wg.Wait()
}

// searchSearXNG performs a search using SearXNG.
func (t *WebSearchTool) searchSearXNG(ctx context.Context, params *SearchParams) (*SearchResponse, error) {
	if t.config.SearXNGURL == "" {
		return nil, fmt.Errorf("SearXNG URL not configured")
	}

	// Build request URL
	searchURL, err := url.Parse(t.config.SearXNGURL)
	if err != nil {
		return nil, fmt.Errorf("invalid SearXNG URL: %w", err)
	}

	query := url.Values{}
	query.Set("q", params.Query)
	query.Set("format", "json")
	query.Set("pageno", "1")

	// Set category based on search type
	switch params.Type {
	case SearchTypeImage:
		query.Set("categories", "images")
	case SearchTypeNews:
		query.Set("categories", "news")
	default:
		query.Set("categories", "general")
	}

	searchURL.Path = "/search"
	searchURL.RawQuery = query.Encode()

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SearXNG returned status %d", resp.StatusCode)
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var searxngResp struct {
		Query   string `json:"query"`
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			ImgSrc        string `json:"img_src,omitempty"`
			PublishedDate string `json:"publishedDate,omitempty"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &searxngResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to our format
	results := make([]SearchResult, 0, params.ResultCount)
	for i := 0; i < len(searxngResp.Results) && i < params.ResultCount; i++ {
		r := searxngResp.Results[i]
		result := SearchResult{
			Title:       r.Title,
			URL:         r.URL,
			Snippet:     r.Content,
			ImageURL:    r.ImgSrc,
			PublishedAt: r.PublishedDate,
		}
		results = append(results, result)
	}

	return &SearchResponse{
		Query:       params.Query,
		Type:        params.Type,
		Results:     results,
		ResultCount: len(results),
		Backend:     BackendSearXNG,
	}, nil
}

// searchDuckDuckGo performs a search using DuckDuckGo's Instant Answer API.
func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, params *SearchParams) (*SearchResponse, error) {
	// Use the DuckDuckGo Instant Answer API for reliable structured results
	instantURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1", url.QueryEscape(params.Query))
	req, err := http.NewRequestWithContext(ctx, "GET", instantURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NexusBot/1.0)")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var ddgResp struct {
		Abstract       string `json:"Abstract"`
		AbstractText   string `json:"AbstractText"`
		AbstractSource string `json:"AbstractSource"`
		AbstractURL    string `json:"AbstractURL"`
		Heading        string `json:"Heading"`
		RelatedTopics  []struct {
			FirstURL string `json:"FirstURL"`
			Text     string `json:"Text"`
		} `json:"RelatedTopics"`
	}

	if err := json.Unmarshal(body, &ddgResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to our format
	results := make([]SearchResult, 0)

	// Add abstract as first result if available
	if ddgResp.AbstractText != "" && ddgResp.AbstractURL != "" {
		results = append(results, SearchResult{
			Title:   ddgResp.Heading,
			URL:     ddgResp.AbstractURL,
			Snippet: ddgResp.AbstractText,
		})
	}

	// Add related topics
	for i := 0; i < len(ddgResp.RelatedTopics) && len(results) < params.ResultCount; i++ {
		topic := ddgResp.RelatedTopics[i]
		if topic.FirstURL != "" && topic.Text != "" {
			results = append(results, SearchResult{
				Title:   topic.Text[:min(len(topic.Text), 100)],
				URL:     topic.FirstURL,
				Snippet: topic.Text,
			})
		}
	}

	return &SearchResponse{
		Query:       params.Query,
		Type:        params.Type,
		Results:     results,
		ResultCount: len(results),
		Backend:     BackendDuckDuckGo,
	}, nil
}

// searchBrave performs a search using the Brave Search API.
func (t *WebSearchTool) searchBrave(ctx context.Context, params *SearchParams) (*SearchResponse, error) {
	if t.config.BraveAPIKey == "" {
		return nil, fmt.Errorf("Brave API key not configured")
	}

	// Build request URL
	baseURL := "https://api.search.brave.com/res/v1"
	var endpoint string

	switch params.Type {
	case SearchTypeImage:
		endpoint = "/images/search"
	case SearchTypeNews:
		endpoint = "/news/search"
	default:
		endpoint = "/web/search"
	}

	searchURL, err := url.Parse(baseURL + endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	query := url.Values{}
	query.Set("q", params.Query)
	query.Set("count", fmt.Sprintf("%d", params.ResultCount))
	searchURL.RawQuery = query.Encode()

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.config.BraveAPIKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("Brave API returned status %d and failed to read body: %w", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("Brave API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response based on type
	results := make([]SearchResult, 0)

	switch params.Type {
	case SearchTypeImage:
		var braveResp struct {
			Results []struct {
				Title     string `json:"title"`
				URL       string `json:"url"`
				Thumbnail struct {
					Src string `json:"src"`
				} `json:"thumbnail"`
				Properties struct {
					URL string `json:"url"`
				} `json:"properties"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &braveResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		for _, r := range braveResp.Results {
			results = append(results, SearchResult{
				Title:    r.Title,
				URL:      r.Properties.URL,
				ImageURL: r.Thumbnail.Src,
			})
		}

	case SearchTypeNews:
		var braveResp struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &braveResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		for _, r := range braveResp.Results {
			results = append(results, SearchResult{
				Title:       r.Title,
				URL:         r.URL,
				Snippet:     r.Description,
				PublishedAt: r.Age,
			})
		}

	default:
		var braveResp struct {
			Web struct {
				Results []struct {
					Title       string `json:"title"`
					URL         string `json:"url"`
					Description string `json:"description"`
				} `json:"results"`
			} `json:"web"`
		}
		if err := json.Unmarshal(body, &braveResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		for _, r := range braveResp.Web.Results {
			results = append(results, SearchResult{
				Title:   r.Title,
				URL:     r.URL,
				Snippet: r.Description,
			})
		}
	}

	return &SearchResponse{
		Query:       params.Query,
		Type:        params.Type,
		Results:     results,
		ResultCount: len(results),
		Backend:     BackendBraveSearch,
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
