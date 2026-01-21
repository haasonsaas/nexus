# Nexus - Component Design

Detailed design for each major component of the Nexus system.

## 1. Gateway

The gateway is the central nervous system of Nexus, handling all client connections and message routing.

### Structure

```
internal/gateway/
├── server.go           # gRPC server setup
├── handlers.go         # RPC method handlers
├── router.go           # Message routing logic
├── middleware.go       # Auth, logging, rate limiting
└── stream.go           # Bidirectional streaming
```

### gRPC Service Definition

```protobuf
service NexusGateway {
  // Bidirectional streaming for real-time conversation
  rpc Stream(stream ClientMessage) returns (stream ServerMessage);

  // Unary RPCs for management
  rpc CreateSession(CreateSessionRequest) returns (Session);
  rpc GetSession(GetSessionRequest) returns (Session);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
  rpc DeleteSession(DeleteSessionRequest) returns (Empty);

  // Agent management
  rpc CreateAgent(CreateAgentRequest) returns (Agent);
  rpc UpdateAgent(UpdateAgentRequest) returns (Agent);
  rpc ListAgents(ListAgentsRequest) returns (ListAgentsResponse);

  // Channel management
  rpc ConnectChannel(ConnectChannelRequest) returns (ChannelStatus);
  rpc DisconnectChannel(DisconnectChannelRequest) returns (Empty);
  rpc ListChannels(ListChannelsRequest) returns (ListChannelsResponse);
}

message ClientMessage {
  oneof payload {
    SendMessage send = 1;
    ToolResponse tool_response = 2;
    Ping ping = 3;
  }
}

message ServerMessage {
  oneof payload {
    MessageChunk chunk = 1;        // Streaming response text
    ToolCall tool_call = 2;        // Tool execution request
    SessionEvent event = 3;        // Session state changes
    Pong pong = 4;
  }
}
```

### Router

The router resolves incoming messages to the appropriate session:

```go
type Router struct {
    sessions  SessionStore
    rateLimit *RateLimiter
    metrics   *Metrics
}

func (r *Router) Route(ctx context.Context, msg *Message) (*Session, error) {
    // 1. Extract user identity from channel-specific metadata
    userID := r.extractUserID(msg)

    // 2. Check rate limits
    if err := r.rateLimit.Check(userID); err != nil {
        return nil, err
    }

    // 3. Find or create session
    sessionKey := r.buildSessionKey(msg)
    session, err := r.sessions.GetOrCreate(ctx, sessionKey)
    if err != nil {
        return nil, err
    }

    // 4. Enrich message with session context
    msg.SessionID = session.ID

    return session, nil
}
```

---

## 2. Channel Adapters

Each channel adapter implements capability-based interfaces (inbound, outbound, lifecycle, health) and is registered through the channel plugin registry for lazy loading. External plugins can ship a `nexus.plugin.json` (or `clawdbot.plugin.json`) manifest with a strict JSON schema for config validation; runtime plugins can additionally expose a `NexusPlugin` symbol in a `.so` for in-process loading.

Example plugin:
- `examples/plugins/echo` shows a minimal runtime plugin (manifest + `plugin.so` build).

### Telegram Adapter

```
internal/channels/telegram/
├── adapter.go          # Main adapter implementation
├── bot.go              # Telegram bot client wrapper
├── convert.go          # Message conversion
└── media.go            # Media upload/download
```

**Key Libraries:**
- `github.com/go-telegram/bot` - Modern Telegram Bot API client

**Features:**
- Long polling (default) or webhook mode
- Inline keyboards for interactive responses
- Media handling (images, audio, documents)
- Group chat support with mention detection

```go
type TelegramAdapter struct {
    bot      *tgbot.Bot
    messages chan *Message
    config   TelegramConfig
}

func (a *TelegramAdapter) Start(ctx context.Context) error {
    a.bot.RegisterHandler(tgbot.HandlerTypeMessageText, a.handleMessage)
    return a.bot.Start(ctx)
}

func (a *TelegramAdapter) handleMessage(ctx context.Context, b *tgbot.Bot, update *models.Update) {
    msg := a.convertToInternal(update.Message)
    a.messages <- msg
}
```

### Discord Adapter

```
internal/channels/discord/
├── adapter.go          # Main adapter implementation
├── bot.go              # Discord session wrapper
├── convert.go          # Message conversion
├── slash.go            # Slash command registration
└── components.go       # Interactive components
```

**Key Libraries:**
- `github.com/bwmarrin/discordgo` - Discord API client

