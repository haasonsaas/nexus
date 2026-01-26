package toolconv

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/haasonsaas/nexus/internal/agent"
)

// ToBedrockTools converts internal tool definitions to Bedrock tool configuration.
func ToBedrockTools(tools []agent.Tool) *types.ToolConfiguration {
	bedrockTools := make([]types.Tool, len(tools))

	for i, tool := range tools {
		var schema any
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}

		bedrockTools[i] = &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        aws.String(tool.Name()),
				Description: aws.String(tool.Description()),
				InputSchema: &types.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
			},
		}
	}

	return &types.ToolConfiguration{Tools: bedrockTools}
}
