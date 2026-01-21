# Web Search Tool Integration Guide

## Quick Start

To integrate the web search tool into your Nexus agent:

### 1. Import the Package

```go
import (
    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/tools/websearch"
)
```

### 2. Create and Configure the Tool

```go
// Create configuration
searchConfig := &websearch.Config{
    // Option 1: Use SearXNG (self-hosted, recommended)
    SearXNGURL:         os.Getenv("SEARXNG_URL"),
    DefaultBackend:     websearch.BackendSearXNG,

    // Option 2: Use Brave Search API (requires API key)
    // BraveAPIKey:     os.Getenv("BRAVE_API_KEY"),
    // DefaultBackend:  websearch.BackendBraveSearch,

    // Option 3: Use DuckDuckGo (fallback, no config needed)
    // DefaultBackend:  websearch.BackendDuckDuckGo,

    // Default settings
    ExtractContent:     false,  // Set to true to extract full content by default
    DefaultResultCount: 5,      // Default number of results
    CacheTTL:          300,     // Cache for 5 minutes
}

// Create the tool
searchTool := websearch.NewWebSearchTool(searchConfig)
```

### 3. Register with Agent Runtime

```go
// Create your agent runtime
runtime := agent.NewRuntime(llmProvider, sessionStore)

// Register the web search tool
runtime.RegisterTool(searchTool)
```

### 4. Done!

The agent can now call the `web_search` tool through function calling:

```json
{
  "name": "web_search",
  "input": {
    "query": "latest golang features",
    "result_count": 5,
    "type": "web"
  }
}
```

## Environment Variables

Set these environment variables for your deployment:

```bash
# For SearXNG backend
export SEARXNG_URL=https://searxng.example.com

# For Brave Search API backend
export BRAVE_API_KEY=your-brave-api-key-here
```

## Tool Schema

The LLM receives this schema for the `web_search` tool:

```json
{
  "name": "web_search",
  "description": "Search the web for information. Supports web search, image search, and news search. Can optionally extract full content from result URLs.",
  "input_schema": {
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
}
```

## Response Format

The tool returns JSON-formatted results:

```json
{
  "query": "golang best practices",
  "type": "web",
  "results": [
    {
      "title": "Effective Go",
      "url": "https://golang.org/doc/effective_go",
      "snippet": "A comprehensive guide to writing clear, idiomatic Go code...",
      "content": "Full extracted content (if extract_content is true)..."
    }
  ],
  "result_count": 5,
  "backend": "searxng"
}
```

## Example Agent System Prompt

Include this in your agent's system prompt to help it use the tool effectively:

```
You have access to a web_search tool that can search the internet for current information.

Use it when:
- User asks for recent information beyond your training data
- User requests specific facts that require verification
- User wants to find resources, documentation, or articles
- User needs image or news search results

Example usage:
- To find general information: {"query": "climate change statistics", "type": "web"}
- To search for images: {"query": "sunset photos", "type": "image"}
- To find news: {"query": "technology news", "type": "news"}
- To get full content: {"query": "tutorial", "extract_content": true}

The tool will return structured results with titles, URLs, and snippets.
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/sessions"
    "github.com/haasonsaas/nexus/internal/tools/websearch"
    "github.com/haasonsaas/nexus/pkg/models"
)

func main() {
    // Create session store
    sessionStore := sessions.NewMemoryStore()

    // Create LLM provider (implement your provider)
    llmProvider := createLLMProvider()

    // Create agent runtime
    runtime := agent.NewRuntime(llmProvider, sessionStore)

    // Configure and register web search tool
    searchConfig := &websearch.Config{
        SearXNGURL:         os.Getenv("SEARXNG_URL"),
        BraveAPIKey:        os.Getenv("BRAVE_API_KEY"),
        DefaultBackend:     websearch.BackendSearXNG,
        ExtractContent:     false,
        DefaultResultCount: 5,
        CacheTTL:          300,
    }
    searchTool := websearch.NewWebSearchTool(searchConfig)
    runtime.RegisterTool(searchTool)

    // Create or get session
    session := &models.Session{
        ID:      "session-123",
        AgentID: "agent-456",
    }

    // Process user message
    userMessage := &models.Message{
        Role:    models.RoleUser,
        Content: "What are the latest developments in AI?",
    }

    // Get streaming response
    responses, err := runtime.Process(context.Background(), session, userMessage)
    if err != nil {
        log.Fatal(err)
    }

    // Handle responses
    for chunk := range responses {
        if chunk.Error != nil {
            log.Printf("Error: %v", chunk.Error)
            continue
        }

        if chunk.Text != "" {
            // Stream text to user
            print(chunk.Text)
        }

        if chunk.ToolResult != nil {
            // Tool execution completed
            log.Printf("Tool result: %s", chunk.ToolResult.Content)
        }
    }
}

func createLLMProvider() agent.LLMProvider {
    // Implement your LLM provider (Anthropic, OpenAI, etc.)
    panic("implement your LLM provider")
}
```

