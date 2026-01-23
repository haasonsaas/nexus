// Package main provides the nexus-edge daemon.
//
// browser_tools.go implements browser relay tools for attaching to and
// controlling existing Chrome sessions via the Chrome DevTools Protocol.
//
// Prerequisites:
//   - Chrome must be started with: --remote-debugging-port=9222
//   - Example: /Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222
//
// Tools:
//   - browser.list_tabs: List all open Chrome tabs
//   - browser.attach: Attach to a specific tab by URL or title
//   - browser.snapshot: Take a screenshot of the attached tab
//   - browser.act: Perform actions on the attached tab
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// BrowserRelay manages connections to Chrome sessions.
type BrowserRelay struct {
	mu          sync.RWMutex
	allocCtx    context.Context
	allocCancel context.CancelFunc
	taskCtx     context.Context
	taskCancel  context.CancelFunc
	debugURL    string
	targetID    string
	targetURL   string
	targetTitle string
}

var browserRelay = &BrowserRelay{}

// RegisterBrowserTools registers all browser relay tools with the daemon.
func RegisterBrowserTools(daemon *EdgeDaemon) {
	daemon.RegisterTool(browserListTabsTool())
	daemon.RegisterTool(browserAttachTool())
	daemon.RegisterTool(browserSnapshotTool())
	daemon.RegisterTool(browserActTool())
	daemon.RegisterTool(browserDetachTool())
}

// browserListTabsTool lists all available Chrome tabs.
func browserListTabsTool() *Tool {
	return &Tool{
		Name:        "browser.list_tabs",
		Description: "List all open Chrome tabs. Chrome must be running with --remote-debugging-port=9222",
		InputSchema: `{
			"type": "object",
			"properties": {
				"debug_url": {
					"type": "string",
					"description": "Chrome DevTools URL (default: http://localhost:9222)",
					"default": "http://localhost:9222"
				}
			}
		}`,
		RequiresApproval:  false,
		TimeoutSeconds:    10,
		ProducesArtifacts: false,
		Handler:           handleBrowserListTabs,
	}
}

func handleBrowserListTabs(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		DebugURL string `json:"debug_url"`
	}
	params.DebugURL = "http://localhost:9222"
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Create a remote allocator to connect to Chrome
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, params.DebugURL)
	defer allocCancel()

	// Create a context for getting targets
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	// Get list of targets (tabs)
	targets, err := chromedp.Targets(taskCtx)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to connect to Chrome. Ensure Chrome is running with --remote-debugging-port=9222\nError: %v", err),
			IsError: true,
		}, nil
	}

	var tabInfo []string
	for _, t := range targets {
		if t.Type == "page" {
			tabInfo = append(tabInfo, fmt.Sprintf("- ID: %s\n  Title: %s\n  URL: %s", t.TargetID, t.Title, t.URL))
		}
	}

	if len(tabInfo) == 0 {
		return &ToolResult{
			Content: "No tabs found. Ensure Chrome has at least one open tab.",
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("Found %d tab(s):\n\n%s", len(tabInfo), strings.Join(tabInfo, "\n\n")),
	}, nil
}

// browserAttachTool attaches to a specific Chrome tab.
func browserAttachTool() *Tool {
	return &Tool{
		Name:        "browser.attach",
		Description: "Attach to a Chrome tab by URL pattern or title. Required before using snapshot/act tools.",
		InputSchema: `{
			"type": "object",
			"properties": {
				"url_pattern": {
					"type": "string",
					"description": "Regex pattern to match tab URL"
				},
				"title_pattern": {
					"type": "string",
					"description": "Regex pattern to match tab title"
				},
				"target_id": {
					"type": "string",
					"description": "Specific target ID from list_tabs"
				},
				"debug_url": {
					"type": "string",
					"description": "Chrome DevTools URL (default: http://localhost:9222)",
					"default": "http://localhost:9222"
				}
			}
		}`,
		RequiresApproval:  true,
		TimeoutSeconds:    15,
		ProducesArtifacts: false,
		Handler:           handleBrowserAttach,
	}
}

