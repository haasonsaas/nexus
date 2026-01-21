# Channel Adapters

Production-ready channel adapters for Telegram, Discord, and Slack messaging platforms.

## Overview

This package provides a unified interface for interacting with multiple messaging platforms. Each adapter implements the `Adapter` interface with comprehensive production features:

- **Structured Logging**: Using `log/slog` with contextual fields for observability
- **Error Handling**: Typed errors with error codes for different failure modes
- **Rate Limiting**: Token bucket algorithm to prevent API throttling
- **Metrics Collection**: Counters and histograms for monitoring
- **Health Checks**: Connectivity verification endpoints
- **Graceful Degradation**: Automatic reconnection with backoff strategies

## Architecture

```
channels/
├── channel.go       # Base Adapter interface and Registry
├── errors.go        # Typed error codes and error handling
├── metrics.go       # Metrics collection infrastructure
├── ratelimit.go     # Rate limiting implementation
├── telegram/        # Telegram adapter
├── discord/         # Discord adapter
└── slack/           # Slack adapter
```

## Features

### 1. Structured Logging

All adapters use `log/slog` for structured, contextual logging:

```go
logger.Info("starting telegram adapter",
    "mode", config.Mode,
    "rate_limit", config.RateLimit)

logger.Debug("sending message",
    "chat_id", chatID,
    "content_length", len(msg.Content))
```

### 2. Typed Error Codes

Errors are categorized with error codes for better monitoring and handling:

```go
// Error codes
type ErrorCode string

const (
    ErrCodeConnection     ErrorCode = "CONNECTION_ERROR"
    ErrCodeAuthentication ErrorCode = "AUTH_ERROR"
    ErrCodeRateLimit      ErrorCode = "RATE_LIMIT_ERROR"
    ErrCodeInvalidInput   ErrorCode = "INVALID_INPUT"
    ErrCodeTimeout        ErrorCode = "TIMEOUT_ERROR"
    // ... and more
)

// Creating errors
err := channels.ErrRateLimit("telegram rate limit exceeded", baseErr)
```

### 3. Rate Limiting

Token bucket rate limiter prevents API throttling:

```go
// Configure rate limiting
config := telegram.Config{
    Token:     "your-token",
    RateLimit: 30,  // 30 operations per second
    RateBurst: 20,  // Allow bursts up to 20
}

// Rate limiting is automatically applied to all operations
adapter.Send(ctx, message) // Waits if rate limit is exceeded
```

### 4. Metrics Collection

Track operational metrics for monitoring:

```go
// Get metrics snapshot
snapshot := adapter.Metrics()

fmt.Printf("Messages sent: %d\n", snapshot.MessagesSent)
fmt.Printf("Messages received: %d\n", snapshot.MessagesReceived)
fmt.Printf("P95 latency: %v\n", snapshot.SendLatency.P95)
fmt.Printf("Error rate: %d\n", len(snapshot.ErrorsByCode))
```

Available metrics:
- Message counters (sent, received, failed)
- Error counters by error code
- Latency histograms (min, max, mean, P50, P95, P99)
- Connection events (opened, closed, reconnect attempts)

### 5. Health Checks

Verify adapter connectivity and health:

```go
health := adapter.HealthCheck(ctx)

if health.Healthy {
    fmt.Printf("Healthy (latency: %v)\n", health.Latency)
} else {
    fmt.Printf("Unhealthy: %s\n", health.Message)
}

if health.Degraded {
    fmt.Printf("Operating in degraded mode\n")
}
```

### 6. Graceful Degradation

Automatic reconnection with exponential backoff:

- Detects connection failures
- Enters degraded mode during reconnection attempts
- Exponential backoff strategy (1s, 2s, 4s, 8s, ...)
- Configurable max reconnection attempts
- Continues accepting operations during degradation

## Usage Examples

### Telegram Adapter

```go
import (
    "context"
    "log/slog"
    "github.com/haasonsaas/nexus/internal/channels/telegram"
)

// Configure adapter
config := telegram.Config{
    Token:                "your-bot-token",
    Mode:                 telegram.ModeLongPolling,
    MaxReconnectAttempts: 5,
    ReconnectDelay:       5 * time.Second,
    RateLimit:            30,
    RateBurst:            20,
    Logger:               slog.Default(),
}

// Create adapter
adapter, err := telegram.NewAdapter(config)
if err != nil {
    log.Fatal(err)
}

// Start adapter
ctx := context.Background()
if err := adapter.Start(ctx); err != nil {
    log.Fatal(err)
}
defer adapter.Stop(context.WithTimeout(ctx, 10*time.Second))

// Send message
msg := &models.Message{
    Content: "Hello from Telegram!",
    Metadata: map[string]any{
        "chat_id": 123456789,
    },
}

if err := adapter.Send(ctx, msg); err != nil {
    log.Printf("Failed to send: %v", err)
}

// Receive messages
for msg := range adapter.Messages() {
    log.Printf("Received: %s", msg.Content)
}
```

### Discord Adapter

```go
import (
    "github.com/haasonsaas/nexus/internal/channels/discord"
)

config := discord.Config{
    Token:                "your-bot-token",
    MaxReconnectAttempts: 5,
    ReconnectBackoff:     60 * time.Second,
    RateLimit:            5,
    RateBurst:            10,
    Logger:               slog.Default(),
}

adapter, err := discord.NewAdapter(config)
// ... use similar to Telegram
```

