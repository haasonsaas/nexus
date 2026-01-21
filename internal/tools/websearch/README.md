# Web Search Tool

A comprehensive web search tool for the Nexus agent framework that supports multiple search backends with content extraction capabilities.

## Features

- **Multiple Search Backends**:
  - SearXNG (self-hosted, privacy-focused)
  - DuckDuckGo (no API key required)
  - Brave Search API (requires API key)

- **Search Types**:
  - Web search
  - Image search
  - News search

- **Content Extraction**:
  - Automatic extraction of readable content from URLs
  - Readability-based algorithm
  - Removes navigation, ads, scripts, and styles
  - Preserves main article content

- **Performance**:
  - Result caching with configurable TTL
  - Concurrent content extraction
  - Automatic fallback to DuckDuckGo on failures

## Installation

The tool is part of the Nexus internal tools package:

```go
import "github.com/haasonsaas/nexus/internal/tools/websearch"
```

## Configuration

```go
config := &websearch.Config{
    // SearXNG configuration (optional)
    SearXNGURL: "https://searxng.example.com",

    // Brave Search API configuration (optional)
    BraveAPIKey: "your-brave-api-key",

    // Default backend to use
    DefaultBackend: websearch.BackendSearXNG,

    // Whether to extract full content from URLs by default
    ExtractContent: false,

    // Default number of results (default: 5)
    DefaultResultCount: 5,

    // Cache TTL in seconds (default: 300)
    CacheTTL: 300,
}

tool := websearch.NewWebSearchTool(config)
```

## Usage

### Basic Search

```go
params := websearch.SearchParams{
    Query:       "golang best practices",
    ResultCount: 5,
}

paramsJSON, _ := json.Marshal(params)
result, err := tool.Execute(context.Background(), paramsJSON)
if err != nil {
    log.Fatal(err)
}

var response websearch.SearchResponse
json.Unmarshal([]byte(result.Content), &response)

for _, result := range response.Results {
    fmt.Printf("Title: %s\n", result.Title)
    fmt.Printf("URL: %s\n", result.URL)
    fmt.Printf("Snippet: %s\n\n", result.Snippet)
}
```

### Search with Content Extraction

```go
params := websearch.SearchParams{
    Query:          "machine learning tutorials",
    ResultCount:    3,
    ExtractContent: true,  // Extract full content from URLs
}

paramsJSON, _ := json.Marshal(params)
result, err := tool.Execute(context.Background(), paramsJSON)

// Results will include full content in the Content field
```

### Image Search

```go
params := websearch.SearchParams{
    Query:       "sunset photography",
    Type:        websearch.SearchTypeImage,
    ResultCount: 10,
}

paramsJSON, _ := json.Marshal(params)
result, err := tool.Execute(context.Background(), paramsJSON)
```

### News Search

```go
params := websearch.SearchParams{
    Query:       "technology news",
    Type:        websearch.SearchTypeNews,
    ResultCount: 5,
    Backend:     websearch.BackendBraveSearch,
}

paramsJSON, _ := json.Marshal(params)
result, err := tool.Execute(context.Background(), paramsJSON)
```

## Integration with Agent Runtime

```go
// Create the agent runtime
runtime := agent.NewRuntime(provider, sessionStore)

// Create and register the web search tool
searchConfig := &websearch.Config{
    SearXNGURL:         os.Getenv("SEARXNG_URL"),
    BraveAPIKey:        os.Getenv("BRAVE_API_KEY"),
    DefaultBackend:     websearch.BackendSearXNG,
    DefaultResultCount: 5,
    CacheTTL:          300,
}
searchTool := websearch.NewWebSearchTool(searchConfig)
runtime.RegisterTool(searchTool)

// The LLM can now call the web_search tool
```

## Search Backends

### SearXNG (Recommended for Self-Hosting)

SearXNG is a privacy-focused metasearch engine that aggregates results from multiple sources.

**Pros**:
- Privacy-focused (no tracking)
- Aggregates results from multiple search engines
- Self-hostable
- Free to use

**Cons**:
- Requires self-hosting or finding a public instance
- Quality depends on configured search engines

**Configuration**:
```go
config := &websearch.Config{
    SearXNGURL:     "https://searxng.example.com",
    DefaultBackend: websearch.BackendSearXNG,
}
```

### DuckDuckGo (Default Fallback)

Uses DuckDuckGo's Instant Answer API for searches.

**Pros**:
- No API key required
- No rate limiting (reasonable use)
- Privacy-focused

