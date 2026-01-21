# LLM Providers

This package contains implementations of the `agent.LLMProvider` interface for various LLM services.

## OpenAI Provider

The OpenAI provider implements support for OpenAI's API with the following features:

### Features

- **Streaming responses**: Real-time token streaming for better UX
- **Function calling**: Full support for OpenAI's tools/function calling API
- **System prompts**: Configure agent behavior with system messages
- **Multiple models**: Support for GPT-4o, GPT-4 Turbo, GPT-3.5 Turbo, and GPT-4
- **Vision support**: Send images to vision-capable models (GPT-4o, GPT-4 Turbo)
- **Rate limiting**: Automatic retries with exponential backoff for rate limits and server errors
- **Error handling**: Comprehensive error handling with retry logic

### Usage

```go
import (
    "context"
    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/agent/providers"
)

// Create provider
provider := providers.NewOpenAIProvider("your-api-key")

// Basic text completion
req := &agent.CompletionRequest{
    Model:     "gpt-4o",
    System:    "You are a helpful assistant.",
    Messages: []agent.CompletionMessage{
        {Role: "user", Content: "Hello, how are you?"},
    },
    MaxTokens: 1000,
}

chunks, err := provider.Complete(context.Background(), req)
if err != nil {
    panic(err)
}

// Process streaming response
for chunk := range chunks {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }

    if chunk.Text != "" {
        fmt.Print(chunk.Text)
    }

    if chunk.ToolCall != nil {
        // Handle tool call
        fmt.Printf("Tool call: %s(%s)\n", chunk.ToolCall.Name, chunk.ToolCall.Input)
    }

    if chunk.Done {
        break
    }
}
```

### Vision Support

```go
req := &agent.CompletionRequest{
    Model: "gpt-4o",
    Messages: []agent.CompletionMessage{
        {
            Role:    "user",
            Content: "What's in this image?",
            Attachments: []models.Attachment{
                {
                    Type: "image",
                    URL:  "https://example.com/image.jpg",
                },
            },
        },
    },
}

chunks, err := provider.Complete(context.Background(), req)
// ... process response
```

### Function Calling

```go
// Define a tool
type WeatherTool struct{}

func (t *WeatherTool) Name() string {
    return "get_weather"
}

func (t *WeatherTool) Description() string {
    return "Get current weather for a location"
}

func (t *WeatherTool) Schema() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "location": {"type": "string", "description": "City name"}
        },
        "required": ["location"]
    }`)
}

func (t *WeatherTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
    // Implementation...
    return &agent.ToolResult{Content: "Sunny, 72F"}, nil
}

// Use tool with provider
req := &agent.CompletionRequest{
    Model: "gpt-4o",
    Messages: []agent.CompletionMessage{
        {Role: "user", Content: "What's the weather in NYC?"},
    },
    Tools: []agent.Tool{&WeatherTool{}},
}

chunks, err := provider.Complete(context.Background(), req)
for chunk := range chunks {
    if chunk.ToolCall != nil {
        fmt.Printf("Calling: %s\n", chunk.ToolCall.Name)
        // Execute tool and send result back...
    }
}
```

### Supported Models

| Model | Context Size | Vision | Use Case |
|-------|--------------|--------|----------|
| gpt-4o | 128K | Yes | Best for complex tasks, vision, and function calling |
| gpt-4-turbo | 128K | Yes | Fast GPT-4 with vision support |
| gpt-3.5-turbo | 16K | No | Fast and cost-effective for simple tasks |
| gpt-4 | 8K | No | Original GPT-4 model |

### Rate Limiting

The provider automatically handles rate limits with:
- Maximum 3 retries by default
- Exponential backoff
- Automatic retry for 429, 500, 502, 503, 504 errors
- Context-aware cancellation

### Error Handling

```go
chunks, err := provider.Complete(ctx, req)
if err != nil {
    if errors.Is(err, context.Canceled) {
        // Request was cancelled
    } else if strings.Contains(err.Error(), "API key") {
        // Authentication error
    } else {
        // Other error
    }
}

for chunk := range chunks {
    if chunk.Error != nil {
        // Streaming error occurred
        log.Printf("Stream error: %v", chunk.Error)
        break
    }
}
```

## Testing

The provider includes comprehensive tests covering:
- Message format conversion
- Tool/function calling
- Vision support
- Error handling
- Rate limiting logic
- Streaming responses

Run tests:
```bash
go test ./internal/agent/providers/openai*.go -v
```