func handleBrowserAttach(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		URLPattern   string `json:"url_pattern"`
		TitlePattern string `json:"title_pattern"`
		TargetID     string `json:"target_id"`
		DebugURL     string `json:"debug_url"`
	}
	params.DebugURL = "http://localhost:9222"
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Create a temporary context to list targets
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, params.DebugURL)
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)

	// Get list of targets
	targets, err := chromedp.Targets(taskCtx)
	taskCancel()
	allocCancel()

	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to connect to Chrome: %v", err),
			IsError: true,
		}, nil
	}

	// Find matching target
	var matchedTarget *target.Info
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}

		// Match by target ID
		if params.TargetID != "" && string(t.TargetID) == params.TargetID {
			matchedTarget = t
			break
		}

		// Match by URL pattern
		if params.URLPattern != "" {
			re, err := regexp.Compile(params.URLPattern)
			if err == nil && re.MatchString(t.URL) {
				matchedTarget = t
				break
			}
		}

		// Match by title pattern
		if params.TitlePattern != "" {
			re, err := regexp.Compile(params.TitlePattern)
			if err == nil && re.MatchString(t.Title) {
				matchedTarget = t
				break
			}
		}
	}

	if matchedTarget == nil {
		return &ToolResult{
			Content: "No matching tab found. Use browser.list_tabs to see available tabs.",
			IsError: true,
		}, nil
	}

	// Detach from any existing session
	browserRelay.mu.Lock()
	if browserRelay.taskCancel != nil {
		browserRelay.taskCancel()
	}
	if browserRelay.allocCancel != nil {
		browserRelay.allocCancel()
	}

	// Create new context attached to the target
	newAllocCtx, newAllocCancel := chromedp.NewRemoteAllocator(context.Background(), params.DebugURL)
	newTaskCtx, newTaskCancel := chromedp.NewContext(newAllocCtx,
		chromedp.WithTargetID(matchedTarget.TargetID),
	)

	browserRelay.allocCtx = newAllocCtx
	browserRelay.allocCancel = newAllocCancel
	browserRelay.taskCtx = newTaskCtx
	browserRelay.taskCancel = newTaskCancel
	browserRelay.debugURL = params.DebugURL
	browserRelay.targetID = string(matchedTarget.TargetID)
	browserRelay.targetURL = matchedTarget.URL
	browserRelay.targetTitle = matchedTarget.Title
	browserRelay.mu.Unlock()

	return &ToolResult{
		Content: fmt.Sprintf("Attached to tab:\n  Title: %s\n  URL: %s\n  ID: %s", matchedTarget.Title, matchedTarget.URL, matchedTarget.TargetID),
	}, nil
}

// browserSnapshotTool takes a screenshot of the attached tab.
func browserSnapshotTool() *Tool {
	return &Tool{
		Name:        "browser.snapshot",
		Description: "Take a screenshot of the currently attached Chrome tab.",
		InputSchema: `{
			"type": "object",
			"properties": {
				"full_page": {
					"type": "boolean",
					"description": "Capture the full page instead of just the viewport",
					"default": false
				},
				"quality": {
					"type": "integer",
					"description": "JPEG quality (1-100)",
					"default": 90
				}
			}
		}`,
		RequiresApproval:  true,
		TimeoutSeconds:    30,
		ProducesArtifacts: true,
		Handler:           handleBrowserSnapshot,
	}
}

func handleBrowserSnapshot(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		FullPage bool `json:"full_page"`
		Quality  int  `json:"quality"`
	}
	params.Quality = 90
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	browserRelay.mu.RLock()
	taskCtx := browserRelay.taskCtx
	targetTitle := browserRelay.targetTitle
	browserRelay.mu.RUnlock()

	if taskCtx == nil {
		return &ToolResult{
			Content: "No tab attached. Use browser.attach first.",
			IsError: true,
		}, nil
	}

	// Take screenshot
	var buf []byte
	timeoutCtx, cancel := context.WithTimeout(taskCtx, 15*time.Second)
	defer cancel()

	var screenshotAction chromedp.Action
	if params.FullPage {
		screenshotAction = chromedp.FullScreenshot(&buf, params.Quality)
	} else {
		screenshotAction = chromedp.CaptureScreenshot(&buf)
	}

	if err := chromedp.Run(timeoutCtx, screenshotAction); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Screenshot failed: %v", err),
			IsError: true,
		}, nil
	}

	// Create artifact
	artifact := &pb.Artifact{
		Id:         uuid.NewString(),
		Type:       "screenshot",
		MimeType:   "image/png",
		Filename:   fmt.Sprintf("browser_snapshot_%s.png", time.Now().Format("20060102_150405")),
		Size:       int64(len(buf)),
		Data:       buf,
		TtlSeconds: 3600, // 1 hour
	}

	return &ToolResult{
		Content:   fmt.Sprintf("Screenshot captured from: %s (%d bytes)", targetTitle, len(buf)),
		Artifacts: []*pb.Artifact{artifact},
	}, nil
}

// browserActTool performs actions on the attached tab.
func browserActTool() *Tool {
	return &Tool{
		Name:        "browser.act",
		Description: "Perform an action on the attached Chrome tab (click, type, navigate, etc.)",
		InputSchema: `{
			"type": "object",
			"required": ["action"],
			"properties": {
				"action": {
					"type": "string",
					"enum": ["click", "type", "navigate", "scroll", "wait", "evaluate"],
					"description": "The action to perform"
				},
				"selector": {
					"type": "string",
					"description": "CSS selector for click/type actions"
				},
				"text": {
					"type": "string",
					"description": "Text to type (for type action)"
				},
				"url": {
					"type": "string",
					"description": "URL to navigate to (for navigate action)"
				},
				"script": {
					"type": "string",
					"description": "JavaScript to execute (for evaluate action)"
				},
				"direction": {
					"type": "string",
					"enum": ["up", "down"],
					"description": "Scroll direction (for scroll action)"
				},
				"amount": {
					"type": "integer",
					"description": "Scroll amount in pixels (for scroll action)",
					"default": 300
				},
				"wait_ms": {
					"type": "integer",
					"description": "Time to wait in milliseconds (for wait action)",
					"default": 1000
				}
			}
		}`,
		RequiresApproval:  true,
		TimeoutSeconds:    30,
		ProducesArtifacts: false,
		Handler:           handleBrowserAct,
	}
}

