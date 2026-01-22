// Package templates provides agent templates for quickly creating new agents
// with predefined configurations, prompts, and tool setups.
package templates

import (
	"time"

	"github.com/haasonsaas/nexus/internal/multiagent"
	"github.com/haasonsaas/nexus/internal/tools/policy"
)

// AgentTemplate defines a reusable agent configuration template.
type AgentTemplate struct {
	// Name is the unique template identifier (lowercase, hyphens allowed).
	Name string `json:"name" yaml:"name"`

	// Version is the template version (semver format).
	Version string `json:"version,omitempty" yaml:"version"`

	// Description explains what this template is for and when to use it.
	Description string `json:"description" yaml:"description"`

	// Author is the template creator.
	Author string `json:"author,omitempty" yaml:"author"`

	// Homepage is an optional URL to template documentation.
	Homepage string `json:"homepage,omitempty" yaml:"homepage"`

	// Tags are keywords for template discovery.
	Tags []string `json:"tags,omitempty" yaml:"tags"`

	// Variables are the template parameters that can be customized.
	Variables []TemplateVariable `json:"variables,omitempty" yaml:"variables"`

	// Agent contains the agent definition template.
	Agent AgentTemplateSpec `json:"agent" yaml:"agent"`

	// Content is the markdown body with the system prompt template (lazy loaded).
	Content string `json:"-"`

	// Path is the directory path where the template was discovered.
	Path string `json:"path"`

	// Source indicates where the template was discovered from.
	Source SourceType `json:"source"`

	// SourcePriority is used for conflict resolution (higher wins).
	SourcePriority int `json:"-"`

	// CreatedAt is when the template was first created.
	CreatedAt *time.Time `json:"created_at,omitempty" yaml:"created_at"`

	// UpdatedAt is when the template was last modified.
	UpdatedAt *time.Time `json:"updated_at,omitempty" yaml:"updated_at"`
}

// AgentTemplateSpec contains the agent-specific template configuration.
type AgentTemplateSpec struct {
	// Model specifies the LLM model to use (optional, can use variable).
	Model string `json:"model,omitempty" yaml:"model"`

	// Provider specifies the LLM provider (optional, can use variable).
	Provider string `json:"provider,omitempty" yaml:"provider"`

	// Tools lists the tools this agent has access to.
	Tools []string `json:"tools,omitempty" yaml:"tools"`

	// ToolPolicy defines tool access rules for this agent.
	ToolPolicy *policy.Policy `json:"tool_policy,omitempty" yaml:"tool_policy"`

	// MCPServers lists MCP servers this agent should connect to.
	MCPServers []MCPServerRef `json:"mcp_servers,omitempty" yaml:"mcp_servers"`

	// HandoffRules defines when this agent should hand off to others.
	HandoffRules []multiagent.HandoffRule `json:"handoff_rules,omitempty" yaml:"handoff_rules"`

	// CanReceiveHandoffs indicates if other agents can hand off to this one.
	CanReceiveHandoffs bool `json:"can_receive_handoffs" yaml:"can_receive_handoffs"`

	// MaxIterations limits the agent's agentic loop iterations.
	MaxIterations int `json:"max_iterations,omitempty" yaml:"max_iterations"`

	// Temperature for LLM sampling (0.0-2.0).
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature"`

	// MaxTokens limits the response length.
	MaxTokens int `json:"max_tokens,omitempty" yaml:"max_tokens"`

	// Metadata contains additional agent configuration.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata"`
}

// TemplateVariable defines a customizable parameter in a template.
type TemplateVariable struct {
	// Name is the variable identifier used in templates (e.g., "company_name").
	Name string `json:"name" yaml:"name"`

	// Description explains what this variable is for.
	Description string `json:"description,omitempty" yaml:"description"`

	// Type is the variable type: string, number, boolean, array, object.
	Type VariableType `json:"type" yaml:"type"`

	// Default is the default value if not provided.
	Default any `json:"default,omitempty" yaml:"default"`

	// Required indicates if this variable must be provided.
	Required bool `json:"required,omitempty" yaml:"required"`

	// Validation contains optional validation rules.
	Validation *VariableValidation `json:"validation,omitempty" yaml:"validation"`

	// Options are allowed values for enum-style variables.
	Options []any `json:"options,omitempty" yaml:"options"`

	// Sensitive marks this variable as containing sensitive data.
	Sensitive bool `json:"sensitive,omitempty" yaml:"sensitive"`
}