### Slack Adapter

```go
import (
    "github.com/haasonsaas/nexus/internal/channels/slack"
)

config := slack.Config{
    BotToken:  "xoxb-your-bot-token",
    AppToken:  "xapp-your-app-token",
    RateLimit: 1,  // Slack's rate limit
    RateBurst: 5,
    Logger:    slog.Default(),
}

adapter, err := slack.NewAdapter(config)
// ... use similar to Telegram
```

## Registry

Manage multiple adapters with the Registry:

```go
registry := channels.NewRegistry()

// Register adapters
registry.Register(telegramAdapter)
registry.Register(discordAdapter)
registry.Register(slackAdapter)

// Start all adapters
if err := registry.StartAll(ctx); err != nil {
    log.Fatal(err)
}

// Aggregate messages from all channels
messages := registry.AggregateMessages(ctx)
for msg := range messages {
    log.Printf("[%s] %s", msg.Channel, msg.Content)
}

// Stop all adapters
registry.StopAll(context.WithTimeout(ctx, 30*time.Second))
```

## Error Handling

Handle errors with proper classification:

```go
if err := adapter.Send(ctx, msg); err != nil {
    var chErr *channels.Error
    if errors.As(err, &chErr) {
        switch chErr.Code {
        case channels.ErrCodeRateLimit:
            // Wait and retry
            time.Sleep(time.Second)
            return adapter.Send(ctx, msg)

        case channels.ErrCodeConnection:
            // Check health status
            health := adapter.HealthCheck(ctx)
            if health.Degraded {
                log.Warn("Adapter in degraded mode, may recover")
            }

        case channels.ErrCodeInvalidInput:
            // Don't retry, fix the input
            log.Error("Invalid message format")
            return err
        }
    }
}
```

## Best Practices

### 1. Always Use Context

Pass context for proper cancellation and timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := adapter.Send(ctx, msg); err != nil {
    // Handle error
}
```

### 2. Monitor Metrics

Regularly check metrics for health monitoring:

```go
ticker := time.NewTicker(1 * time.Minute)
for range ticker.C {
    snapshot := adapter.Metrics()

    // Alert on high error rate
    errorRate := float64(snapshot.MessagesFailed) /
                float64(snapshot.MessagesSent + snapshot.MessagesFailed)
    if errorRate > 0.1 {
        log.Warn("High error rate", "rate", errorRate)
    }

    // Alert on high latency
    if snapshot.SendLatency.P95 > 5*time.Second {
        log.Warn("High latency", "p95", snapshot.SendLatency.P95)
    }
}
```

### 3. Implement Health Checks

Run periodic health checks:

```go
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    health := adapter.HealthCheck(ctx)
    cancel()

    if !health.Healthy {
        log.Error("Health check failed", "message", health.Message)
        // Alert or take corrective action
    }
}
```

### 4. Graceful Shutdown

Always allow time for graceful shutdown:

```go
// Handle shutdown signal
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

<-sigChan
log.Info("Shutting down...")

// Stop with timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := adapter.Stop(ctx); err != nil {
    log.Error("Failed to stop gracefully", "error", err)
}
```

## Configuration

### Environment Variables

Adapters support configuration via environment variables:

```bash
# Telegram
TELEGRAM_BOT_TOKEN=your-token
TELEGRAM_MODE=long_polling
TELEGRAM_RATE_LIMIT=30

# Discord
DISCORD_BOT_TOKEN=your-token
DISCORD_RATE_LIMIT=5

# Slack
SLACK_BOT_TOKEN=xoxb-your-token
SLACK_APP_TOKEN=xapp-your-token
SLACK_RATE_LIMIT=1
```

### Rate Limits by Platform

Default rate limits based on platform guidelines:

- **Telegram**: 30 messages/second (burst: 20)
- **Discord**: 5 messages/second (burst: 10)
- **Slack**: 1 message/second (burst: 5)

Adjust based on your plan and requirements.

## Testing

All adapters support dependency injection for testing:

```go
// Mock the underlying client
type mockTelegramBot struct {
    sendFunc func(context.Context, *bot.SendMessageParams) (*models.Message, error)
}

func TestAdapter(t *testing.T) {
    adapter := telegram.NewAdapter(config)
    // Inject mock
    adapter.bot = mockBot

    // Test your logic
}
```

## Performance Considerations

1. **Channel Buffering**: Message channels are buffered (100 messages) to prevent blocking
2. **Rate Limiting**: Applied per-adapter with token bucket algorithm
3. **Metrics**: Lock-free atomic operations for counters, minimal overhead
4. **Logging**: Structured logging with appropriate log levels (use Debug for verbose output)

## Contributing

When adding new adapters:

1. Implement the `Adapter` interface
2. Use structured logging with `log/slog`
3. Apply rate limiting with `channels.RateLimiter`
4. Record metrics with `channels.Metrics`
5. Return typed errors with `channels.Error`
6. Implement health checks
7. Support graceful degradation
8. Add comprehensive GoDoc comments

## License

See project root LICENSE file.
