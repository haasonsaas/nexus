package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var playwrightCheck struct {
	once sync.Once
	err  error
}

func requirePlaywright(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping browser integration tests in short mode")
	}
	playwrightCheck.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool, err := NewPool(PoolConfig{
			MaxInstances: 1,
			Timeout:      10 * time.Second,
			Headless:     true,
		})
		if err != nil {
			playwrightCheck.err = err
			return
		}
		defer pool.Close()

		instance, err := pool.Acquire(ctx)
		if err != nil {
			playwrightCheck.err = err
			return
		}
		pool.Release(instance)
	})

	if playwrightCheck.err != nil {
		t.Skipf("Playwright not available: %v", playwrightCheck.err)
	}
}

// TestBrowserTool_Name tests the Name method
func TestBrowserTool_Name(t *testing.T) {
	tool := NewBrowserTool(nil)
	if tool.Name() != "browser" {
		t.Errorf("expected name 'browser', got %s", tool.Name())
	}
}

// TestBrowserTool_Description tests the Description method
func TestBrowserTool_Description(t *testing.T) {
	tool := NewBrowserTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
	if !strings.Contains(desc, "browser") {
		t.Errorf("description should mention browser, got: %s", desc)
	}
}

// TestBrowserTool_Schema tests the Schema method
func TestBrowserTool_Schema(t *testing.T) {
	tool := NewBrowserTool(nil)
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Error("schema should not be empty")
	}

	// Verify it's valid JSON
	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Errorf("schema should be valid JSON: %v", err)
	}

	// Check for required fields
	if _, ok := schemaMap["type"]; !ok {
		t.Error("schema should have 'type' field")
	}
	if _, ok := schemaMap["properties"]; !ok {
		t.Error("schema should have 'properties' field")
	}
}

