# Nexus Protocol Buffers

This directory contains the gRPC protocol buffer definitions and generated Go code for the Nexus AI Gateway.

## Overview

The Nexus proto defines a comprehensive gRPC API for:
- Real-time bidirectional message streaming
- Session management (conversations)
- Agent management (AI configurations)
- Channel management (messaging platform connections)
- Health checks

## Services

### NexusGateway
Provides real-time bidirectional streaming for client-server communication.

```go
// Stream establishes a bidirectional connection
rpc Stream(stream ClientMessage) returns (stream ServerMessage);
```

### SessionService
Manages conversation sessions with CRUD operations.

```go
rpc CreateSession(CreateSessionRequest) returns (CreateSessionResponse);
rpc GetSession(GetSessionRequest) returns (GetSessionResponse);
rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
rpc DeleteSession(DeleteSessionRequest) returns (DeleteSessionResponse);
rpc UpdateSession(UpdateSessionRequest) returns (UpdateSessionResponse);
```

### AgentService
Manages AI agent configurations.

```go
rpc CreateAgent(CreateAgentRequest) returns (CreateAgentResponse);
rpc GetAgent(GetAgentRequest) returns (GetAgentResponse);
rpc ListAgents(ListAgentsRequest) returns (ListAgentsResponse);
rpc UpdateAgent(UpdateAgentRequest) returns (UpdateAgentResponse);
rpc DeleteAgent(DeleteAgentRequest) returns (DeleteAgentResponse);
```

### ChannelService
Manages connections to messaging platforms (Telegram, Discord, Slack).

```go
rpc ConnectChannel(ConnectChannelRequest) returns (ConnectChannelResponse);
rpc DisconnectChannel(DisconnectChannelRequest) returns (DisconnectChannelResponse);
rpc GetChannelStatus(GetChannelStatusRequest) returns (GetChannelStatusResponse);
rpc ListChannels(ListChannelsRequest) returns (ListChannelsResponse);
```

### HealthService
Standard health check service.

```go
rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
```

## Message Types

### Core Domain Messages
- **Message**: Unified message format across all channels
- **Session**: Conversation thread
- **Agent**: AI agent configuration
- **User**: Authenticated user
- **APIKey**: API key for programmatic access

### Streaming Messages
- **ClientMessage**: Messages from client to server (SendMessage, SessionEvent, Subscribe, etc.)
- **ServerMessage**: Messages from server to client (MessageChunk, MessageComplete, ToolCallRequest, etc.)

### Supporting Types
- **Attachment**: File or media attachments
- **ToolCall**: LLM tool execution requests
- **ToolResult**: Tool execution results
- **SessionEvent**: Real-time events (typing indicators, read receipts)

## Enums

- **ChannelType**: telegram, discord, slack, api
- **Direction**: inbound, outbound
- **Role**: user, assistant, system, tool
- **EventType**: typing_start, typing_stop, read, presence
- **ChunkType**: text, tool_call, metadata
- **ConnectionStatus**: connected, disconnected, error, connecting
- **ServingStatus**: serving, not_serving, service_unknown

## Code Generation

### Using Make (Recommended)

```bash
# Generate proto code
make proto

# Lint proto files
make proto-lint

# Check for breaking changes
make proto-breaking
```

### Using protoc directly

```bash
protoc --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  pkg/proto/nexus.proto
```

### Using buf

```bash
buf generate
```

## Usage Example

### Server Implementation

```go
import (
    pb "github.com/haasonsaas/nexus/pkg/proto"
    "google.golang.org/grpc"
)

type nexusServer struct {
    pb.UnimplementedNexusGatewayServer
}

func (s *nexusServer) Stream(stream pb.NexusGateway_StreamServer) error {
    for {
        msg, err := stream.Recv()
        if err != nil {
            return err
        }

        // Handle different message types
        switch m := msg.Message.(type) {
        case *pb.ClientMessage_SendMessage:
            // Process message
            response := &pb.ServerMessage{
                Message: &pb.ServerMessage_MessageChunk{
                    MessageChunk: &pb.MessageChunk{
                        Content: "Response",
                    },
                },
            }
            stream.Send(response)
        }
    }
}
```

### Client Implementation

```go
import (
    pb "github.com/haasonsaas/nexus/pkg/proto"
    "google.golang.org/grpc"
)

conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := pb.NewNexusGatewayClient(conn)
stream, err := client.Stream(context.Background())
if err != nil {
    log.Fatal(err)
}

// Send a message
err = stream.Send(&pb.ClientMessage{
    Message: &pb.ClientMessage_SendMessage{
        SendMessage: &pb.SendMessageRequest{
            SessionId: "session-123",
            Content:   "Hello, Nexus!",
        },
    },
})

// Receive responses
for {
    msg, err := stream.Recv()
    if err != nil {
        break
    }
    // Handle server message
}
```

## Features

### Real-time Streaming
- Bidirectional streaming for low-latency communication
- Message chunking for streaming AI responses
- Ping/pong for connection keepalive

### Tool Execution
- LLM can request tool execution via ToolCallRequest
- Client executes tools and returns results
- Supports complex tool workflows

### Session Events
- Typing indicators
- Read receipts
- Presence tracking
- Real-time event notifications

### Multi-channel Support
- Unified message format across Telegram, Discord, Slack
- Channel-specific metadata preservation
- Cross-platform session management

## Development

### Prerequisites

- Go 1.24+
- protoc (Protocol Buffer Compiler)
- buf (optional, for better tooling)

### Installation

```bash
# Install required tools
make install-tools

# Generate proto code
make proto
```

### Regenerating Code

After modifying `nexus.proto`, regenerate the Go code:

```bash
make proto
```

## Files

- `nexus.proto` - Protocol buffer definitions
- `nexus.pb.go` - Generated Go types
- `nexus_grpc.pb.go` - Generated gRPC service code
- `buf.yaml` - Buf configuration
- `buf.gen.yaml` - Buf code generation config

## Version

Protocol version: v1
Package: `nexus.v1`
Go package: `github.com/haasonsaas/nexus/pkg/proto`
