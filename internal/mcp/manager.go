package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// Manager manages multiple MCP server connections.
type Manager struct {
	config  *Config
	logger  *slog.Logger
	clients map[string]*Client
	mu      sync.RWMutex
}

// Config holds the MCP manager configuration.
type Config struct {
	Enabled bool            `yaml:"enabled"`
	Servers []*ServerConfig `yaml:"servers"`
}

// NewManager creates a new MCP manager.
func NewManager(cfg *Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		config:  cfg,
		logger:  logger.With("component", "mcp"),
		clients: make(map[string]*Client),
	}
}

// Start connects to all configured MCP servers with auto_start enabled.
func (m *Manager) Start(ctx context.Context) error {
	if m.config == nil || !m.config.Enabled {
		m.logger.Debug("MCP disabled")
		return nil
	}

	for _, serverCfg := range m.config.Servers {
		if !serverCfg.AutoStart {
			continue
		}

		if err := m.Connect(ctx, serverCfg.ID); err != nil {
			m.logger.Error("failed to connect to MCP server",
				"server", serverCfg.ID,
				"error", err)
			// Continue with other servers
		}
	}

	return nil
}

// Stop disconnects from all MCP servers.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, client := range m.clients {
		if err := client.Close(); err != nil {
			m.logger.Error("failed to close MCP client",
				"server", id,
				"error", err)
		}
		delete(m.clients, id)
	}

	return nil
}

// Connect connects to a specific MCP server by ID.
func (m *Manager) Connect(ctx context.Context, serverID string) error {
	// Find server config
	var serverCfg *ServerConfig
	for _, cfg := range m.config.Servers {
		if cfg.ID == serverID {
			serverCfg = cfg
			break
		}
	}

	if serverCfg == nil {
		return fmt.Errorf("server %q not found in config", serverID)
	}

	// Check if already connected
	m.mu.RLock()
	if _, exists := m.clients[serverID]; exists {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	// Create and connect client
	client := NewClient(serverCfg, m.logger)
	if err := client.Connect(ctx); err != nil {
		return err
	}

	m.mu.Lock()
	m.clients[serverID] = client
	m.mu.Unlock()

	m.logger.Info("connected to MCP server",
		"server", serverID,
		"name", client.ServerInfo().Name)

	return nil
}

// Disconnect disconnects from a specific MCP server.
func (m *Manager) Disconnect(serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[serverID]
	if !exists {
		return nil
	}

	if err := client.Close(); err != nil {
		return err
	}

	delete(m.clients, serverID)
	m.logger.Info("disconnected from MCP server", "server", serverID)

	return nil
}

// Client returns a client for a specific server.
func (m *Manager) Client(serverID string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, exists := m.clients[serverID]
	return client, exists
}

// Clients returns all connected clients.
func (m *Manager) Clients() map[string]*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*Client, len(m.clients))
	for id, client := range m.clients {
		result[id] = client
	}
	return result
}

// AllTools returns all tools from all connected servers.
func (m *Manager) AllTools() map[string][]*MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]*MCPTool)
	for id, client := range m.clients {
		if tools := client.Tools(); len(tools) > 0 {
			result[id] = tools
		}
	}
	return result
}

// AllResources returns all resources from all connected servers.
func (m *Manager) AllResources() map[string][]*MCPResource {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]*MCPResource)
	for id, client := range m.clients {
		if resources := client.Resources(); len(resources) > 0 {
			result[id] = resources
		}
	}
	return result
}

// AllPrompts returns all prompts from all connected servers.
func (m *Manager) AllPrompts() map[string][]*MCPPrompt {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]*MCPPrompt)
	for id, client := range m.clients {
		if prompts := client.Prompts(); len(prompts) > 0 {
			result[id] = prompts
		}
	}
	return result
}

// CallTool calls a tool on a specific server.
func (m *Manager) CallTool(ctx context.Context, serverID string, toolName string, arguments map[string]any) (*ToolCallResult, error) {
	client, exists := m.Client(serverID)
	if !exists {
		return nil, fmt.Errorf("server %q not connected", serverID)
	}

	return client.CallTool(ctx, toolName, arguments)
}

// FindTool finds a tool by name across all servers.
// Returns the server ID and tool definition, or empty string if not found.
func (m *Manager) FindTool(name string) (serverID string, tool *MCPTool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, client := range m.clients {
		for _, t := range client.Tools() {
			if t.Name == name {
				return id, t
			}
		}
	}
	return "", nil
}

// ReadResource reads a resource from a specific server.
func (m *Manager) ReadResource(ctx context.Context, serverID string, uri string) ([]*ResourceContent, error) {
	client, exists := m.Client(serverID)
	if !exists {
		return nil, fmt.Errorf("server %q not connected", serverID)
	}

	return client.ReadResource(ctx, uri)
}

// GetPrompt gets a prompt from a specific server.
func (m *Manager) GetPrompt(ctx context.Context, serverID string, name string, arguments map[string]string) (*GetPromptResult, error) {
	client, exists := m.Client(serverID)
	if !exists {
		return nil, fmt.Errorf("server %q not connected", serverID)
	}

	return client.GetPrompt(ctx, name, arguments)
}

// ToolSchema represents the JSON schema for a tool, used by LLMs.
type ToolSchema struct {
	ServerID    string          `json:"server_id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolSchemas returns tool schemas suitable for LLM tool definitions.
func (m *Manager) ToolSchemas() []ToolSchema {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var schemas []ToolSchema
	for id, client := range m.clients {
		for _, tool := range client.Tools() {
			schemas = append(schemas, ToolSchema{
				ServerID:    id,
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
	}
	return schemas
}

// ServerStatus represents the status of an MCP server.
type ServerStatus struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Connected bool       `json:"connected"`
	Server    ServerInfo `json:"server"`
	Tools     int        `json:"tools"`
	Resources int        `json:"resources"`
	Prompts   int        `json:"prompts"`
}

// Status returns the status of all configured servers.
func (m *Manager) Status() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []ServerStatus
	for _, cfg := range m.config.Servers {
		status := ServerStatus{
			ID:   cfg.ID,
			Name: cfg.Name,
		}

		if client, exists := m.clients[cfg.ID]; exists {
			status.Connected = client.Connected()
			status.Server = client.ServerInfo()
			status.Tools = len(client.Tools())
			status.Resources = len(client.Resources())
			status.Prompts = len(client.Prompts())
		}

		statuses = append(statuses, status)
	}

	return statuses
}
