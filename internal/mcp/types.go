// Package mcp provides a Model Context Protocol (MCP) client implementation.
package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// TransportType specifies the MCP transport protocol.
type TransportType string

const (
	TransportStdio TransportType = "stdio"
	TransportHTTP  TransportType = "http"
)

// ServerConfig holds configuration for an MCP server.
type ServerConfig struct {
	ID        string        `yaml:"id" json:"id"`
	Name      string        `yaml:"name" json:"name"`
	Transport TransportType `yaml:"transport" json:"transport"`

	// Stdio transport options
	Command string            `yaml:"command" json:"command,omitempty"`
	Args    []string          `yaml:"args" json:"args,omitempty"`
	Env     map[string]string `yaml:"env" json:"env,omitempty"`
	WorkDir string            `yaml:"workdir" json:"workdir,omitempty"`

	// HTTP transport options
	URL     string            `yaml:"url" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`

	// Common options
	Timeout   time.Duration `yaml:"timeout" json:"timeout,omitempty"`
	AutoStart bool          `yaml:"auto_start" json:"auto_start,omitempty"`
}

// Validate checks the server configuration for security issues.
func (c *ServerConfig) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("server ID is required")
	}

	if c.Transport == TransportStdio {
		if err := c.validateStdioConfig(); err != nil {
			return fmt.Errorf("stdio config for %s: %w", c.ID, err)
		}
	}

	if c.Transport == TransportHTTP {
		if err := c.validateHTTPConfig(); err != nil {
			return fmt.Errorf("http config for %s: %w", c.ID, err)
		}
	}

	return nil
}

// validateStdioConfig validates stdio transport configuration.
func (c *ServerConfig) validateStdioConfig() error {
	if c.Command == "" {
		return fmt.Errorf("command is required")
	}

	// Check for path traversal in command
	if err := validatePath(c.Command, "command"); err != nil {
		return err
	}

	// Check for path traversal in work directory
	if c.WorkDir != "" {
		if err := validatePath(c.WorkDir, "workdir"); err != nil {
			return err
		}
	}

	// Check for suspicious shell metacharacters in args
	for i, arg := range c.Args {
		if containsShellMetachars(arg) {
			return fmt.Errorf("arg[%d] contains suspicious shell metacharacters: %q", i, arg)
		}
	}

	return nil
}

// validateHTTPConfig validates HTTP transport configuration.
func (c *ServerConfig) validateHTTPConfig() error {
	if c.URL == "" {
		return fmt.Errorf("URL is required")
	}

	// Basic URL validation - must start with http:// or https://
	if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}

	return nil
}

// validatePath checks a path for traversal attacks.
func validatePath(path, fieldName string) error {
	if path == "" {
		return nil
	}

	// Clean the path
	cleaned := filepath.Clean(path)

	// Check for path traversal after cleaning
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("%s contains path traversal: %q", fieldName, path)
	}

	return nil
}

// containsShellMetachars checks for shell metacharacters that could indicate injection.
func containsShellMetachars(s string) bool {
	// Only flag the most dangerous patterns that suggest command chaining
	// We allow spaces, quotes, etc. since they're common in legitimate args
	dangerousPatterns := []string{
		"$(", "${", // Command substitution
		"`",        // Backtick substitution
		"&&", "||", // Command chaining
		";",      // Command separator
		"|",      // Pipe
		">", "<", // Redirection
		"\n", "\r", // Newlines
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(s, pattern) {
			return true
		}
	}
	return false
}

// MCPTool represents a tool exposed by an MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// MCPResource represents a resource exposed by an MCP server.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// MCPPrompt represents a prompt template exposed by an MCP server.
type MCPPrompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes a parameter for an MCP prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ResourceContent holds the content of an MCP resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // Base64 encoded
}

// PromptMessage represents a message in a prompt response.
type PromptMessage struct {
	Role    string         `json:"role"` // user | assistant
	Content MessageContent `json:"content"`
}

// MessageContent holds the content of a prompt message.
type MessageContent struct {
	Type     string           `json:"type"` // text | image | resource
	Text     string           `json:"text,omitempty"`
	Data     string           `json:"data,omitempty"`
	MimeType string           `json:"mimeType,omitempty"`
	Resource *ResourceContent `json:"resource,omitempty"`
}

// SamplingMessage represents a message for sampling requests.
type SamplingMessage struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

// ModelPreferences describes preferred models for sampling.
type ModelPreferences struct {
	Hints []ModelHint `json:"hints,omitempty"`
}

// ModelHint suggests a model name.
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// SamplingRequest represents a server-initiated sampling request.
type SamplingRequest struct {
	Messages     []SamplingMessage `json:"messages"`
	ModelPrefs   *ModelPreferences `json:"modelPreferences,omitempty"`
	SystemPrompt string            `json:"systemPrompt,omitempty"`
	MaxTokens    int               `json:"maxTokens,omitempty"`
	Model        string            `json:"model,omitempty"`
}

// SamplingResponse represents a client response to a sampling request.
type SamplingResponse struct {
	Role       string         `json:"role"`
	Content    MessageContent `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stopReason,omitempty"`
}

// ToolCallResult holds the result of calling an MCP tool.
type ToolCallResult struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// ToolResultContent holds a piece of content from a tool result.
type ToolResultContent struct {
	Type     string `json:"type"` // text | image | resource
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// JSON-RPC types

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCNotification is a JSON-RPC 2.0 notification (no ID).
type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// MCP-specific error codes
const (
	ErrCodeResourceNotFound = -32001
	ErrCodeToolNotFound     = -32002
	ErrCodePromptNotFound   = -32003
)

// ServerInfo holds information about an MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientInfo holds information about the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities holds the capabilities of an MCP client or server.
type Capabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
	Sampling  *SamplingCapability  `json:"sampling,omitempty"`
	Roots     *RootsCapability     `json:"roots,omitempty"`
}

// ToolsCapability describes tool-related capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability describes resource-related capabilities.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability describes prompt-related capabilities.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability describes sampling-related capabilities.
type SamplingCapability struct{}

// RootsCapability describes roots-related capabilities.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeResult holds the result of the initialize method.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// ListToolsResult holds the result of tools/list.
type ListToolsResult struct {
	Tools []*MCPTool `json:"tools"`
}

// ListResourcesResult holds the result of resources/list.
type ListResourcesResult struct {
	Resources []*MCPResource `json:"resources"`
}

// ListPromptsResult holds the result of prompts/list.
type ListPromptsResult struct {
	Prompts []*MCPPrompt `json:"prompts"`
}

// ReadResourceResult holds the result of resources/read.
type ReadResourceResult struct {
	Contents []*ResourceContent `json:"contents"`
}

// GetPromptResult holds the result of prompts/get.
type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// CallToolParams holds parameters for tools/call.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
