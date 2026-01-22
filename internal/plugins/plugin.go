package plugins

import (
	"context"
	"fmt"
	"sync"
)

// Plugin represents a loaded plugin.
type Plugin interface {
	// ID returns the unique identifier for this plugin.
	ID() string

	// Name returns the display name.
	Name() string

	// Description returns a brief description.
	Description() string

	// Version returns the plugin version.
	Version() string
}

// PluginStatus indicates the current state of a plugin.
type PluginStatus string

const (
	PluginStatusLoaded   PluginStatus = "loaded"
	PluginStatusDisabled PluginStatus = "disabled"
	PluginStatusError    PluginStatus = "error"
)

// PluginRecord contains metadata about a registered plugin.
type PluginRecord struct {
	ID          string
	Name        string
	Description string
	Version     string
	Source      string
	Status      PluginStatus
	Error       string
	Enabled     bool

	// Capabilities
	Tools           []string
	Channels        []string
	Providers       []string
	GatewayMethods  []string
	Commands        []string
	Services        []string
	HTTPHandlers    int
	HasConfigSchema bool
}

// PluginConfig configures plugin loading.
type PluginConfig struct {
	// Enabled controls whether plugins are loaded at all.
	Enabled bool

	// Allow is an allowlist of plugin IDs. Empty means all allowed.
	Allow []string

	// Deny is a denylist of plugin IDs.
	Deny []string

	// Paths is a list of directories to search for plugins.
	Paths []string

	// Entries contains per-plugin configuration.
	Entries map[string]PluginEntryConfig
}

// PluginEntryConfig contains per-plugin configuration.
type PluginEntryConfig struct {
	Enabled *bool
	Config  map[string]any
}

// DiagnosticLevel indicates severity of a diagnostic message.
type DiagnosticLevel string

const (
	DiagnosticInfo  DiagnosticLevel = "info"
	DiagnosticWarn  DiagnosticLevel = "warn"
	DiagnosticError DiagnosticLevel = "error"
)

// Diagnostic represents a message about plugin loading.
type Diagnostic struct {
	Level    DiagnosticLevel
	PluginID string
	Source   string
	Message  string
}

// PluginAPI provides capabilities to plugins during registration.
type PluginAPI struct {
	record   *PluginRecord
	registry *Registry

	// AppConfig is the application configuration.
	AppConfig map[string]any

	// PluginConfig is the plugin-specific configuration.
	PluginConfig map[string]any
}

// RegisterTool registers a tool provided by this plugin.
func (api *PluginAPI) RegisterTool(name string, handler any) {
	api.record.Tools = append(api.record.Tools, name)
	api.registry.tools[name] = handler
}

// RegisterChannel registers a channel provided by this plugin.
func (api *PluginAPI) RegisterChannel(id string, handler any) {
	api.record.Channels = append(api.record.Channels, id)
	api.registry.channels[id] = handler
}

// RegisterProvider registers an AI provider.
func (api *PluginAPI) RegisterProvider(id string, handler any) {
	api.record.Providers = append(api.record.Providers, id)
	api.registry.providers[id] = handler
}

// RegisterGatewayMethod registers a gateway RPC method.
func (api *PluginAPI) RegisterGatewayMethod(name string, handler any) {
	api.record.GatewayMethods = append(api.record.GatewayMethods, name)
	api.registry.gatewayMethods[name] = handler
}

// RegisterCommand registers a CLI command.
func (api *PluginAPI) RegisterCommand(name string, handler any) {
	api.record.Commands = append(api.record.Commands, name)
	api.registry.commands[name] = handler
}

// RegisterService registers a background service.
func (api *PluginAPI) RegisterService(name string, service any) {
	api.record.Services = append(api.record.Services, name)
	api.registry.services[name] = service
}

// RegisterHTTPHandler registers an HTTP handler.
func (api *PluginAPI) RegisterHTTPHandler(pattern string, handler any) {
	api.record.HTTPHandlers++
	api.registry.httpHandlers[pattern] = handler
}

// Logger returns a logger for this plugin.
func (api *PluginAPI) Logger() Logger {
	return api.registry.logger
}

// Logger is a minimal logging interface.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// RegisterFunc is the function signature for plugin registration.
type RegisterFunc func(api *PluginAPI) error

// PluginDefinition defines a plugin's metadata and registration.
type PluginDefinition struct {
	ID           string
	Name         string
	Description  string
	Version      string
	ConfigSchema any // Optional schema for validation
	Register     RegisterFunc
}

// Registry manages loaded plugins.
type Registry struct {
	mu          sync.RWMutex
	plugins     []*PluginRecord
	definitions map[string]*PluginDefinition
	diagnostics []Diagnostic
	logger      Logger

	// Registered capabilities
	tools          map[string]any
	channels       map[string]any
	providers      map[string]any
	gatewayMethods map[string]any
	commands       map[string]any
	services       map[string]any
	httpHandlers   map[string]any
}

// NewRegistry creates a new plugin registry.
func NewRegistry(logger Logger) *Registry {
	if logger == nil {
		logger = &noopLogger{}
	}

	return &Registry{
		plugins:        make([]*PluginRecord, 0),
		definitions:    make(map[string]*PluginDefinition),
		diagnostics:    make([]Diagnostic, 0),
		logger:         logger,
		tools:          make(map[string]any),
		channels:       make(map[string]any),
		providers:      make(map[string]any),
		gatewayMethods: make(map[string]any),
		commands:       make(map[string]any),
		services:       make(map[string]any),
		httpHandlers:   make(map[string]any),
	}
}

