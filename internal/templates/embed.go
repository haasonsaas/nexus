package templates

import (
	"context"
	"embed"
	"io/fs"
)

//go:embed builtin/**/TEMPLATE.md
var builtinFS embed.FS

// BuiltinFS returns the embedded filesystem containing builtin templates.
func BuiltinFS() fs.FS {
	// Return the builtin subdirectory as the root
	sub, err := fs.Sub(builtinFS, "builtin")
	if err != nil {
		// This should never happen with a valid embed
		return builtinFS
	}
	return sub
}

// NewBuiltinSource creates a discovery source for builtin templates.
func NewBuiltinSource() *EmbeddedSource {
	return NewEmbeddedSource(BuiltinFS(), SourceBuiltin, PriorityBuiltin)
}

// AddBuiltinSource adds the builtin templates source to a registry.
func AddBuiltinSource(registry *Registry) {
	registry.AddSource(NewBuiltinSource())
}

// BuiltinTemplateNames returns the names of all builtin templates.
func BuiltinTemplateNames() []string {
	return []string{
		"customer-support",
		"code-review",
		"research-assistant",
	}
}

// IsBuiltinTemplate checks if a template name is a builtin template.
func IsBuiltinTemplate(name string) bool {
	for _, builtin := range BuiltinTemplateNames() {
		if builtin == name {
			return true
		}
	}
	return false
}

// LoadBuiltinTemplate loads a specific builtin template by name.
func LoadBuiltinTemplate(name string) (*AgentTemplate, error) {
	source := NewBuiltinSource()
	templates, err := source.Discover(context.TODO())
	if err != nil {
		return nil, err
	}

	for _, tmpl := range templates {
		if tmpl.Name == name {
			return tmpl, nil
		}
	}

	return nil, nil
}

// NewRegistryWithBuiltins creates a registry pre-populated with builtin templates.
func NewRegistryWithBuiltins(cfg *TemplatesConfig, workspacePath string) (*Registry, error) {
	registry, err := NewRegistry(cfg, workspacePath)
	if err != nil {
		return nil, err
	}

	// Add builtin source with appropriate priority
	AddBuiltinSource(registry)

	return registry, nil
}