// TestBrowserTool_Navigate tests navigation functionality
func TestBrowserTool_Navigate(t *testing.T) {
	requirePlaywright(t)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<head><title>Test Page</title></head>
			<body>
				<h1>Welcome</h1>
				<p>This is a test page</p>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	params := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	paramsJSON, _ := json.Marshal(params)

	ctx := context.Background()
	result, err := tool.Execute(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	if !strings.Contains(result.Content, "navigated") && !strings.Contains(result.Content, "success") {
		t.Errorf("expected success message, got: %s", result.Content)
	}
}

// TestBrowserTool_Click tests click functionality
func TestBrowserTool_Click(t *testing.T) {
	requirePlaywright(t)

	// Create test server with clickable button
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body>
				<button id="test-button" onclick="this.innerText='Clicked!'">Click Me</button>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// First navigate
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Then click
	clickParams := ClickParams{
		Action:   "click",
		Selector: "#test-button",
	}
	clickJSON, _ := json.Marshal(clickParams)
	result, err := tool.Execute(ctx, clickJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
}

// TestBrowserTool_Type tests typing/filling forms
func TestBrowserTool_Type(t *testing.T) {
	requirePlaywright(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body>
				<input id="username" type="text" />
				<input id="password" type="password" />
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// Navigate first
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Type text
	typeParams := TypeParams{
		Action:   "type",
		Selector: "#username",
		Text:     "testuser",
	}
	typeJSON, _ := json.Marshal(typeParams)
	result, err := tool.Execute(ctx, typeJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
}

// TestBrowserTool_Screenshot tests screenshot capture
func TestBrowserTool_Screenshot(t *testing.T) {
	requirePlaywright(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body><h1>Screenshot Test</h1></body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// Navigate first
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Take screenshot
	screenshotParams := ScreenshotParams{
		Action: "screenshot",
	}
	screenshotJSON, _ := json.Marshal(screenshotParams)
	result, err := tool.Execute(ctx, screenshotJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Should return base64 encoded image or file path
	if !strings.Contains(result.Content, "screenshot") && !strings.Contains(result.Content, "base64") {
		t.Errorf("expected screenshot info in result, got: %s", result.Content)
	}
}

// TestBrowserTool_ExtractText tests text content extraction
func TestBrowserTool_ExtractText(t *testing.T) {
	requirePlaywright(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body>
				<div id="content">Hello World</div>
				<p class="description">This is a test paragraph</p>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// Navigate first
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Extract text
	extractParams := ExtractParams{
		Action:   "extract_text",
		Selector: "#content",
	}
	extractJSON, _ := json.Marshal(extractParams)
	result, err := tool.Execute(ctx, extractJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	if !strings.Contains(result.Content, "Hello World") {
		t.Errorf("expected 'Hello World' in result, got: %s", result.Content)
	}
}

// TestBrowserTool_ExtractHTML tests HTML extraction
func TestBrowserTool_ExtractHTML(t *testing.T) {
	requirePlaywright(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body>
				<div id="content"><p>Test HTML</p></div>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// Navigate first
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Extract HTML
	extractParams := ExtractParams{
		Action:   "extract_html",
		Selector: "#content",
	}
	extractJSON, _ := json.Marshal(extractParams)
	result, err := tool.Execute(ctx, extractJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	if !strings.Contains(result.Content, "<p>") || !strings.Contains(result.Content, "Test HTML") {
		t.Errorf("expected HTML content in result, got: %s", result.Content)
	}
}

// TestBrowserTool_ExecuteJS tests JavaScript execution
func TestBrowserTool_ExecuteJS(t *testing.T) {
	requirePlaywright(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body>
				<div id="result"></div>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// Navigate first
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Execute JavaScript
	jsParams := ExecuteJSParams{
		Action: "execute_js",
		Script: "return document.title;",
	}
	jsJSON, _ := json.Marshal(jsParams)
	result, err := tool.Execute(ctx, jsJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
}

// TestBrowserTool_WaitForElement tests waiting for elements
func TestBrowserTool_WaitForElement(t *testing.T) {
	requirePlaywright(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<body>
				<div id="initial">Initial content</div>
				<script>
					setTimeout(function() {
						var div = document.createElement('div');
						div.id = 'dynamic';
						div.textContent = 'Dynamic content';
						document.body.appendChild(div);
					}, 100);
				</script>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	// Navigate first
	navParams := NavigateParams{
		Action: "navigate",
		URL:    ts.URL,
	}
	navJSON, _ := json.Marshal(navParams)
	ctx := context.Background()
	_, err = tool.Execute(ctx, navJSON)
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	// Wait for element
	waitParams := WaitParams{
		Action:   "wait_for_element",
		Selector: "#dynamic",
		Timeout:  5000, // 5 seconds
	}
	waitJSON, _ := json.Marshal(waitParams)
	result, err := tool.Execute(ctx, waitJSON)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
}

// TestBrowserTool_InvalidAction tests handling of invalid actions
func TestBrowserTool_InvalidAction(t *testing.T) {
	requirePlaywright(t)

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	tool := NewBrowserTool(pool)

	params := map[string]interface{}{
		"action": "invalid_action",
	}
	paramsJSON, _ := json.Marshal(params)

	ctx := context.Background()
	result, err := tool.Execute(ctx, paramsJSON)
	if err != nil {
		t.Fatalf("execute should not return error for invalid action: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid action")
	}
}

// TestPool_Acquire tests browser instance acquisition
func TestPool_Acquire(t *testing.T) {
	requirePlaywright(t)

	pool, err := NewPool(PoolConfig{
		MaxInstances: 2,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	instance, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire instance: %v", err)
	}

	if instance == nil {
		t.Error("instance should not be nil")
	}

	pool.Release(instance)
}

// TestPool_MaxInstances tests pool max instances limit
func TestPool_MaxInstances(t *testing.T) {
	requirePlaywright(t)

	pool, err := NewPool(PoolConfig{
		MaxInstances: 1,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Acquire first instance
	instance1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire first instance: %v", err)
	}

	// Try to acquire second instance (should block or timeout)
	ctx2, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err = pool.Acquire(ctx2)
	if err != context.DeadlineExceeded {
		t.Error("expected context deadline exceeded when pool is full")
	}

	// Release first instance
	pool.Release(instance1)

	// Now should be able to acquire
	instance2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire after release: %v", err)
	}
	pool.Release(instance2)
}

// Parameter types for different actions
type NavigateParams struct {
	Action string `json:"action"`
	URL    string `json:"url"`
}

type ClickParams struct {
	Action   string `json:"action"`
	Selector string `json:"selector"`
}

type TypeParams struct {
	Action   string `json:"action"`
	Selector string `json:"selector"`
	Text     string `json:"text"`
}

type ScreenshotParams struct {
	Action   string `json:"action"`
	FullPage bool   `json:"full_page,omitempty"`
}

type ExtractParams struct {
	Action   string `json:"action"`
	Selector string `json:"selector,omitempty"`
}

type ExecuteJSParams struct {
	Action string `json:"action"`
	Script string `json:"script"`
}

type WaitParams struct {
	Action   string `json:"action"`
	Selector string `json:"selector"`
	Timeout  int    `json:"timeout,omitempty"`
}
