package plugins

import "github.com/haasonsaas/nexus/internal/config"

func init() {
	config.RegisterPluginValidator(ValidationIssues)
}
