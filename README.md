# Nexus

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**Nexus** is a multi-channel AI agent gateway written in Go. It connects messaging platforms (Telegram, Discord, Slack) to LLM providers (Anthropic, OpenAI, Google, OpenRouter) with powerful tool execution capabilities including web search, sandboxed code execution, and browser automation.

## Why Nexus?

- **Unified Interface** - One codebase to manage AI conversations across all your messaging platforms
- **Provider Agnostic** - Swap between Claude, GPT-4, Gemini without changing your bot logic
- **Secure by Default** - Code execution happens in Firecracker microVMs with strict isolation
- **Production Ready** - Built for scale with CockroachDB, Kubernetes-native, and comprehensive observability
- **Open Core** - Core functionality is fully open source

## Features

### Messaging Channels
- **Telegram** - Full bot API support with inline keyboards, media handling
- **Discord** - Slash commands, threads, rich embeds, guild management
- **Slack** - Socket Mode, Block Kit, app mentions, thread replies

### LLM Providers
- **Anthropic** - Claude 3.5 Sonnet, Claude 3 Opus, with tool use
- **OpenAI** - GPT-4o, GPT-4 Turbo, with function calling
- **Google** - Gemini Pro, Gemini Ultra
- **OpenRouter** - Access to 100+ models through unified API

### Tool Capabilities
- **Web Search** - SearXNG-powered web search with content extraction
- **Code Sandbox** - Secure code execution in Firecracker microVMs
- **Browser Automation** - Playwright-based web browsing and scraping

### Infrastructure
- **gRPC Streaming** - Real-time bidirectional communication
- **CockroachDB** - Distributed SQL for horizontal scaling
- **Full Persistence** - Conversation history with vector embeddings
- **OAuth + API Keys** - Flexible authentication for users and services

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Messaging Clients                               │
│    ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐            │
│    │ Telegram │    │ Discord  │    │  Slack   │    │ gRPC CLI │            │
│    └────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘            │
└─────────┼───────────────┼───────────────┼───────────────┼──────────────────┘
          │               │               │               │
          ▼               ▼               ▼               ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Gateway Server                                     │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Channel Adapters                                 │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │   │
│  │  │  Telegram   │  │   Discord   │  │    Slack    │                  │   │
│  │  │ (go-tg-bot) │  │ (discordgo) │  │ (slack-go)  │                  │   │
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
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Agent Runtime                                    │   │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐        │   │
│  │  │ Anthropic │  │  OpenAI   │  │  Google   │  │OpenRouter │        │   │
│  │  └───────────┘  └───────────┘  └───────────┘  └───────────┘        │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     Tool Executor                                    │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │   │
│  │  │ Web Search  │  │Code Sandbox │  │   Browser   │                  │   │
│  │  │  (SearXNG)  │  │(Firecracker)│  │ (Playwright)│                  │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘                  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Data Layer                                         │
│  ┌──────────────────────────────────┐  ┌──────────────────────────────┐    │
│  │          CockroachDB             │  │      Object Storage          │    │
│  │  • Users & API keys              │  │  • Media attachments         │    │
│  │  • Sessions & messages           │  │  • Browser screenshots       │    │
│  │  • Channel credentials           │  │  • Session exports           │    │
│  │  • Vector embeddings             │  │                              │    │
│  └──────────────────────────────────┘  └──────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.24+
- Docker (for CockroachDB and optional services)
- A bot token for at least one messaging platform

### Installation

```bash
# Clone the repository
git clone https://github.com/haasonsaas/nexus.git
cd nexus

# Install dependencies
go mod download

# Build the binary
go build -o bin/nexus ./cmd/nexus
```

### Database Setup

```bash
# Start CockroachDB (single node for development)
docker run -d --name cockroach \
  -p 26257:26257 -p 8080:8080 \
  cockroachdb/cockroach:v23.2.0 start-single-node --insecure

# Create the database
docker exec cockroach cockroach sql --insecure -e "CREATE DATABASE nexus;"

# Run migrations
./bin/nexus migrate up
```

