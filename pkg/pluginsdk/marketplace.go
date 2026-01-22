// Package pluginsdk provides the plugin SDK for Nexus plugins.
package pluginsdk

import (
	"encoding/json"
	"time"
)

// MarketplaceManifest describes a plugin available in a marketplace registry.
type MarketplaceManifest struct {
	// ID is the unique plugin identifier (e.g., "org/plugin-name").
	ID string `json:"id" yaml:"id"`

	// Name is the human-readable plugin name.
	Name string `json:"name" yaml:"name"`

	// Description explains what the plugin does.
	Description string `json:"description" yaml:"description"`

	// Version is the current semantic version (e.g., "1.2.3").
	Version string `json:"version" yaml:"version"`

	// Author is the plugin author or organization.
	Author string `json:"author,omitempty" yaml:"author"`

	// Homepage is the plugin's documentation or source URL.
	Homepage string `json:"homepage,omitempty" yaml:"homepage"`

	// License is the SPDX license identifier (e.g., "MIT", "Apache-2.0").
	License string `json:"license,omitempty" yaml:"license"`

	// Keywords are searchable tags for the plugin.
	Keywords []string `json:"keywords,omitempty" yaml:"keywords"`

	// Categories classify the plugin (e.g., "channels", "tools", "integrations").
	Categories []string `json:"categories,omitempty" yaml:"categories"`

	// Requires defines runtime requirements.
	Requires *PluginRequirements `json:"requires,omitempty" yaml:"requires"`

	// Artifacts lists available download artifacts for different platforms.
	Artifacts []PluginArtifact `json:"artifacts" yaml:"artifacts"`

	// Signature is the Ed25519 signature of the manifest (base64-encoded).
	Signature string `json:"signature,omitempty" yaml:"signature"`

	// PublishedAt is when this version was published.
	PublishedAt time.Time `json:"publishedAt,omitempty" yaml:"publishedAt"`

	// Deprecated indicates this plugin is deprecated.
	Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated"`

	// DeprecationMessage explains why the plugin is deprecated.
	DeprecationMessage string `json:"deprecationMessage,omitempty" yaml:"deprecationMessage"`

	// ConfigSchema is the JSON Schema for plugin configuration.
	ConfigSchema json.RawMessage `json:"configSchema,omitempty" yaml:"configSchema"`

	// Metadata contains additional plugin metadata.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata"`
}

// PluginRequirements defines what a plugin needs to run.
type PluginRequirements struct {
	// NexusVersion is the minimum required Nexus version (semver).
	NexusVersion string `json:"nexusVersion,omitempty" yaml:"nexusVersion"`

	// Go is the minimum required Go version.
	Go string `json:"go,omitempty" yaml:"go"`

	// OS restricts to specific operating systems (darwin, linux, windows).
	OS []string `json:"os,omitempty" yaml:"os"`

	// Arch restricts to specific architectures (amd64, arm64).
	Arch []string `json:"arch,omitempty" yaml:"arch"`

	// Dependencies lists required plugins by ID.
	Dependencies []PluginDependency `json:"dependencies,omitempty" yaml:"dependencies"`
}

// PluginDependency specifies a required plugin.
type PluginDependency struct {
	// ID is the plugin ID.
	ID string `json:"id" yaml:"id"`

	// Version is the required version constraint (semver range).
	Version string `json:"version,omitempty" yaml:"version"`

	// Optional means the plugin works without this dependency.
	Optional bool `json:"optional,omitempty" yaml:"optional"`
}

// PluginArtifact describes a downloadable plugin binary.
type PluginArtifact struct {
	// OS is the target operating system (darwin, linux, windows).
	OS string `json:"os" yaml:"os"`

	// Arch is the target architecture (amd64, arm64).
	Arch string `json:"arch" yaml:"arch"`

	// URL is the download URL for the artifact.
	URL string `json:"url" yaml:"url"`

	// Checksum is the SHA256 hash of the artifact (hex-encoded).
	Checksum string `json:"checksum" yaml:"checksum"`

	// Signature is the Ed25519 signature of the artifact (base64-encoded).
	Signature string `json:"signature,omitempty" yaml:"signature"`

	// Size is the artifact size in bytes.
	Size int64 `json:"size,omitempty" yaml:"size"`

	// Format is the artifact format (so, tar.gz, zip).
	Format string `json:"format,omitempty" yaml:"format"`
}

