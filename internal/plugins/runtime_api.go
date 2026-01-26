package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
	"github.com/spf13/cobra"
)

var closedMessages <-chan *models.Message = func() <-chan *models.Message {
	ch := make(chan *models.Message)
	close(ch)
	return ch
}()

const (
	capabilityChannelPrefix = "channel:"
	capabilityToolPrefix    = "tool:"
	capabilityCLIPrefix     = "cli:"
	capabilityServicePrefix = "service:"
	capabilityHookPrefix    = "hook:"
)

type capabilityGate struct {
	pluginID string
	declared []string
}

func newCapabilityGate(pluginID string, manifest *pluginsdk.Manifest) *capabilityGate {
	if manifest == nil {
		return nil
	}
	declared := manifest.DeclaredCapabilities()
	if len(declared) == 0 {
		return nil
	}
	return &capabilityGate{pluginID: pluginID, declared: declared}
}

func (g *capabilityGate) require(capability string) error {
	if g == nil {
		return nil
	}
	capability = strings.TrimSpace(capability)
	if capability == "" {
		return fmt.Errorf("plugin %q missing capability for empty target", g.pluginID)
	}
	for _, allowed := range g.declared {
		if pluginsdk.CapabilityMatches(allowed, capability) {
			return nil
		}
	}
	return fmt.Errorf("plugin %q missing capability %q", g.pluginID, capability)
}

func channelCapability(channel models.ChannelType) string {
	return capabilityChannelPrefix + string(channel)
}

func toolCapability(name string) string {
	return capabilityToolPrefix + strings.TrimSpace(name)
}

func serviceCapability(id string) string {
	return capabilityServicePrefix + strings.TrimSpace(id)
}

func hookCapability(eventType string) string {
	return capabilityHookPrefix + strings.TrimSpace(eventType)
}

func validateCLICapabilities(gate *capabilityGate, paths []string) error {
	if gate == nil {
		return nil
	}
	for _, path := range paths {
		if err := gate.require(capabilityCLIPrefix + path); err != nil {
			return err
		}
	}
	return nil
}

type runtimeChannelRegistry struct {
	registry     *channels.Registry
	pluginID     string
	allowed      map[string]struct{}
	capabilities *capabilityGate
}

func (r *runtimeChannelRegistry) RegisterChannel(adapter pluginsdk.ChannelAdapter) error {
	if r.registry == nil {
		return fmt.Errorf("channel registry is nil")
	}
	if adapter == nil {
		return fmt.Errorf("plugin adapter is nil")
	}
	channelID := string(adapter.Type())
	if len(r.allowed) > 0 {
		if _, ok := r.allowed[channelID]; !ok {
			return fmt.Errorf("plugin %q attempted to register undeclared channel %q", r.pluginID, channelID)
		}
	}
	if err := r.capabilities.require(channelCapability(adapter.Type())); err != nil {
		return err
	}
	r.registry.Register(pluginAdapterWrapper{adapter: adapter})
	return nil
}

type runtimeToolRegistry struct {
	runtime      *agent.Runtime
	pluginID     string
	allowed      map[string]struct{}
	capabilities *capabilityGate
}

func (r *runtimeToolRegistry) RegisterTool(def pluginsdk.ToolDefinition, handler pluginsdk.ToolHandler) error {
	if r.runtime == nil {
		return fmt.Errorf("runtime is nil")
	}
	if handler == nil {
		return fmt.Errorf("tool handler is nil")
	}
	if def.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if len(r.allowed) > 0 {
		if _, ok := r.allowed[def.Name]; !ok {
			return fmt.Errorf("plugin %q attempted to register undeclared tool %q", r.pluginID, def.Name)
		}
	}
	if err := r.capabilities.require(toolCapability(def.Name)); err != nil {
		return err
	}
	tool := &pluginTool{
		definition: def,
		handler:    handler,
	}
	r.runtime.RegisterTool(tool)
	return nil
}

type pluginTool struct {
	definition pluginsdk.ToolDefinition
	handler    pluginsdk.ToolHandler
}

func (t *pluginTool) Name() string {
	return t.definition.Name
}

func (t *pluginTool) Description() string {
	return t.definition.Description
}

func (t *pluginTool) Schema() json.RawMessage {
	return t.definition.Schema
}

