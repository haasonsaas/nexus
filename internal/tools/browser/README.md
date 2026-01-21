# Browser Automation Tool

A comprehensive browser automation tool for the Nexus agent system, built using Playwright for Go.

## Overview

This package provides a robust browser automation tool that implements the `agent.Tool` interface, enabling AI agents to interact with web pages programmatically. It supports headless browsing, browser instance pooling, and a wide range of web automation actions.

## Features

- **Full Browser Automation**: Navigate, click, type, screenshot, and extract content
- **Instance Pooling**: Efficient resource management with configurable pool sizes
- **Headless Mode**: Run browsers without GUI (default behavior)
- **Timeout Handling**: Configurable timeouts for all operations
- **Cookie/Session Management**: Set, get, and clear cookies
- **Viewport Configuration**: Custom viewport sizes for different devices
- **User Agent Rotation**: Automatic rotation through 5 different user agents
- **Thread-Safe**: Safe for concurrent use across multiple goroutines
- **Error Handling**: Graceful error handling with detailed error messages

## Installation

The package is already integrated with the Nexus project. The playwright-go dependency is included in `go.mod`:

```go
github.com/playwright-community/playwright-go v0.5200.1
```

## Architecture

### Files

1. **browser.go** (460 lines)
   - Main tool implementation
   - Implements `agent.Tool` interface
   - Handles all browser actions (navigate, click, type, etc.)

2. **pool.go** (277 lines)
   - Browser instance pooling
   - Resource management
   - User agent rotation
   - Cookie/session helpers

3. **browser_test.go** (646 lines)
   - Comprehensive TDD test suite
   - Tests for all actions
   - Pool management tests
   - Uses httptest for test pages

4. **example_usage.go**
   - Usage examples and documentation
   - Integration guide
   - Best practices

## Usage

### Basic Setup

```go
import (
    "time"
    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/tools/browser"
)

// Create browser pool
pool, err := browser.NewPool(browser.PoolConfig{
    MaxInstances:   5,                // Max concurrent browsers
    Timeout:        30 * time.Second, // Default timeout
    Headless:       true,             // Run headless
    ViewportWidth:  1920,             // Viewport width
    ViewportHeight: 1080,             // Viewport height
})
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

// Create and register tool
browserTool := browser.NewBrowserTool(pool)
runtime.RegisterTool(browserTool)
```

### Supported Actions

#### 1. Navigate
```json
{
    "action": "navigate",
    "url": "https://example.com"
}
```

#### 2. Click Element
```json
{
    "action": "click",
    "selector": "#submit-button"
}
```

#### 3. Type Text
```json
{
    "action": "type",
    "selector": "#username",
    "text": "myusername"
}
```

#### 4. Take Screenshot
```json
{
    "action": "screenshot",
    "full_page": true
}
```

#### 5. Extract Text
```json
{
    "action": "extract_text",
    "selector": ".article-content"
}
```

#### 6. Extract HTML
```json
{
    "action": "extract_html",
    "selector": "#main-content"
}
```

#### 7. Wait for Element
```json
{
    "action": "wait_for_element",
    "selector": ".dynamic-content",
    "timeout": 5000
}
```

#### 8. Wait for Navigation
```json
{
    "action": "wait_for_navigation",
    "timeout": 10000
}
```

#### 9. Execute JavaScript
```json
{
    "action": "execute_js",
    "script": "return document.title;"
}
```

## Pool Configuration

```go
type PoolConfig struct {
    MaxInstances   int           // Maximum browser instances (default: 5)
    Timeout        time.Duration // Default timeout (default: 30s)
    Headless       bool          // Headless mode (default: true)
    ViewportWidth  int           // Viewport width (default: 1920)
    ViewportHeight int           // Viewport height (default: 1080)
}
```

## Browser Instance API

```go
// Acquire a browser instance
instance, err := pool.Acquire(ctx)
defer pool.Release(instance)

// Cookie management
instance.SetCookie(cookies...)
cookies, err := instance.GetCookies()
instance.ClearCookies()

// Viewport configuration
instance.SetViewport(width, height)

// Direct page access
instance.Page.Click(selector)
instance.Page.Fill(selector, text)
// ... all Playwright Page methods available
```