### Configuration

Create a configuration file:

```bash
cp nexus.example.yaml nexus.yaml
```

Edit `nexus.yaml` with your credentials:

```yaml
server:
  host: 0.0.0.0
  grpc_port: 50051
  http_port: 8080
  metrics_port: 9090

database:
  url: postgres://root@localhost:26257/nexus?sslmode=disable

auth:
  jwt_secret: ${JWT_SECRET}  # Generate with: openssl rand -base64 32

session:
  default_agent_id: main
  slack_scope: thread
  discord_scope: thread
  memory:
    enabled: false
    directory: memory
    max_lines: 20
  heartbeat:
    enabled: false
    file: HEARTBEAT.md

identity:
  name: ""
  creature: ""
  vibe: ""
  emoji: ""

user:
  name: ""
  preferred_address: ""
  pronouns: ""
  timezone: ""
  notes: ""

llm:
  default_provider: anthropic
  providers:
    anthropic:
      api_key: ${ANTHROPIC_API_KEY}
      default_model: claude-sonnet-4-20250514
    openai:
      api_key: ${OPENAI_API_KEY}
      default_model: gpt-4o

channels:
  telegram:
    enabled: true
    bot_token: ${TELEGRAM_BOT_TOKEN}
  discord:
    enabled: true
    bot_token: ${DISCORD_BOT_TOKEN}
    app_id: ${DISCORD_APP_ID}
  slack:
    enabled: true
    bot_token: ${SLACK_BOT_TOKEN}
    app_token: ${SLACK_APP_TOKEN}

tools:
  notes: ""
  notes_file: ""
  sandbox:
    enabled: true
    pool_size: 5
    timeout: 30s
  browser:
    enabled: true
    headless: true
  websearch:
    enabled: true
    provider: searxng
    url: http://localhost:8888

logging:
  level: info
  format: json
```

Notes:
- Config parsing is strict; unknown keys will fail validation.

### Testing

Standard test run:
```bash
go test ./...
```

Integration tests (requires Docker + Playwright deps):
```bash
NEXUS_DOCKER_TESTS=1 NEXUS_DOCKER_PULL=1 NEXUS_BROWSER_TESTS=1 go test ./...
```

### Running

```bash
# Start the server
./bin/nexus serve

# Or with environment variables
ANTHROPIC_API_KEY=sk-ant-... TELEGRAM_BOT_TOKEN=... ./bin/nexus serve
```

## CLI Commands

```bash
# Server
nexus serve              # Start the gateway server
nexus serve --config /path/to/config.yaml

# Database
nexus migrate up         # Run pending migrations
nexus migrate down       # Rollback last migration
nexus migrate status     # Show migration status

# Channels
nexus channels list      # List configured channels
nexus channels status    # Show connection status

# Agents
nexus agents list        # List configured agents
nexus agents create      # Create a new agent

# System
nexus status             # Show system health
nexus version            # Show version info
```

## Development

### Project Structure

```
nexus/
├── cmd/
│   └── nexus/           # CLI entry point
├── internal/
│   ├── gateway/         # gRPC gateway server
│   ├── channels/        # Channel adapters
│   │   ├── telegram/
│   │   ├── discord/
│   │   └── slack/
│   ├── agent/           # LLM orchestration
│   │   └── providers/   # LLM provider implementations
│   ├── tools/           # Tool implementations
│   │   ├── sandbox/     # Firecracker code execution
│   │   ├── browser/     # Playwright automation
│   │   └── websearch/   # Web search
│   ├── sessions/        # Session persistence
│   ├── auth/            # Authentication
│   └── config/          # Configuration loading
├── pkg/
│   ├── models/          # Shared data types
│   └── proto/           # gRPC protocol definitions
├── deployments/
│   ├── kubernetes/      # K8s manifests
│   └── docker/          # Docker Compose files
├── docs/                # Documentation
└── scripts/             # Build and utility scripts
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package tests
go test ./internal/channels/telegram/...

# Run with verbose output
go test -v ./...
```

