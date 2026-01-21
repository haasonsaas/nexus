# Nexus MCP Client Design

## Overview

This document specifies the design for a full Model Context Protocol (MCP) client in Nexus, supporting tools, resources, prompts, and sampling.

## Goals

1. **Full MCP client**: Support all MCP primitives (tools, resources, prompts, sampling)
2. **Server discovery**: Config-based server connections
3. **Tool bridging**: Expose MCP tools to agents as native tools
4. **Resource integration**: Load MCP resources as context

---

## 1. MCP Protocol Overview

MCP (Model Context Protocol) is a standardized protocol for LLM-tool communication.

### 1.1 Primitives

| Primitive | Description |
|-----------|-------------|
| **Tools** | Functions the model can call |
| **Resources** | Data sources the model can read |
| **Prompts** | Templated prompts with parameters |
| **Sampling** | Request model completions from server |

### 1.2 Transport

MCP supports two transports:
- **stdio**: Subprocess with JSON-RPC over stdin/stdout
- **HTTP/SSE**: HTTP requests with Server-Sent Events for streaming

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        MCP Manager                               │
│   (server lifecycle, tool registry, resource cache)             │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      ┌───────────┐   ┌───────────┐   ┌───────────┐
      │  Server 1 │   │  Server 2 │   │  Server 3 │
      │  (stdio)  │   │  (HTTP)   │   │  (stdio)  │
      └───────────┘   └───────────┘   └───────────┘
              │               │               │
              └───────────────┼───────────────┘
                              ▼
                    ┌───────────────┐
                    │  Tool Bridge  │
                    │ (MCP → Nexus) │
                    └───────────────┘
```

---

## 3. Data Types

### 3.1 Server Configuration

```go
// internal/mcp/types.go

type ServerConfig struct {
    ID        string            `yaml:"id"`
    Name      string            `yaml:"name"`
    Transport TransportType     `yaml:"transport"`  // stdio | http

    // Stdio transport
    Command   string            `yaml:"command"`
    Args      []string          `yaml:"args"`
    Env       map[string]string `yaml:"env"`
    WorkDir   string            `yaml:"workdir"`

    // HTTP transport
    URL       string            `yaml:"url"`
    Headers   map[string]string `yaml:"headers"`

    // Options
    Timeout   time.Duration     `yaml:"timeout"`
    AutoStart bool              `yaml:"auto_start"`
}

type TransportType string

const (
    TransportStdio TransportType = "stdio"
    TransportHTTP  TransportType = "http"
)
```

### 3.2 MCP Primitives

```go
// Tool definition from server
type MCPTool struct {
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    InputSchema json.RawMessage `json:"inputSchema"`  // JSON Schema
}

