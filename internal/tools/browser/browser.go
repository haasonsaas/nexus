package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/playwright-community/playwright-go"
)

// BrowserTool implements the agent.Tool interface for browser automation
type BrowserTool struct {
	pool *Pool
}

// NewBrowserTool creates a new browser automation tool
func NewBrowserTool(pool *Pool) *BrowserTool {
	return &BrowserTool{
		pool: pool,
	}
}

// Name returns the tool name
func (b *BrowserTool) Name() string {
	return "browser"
}

// Description returns the tool description
func (b *BrowserTool) Description() string {
	return "Automate web browser interactions including navigation, clicking, form filling, screenshots, content extraction, and JavaScript execution. Supports headless browsing with configurable timeouts and session management."
}

// Schema returns the JSON schema for the tool parameters
func (b *BrowserTool) Schema() json.RawMessage {
	schema := `{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["navigate", "click", "type", "screenshot", "extract_text", "extract_html", "wait_for_element", "wait_for_navigation", "execute_js"],
				"description": "The browser action to perform"
			},
			"url": {
				"type": "string",
				"description": "URL to navigate to (required for navigate action)"
			},
			"selector": {
				"type": "string",
				"description": "CSS selector for the target element (required for click, type, extract actions)"
			},
			"text": {
				"type": "string",
				"description": "Text to type into an input field (required for type action)"
			},
			"script": {
				"type": "string",
				"description": "JavaScript code to execute (required for execute_js action)"
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in milliseconds for wait operations (default: 30000)"
			},
			"full_page": {
				"type": "boolean",
				"description": "Whether to capture full page screenshot (default: false)"
			}
		},
		"required": ["action"]
	}`
	return json.RawMessage(schema)
}

// Execute runs the browser tool with the given parameters
func (b *BrowserTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var baseParams struct {
		Action string `json:"action"`
	}

	if err := json.Unmarshal(params, &baseParams); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Get browser instance from pool
	instance, err := b.pool.Acquire(ctx)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("failed to acquire browser instance: %v", err),
			IsError: true,
		}, nil
	}
	defer b.pool.Release(instance)

	// Route to appropriate action handler
	switch baseParams.Action {
	case "navigate":
		return b.handleNavigate(ctx, instance, params)
	case "click":
		return b.handleClick(ctx, instance, params)
	case "type":
		return b.handleType(ctx, instance, params)
	case "screenshot":
		return b.handleScreenshot(ctx, instance, params)
	case "extract_text":
		return b.handleExtractText(ctx, instance, params)
	case "extract_html":
		return b.handleExtractHTML(ctx, instance, params)
	case "wait_for_element":
		return b.handleWaitForElement(ctx, instance, params)
	case "wait_for_navigation":
		return b.handleWaitForNavigation(ctx, instance, params)
	case "execute_js":
		return b.handleExecuteJS(ctx, instance, params)
	default:
		return &agent.ToolResult{
			Content: fmt.Sprintf("unknown action: %s", baseParams.Action),
			IsError: true,
		}, nil
	}
}

// handleNavigate navigates to a URL
func (b *BrowserTool) handleNavigate(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		URL string `json:"url"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid navigate parameters: %v", err),
			IsError: true,
		}, nil
	}

	if p.URL == "" {
		return &agent.ToolResult{
			Content: "url parameter is required for navigate action",
			IsError: true,
		}, nil
	}

	_, err := instance.Page.Goto(p.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("navigation failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Successfully navigated to %s", p.URL),
		IsError: false,
	}, nil
}

// handleClick clicks an element
func (b *BrowserTool) handleClick(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Selector string `json:"selector"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid click parameters: %v", err),
			IsError: true,
		}, nil
	}

	if p.Selector == "" {
		return &agent.ToolResult{
			Content: "selector parameter is required for click action",
			IsError: true,
		}, nil
	}

	if err := instance.Page.Click(p.Selector); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("click failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Successfully clicked element: %s", p.Selector),
		IsError: false,
	}, nil
}