func (t *pluginTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	result, err := t.handler(ctx, params)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &agent.ToolResult{Content: ""}, nil
	}
	return &agent.ToolResult{
		Content: result.Content,
		IsError: result.IsError,
	}, nil
}

type pluginAdapterWrapper struct {
	adapter pluginsdk.ChannelAdapter
}

func (p pluginAdapterWrapper) Type() models.ChannelType {
	return p.adapter.Type()
}

func (p pluginAdapterWrapper) Start(ctx context.Context) error {
	if adapter, ok := p.adapter.(pluginsdk.LifecycleAdapter); ok {
		return adapter.Start(ctx)
	}
	return nil
}

func (p pluginAdapterWrapper) Stop(ctx context.Context) error {
	if adapter, ok := p.adapter.(pluginsdk.LifecycleAdapter); ok {
		return adapter.Stop(ctx)
	}
	return nil
}

func (p pluginAdapterWrapper) Send(ctx context.Context, msg *models.Message) error {
	if adapter, ok := p.adapter.(pluginsdk.OutboundAdapter); ok {
		return adapter.Send(ctx, msg)
	}
	return fmt.Errorf("outbound not supported")
}

func (p pluginAdapterWrapper) Messages() <-chan *models.Message {
	if adapter, ok := p.adapter.(pluginsdk.InboundAdapter); ok {
		return adapter.Messages()
	}
	return closedMessages
}

func (p pluginAdapterWrapper) Status() channels.Status {
	if adapter, ok := p.adapter.(pluginsdk.HealthAdapter); ok {
		return toChannelStatus(adapter.Status())
	}
	return channels.Status{}
}

func (p pluginAdapterWrapper) HealthCheck(ctx context.Context) channels.HealthStatus {
	if adapter, ok := p.adapter.(pluginsdk.HealthAdapter); ok {
		return toChannelHealth(adapter.HealthCheck(ctx))
	}
	return channels.HealthStatus{}
}

func (p pluginAdapterWrapper) Metrics() channels.MetricsSnapshot {
	return channels.MetricsSnapshot{ChannelType: p.adapter.Type()}
}

func toChannelStatus(status pluginsdk.Status) channels.Status {
	return channels.Status{
		Connected: status.Connected,
		Error:     status.Error,
		LastPing:  status.LastPing,
	}
}

func toChannelHealth(status pluginsdk.HealthStatus) channels.HealthStatus {
	return channels.HealthStatus{
		Healthy:   status.Healthy,
		Latency:   status.Latency,
		Message:   status.Message,
		LastCheck: status.LastCheck,
		Degraded:  status.Degraded,
	}
}

// =============================================================================
// CLI Registry
// =============================================================================

// runtimeCLIRegistry adapts a cobra root command for plugin CLI registration.
type runtimeCLIRegistry struct {
	rootCmd      *cobra.Command
	pluginID     string
	allowed      map[string]struct{}
	capabilities *capabilityGate
}

func (r *runtimeCLIRegistry) RegisterCommand(cmd *pluginsdk.CLICommand) error {
	if r.rootCmd == nil {
		return fmt.Errorf("CLI root command is nil")
	}
	if cmd == nil {
		return fmt.Errorf("CLI command is nil")
	}

	paths, err := cliCommandPaths("", cmd)
	if err != nil {
		return err
	}
	if err := validateCLICapabilities(r.capabilities, paths); err != nil {
		return err
	}
	if len(r.allowed) > 0 {
		for _, path := range paths {
			if _, ok := r.allowed[path]; !ok {
				return fmt.Errorf("plugin %q attempted to register undeclared CLI command %q", r.pluginID, path)
			}
		}
	}

	rootPath := paths[0]
	if existing := findCommand(r.rootCmd, rootPath); existing != nil {
		return fmt.Errorf("CLI command %q already exists", rootPath)
	}

	cobraCmd := convertCLICommand(cmd)
	r.rootCmd.AddCommand(cobraCmd)
	return nil
}