func handleBrowserAct(ctx context.Context, input string) (*ToolResult, error) {
	var params struct {
		Action    string `json:"action"`
		Selector  string `json:"selector"`
		Text      string `json:"text"`
		URL       string `json:"url"`
		Script    string `json:"script"`
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
		WaitMs    int    `json:"wait_ms"`
	}
	params.Amount = 300
	params.WaitMs = 1000
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	browserRelay.mu.RLock()
	taskCtx := browserRelay.taskCtx
	browserRelay.mu.RUnlock()

	if taskCtx == nil {
		return &ToolResult{
			Content: "No tab attached. Use browser.attach first.",
			IsError: true,
		}, nil
	}

	timeoutCtx, cancel := context.WithTimeout(taskCtx, 20*time.Second)
	defer cancel()

	var result string
	var actions []chromedp.Action

	switch params.Action {
	case "click":
		if params.Selector == "" {
			return &ToolResult{
				Content: "Selector is required for click action",
				IsError: true,
			}, nil
		}
		actions = append(actions,
			chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
			chromedp.Click(params.Selector, chromedp.ByQuery),
		)
		result = fmt.Sprintf("Clicked: %s", params.Selector)

	case "type":
		if params.Selector == "" || params.Text == "" {
			return &ToolResult{
				Content: "Selector and text are required for type action",
				IsError: true,
			}, nil
		}
		actions = append(actions,
			chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
			chromedp.SendKeys(params.Selector, params.Text, chromedp.ByQuery),
		)
		result = fmt.Sprintf("Typed '%s' into: %s", params.Text, params.Selector)

	case "navigate":
		if params.URL == "" {
			return &ToolResult{
				Content: "URL is required for navigate action",
				IsError: true,
			}, nil
		}
		actions = append(actions, chromedp.Navigate(params.URL))
		result = fmt.Sprintf("Navigated to: %s", params.URL)

	case "scroll":
		dir := 1
		if params.Direction == "up" {
			dir = -1
		}
		scrollScript := fmt.Sprintf("window.scrollBy(0, %d)", dir*params.Amount)
		actions = append(actions, chromedp.Evaluate(scrollScript, nil))
		result = fmt.Sprintf("Scrolled %s by %d pixels", params.Direction, params.Amount)

	case "wait":
		actions = append(actions, chromedp.Sleep(time.Duration(params.WaitMs)*time.Millisecond))
		result = fmt.Sprintf("Waited %d ms", params.WaitMs)

	case "evaluate":
		if params.Script == "" {
			return &ToolResult{
				Content: "Script is required for evaluate action",
				IsError: true,
			}, nil
		}
		var evalResult interface{}
		actions = append(actions, chromedp.Evaluate(params.Script, &evalResult))
		if err := chromedp.Run(timeoutCtx, actions...); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Evaluate failed: %v", err),
				IsError: true,
			}, nil
		}
		resultJSON, err := json.Marshal(evalResult)
		if err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Evaluate result marshal failed: %v", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Evaluate result: %s", string(resultJSON)),
		}, nil

	default:
		return &ToolResult{
			Content: fmt.Sprintf("Unknown action: %s", params.Action),
			IsError: true,
		}, nil
	}

	if err := chromedp.Run(timeoutCtx, actions...); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Action failed: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: result,
	}, nil
}

// browserDetachTool detaches from the current Chrome tab.
func browserDetachTool() *Tool {
	return &Tool{
		Name:        "browser.detach",
		Description: "Detach from the currently attached Chrome tab.",
		InputSchema: `{
			"type": "object",
			"properties": {}
		}`,
		RequiresApproval:  false,
		TimeoutSeconds:    5,
		ProducesArtifacts: false,
		Handler:           handleBrowserDetach,
	}
}

func handleBrowserDetach(ctx context.Context, input string) (*ToolResult, error) {
	browserRelay.mu.Lock()
	defer browserRelay.mu.Unlock()

	if browserRelay.taskCancel == nil {
		return &ToolResult{
			Content: "No tab currently attached.",
		}, nil
	}

	title := browserRelay.targetTitle
	browserRelay.taskCancel()
	if browserRelay.allocCancel != nil {
		browserRelay.allocCancel()
	}
	browserRelay.allocCtx = nil
	browserRelay.allocCancel = nil
	browserRelay.taskCtx = nil
	browserRelay.taskCancel = nil
	browserRelay.targetID = ""
	browserRelay.targetURL = ""
	browserRelay.targetTitle = ""

	return &ToolResult{
		Content: fmt.Sprintf("Detached from tab: %s", title),
	}, nil
}
