package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/haasonsaas/nexus/internal/plugins"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	switch cmd {
	case "list-tools":
		runListTools(os.Args[2:])
	case "exec-tool":
		runExecTool(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: nexus-plugin-runner <list-tools|exec-tool> [options]")
}

func runListTools(args []string) {
	flags := flag.NewFlagSet("list-tools", flag.ExitOnError)
	pluginPath := flags.String("plugin", "", "Path to plugin .so")
	configJSON := flags.String("config", "", "Plugin config JSON")
	configFile := flags.String("config-file", "", "Plugin config file path")
	_ = flags.Parse(args)

	cfg, err := loadConfig(*configJSON, *configFile)
	if err != nil {
		writeError(err)
	}

	plug, err := plugins.LoadRuntimePlugin(strings.TrimSpace(*pluginPath))
	if err != nil {
		writeError(err)
	}

	registry := newToolRegistry()
	if err := registerTools(plug, registry, cfg); err != nil {
		writeError(err)
	}

	resp := toolListResponse{Tools: registry.defs}
	writeJSON(resp)
}

func runExecTool(args []string) {
	flags := flag.NewFlagSet("exec-tool", flag.ExitOnError)
	pluginPath := flags.String("plugin", "", "Path to plugin .so")
	toolName := flags.String("tool", "", "Tool name")
	paramsJSON := flags.String("params", "", "Tool params JSON")
	paramsFile := flags.String("params-file", "", "Tool params file")
	configJSON := flags.String("config", "", "Plugin config JSON")
	configFile := flags.String("config-file", "", "Plugin config file path")
	_ = flags.Parse(args)

	if strings.TrimSpace(*toolName) == "" {
		writeError(fmt.Errorf("tool name is required"))
	}

	cfg, err := loadConfig(*configJSON, *configFile)
	if err != nil {
		writeError(err)
	}

	params, err := loadParams(*paramsJSON, *paramsFile)
	if err != nil {
		writeError(err)
	}

	plug, err := plugins.LoadRuntimePlugin(strings.TrimSpace(*pluginPath))
	if err != nil {
		writeError(err)
	}

	registry := newToolRegistry()
	if err := registerTools(plug, registry, cfg); err != nil {
		writeError(err)
	}

	handler, ok := registry.handlers[*toolName]
	if !ok {
		writeError(fmt.Errorf("tool %q not registered", *toolName))
	}

	result, err := handler(context.Background(), params)
	if err != nil {
		writeError(err)
	}
	writeJSON(toolExecResponse{Result: result})
}

func loadConfig(raw string, file string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	file = strings.TrimSpace(file)
	if raw == "" && file == "" {
		return map[string]any{}, nil
	}
	if raw != "" && file != "" {
		return nil, fmt.Errorf("config and config-file are mutually exclusive")
	}
	var data []byte
	if raw != "" {
		data = []byte(raw)
	} else {
		payload, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		data = payload
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

func loadParams(raw string, file string) (json.RawMessage, error) {
	raw = strings.TrimSpace(raw)
	file = strings.TrimSpace(file)
	if raw == "" && file == "" {
		return json.RawMessage([]byte("{}")), nil
	}
	if raw != "" && file != "" {
		return nil, fmt.Errorf("params and params-file are mutually exclusive")
	}
	var data []byte
	if raw != "" {
		data = []byte(raw)
	} else {
		payload, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		data = payload
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("params must be valid JSON")
	}
	return json.RawMessage(data), nil
}

func registerTools(plugin pluginsdk.RuntimePlugin, registry *toolRegistry, cfg map[string]any) error {
	if plugin == nil {
		return fmt.Errorf("plugin is nil")
	}
	switch p := plugin.(type) {
	case pluginsdk.FullPlugin:
		api := &pluginsdk.PluginAPI{
			Tools:       registry,
			Channels:    &unsupportedChannelRegistry{},
			CLI:         &unsupportedCLIRegistry{},
			Services:    &unsupportedServiceRegistry{},
			Hooks:       &unsupportedHookRegistry{},
			Config:      cfg,
			Logger:      stderrLogger{},
			ResolvePath: func(path string) string { return path },
		}
		return p.Register(api)
	default:
		return plugin.RegisterTools(registry, cfg)
	}
}

type toolRegistry struct {
	defs     []pluginsdk.ToolDefinition
	handlers map[string]pluginsdk.ToolHandler
}

func newToolRegistry() *toolRegistry {
	return &toolRegistry{
		defs:     make([]pluginsdk.ToolDefinition, 0),
		handlers: make(map[string]pluginsdk.ToolHandler),
	}
}

func (r *toolRegistry) RegisterTool(def pluginsdk.ToolDefinition, handler pluginsdk.ToolHandler) error {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return fmt.Errorf("tool name is required")
	}
	if handler == nil {
		return fmt.Errorf("tool handler is required")
	}
	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	def.Name = name
	r.defs = append(r.defs, def)
	r.handlers[name] = handler
	return nil
}

type stderrLogger struct{}

func (stderrLogger) Debug(msg string, args ...any) { fmt.Fprintln(os.Stderr, msg, args) }
func (stderrLogger) Info(msg string, args ...any)  { fmt.Fprintln(os.Stderr, msg, args) }
func (stderrLogger) Warn(msg string, args ...any)  { fmt.Fprintln(os.Stderr, msg, args) }
func (stderrLogger) Error(msg string, args ...any) { fmt.Fprintln(os.Stderr, msg, args) }

type unsupportedChannelRegistry struct{}

func (unsupportedChannelRegistry) RegisterChannel(adapter pluginsdk.ChannelAdapter) error {
	return fmt.Errorf("channel registration not supported in isolated runner")
}

type unsupportedCLIRegistry struct{}

func (unsupportedCLIRegistry) RegisterCommand(cmd *pluginsdk.CLICommand) error {
	return fmt.Errorf("cli registration not supported in isolated runner")
}

func (unsupportedCLIRegistry) RegisterSubcommand(parent string, cmd *pluginsdk.CLICommand) error {
	return fmt.Errorf("cli registration not supported in isolated runner")
}

type unsupportedServiceRegistry struct{}

func (unsupportedServiceRegistry) RegisterService(svc *pluginsdk.Service) error {
	return fmt.Errorf("service registration not supported in isolated runner")
}

type unsupportedHookRegistry struct{}

func (unsupportedHookRegistry) RegisterHook(reg *pluginsdk.HookRegistration) error {
	return fmt.Errorf("hook registration not supported in isolated runner")
}

type toolListResponse struct {
	Tools []pluginsdk.ToolDefinition `json:"tools"`
	Error string                     `json:"error,omitempty"`
}

type toolExecResponse struct {
	Result *pluginsdk.ToolResult `json:"result,omitempty"`
	Error  string                `json:"error,omitempty"`
}

func writeError(err error) {
	if err == nil {
		os.Exit(1)
	}
	writeJSON(toolExecResponse{Error: err.Error()})
	os.Exit(1)
}

func writeJSON(payload any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
