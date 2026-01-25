package extensions

import (
	"sort"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/skills"
)

// Kind represents a unified extension type.
type Kind string

const (
	KindSkill  Kind = "skill"
	KindPlugin Kind = "plugin"
	KindMCP    Kind = "mcp"
)

// Extension describes a configured extension across systems.
type Extension struct {
	ID     string
	Name   string
	Kind   Kind
	Source string
	Status string
}

// List returns a unified list of configured extensions.
func List(cfg *config.Config, skillsMgr *skills.Manager) []Extension {
	var out []Extension

	// Skills
	if skillsMgr != nil {
		eligible := map[string]struct{}{}
		for _, skill := range skillsMgr.ListEligible() {
			eligible[skill.Name] = struct{}{}
		}
		for _, skill := range skillsMgr.ListAll() {
			status := "ineligible"
			if _, ok := eligible[skill.Name]; ok {
				status = "eligible"
			}
			out = append(out, Extension{
				ID:     skill.Name,
				Name:   skill.Name,
				Kind:   KindSkill,
				Source: string(skill.Source),
				Status: status,
			})
		}
	}

	// Plugins
	if cfg != nil {
		for id, entry := range cfg.Plugins.Entries {
			status := "disabled"
			if entry.Enabled {
				status = "enabled"
			}
			out = append(out, Extension{
				ID:     id,
				Name:   id,
				Kind:   KindPlugin,
				Source: strings.TrimSpace(entry.Path),
				Status: status,
			})
		}
	}

	// MCP servers
	if cfg != nil && cfg.MCP.Enabled {
		for _, server := range cfg.MCP.Servers {
			if server == nil {
				continue
			}
			status := "configured"
			if server.AutoStart {
				status = "auto_start"
			}
			name := server.Name
			if strings.TrimSpace(name) == "" {
				name = server.ID
			}
			out = append(out, Extension{
				ID:     server.ID,
				Name:   name,
				Kind:   KindMCP,
				Source: string(server.Transport),
				Status: status,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].ID < out[j].ID
		}
		return out[i].Kind < out[j].Kind
	})

	return out
}
