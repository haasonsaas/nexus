package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchTool_Success(t *testing.T) {
	htmlContent := `
<!DOCTYPE html>
<html>
<head><title>Fetch Test</title></head>
<body><main><p>Hello from fetch.</p></main></body>
</html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	tool := NewWebFetchTool(&FetchConfig{MaxChars: 500}, WithExtractor(NewContentExtractorForTesting()))
	params := map[string]interface{}{
		"url":         server.URL,
		"extractMode": "text",
	}
	raw, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "Hello from fetch") {
		t.Fatalf("expected content to include fetched text, got: %q", content)
	}
}

func TestWebFetchTool_Truncates(t *testing.T) {
	builder := strings.Repeat("A", 200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>" + builder + "</body></html>"))
	}))
	defer server.Close()

	tool := NewWebFetchTool(&FetchConfig{MaxChars: 50}, WithExtractor(NewContentExtractorForTesting()))
	params := map[string]interface{}{
		"url":      server.URL,
		"maxChars": 50,
	}
	raw, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if truncated, ok := payload["truncated"].(bool); !ok || !truncated {
		t.Fatalf("expected truncated=true, got %v", payload["truncated"])
	}
	content, _ := payload["content"].(string)
	if len(content) > 53 { // max + "..."
		t.Fatalf("expected content to be truncated, got len=%d", len(content))
	}
}

func TestWebFetchTool_SSRFBlocked(t *testing.T) {
	tool := NewWebFetchTool(nil)
	params := map[string]interface{}{
		"url": "http://localhost:1234",
	}
	raw, _ := json.Marshal(params)
	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected SSRF error, got success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "URL validation failed") {
		t.Fatalf("expected URL validation error, got: %s", result.Content)
	}
}