**Features:**
- Slash commands for structured interactions
- Thread support for long conversations
- Rich embeds for formatted responses
- Guild-specific configuration

```go
type DiscordAdapter struct {
    session  *discordgo.Session
    messages chan *Message
    config   DiscordConfig
}

func (a *DiscordAdapter) Start(ctx context.Context) error {
    a.session.AddHandler(a.handleMessage)
    a.session.Identify.Intents = discordgo.IntentsGuildMessages |
                                  discordgo.IntentsDirectMessages
    return a.session.Open()
}
```

### Slack Adapter

```
internal/channels/slack/
├── adapter.go          # Main adapter implementation
├── bot.go              # Slack client wrapper
├── convert.go          # Message conversion
├── events.go           # Event API handling
└── blocks.go           # Block Kit builders
```

**Key Libraries:**
- `github.com/slack-go/slack` - Slack API client
- `github.com/slack-go/slack/socketmode` - Socket Mode for real-time events

**Features:**
- Socket Mode (no public webhook needed)
- Block Kit for rich message formatting
- App mentions and DMs
- Thread replies for context preservation

```go
type SlackAdapter struct {
    client   *slack.Client
    socket   *socketmode.Client
    messages chan *Message
    config   SlackConfig
}

func (a *SlackAdapter) Start(ctx context.Context) error {
    go a.handleEvents(ctx)
    return a.socket.Run()
}
```

---

## 3. Agent Runtime

The agent runtime manages conversation state and orchestrates LLM interactions.

### Structure

```
internal/agent/
├── runtime.go          # Main agent loop
├── prompt.go           # System prompt construction
├── context.go          # Context window management
├── tools.go            # Tool registration and dispatch
└── providers/
    ├── provider.go     # Provider interface
    ├── anthropic.go    # Claude integration
    ├── openai.go       # GPT integration
    ├── google.go       # Gemini integration
    └── openrouter.go   # OpenRouter integration
```

### Runtime Loop

```go
type Runtime struct {
    provider    LLMProvider
    tools       *ToolRegistry
    sessions    SessionStore
    sandbox     SandboxExecutor
}

func (r *Runtime) Process(ctx context.Context, session *Session, msg *Message) (<-chan *ResponseChunk, error) {
    chunks := make(chan *ResponseChunk)

    go func() {
        defer close(chunks)

        // 1. Build conversation context
        history, err := r.sessions.GetHistory(ctx, session.ID)
        if err != nil {
            chunks <- &ResponseChunk{Error: err}
            return
        }

        // 2. Construct prompt
        prompt := r.buildPrompt(session.Agent, history, msg)

        // 3. Stream completion
        completion, err := r.provider.Complete(ctx, prompt)
        if err != nil {
            chunks <- &ResponseChunk{Error: err}
            return
        }

        // 4. Process stream, executing tools as needed
        for chunk := range completion {
            if chunk.ToolCall != nil {
                result := r.executeTool(ctx, session, chunk.ToolCall)
                // Continue with tool result...
            } else {
                chunks <- &ResponseChunk{Text: chunk.Text}
            }
        }
    }()

    return chunks, nil
}
```

### Provider Implementation (Anthropic)

```go
type AnthropicProvider struct {
    client *anthropic.Client
    model  string
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan *CompletionChunk, error) {
    chunks := make(chan *CompletionChunk)

    go func() {
        defer close(chunks)

        stream, err := p.client.Messages.Stream(ctx, anthropic.MessageParams{
            Model:     p.model,
            MaxTokens: req.MaxTokens,
            System:    req.System,
            Messages:  convertMessages(req.Messages),
            Tools:     convertTools(req.Tools),
        })
        if err != nil {
            chunks <- &CompletionChunk{Error: err}
            return
        }

        for event := range stream.Events() {
            switch e := event.(type) {
            case anthropic.ContentBlockDeltaEvent:
                chunks <- &CompletionChunk{Text: e.Delta.Text}
            case anthropic.ToolUseEvent:
                chunks <- &CompletionChunk{ToolCall: convertToolCall(e)}
            }
        }
    }()

    return chunks, nil
}
```

---

## 4. Tools

Tools extend agent capabilities with external integrations.

### Structure

```
internal/tools/
├── registry.go         # Tool registration and dispatch
├── schema.go           # JSON Schema generation
├── sandbox/
│   ├── executor.go     # Firecracker orchestration
│   ├── pool.go         # VM pool management
│   └── snapshot.go     # VM snapshot management
├── browser/
│   ├── browser.go      # Playwright wrapper
│   ├── actions.go      # Browser actions
│   └── screenshot.go   # Screenshot capture
└── websearch/
    ├── search.go       # Search orchestration
    ├── searxng.go      # SearXNG provider
    └── extract.go      # Content extraction
```