// Resource definition
type MCPResource struct {
    URI         string `json:"uri"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    MimeType    string `json:"mimeType,omitempty"`
}

// Prompt definition
type MCPPrompt struct {
    Name        string           `json:"name"`
    Description string           `json:"description,omitempty"`
    Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    Required    bool   `json:"required,omitempty"`
}

// Resource content
type ResourceContent struct {
    URI      string `json:"uri"`
    MimeType string `json:"mimeType,omitempty"`
    Text     string `json:"text,omitempty"`
    Blob     string `json:"blob,omitempty"`  // Base64
}

// Prompt message
type PromptMessage struct {
    Role    string          `json:"role"`  // user | assistant
    Content MessageContent  `json:"content"`
}

type MessageContent struct {
    Type     string          `json:"type"`  // text | image | resource
    Text     string          `json:"text,omitempty"`
    Data     string          `json:"data,omitempty"`
    MimeType string          `json:"mimeType,omitempty"`
    Resource *ResourceContent `json:"resource,omitempty"`
}
```

### 3.3 JSON-RPC Messages

```go
type JSONRPCRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      any             `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      any             `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}
```

---

## 4. Transport Layer

### 4.1 Transport Interface

```go
// internal/mcp/transport/transport.go

type Transport interface {
    // Lifecycle
    Connect(ctx context.Context) error
    Close() error

    // Request/response
    Call(ctx context.Context, method string, params any) (json.RawMessage, error)

    // Notifications (no response expected)
    Notify(ctx context.Context, method string, params any) error

    // Event stream
    Events() <-chan *JSONRPCNotification

    // Status
    Connected() bool
}
```

### 4.2 Stdio Transport

```go
// internal/mcp/transport/stdio.go

type StdioTransport struct {
    config  *ServerConfig
    process *exec.Cmd
    stdin   io.WriteCloser
    stdout  *bufio.Scanner
    stderr  io.ReadCloser

    pending   map[any]chan *JSONRPCResponse
    pendingMu sync.Mutex
    events    chan *JSONRPCNotification
    nextID    atomic.Int64

    connected atomic.Bool
    stopChan  chan struct{}
}

func NewStdioTransport(cfg *ServerConfig) *StdioTransport {
    return &StdioTransport{
        config:   cfg,
        pending:  make(map[any]chan *JSONRPCResponse),
        events:   make(chan *JSONRPCNotification, 100),
        stopChan: make(chan struct{}),
    }
}

func (t *StdioTransport) Connect(ctx context.Context) error {
    // Build command
    t.process = exec.CommandContext(ctx, t.config.Command, t.config.Args...)

    // Set environment
    t.process.Env = os.Environ()
    for k, v := range t.config.Env {
        t.process.Env = append(t.process.Env, fmt.Sprintf("%s=%s", k, v))
    }

    if t.config.WorkDir != "" {
        t.process.Dir = t.config.WorkDir
    }

    // Set up pipes
    var err error
    t.stdin, err = t.process.StdinPipe()
    if err != nil {
        return fmt.Errorf("stdin pipe: %w", err)
    }

    stdout, err := t.process.StdoutPipe()
    if err != nil {
        return fmt.Errorf("stdout pipe: %w", err)
    }
    t.stdout = bufio.NewScanner(stdout)
    t.stdout.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

    t.stderr, _ = t.process.StderrPipe()

    // Start process
    if err := t.process.Start(); err != nil {
        return fmt.Errorf("start process: %w", err)
    }

    t.connected.Store(true)

    // Start reader goroutine
    go t.readLoop()

    // Log stderr
    go t.logStderr()

    return nil
}

func (t *StdioTransport) readLoop() {
    defer t.connected.Store(false)

    for t.stdout.Scan() {
        line := t.stdout.Text()
        if line == "" {
            continue
        }

        // Try to parse as response
        var resp JSONRPCResponse
        if err := json.Unmarshal([]byte(line), &resp); err == nil && resp.ID != nil {
            t.pendingMu.Lock()
            if ch, ok := t.pending[resp.ID]; ok {
                ch <- &resp
                delete(t.pending, resp.ID)
            }
            t.pendingMu.Unlock()
            continue
        }

        // Try to parse as notification
        var notif JSONRPCNotification
        if err := json.Unmarshal([]byte(line), &notif); err == nil && notif.Method != "" {
            select {
            case t.events <- &notif:
            default:
                // Drop if channel full
            }
        }
    }
}

func (t *StdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
    id := t.nextID.Add(1)

    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      id,
        Method:  method,
    }

    if params != nil {
        paramsJSON, err := json.Marshal(params)
        if err != nil {
            return nil, fmt.Errorf("marshal params: %w", err)
        }
        req.Params = paramsJSON
    }

    // Create response channel
    respChan := make(chan *JSONRPCResponse, 1)
    t.pendingMu.Lock()
    t.pending[id] = respChan
    t.pendingMu.Unlock()

    // Send request
    data, _ := json.Marshal(req)
    if _, err := t.stdin.Write(append(data, '\n')); err != nil {
        t.pendingMu.Lock()
        delete(t.pending, id)
        t.pendingMu.Unlock()
        return nil, fmt.Errorf("write request: %w", err)
    }

    // Wait for response
    timeout := t.config.Timeout
    if timeout == 0 {
        timeout = 30 * time.Second
    }

    select {
    case resp := <-respChan:
        if resp.Error != nil {
            return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
        }
        return resp.Result, nil
    case <-ctx.Done():
        t.pendingMu.Lock()
        delete(t.pending, id)
        t.pendingMu.Unlock()
        return nil, ctx.Err()
    case <-time.After(timeout):
        t.pendingMu.Lock()
        delete(t.pending, id)
        t.pendingMu.Unlock()
        return nil, fmt.Errorf("request timeout after %v", timeout)
    }
}

func (t *StdioTransport) Close() error {
    close(t.stopChan)
    t.stdin.Close()
    return t.process.Wait()
}
```

### 4.3 HTTP/SSE Transport

```go
// internal/mcp/transport/http.go

type HTTPTransport struct {
    config    *ServerConfig
    client    *http.Client
    events    chan *JSONRPCNotification
    connected atomic.Bool
    stopChan  chan struct{}
}

func NewHTTPTransport(cfg *ServerConfig) *HTTPTransport {
    return &HTTPTransport{
        config: cfg,
        client: &http.Client{
            Timeout: cfg.Timeout,
        },
        events:   make(chan *JSONRPCNotification, 100),
        stopChan: make(chan struct{}),
    }
}

func (t *HTTPTransport) Connect(ctx context.Context) error {
    // Test connection with initialize
    _, err := t.Call(ctx, "initialize", map[string]any{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]any{},
        "clientInfo": map[string]any{
            "name":    "nexus",
            "version": "1.0.0",
        },
    })
    if err != nil {
        return err
    }

    t.connected.Store(true)

    // Start SSE listener for notifications
    go t.sseLoop(ctx)

    return nil
}

func (t *HTTPTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
    req := JSONRPCRequest{
        JSONRPC: "2.0",
        ID:      uuid.New().String(),
        Method:  method,
    }

    if params != nil {
        paramsJSON, _ := json.Marshal(params)
        req.Params = paramsJSON
    }

    body, _ := json.Marshal(req)

    httpReq, err := http.NewRequestWithContext(ctx, "POST", t.config.URL, bytes.NewReader(body))
    if err != nil {
        return nil, err
    }

    httpReq.Header.Set("Content-Type", "application/json")
    for k, v := range t.config.Headers {
        httpReq.Header.Set(k, v)
    }

    resp, err := t.client.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
    }

    var rpcResp JSONRPCResponse
    if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
        return nil, err
    }

    if rpcResp.Error != nil {
        return nil, fmt.Errorf("MCP error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
    }

    return rpcResp.Result, nil
}

func (t *HTTPTransport) sseLoop(ctx context.Context) {
    // Connect to SSE endpoint
    sseURL := strings.TrimSuffix(t.config.URL, "/") + "/sse"

    for {
        select {
        case <-ctx.Done():
            return
        case <-t.stopChan:
            return
        default:
        }

        req, _ := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
        req.Header.Set("Accept", "text/event-stream")

        resp, err := t.client.Do(req)
        if err != nil {
            time.Sleep(5 * time.Second)
            continue
        }

        scanner := bufio.NewScanner(resp.Body)
        for scanner.Scan() {
            line := scanner.Text()
            if strings.HasPrefix(line, "data: ") {
                data := strings.TrimPrefix(line, "data: ")
                var notif JSONRPCNotification
                if json.Unmarshal([]byte(data), &notif) == nil {
                    t.events <- &notif
                }
            }
        }
        resp.Body.Close()
    }
}
```

---

## 5. MCP Client

### 5.1 Client Implementation

```go
// internal/mcp/client.go

type Client struct {
    config    *ServerConfig
    transport Transport
    logger    *slog.Logger

    // Cached capabilities
    tools     []*MCPTool
    resources []*MCPResource
    prompts   []*MCPPrompt
    mu        sync.RWMutex

    // Server info
    serverName    string
    serverVersion string
}

func NewClient(cfg *ServerConfig) *Client {
    var transport Transport
    switch cfg.Transport {
    case TransportStdio:
        transport = NewStdioTransport(cfg)
    case TransportHTTP:
        transport = NewHTTPTransport(cfg)
    }

    return &Client{
        config:    cfg,
        transport: transport,
        logger:    slog.Default().With("mcp_server", cfg.ID),
    }
}

func (c *Client) Connect(ctx context.Context) error {
    if err := c.transport.Connect(ctx); err != nil {
        return fmt.Errorf("transport connect: %w", err)
    }

    // Initialize
    result, err := c.transport.Call(ctx, "initialize", map[string]any{
        "protocolVersion": "2024-11-05",
        "capabilities": map[string]any{
            "roots": map[string]any{
                "listChanged": true,
            },
        },
        "clientInfo": map[string]any{
            "name":    "nexus",
            "version": "1.0.0",
        },
    })
    if err != nil {
        return fmt.Errorf("initialize: %w", err)
    }

    var initResult struct {
        ProtocolVersion string `json:"protocolVersion"`
        ServerInfo      struct {
            Name    string `json:"name"`
            Version string `json:"version"`
        } `json:"serverInfo"`
    }
    json.Unmarshal(result, &initResult)

    c.serverName = initResult.ServerInfo.Name
    c.serverVersion = initResult.ServerInfo.Version

    c.logger.Info("connected to MCP server",
        "name", c.serverName,
        "version", c.serverVersion,
        "protocol", initResult.ProtocolVersion)

    // Send initialized notification
    c.transport.Notify(ctx, "notifications/initialized", nil)

    // Refresh capabilities
    return c.RefreshCapabilities(ctx)
}

func (c *Client) RefreshCapabilities(ctx context.Context) error {
    // List tools
    if result, err := c.transport.Call(ctx, "tools/list", nil); err == nil {
        var resp struct {
            Tools []*MCPTool `json:"tools"`
        }
        json.Unmarshal(result, &resp)
        c.mu.Lock()
        c.tools = resp.Tools
        c.mu.Unlock()
    }

    // List resources
    if result, err := c.transport.Call(ctx, "resources/list", nil); err == nil {
        var resp struct {
            Resources []*MCPResource `json:"resources"`
        }
        json.Unmarshal(result, &resp)
        c.mu.Lock()
        c.resources = resp.Resources
        c.mu.Unlock()
    }

    // List prompts
    if result, err := c.transport.Call(ctx, "prompts/list", nil); err == nil {
        var resp struct {
            Prompts []*MCPPrompt `json:"prompts"`
        }
        json.Unmarshal(result, &resp)
        c.mu.Lock()
        c.prompts = resp.Prompts
        c.mu.Unlock()
    }

    return nil
}

// Tools

func (c *Client) ListTools() []*MCPTool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.tools
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolResult, error) {
    result, err := c.transport.Call(ctx, "tools/call", map[string]any{
        "name":      name,
        "arguments": arguments,
    })
    if err != nil {
        return nil, err
    }

    var resp struct {
        Content []struct {
            Type string `json:"type"`
            Text string `json:"text,omitempty"`
        } `json:"content"`
        IsError bool `json:"isError,omitempty"`
    }
    json.Unmarshal(result, &resp)

    var text strings.Builder
    for _, c := range resp.Content {
        if c.Type == "text" {
            text.WriteString(c.Text)
        }
    }

    return &ToolResult{
        Content: text.String(),
        IsError: resp.IsError,
    }, nil
}

// Resources

func (c *Client) ListResources() []*MCPResource {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.resources
}

func (c *Client) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
    result, err := c.transport.Call(ctx, "resources/read", map[string]any{
        "uri": uri,
    })
    if err != nil {
        return nil, err
    }

    var resp struct {
        Contents []*ResourceContent `json:"contents"`
    }
    json.Unmarshal(result, &resp)

    if len(resp.Contents) == 0 {
        return nil, fmt.Errorf("resource not found: %s", uri)
    }

    return resp.Contents[0], nil
}

// Prompts

func (c *Client) ListPrompts() []*MCPPrompt {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.prompts
}

func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) ([]*PromptMessage, error) {
    result, err := c.transport.Call(ctx, "prompts/get", map[string]any{
        "name":      name,
        "arguments": arguments,
    })
    if err != nil {
        return nil, err
    }

    var resp struct {
        Messages []*PromptMessage `json:"messages"`
    }
    json.Unmarshal(result, &resp)

    return resp.Messages, nil
}

// Sampling (server requests completion from us)

func (c *Client) HandleSampling(handler SamplingHandler) {
    go func() {
        for notif := range c.transport.Events() {
            if notif.Method == "sampling/createMessage" {
                go c.handleSamplingRequest(notif, handler)
            }
        }
    }()
}

type SamplingHandler func(ctx context.Context, req *SamplingRequest) (*SamplingResponse, error)

type SamplingRequest struct {
    Messages      []SamplingMessage `json:"messages"`
    ModelPrefs    *ModelPrefs       `json:"modelPreferences,omitempty"`
    SystemPrompt  string            `json:"systemPrompt,omitempty"`
    MaxTokens     int               `json:"maxTokens"`
}

type SamplingResponse struct {
    Role    string `json:"role"`
    Content struct {
        Type string `json:"type"`
        Text string `json:"text"`
    } `json:"content"`
    Model      string `json:"model"`
    StopReason string `json:"stopReason,omitempty"`
}

func (c *Client) Close() error {
    return c.transport.Close()
}
```

---

## 6. MCP Manager

### 6.1 Implementation

```go
// internal/mcp/manager.go

type Manager struct {
    clients map[string]*Client  // serverID -> client
    config  *config.MCPConfig
    logger  *slog.Logger
    mu      sync.RWMutex
}

func NewManager(cfg *config.MCPConfig) *Manager {
    return &Manager{
        clients: make(map[string]*Client),
        config:  cfg,
        logger:  slog.Default().With("component", "mcp"),
    }
}

func (m *Manager) Start(ctx context.Context) error {
    for _, serverCfg := range m.config.Servers {
        if !serverCfg.AutoStart {
            continue
        }

        client := NewClient(&serverCfg)
        if err := client.Connect(ctx); err != nil {
            m.logger.Error("failed to connect to MCP server",
                "server", serverCfg.ID,
                "error", err)
            continue
        }

        m.mu.Lock()
        m.clients[serverCfg.ID] = client
        m.mu.Unlock()

        m.logger.Info("connected to MCP server",
            "server", serverCfg.ID,
            "tools", len(client.ListTools()),
            "resources", len(client.ListResources()))
    }

    return nil
}

func (m *Manager) GetClient(serverID string) (*Client, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    client, ok := m.clients[serverID]
    return client, ok
}

// Aggregate all tools from all servers
func (m *Manager) AllTools() []*BridgedTool {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var tools []*BridgedTool
    for serverID, client := range m.clients {
        for _, tool := range client.ListTools() {
            tools = append(tools, &BridgedTool{
                ServerID:    serverID,
                Tool:        tool,
                FullName:    fmt.Sprintf("mcp:%s.%s", serverID, tool.Name),
            })
        }
    }
    return tools
}

// Aggregate all resources from all servers
func (m *Manager) AllResources() []*BridgedResource {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var resources []*BridgedResource
    for serverID, client := range m.clients {
        for _, resource := range client.ListResources() {
            resources = append(resources, &BridgedResource{
                ServerID: serverID,
                Resource: resource,
            })
        }
    }
    return resources
}

func (m *Manager) CallTool(ctx context.Context, fullName string, arguments map[string]any) (*ToolResult, error) {
    // Parse "mcp:server.tool" format
    if !strings.HasPrefix(fullName, "mcp:") {
        return nil, fmt.Errorf("invalid MCP tool name: %s", fullName)
    }

    parts := strings.SplitN(strings.TrimPrefix(fullName, "mcp:"), ".", 2)
    if len(parts) != 2 {
        return nil, fmt.Errorf("invalid MCP tool name format: %s", fullName)
    }

    serverID, toolName := parts[0], parts[1]

    client, ok := m.GetClient(serverID)
    if !ok {
        return nil, fmt.Errorf("MCP server not found: %s", serverID)
    }

    return client.CallTool(ctx, toolName, arguments)
}

func (m *Manager) ReadResource(ctx context.Context, serverID, uri string) (*ResourceContent, error) {
    client, ok := m.GetClient(serverID)
    if !ok {
        return nil, fmt.Errorf("MCP server not found: %s", serverID)
    }

    return client.ReadResource(ctx, uri)
}

func (m *Manager) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    for _, client := range m.clients {
        client.Close()
    }
    return nil
}

// Bridged types for aggregation
type BridgedTool struct {
    ServerID string
    Tool     *MCPTool
    FullName string  // mcp:server.tool
}

type BridgedResource struct {
    ServerID string
    Resource *MCPResource
}
```

---

## 7. Tool Bridge

### 7.1 MCP Tool as Nexus Tool

```go
// internal/mcp/bridge.go

type MCPToolBridge struct {
    manager *Manager
    tool    *BridgedTool
}

func NewMCPToolBridge(mgr *Manager, tool *BridgedTool) *MCPToolBridge {
    return &MCPToolBridge{
        manager: mgr,
        tool:    tool,
    }
}

// Implement tools.Tool interface
func (b *MCPToolBridge) Name() string {
    return b.tool.FullName
}

func (b *MCPToolBridge) Description() string {
    return b.tool.Tool.Description
}

func (b *MCPToolBridge) Schema() json.RawMessage {
    return b.tool.Tool.InputSchema
}

func (b *MCPToolBridge) Execute(ctx context.Context, params json.RawMessage) (*tools.ToolResult, error) {
    var arguments map[string]any
    if err := json.Unmarshal(params, &arguments); err != nil {
        return nil, err
    }

    result, err := b.manager.CallTool(ctx, b.tool.FullName, arguments)
    if err != nil {
        return nil, err
    }

    return &tools.ToolResult{
        Content: result.Content,
    }, nil
}

// Register all MCP tools with agent runtime
func RegisterMCPTools(runtime *agent.Runtime, mgr *Manager) {
    for _, tool := range mgr.AllTools() {
        bridge := NewMCPToolBridge(mgr, tool)
        runtime.RegisterTool(bridge)
    }
}
```

---

## 8. Configuration

```yaml
# nexus.yaml
mcp:
  enabled: true

  servers:
    # Stdio server example
    - id: github
      name: GitHub MCP Server
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        GITHUB_TOKEN: ${GITHUB_TOKEN}
      auto_start: true
      timeout: 30s

    # Another stdio server
    - id: filesystem
      name: Filesystem MCP Server
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/projects"]
      auto_start: true

    # HTTP server example
    - id: custom
      name: Custom MCP Server
      transport: http
      url: http://localhost:3000/mcp
      headers:
        Authorization: Bearer ${MCP_TOKEN}
      auto_start: false
```

---

## 9. CLI Commands

```bash
# List configured servers
nexus mcp servers

# Connect to a server
nexus mcp connect <server-id>

# List tools from a server
nexus mcp tools [--server <id>]

# Call a tool
nexus mcp call <server.tool> --arg key=value

# List resources
nexus mcp resources [--server <id>]

# Read a resource
nexus mcp read <server> <uri>

# List prompts
nexus mcp prompts [--server <id>]

# Get prompt
nexus mcp prompt <server.prompt> --arg key=value
```

---

## 10. Implementation Phases

### Phase 1: Transport (Week 1)
- [ ] Transport interface
- [ ] Stdio transport implementation
- [ ] JSON-RPC request/response handling
- [ ] Basic error handling

### Phase 2: Client (Week 2)
- [ ] MCP Client implementation
- [ ] Initialize/capabilities negotiation
- [ ] Tools list/call
- [ ] Resources list/read
- [ ] Prompts list/get

### Phase 3: Manager & Bridge (Week 3)
- [ ] MCP Manager for multiple servers
- [ ] Server lifecycle management
- [ ] Tool aggregation
- [ ] Tool bridge to Nexus interface

### Phase 4: HTTP Transport & Polish (Week 4)
- [ ] HTTP/SSE transport
- [ ] Sampling support
- [ ] CLI commands
- [ ] Documentation

---

## Appendix: MCP Resources

- [MCP Specification](https://spec.modelcontextprotocol.io/)
- [MCP SDK](https://github.com/modelcontextprotocol/sdk)
- [MCP Servers](https://github.com/modelcontextprotocol/servers)
