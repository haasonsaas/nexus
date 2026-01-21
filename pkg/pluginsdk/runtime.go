package pluginsdk

import (
	"context"
	"encoding/json"
)

// ToolDefinition describes a tool exposed by a runtime plugin.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// ToolResult contains the output from a plugin tool execution.
type ToolResult struct {
	Content string
	IsError bool
}

// ToolHandler executes a plugin tool with JSON arguments.
type ToolHandler func(ctx context.Context, params json.RawMessage) (*ToolResult, error)

// ChannelRegistry allows plugins to register channel adapters.
type ChannelRegistry interface {
	RegisterChannel(adapter ChannelAdapter) error
}

// ToolRegistry allows plugins to register tools.
type ToolRegistry interface {
	RegisterTool(def ToolDefinition, handler ToolHandler) error
}

// RuntimePlugin is the interface runtime plugins must implement.
type RuntimePlugin interface {
	Manifest() *Manifest
	RegisterChannels(registry ChannelRegistry, cfg map[string]any) error
	RegisterTools(registry ToolRegistry, cfg map[string]any) error
}
