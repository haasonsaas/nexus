package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/mcp"
)

func (s *Server) registerMCPSamplingHandler() {
	if s == nil || s.mcpManager == nil || s.llmProvider == nil {
		return
	}
	provider := s.llmProvider
	defaultModel := s.defaultModel
	s.mcpManager.SetSamplingHandler(func(ctx context.Context, req *mcp.SamplingRequest) (*mcp.SamplingResponse, error) {
		model := selectSamplingModel(provider, defaultModel, req)
		system := strings.TrimSpace(req.SystemPrompt)

		messages := make([]agent.CompletionMessage, 0, len(req.Messages))
		for _, msg := range req.Messages {
			role := strings.ToLower(strings.TrimSpace(msg.Role))
			if msg.Content.Type != "" && msg.Content.Type != "text" {
				return nil, fmt.Errorf("unsupported sampling content type: %s", msg.Content.Type)
			}
			if role == "system" {
				if msg.Content.Text != "" {
					if system != "" {
						system += "\n"
					}
					system += msg.Content.Text
				}
				continue
			}
			messages = append(messages, agent.CompletionMessage{
				Role:    role,
				Content: msg.Content.Text,
			})
		}

		reqBody := &agent.CompletionRequest{
			Model:     model,
			System:    system,
			Messages:  messages,
			MaxTokens: req.MaxTokens,
		}
		completion, err := provider.Complete(ctx, reqBody)
		if err != nil {
			return nil, err
		}

		var output strings.Builder
		for chunk := range completion {
			if chunk.Error != nil {
				return nil, chunk.Error
			}
			if chunk.ToolCall != nil {
				return nil, fmt.Errorf("sampling does not support tool calls")
			}
			if chunk.Text != "" {
				output.WriteString(chunk.Text)
			}
		}

		return &mcp.SamplingResponse{
			Role: "assistant",
			Content: mcp.MessageContent{
				Type: "text",
				Text: output.String(),
			},
			Model: model,
		}, nil
	})
}

func selectSamplingModel(provider agent.LLMProvider, defaultModel string, req *mcp.SamplingRequest) string {
	if req != nil && strings.TrimSpace(req.Model) != "" {
		return strings.TrimSpace(req.Model)
	}
	if req != nil && req.ModelPrefs != nil {
		for _, hint := range req.ModelPrefs.Hints {
			name := strings.TrimSpace(hint.Name)
			if name == "" {
				continue
			}
			if providerSupportsModel(provider, name) {
				return name
			}
		}
	}
	if strings.TrimSpace(defaultModel) != "" {
		return defaultModel
	}
	models := provider.Models()
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func providerSupportsModel(provider agent.LLMProvider, model string) bool {
	if provider == nil || model == "" {
		return false
	}
	for _, entry := range provider.Models() {
		if entry.ID == model {
			return true
		}
	}
	return false
}