**Cons**:
- Limited result format
- Primarily returns instant answers and related topics
- Not suitable for comprehensive searches

**Configuration**:
```go
config := &websearch.Config{
    DefaultBackend: websearch.BackendDuckDuckGo,
}
```

### Brave Search API

Official Brave Search API with comprehensive results.

**Pros**:
- High-quality results
- Comprehensive API
- Good rate limits
- Supports web, image, and news search

**Cons**:
- Requires API key (paid service)
- Rate limits apply based on plan

**Configuration**:
```go
config := &websearch.Config{
    BraveAPIKey:    "your-api-key",
    DefaultBackend: websearch.BackendBraveSearch,
}
```

## Content Extraction

The content extractor uses a readability-based algorithm to extract clean, readable content from web pages.

**Features**:
- Removes navigation, headers, footers, and sidebars
- Extracts page title and meta description
- Identifies main content containers (`<main>`, `<article>`, etc.)
- Cleans HTML and preserves paragraph structure
- Handles HTML entities
- Limits content to 10KB to prevent excessive data

**Direct Usage**:
```go
extractor := websearch.NewContentExtractor()
content, err := extractor.Extract(context.Background(), "https://example.com/article")
if err != nil {
    log.Fatal(err)
}
fmt.Println(content)
```

**Batch Extraction**:
```go
extractor := websearch.NewContentExtractor()
urls := []string{
    "https://example.com/page1",
    "https://example.com/page2",
    "https://example.com/page3",
}
results := extractor.ExtractBatch(context.Background(), urls)

for url, content := range results {
    fmt.Printf("Content from %s:\n%s\n\n", url, content)
}
```

## Caching

The tool includes built-in caching with configurable TTL:

- Cache key includes: backend, search type, result count, extract content flag, and query
- Expired entries are automatically cleaned up
- Cache is in-memory (lost on restart)
- Default TTL: 300 seconds (5 minutes)

**Note**: For production use, consider implementing persistent caching with Redis or similar.

## Error Handling

The tool includes automatic fallback:

1. Attempts search with configured backend
2. If primary backend fails, automatically falls back to DuckDuckGo
3. Returns structured error in ToolResult if all backends fail

```go
result, err := tool.Execute(ctx, paramsJSON)
if err != nil {
    // Tool execution error (programming error)
    log.Fatal(err)
}

if result.IsError {
    // Search failed (network error, API error, etc.)
    fmt.Printf("Search error: %s\n", result.Content)
}
```

## Testing

The package includes comprehensive tests:

```bash
# Run all tests
go test ./internal/tools/websearch/

# Run with coverage
go test -cover ./internal/tools/websearch/

# Run specific tests
go test -run TestWebSearchTool_Execute_SearXNG ./internal/tools/websearch/
```

## Schema for LLM Function Calling

The tool provides a JSON schema for LLM function calling:

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query"
    },
    "type": {
      "type": "string",
      "enum": ["web", "image", "news"],
      "description": "Type of search to perform (default: web)"
    },
    "result_count": {
      "type": "integer",
      "description": "Number of results to return (default: 5, max: 20)",
      "minimum": 1,
      "maximum": 20
    },
    "extract_content": {
      "type": "boolean",
      "description": "Whether to extract full content from result URLs (default: false)"
    },
    "backend": {
      "type": "string",
      "enum": ["searxng", "duckduckgo", "brave"],
      "description": "Search backend to use (default: configured default)"
    }
  },
  "required": ["query"]
}
```

## Environment Variables

For production deployments:

```bash
# SearXNG instance URL
export SEARXNG_URL=https://searxng.example.com

# Brave Search API key
export BRAVE_API_KEY=your-api-key-here
```

## Performance Considerations

- **Content Extraction**: Extracting content from URLs adds latency (~1-5 seconds per URL). Only enable when necessary.
- **Result Count**: More results = longer processing time, especially with content extraction
- **Caching**: Significantly improves performance for repeated queries
- **Concurrent Extraction**: Content is extracted in parallel when enabled

## Limitations

- Content extraction requires HTML content (PDFs, images not supported)
- Content extraction may fail on heavily JavaScript-dependent sites
- Rate limits apply for Brave Search API
- SearXNG quality depends on configured search engines
- DuckDuckGo returns limited results compared to other backends

## Future Enhancements

- Persistent caching with Redis
- Support for search result filtering and ranking
- Support for PDF content extraction
- Configurable user agents
- Rate limiting and retry logic
- Search result deduplication
- Support for search operators and advanced queries
