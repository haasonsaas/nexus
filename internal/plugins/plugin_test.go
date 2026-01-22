package plugins

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		ID:          "test-plugin",
		Name:        "Test Plugin",
		Description: "A test plugin",
		Version:     "1.0.0",
	}

	err := registry.Register(def)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Registering again should fail
	err = registry.Register(def)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistry_RegisterEmptyID(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		Name: "No ID Plugin",
	}

	err := registry.Register(def)
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestRegistry_LoadBasic(t *testing.T) {
	registry := NewRegistry(nil)

	var registerCalled bool
	def := &PluginDefinition{
		ID:          "test-plugin",
		Name:        "Test Plugin",
		Description: "A test plugin",
		Version:     "1.0.0",
		Register: func(api *PluginAPI) error {
			registerCalled = true
			return nil
		},
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	if err := registry.Load(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	if !registerCalled {
		t.Error("expected register function to be called")
	}

	plugins := registry.Plugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}

	if plugins[0].Status != PluginStatusLoaded {
		t.Errorf("expected status 'loaded', got %s", plugins[0].Status)
	}
}

func TestRegistry_LoadDisabled(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		ID:   "test-plugin",
		Name: "Test Plugin",
		Register: func(api *PluginAPI) error {
			t.Error("register should not be called when disabled")
			return nil
		},
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	// Disable plugins
	config := &PluginConfig{Enabled: false}
	if err := registry.Load(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	plugins := registry.Plugins()
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins when disabled, got %d", len(plugins))
	}
}

func TestRegistry_LoadDenylist(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		ID:   "blocked-plugin",
		Name: "Blocked Plugin",
		Register: func(api *PluginAPI) error {
			t.Error("register should not be called for denylisted plugin")
			return nil
		},
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	config := &PluginConfig{
		Enabled: true,
		Deny:    []string{"blocked-plugin"},
	}
	if err := registry.Load(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	plugins := registry.Plugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin record, got %d", len(plugins))
	}

	if plugins[0].Status != PluginStatusDisabled {
		t.Errorf("expected status 'disabled', got %s", plugins[0].Status)
	}
	if plugins[0].Error != "blocked by denylist" {
		t.Errorf("expected reason 'blocked by denylist', got %s", plugins[0].Error)
	}
}

func TestRegistry_LoadAllowlist(t *testing.T) {
	registry := NewRegistry(nil)

	def1 := &PluginDefinition{
		ID:   "allowed-plugin",
		Name: "Allowed Plugin",
	}
	def2 := &PluginDefinition{
		ID:   "not-allowed-plugin",
		Name: "Not Allowed Plugin",
	}

	registry.Register(def1)
	registry.Register(def2)

	config := &PluginConfig{
		Enabled: true,
		Allow:   []string{"allowed-plugin"},
	}
	if err := registry.Load(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	plugins := registry.Plugins()
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugin records, got %d", len(plugins))
	}

	var allowedFound, notAllowedFound bool
	for _, p := range plugins {
		if p.ID == "allowed-plugin" {
			allowedFound = true
			if p.Status != PluginStatusLoaded {
				t.Errorf("allowed plugin should be loaded, got %s", p.Status)
			}
		}
		if p.ID == "not-allowed-plugin" {
			notAllowedFound = true
			if p.Status != PluginStatusDisabled {
				t.Errorf("not-allowed plugin should be disabled, got %s", p.Status)
			}
		}
	}

	if !allowedFound || !notAllowedFound {
		t.Error("expected both plugins to be in records")
	}
}

func TestRegistry_LoadPerPluginDisabled(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		ID:   "test-plugin",
		Name: "Test Plugin",
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	disabled := false
	config := &PluginConfig{
		Enabled: true,
		Entries: map[string]PluginEntryConfig{
			"test-plugin": {Enabled: &disabled},
		},
	}
	if err := registry.Load(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	plugin, ok := registry.Plugin("test-plugin")
	if !ok {
		t.Fatal("plugin not found")
	}

	if plugin.Status != PluginStatusDisabled {
		t.Errorf("expected status 'disabled', got %s", plugin.Status)
	}
}

func TestRegistry_LoadRegisterError(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		ID:   "failing-plugin",
		Name: "Failing Plugin",
		Register: func(api *PluginAPI) error {
			return errors.New("registration failed")
		},
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	if err := registry.Load(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	plugin, ok := registry.Plugin("failing-plugin")
	if !ok {
		t.Fatal("plugin not found")
	}

	if plugin.Status != PluginStatusError {
		t.Errorf("expected status 'error', got %s", plugin.Status)
	}
	if plugin.Error != "registration failed" {
		t.Errorf("expected error 'registration failed', got %s", plugin.Error)
	}

	diagnostics := registry.Diagnostics()
	if len(diagnostics) == 0 {
		t.Error("expected diagnostic for error")
	}
}

func TestRegistry_RegisterCapabilities(t *testing.T) {
	registry := NewRegistry(nil)

	def := &PluginDefinition{
		ID:   "capability-plugin",
		Name: "Capability Plugin",
		Register: func(api *PluginAPI) error {
			api.RegisterTool("test-tool", "tool-handler")
			api.RegisterChannel("test-channel", "channel-handler")
			api.RegisterProvider("test-provider", "provider-handler")
			api.RegisterGatewayMethod("test-method", "method-handler")
			api.RegisterCommand("test-command", "command-handler")
			api.RegisterService("test-service", "service-handler")
			api.RegisterHTTPHandler("/test", "http-handler")
			return nil
		},
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	if err := registry.Load(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Check plugin record
	plugin, _ := registry.Plugin("capability-plugin")
	if len(plugin.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(plugin.Tools))
	}
	if len(plugin.Channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(plugin.Channels))
	}
	if plugin.HTTPHandlers != 1 {
		t.Errorf("expected 1 HTTP handler, got %d", plugin.HTTPHandlers)
	}

	// Check registry lookups
	if _, ok := registry.Tool("test-tool"); !ok {
		t.Error("tool not found in registry")
	}
	if _, ok := registry.Channel("test-channel"); !ok {
		t.Error("channel not found in registry")
	}
	if _, ok := registry.Provider("test-provider"); !ok {
		t.Error("provider not found in registry")
	}
	if _, ok := registry.GatewayMethod("test-method"); !ok {
		t.Error("gateway method not found in registry")
	}
	if _, ok := registry.Command("test-command"); !ok {
		t.Error("command not found in registry")
	}
	if _, ok := registry.Service("test-service"); !ok {
		t.Error("service not found in registry")
	}

	// Check listings
	if len(registry.ToolNames()) != 1 {
		t.Error("expected 1 tool name")
	}
	if len(registry.ChannelIDs()) != 1 {
		t.Error("expected 1 channel ID")
	}
	if len(registry.ProviderIDs()) != 1 {
		t.Error("expected 1 provider ID")
	}
}

func TestRegistry_PluginConfig(t *testing.T) {
	registry := NewRegistry(nil)

	var receivedConfig map[string]any
	def := &PluginDefinition{
		ID:   "configurable-plugin",
		Name: "Configurable Plugin",
		Register: func(api *PluginAPI) error {
			receivedConfig = api.PluginConfig
			return nil
		},
	}

	if err := registry.Register(def); err != nil {
		t.Fatal(err)
	}

	config := &PluginConfig{
		Enabled: true,
		Entries: map[string]PluginEntryConfig{
			"configurable-plugin": {
				Config: map[string]any{
					"setting1": "value1",
					"setting2": 42,
				},
			},
		},
	}

	if err := registry.Load(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	if receivedConfig == nil {
		t.Fatal("expected plugin config to be passed")
	}
	if receivedConfig["setting1"] != "value1" {
		t.Errorf("expected setting1 = 'value1', got %v", receivedConfig["setting1"])
	}
	if receivedConfig["setting2"] != 42 {
		t.Errorf("expected setting2 = 42, got %v", receivedConfig["setting2"])
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Reset default registry for test
	DefaultRegistry = NewRegistry(nil)

	def := &PluginDefinition{
		ID:   "default-test",
		Name: "Default Test",
	}

	if err := RegisterPlugin(def); err != nil {
		t.Fatal(err)
	}

	if err := LoadPlugins(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	plugins := DefaultRegistry.Plugins()
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(plugins))
	}
}
