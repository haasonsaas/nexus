package pluginsdk

import (
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// ValidateConfig validates the plugin config against the manifest schema.
func (m *Manifest) ValidateConfig(config any) error {
	if err := m.Validate(); err != nil {
		return err
	}

	schema, err := jsonschema.CompileString("plugin.schema.json", string(m.ConfigSchema))
	if err != nil {
		return fmt.Errorf("compile plugin schema: %w", err)
	}

	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode plugin config: %w", err)
	}

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode plugin config: %w", err)
	}

	if err := schema.Validate(decoded); err != nil {
		return fmt.Errorf("plugin config invalid: %w", err)
	}

	return nil
}