// VariableType defines the type of a template variable.
type VariableType string

const (
	// VariableTypeString is a string value.
	VariableTypeString VariableType = "string"

	// VariableTypeNumber is a numeric value.
	VariableTypeNumber VariableType = "number"

	// VariableTypeBoolean is a boolean value.
	VariableTypeBoolean VariableType = "boolean"

	// VariableTypeArray is an array value.
	VariableTypeArray VariableType = "array"

	// VariableTypeObject is an object/map value.
	VariableTypeObject VariableType = "object"
)

// VariableValidation contains validation rules for a template variable.
type VariableValidation struct {
	// Pattern is a regex pattern for string validation.
	Pattern string `json:"pattern,omitempty" yaml:"pattern"`

	// MinLength is the minimum string length.
	MinLength *int `json:"min_length,omitempty" yaml:"min_length"`

	// MaxLength is the maximum string length.
	MaxLength *int `json:"max_length,omitempty" yaml:"max_length"`

	// Min is the minimum numeric value.
	Min *float64 `json:"min,omitempty" yaml:"min"`

	// Max is the maximum numeric value.
	Max *float64 `json:"max,omitempty" yaml:"max"`

	// MinItems is the minimum array length.
	MinItems *int `json:"min_items,omitempty" yaml:"min_items"`

	// MaxItems is the maximum array length.
	MaxItems *int `json:"max_items,omitempty" yaml:"max_items"`
}

// MCPServerRef references an MCP server configuration.
type MCPServerRef struct {
	// Name is the unique identifier for this server reference.
	Name string `json:"name" yaml:"name"`

	// Command is the command to start the MCP server.
	Command string `json:"command,omitempty" yaml:"command"`

	// Args are the command arguments.
	Args []string `json:"args,omitempty" yaml:"args"`

	// Env are environment variables for the server.
	Env map[string]string `json:"env,omitempty" yaml:"env"`

	// URL is the HTTP URL for remote MCP servers.
	URL string `json:"url,omitempty" yaml:"url"`

	// Required indicates if this server must be available.
	Required bool `json:"required,omitempty" yaml:"required"`

	// Tools lists specific tools to use from this server (empty = all).
	Tools []string `json:"tools,omitempty" yaml:"tools"`
}

// SourceType indicates where a template was discovered from.
type SourceType string

const (
	// SourceBuiltin means shipped with nexus binary.
	SourceBuiltin SourceType = "builtin"

	// SourceLocal means from ~/.nexus/templates/.
	SourceLocal SourceType = "local"

	// SourceWorkspace means from <workspace>/templates/.
	SourceWorkspace SourceType = "workspace"

	// SourceExtra means from templates.load.extraDirs.
	SourceExtra SourceType = "extra"

	// SourceGit means from a Git repository.
	SourceGit SourceType = "git"

	// SourceRegistry means from an HTTP registry.
	SourceRegistry SourceType = "registry"
)

// TemplateSnapshot is a lightweight representation for listing.
type TemplateSnapshot struct {
	Name        string     `json:"name"`
	Version     string     `json:"version,omitempty"`
	Description string     `json:"description"`
	Tags        []string   `json:"tags,omitempty"`
	Source      SourceType `json:"source"`
	Path        string     `json:"path"`
}

// SourceConfig configures a template discovery source.
type SourceConfig struct {
	// Type is the source type: local, git, registry.
	Type SourceType `json:"type" yaml:"type"`

	// Path is the directory path for local sources.
	Path string `json:"path,omitempty" yaml:"path"`

	// URL is the repository/registry URL for git/registry sources.
	URL string `json:"url,omitempty" yaml:"url"`

	// Branch is the git branch to use.
	Branch string `json:"branch,omitempty" yaml:"branch"`

	// SubPath is a subdirectory within a git repository.
	SubPath string `json:"subPath,omitempty" yaml:"subPath"`

	// Refresh is the auto-pull interval for git sources.
	Refresh time.Duration `json:"refresh,omitempty" yaml:"refresh"`

	// Auth is the authentication token for registry sources.
	Auth string `json:"auth,omitempty" yaml:"auth"`
}