func (r *runtimeCLIRegistry) RegisterSubcommand(parent string, cmd *pluginsdk.CLICommand) error {
	if r.rootCmd == nil {
		return fmt.Errorf("CLI root command is nil")
	}
	if cmd == nil {
		return fmt.Errorf("CLI command is nil")
	}

	// Find parent command
	parentCmd := findCommand(r.rootCmd, parent)
	if parentCmd == nil {
		return fmt.Errorf("parent command %q not found", parent)
	}

	canonicalParent := strings.Join(splitCommandPath(parent), ".")
	paths, err := cliCommandPaths(canonicalParent, cmd)
	if err != nil {
		return err
	}
	if err := validateCLICapabilities(r.capabilities, paths); err != nil {
		return err
	}
	if len(r.allowed) > 0 {
		for _, path := range paths {
			if _, ok := r.allowed[path]; !ok {
				return fmt.Errorf("plugin %q attempted to register undeclared CLI command %q", r.pluginID, path)
			}
		}
	}

	rootPath := paths[0]
	if existing := findCommand(r.rootCmd, rootPath); existing != nil {
		return fmt.Errorf("CLI command %q already exists", rootPath)
	}

	cobraCmd := convertCLICommand(cmd)
	parentCmd.AddCommand(cobraCmd)
	return nil
}

// convertCLICommand converts a pluginsdk.CLICommand to a cobra.Command.
func convertCLICommand(cmd *pluginsdk.CLICommand) *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:     cmd.Use,
		Short:   cmd.Short,
		Long:    cmd.Long,
		Example: cmd.Example,
		Args:    cmd.Args,
	}

	if cmd.Run != nil {
		cobraCmd.RunE = cmd.Run
	}

	if cmd.Flags != nil {
		cmd.Flags(cobraCmd)
	}

	// Add subcommands recursively
	for _, sub := range cmd.Subcommands {
		cobraCmd.AddCommand(convertCLICommand(sub))
	}

	return cobraCmd
}

