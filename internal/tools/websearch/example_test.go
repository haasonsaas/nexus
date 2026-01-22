package websearch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/haasonsaas/nexus/internal/tools/websearch"
)

// Example demonstrates basic web search usage
func Example_basicSearch() {
	// Create the web search tool with default configuration
	config := &websearch.Config{
		DefaultBackend:     websearch.BackendDuckDuckGo,
		DefaultResultCount: 5,
		CacheTTL:           300,
	}
	tool := websearch.NewWebSearchTool(config)

	// Prepare search parameters
	params := websearch.SearchParams{
		Query:       "golang programming",
		ResultCount: 3,
	}

	// Execute search
	paramsJSON, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	if result.IsError {
		log.Printf("Search failed: %s", result.Content)
		return
	}

	// Parse response
	var response websearch.SearchResponse
	if err := json.Unmarshal([]byte(result.Content), &response); err != nil {
		log.Fatal(err)
	}

	// Display results
	fmt.Printf("Query: %s\n", response.Query)
	fmt.Printf("Backend: %s\n", response.Backend)
	fmt.Printf("Results: %d\n\n", response.ResultCount)

	for i, result := range response.Results {
		fmt.Printf("%d. %s\n", i+1, result.Title)
		fmt.Printf("   URL: %s\n", result.URL)
		if result.Snippet != "" {
			fmt.Printf("   %s\n", result.Snippet)
		}
		fmt.Println()
	}
}

// Example demonstrates web search with content extraction
func Example_withContentExtraction() {
	config := &websearch.Config{
		DefaultBackend:     websearch.BackendDuckDuckGo,
		ExtractContent:     true, // Enable content extraction
		DefaultResultCount: 2,
	}
	tool := websearch.NewWebSearchTool(config)

	params := websearch.SearchParams{
		Query:          "machine learning tutorial",
		ResultCount:    2,
		ExtractContent: true,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	var response websearch.SearchResponse
	_ = json.Unmarshal([]byte(result.Content), &response)

	for _, result := range response.Results {
		fmt.Printf("Title: %s\n", result.Title)
		fmt.Printf("URL: %s\n", result.URL)
		if result.Content != "" {
			fmt.Printf("Content Preview: %s...\n", result.Content[:min(200, len(result.Content))])
		}
		fmt.Println()
	}
}

// Example demonstrates direct content extraction from URLs
func Example_contentExtraction() {
	extractor := websearch.NewContentExtractor()

	// Extract content from a single URL
	content, err := extractor.Extract(
		context.Background(),
		"https://example.com/article",
	)
	if err != nil {
		log.Printf("Failed to extract content: %v", err)
		return
	}

	fmt.Printf("Extracted content:\n%s\n", content)
}

// Example demonstrates batch content extraction
func Example_batchExtraction() {
	extractor := websearch.NewContentExtractor()

	urls := []string{
		"https://example.com/article1",
		"https://example.com/article2",
		"https://example.com/article3",
	}

	results := extractor.ExtractBatch(context.Background(), urls)

	for url, content := range results {
		fmt.Printf("Content from %s:\n", url)
		fmt.Printf("%s\n\n", content[:min(200, len(content))])
	}
}

// Example demonstrates image search
func Example_imageSearch() {
	config := &websearch.Config{
		DefaultBackend: websearch.BackendDuckDuckGo,
	}
	tool := websearch.NewWebSearchTool(config)

	params := websearch.SearchParams{
		Query:       "golang gopher mascot",
		Type:        websearch.SearchTypeImage,
		ResultCount: 5,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	var response websearch.SearchResponse
	_ = json.Unmarshal([]byte(result.Content), &response)

	for i, result := range response.Results {
		fmt.Printf("%d. %s\n", i+1, result.Title)
		fmt.Printf("   Image: %s\n", result.ImageURL)
		fmt.Printf("   Source: %s\n\n", result.URL)
	}
}

// Example demonstrates news search
func Example_newsSearch() {
	config := &websearch.Config{
		DefaultBackend: websearch.BackendDuckDuckGo,
	}
	tool := websearch.NewWebSearchTool(config)

	params := websearch.SearchParams{
		Query:       "technology news",
		Type:        websearch.SearchTypeNews,
		ResultCount: 5,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	var response websearch.SearchResponse
	_ = json.Unmarshal([]byte(result.Content), &response)

	for i, result := range response.Results {
		fmt.Printf("%d. %s\n", i+1, result.Title)
		if result.PublishedAt != "" {
			fmt.Printf("   Published: %s\n", result.PublishedAt)
		}
		fmt.Printf("   %s\n", result.Snippet)
		fmt.Printf("   %s\n\n", result.URL)
	}
}

// Example demonstrates using SearXNG backend
func Example_searxngBackend() {
	config := &websearch.Config{
		SearXNGURL:     "https://searxng.example.com",
		DefaultBackend: websearch.BackendSearXNG,
	}
	tool := websearch.NewWebSearchTool(config)

	params := websearch.SearchParams{
		Query:       "privacy-focused search",
		ResultCount: 5,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	var response websearch.SearchResponse
	_ = json.Unmarshal([]byte(result.Content), &response)

	fmt.Printf("Using backend: %s\n", response.Backend)
	fmt.Printf("Found %d results\n", response.ResultCount)
}

// Example demonstrates Brave Search API
func Example_braveBackend() {
	config := &websearch.Config{
		BraveAPIKey:    "your-api-key-here",
		DefaultBackend: websearch.BackendBraveSearch,
	}
	tool := websearch.NewWebSearchTool(config)

	params := websearch.SearchParams{
		Query:       "artificial intelligence",
		ResultCount: 10,
		Backend:     websearch.BackendBraveSearch,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	var response websearch.SearchResponse
	_ = json.Unmarshal([]byte(result.Content), &response)

	for _, result := range response.Results {
		fmt.Printf("Title: %s\n", result.Title)
		fmt.Printf("URL: %s\n", result.URL)
		fmt.Printf("Snippet: %s\n\n", result.Snippet)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
