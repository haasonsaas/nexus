# Slack Channel Adapter

This package implements a Slack adapter for the Nexus messaging platform. It provides a unified interface for sending and receiving messages via Slack's Socket Mode API.

## Features

- **Socket Mode Connection**: Real-time bidirectional communication with automatic reconnection
- **App Mentions**: Responds to @bot mentions in channels
- **Direct Messages**: Handles DM conversations with the bot
- **Thread Support**: Maintains conversation context in threaded messages
- **Block Kit**: Rich message formatting using Slack's Block Kit
- **File Handling**: Processes file attachments in messages
- **Reactions**: Supports adding emoji reactions to messages
- **Session Management**: Deterministic session IDs for thread-based conversations

## Configuration

To use the Slack adapter, you need two tokens:

1. **Bot Token** (`xoxb-...`): For making API calls
2. **App Token** (`xapp-...`): For Socket Mode connections

### Required Slack App Scopes

Your Slack app needs the following bot token scopes:

- `app_mentions:read` - To receive @mentions
- `channels:history` - To read channel messages
- `channels:read` - To view channel information
- `chat:write` - To send messages
- `files:read` - To access file attachments
- `files:write` - To upload file attachments (required when UploadAttachments is enabled)
- `im:history` - To read direct messages
- `im:read` - To view direct message channels
- `im:write` - To send direct messages
- `reactions:write` - To add emoji reactions
- `users:read` - To get user information

### Socket Mode Setup

Enable Socket Mode in your Slack app settings and generate an app-level token with `connections:write` scope.

## Usage

### Basic Setup

```go
import (
    "context"
    "github.com/haasonsaas/nexus/internal/channels/slack"
    "github.com/haasonsaas/nexus/pkg/models"
)

// Create adapter configuration
cfg := slack.Config{
    BotToken: "xoxb-your-bot-token",
    AppToken: "xapp-your-app-level-token",
    UploadAttachments: true, // Upload outbound attachments as files
}

// Initialize the adapter
adapter := slack.NewAdapter(cfg)

// Start listening for messages
ctx := context.Background()
if err := adapter.Start(ctx); err != nil {
    log.Fatal(err)
}
defer adapter.Stop(ctx)
```

When `UploadAttachments` is enabled, attachment URLs must be reachable by the server so they can be fetched and uploaded to Slack.

### Receiving Messages

```go
// Process incoming messages
for msg := range adapter.Messages() {
    fmt.Printf("From: %s\n", msg.Metadata["slack_user_id"])
    fmt.Printf("Channel: %s\n", msg.Metadata["slack_channel"])
    fmt.Printf("Content: %s\n", msg.Content)

    // Check if it's a thread reply
    if threadTS, ok := msg.Metadata["slack_thread_ts"].(string); ok && threadTS != "" {
        fmt.Printf("In thread: %s\n", threadTS)
    }

    // Process file attachments
    for _, att := range msg.Attachments {
        fmt.Printf("File: %s (%s)\n", att.Filename, att.Type)
    }
}
```

### Sending Messages

#### Simple Text Message

```go
msg := &models.Message{
    Content: "Hello, Slack!",
    Metadata: map[string]any{
        "slack_channel": "C123456789", // Channel ID
    },
}
adapter.Send(ctx, msg)
```

#### Reply in Thread

```go
msg := &models.Message{
    Content: "This is a thread reply",
    Metadata: map[string]any{
        "slack_channel":   "C123456789",
        "slack_thread_ts": "1234567890.123456", // Parent message timestamp
    },
}
adapter.Send(ctx, msg)
```

#### Message with Images

```go
msg := &models.Message{
    Content: "Check out these images:",
    Attachments: []models.Attachment{
        {
            Type:     "image",
            URL:      "https://example.com/image.png",
            Filename: "image.png",
        },
    },
    Metadata: map[string]any{
        "slack_channel": "C123456789",
    },
}
adapter.Send(ctx, msg)
```

#### Add Reaction to Message

```go
msg := &models.Message{
    Content: "Great job!",
    Metadata: map[string]any{
        "slack_channel":  "C123456789",
        "slack_reaction": "tada", // Adds :tada: emoji
    },
}
adapter.Send(ctx, msg)
```

## Message Metadata

The adapter uses the following metadata fields:

### Inbound Messages (Slack → Nexus)

- `slack_user_id`: Slack user ID who sent the message
- `slack_channel`: Slack channel ID where message was sent
- `slack_ts`: Message timestamp (unique identifier)
- `slack_thread_ts`: Thread parent timestamp (if in thread)

### Outbound Messages (Nexus → Slack)

Required:
- `slack_channel`: Target channel ID

Optional:
- `slack_thread_ts`: Parent message timestamp for thread replies
- `slack_reaction`: Emoji name for reaction (without colons)

## Message Conversion

The adapter converts between Slack's format and the unified `models.Message`:

### Text Processing

- Removes bot mention tags (`<@BOT_ID>`) from message text
- Preserves Slack's markdown formatting
- Handles multi-line messages

### Attachment Types

Files are categorized by MIME type:
- `image/*` → `"image"`
- `audio/*` → `"audio"`
- `video/*` → `"video"`
- Everything else → `"document"`

### Session Management

Each conversation thread gets a deterministic session ID:
```
SHA256("slack:{channel_id}:{thread_timestamp}")
```

This ensures consistent session tracking across message exchanges.

## Block Kit Messages

Outbound messages are formatted using Slack's Block Kit for rich presentation:

- Text content → Section blocks with markdown
- Images → Image blocks (displayed inline)
- Other files → Context blocks with file metadata

## Error Handling

The adapter handles various error scenarios:

- **Connection Loss**: Automatic reconnection via Socket Mode
- **API Rate Limits**: Inherent to slack-go/slack library
- **Invalid Tokens**: Fails during `Start()` with auth error
- **Missing Metadata**: Returns error from `Send()` if required fields missing

## Testing

The package includes comprehensive tests:

```bash
go test ./internal/channels/slack/... -v
```

Tests cover:
- Adapter lifecycle (start/stop)
- Message conversion (Slack ↔ Unified format)
- Block Kit message building
- Attachment type detection
- Session ID generation
- Interface compliance

## Architecture

### Components

1. **Adapter**: Main struct implementing `channels.Adapter` interface
2. **Socket Mode Client**: Manages WebSocket connection to Slack
3. **Event Handler**: Processes incoming Slack events
4. **Message Converter**: Transforms between formats
5. **Block Kit Builder**: Creates rich message layouts

### Concurrency

- Event handling runs in separate goroutine
- Socket Mode client runs in separate goroutine
- Messages channel is buffered (100 messages)
- Thread-safe status updates with mutex

### Graceful Shutdown

The adapter implements proper cleanup:
1. Context cancellation signals shutdown
2. Wait groups ensure goroutines finish
3. Message channel is closed
4. Status updated to disconnected

## Dependencies

- `github.com/slack-go/slack` - Official Slack Go library
- `github.com/haasonsaas/nexus/pkg/models` - Unified message models
- `github.com/haasonsaas/nexus/internal/channels` - Channel adapter interface

## Examples

See `example_test.go` for complete working examples.

## License

Part of the Nexus project.
