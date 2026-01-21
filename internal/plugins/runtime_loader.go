//go:build !windows

package plugins

import (
	"fmt"
	"plugin"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

const runtimePluginSymbol = "NexusPlugin"

func loadRuntimePlugin(path string) (pluginsdk.RuntimePlugin, error) {
	if path == "" {
		return nil, fmt.Errorf("plugin path is empty")
	}
	plug, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open plugin %s: %w", path, err)
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
