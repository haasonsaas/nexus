# Nexus

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**Nexus** is a multi-channel AI agent gateway written in Go. It connects messaging platforms to LLM providers with tool execution, semantic memory, and MCP (Model Context Protocol) support.

## Why Nexus?

- **Unified Interface** - One codebase to manage AI conversations across all your messaging platforms
- **Provider Agnostic** - Swap between Claude, GPT-4, Gemini without changing your bot logic
- **MCP Native** - First-class support for Model Context Protocol servers
- **Semantic Memory** - Vector-based memory with OpenAI/Ollama embeddings
- **Production Ready** - Built for scale with CockroachDB, Kubernetes-native, and comprehensive observability

## Features

### Messaging Channels

| Channel | Status | Features |
|---------|--------|----------|
| **Telegram** | Stable | Long polling, webhooks, inline keyboards, media handling |
| **Discord** | Stable | Slash commands, threads, rich embeds, guild management |
| **Slack** | Stable | Socket Mode, Block Kit, app mentions, thread replies |
| **Matrix** | Beta | Room messaging, E2E encryption support |
| **WhatsApp** | Alpha | Business API integration |
| **Signal** | Alpha | Signal Protocol messaging |
| **iMessage** | Alpha | macOS-only, requires BlueBubbles/pypush |

### LLM Providers

- **Anthropic** - Claude Sonnet 4, Claude Opus 4, with tool use
- **OpenAI** - GPT-4o, GPT-4 Turbo, with function calling
- **Google** - Gemini Pro, Gemini Ultra
- **OpenRouter** - Access to 100+ models through unified API

### Tool Capabilities

- **Web Search** - SearXNG-powered web search with content extraction
- **Browser Automation** - Playwright-based web browsing and scraping
- **Memory Search** - Semantic search across conversation history
- **Code Sandbox** - Container-based code execution (planned: Firecracker microVMs)

### MCP Integration

Full Model Context Protocol support:
- **Stdio & HTTP transports** - Connect to any MCP server
- **Tool aggregation** - Expose MCP tools to LLM providers
- **Resource access** - Read MCP resources into context
- **Prompt templates** - Use MCP prompts in conversations
- **Sampling** - Handle MCP sampling requests

### Memory & Context

- **Vector Memory** - SQLite-vec backend with semantic search
- **Embedding Providers** - OpenAI, Ollama (local)
- **Conversation Summarization** - Automatic context compaction
- **Tool Policies** - Fine-grained allow/deny rules per tool

### Infrastructure

- **gRPC Streaming** - Real-time bidirectional communication
- **CockroachDB** - Distributed SQL for horizontal scaling
- **Full Persistence** - Conversation history with vector embeddings
- **OAuth + API Keys** - Flexible authentication for users and services

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Messaging Clients                               │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐        │
│  │Telegram│ │Discord │ │ Slack  │ │ Matrix │ │WhatsApp│ │ gRPC   │        │
│  └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘ └───┬────┘        │
└──────┼──────────┼──────────┼──────────┼──────────┼──────────┼─────────────┘
       └──────────┴──────────┴────┬─────┴──────────┴──────────┘
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Gateway Server                                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  Channel Registry → Message Router → Rate Limiter → Session Resolver│   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        Agent Runtime                                 │   │
│  │  • Context packing & summarization                                   │   │
│  │  • Tool policy enforcement                                           │   │
│  │  • Concurrent tool execution                                         │   │
│  │  • Streaming response handling                                       │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│           │                        │                        │              │
│           ▼                        ▼                        ▼              │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐        │
│  │  LLM Providers  │    │  Tool Executor  │    │   MCP Manager   │        │
│  │  ├─ Anthropic   │    │  ├─ WebSearch   │    │  ├─ Stdio       │        │
│  │  ├─ OpenAI      │    │  ├─ Browser     │    │  ├─ HTTP        │        │
│  │  ├─ Google      │    │  └─ MemSearch   │    │  └─ Tools/Res   │        │
│  │  └─ OpenRouter  │    └─────────────────┘    └─────────────────┘        │
│  └─────────────────┘                                                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Data Layer                                      │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │   CockroachDB    │  │   SQLite-vec     │  │  Object Storage  │          │
│  │  • Sessions      │  │  • Embeddings    │  │  • Attachments   │          │
│  │  • Messages      │  │  • Memory index  │  │  • Screenshots   │          │
│  │  • Users/Keys    │  │                  │  │                  │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.24+
- Docker (for CockroachDB and optional services)
- A bot token for at least one messaging platform

