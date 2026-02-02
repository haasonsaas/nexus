package plugins

import (
	"errors"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// ErrIsolationUnavailable indicates the requested isolation backend cannot be used.
var ErrIsolationUnavailable = errors.New("plugin isolation backend unavailable")

// ErrIsolationUnsupported indicates the backend cannot support the plugin capabilities.
var ErrIsolationUnsupported = errors.New("plugin isolation backend unsupported for plugin")

type isolationRuntimePluginLoader struct {
	backend string
	err     error
}

func newIsolationRuntimePluginLoader(cfg config.PluginIsolationConfig) runtimePluginLoader {
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		return isolationRuntimePluginLoader{
			err: fmt.Errorf("%w: backend not configured", ErrIsolationUnavailable),
		}
	}
	switch backend {
	case "daytona":
		return newDaytonaRuntimePluginLoader(cfg)
	case "docker", "firecracker":
		return isolationRuntimePluginLoader{
			backend: backend,
			err:     fmt.Errorf("%w: backend %q not implemented", ErrIsolationUnavailable, backend),
		}
	default:
		return isolationRuntimePluginLoader{
			backend: backend,
			err:     fmt.Errorf("%w: unknown backend %q", ErrIsolationUnavailable, cfg.Backend),
		}
	}
}

func (l isolationRuntimePluginLoader) Load(pluginID string, path string) (pluginsdk.RuntimePlugin, error) {
	if l.err != nil {
		return nil, l.err
	}
	return nil, fmt.Errorf("%w: backend %q not implemented", ErrIsolationUnavailable, l.backend)
}

func isIsolationUnavailable(err error) bool {
	return errors.Is(err, ErrIsolationUnavailable) || errors.Is(err, ErrIsolationUnsupported)
}