// Register registers a plugin definition.
func (r *Registry) Register(def *PluginDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if def.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}

	if _, exists := r.definitions[def.ID]; exists {
		return fmt.Errorf("plugin %s already registered", def.ID)
	}

	r.definitions[def.ID] = def
	return nil
}

// Load loads all registered plugins with the given configuration.
func (r *Registry) Load(ctx context.Context, config *PluginConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if config == nil {
		config = &PluginConfig{Enabled: true}
	}

	if !config.Enabled {
		r.diagnostics = append(r.diagnostics, Diagnostic{
			Level:   DiagnosticInfo,
			Message: "plugins disabled",
		})
		return nil
	}

	for id, def := range r.definitions {
		record := &PluginRecord{
			ID:          id,
			Name:        def.Name,
			Description: def.Description,
			Version:     def.Version,
			Source:      "builtin",
		}

		// Check enable state
		enableState := r.resolveEnableState(id, config)
		if !enableState.enabled {
			record.Status = PluginStatusDisabled
			record.Error = enableState.reason
			record.Enabled = false
			r.plugins = append(r.plugins, record)
			continue
		}

		record.Enabled = true

		// Get plugin config
		var pluginConfig map[string]any
		if entry, ok := config.Entries[id]; ok {
			pluginConfig = entry.Config
		}

		// Create API
		api := &PluginAPI{
			record:       record,
			registry:     r,
			PluginConfig: pluginConfig,
		}

		// Call register function
		if def.Register != nil {
			if err := def.Register(api); err != nil {
				record.Status = PluginStatusError
				record.Error = err.Error()
				r.diagnostics = append(r.diagnostics, Diagnostic{
					Level:    DiagnosticError,
					PluginID: id,
					Message:  fmt.Sprintf("failed to register: %v", err),
				})
				r.plugins = append(r.plugins, record)
				continue
			}
		}

		record.Status = PluginStatusLoaded
		record.HasConfigSchema = def.ConfigSchema != nil
		r.plugins = append(r.plugins, record)

		r.logger.Info("plugin loaded", "id", id, "name", def.Name)
	}

	return nil
}

type enableState struct {
	enabled bool
	reason  string
}

func (r *Registry) resolveEnableState(id string, config *PluginConfig) enableState {
	if !config.Enabled {
		return enableState{false, "plugins disabled"}
	}

	// Check denylist
	for _, denied := range config.Deny {
		if denied == id {
			return enableState{false, "blocked by denylist"}
		}
	}

	// Check allowlist
	if len(config.Allow) > 0 {
		found := false
		for _, allowed := range config.Allow {
			if allowed == id {
				found = true
				break
			}
		}
		if !found {
			return enableState{false, "not in allowlist"}
		}
	}

	// Check per-plugin config
	if entry, ok := config.Entries[id]; ok {
		if entry.Enabled != nil && !*entry.Enabled {
			return enableState{false, "disabled in config"}
		}
	}

	return enableState{true, ""}
}

// Plugins returns all plugin records.
func (r *Registry) Plugins() []*PluginRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*PluginRecord, len(r.plugins))
	copy(result, r.plugins)
	return result
}

// Plugin returns a plugin record by ID.
func (r *Registry) Plugin(id string) (*PluginRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.plugins {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// Diagnostics returns all diagnostic messages.
func (r *Registry) Diagnostics() []Diagnostic {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Diagnostic, len(r.diagnostics))
	copy(result, r.diagnostics)
	return result
}

// Tool returns a registered tool by name.
func (r *Registry) Tool(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Channel returns a registered channel by ID.
func (r *Registry) Channel(id string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.channels[id]
	return c, ok
}

// Provider returns a registered provider by ID.
func (r *Registry) Provider(id string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// GatewayMethod returns a registered gateway method by name.
func (r *Registry) GatewayMethod(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.gatewayMethods[name]
	return m, ok
}

// Command returns a registered command by name.
func (r *Registry) Command(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.commands[name]
	return c, ok
}

// Service returns a registered service by name.
func (r *Registry) Service(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.services[name]
	return s, ok
}

// ToolNames returns all registered tool names.
func (r *Registry) ToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// ChannelIDs returns all registered channel IDs.
func (r *Registry) ChannelIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.channels))
	for id := range r.channels {
		ids = append(ids, id)
	}
	return ids
}

// ProviderIDs returns all registered provider IDs.
func (r *Registry) ProviderIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

type noopLogger struct{}

func (l *noopLogger) Info(_ string, _ ...any) {}
func (l *noopLogger) Warn(_ string, _ ...any) {}
func (l *noopLogger) Error(_ string, _ ...any) {}

// DefaultRegistry is the global plugin registry.
var DefaultRegistry = NewRegistry(nil)

// Register registers a plugin with the default registry.
func RegisterPlugin(def *PluginDefinition) error {
	return DefaultRegistry.Register(def)
}

// LoadPlugins loads all plugins with the default registry.
func LoadPlugins(ctx context.Context, config *PluginConfig) error {
	return DefaultRegistry.Load(ctx, config)
}
