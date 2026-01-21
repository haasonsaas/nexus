package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
)

// Example tool that gets the current weather
type weatherTool struct{}

func (w *weatherTool) Name() string {
	return "get_weather"
}

func (w *weatherTool) Description() string {
	return "Get the current weather for a given city"
}

func (w *weatherTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {
				"type": "string",
				"description": "The city name"
			}
		},
		"required": ["city"]
	}`)
}

func (w *weatherTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: "Invalid input",
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("The weather in %s is sunny and 72°F", input.City),
	}, nil
}

// Example demonstrates basic usage of the Anthropic provider
func Example_basicUsage() {
	// Create provider
	provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey:       "sk-ant-api03-...", // Your API key
		DefaultModel: "claude-sonnet-4-20250514",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create a completion request
	req := &agent.CompletionRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []agent.CompletionMessage{
			{
				Role:    "user",
				Content: "Hello! How are you today?",
			},
		},
		MaxTokens: 1024,
	}

	// Send request and receive streaming response
	ctx := context.Background()
	chunks, err := provider.Complete(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	// Process streaming response
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			continue
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}

		if chunk.Done {
			fmt.Println("\n[Done]")
		}
	}
}

// Example demonstrates using the provider with tools (function calling)
func Example_withTools() {
	// Create provider
	provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey: "sk-ant-api03-...", // Your API key
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create completion request with tools
	weatherTool := &weatherTool{}
	req := &agent.CompletionRequest{
		System: "You are a helpful weather assistant.",
		Messages: []agent.CompletionMessage{
			{
				Role:    "user",
				Content: "What's the weather like in San Francisco?",
			},
		},
		Tools:     []agent.Tool{weatherTool},
		MaxTokens: 1024,
	}

	// Send request
	ctx := context.Background()
	chunks, err := provider.Complete(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	// Process response and handle tool calls
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			continue
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}

		if chunk.ToolCall != nil {
			fmt.Printf("\n[Tool Call: %s]\n", chunk.ToolCall.Name)

			// Execute the tool
			result, err := weatherTool.Execute(ctx, chunk.ToolCall.Input)
			if err != nil {
				log.Printf("Tool execution error: %v", err)
				continue
			}

			fmt.Printf("Tool Result: %s\n", result.Content)
		}

		if chunk.Done {
			fmt.Println("\n[Done]")
		}
	}
}

// Example demonstrates handling different Claude models
func Example_multipleModels() {
	provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey: "sk-ant-api03-...",
	})
	if err != nil {
		log.Fatal(err)
	}

	// List available models
	fmt.Println("Available Claude models:")
	for _, model := range provider.Models() {
		fmt.Printf("- %s: %s (context: %d tokens, vision: %v)\n",
			model.ID, model.Name, model.ContextSize, model.SupportsVision)
	}

	// Use different models for different tasks
	models := []struct {
		name  string
		model string
		task  string
	}{
		{"Fast", "claude-3-haiku-20240307", "Quick question answering"},
		{"Balanced", "claude-sonnet-4-20250514", "General purpose tasks"},
		{"Advanced", "claude-opus-4-20250514", "Complex reasoning"},
	}

	for _, m := range models {
		fmt.Printf("\n%s model (%s) for: %s\n", m.name, m.model, m.task)
	}
}

// Example demonstrates error handling and retries
func Example_errorHandling() {
	provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey:     "sk-ant-api03-...",
		MaxRetries: 3,
	})
	if err != nil {
		log.Fatal(err)
	}

	req := &agent.CompletionRequest{
		Messages: []agent.CompletionMessage{
			{Role: "user", Content: "Hello!"},
		},
		MaxTokens: 100,
	}

	ctx := context.Background()
	chunks, err := provider.Complete(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	// Handle both streaming errors and final errors
	for chunk := range chunks {
		if chunk.Error != nil {
			// Check if error is retryable
			fmt.Printf("Error occurred: %v\n", chunk.Error)
			// Provider automatically handles retries for rate limits, timeouts, etc.
			continue
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}
	}
}

// Example demonstrates system prompts and configuration
func Example_systemPrompt() {
	provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
		APIKey: "sk-ant-api03-...",
	})
	if err != nil {
		log.Fatal(err)
	}

	// System prompt guides the model's behavior
	req := &agent.CompletionRequest{
		System: `You are a helpful programming assistant. You provide clear,
concise code examples and explanations. Always format code with proper syntax highlighting.`,
		Messages: []agent.CompletionMessage{
			{
				Role:    "user",
				Content: "How do I create a HTTP server in Go?",
			},
		},
		MaxTokens: 2048,
	}

	ctx := context.Background()
	chunks, err := provider.Complete(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			continue
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}
	}
}