### Code Execution (Sandbox)

The sandbox uses Firecracker microVMs for secure code execution:

```go
type SandboxExecutor struct {
    pool     *VMPool
    timeout  time.Duration
    limits   ResourceLimits
}

type ResourceLimits struct {
    MaxCPU      int           // vCPUs
    MaxMemory   int64         // bytes
    MaxTime     time.Duration // execution timeout
    MaxOutput   int64         // stdout/stderr bytes
    NetworkMode NetworkMode   // none, restricted, full
}

func (s *SandboxExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error) {
    // 1. Acquire VM from pool
    vm, err := s.pool.Acquire(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to acquire VM: %w", err)
    }
    defer s.pool.Release(vm)

    // 2. Configure VM with resource limits
    if err := vm.Configure(s.limits); err != nil {
        return nil, fmt.Errorf("failed to configure VM: %w", err)
    }

    // 3. Copy code to VM
    if err := vm.WriteFile("/code/main"+req.Extension, req.Code); err != nil {
        return nil, fmt.Errorf("failed to write code: %w", err)
    }

    // 4. Execute with timeout
    execCtx, cancel := context.WithTimeout(ctx, s.timeout)
    defer cancel()

    output, err := vm.Exec(execCtx, req.Command, req.Args...)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            return &ExecuteResult{
                ExitCode: -1,
                Stderr:   "Execution timed out",
            }, nil
        }
        return nil, err
    }

    return &ExecuteResult{
        ExitCode: output.ExitCode,
        Stdout:   output.Stdout,
        Stderr:   output.Stderr,
    }, nil
}
```

### Browser Automation

```go
type Browser struct {
    pw       *playwright.Playwright
    browser  playwright.Browser
    timeout  time.Duration
}

func (b *Browser) Execute(ctx context.Context, req *BrowserRequest) (*BrowserResult, error) {
    page, err := b.browser.NewPage()
    if err != nil {
        return nil, err
    }
    defer page.Close()

    // Navigate
    if _, err := page.Goto(req.URL); err != nil {
        return nil, err
    }

    // Execute actions
    for _, action := range req.Actions {
        switch action.Type {
        case "click":
            if err := page.Click(action.Selector); err != nil {
                return nil, err
            }
        case "type":
            if err := page.Fill(action.Selector, action.Value); err != nil {
                return nil, err
            }
        case "screenshot":
            data, err := page.Screenshot()
            if err != nil {
                return nil, err
            }
            return &BrowserResult{Screenshot: data}, nil
        case "extract":
            content, err := page.TextContent(action.Selector)
            if err != nil {
                return nil, err
            }
            return &BrowserResult{Content: content}, nil
        }
    }

    return &BrowserResult{}, nil
}
```

### Web Search

```go
type SearchEngine struct {
    searxng  *SearXNGClient
    extract  *ContentExtractor
}

func (s *SearchEngine) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error) {
    // 1. Search via SearXNG
    results, err := s.searxng.Search(ctx, query, opts.Categories, opts.Limit)
    if err != nil {
        return nil, err
    }

    // 2. Optionally extract full content
    if opts.ExtractContent {
        for i, r := range results.Items {
            content, err := s.extract.Extract(ctx, r.URL)
            if err == nil {
                results.Items[i].Content = content
            }
        }
    }

    return results, nil
}
```

---

## 5. Sessions

Session management handles conversation persistence and context.

### Structure

```
internal/sessions/
├── store.go            # Session storage interface
├── cockroach.go        # CockroachDB implementation
├── memory.go           # In-memory implementation (testing)
└── migration.go        # Schema migrations
```

### Session Store

```go
type SessionStore interface {
    // Session CRUD
    Create(ctx context.Context, session *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    Update(ctx context.Context, session *Session) error
    Delete(ctx context.Context, id string) error

    // Session lookup
    GetByKey(ctx context.Context, key SessionKey) (*Session, error)
    List(ctx context.Context, userID string, opts ListOptions) ([]*Session, error)

    // Message history
    AppendMessage(ctx context.Context, sessionID string, msg *Message) error
    GetHistory(ctx context.Context, sessionID string, limit int) ([]*Message, error)

    // Memory/embeddings
    StoreEmbedding(ctx context.Context, sessionID string, emb *Embedding) error
    SearchSimilar(ctx context.Context, sessionID string, vector []float32, limit int) ([]*Embedding, error)
}

type CockroachStore struct {
    db *sql.DB
}

func (s *CockroachStore) AppendMessage(ctx context.Context, sessionID string, msg *Message) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO messages (id, session_id, role, content, metadata, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, msg.ID, sessionID, msg.Role, msg.Content, msg.Metadata, msg.CreatedAt)
    return err
}
```