// LoadConfig configures template loading behavior.
type LoadConfig struct {
	// ExtraDirs are additional directories to scan for templates.
	ExtraDirs []string `json:"extraDirs,omitempty" yaml:"extraDirs"`

	// Watch enables file watching for template changes.
	Watch bool `json:"watch,omitempty" yaml:"watch"`

	// WatchDebounceMs is the debounce delay for the watcher.
	WatchDebounceMs int `json:"watchDebounceMs,omitempty" yaml:"watchDebounceMs"`
}

// TemplateConfig provides per-template configuration overrides.
type TemplateConfig struct {
	// Enabled controls whether the template is available.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled"`

	// Variables provides default variable overrides.
	Variables map[string]any `json:"variables,omitempty" yaml:"variables"`
}

// TemplatesConfig is the top-level templates configuration.
type TemplatesConfig struct {
	// Sources are additional discovery sources beyond defaults.
	Sources []SourceConfig `json:"sources,omitempty" yaml:"sources"`

	// Load configures loading behavior.
	Load *LoadConfig `json:"load,omitempty" yaml:"load"`

	// Entries provides per-template configuration.
	Entries map[string]*TemplateConfig `json:"entries,omitempty" yaml:"entries"`
}

// ConfigKey returns the configuration key for this template.
func (t *AgentTemplate) ConfigKey() string {
	return t.Name
}

// IsEnabled checks if the template is enabled based on config overrides.
func (t *AgentTemplate) IsEnabled(overrides map[string]*TemplateConfig) bool {
	cfg, ok := overrides[t.ConfigKey()]
	if !ok || cfg.Enabled == nil {
		return true // Enabled by default
	}
	return *cfg.Enabled
}

// ToSnapshot creates a lightweight snapshot for listing.
func (t *AgentTemplate) ToSnapshot() *TemplateSnapshot {
	return &TemplateSnapshot{
		Name:        t.Name,
		Version:     t.Version,
		Description: t.Description,
		Tags:        t.Tags,
		Source:      t.Source,
		Path:        t.Path,
	}
}

// HasVariable checks if the template has a variable with the given name.
func (t *AgentTemplate) HasVariable(name string) bool {
	for _, v := range t.Variables {
		if v.Name == name {
			return true
		}
	}
	return false
}

// GetVariable returns a variable by name.
func (t *AgentTemplate) GetVariable(name string) *TemplateVariable {
	for i := range t.Variables {
		if t.Variables[i].Name == name {
			return &t.Variables[i]
		}
	}
	return nil
}

// GetRequiredVariables returns all required variables without defaults.
func (t *AgentTemplate) GetRequiredVariables() []TemplateVariable {
	var required []TemplateVariable
	for _, v := range t.Variables {
		if v.Required && v.Default == nil {
			required = append(required, v)
		}
	}
	return required
}

// InstantiationRequest contains parameters for creating an agent from a template.
type InstantiationRequest struct {
	// TemplateNme is the name of the template to instantiate.
	TemplateName string `json:"template_name"`

	// AgentID is the unique identifier for the new agent.
	AgentID string `json:"agent_id"`

	// AgentName is the human-readable name for the new agent.
	AgentName string `json:"agent_name,omitempty"`

	// Variables contains the variable values to use.
	Variables map[string]any `json:"variables,omitempty"`

	// Overrides allows overriding template defaults.
	Overrides *AgentTemplateSpec `json:"overrides,omitempty"`
}

// InstantiationResult contains the result of template instantiation.
type InstantiationResult struct {
	// Agent is the created agent definition.
	Agent *multiagent.AgentDefinition `json:"agent"`

	// Template is the source template.
	Template *AgentTemplate `json:"template"`

	// UsedVariables contains the final variable values used.
	UsedVariables map[string]any `json:"used_variables"`

	// Warnings contains non-fatal issues encountered.
	Warnings []string `json:"warnings,omitempty"`
}
