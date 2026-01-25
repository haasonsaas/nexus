package toolconv

import (
	"encoding/json"

	"github.com/haasonsaas/nexus/internal/agent"
	openai "github.com/sashabaranov/go-openai"
)

// ToOpenAITools converts internal tool definitions to OpenAI function schema.
func ToOpenAITools(tools []agent.Tool) []openai.Tool {
	result := make([]openai.Tool, len(tools))
	for i, tool := range tools {
		var schemaMap map[string]any
		if err := json.Unmarshal(tool.Schema(), &schemaMap); err != nil {
			schemaMap = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}

		result[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  schemaMap,
			},
		}
	}
	return result
}
