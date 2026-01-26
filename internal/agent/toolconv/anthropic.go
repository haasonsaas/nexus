package toolconv

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/haasonsaas/nexus/internal/agent"
)

// ToAnthropicTools converts internal tools to Anthropic tool definitions.
func ToAnthropicTools(tools []agent.Tool) ([]anthropic.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		param, err := ToAnthropicTool(tool)
		if err != nil {
			return nil, err
		}
		result = append(result, param)
	}
	return result, nil
}

// ToAnthropicTool converts a single tool to Anthropic tool definition.
func ToAnthropicTool(tool agent.Tool) (anthropic.ToolUnionParam, error) {
	var schema anthropic.ToolInputSchemaParam
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		return anthropic.ToolUnionParam{}, fmt.Errorf("invalid tool schema for %s: %w", tool.Name(), err)
	}

	toolParam := anthropic.ToolUnionParamOfTool(schema, tool.Name())
	if toolParam.OfTool == nil {
		return anthropic.ToolUnionParam{}, fmt.Errorf("invalid tool schema for %s: missing tool definition", tool.Name())
	}
	toolParam.OfTool.Description = anthropic.String(tool.Description())
	return toolParam, nil
}

// ToAnthropicBetaTools converts internal tools to Anthropic beta tool definitions.
func ToAnthropicBetaTools(tools []agent.Tool) ([]anthropic.BetaToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]anthropic.BetaToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		param, err := ToAnthropicBetaTool(tool)
		if err != nil {
			return nil, err
		}
		result = append(result, param)
	}
	return result, nil
}

// ToAnthropicBetaTool converts a single tool to Anthropic beta tool definition.
func ToAnthropicBetaTool(tool agent.Tool) (anthropic.BetaToolUnionParam, error) {
	var schema anthropic.BetaToolInputSchemaParam
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		return anthropic.BetaToolUnionParam{}, fmt.Errorf("invalid tool schema for %s: %w", tool.Name(), err)
	}

	toolParam := anthropic.BetaToolUnionParamOfTool(schema, tool.Name())
	if toolParam.OfTool == nil {
		return anthropic.BetaToolUnionParam{}, fmt.Errorf("invalid tool schema for %s: missing tool definition", tool.Name())
	}
	toolParam.OfTool.Description = anthropic.String(tool.Description())
	return toolParam, nil
}
