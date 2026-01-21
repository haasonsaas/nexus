package browser

// Example usage of the browser tool
//
// This file demonstrates how to integrate and use the browser automation tool
// in the Nexus agent system.

/*
Example 1: Basic Setup and Registration

import (
	"time"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tools/browser"
)

func setupBrowserTool(runtime *agent.Runtime) error {
	// Create browser pool
	pool, err := browser.NewPool(browser.PoolConfig{
		MaxInstances: 5,                  // Max 5 concurrent browser instances
		Timeout:      30 * time.Second,   // 30 second default timeout
		Headless:     true,               // Run in headless mode
		ViewportWidth:  1920,             // Standard desktop viewport
		ViewportHeight: 1080,
	})
	if err != nil {
		return err
	}

	// Create and register browser tool
	browserTool := browser.NewBrowserTool(pool)
	runtime.RegisterTool(browserTool)

	return nil
}

Example 2: Navigate to a URL

{
	"action": "navigate",
	"url": "https://example.com"
}

Response: "Successfully navigated to https://example.com"

Example 3: Click an Element

{
	"action": "click",
	"selector": "#submit-button"
}

Response: "Successfully clicked element: #submit-button"

Example 4: Fill a Form

{
	"action": "type",
	"selector": "#username",
	"text": "myusername"
}

Response: "Successfully typed text into element: #username"

Example 5: Take a Screenshot

{
	"action": "screenshot",
	"full_page": true
}

Response: "Screenshot captured (base64): iVBORw0KGgoAAAANSUhEUgAA..."

Example 6: Extract Text Content

{
	"action": "extract_text",
	"selector": ".article-content"
}

Response: "This is the text content from the article..."

Example 7: Extract HTML

{
	"action": "extract_html",
	"selector": "#main-content"
}

Response: "<div class=\"content\"><h1>Title</h1><p>Content...</p></div>"

Example 8: Wait for Element

{
	"action": "wait_for_element",
	"selector": ".dynamic-content",
	"timeout": 5000
}

Response: "Element appeared: .dynamic-content"

Example 9: Execute JavaScript

{
	"action": "execute_js",
	"script": "return document.title;"
}

Response: "Page Title"

Example 10: Complex Workflow

// Step 1: Navigate
{
	"action": "navigate",
	"url": "https://login.example.com"
}

// Step 2: Fill username
{
	"action": "type",
	"selector": "#username",
	"text": "user@example.com"
}

// Step 3: Fill password
{
	"action": "type",
	"selector": "#password",
	"text": "password123"
}

// Step 4: Click login button
{
	"action": "click",
	"selector": "#login-btn"
}

// Step 5: Wait for dashboard
{
	"action": "wait_for_element",
	"selector": ".dashboard",
	"timeout": 10000
}

// Step 6: Extract dashboard data
{
	"action": "extract_text",
	"selector": ".dashboard"
}

Example 11: Working with Cookies

import (
	"context"
	"github.com/playwright-community/playwright-go"
)

func workWithCookies(pool *browser.Pool) error {
	ctx := context.Background()
	instance, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer pool.Release(instance)

	// Set a cookie
	instance.SetCookie(playwright.OptionalCookie{
		Name:   playwright.String("session_id"),
		Value:  playwright.String("abc123"),
		Domain: playwright.String(".example.com"),
		Path:   playwright.String("/"),
	})

	// Get cookies
	cookies, err := instance.GetCookies()
	if err != nil {
		return err
	}

	// Clear cookies
	return instance.ClearCookies()
}

Example 12: Custom Viewport

func customViewport(pool *browser.Pool) error {
	ctx := context.Background()
	instance, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer pool.Release(instance)

	// Set mobile viewport
	return instance.SetViewport(375, 667)
}

Example 13: Pool Statistics

func checkPoolStats(pool *browser.Pool) {
	stats := pool.GetStats()
	fmt.Printf("Max Instances: %d\n", stats.MaxInstances)
	fmt.Printf("Available: %d\n", stats.AvailableInstances)
	fmt.Printf("Closed: %v\n", stats.IsClosed)
}

Example 14: Proper Cleanup

func main() {
	pool, err := browser.NewPool(browser.PoolConfig{
		MaxInstances: 3,
		Timeout:      30 * time.Second,
		Headless:     true,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close() // Always close the pool when done

	// Use the pool...
}

Supported Actions:
- navigate: Navigate to a URL
- click: Click an element by CSS selector
- type: Type text into an input field
- screenshot: Capture a screenshot (full page or viewport)
- extract_text: Extract text content from an element
- extract_html: Extract HTML content from an element
- wait_for_element: Wait for an element to appear
- wait_for_navigation: Wait for navigation to complete
- execute_js: Execute JavaScript in the page context

Features:
- Browser instance pooling for efficient resource usage
- Headless mode (default)
- Configurable timeouts
- Cookie and session management
- Custom viewport configuration
- User agent rotation (5 different user agents)
- Automatic cleanup and resource management
- Thread-safe pool operations

Error Handling:
All tool executions return a ToolResult with:
- Content: The result content or error message
- IsError: Boolean indicating if an error occurred

The tool handles errors gracefully and returns them as ToolResult
instead of panicking, making it safe for agent use.

*/
