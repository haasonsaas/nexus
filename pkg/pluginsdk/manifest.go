package pluginsdk

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	ManifestFilename       = "nexus.plugin.json"
	LegacyManifestFilename = "clawdbot.plugin.json"
)

// Manifest describes a plugin and its configuration schema.
type Manifest struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind,omitempty"`
	Name         string          `json:"name,omitempty"`
	Description  string          `json:"description,omitempty"`
	Version      string          `json:"version,omitempty"`
	Tools        []string        `json:"tools,omitempty"`
	Channels     []string        `json:"channels,omitempty"`
	Providers    []string        `json:"providers,omitempty"`
	Commands     []string        `json:"commands,omitempty"`
	Services     []string        `json:"services,omitempty"`
	Hooks        []string        `json:"hooks,omitempty"`
	ConfigSchema json.RawMessage `json:"configSchema"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	UIHints      *UIHints        `json:"uiHints,omitempty"`
}

// UIHints provides configuration hints for better onboarding UX.
type UIHints struct {
	// ConfigFields maps config field paths to their UI hints.
	// Paths use dot notation (e.g., "telegram.bot_token").
	ConfigFields map[string]*FieldHint `json:"configFields,omitempty"`

	// SetupSteps provides ordered setup instructions.
	SetupSteps []*SetupStep `json:"setupSteps,omitempty"`

	// Requirements lists external requirements (APIs, accounts, etc.).
	Requirements []*Requirement `json:"requirements,omitempty"`

	// Links provides helpful URLs for setup and documentation.
	Links map[string]string `json:"links,omitempty"`
}

// FieldHint provides UI hints for a configuration field.
type FieldHint struct {
	// Label is a human-readable label for the field.
	Label string `json:"label,omitempty"`

	// Description explains what the field is for.
	Description string `json:"description,omitempty"`

	// Placeholder shows example input.
	Placeholder string `json:"placeholder,omitempty"`

	// HelpURL links to documentation for this field.
	HelpURL string `json:"helpUrl,omitempty"`

	// InputType specifies the UI input type.
	// Values: text, password, number, boolean, select, multiselect, textarea
	InputType string `json:"inputType,omitempty"`

	// Options for select/multiselect input types.
	Options []FieldOption `json:"options,omitempty"`

	// Required indicates if this field must be set.
	Required bool `json:"required,omitempty"`

	// Sensitive indicates the field contains secret data.
	Sensitive bool `json:"sensitive,omitempty"`

	// EnvVar suggests an environment variable to use.
	EnvVar string `json:"envVar,omitempty"`

	// Default is the default value if not specified.
	Default any `json:"default,omitempty"`

	// Validation provides validation hints.
	Validation *FieldValidation `json:"validation,omitempty"`
}

// FieldOption represents an option for select inputs.
type FieldOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// FieldValidation provides validation hints for a field.
type FieldValidation struct {
	// Pattern is a regex pattern for validation.
	Pattern string `json:"pattern,omitempty"`

	// MinLength is the minimum string length.
	MinLength int `json:"minLength,omitempty"`

	// MaxLength is the maximum string length.
	MaxLength int `json:"maxLength,omitempty"`

	// Min is the minimum numeric value.
	Min *float64 `json:"min,omitempty"`

	// Max is the maximum numeric value.
	Max *float64 `json:"max,omitempty"`
}

// SetupStep describes a step in the setup process.
type SetupStep struct {
	// Title is the step title.
	Title string `json:"title"`

	// Description explains what to do.
	Description string `json:"description"`

	// Commands lists commands to run (if any).
	Commands []string `json:"commands,omitempty"`

	// ConfigFields lists config fields relevant to this step.
	ConfigFields []string `json:"configFields,omitempty"`

	// URL links to external setup pages.
	URL string `json:"url,omitempty"`
}

// Requirement describes an external requirement.
type Requirement struct {
	// Name of the requirement (e.g., "Telegram Bot Token").
	Name string `json:"name"`

	// Description explains what's needed.
	Description string `json:"description"`

	// URL links to where to get it.
	URL string `json:"url,omitempty"`

	// Optional indicates if this is optional.
	Optional bool `json:"optional,omitempty"`
}

func DecodeManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &manifest, nil
}

func DecodeManifestFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return DecodeManifest(data)
}

func (m *Manifest) Validate() error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("manifest id is required")
	}
	if len(m.ConfigSchema) == 0 {
		return fmt.Errorf("manifest configSchema is required")
	}
	return nil
}

// GetFieldHint returns the UI hint for a config field path.
func (m *Manifest) GetFieldHint(path string) *FieldHint {
	if m == nil || m.UIHints == nil || m.UIHints.ConfigFields == nil {
		return nil
	}
	return m.UIHints.ConfigFields[path]
}

// GetSetupSteps returns the setup steps for this plugin.
func (m *Manifest) GetSetupSteps() []*SetupStep {
	if m == nil || m.UIHints == nil {
		return nil
	}
	return m.UIHints.SetupSteps
}

// GetRequirements returns the requirements for this plugin.
func (m *Manifest) GetRequirements() []*Requirement {
	if m == nil || m.UIHints == nil {
		return nil
	}
	return m.UIHints.Requirements
}

// GetRequiredFields returns all required config field paths.
func (m *Manifest) GetRequiredFields() []string {
	if m == nil || m.UIHints == nil || m.UIHints.ConfigFields == nil {
		return nil
	}
	var required []string
	for path, hint := range m.UIHints.ConfigFields {
		if hint != nil && hint.Required {
			required = append(required, path)
		}
	}
	return required
}

// GetSensitiveFields returns all sensitive config field paths.
func (m *Manifest) GetSensitiveFields() []string {
	if m == nil || m.UIHints == nil || m.UIHints.ConfigFields == nil {
		return nil
	}
	var sensitive []string
	for path, hint := range m.UIHints.ConfigFields {
		if hint != nil && hint.Sensitive {
			sensitive = append(sensitive, path)
		}
	}
	return sensitive
}
