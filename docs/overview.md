# Nexus - Architecture Overview

Nexus is a multi-channel AI agent orchestration platform written in Go. It connects messaging platforms (Telegram, Discord, Slack) to LLM providers (Anthropic, OpenAI, Google, OpenRouter) with tool execution capabilities including web search, code execution, and browser automation.

## Design Principles

1. **Open Core** - Core functionality is open source; premium features and managed hosting are commercial
2. **Hybrid Deployment** - Runs as managed SaaS or self-hosted on any Kubernetes cluster
3. **Channel Agnostic** - Unified message format across all platforms
4. **Provider Agnostic** - Swap LLM providers without changing agent logic
5. **Secure by Default** - Tool execution in sandboxed runners (Docker by default; optional Firecracker), encrypted credentials

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Clients                                         │
│    ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐            │
│    │ Telegram │    │ Discord  │    │  Slack   │    │ gRPC CLI │            │
│    └────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘            │
└─────────┼───────────────┼───────────────┼───────────────┼──────────────────┘
          │               │               │               │
          ▼               ▼               ▼               ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Gateway (gRPC Streaming)                          │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Channel Adapters                                 │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │   │
│  │  │  Telegram   │  │   Discord   │  │    Slack    │                  │   │
│  │  │  (go-tg)    │  │ (discordgo) │  │ (slack-go)  │                  │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘                  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      Message Router                                  │   │
│  │  • Session resolution (user → agent session)                        │   │
│  │  • Rate limiting & abuse prevention                                  │   │
│  │  • Message normalization                                             │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Agent Runtime                                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Session Manager                                  │   │
│  │  • Session state (CockroachDB)                                      │   │
│  │  • Conversation history                                              │   │
│  │  • Memory & embeddings (pgvector)                                   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     LLM Orchestrator                                 │   │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │   │
│  │  │ Anthropic │  │  OpenAI   │  │  Google   │  │OpenRouter │        │   │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │   │
│  │  • Provider failover                                                 │   │
│  │  • Token counting & cost tracking                                   │   │
│  │  • Streaming responses                                               │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Tool Executor                                    │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │   │
│  │  │ Web Search  │  │Code Sandbox │  │   Browser   │                  │   │
│  │  │  (SearXNG)  │  │(Docker/FC) │  │ (Playwright)│                  │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘                  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Data Layer                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      CockroachDB                                     │   │
│  │  • Users & authentication                                           │   │
│  │  • Sessions & conversation history                                  │   │
│  │  • Channel credentials (encrypted)                                  │   │
│  │  • Usage tracking & billing                                         │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      Object Storage (S3/Minio)                      │   │
│  │  • Media attachments                                                │   │
│  │  • Session exports                                                  │   │
│  │  • Browser screenshots                                              │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Request Flow

1. **Inbound Message** - User sends message via Telegram/Discord/Slack
2. **Channel Adapter** - Normalizes message to internal format
3. **Router** - Resolves user → session, applies rate limits
4. **Session Manager** - Loads conversation context from CockroachDB
5. **LLM Orchestrator** - Sends prompt to configured provider
6. **Tool Executor** - Executes tool calls in a sandbox (Docker by default; optional Firecracker)
7. **Response Stream** - Streams response back via gRPC
8. **Channel Adapter** - Formats and sends reply to originating channel

## Key Interfaces

### Unified Message Format

```go
type Message struct {
    ID          string            `json:"id"`
    SessionID   string            `json:"session_id"`
    Channel     ChannelType       `json:"channel"`      // telegram, discord, slack
    Direction   Direction         `json:"direction"`    // inbound, outbound
    Role        Role              `json:"role"`         // user, assistant, system, tool
    Content     string            `json:"content"`
    Attachments []Attachment      `json:"attachments"`
    Metadata    map[string]any    `json:"metadata"`
    CreatedAt   time.Time         `json:"created_at"`
}
```

### Channel Adapter Interface

```go
type ChannelAdapter interface {
    // Start begins listening for messages
    Start(ctx context.Context) error

    // Stop gracefully shuts down the adapter
    Stop(ctx context.Context) error

    // Send delivers a message to the channel
    Send(ctx context.Context, msg *Message) error

    // Messages returns a channel of inbound messages
    Messages() <-chan *Message

    // Type returns the channel type
    Type() ChannelType
}
```

### LLM Provider Interface

```go
type LLMProvider interface {
    // Complete sends a prompt and returns a streaming response
    Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error)

    // Name returns the provider name
    Name() string

    // Models returns available models
    Models() []Model

    // SupportsTools returns whether the provider supports tool use
    SupportsTools() bool
}
```

### Tool Interface

```go
type Tool interface {
    // Name returns the tool name for LLM function calling
    Name() string

    // Description returns the tool description
    Description() string

    // Schema returns the JSON schema for parameters
    Schema() json.RawMessage

    // Execute runs the tool with given parameters
    Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}
```

## Security Model

### Authentication

- **OAuth 2.0** - Google, GitHub, Discord for user login
- **API Keys** - For programmatic access and self-hosted deployments
- **JWT Tokens** - Short-lived tokens for session authentication

### Authorization

- **RBAC** - Role-based access control (admin, user, viewer)
- **Resource Policies** - Per-agent, per-channel permissions
- **Rate Limits** - Per-user, per-tier rate limiting

### Secrets Management

- **Channel Credentials** - Encrypted at rest with per-user keys
- **LLM API Keys** - User-provided keys stored encrypted
- **Kubernetes Secrets** - For managed deployment secrets

### Sandbox Isolation

- **Sandbox backends** - Docker (default) and optional Firecracker microVMs for stronger isolation
- **Network Policies** - Restricted egress, no inter-session communication
- **Resource Limits** - CPU, memory, execution time caps
- **Ephemeral Storage** - Container/VM state destroyed after execution

## Data Model

### Core Entities

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│    User     │────▶│   Agent     │────▶│   Session   │
└─────────────┘     └─────────────┘     └─────────────┘
       │                   │                   │
       │                   │                   │
       ▼                   ▼                   ▼
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  APIKey     │     │ChannelCred  │     │  Message    │
└─────────────┘     └─────────────┘     └─────────────┘
```

- **User** - Account holder, owns agents and credentials
- **Agent** - Configured AI agent with personality, tools, model
- **Session** - Conversation thread, scoped to user+agent+channel
- **Message** - Individual message in a session
- **APIKey** - Programmatic access credential
- **ChannelCred** - Encrypted Telegram/Discord/Slack credentials

## Scalability

### Horizontal Scaling

- **Stateless Gateway** - Any instance can handle any request
- **Session Affinity** - Optional sticky sessions for active conversations
- **CockroachDB** - Distributed, horizontally scalable

### Vertical Scaling

- **Connection Pooling** - Efficient database connections
- **gRPC Streaming** - Low-overhead real-time communication
- **Async Tool Execution** - Non-blocking tool calls

### Multi-Region

- **CockroachDB Geo-Partitioning** - Data locality for compliance
- **Edge Routing** - Route users to nearest gateway
- **Regional Sandboxes** - Tool execution in user's region
