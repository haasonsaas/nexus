# LLM Providers Package

This package provides production-ready LLM provider integrations for the Nexus agent framework. It implements the `agent.LLMProvider` interface for multiple LLM services, enabling unified access to different AI models through a consistent API.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Provider Implementations](#provider-implementations)
  - [Anthropic Provider](#anthropic-provider)
  - [OpenAI Provider](#openai-provider)
- [Core Concepts](#core-concepts)
- [Usage Guide](#usage-guide)
- [Error Handling](#error-handling)
- [Testing](#testing)
- [Contributing](#contributing)

## Architecture Overview

### Design Principles

The providers package follows these architectural principles:

1. **Unified Interface**: All providers implement `agent.LLMProvider`, enabling provider-agnostic code
2. **Streaming First**: Real-time token streaming for better UX and lower latency
3. **Production Ready**: Automatic retries, error handling, context cancellation
4. **Format Abstraction**: Hide provider-specific APIs behind common types
5. **Tool Support**: First-class support for function/tool calling across providers

### Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    Nexus Agent Runtime                       │
│                  (internal/agent/runtime.go)                 │
└───────────────────────┬─────────────────────────────────────┘
                        │ Uses
                        ▼
        ┌───────────────────────────────┐
        │   agent.LLMProvider Interface │
        │   ────────────────────────    │
        │   • Complete(req) -> chunks   │
        │   • Name() -> string          │
        │   • Models() -> []Model       │
        │   • SupportsTools() -> bool   │
        └───────────┬───────────────────┘
                    │ Implemented by
        ┌───────────┴───────────┬────────────────┐
        ▼                       ▼                ▼
┌──────────────────┐  ┌──────────────────┐   ┌─────┐
│ AnthropicProvider│  │  OpenAIProvider  │   │ ... │
├──────────────────┤  ├──────────────────┤   └─────┘
│ • Anthropic SDK  │  │ • go-openai SDK  │
│ • SSE Streaming  │  │ • Chat Streaming │
│ • Tool Calling   │  │ • Function Call  │
│ • Vision Support │  │ • Vision Support │
└──────────────────┘  └──────────────────┘
        │                       │
        └───────────┬───────────┘
                    │ Emits
                    ▼
        ┌───────────────────────┐
        │  CompletionChunk      │
        ├───────────────────────┤
        │  • Text: string       │
        │  • ToolCall: *ToolCall│
        │  • Done: bool         │
        │  • Error: error       │
        └───────────────────────┘
```

### Request/Response Flow

```
User Code
  │
  ├──> Complete(CompletionRequest)
  │      │
  │      ├─> Validate & Convert Messages
  │      ├─> Create Stream (with retries)
  │      └─> Spawn Goroutine
  │           │
  │           ├─> Process SSE/Stream Events
  │           ├─> Convert to CompletionChunk
  │           └─> Send to Channel
  │
  └──> for chunk := range chunks
         │
         ├─> chunk.Text (streaming response)
         ├─> chunk.ToolCall (function call request)
         ├─> chunk.Error (handle errors)
         └─> chunk.Done (completion)
```

## Provider Implementations

### Anthropic Provider

The Anthropic provider integrates with Claude models using the official Anthropic Go SDK.

#### Features

- **Server-Sent Events (SSE)**: Native SSE streaming for real-time responses
- **Tool Use**: Anthropic's tool use API for function calling
- **System Prompts**: Separate system parameter (not in messages array)
- **Multiple Models**: Claude Sonnet 4, Opus 4, Claude 3.5, Claude 3 family
- **Vision Support**: All Claude models support image understanding
- **Exponential Backoff**: Smart retry logic with exponential backoff
- **Context Windows**: 200K tokens across all modern Claude models

#### API Specifics

**Message Format**:
- System messages handled separately via `params.System`
- Content blocks: text, tool_use, tool_result
- Tool results embedded in user messages

**Streaming Events**:
- `message_start`: Stream initialization
- `content_block_start`: New content block (text or tool use)
- `content_block_delta`: Incremental updates (text or tool input JSON)
- `content_block_stop`: Content block complete
- `message_stop`: Stream complete

**Tool Calling**:
```go
// Tool use arrives as:
// 1. content_block_start with ToolUseBlock (id + name)
// 2. content_block_delta with InputJSONDelta (partial JSON)
// 3. content_block_stop (finalize complete JSON)
```

#### Configuration Example

```go
import "github.com/haasonsaas/nexus/internal/agent/providers"

provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
    APIKey:       os.Getenv("ANTHROPIC_API_KEY"),
    MaxRetries:   3,
    RetryDelay:   time.Second,
    DefaultModel: "claude-sonnet-4-20250514",
})
```

#### Supported Models

| Model | Context | Vision | Best For |
|-------|---------|--------|----------|
| claude-sonnet-4-20250514 | 200K | ✓ | Balanced performance & cost |
| claude-opus-4-20250514 | 200K | ✓ | Most capable, complex tasks |
| claude-3-5-sonnet-20241022 | 200K | ✓ | Previous generation |
| claude-3-opus-20240229 | 200K | ✓ | Legacy, high capability |
| claude-3-sonnet-20240229 | 200K | ✓ | Legacy, balanced |
| claude-3-haiku-20240307 | 200K | ✓ | Legacy, fast & efficient |

---

### OpenAI Provider

The OpenAI provider integrates with GPT models using the go-openai community SDK.

#### Features

- **Chat Completions Streaming**: OpenAI's streaming chat API
- **Function Calling**: OpenAI's tools/function calling with JSON mode
- **System Messages**: Injected as first message in array
- **Multiple Models**: GPT-4o, GPT-4 Turbo, GPT-3.5 Turbo, GPT-4
- **Vision Support**: Multi-content format for images (GPT-4o, GPT-4 Turbo)
- **Linear Backoff**: Retry logic with linear delay increases
- **Variable Context**: 8K-128K tokens depending on model

#### API Specifics

**Message Format**:
- System messages prepended to messages array
- Tool results require separate messages (one per result)
- Vision uses `MultiContent` field with parts array

**Streaming Deltas**:
- Text: `delta.Content` - emit immediately
- Tool calls: Incremental updates to ID, name, arguments
- Must accumulate tool call fragments across chunks
- `FinishReason` "tool_calls" signals completion

**Tool Calling**:
```go
// Function calls stream incrementally:
// 1. First delta: ID field
// 2. Second delta: Function.Name
// 3. Multiple deltas: Function.Arguments (JSON fragments)
// 4. FinishReason "tool_calls": All calls complete
```

#### Configuration Example

```go
import "github.com/haasonsaas/nexus/internal/agent/providers"

provider := providers.NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"))
// Note: OpenAI provider uses hardcoded defaults (3 retries, 1s delay)
```

#### Supported Models

| Model | Context | Vision | Best For |
|-------|---------|--------|----------|
| gpt-4o | 128K | ✓ | Latest multimodal, best overall |
| gpt-4-turbo | 128K | ✓ | Fast GPT-4 with vision |
| gpt-3.5-turbo | 16K | ✗ | Cost-effective, simple tasks |
| gpt-4 | 8K | ✗ | Original GPT-4, legacy |

---

## Core Concepts

### Streaming Architecture

Both providers use Go channels for streaming, but handle events differently:

**Anthropic (SSE)**:
```go
// Claude streams complete units
for stream.Next() {
    event := stream.Current()
    if textDelta := event.TextDelta(); textDelta != "" {
        chunks <- CompletionChunk{Text: textDelta}
    }
}
```

**OpenAI (Incremental Deltas)**:
```go
// GPT streams incremental updates
response, _ := stream.Recv()
delta := response.Choices[0].Delta
if delta.Content != "" {
    chunks <- CompletionChunk{Text: delta.Content}
}
```

### Tool/Function Calling

Both providers support tool calling but with different formats:

**Anthropic Tool Use**:
- Tools defined with input schema
- Tool use comes as content block
- Tool results embedded in messages

**OpenAI Function Calling**:
- Functions defined with parameters schema
- Function calls streamed incrementally
- Tool results as separate "tool" role messages

### Message Format Conversion

The providers handle conversion between internal and provider-specific formats:

**Internal Format** (Provider-Agnostic):
```go
type CompletionMessage struct {
    Role        string
    Content     string
    ToolCalls   []ToolCall
    ToolResults []ToolResult
    Attachments []Attachment
}
```

**Anthropic Format**:
```go
anthropic.MessageParam{
    Role: "user",
    Content: []ContentBlockParamUnion{
        TextBlock("Hello"),
        ToolResultBlock(toolCallID, result, isError),
    },
}
```

**OpenAI Format**:
```go
openai.ChatCompletionMessage{
    Role:    "user",
    Content: "Hello",  // OR MultiContent for vision
}
// Tool results as separate messages:
openai.ChatCompletionMessage{
    Role:       "tool",
    Content:    result,
    ToolCallID: toolCallID,
}
```

### Error Handling & Retries

Both providers implement retry logic but with different strategies:

**Anthropic**: Exponential backoff
```
Attempt 0: immediate
Attempt 1: 1s delay  (1s * 2^0)
Attempt 2: 2s delay  (1s * 2^1)
Attempt 3: 4s delay  (1s * 2^2)
```

**OpenAI**: Linear backoff
```
Attempt 0: immediate
Attempt 1: 1s delay  (1s * 1)
Attempt 2: 2s delay  (1s * 2)
Attempt 3: 3s delay  (1s * 3)
```

**Retryable Errors** (both providers):
- Rate limits: 429, "rate_limit", "too many requests"
- Server errors: 500, 502, 503, 504
- Timeouts: "timeout", "deadline exceeded"
- Network: "connection reset", "connection refused"

**Non-Retryable Errors** (both providers):
- Authentication: 401, 403 (invalid API key)
- Validation: 400 (bad request format)
- Not found: 404 (invalid endpoint/model)

---

## Usage Guide

### Basic Text Completion

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/agent/providers"
)

func main() {
    // Create provider (Anthropic or OpenAI)
    provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    // Build completion request
    req := &agent.CompletionRequest{
        Model:     "claude-sonnet-4-20250514",
        System:    "You are a helpful assistant.",
        Messages: []agent.CompletionMessage{
            {Role: "user", Content: "Explain quantum entanglement in simple terms."},
        },
        MaxTokens: 1024,
    }

    // Start streaming completion
    chunks, err := provider.Complete(context.Background(), req)
    if err != nil {
        log.Fatalf("Failed to create completion: %v", err)
    }

    // Process streaming response
    for chunk := range chunks {
        if chunk.Error != nil {
            log.Printf("Stream error: %v", chunk.Error)
            break
        }

        if chunk.Text != "" {
            fmt.Print(chunk.Text) // Print tokens as they arrive
        }

        if chunk.Done {
            fmt.Println() // Newline at end
            break
        }
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
### Vision Support

```go
req := &agent.CompletionRequest{
    Model: "gpt-4o", // or claude-sonnet-4-20250514
    Messages: []agent.CompletionMessage{
        {
            Role:    "user",
            Content: "What's in this image?",
            Attachments: []models.Attachment{
                {
                    Type: "image",
                    URL:  "https://example.com/photo.jpg",
                },
            },
        },
    },
}

chunks, err := provider.Complete(ctx, req)
// Process response...
```

### Tool/Function Calling

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
    var input struct {
        Location string `json:"location"`
    }
    if err := json.Unmarshal(params, &input); err != nil {
        return nil, err
    }

    // Fetch weather data...
    return &agent.ToolResult{
        Content: fmt.Sprintf("Weather in %s: Sunny, 72°F", input.Location),
    }, nil
}

// Use tool with provider
req := &agent.CompletionRequest{
    Model: "claude-sonnet-4-20250514",
    Messages: []agent.CompletionMessage{
        {Role: "user", Content: "What's the weather in San Francisco?"},
    },
    Tools: []agent.Tool{&WeatherTool{}},
}

chunks, err := provider.Complete(ctx, req)
for chunk := range chunks {
    if chunk.ToolCall != nil {
        fmt.Printf("Model wants to call: %s\n", chunk.ToolCall.Name)

        // Execute tool
        tool := &WeatherTool{}
        result, err := tool.Execute(ctx, chunk.ToolCall.Input)

        // Continue conversation with result...
        // (In practice, you'd create a new request with the tool result)
    }
    if chunk.Text != "" {
        fmt.Print(chunk.Text)
    }
}
```

### Context Cancellation & Timeouts

```go
// With timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

chunks, err := provider.Complete(ctx, req)
for chunk := range chunks {
    if chunk.Error != nil {
        if errors.Is(chunk.Error, context.DeadlineExceeded) {
            log.Println("Request timed out after 30 seconds")
        } else if errors.Is(chunk.Error, context.Canceled) {
            log.Println("Request was cancelled")
        }
        break
    }
    // Process chunks...
}

// Manual cancellation
ctx, cancel := context.WithCancel(context.Background())

// Start completion in goroutine
go func() {
    chunks, _ := provider.Complete(ctx, req)
    for chunk := range chunks {
        // Process...
    }
}()

// Cancel after some condition
time.Sleep(5 * time.Second)
cancel() // Stops the stream immediately
```

### Provider-Agnostic Code

```go
// Write code that works with any provider
func runCompletion(provider agent.LLMProvider, prompt string) (string, error) {
    req := &agent.CompletionRequest{
        Messages: []agent.CompletionMessage{
            {Role: "user", Content: prompt},
        },
    }

    chunks, err := provider.Complete(context.Background(), req)
    if err != nil {
        return "", err
    }

    var response strings.Builder
    for chunk := range chunks {
        if chunk.Error != nil {
            return "", chunk.Error
        }
        response.WriteString(chunk.Text)
    }

    return response.String(), nil
}

// Use with any provider
anthropic, _ := providers.NewAnthropicProvider(...)
openai := providers.NewOpenAIProvider(...)

result1, _ := runCompletion(anthropic, "Hello!")
result2, _ := runCompletion(openai, "Hello!")
```

---

## Error Handling

### Handling Stream Errors

```go
chunks, err := provider.Complete(ctx, req)
if err != nil {
    // Immediate failure (before streaming started)
    switch {
    case strings.Contains(err.Error(), "API key"):
        log.Fatal("Invalid API key - check configuration")
    case strings.Contains(err.Error(), "max retries exceeded"):
        log.Fatal("Service unavailable - try again later")
    default:
        log.Fatalf("Failed to start completion: %v", err)
    }
}

// Stream errors
for chunk := range chunks {
    if chunk.Error != nil {
        // Error during streaming
        if errors.Is(chunk.Error, context.Canceled) {
            return nil // Clean cancellation
        }
        return fmt.Errorf("stream error: %w", chunk.Error)
    }
    // Process chunk...
}
```

### Retry Strategies

Both providers implement automatic retries, but you can add application-level retries:

```go
func completeWithRetry(provider agent.LLMProvider, req *agent.CompletionRequest, maxAttempts int) (string, error) {
    var lastErr error

    for attempt := 0; attempt < maxAttempts; attempt++ {
        var response strings.Builder
        chunks, err := provider.Complete(context.Background(), req)
        if err != nil {
            lastErr = err
            time.Sleep(time.Second * time.Duration(attempt+1))
            continue
        }

        streamErr := false
        for chunk := range chunks {
            if chunk.Error != nil {
                lastErr = chunk.Error
                streamErr = true
                break
            }
            response.WriteString(chunk.Text)
        }

        if !streamErr {
            return response.String(), nil
        }

        time.Sleep(time.Second * time.Duration(attempt+1))
    }

    return "", fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}
```

### Token Counting

```go
// Anthropic provider includes CountTokens() for estimation
provider, _ := providers.NewAnthropicProvider(...)

tokens := provider.CountTokens(req)
if tokens > 190000 {
    return fmt.Errorf("request too large: %d tokens (max 200K)", tokens)
}

// Estimate costs (approximate)
costPer1kTokens := 0.003 // $0.003/1K for Claude Sonnet 4
estimatedCost := float64(tokens) / 1000.0 * costPer1kTokens
log.Printf("Estimated cost: $%.4f", estimatedCost)
```

---

## Testing

The package includes comprehensive tests covering all major functionality.

### Running Tests

```bash
# Run all provider tests
go test ./internal/agent/providers/... -v

# Run specific provider tests
go test ./internal/agent/providers/anthropic*.go -v
go test ./internal/agent/providers/openai*.go -v

# Run with race detection
go test ./internal/agent/providers/... -race -v

# Run example tests
go test ./internal/agent/providers/example_test.go -v
```

### Test Coverage

The test suite covers:

- ✓ Provider initialization and configuration
- ✓ Message format conversion (user, assistant, tool, system)
- ✓ Tool/function calling with complex schemas
- ✓ Vision support with image attachments
- ✓ Streaming response processing
- ✓ Error handling and retries
- ✓ Context cancellation
- ✓ Edge cases (empty messages, invalid JSON, etc.)

### Example Tests

See `example_test.go` and `anthropic_example_test.go` for runnable examples that demonstrate:

- Basic completions
- Multi-turn conversations
- Tool execution loops
- Error handling patterns
- Vision queries

---

## Best Practices

### 1. Always Use Contexts with Timeouts

```go
// Good: Prevents hanging requests
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
chunks, err := provider.Complete(ctx, req)

// Bad: No timeout, request could hang forever
chunks, err := provider.Complete(context.Background(), req)
```

### 2. Handle Both Immediate and Stream Errors

```go
// Check both error types
chunks, err := provider.Complete(ctx, req)
if err != nil {
    // Handle immediate errors
}

for chunk := range chunks {
    if chunk.Error != nil {
        // Handle streaming errors
    }
}
```

### 3. Accumulate Complete Responses

```go
// Use strings.Builder for efficient accumulation
var response strings.Builder
for chunk := range chunks {
    if chunk.Error != nil {
        return "", chunk.Error
    }
    if chunk.Text != "" {
        response.WriteString(chunk.Text)
    }
}
return response.String(), nil
```

### 4. Validate Tool Schemas

```go
// Ensure tool schemas are valid JSON before use
func (t *MyTool) Schema() json.RawMessage {
    schema := map[string]any{
        "type": "object",
        "properties": map[string]any{
            "param": map[string]any{
                "type": "string",
                "description": "Parameter description",
            },
        },
        "required": []string{"param"},
    }

    data, _ := json.Marshal(schema)
    return json.RawMessage(data)
}
```

### 5. Monitor Token Usage

```go
// Check token count before expensive requests
if provider.CountTokens(req) > model.ContextSize*0.9 {
    return errors.New("request too large for context window")
}
```

### 6. Use Provider-Agnostic Interfaces

```go
// Design interfaces around agent.LLMProvider, not specific providers
type ChatService struct {
    llm agent.LLMProvider
}

func NewChatService(provider agent.LLMProvider) *ChatService {
    return &ChatService{llm: provider}
}
```

### 7. Log Errors with Context

```go
for chunk := range chunks {
    if chunk.Error != nil {
        log.Printf("Stream error for user %s, model %s: %v",
            userID, req.Model, chunk.Error)
        return chunk.Error
    }
}
```

---

## Contributing

### Adding a New Provider

To add support for a new LLM provider:

1. **Create provider file**: `{provider}_provider.go`
2. **Implement interface**:
```go
type MyProvider struct {
    client  *myclient.Client
    apiKey  string
    // ...
}

func (p *MyProvider) Name() string { return "myprovider" }
func (p *MyProvider) Models() []agent.Model { /* ... */ }
func (p *MyProvider) SupportsTools() bool { /* ... */ }
func (p *MyProvider) Complete(ctx, req) (<-chan *agent.CompletionChunk, error) { /* ... */ }
```

3. **Handle format conversion**:
   - Implement message format conversion
   - Handle tool/function calling format
   - Support vision if available

4. **Add streaming support**:
   - Process provider's streaming format
   - Convert to CompletionChunk
   - Handle errors and completion

5. **Add tests**:
   - Unit tests for conversion functions
   - Integration tests (if possible)
   - Example tests

6. **Document**:
   - Add GoDoc comments
   - Update README with provider details
   - Add usage examples

### Code Style

- Follow Go best practices and conventions
- Use meaningful variable names
- Add comprehensive GoDoc comments
- Include usage examples in comments
- Document error conditions
- Use inline comments for complex logic

---

## License

See the main repository LICENSE file.

## Support

For issues, questions, or contributions, please open an issue in the GitHub repository.