### Installation

```bash
git clone https://github.com/haasonsaas/nexus.git
cd nexus

go mod download
go build -o bin/nexus ./cmd/nexus
```

### Database Setup

```bash
# Start CockroachDB (single node for development)
docker run -d --name cockroach \
  -p 26257:26257 -p 8080:8080 \
  cockroachdb/cockroach:v23.2.0 start-single-node --insecure

# Create database and run migrations
docker exec cockroach cockroach sql --insecure -e "CREATE DATABASE nexus;"
./bin/nexus migrate up
```

### Configuration

```bash
cp nexus.example.yaml nexus.yaml
```

Minimal configuration:

```yaml
server:
  host: 0.0.0.0
  grpc_port: 50051
  http_port: 8080

database:
  url: postgres://root@localhost:26257/nexus?sslmode=disable

llm:
  default_provider: anthropic
  providers:
    anthropic:
      api_key: ${ANTHROPIC_API_KEY}
      default_model: claude-sonnet-4-20250514

channels:
  telegram:
    enabled: true
    bot_token: ${TELEGRAM_BOT_TOKEN}
```

### Running

```bash
./bin/nexus serve
```

## Configuration Reference

### Memory (Vector Search)

```yaml
memory:
  enabled: true
  backend: sqlite-vec
  dimension: 1536  # Must match embedding model

  sqlite_vec:
    path: ./data/memory.db

  embeddings:
    provider: openai  # or: ollama
    api_key: ${OPENAI_API_KEY}
    model: text-embedding-3-small

    # For Ollama:
    # provider: ollama
    # ollama_url: http://localhost:11434
    # model: nomic-embed-text

  indexing:
    auto_index_messages: true
    min_content_length: 10
    batch_size: 100

  search:
    default_limit: 10
    default_threshold: 0.7
    default_scope: session  # or: user, global
```

### MCP Servers

```yaml
mcp:
  enabled: true
  servers:
    - id: filesystem
      name: Filesystem Access
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"]
      auto_start: true

    - id: github
      name: GitHub
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_TOKEN: ${GITHUB_TOKEN}
```

### Tool Policies

```yaml
tools:
  policies:
    default: allow  # or: deny
    rules:
      - tool: browser
        action: deny
        channels: [telegram]  # Deny browser in Telegram
      - tool: websearch
        action: allow
```

### Session Behavior

```yaml
session:
  default_agent_id: main
  slack_scope: thread    # thread or channel
  discord_scope: thread  # thread or channel

  # Automatic summarization
  summarization:
    enabled: true
    threshold_messages: 50
    threshold_tokens: 8000
    max_summary_length: 500
```

### Workspace Files

Nexus can read context from workspace files:

```yaml
workspace:
  enabled: true
  path: ./workspace
  agents_file: AGENTS.md    # Agent definitions
  soul_file: SOUL.md        # Personality/system prompt
  user_file: USER.md        # User preferences
  identity_file: IDENTITY.md
  tools_file: TOOLS.md      # Tool-specific instructions
  memory_file: MEMORY.md    # Persistent notes
```

## CLI Commands

```bash
# Server
nexus serve                    # Start the gateway
nexus serve --config custom.yaml

# Setup & Diagnostics
nexus doctor --config nexus.yaml           # Validate config
nexus doctor --repair --config nexus.yaml  # Fix issues
nexus doctor --probe --config nexus.yaml   # Test channel connectivity
nexus setup --workspace ./mybot            # Bootstrap workspace files

# Onboarding
nexus onboard --config nexus.yaml          # Guided setup wizard
nexus profile init prod --provider anthropic --use
nexus auth set --provider anthropic --api-key $KEY

# Database
nexus migrate up       # Run migrations
nexus migrate down     # Rollback
nexus migrate status   # Show status

# Channels & Agents
nexus channels list    # List configured channels
nexus channels status  # Connection status
nexus agents list      # List agents

# Debug
nexus prompt --config nexus.yaml --session-id test --channel slack
```

