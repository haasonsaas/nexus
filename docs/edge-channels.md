# Edge-Only Channels

This document explains when to run a channel adapter on an edge daemon versus in the core, and how to implement edge-hosted channels.

## When to Use Edge Channels

Run a channel on the edge daemon when:

1. **Platform-Specific APIs**: The channel requires APIs only available on certain platforms
   - iMessage (macOS only, requires AppleScript or Messages.framework)
   - Signal Desktop (requires local database access)
   - WhatsApp Web bridge (requires local browser automation)

2. **Local State Requirements**: The channel maintains state that cannot be cloud-hosted
   - Local database files (Signal's encrypted SQLite)
   - Browser sessions that cannot be serialized
   - Hardware-bound credentials

3. **Privacy Sensitivity**: Messages should not pass through cloud infrastructure
   - End-to-end encrypted channels where decryption happens locally
   - Compliance requirements for data locality

4. **Network Topology**: The channel endpoint is only accessible from the local network
   - Local IoT hubs
   - VPN-only services

## When to Use Core Channels

Run a channel in the core when:

1. **Cloud APIs**: The channel uses a cloud-accessible API
   - Telegram Bot API (HTTP webhook)
   - Discord Bot (WebSocket to Discord gateway)
   - Slack (HTTP webhook or WebSocket)

2. **Stateless Operation**: No local state is required
   - API-based channels with token auth
   - Webhook receivers

3. **Scalability**: The channel needs to handle high message volumes
   - Core can scale horizontally
   - Multiple instances can share load

## Edge Channel Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Nexus Core                                │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                   Session Pipeline                           ││
│  │  - Routes messages to agents                                 ││
│  │  - Maintains conversation history                            ││
│  └────────────────────────┬────────────────────────────────────┘│
│                           │                                      │
│  ┌────────────────────────▼────────────────────────────────────┐│
│  │                   Edge Manager                               ││
│  │  - Tracks edge connections                                   ││
│  │  - Routes channel messages                                   ││
│  │  - Handles delivery acknowledgments                          ││
│  └─────────────────────────┬───────────────────────────────────┘│
└───────────────────────────┬│────────────────────────────────────┘
                            ││ gRPC streaming
            ┌───────────────┼│───────────────┐
            │               ││               │
      ┌─────▼─────┐   ┌─────▼│────┐   ┌─────▼─────┐
      │   Edge    │   │   Edge    │   │   Edge    │
      │  MacBook  │   │  iPhone   │   │  Server   │
      │           │   │           │   │           │
      │ ┌───────┐ │   │           │   │           │
      │ │iMessage│ │   │           │   │           │
      │ └───────┘ │   │           │   │           │
      └───────────┘   └───────────┘   └───────────┘
```

## Protocol Messages

### EdgeChannelInbound

Sent by edge when a message arrives from the user:

```protobuf
message EdgeChannelInbound {
  string edge_id = 1;
  ChannelType channel_type = 2;
  string channel_id = 3;
  string session_key = 4;
  string content = 5;
  string sender_id = 6;
  string sender_name = 7;
  repeated Attachment attachments = 8;
  map<string, string> metadata = 9;
  google.protobuf.Timestamp received_at = 10;
}
```

### CoreChannelOutbound

Sent by core to deliver a message through the edge channel:

```protobuf
message CoreChannelOutbound {
  string message_id = 1;
  string session_id = 2;
  ChannelType channel_type = 3;
  string channel_id = 4;
  string content = 5;
  repeated Attachment attachments = 6;
  string reply_to_id = 7;
  map<string, string> options = 8;
}
```

### EdgeChannelAck

Sent by edge to acknowledge message delivery:

```protobuf
message EdgeChannelAck {
  string message_id = 1;
  ChannelDeliveryStatus status = 2;
  string error = 3;
  string external_id = 4;
  google.protobuf.Timestamp delivered_at = 5;
}
```

## Implementation Guide

### Edge Side

1. Register channel capability during connection:

```go
stream.Send(&pb.EdgeMessage{
    Message: &pb.EdgeMessage_Register{
        Register: &pb.EdgeRegister{
            EdgeId: "my-edge",
            ChannelTypes: []string{"imessage"},
            Capabilities: &pb.EdgeCapabilities{
                Channels: true,
            },
        },
    },
})
```

2. Forward inbound messages:

```go
// When a message arrives from iMessage
stream.Send(&pb.EdgeMessage{
    Message: &pb.EdgeMessage_ChannelInbound{
        ChannelInbound: &pb.EdgeChannelInbound{
            EdgeId:      "my-edge",
            ChannelType: pb.ChannelType_CHANNEL_TYPE_IMESSAGE,
            ChannelId:   "+1234567890",
            SessionKey:  "imessage:+1234567890",
            Content:     "Hello from iMessage",
            SenderId:    "+1234567890",
            SenderName:  "John Doe",
            ReceivedAt:  timestamppb.Now(),
        },
    },
})
```

3. Handle outbound messages:

```go
for {
    msg, _ := stream.Recv()
    if outbound := msg.GetChannelOutbound(); outbound != nil {
        // Send via local iMessage API
        err := sendIMessage(outbound.ChannelId, outbound.Content)

        // Acknowledge delivery
        stream.Send(&pb.EdgeMessage{
            Message: &pb.EdgeMessage_ChannelAck{
                ChannelAck: &pb.EdgeChannelAck{
                    MessageId:   outbound.MessageId,
                    Status:      pb.ChannelDeliveryStatus_CHANNEL_DELIVERY_STATUS_SENT,
                    DeliveredAt: timestamppb.Now(),
                },
            },
        })
    }
}
```

### Core Side

1. Set up the channel handler:

```go
edgeManager.SetChannelHandler(func(ctx context.Context, msg *pb.EdgeChannelInbound) error {
    // Route to session pipeline
    return sessionManager.HandleInbound(ctx, msg)
})
```

2. Send outbound messages:

```go
// Find an edge that supports iMessage
edges := edgeManager.GetEdgesWithChannel("imessage")
if len(edges) == 0 {
    return errors.New("no edge available for iMessage")
}

// Send through the edge
ack, err := edgeManager.SendChannelMessage(ctx, edges[0].ID, &pb.CoreChannelOutbound{
    MessageId:   uuid.New().String(),
    SessionId:   session.ID,
    ChannelType: pb.ChannelType_CHANNEL_TYPE_IMESSAGE,
    ChannelId:   "+1234567890",
    Content:     "Hello from Nexus",
})
```

## Supported Edge Channel Types

| Channel Type | Platform | Status |
|-------------|----------|--------|
| `CHANNEL_TYPE_IMESSAGE` | macOS | Supported |
| `CHANNEL_TYPE_SIGNAL` | Desktop (any) | Planned |
| `CHANNEL_TYPE_WHATSAPP` | Desktop (any) | Planned |

## Troubleshooting

### No Edge Available

If `GetEdgesWithChannel` returns empty:
1. Check edge is connected: `manager.ListEdges()`
2. Verify edge registered the channel type in `ChannelTypes`
3. Check edge capabilities include `Channels: true`

### Message Not Delivered

If `SendChannelMessage` times out:
1. Check edge stream is healthy (heartbeats flowing)
2. Verify edge is handling `GetChannelOutbound()` messages
3. Check edge is sending `EdgeChannelAck` responses

### Duplicate Messages

If messages appear multiple times:
1. Ensure idempotency via `MessageId`
2. Check edge deduplication logic using `external_id`
3. Verify session key derivation is consistent
