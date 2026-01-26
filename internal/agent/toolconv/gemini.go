package toolconv

import (
	"encoding/json"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"google.golang.org/genai"
)

// ToGeminiTools converts internal tool definitions to Gemini Tool format.
func ToGeminiTools(tools []agent.Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	declarations := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		var schemaMap map[string]any
		if err := json.Unmarshal(tool.Schema(), &schemaMap); err != nil {
			continue
		}

		schema := ToGeminiSchema(schemaMap)
		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  schema,
		})
	}

	if len(declarations) == 0 {
		return nil
	}

	return []*genai.Tool{
		{
			FunctionDeclarations: declarations,
		},
	}
}

// ToGeminiSchema converts a JSON Schema map to Gemini's Schema type.
func ToGeminiSchema(schemaMap map[string]any) *genai.Schema {
	if schemaMap == nil {
		return nil
	}

	schema := &genai.Schema{}

	if t, ok := schemaMap["type"].(string); ok {
		schema.Type = genai.Type(strings.ToUpper(t))
	}

	if desc, ok := schemaMap["description"].(string); ok {
		schema.Description = desc
	}

	if enum, ok := schemaMap["enum"].([]any); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				schema.Enum = append(schema.Enum, s)
			}
		}
	}

	if props, ok := schemaMap["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				schema.Properties[name] = ToGeminiSchema(propMap)
			}
		}
	}

	if required, ok := schemaMap["required"].([]any); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	if items, ok := schemaMap["items"].(map[string]any); ok {
		schema.Items = ToGeminiSchema(items)
	}

	return schema
}