## Testing

Run the test suite:

```bash
# Run all tests
go test ./internal/tools/browser

# Run specific test
go test ./internal/tools/browser -run TestBrowserTool_Navigate

# Run with verbose output
go test -v ./internal/tools/browser

# Run with coverage
go test -cover ./internal/tools/browser
```

### Test Coverage

The test suite includes:
- Tool interface implementation tests (Name, Description, Schema)
- Navigation tests
- Element interaction tests (click, type)
- Screenshot capture tests
- Content extraction tests (text and HTML)
- JavaScript execution tests
- Wait operation tests
- Pool management tests
- Error handling tests

## Error Handling

All operations return `*agent.ToolResult`:

```go
type ToolResult struct {
    Content string // Result content or error message
    IsError bool   // True if an error occurred
}
```

Example error response:
```json
{
    "content": "navigation failed: timeout exceeded",
    "is_error": true
}
```

## Performance Considerations

1. **Pool Size**: Set `MaxInstances` based on available memory
   - Each browser instance uses ~50-100MB RAM
   - Recommended: 3-10 instances for most use cases

2. **Timeouts**: Adjust based on network conditions
   - Fast connections: 10-15 seconds
   - Slow connections: 30-60 seconds

3. **Headless Mode**: Always use headless mode in production
   - Headless is faster and uses less memory
   - GUI mode only for debugging

4. **Resource Cleanup**: Always close the pool when done
   ```go
   defer pool.Close()
   ```

## Security Considerations

1. **User Input Validation**: Always validate URLs and selectors
2. **Timeout Limits**: Set reasonable timeouts to prevent hanging
3. **Resource Limits**: Configure max instances to prevent DoS
4. **Cookie Management**: Clear sensitive cookies after use
5. **HTTPS**: Use HTTPS URLs when possible

## Troubleshooting

### Common Issues

**Issue**: "playwright not initialized"
- **Solution**: Ensure Playwright is installed: `go run github.com/playwright-community/playwright-go/cmd/playwright install`

**Issue**: "failed to acquire browser instance"
- **Solution**: Pool is at max capacity. Increase `MaxInstances` or ensure instances are being released.

**Issue**: "timeout exceeded"
- **Solution**: Increase timeout in PoolConfig or specific operation timeout parameter.

**Issue**: "element not found"
- **Solution**: Use `wait_for_element` before interacting with dynamic content.

## Advanced Usage

### Complex Workflows

```go
// Login workflow
actions := []struct {
    action   string
    params   map[string]interface{}
}{
    {"navigate", map[string]interface{}{"url": "https://example.com/login"}},
    {"type", map[string]interface{}{"selector": "#email", "text": "user@example.com"}},
    {"type", map[string]interface{}{"selector": "#password", "text": "password"}},
    {"click", map[string]interface{}{"selector": "#login-btn"}},
    {"wait_for_element", map[string]interface{}{"selector": ".dashboard", "timeout": 10000}},
    {"screenshot", map[string]interface{}{"full_page": true}},
}

for _, a := range actions {
    params, _ := json.Marshal(a.params)
    result, err := tool.Execute(ctx, params)
    if err != nil || result.IsError {
        log.Printf("Action %s failed: %s", a.action, result.Content)
        break
    }
}
```

### Custom User Agents

The pool automatically rotates through these user agents:
- Windows Chrome
- macOS Chrome
- Linux Chrome
- Windows Firefox
- macOS Safari

### Mobile Emulation

```go
// Acquire instance
instance, err := pool.Acquire(ctx)
defer pool.Release(instance)

// Set iPhone viewport
instance.SetViewport(375, 667)

// Navigate and interact as mobile device
instance.Page.Goto("https://example.com")
```

## Contributing

When extending this tool:

1. Add tests for new functionality
2. Update the schema in `Schema()` method
3. Document new actions in this README
4. Follow TDD approach (tests first)
5. Ensure thread safety

## License

Part of the Nexus project. See main project LICENSE file.

## Credits

Built with [playwright-community/playwright-go](https://github.com/playwright-community/playwright-go)
