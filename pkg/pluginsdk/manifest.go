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
	Channels     []string        `json:"channels,omitempty"`
	Providers    []string        `json:"providers,omitempty"`
	ConfigSchema json.RawMessage `json:"configSchema"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	UIHints      map[string]any  `json:"uiHints,omitempty"`
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