### Building

```bash
# Development build
go build -o bin/nexus ./cmd/nexus

# Production build with version info
go build -ldflags "-X main.version=$(git describe --tags) -X main.commit=$(git rev-parse HEAD)" \
  -o bin/nexus ./cmd/nexus

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o bin/nexus-linux-amd64 ./cmd/nexus
GOOS=darwin GOARCH=arm64 go build -o bin/nexus-darwin-arm64 ./cmd/nexus
```

### Docker

```bash
# Build image
docker build -t nexus:latest .

# Run with Docker Compose
docker-compose up -d
```

## Deployment

### Kubernetes

See [docs/deployment.md](docs/deployment.md) for complete Kubernetes deployment instructions including:

- Namespace and secrets setup
- CockroachDB StatefulSet
- Nexus Deployment with HPA
- Ingress configuration
- Prometheus/Grafana monitoring

Quick start:

```bash
# Apply all manifests
kubectl apply -k deployments/kubernetes/

# Check status
kubectl get pods -n nexus
```

### Docker Compose (Self-Hosted)

```bash
cd deployments/docker
docker-compose up -d
```

### Systemd (Bare Metal)

```bash
# Copy binary
sudo cp bin/nexus /usr/local/bin/

# Install service
sudo cp deployments/systemd/nexus.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable nexus
sudo systemctl start nexus
```

## API Reference

### gRPC API

The gRPC API is defined in `pkg/proto/nexus.proto`. Key services:

```protobuf
service NexusGateway {
  // Bidirectional streaming for real-time conversation
  rpc Stream(stream ClientMessage) returns (stream ServerMessage);

  // Session management
  rpc CreateSession(CreateSessionRequest) returns (Session);
  rpc GetSession(GetSessionRequest) returns (Session);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);

  // Agent management
  rpc CreateAgent(CreateAgentRequest) returns (Agent);
  rpc ListAgents(ListAgentsRequest) returns (ListAgentsResponse);
}
```

### REST API

A REST API is available on the HTTP port for webhook integrations and management:

```
GET  /health              # Health check
GET  /status              # Detailed status
GET  /metrics             # Prometheus metrics

POST /api/v1/send         # Send message to channel
GET  /api/v1/sessions     # List sessions
GET  /api/v1/agents       # List agents
```

## Monitoring

### Prometheus Metrics

Nexus exposes metrics at `/metrics`:

- `nexus_requests_total` - Total RPC requests by method and status
- `nexus_request_duration_seconds` - Request latency histogram
- `nexus_llm_tokens_total` - LLM token usage by provider
- `nexus_tool_executions_total` - Tool execution counts
- `nexus_active_sessions` - Current active session count

### Health Checks

```bash
# gRPC health check
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check

# HTTP health check
curl http://localhost:8080/health
```

## Security

### Authentication

- **OAuth 2.0** - Google, GitHub, Discord for user authentication
- **API Keys** - For programmatic access with scoped permissions
- **JWT Tokens** - Short-lived tokens for session authentication

### Sandbox Isolation

Code execution runs in Firecracker microVMs with:

- Isolated network namespace
- Limited CPU and memory
- Read-only root filesystem
- No persistent storage
- Execution time limits

### Secrets Management

- Channel credentials encrypted at rest
- Environment variable expansion for sensitive config
- Kubernetes Secrets integration

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Inspired by [Clawdbot](https://github.com/clawdbot/clawdbot)
- Built with [gRPC-Go](https://github.com/grpc/grpc-go)
- Channel libraries: [discordgo](https://github.com/bwmarrin/discordgo), [slack-go](https://github.com/slack-go/slack), [go-telegram](https://github.com/go-telegram/bot)