// InstalledPlugin represents a locally installed plugin.
type InstalledPlugin struct {
	// ID is the plugin identifier.
	ID string `json:"id" yaml:"id"`

	// Version is the installed version.
	Version string `json:"version" yaml:"version"`

	// Path is the local installation path.
	Path string `json:"path" yaml:"path"`

	// BinaryPath is the path to the plugin binary (.so file).
	BinaryPath string `json:"binaryPath" yaml:"binaryPath"`

	// ManifestPath is the path to the plugin manifest.
	ManifestPath string `json:"manifestPath" yaml:"manifestPath"`

	// Checksum is the SHA256 hash of the installed binary.
	Checksum string `json:"checksum" yaml:"checksum"`

	// Verified indicates whether signature verification passed.
	Verified bool `json:"verified" yaml:"verified"`

	// InstalledAt is when the plugin was installed.
	InstalledAt time.Time `json:"installedAt" yaml:"installedAt"`

	// UpdatedAt is when the plugin was last updated.
	UpdatedAt time.Time `json:"updatedAt" yaml:"updatedAt"`

	// Source is where the plugin was installed from.
	Source string `json:"source" yaml:"source"`

	// AutoUpdate indicates if auto-updates are enabled.
	AutoUpdate bool `json:"autoUpdate" yaml:"autoUpdate"`

	// Enabled indicates if the plugin is enabled.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Config is the plugin-specific configuration.
	Config map[string]any `json:"config,omitempty" yaml:"config"`

	// Manifest is the cached marketplace manifest.
	Manifest *MarketplaceManifest `json:"manifest,omitempty" yaml:"manifest"`
}

// PluginSearchResult represents a search result from the marketplace.
type PluginSearchResult struct {
	// Plugin is the marketplace manifest.
	Plugin *MarketplaceManifest `json:"plugin"`

	// Score is the search relevance score (0-1).
	Score float64 `json:"score"`

	// Installed indicates if this plugin is already installed.
	Installed bool `json:"installed"`

	// InstalledVersion is the installed version if any.
	InstalledVersion string `json:"installedVersion,omitempty"`

	// UpdateAvailable indicates if an update is available.
	UpdateAvailable bool `json:"updateAvailable"`
}

// PluginIndex is the local index of installed plugins.
type PluginIndex struct {
	// Version is the index format version.
	Version string `json:"version"`

	// Plugins is the map of installed plugins by ID.
	Plugins map[string]*InstalledPlugin `json:"plugins"`

	// LastUpdated is when the index was last modified.
	LastUpdated time.Time `json:"lastUpdated"`

	// Registries are the configured registry URLs.
	Registries []string `json:"registries,omitempty"`
}

// NewPluginIndex creates an empty plugin index.
func NewPluginIndex() *PluginIndex {
	return &PluginIndex{
		Version:     "1",
		Plugins:     make(map[string]*InstalledPlugin),
		LastUpdated: time.Now(),
	}
}

// RegistryIndex is the index served by a plugin registry.
type RegistryIndex struct {
	// Version is the index format version.
	Version string `json:"version"`

	// Name is the registry name.
	Name string `json:"name"`

	// Description is the registry description.
	Description string `json:"description,omitempty"`

	// Plugins is the list of available plugins.
	Plugins []*MarketplaceManifest `json:"plugins"`

	// PublicKey is the Ed25519 public key for signature verification (base64).
	PublicKey string `json:"publicKey,omitempty"`

	// UpdatedAt is when the index was last updated.
	UpdatedAt time.Time `json:"updatedAt"`
}

// InstallOptions configures plugin installation.
type InstallOptions struct {
	// Version is the specific version to install (empty for latest).
	Version string

	// Force reinstalls even if already installed.
	Force bool

	// SkipVerify skips signature verification (not recommended).
	SkipVerify bool

	// AutoUpdate enables automatic updates.
	AutoUpdate bool

	// Config is the initial plugin configuration.
	Config map[string]any
}

// UpdateOptions configures plugin updates.
type UpdateOptions struct {
	// Version is the specific version to update to (empty for latest).
	Version string

	// Force updates even if no new version is available.
	Force bool

	// SkipVerify skips signature verification.
	SkipVerify bool
}
