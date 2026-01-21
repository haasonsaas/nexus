package pluginsdk

import "testing"

func TestManifestValidateConfig(t *testing.T) {
	manifest := &Manifest{
		ID: "voice-call",
		ConfigSchema: []byte(`{
      "type": "object",
      "additionalProperties": false,
      "required": ["token"],
      "properties": {
        "token": { "type": "string" }
      }
    }`),
	}

	if err := manifest.ValidateConfig(map[string]any{"token": "abc"}); err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
	if err := manifest.ValidateConfig(map[string]any{}); err == nil {
		t.Fatalf("expected config validation error")
	}
}
