package pluginsdk

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// ValidateConfig validates the plugin config against the manifest schema.
func (m *Manifest) ValidateConfig(config any) error {
	if err := m.Validate(); err != nil {
		return err
	}

	schema, err := compileSchema(m.ConfigSchema)
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

var schemaCache sync.Map

func compileSchema(schema []byte) (*jsonschema.Schema, error) {
	key := string(schema)
	if cached, ok := schemaCache.Load(key); ok {
		if compiled, ok := cached.(*jsonschema.Schema); ok {
			return compiled, nil
		}
	}

	compiled, err := jsonschema.CompileString("plugin.schema.json", key)
	if err != nil {
		return nil, err
	}
	schemaCache.Store(key, compiled)
	return compiled, nil
}