### Database Schema

```sql
-- Users and authentication
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email STRING UNIQUE NOT NULL,
    name STRING,
    avatar_url STRING,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name STRING NOT NULL,
    key_hash STRING NOT NULL,
    prefix STRING NOT NULL,  -- First 8 chars for identification
    scopes STRING[] DEFAULT ARRAY[]::STRING[],
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Agents
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name STRING NOT NULL,
    system_prompt TEXT,
    model STRING NOT NULL DEFAULT 'claude-sonnet-4-20250514',
    provider STRING NOT NULL DEFAULT 'anthropic',
    tools STRING[] DEFAULT ARRAY[]::STRING[],
    config JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- Sessions
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID REFERENCES agents(id) ON DELETE CASCADE,
    channel STRING NOT NULL,
    channel_id STRING NOT NULL,  -- Platform-specific user/chat ID
    session_key STRING NOT NULL UNIQUE,
    title STRING,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    INDEX (agent_id, channel),
    INDEX (session_key)
);

-- Messages
CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES sessions(id) ON DELETE CASCADE,
    role STRING NOT NULL,  -- user, assistant, system, tool
    content TEXT NOT NULL,
    tool_calls JSONB,
    tool_results JSONB,
    tokens_input INT,
    tokens_output INT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now(),
    INDEX (session_id, created_at)
);

-- Embeddings for memory/search
CREATE TABLE embeddings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES sessions(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    vector VECTOR(1536),  -- OpenAI embedding dimension
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now(),
    INDEX (session_id)
);

-- Channel credentials (encrypted)
CREATE TABLE channel_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    channel STRING NOT NULL,
    name STRING NOT NULL,
    credentials_encrypted BYTES NOT NULL,
    status STRING DEFAULT 'pending',
    last_connected_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    UNIQUE (user_id, channel, name)
);

-- Usage tracking
CREATE TABLE usage_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    session_id UUID REFERENCES sessions(id),
    event_type STRING NOT NULL,  -- message, tool_call, etc.
    tokens_input INT,
    tokens_output INT,
    cost_cents INT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT now(),
    INDEX (user_id, created_at)
);
```

---

## 6. Authentication

### Structure

```
internal/auth/
├── auth.go             # Auth service interface
├── oauth.go            # OAuth 2.0 providers
├── apikey.go           # API key validation
├── jwt.go              # JWT token generation
└── middleware.go       # gRPC interceptors
```

### OAuth Flow

```go
type OAuthProvider interface {
    AuthURL(state string) string
    Exchange(ctx context.Context, code string) (*oauth2.Token, error)
    UserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error)
}

type AuthService struct {
    providers map[string]OAuthProvider
    users     UserStore
    keys      APIKeyStore
    jwt       *JWTService
}

func (s *AuthService) HandleCallback(ctx context.Context, provider, code string) (*AuthResult, error) {
    p, ok := s.providers[provider]
    if !ok {
        return nil, ErrUnknownProvider
    }

    // Exchange code for token
    token, err := p.Exchange(ctx, code)
    if err != nil {
        return nil, err
    }

    // Get user info
    info, err := p.UserInfo(ctx, token)
    if err != nil {
        return nil, err
    }

    // Find or create user
    user, err := s.users.FindOrCreate(ctx, info)
    if err != nil {
        return nil, err
    }

    // Generate JWT
    jwt, err := s.jwt.Generate(user)
    if err != nil {
        return nil, err
    }

    return &AuthResult{
        User:  user,
        Token: jwt,
    }, nil
}
```

### gRPC Middleware

```go
func AuthInterceptor(auth *AuthService) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
        // Extract token from metadata
        md, ok := metadata.FromIncomingContext(ctx)
        if !ok {
            return nil, status.Error(codes.Unauthenticated, "missing metadata")
        }

        // Try JWT first, then API key
        token := extractBearer(md)
        if token != "" {
            user, err := auth.ValidateJWT(token)
            if err != nil {
                return nil, status.Error(codes.Unauthenticated, "invalid token")
            }
            ctx = WithUser(ctx, user)
        } else {
            apiKey := extractAPIKey(md)
            if apiKey == "" {
                return nil, status.Error(codes.Unauthenticated, "missing credentials")
            }
            user, err := auth.ValidateAPIKey(ctx, apiKey)
            if err != nil {
                return nil, status.Error(codes.Unauthenticated, "invalid API key")
            }
            ctx = WithUser(ctx, user)
        }

        return handler(ctx, req)
    }
}
```