## Testing Your Integration

### Unit Test

```go
func TestWebSearchToolIntegration(t *testing.T) {
    // Create tool with test configuration
    config := &websearch.Config{
        DefaultBackend:     websearch.BackendDuckDuckGo,
        DefaultResultCount: 3,
        CacheTTL:          300,
    }
    tool := websearch.NewWebSearchTool(config)

    // Verify interface compliance
    var _ agent.Tool = tool

    // Test basic search
    params := websearch.SearchParams{
        Query:       "test query",
        ResultCount: 3,
    }
    paramsJSON, _ := json.Marshal(params)

    result, err := tool.Execute(context.Background(), paramsJSON)
    if err != nil {
        t.Fatalf("Execute failed: %v", err)
    }

    if result.IsError {
        t.Errorf("Search failed: %s", result.Content)
    }

    // Verify result format
    var response websearch.SearchResponse
    if err := json.Unmarshal([]byte(result.Content), &response); err != nil {
        t.Fatalf("Invalid response format: %v", err)
    }

    t.Logf("Found %d results for query: %s", response.ResultCount, response.Query)
}
```

### Integration Test

```go
func TestAgentWithWebSearch(t *testing.T) {
    // Setup
    sessionStore := sessions.NewMemoryStore()
    llmProvider := &mockLLMProvider{}
    runtime := agent.NewRuntime(llmProvider, sessionStore)

    // Register web search tool
    searchConfig := &websearch.Config{
        DefaultBackend: websearch.BackendDuckDuckGo,
    }
    searchTool := websearch.NewWebSearchTool(searchConfig)
    runtime.RegisterTool(searchTool)

    // Create session
    session := &models.Session{ID: "test-session"}

    // Send message
    message := &models.Message{
        Role:    models.RoleUser,
        Content: "Search for golang tutorials",
    }

    // Process and verify tool is available
    responses, err := runtime.Process(context.Background(), session, message)
    if err != nil {
        t.Fatalf("Process failed: %v", err)
    }

    // Consume responses
    for range responses {
        // Process responses
    }
}
```

## Troubleshooting

### Tool Not Available to LLM

**Problem**: The LLM doesn't see the web_search tool.

**Solution**: Ensure the tool is registered before processing messages:
```go
runtime.RegisterTool(searchTool)
```

### Search Returns No Results

**Problem**: Searches complete but return empty results.

**Solutions**:
1. Check backend configuration (URL for SearXNG, API key for Brave)
2. Verify network connectivity to the search backend
3. Try fallback to DuckDuckGo: `DefaultBackend: websearch.BackendDuckDuckGo`
4. Check backend-specific logs/errors

### Content Extraction Fails

**Problem**: `extract_content: true` but no content is returned.

**Solutions**:
1. Verify the URL returns HTML content (not PDF, images, etc.)
2. Check that the site doesn't block bots (User-Agent)
3. Some sites require JavaScript - content extraction won't work
4. Check network connectivity and timeouts

### Cache Not Working

**Problem**: Same queries hit the backend every time.

**Solutions**:
1. Verify `CacheTTL` is set > 0
2. Cache is in-memory - lost on restart
3. Cache key includes all parameters - different params = cache miss
4. Consider implementing persistent caching with Redis

### Rate Limiting Issues

**Problem**: Getting rate limited by search backends.

**Solutions**:
1. Increase `CacheTTL` to reduce backend requests
2. For Brave API: Check your plan's rate limits
3. For SearXNG: Configure your instance's rate limits
4. Implement request throttling in your application

## Production Recommendations

1. **Use SearXNG**: Self-host for best control and privacy
2. **Enable Caching**: Set CacheTTL to at least 300 seconds
3. **Content Extraction**: Only enable when needed (adds latency)
4. **Monitoring**: Log tool execution times and error rates
5. **Fallback**: Always configure DuckDuckGo as fallback
6. **API Keys**: Store securely in environment variables or secrets manager
7. **Rate Limiting**: Implement application-level rate limiting
8. **Persistent Cache**: Consider Redis for production caching

## Next Steps

1. Implement your LLM provider
2. Configure your preferred search backend
3. Test the integration with your agent
4. Deploy and monitor tool usage
5. Adjust configuration based on usage patterns

For more details, see the [README.md](./README.md).
