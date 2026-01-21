# Telegram Channel Adapter

The Telegram adapter implements the `channels.Adapter` interface for the Nexus project, enabling integration with the Telegram Bot API.

## Features

- **Dual Operation Modes:**
  - Long polling (default) - suitable for most use cases
  - Webhook mode - for production deployments with public endpoints

- **Message Support:**
  - Text messages
  - Photos (images)
  - Documents (PDF, files, etc.)
  - Audio files

- **Advanced Features:**
  - Inline keyboards for interactive responses
  - Reply to messages
  - Message editing
  - Automatic reconnection with exponential backoff

- **Error Handling:**
  - Graceful error recovery
  - Configurable reconnection attempts
  - Detailed status reporting

## Usage

### Basic Setup (Long Polling)

```go
package main

import (
    "context"
    "log"

    "github.com/haasonsaas/nexus/internal/channels/telegram"
    "github.com/haasonsaas/nexus/pkg/models"
)

func main() {
    // Create adapter configuration
    config := telegram.Config{
        Token:    "YOUR_BOT_TOKEN",
        Mode:     telegram.ModeLongPolling,
        LogLevel: telegram.LogLevelInfo,
    }

    // Initialize adapter
    adapter, err := telegram.NewAdapter(config)
    if err != nil {
        log.Fatalf("Failed to create adapter: %v", err)
    }

    // Start listening for messages
    ctx := context.Background()
    go func() {
        if err := adapter.Start(ctx); err != nil {
            log.Printf("Adapter error: %v", err)
        }
    }()

    // Process incoming messages
    for msg := range adapter.Messages() {
        log.Printf("Received message: %s", msg.Content)

        // Send response
        response := &models.Message{
            SessionID: msg.SessionID,
            Channel:   models.ChannelTelegram,
            Direction: models.DirectionOutbound,
            Role:      models.RoleAssistant,
            Content:   "Hello! I received your message.",
            Metadata:  msg.Metadata, // Preserve chat_id for routing
        }

        if err := adapter.Send(ctx, response); err != nil {
            log.Printf("Failed to send message: %v", err)
        }
    }
}
```

### Webhook Mode

```go
config := telegram.Config{
    Token:      "YOUR_BOT_TOKEN",
    Mode:       telegram.ModeWebhook,
    WebhookURL: "https://yourdomain.com/webhook",
    ListenAddr: ":8443",
    LogLevel:   telegram.LogLevelInfo,
}

adapter, err := telegram.NewAdapter(config)
if err != nil {
    log.Fatalf("Failed to create adapter: %v", err)
}

// Start webhook server
ctx := context.Background()
if err := adapter.Start(ctx); err != nil {
    log.Fatalf("Failed to start webhook: %v", err)
}
```

### Sending Messages with Inline Keyboards

```go
import "github.com/go-telegram/bot/models"

response := &models.Message{
    SessionID: msg.SessionID,
    Channel:   models.ChannelTelegram,
    Direction: models.DirectionOutbound,
    Role:      models.RoleAssistant,
    Content:   "Choose an option:",
    Metadata: map[string]any{
        "chat_id": chatID,
        "inline_keyboard": &models.InlineKeyboardMarkup{
            InlineKeyboard: [][]models.InlineKeyboardButton{
                {
                    {Text: "Option 1", CallbackData: "opt1"},
                    {Text: "Option 2", CallbackData: "opt2"},
                },
            },
        },
    },
}

if err := adapter.Send(ctx, response); err != nil {
    log.Printf("Failed to send message: %v", err)
}
```

### Reply to Messages

```go
response := &models.Message{
    SessionID: msg.SessionID,
    Channel:   models.ChannelTelegram,
    Direction: models.DirectionOutbound,
    Role:      models.RoleAssistant,
    Content:   "This is a reply!",
    Metadata: map[string]any{
        "chat_id":            chatID,
        "reply_to_message_id": originalMessageID,
    },
}
```

## Configuration Options

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `Token` | string | Telegram Bot API token (required) | - |
| `Mode` | Mode | Operation mode: `ModeLongPolling` or `ModeWebhook` | `ModeLongPolling` |
| `WebhookURL` | string | Webhook URL (required for webhook mode) | - |
| `ListenAddr` | string | Address for webhook server | `:8443` |
| `LogLevel` | LogLevel | Logging verbosity: `LogLevelDebug`, `LogLevelInfo`, `LogLevelWarn`, `LogLevelError` | `LogLevelInfo` |
| `MaxReconnectAttempts` | int | Maximum reconnection attempts | 5 |
| `ReconnectDelay` | time.Duration | Delay between reconnection attempts | 5s |

## Message Format

The adapter converts Telegram messages to the unified `models.Message` format:

```go
type Message struct {
    ID          string            // Format: "tg_<message_id>"
    SessionID   string            // Format: "telegram:<chat_id>"
    Channel     ChannelType       // ChannelTelegram
    ChannelID   string            // Telegram message ID as string
    Direction   Direction         // DirectionInbound/DirectionOutbound
    Role        Role              // RoleUser/RoleAssistant
    Content     string            // Message text
    Attachments []Attachment      // Photos, documents, audio
    Metadata    map[string]any    // Additional data (chat_id, user_id, etc.)
    CreatedAt   time.Time         // Message timestamp
}
```

### Metadata Fields

Inbound messages include:
- `chat_id` (int64): Telegram chat ID
- `user_id` (int64): Sender's user ID
- `user_first` (string): Sender's first name
- `user_last` (string): Sender's last name

Outbound messages can include:
- `chat_id` (int64/string): Target chat ID (required)
- `inline_keyboard` (interface{}): Inline keyboard markup
- `reply_to_message_id` (int): Message ID to reply to

## Testing

Run the test suite:

```bash
go test ./internal/channels/telegram/
```

Run with verbose output:

```bash
go test -v ./internal/channels/telegram/
```

## Error Handling

The adapter includes automatic reconnection logic:

1. If the bot connection fails, it will attempt to reconnect
2. Reconnection attempts use exponential backoff (default: 5 seconds)
3. After max attempts (default: 5), the adapter stops
4. Connection status is available via `adapter.Status()`

```go
status := adapter.Status()
if !status.Connected {
    log.Printf("Adapter disconnected: %s", status.Error)
}
```

## Getting a Bot Token

1. Open Telegram and search for [@BotFather](https://t.me/botfather)
2. Send `/newbot` command
3. Follow the instructions to create your bot
4. Copy the provided token
5. Use the token in your adapter configuration

## Security Notes

- Never commit bot tokens to version control
- Use environment variables or secure configuration management
- For webhook mode, use HTTPS with valid certificates
- Consider implementing rate limiting for production use
- Validate webhook requests to prevent unauthorized access

## Dependencies

- [github.com/go-telegram/bot](https://github.com/go-telegram/bot) - Telegram Bot API client library

## License

Part of the Nexus project.