// findCommand finds a command by name path (e.g., "memory.search").
func findCommand(root *cobra.Command, path string) *cobra.Command {
	if path == "" {
		return root
	}

	parts := splitCommandPath(path)
	current := root

	for _, part := range parts {
		found := false
		for _, child := range current.Commands() {
			if child.Name() == part {
				current = child
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return current
}

func splitCommandPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '.' || c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func commandNameFromUse(use string) string {
	fields := strings.Fields(use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func cliCommandPaths(prefix string, cmd *pluginsdk.CLICommand) ([]string, error) {
	var paths []string
	seen := make(map[string]struct{})

	var walk func(prefix string, cmd *pluginsdk.CLICommand) error
	walk = func(prefix string, cmd *pluginsdk.CLICommand) error {
		if cmd == nil {
			return nil
		}
		name := commandNameFromUse(cmd.Use)
		if name == "" {
			return fmt.Errorf("command name is required")
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if _, ok := seen[path]; ok {
			return fmt.Errorf("duplicate CLI command %q", path)
		}
		seen[path] = struct{}{}
		paths = append(paths, path)

		for _, sub := range cmd.Subcommands {
			if err := walk(path, sub); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(prefix, cmd); err != nil {
		return nil, err
	}
	return paths, nil
}

// =============================================================================
// Service Registry
// =============================================================================

// ServiceManager manages plugin services lifecycle.
type ServiceManager struct {
	services []*pluginService
	logger   *slog.Logger
}

type pluginService struct {
	def      *pluginsdk.Service
	pluginID string
	running  bool
}

// NewServiceManager creates a new service manager.
func NewServiceManager(logger *slog.Logger) *ServiceManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceManager{
		logger: logger.With("component", "service-manager"),
	}
}

// StartAll starts all registered services.
func (m *ServiceManager) StartAll(ctx context.Context) error {
	for _, svc := range m.services {
		if svc.running {
			continue
		}
		if err := svc.def.Start(ctx); err != nil {
			m.logger.Error("failed to start service",
				"service_id", svc.def.ID,
				"plugin_id", svc.pluginID,
				"error", err)
			continue
		}
		svc.running = true
		m.logger.Info("started service", "service_id", svc.def.ID, "plugin_id", svc.pluginID)
	}
	return nil
}

// StopAll stops all running services.
func (m *ServiceManager) StopAll(ctx context.Context) error {
	for i := len(m.services) - 1; i >= 0; i-- {
		svc := m.services[i]
		if !svc.running {
			continue
		}
		if err := svc.def.Stop(ctx); err != nil {
			m.logger.Error("failed to stop service",
				"service_id", svc.def.ID,
				"plugin_id", svc.pluginID,
				"error", err)
			continue
		}
		svc.running = false
		m.logger.Info("stopped service", "service_id", svc.def.ID, "plugin_id", svc.pluginID)
	}
	return nil
}

// HealthCheck runs health checks for all services.
func (m *ServiceManager) HealthCheck(ctx context.Context) map[string]error {
	results := make(map[string]error)
	for _, svc := range m.services {
		if !svc.running || svc.def.HealthCheck == nil {
			continue
		}
		results[svc.def.ID] = svc.def.HealthCheck(ctx)
	}
	return results
}

// Services returns all registered services.
func (m *ServiceManager) Services() []*pluginsdk.Service {
	result := make([]*pluginsdk.Service, len(m.services))
	for i, svc := range m.services {
		result[i] = svc.def
	}
	return result
}

// runtimeServiceRegistry adapts the service manager for plugin registration.
type runtimeServiceRegistry struct {
	manager      *ServiceManager
	pluginID     string
	allowed      map[string]struct{}
	capabilities *capabilityGate
}

func (r *runtimeServiceRegistry) RegisterService(svc *pluginsdk.Service) error {
	if r.manager == nil {
		return fmt.Errorf("service manager is nil")
	}
	if svc == nil {
		return fmt.Errorf("service is nil")
	}
	if svc.ID == "" {
		return fmt.Errorf("service ID is required")
	}
	if len(r.allowed) > 0 {
		if _, ok := r.allowed[svc.ID]; !ok {
			return fmt.Errorf("plugin %q attempted to register undeclared service %q", r.pluginID, svc.ID)
		}
	}
	if err := r.capabilities.require(serviceCapability(svc.ID)); err != nil {
		return err
	}
	if svc.Start == nil {
		return fmt.Errorf("service Start function is required")
	}
	if svc.Stop == nil {
		return fmt.Errorf("service Stop function is required")
	}

	svcCopy := *svc
	r.manager.services = append(r.manager.services, &pluginService{
		def:      &svcCopy,
		pluginID: r.pluginID,
	})
	return nil
}

// =============================================================================
// Hook Registry Adapter
// =============================================================================

// runtimeHookRegistry adapts the internal hooks.Registry for plugin registration.
type runtimeHookRegistry struct {
	registry     *hooks.Registry
	pluginID     string
	allowed      map[string]struct{}
	capabilities *capabilityGate
}

func (r *runtimeHookRegistry) RegisterHook(reg *pluginsdk.HookRegistration) error {
	if r.registry == nil {
		return fmt.Errorf("hook registry is nil")
	}
	if reg == nil {
		return fmt.Errorf("hook registration is nil")
	}
	if reg.EventType == "" {
		return fmt.Errorf("event type is required")
	}
	if len(r.allowed) > 0 {
		if _, ok := r.allowed[reg.EventType]; !ok {
			return fmt.Errorf("plugin %q attempted to register undeclared hook %q", r.pluginID, reg.EventType)
		}
	}
	if err := r.capabilities.require(hookCapability(reg.EventType)); err != nil {
		return err
	}
	if err := r.capabilities.require(hookCapability(reg.EventType)); err != nil {
		return err
	}

	eventType := reg.EventType
	handler := reg.Handler
	name := reg.Name
	priority := reg.Priority

	// Convert plugin handler to internal handler
	internalHandler := func(ctx context.Context, event *hooks.Event) error {
		pluginEvent := &pluginsdk.HookEvent{
			Type:      string(event.Type),
			SessionID: event.SessionKey,
			ChannelID: event.ChannelID,
			Data:      event.Context,
		}
		return handler(ctx, pluginEvent)
	}

	// Build options
	opts := []hooks.RegisterOption{
		hooks.WithSource(r.pluginID),
	}
	if name != "" {
		opts = append(opts, hooks.WithName(name))
	}
	if priority != 0 {
		opts = append(opts, hooks.WithPriority(hooks.Priority(priority)))
	}

	r.registry.Register(eventType, internalHandler, opts...)
	return nil
}

// =============================================================================
// Plugin Logger Adapter
// =============================================================================

// pluginLoggerAdapter adapts slog.Logger for pluginsdk.PluginLogger interface.
type pluginLoggerAdapter struct {
	logger *slog.Logger
}

func (l *pluginLoggerAdapter) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

func (l *pluginLoggerAdapter) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

func (l *pluginLoggerAdapter) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

func (l *pluginLoggerAdapter) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}
