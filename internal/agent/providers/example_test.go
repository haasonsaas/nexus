package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Example of basic usage with text completion
func ExampleOpenAIProvider_basicCompletion() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	// Create provider
	provider := providers.NewOpenAIProvider(apiKey)

	// Create completion request
	req := &agent.CompletionRequest{
		Model:  "gpt-3.5-turbo",
		System: "You are a helpful assistant.",
		Messages: []agent.CompletionMessage{
			{Role: "user", Content: "Say hello in 3 words"},
		},
		MaxTokens: 50,
	}

	// Get streaming response
	chunks, err := provider.Complete(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}

	// Process chunks
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}

		if chunk.Done {
			break
		}
	}
}

// Example of using vision capabilities
func ExampleOpenAIProvider_vision() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	provider := providers.NewOpenAIProvider(apiKey)

	req := &agent.CompletionRequest{
		Model: "gpt-4o",
		Messages: []agent.CompletionMessage{
			{
				Role:    "user",
				Content: "Describe this image in detail",
				Attachments: []models.Attachment{
					{
						ID:   "img1",
						Type: "image",
						URL:  "https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/2560px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg",
					},
				},
			},
		},
		MaxTokens: 300,
	}

	chunks, err := provider.Complete(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Image description:")
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}
		if chunk.Done {
			fmt.Println()
			break
		}
	}
}

// Example weather tool for function calling
type ExampleWeatherTool struct{}

func (t *ExampleWeatherTool) Name() string {
	return "get_weather"
}

func (t *ExampleWeatherTool) Description() string {
	return "Get the current weather for a location"
}

func (t *ExampleWeatherTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"location": {
				"type": "string",
				"description": "The city name, e.g., 'San Francisco'"
			},
			"unit": {
				"type": "string",
				"enum": ["celsius", "fahrenheit"],
				"description": "Temperature unit"
			}
		},
		"required": ["location"]
	}`)
}

func (t *ExampleWeatherTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var args struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}

	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}

	// Mock weather data
	return &agent.ToolResult{
		Content: fmt.Sprintf("The weather in %s is sunny and 72°F", args.Location),
	}, nil
}

// Example of function calling
func ExampleOpenAIProvider_functionCalling() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	provider := providers.NewOpenAIProvider(apiKey)

	req := &agent.CompletionRequest{
		Model: "gpt-4o",
		Messages: []agent.CompletionMessage{
			{Role: "user", Content: "What's the weather in San Francisco?"},
		},
		Tools: []agent.Tool{
			&ExampleWeatherTool{},
		},
		MaxTokens: 500,
	}

	chunks, err := provider.Complete(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Conversation:")
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}

		if chunk.ToolCall != nil {
			fmt.Printf("\n[Tool Call: %s]\n", chunk.ToolCall.Name)
			fmt.Printf("Arguments: %s\n", string(chunk.ToolCall.Input))

			// In a real application, you would execute the tool
			// and send the result back to continue the conversation
		}

		if chunk.Done {
			fmt.Println()
			break
		}
	}
}

// Example of listing available models
func ExampleOpenAIProvider_listModels() {
	provider := providers.NewOpenAIProvider("")

	fmt.Println("Available OpenAI models:")
	for _, model := range provider.Models() {
		fmt.Printf("- %s: %s (context: %dK, vision: %t)\n",
			model.ID,
			model.Name,
			model.ContextSize/1000,
			model.SupportsVision,
		)
	}
}

// Example of handling multiple images in one request
func ExampleOpenAIProvider_multipleImages() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	provider := providers.NewOpenAIProvider(apiKey)

	req := &agent.CompletionRequest{
		Model: "gpt-4o",
		Messages: []agent.CompletionMessage{
			{
				Role:    "user",
				Content: "Compare these two images and tell me the differences",
				Attachments: []models.Attachment{
					{
						ID:   "img1",
						Type: "image",
						URL:  "https://example.com/image1.jpg",
					},
					{
						ID:   "img2",
						Type: "image",
						URL:  "https://example.com/image2.jpg",
					},
				},
			},
		},
		MaxTokens: 500,
	}

	chunks, err := provider.Complete(context.Background(), req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Comparison:")
	for chunk := range chunks {
		if chunk.Error != nil {
			log.Printf("Error: %v", chunk.Error)
			break
		}
		if chunk.Text != "" {
			fmt.Print(chunk.Text)
		}
		if chunk.Done {
			fmt.Println()
			break
		}
	}
}
