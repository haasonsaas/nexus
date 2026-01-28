package gateway

import (
	"log/slog"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestBuildProviderSupportsAdditionalProviders(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Providers: map[string]config.LLMProviderConfig{
				"openrouter": {
					APIKey:       "test-openrouter",
					DefaultModel: "openai/gpt-4o",
				},
				"azure": {
					APIKey:       "test-azure",
					BaseURL:      "https://example.openai.azure.com",
					DefaultModel: "gpt-4o-deployment",
				},
				"bedrock": {
					DefaultModel: "anthropic.claude-3-sonnet-20240229-v1:0",
				},
				"ollama": {
					BaseURL:      "http://localhost:11434",
					DefaultModel: "llama3",
				},
				"copilot-proxy": {
					BaseURL:      "http://localhost:3000/v1",
					DefaultModel: "gpt-5.2",
				},
			},
			Bedrock: config.BedrockConfig{
				Region: "us-east-1",
			},
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	providers := []string{"openrouter", "azure", "bedrock", "ollama", "copilot-proxy"}
	for _, providerID := range providers {
		if _, _, err := server.buildProvider(providerID); err != nil {
			t.Fatalf("buildProvider(%q) error = %v", providerID, err)
		}
	}
}
