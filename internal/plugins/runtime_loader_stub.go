//go:build windows

package plugins

import (
	"fmt"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// ErrWindowsPluginsNotSupported indicates that dynamic plugin loading
// is not available on Windows.
var ErrWindowsPluginsNotSupported = fmt.Errorf(
	"dynamic plugin loading (.so files) is not supported on Windows. " +
		"To use plugins on Windows, either: " +
		"(1) compile plugins directly into the nexus binary using RegisterRuntimePlugin(), or " +
		"(2) use MCP servers which work on all platforms, or " +
		"(3) run nexus in WSL2 or a Linux container",
)

// loadRuntimePlugin attempts to load a plugin from the given path.
// On Windows, dynamic plugin loading (.so files) is not supported.
// Use RegisterRuntimePlugin() for in-process plugins instead.
func loadRuntimePlugin(path string) (pluginsdk.RuntimePlugin, error) {
	return nil, ErrWindowsPluginsNotSupported
}
