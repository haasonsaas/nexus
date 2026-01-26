package pluginsdk

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"
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

// =============================================================================
// CLI Command Registration
// =============================================================================

// CLICommand represents a CLI command registered by a plugin.
type CLICommand struct {
	// Use is the one-line usage message (e.g., "search [query]").
	Use string

	// Short is a short description shown in help.
	Short string

	// Long is a long description shown in help.
	Long string

	// Example shows example usage.
	Example string

	// Args specifies argument validation (optional).
	Args cobra.PositionalArgs

	// Run is the command handler function.
	Run func(cmd *cobra.Command, args []string) error

	// Flags allows setting up command flags.
	// Called during command registration with the command's FlagSet.
	Flags func(cmd *cobra.Command)

	// Subcommands allows nesting commands.
	Subcommands []*CLICommand
}

// CLIRegistry allows plugins to register CLI commands.
type CLIRegistry interface {
	// RegisterCommand registers a top-level CLI command.
	// The command will be accessible as "nexus <command>".
	RegisterCommand(cmd *CLICommand) error

	// RegisterSubcommand registers a command under an existing parent.
	// Parent is specified as a path like "memory" or "plugins.tools".
	RegisterSubcommand(parent string, cmd *CLICommand) error
}

// =============================================================================
// Service Lifecycle
// =============================================================================

// Service represents a background service managed by a plugin.
type Service struct {
	// ID is a unique identifier for this service.
	ID string

	// Name is a human-readable name for display.
	Name string

	// Description explains what the service does.
	Description string

	// Start is called when the gateway starts.
	// Should return quickly; use goroutines for long-running work.
	Start func(ctx context.Context) error

	// Stop is called during graceful shutdown.
	// Should clean up resources and stop goroutines.
	Stop func(ctx context.Context) error

	// HealthCheck returns nil if the service is healthy.
	// Called periodically for status reporting.
	HealthCheck func(ctx context.Context) error
}

// ServiceRegistry allows plugins to register background services.
type ServiceRegistry interface {
	// RegisterService registers a background service.
	RegisterService(svc *Service) error
}

// =============================================================================
// Hook Registration
// =============================================================================

// HookHandler is a function that processes hook events.
type HookHandler func(ctx context.Context, event *HookEvent) error

// HookEvent contains data passed to hook handlers.
type HookEvent struct {
	// Type is the event type (e.g., "agent.started", "message.received").
	Type string

	// SessionID is the session identifier (if applicable).
	SessionID string

	// ChannelID is the channel identifier (if applicable).
	ChannelID string

	// Data contains event-specific data.
	Data map[string]any
}

// HookRegistration configures a hook registration.
type HookRegistration struct {
	// EventType is the event to listen for.
	EventType string

	// Handler is the function to call.
	Handler HookHandler

	// Priority determines call order (lower = earlier, default = 50).
	Priority int

	// Name is a human-readable name for debugging.
	Name string
}

// HookRegistry allows plugins to register event hooks.
type HookRegistry interface {
	// RegisterHook registers a hook for an event type.
	RegisterHook(reg *HookRegistration) error
}

// =============================================================================
// Plugin API
// =============================================================================

// PluginAPI provides access to all plugin registration interfaces.
type PluginAPI struct {
	// Channels for registering channel adapters.
	Channels ChannelRegistry

	// Tools for registering AI tools.
	Tools ToolRegistry

	// CLI for registering CLI commands.
	CLI CLIRegistry

	// Services for registering background services.
	Services ServiceRegistry

	// Hooks for registering event hooks.
	Hooks HookRegistry

	// Config contains the plugin's configuration from nexus.yaml.
	Config map[string]any

	// Logger provides a scoped logger for the plugin.
	Logger PluginLogger

	// ResolvePath resolves a path relative to the workspace.
	ResolvePath func(path string) string
}

// PluginLogger provides logging for plugins.
type PluginLogger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// =============================================================================
// Runtime Plugin Interface
// =============================================================================

// RuntimePlugin is the interface runtime plugins must implement.
type RuntimePlugin interface {
	Manifest() *Manifest
	RegisterChannels(registry ChannelRegistry, cfg map[string]any) error
	RegisterTools(registry ToolRegistry, cfg map[string]any) error
}

// ExtendedPlugin extends RuntimePlugin with additional registration methods.
// Plugins can implement this interface for CLI, services, and hooks support.
type ExtendedPlugin interface {
	RuntimePlugin

	// RegisterCLI registers CLI commands for the plugin.
	// Called during gateway initialization.
	RegisterCLI(registry CLIRegistry, cfg map[string]any) error

	// RegisterServices registers background services.
	// Called during gateway startup.
	RegisterServices(registry ServiceRegistry, cfg map[string]any) error

	// RegisterHooks registers event hooks.
	// Called during gateway initialization.
	RegisterHooks(registry HookRegistry, cfg map[string]any) error
}

// FullPlugin provides all registration methods through a single API.
// This is the recommended interface for new plugins.
type FullPlugin interface {
	Manifest() *Manifest

	// Register is called with the full plugin API.
	// Plugins should register all their components here.
	Register(api *PluginAPI) error
}
