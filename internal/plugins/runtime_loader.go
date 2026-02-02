//go:build !windows

package plugins

import (
	"fmt"
	"plugin"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

const runtimePluginSymbol = "NexusPlugin"

// LoadRuntimePlugin loads a runtime plugin from disk.
func LoadRuntimePlugin(path string) (pluginsdk.RuntimePlugin, error) {
	return loadRuntimePlugin(path)
}

func loadRuntimePlugin(path string) (pluginsdk.RuntimePlugin, error) {
	if path == "" {
		return nil, fmt.Errorf("plugin path is empty")
	}

	// Validate path to prevent traversal attacks (defense in depth)
	validatedPath, err := ValidatePluginPath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin path: %w", err)
	}

	plug, err := plugin.Open(validatedPath)
	if err != nil {
		return nil, fmt.Errorf("open plugin %s: %w", validatedPath, err)
	}
	symbol, err := plug.Lookup(runtimePluginSymbol)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", runtimePluginSymbol, err)
	}

	switch v := symbol.(type) {
	case pluginsdk.RuntimePlugin:
		return v, nil
	case *pluginsdk.RuntimePlugin:
		return *v, nil
	default:
		return nil, fmt.Errorf("plugin symbol %s does not implement RuntimePlugin", runtimePluginSymbol)
	}
}
