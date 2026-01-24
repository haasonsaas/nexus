package config

import (
	"encoding/json"
	"sync"

	"github.com/invopop/jsonschema"
)

var (
	schemaOnce sync.Once
	schemaJSON []byte
	schemaErr  error
)

// JSONSchema returns the JSON Schema for the Config struct.
func JSONSchema() ([]byte, error) {
	schemaOnce.Do(func() {
		r := &jsonschema.Reflector{
			FieldNameTag: "yaml",
		}
		schema := r.Reflect(&Config{})
		schemaJSON, schemaErr = json.MarshalIndent(schema, "", "  ")
	})
	return schemaJSON, schemaErr
}
