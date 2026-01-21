# OpenAI Provider Integration Guide

This guide shows how to integrate the OpenAI provider with the Nexus agent runtime.

## Quick Start

### 1. Basic Setup

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/agent/providers"
    "github.com/haasonsaas/nexus/internal/sessions"
    "github.com/haasonsaas/nexus/pkg/models"
)

func main() {
    // 1. Create OpenAI provider
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey)

    // 2. Create session store (implement your own or use memory store)
    sessionStore := sessions.NewMemoryStore()

    // 3. Create agent runtime
    runtime := agent.NewRuntime(provider, sessionStore)

    // 4. Register tools (optional)
    // runtime.RegisterTool(&MyCustomTool{})

    // 5. Create a session
    session := &models.Session{
        ID:      "session-1",
        AgentID: "agent-1",
        Channel: models.ChannelTelegram,
    }

    // 6. Process a message
    msg := &models.Message{
        Role:    models.RoleUser,
        Content: "Hello! Tell me about yourself.",
    }

    chunks, err := runtime.Process(context.Background(), session, msg)
    if err != nil {
        log.Fatal(err)
    }

    // 7. Handle response
    for chunk := range chunks {
        if chunk.Error != nil {
            log.Printf("Error: %v", chunk.Error)
            break
        }

        if chunk.Text != "" {
            fmt.Print(chunk.Text)
        }

        if chunk.ToolResult != nil {
            fmt.Printf("\n[Tool Result: %s]\n", chunk.ToolResult.Content)
        }
    }
}
```

## Advanced Usage

### Custom Configuration

```go
// Create provider with custom settings
provider := providers.NewOpenAIProvider(apiKey)

// The provider has built-in rate limiting:
// - maxRetries: 3 attempts
// - retryDelay: exponential backoff starting at 1 second
// - Automatically retries on 429, 500, 502, 503, 504 errors
```

### Model Selection

```go
// List available models
for _, model := range provider.Models() {
    fmt.Printf("%s: context=%d, vision=%t\n",
        model.ID,
        model.ContextSize,
        model.SupportsVision,
    )
}

// Use in completion request
req := &agent.CompletionRequest{
    Model: "gpt-4o",  // Best for complex tasks
    // Model: "gpt-4-turbo",  // Fast GPT-4
    // Model: "gpt-3.5-turbo",  // Cost-effective
}
```

### System Prompts

```go
req := &agent.CompletionRequest{
    Model: "gpt-4o",
    System: `You are a helpful coding assistant.
             You provide clear, concise explanations.
             You write clean, maintainable code.`,
    Messages: []agent.CompletionMessage{
        {Role: "user", Content: "How do I sort a slice in Go?"},
    },
}
```

### Vision/Image Analysis

```go
req := &agent.CompletionRequest{
    Model: "gpt-4o",  // Must use vision-capable model
    Messages: []agent.CompletionMessage{
        {
            Role:    "user",
            Content: "What objects are in this image?",
            Attachments: []models.Attachment{
                {
                    Type: "image",
                    URL:  "https://example.com/image.jpg",
                    // Supports both URLs and base64 data URLs
                },
            },
        },
    },
}
```

### Function/Tool Calling

```go
// Define a custom tool
type DatabaseQueryTool struct {
    db *sql.DB
}

func (t *DatabaseQueryTool) Name() string {
    return "query_database"
}

func (t *DatabaseQueryTool) Description() string {
    return "Query the database for user information"
}

func (t *DatabaseQueryTool) Schema() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "user_id": {
                "type": "string",
                "description": "The user ID to query"
            }
        },
        "required": ["user_id"]
    }`)
}

func (t *DatabaseQueryTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
    var args struct {
        UserID string `json:"user_id"`
    }

    if err := json.Unmarshal(params, &args); err != nil {
        return nil, err
    }

    // Query database
    var username string
    err := t.db.QueryRowContext(ctx, "SELECT username FROM users WHERE id = ?", args.UserID).Scan(&username)
    if err != nil {
        return &agent.ToolResult{
            Content: fmt.Sprintf("Error: %v", err),
            IsError: true,
        }, nil
    }

    return &agent.ToolResult{
        Content: fmt.Sprintf("User: %s", username),
        IsError: false,
    }, nil
}

// Use with runtime
runtime.RegisterTool(&DatabaseQueryTool{db: myDB})
```

## Error Handling

### Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

chunks, err := provider.Complete(ctx, req)
if err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        log.Println("Request timed out")
    }
    return
}
```

### Rate Limiting

```go
// The provider automatically handles rate limits
// with exponential backoff. You can detect rate
// limit errors in the stream:

for chunk := range chunks {
    if chunk.Error != nil {
        if strings.Contains(chunk.Error.Error(), "rate limit") {
            log.Println("Hit rate limit, retrying...")
        }
        break
    }
}
```

### API Key Errors

```go
provider := providers.NewOpenAIProvider("")  // Empty key
chunks, err := provider.Complete(ctx, req)
if err != nil {
    // Will return: "OpenAI API key not configured"
    log.Printf("Configuration error: %v", err)
}
```

## Best Practices

### 1. Token Management

```go
// Check model context size
model := provider.Models()[0]  // Get first model
if model.ContextSize < estimatedTokens {
    log.Printf("Warning: Request may exceed context size")
}

// Set max tokens for response
req.MaxTokens = 1000  // Limit response length
```

### 2. Streaming vs Buffering

```go
// Streaming (recommended for UX)
for chunk := range chunks {
    if chunk.Text != "" {
        // Display immediately
        fmt.Print(chunk.Text)
        io.WriteString(writer, chunk.Text)
    }
}

// Buffering (for processing complete response)
var fullResponse string
for chunk := range chunks {
    if chunk.Text != "" {
        fullResponse += chunk.Text
    }
    if chunk.Done {
        processCompleteResponse(fullResponse)
    }
}
```

### 3. Tool Execution Loop

```go
for {
    chunks, err := provider.Complete(ctx, req)
    if err != nil {
        return err
    }

    var toolCalls []models.ToolCall
    for chunk := range chunks {
        if chunk.ToolCall != nil {
            toolCalls = append(toolCalls, *chunk.ToolCall)
        }
    }

    if len(toolCalls) == 0 {
        break  // No more tool calls, conversation done
    }

    // Execute tools and add results to conversation
    for _, tc := range toolCalls {
        result := executeTool(tc)
        req.Messages = append(req.Messages, agent.CompletionMessage{
            Role: "tool",
            ToolResults: []models.ToolResult{result},
        })
    }
    // Continue conversation with tool results
}
```

### 4. Resource Cleanup

```go
// Always consume the channel completely
defer func() {
    // Drain channel if loop exits early
    for range chunks {
    }
}()

for chunk := range chunks {
    if shouldStop() {
        return  // Channel will be drained by defer
    }
    // Process chunk
}
```

## Testing

### Unit Tests

See `openai_test.go` for comprehensive examples of:
- Message conversion
- Tool definition
- Error handling
- Vision support
- Rate limiting

### Integration Tests

```go
func TestOpenAIIntegration(t *testing.T) {
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        t.Skip("OPENAI_API_KEY not set")
    }

    provider := providers.NewOpenAIProvider(apiKey)
    req := &agent.CompletionRequest{
        Model: "gpt-3.5-turbo",
        Messages: []agent.CompletionMessage{
            {Role: "user", Content: "Say 'test'"},
        },
    }

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    chunks, err := provider.Complete(ctx, req)
    require.NoError(t, err)

    var gotResponse bool
    for chunk := range chunks {
        require.NoError(t, chunk.Error)
        if chunk.Text != "" {
            gotResponse = true
        }
    }

    assert.True(t, gotResponse, "Should receive response")
}
```

## Monitoring

### Request Logging

```go
// Log all requests
req := &agent.CompletionRequest{...}
log.Printf("OpenAI request: model=%s, messages=%d, tools=%d",
    req.Model,
    len(req.Messages),
    len(req.Tools),
)

start := time.Now()
chunks, err := provider.Complete(ctx, req)
// ... process chunks
log.Printf("OpenAI response: duration=%v", time.Since(start))
```

### Token Tracking

```go
// Track token usage (approximate)
var textLength int
for chunk := range chunks {
    textLength += len(chunk.Text)
}
estimatedTokens := textLength / 4  // Rough estimate
log.Printf("Estimated tokens used: %d", estimatedTokens)
```

## Troubleshooting

### Common Issues

1. **"API key not configured"**
   - Ensure `OPENAI_API_KEY` environment variable is set
   - Verify API key is valid in OpenAI dashboard

2. **Rate limit errors**
   - Provider automatically retries with backoff
   - Consider implementing application-level rate limiting
   - Upgrade OpenAI plan if hitting limits frequently

3. **Context length errors**
   - Check total tokens (input + output) against model's context size
   - Truncate older messages in conversation history
   - Use models with larger context (gpt-4o: 128K)

4. **Vision not working**
   - Ensure using vision-capable model (gpt-4o, gpt-4-turbo)
   - Verify image URL is accessible
   - Check image format is supported (JPEG, PNG, GIF, WebP)

5. **Tool calls not working**
   - Verify tool schema is valid JSON Schema
   - Ensure tool names are descriptive
   - Check that model supports function calling (all current models do)
