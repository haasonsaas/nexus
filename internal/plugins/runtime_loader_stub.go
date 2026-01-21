//go:build windows

package plugins

import (
	"fmt"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

func loadRuntimePlugin(path string) (pluginsdk.RuntimePlugin, error) {
	return nil, fmt.Errorf("runtime plugins are not supported on Windows")
}