---

## 7. Configuration

### Structure

```
internal/config/
├── config.go           # Main config struct
├── load.go             # Config loading logic
├── validate.go         # Validation rules
└── defaults.go         # Default values
```

### Configuration Structure

```go
type Config struct {
    Server   ServerConfig   `yaml:"server"`
    Database DatabaseConfig `yaml:"database"`
    Auth     AuthConfig     `yaml:"auth"`
    Channels ChannelsConfig `yaml:"channels"`
    LLM      LLMConfig      `yaml:"llm"`
    Tools    ToolsConfig    `yaml:"tools"`
    Logging  LoggingConfig  `yaml:"logging"`
}

type ServerConfig struct {
    Host        string `yaml:"host" env:"NEXUS_HOST" default:"0.0.0.0"`
    GRPCPort    int    `yaml:"grpc_port" env:"NEXUS_GRPC_PORT" default:"50051"`
    HTTPPort    int    `yaml:"http_port" env:"NEXUS_HTTP_PORT" default:"8080"`
    MetricsPort int    `yaml:"metrics_port" env:"NEXUS_METRICS_PORT" default:"9090"`
}

type DatabaseConfig struct {
    URL             string `yaml:"url" env:"DATABASE_URL" required:"true"`
    MaxConnections  int    `yaml:"max_connections" default:"25"`
    ConnMaxLifetime string `yaml:"conn_max_lifetime" default:"5m"`
}

type ChannelsConfig struct {
    Telegram TelegramConfig `yaml:"telegram"`
    Discord  DiscordConfig  `yaml:"discord"`
    Slack    SlackConfig    `yaml:"slack"`
}
```

### Config File (nexus.yaml)

```yaml
server:
  host: 0.0.0.0
  grpc_port: 50051
  http_port: 8080

database:
  url: postgres://nexus:password@localhost:26257/nexus?sslmode=disable

auth:
  jwt_secret: ${JWT_SECRET}
  token_expiry: 24h
  api_keys:
    - key: ${NEXUS_API_KEY}
      user_id: operator
      name: "Operator key"
  oauth:
    google:
      client_id: ${GOOGLE_CLIENT_ID}
      client_secret: ${GOOGLE_CLIENT_SECRET}
    github:
      client_id: ${GITHUB_CLIENT_ID}
      client_secret: ${GITHUB_CLIENT_SECRET}

session:
  default_agent_id: main
  slack_scope: thread
  discord_scope: thread
  memory_flush:
    enabled: false
    threshold: 80
    prompt: "Session nearing compaction. If there are durable facts, store them in memory/YYYY-MM-DD.md or MEMORY.md. Reply NO_REPLY if nothing needs attention."
workspace:
  enabled: false
  path: .
  max_chars: 20000
  agents_file: AGENTS.md
  soul_file: SOUL.md
  user_file: USER.md
  identity_file: IDENTITY.md
  tools_file: TOOLS.md
  memory_file: MEMORY.md

channels:
  telegram:
    enabled: true
  discord:
    enabled: true
  slack:
    enabled: true

llm:
  default_provider: anthropic
  providers:
    anthropic:
      api_key: ${ANTHROPIC_API_KEY}
      default_model: claude-sonnet-4-20250514
    openai:
      api_key: ${OPENAI_API_KEY}
      default_model: gpt-4o

tools:
  sandbox:
    enabled: true
    pool_size: 5
    timeout: 30s
    limits:
      max_cpu: 1
      max_memory: 512MB
  browser:
    enabled: true
    headless: true
  websearch:
    enabled: true
    provider: searxng
    url: http://localhost:8888
  memory_search:
    enabled: false
    directory: memory
    memory_file: MEMORY.md
    max_results: 5
    max_snippet_len: 200
    mode: hybrid
    embeddings:
      provider: openai
      api_key: ${OPENAI_API_KEY}
      base_url: https://api.openai.com/v1
      model: text-embedding-3-small
      cache_dir: ~/.nexus/cache/embeddings
      cache_ttl: 24h
      timeout: 15s

logging:
  level: info
  format: json
```