// handleType types text into an input field
func (b *BrowserTool) handleType(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Selector string `json:"selector"`
		Text     string `json:"text"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid type parameters: %v", err),
			IsError: true,
		}, nil
	}

	if p.Selector == "" {
		return &agent.ToolResult{
			Content: "selector parameter is required for type action",
			IsError: true,
		}, nil
	}

	if err := instance.Page.Fill(p.Selector, p.Text); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("type failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Successfully typed text into element: %s", p.Selector),
		IsError: false,
	}, nil
}

// handleScreenshot takes a screenshot
func (b *BrowserTool) handleScreenshot(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		FullPage bool `json:"full_page"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid screenshot parameters: %v", err),
			IsError: true,
		}, nil
	}

	screenshot, err := instance.Page.Screenshot(playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(p.FullPage),
		Type:     playwright.ScreenshotTypePng,
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("screenshot failed: %v", err),
			IsError: true,
		}, nil
	}

	// Encode screenshot as base64
	encoded := base64.StdEncoding.EncodeToString(screenshot)

	return &agent.ToolResult{
		Content: fmt.Sprintf("Screenshot captured (base64): %s", encoded[:100]+"..."), // Truncate for readability
		IsError: false,
	}, nil
}

// handleExtractText extracts text content from an element
func (b *BrowserTool) handleExtractText(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Selector string `json:"selector"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid extract_text parameters: %v", err),
			IsError: true,
		}, nil
	}

	var text string
	if p.Selector == "" {
		// Extract all text from body
		text, err := instance.Page.TextContent("body")
		if err != nil {
			return &agent.ToolResult{
				Content: fmt.Sprintf("text extraction failed: %v", err),
				IsError: true,
			}, nil
		}
		return &agent.ToolResult{
			Content: text,
			IsError: false,
		}, nil
	}

	text, err := instance.Page.TextContent(p.Selector)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("text extraction failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: text,
		IsError: false,
	}, nil
}

// handleExtractHTML extracts HTML content
func (b *BrowserTool) handleExtractHTML(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Selector string `json:"selector"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid extract_html parameters: %v", err),
			IsError: true,
		}, nil
	}

	var html string
	var err error

	if p.Selector == "" {
		// Get full page HTML
		html, err = instance.Page.Content()
	} else {
		// Get element's innerHTML
		result, evalErr := instance.Page.Evaluate(fmt.Sprintf("document.querySelector('%s').innerHTML", p.Selector))
		if evalErr != nil {
			return &agent.ToolResult{
				Content: fmt.Sprintf("HTML extraction failed: %v", evalErr),
				IsError: true,
			}, nil
		}
		html = fmt.Sprintf("%v", result)
	}

	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("HTML extraction failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: html,
		IsError: false,
	}, nil
}

// handleWaitForElement waits for an element to appear
func (b *BrowserTool) handleWaitForElement(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Selector string  `json:"selector"`
		Timeout  float64 `json:"timeout"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid wait_for_element parameters: %v", err),
			IsError: true,
		}, nil
	}

	if p.Selector == "" {
		return &agent.ToolResult{
			Content: "selector parameter is required for wait_for_element action",
			IsError: true,
		}, nil
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30000 // Default 30 seconds
	}

	_, err := instance.Page.WaitForSelector(p.Selector, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(timeout),
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("wait for element failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Element appeared: %s", p.Selector),
		IsError: false,
	}, nil
}

// handleWaitForNavigation waits for navigation to complete
func (b *BrowserTool) handleWaitForNavigation(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Timeout float64 `json:"timeout"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid wait_for_navigation parameters: %v", err),
			IsError: true,
		}, nil
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30000 // Default 30 seconds
	}

	err := instance.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		Timeout: playwright.Float(timeout),
	})
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("wait for navigation failed: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: "Navigation completed",
		IsError: false,
	}, nil
}

// handleExecuteJS executes JavaScript in the page context
func (b *BrowserTool) handleExecuteJS(ctx context.Context, instance *BrowserInstance, params json.RawMessage) (*agent.ToolResult, error) {
	var p struct {
		Script string `json:"script"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("invalid execute_js parameters: %v", err),
			IsError: true,
		}, nil
	}

	if p.Script == "" {
		return &agent.ToolResult{
			Content: "script parameter is required for execute_js action",
			IsError: true,
		}, nil
	}

	result, err := instance.Page.Evaluate(p.Script)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("JavaScript execution failed: %v", err),
			IsError: true,
		}, nil
	}

	// Convert result to string
	resultStr := fmt.Sprintf("%v", result)

	return &agent.ToolResult{
		Content: resultStr,
		IsError: false,
	}, nil
}
