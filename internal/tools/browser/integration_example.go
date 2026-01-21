package browser

/*
Integration Example: Adding Browser Tool to Nexus Runtime

This example demonstrates how to integrate the browser automation tool
into your Nexus agent runtime.

Step 1: Import Required Packages
================================

import (
	"log"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	"github.com/haasonsaas/nexus/internal/sessions"
)

Step 2: Initialize Browser Pool
==============================

func initializeBrowserTool() (*browser.Pool, error) {
	// Create browser pool with production settings
	pool, err := browser.NewPool(browser.PoolConfig{
		MaxInstances:   5,                  // 5 concurrent browsers
		Timeout:        30 * time.Second,   // 30s timeout
		Headless:       true,               // Headless mode
		ViewportWidth:  1920,               // Desktop viewport
		ViewportHeight: 1080,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create browser pool: %w", err)
	}

	log.Printf("Browser pool initialized with %d max instances", 5)
	return pool, nil
}

Step 3: Register Tool with Runtime
=================================

func setupRuntime(sessionStore sessions.Store, llmProvider agent.LLMProvider) (*agent.Runtime, error) {
	// Create runtime
	runtime := agent.NewRuntime(llmProvider, sessionStore)

	// Initialize browser pool
	pool, err := initializeBrowserTool()
	if err != nil {
		return nil, err
	}

	// Create browser tool
	browserTool := browser.NewBrowserTool(pool)

	// Register with runtime
	runtime.RegisterTool(browserTool)

	log.Printf("Registered browser tool: %s", browserTool.Name())
	log.Printf("Description: %s", browserTool.Description())

	return runtime, nil
}

Step 4: Example Agent Conversation
=================================

func exampleConversation(runtime *agent.Runtime) {
	// Create a session
	session := &models.Session{
		ID:        "session-123",
		CreatedAt: time.Now(),
	}

	// User asks to scrape a website
	userMessage := &models.Message{
		Role:    models.RoleUser,
		Content: "Please navigate to https://example.com and extract the main heading text",
	}

	// Process message
	ctx := context.Background()
	chunks, err := runtime.Process(ctx, session, userMessage)
	if err != nil {
		log.Fatal(err)
	}

	// Stream response
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			continue
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}

		if chunk.ToolResult != nil {
			log.Printf("Tool executed: %s", chunk.ToolResult.Content)
		}
	}
}

Step 5: Main Application
=======================

func main() {
	// Initialize session store
	sessionStore := sessions.NewMemoryStore()

	// Initialize LLM provider (example: Anthropic)
	llmProvider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model:  "claude-3-5-sonnet-20241022",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Setup runtime with browser tool
	runtime, err := setupRuntime(sessionStore, llmProvider)
	if err != nil {
		log.Fatal(err)
	}

	// Run example conversation
	exampleConversation(runtime)
}

Step 6: Docker Deployment Considerations
=======================================

When deploying with Docker, ensure Playwright dependencies are installed:

FROM golang:1.24-bookworm

# Install Playwright dependencies
RUN apt-get update && apt-get install -y \
    libnss3 \
    libnspr4 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libdbus-1-3 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libasound2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Install Playwright browsers
RUN go run github.com/playwright-community/playwright-go/cmd/playwright install --with-deps chromium

RUN go build -o nexus ./cmd/nexus

CMD ["./nexus"]

Step 7: Environment Configuration
================================

# .env file
NEXUS_BROWSER_MAX_INSTANCES=5
NEXUS_BROWSER_TIMEOUT=30s
NEXUS_BROWSER_HEADLESS=true
NEXUS_BROWSER_VIEWPORT_WIDTH=1920
NEXUS_BROWSER_VIEWPORT_HEIGHT=1080

# Load in application
func loadBrowserConfig() browser.PoolConfig {
	return browser.PoolConfig{
		MaxInstances:   getEnvInt("NEXUS_BROWSER_MAX_INSTANCES", 5),
		Timeout:        getEnvDuration("NEXUS_BROWSER_TIMEOUT", 30*time.Second),
		Headless:       getEnvBool("NEXUS_BROWSER_HEADLESS", true),
		ViewportWidth:  getEnvInt("NEXUS_BROWSER_VIEWPORT_WIDTH", 1920),
		ViewportHeight: getEnvInt("NEXUS_BROWSER_VIEWPORT_HEIGHT", 1080),
	}
}

Step 8: Graceful Shutdown
========================

func main() {
	// ... initialization code ...

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run server in goroutine
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal(err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down gracefully...")

	// Close browser pool
	if err := pool.Close(); err != nil {
		log.Printf("Error closing browser pool: %v", err)
	}

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	log.Println("Shutdown complete")
}

Step 9: Monitoring and Metrics
=============================

func monitorBrowserPool(pool *browser.Pool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := pool.GetStats()
		log.Printf(
			"Browser Pool Stats - Max: %d, Available: %d, Closed: %v",
			stats.MaxInstances,
			stats.AvailableInstances,
			stats.IsClosed,
		)

		// Alert if pool is consistently full
		if stats.AvailableInstances == 0 {
			log.Printf("WARNING: Browser pool is at max capacity")
		}
	}
}

Step 10: Error Recovery
======================

func robustBrowserExecution(tool *browser.BrowserTool, params json.RawMessage) (*agent.ToolResult, error) {
	// Retry configuration
	maxRetries := 3
	retryDelay := 2 * time.Second

	var result *agent.ToolResult
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		result, err = tool.Execute(ctx, params)

		// Success
		if err == nil && !result.IsError {
			return result, nil
		}

		// Log retry attempt
		if attempt < maxRetries {
			log.Printf(
				"Browser tool execution failed (attempt %d/%d), retrying in %v...",
				attempt, maxRetries, retryDelay,
			)
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff
		}
	}

	return result, fmt.Errorf("browser tool failed after %d attempts: %w", maxRetries, err)
}

Complete Example with All Steps
==============================

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/browser"
	"github.com/haasonsaas/nexus/pkg/models"
)

func main() {
	// 1. Initialize browser pool
	pool, err := browser.NewPool(browser.PoolConfig{
		MaxInstances:   5,
		Timeout:        30 * time.Second,
		Headless:       true,
		ViewportWidth:  1920,
		ViewportHeight: 1080,
	})
	if err != nil {
		log.Fatalf("Failed to create browser pool: %v", err)
	}
	defer pool.Close()

	// 2. Create browser tool
	browserTool := browser.NewBrowserTool(pool)

	// 3. Initialize session store
	sessionStore := sessions.NewMemoryStore()

	// 4. Initialize LLM provider
	llmProvider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model:  "claude-3-5-sonnet-20241022",
	})
	if err != nil {
		log.Fatalf("Failed to create LLM provider: %v", err)
	}

	// 5. Create runtime and register tool
	runtime := agent.NewRuntime(llmProvider, sessionStore)
	runtime.RegisterTool(browserTool)

	log.Printf("✓ Browser tool registered: %s", browserTool.Name())
	log.Printf("✓ Runtime initialized with browser automation support")

	// 6. Setup monitoring
	go monitorBrowserPool(pool)

	// 7. Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 8. Run application
	// ... your application logic ...

	// 9. Wait for shutdown
	<-sigChan
	log.Println("Shutting down gracefully...")
}

This integration example shows the complete setup process for adding
browser automation capabilities to your Nexus agent system.

*/