## Development

### Project Structure

```
nexus/
├── cmd/nexus/              # CLI entry point
├── internal/
│   ├── agent/              # LLM orchestration & runtime
│   │   ├── context/        # Context packing & summarization
│   │   └── providers/      # Anthropic, OpenAI, Google, OpenRouter
│   ├── channels/           # Channel adapters
│   │   ├── telegram/       # Telegram (stable)
│   │   ├── discord/        # Discord (stable)
│   │   ├── slack/          # Slack (stable)
│   │   ├── matrix/         # Matrix (beta)
│   │   ├── whatsapp/       # WhatsApp (alpha)
│   │   ├── signal/         # Signal (alpha)
│   │   └── imessage/       # iMessage (alpha)
│   ├── mcp/                # MCP client & manager
│   ├── memory/             # Vector memory
│   │   ├── backend/        # SQLite-vec (more planned)
│   │   └── embeddings/     # OpenAI, Ollama providers
│   ├── tools/              # Tool implementations
│   │   ├── browser/        # Playwright automation
│   │   ├── websearch/      # SearXNG integration
│   │   ├── memorysearch/   # Semantic memory search
│   │   └── policy/         # Tool access control
│   ├── sessions/           # Session & message persistence
│   ├── gateway/            # gRPC server
│   └── config/             # Configuration loading
├── pkg/
│   ├── models/             # Shared types
│   ├── proto/              # gRPC definitions
│   └── pluginsdk/          # Plugin development SDK
├── deployments/
│   ├── kubernetes/         # K8s manifests
│   └── docker/             # Docker Compose
└── examples/
    └── plugins/echo/       # Example plugin
```

### Testing

```bash
go test ./...                          # Unit tests
go test -cover ./...                   # With coverage

# Integration tests
NEXUS_DOCKER_TESTS=1 go test ./...     # Requires Docker
NEXUS_BROWSER_TESTS=1 go test ./...    # Requires Playwright
```

### Building

```bash
# Development
go build -o bin/nexus ./cmd/nexus

# Production with version info
go build -ldflags "-X main.version=$(git describe --tags)" -o bin/nexus ./cmd/nexus

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o bin/nexus-linux-amd64 ./cmd/nexus
```

## Deployment

### Docker Compose

```bash
cd deployments/docker
docker-compose up -d
```

### Kubernetes

```bash
kubectl apply -k deployments/kubernetes/
```

See [docs/deployment.md](docs/deployment.md) for complete instructions.

### Systemd

```bash
sudo cp bin/nexus /usr/local/bin/
sudo cp deployments/systemd/nexus.service /etc/systemd/system/
sudo systemctl enable --now nexus
```

## API Reference

### gRPC

```protobuf
service NexusGateway {
  rpc Stream(stream ClientMessage) returns (stream ServerMessage);
  rpc CreateSession(CreateSessionRequest) returns (Session);
  rpc GetSession(GetSessionRequest) returns (Session);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
}
```

### REST

```
GET  /health          # Health check
GET  /metrics         # Prometheus metrics
POST /api/v1/send     # Send message
GET  /api/v1/sessions # List sessions
```

## Monitoring

Prometheus metrics at `/metrics`:

- `nexus_requests_total` - RPC requests by method/status
- `nexus_request_duration_seconds` - Latency histogram
- `nexus_llm_tokens_total` - Token usage by provider
- `nexus_tool_executions_total` - Tool execution counts
- `nexus_active_sessions` - Active session gauge
- `nexus_channel_messages_total` - Messages by channel
- `nexus_memory_searches_total` - Memory search operations

## Roadmap

- [ ] Firecracker microVM sandbox
- [ ] LanceDB vector backend
- [ ] pgvector backend
- [ ] Voice message transcription
- [ ] Multi-agent orchestration
- [ ] Web UI for session management

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

- Inspired by [Clawdbot](https://github.com/clawdbot/clawdbot)
- Built with [gRPC-Go](https://github.com/grpc/grpc-go), [discordgo](https://github.com/bwmarrin/discordgo), [slack-go](https://github.com/slack-go/slack), [go-telegram](https://github.com/go-telegram/bot)
