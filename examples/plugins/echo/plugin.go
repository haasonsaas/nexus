package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

type echoPlugin struct{}

func (p *echoPlugin) Manifest() *pluginsdk.Manifest {
	return &pluginsdk.Manifest{
		ID:          "sample-echo",
		Name:        "Sample Echo",
		Description: "Example external plugin that adds an echo tool",
		Version:     "0.1.0",
		ConfigSchema: json.RawMessage(`{
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "prefix": {"type": "string"}
      }
    }`),
	}
}

func (p *echoPlugin) RegisterChannels(registry pluginsdk.ChannelRegistry, cfg map[string]any) error {
	return nil
}

func (p *echoPlugin) RegisterTools(registry pluginsdk.ToolRegistry, cfg map[string]any) error {
	defaultPrefix := ""
	if v, ok := cfg["prefix"].(string); ok {
		defaultPrefix = v
	}

	return registry.RegisterTool(pluginsdk.ToolDefinition{
		Name:        "echo",
		Description: "Echo a message with an optional prefix",
		Schema: json.RawMessage(`{
      "type": "object",
      "additionalProperties": false,
      "required": ["message"],
      "properties": {
        "message": {"type": "string"},
        "prefix": {"type": "string"}
      }
    }`),
	}, func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
		var input struct {
			Message string `json:"message"`
			Prefix  string `json:"prefix"`
		}
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, err
		}
		prefix := input.Prefix
		if prefix == "" {
			prefix = defaultPrefix
		}
		return &pluginsdk.ToolResult{Content: fmt.Sprintf("%s%s", prefix, input.Message)}, nil
	})
}

// NexusPlugin is the symbol looked up by the Nexus runtime plugin loader.
var NexusPlugin pluginsdk.RuntimePlugin = &echoPlugin{}
