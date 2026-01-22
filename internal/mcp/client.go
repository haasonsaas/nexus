package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Client is an MCP client that connects to a single server.
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
	serverInfo ServerInfo
}

// NewClient creates a new MCP client.
func NewClient(cfg *ServerConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		config:    cfg,
		transport: NewTransport(cfg),
		logger:    logger.With("mcp_server", cfg.ID),
	}
}

// Connect establishes the connection to the MCP server.
func (c *Client) Connect(ctx context.Context) error {
	// Connect transport
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
		c.transport.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	var initResult InitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		c.transport.Close()
		return fmt.Errorf("parse initialize result: %w", err)
	}

	c.serverInfo = initResult.ServerInfo
	c.logger.Info("connected to MCP server",
		"name", c.serverInfo.Name,
		"version", c.serverInfo.Version,
		"protocol", initResult.ProtocolVersion)

	// Send initialized notification
	if err := c.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		c.logger.Warn("failed to send initialized notification", "error", err)
	}

	// Refresh capabilities
	if err := c.RefreshCapabilities(ctx); err != nil {
		c.logger.Warn("failed to refresh capabilities", "error", err)
	}

	return nil
}

// Close closes the connection to the MCP server.
func (c *Client) Close() error {
	return c.transport.Close()
}

// Config returns the server configuration.
func (c *Client) Config() *ServerConfig {
	return c.config
}

// ServerInfo returns information about the connected server.
func (c *Client) ServerInfo() ServerInfo {
	return c.serverInfo
}

// Connected returns whether the client is connected.
func (c *Client) Connected() bool {
	return c.transport.Connected()
}

// RefreshCapabilities refreshes the cached tools, resources, and prompts.
func (c *Client) RefreshCapabilities(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// List tools
	if result, err := c.transport.Call(ctx, "tools/list", nil); err == nil {
		var resp ListToolsResult
		if json.Unmarshal(result, &resp) == nil {
			c.tools = resp.Tools
			c.logger.Debug("refreshed tools", "count", len(c.tools))
		}
	}

	// List resources
	if result, err := c.transport.Call(ctx, "resources/list", nil); err == nil {
		var resp ListResourcesResult
		if json.Unmarshal(result, &resp) == nil {
			c.resources = resp.Resources
			c.logger.Debug("refreshed resources", "count", len(c.resources))
		}
	}

	// List prompts
	if result, err := c.transport.Call(ctx, "prompts/list", nil); err == nil {
		var resp ListPromptsResult
		if json.Unmarshal(result, &resp) == nil {
			c.prompts = resp.Prompts
			c.logger.Debug("refreshed prompts", "count", len(c.prompts))
		}
	}

	return nil
}

// Tools returns the cached tools.
func (c *Client) Tools() []*MCPTool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

// Resources returns the cached resources.
func (c *Client) Resources() []*MCPResource {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resources
}

// Prompts returns the cached prompts.
func (c *Client) Prompts() []*MCPPrompt {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.prompts
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	params := CallToolParams{
		Name: name,
	}

	if arguments != nil {
		argsJSON, err := json.Marshal(arguments)
		if err != nil {
			return nil, fmt.Errorf("marshal arguments: %w", err)
		}
		params.Arguments = argsJSON
	}

	result, err := c.transport.Call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}

	var callResult ToolCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}

	return &callResult, nil
}

// ReadResource reads a resource from the MCP server.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]*ResourceContent, error) {
	result, err := c.transport.Call(ctx, "resources/read", map[string]any{
		"uri": uri,
	})
	if err != nil {
		return nil, err
	}

	var readResult ReadResourceResult
	if err := json.Unmarshal(result, &readResult); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}

	return readResult.Contents, nil
}

// GetPrompt gets a prompt from the MCP server.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*GetPromptResult, error) {
	result, err := c.transport.Call(ctx, "prompts/get", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return nil, err
	}

	var promptResult GetPromptResult
	if err := json.Unmarshal(result, &promptResult); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}

	return &promptResult, nil
}

// Events returns the notification channel.
func (c *Client) Events() <-chan *JSONRPCNotification {
	return c.transport.Events()
}

// SamplingHandler handles server-initiated sampling requests.
type SamplingHandler func(ctx context.Context, req *SamplingRequest) (*SamplingResponse, error)

// HandleSampling starts processing sampling requests from the server.
func (c *Client) HandleSampling(handler SamplingHandler) {
	if handler == nil {
		return
	}
	go func() {
		for req := range c.transport.Requests() {
			if req == nil || req.Method != "sampling/createMessage" {
				continue
			}
			go c.handleSamplingRequest(req, handler)
		}
	}()
}

func (c *Client) handleSamplingRequest(req *JSONRPCRequest, handler SamplingHandler) {
	ctx := context.Background()
	timeout := c.config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var params SamplingRequest
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = c.transport.Respond(ctx, req.ID, nil, &JSONRPCError{
				Code:    ErrCodeInvalidParams,
				Message: "invalid sampling params",
			})
			return
		}
	}

	response, err := handler(ctx, &params)
	if err != nil {
		_ = c.transport.Respond(ctx, req.ID, nil, &JSONRPCError{
			Code:    ErrCodeInternalError,
			Message: err.Error(),
		})
		return
	}
	if response == nil {
		_ = c.transport.Respond(ctx, req.ID, nil, &JSONRPCError{
			Code:    ErrCodeInternalError,
			Message: "sampling handler returned nil response",
		})
		return
	}

	if err := c.transport.Respond(ctx, req.ID, response, nil); err != nil {
		c.logger.Warn("failed to respond to sampling request", "error", err)
	}
}
