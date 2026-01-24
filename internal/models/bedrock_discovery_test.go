package models

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrock/types"
)

type mockBedrockClient struct {
	models []types.FoundationModelSummary
	err    error
}

func (m *mockBedrockClient) ListFoundationModels(ctx context.Context, params *bedrock.ListFoundationModelsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &bedrock.ListFoundationModelsOutput{
		ModelSummaries: m.models,
	}, nil
}

func TestBedrockDiscovery_Discover(t *testing.T) {
	activeStatus := types.FoundationModelLifecycleStatusActive
	streamingTrue := true
	streamingFalse := false

	tests := []struct {
		name           string
		config         BedrockDiscoveryConfig
		mockModels     []types.FoundationModelSummary
		mockErr        error
		wantCount      int
		wantErr        bool
		wantModelIDs   []string
		providerFilter []string
	}{
		{
			name: "discovers active streaming models",
			config: BedrockDiscoveryConfig{
				Enabled: true,
				Region:  "us-east-1",
			},
			mockModels: []types.FoundationModelSummary{
				{
					ModelId:                    aws.String("anthropic.claude-3-sonnet"),
					ModelName:                  aws.String("Claude 3 Sonnet"),
					ProviderName:               aws.String("Anthropic"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					InputModalities:           []types.ModelModality{types.ModelModalityText, types.ModelModalityImage},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
				{
					ModelId:                    aws.String("amazon.titan-text-express"),
					ModelName:                  aws.String("Titan Text Express"),
					ProviderName:               aws.String("Amazon"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					InputModalities:           []types.ModelModality{types.ModelModalityText},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
			},
			wantCount:    2,
			wantModelIDs: []string{"anthropic.claude-3-sonnet", "amazon.titan-text-express"},
		},
		{
			name: "filters out non-streaming models",
			config: BedrockDiscoveryConfig{
				Enabled: true,
				Region:  "us-east-1",
			},
			mockModels: []types.FoundationModelSummary{
				{
					ModelId:                    aws.String("anthropic.claude-3-sonnet"),
					ModelName:                  aws.String("Claude 3 Sonnet"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
				{
					ModelId:                    aws.String("some.non-streaming-model"),
					ModelName:                  aws.String("Non Streaming"),
					ResponseStreamingSupported: &streamingFalse,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
			},
			wantCount:    1,
			wantModelIDs: []string{"anthropic.claude-3-sonnet"},
		},
		{
			name: "applies provider filter",
			config: BedrockDiscoveryConfig{
				Enabled:        true,
				Region:         "us-east-1",
				ProviderFilter: []string{"anthropic"},
			},
			mockModels: []types.FoundationModelSummary{
				{
					ModelId:                    aws.String("anthropic.claude-3-sonnet"),
					ProviderName:               aws.String("Anthropic"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
				{
					ModelId:                    aws.String("amazon.titan-text"),
					ProviderName:               aws.String("Amazon"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
			},
			wantCount:    1,
			wantModelIDs: []string{"anthropic.claude-3-sonnet"},
		},
		{
			name: "disabled returns nil",
			config: BedrockDiscoveryConfig{
				Enabled: false,
			},
			wantCount: 0,
		},
		{
			name: "handles API error",
			config: BedrockDiscoveryConfig{
				Enabled: true,
				Region:  "us-east-1",
			},
			mockErr: errors.New("API error"),
			wantErr: true,
		},
		{
			name: "filters models without text output",
			config: BedrockDiscoveryConfig{
				Enabled: true,
				Region:  "us-east-1",
			},
			mockModels: []types.FoundationModelSummary{
				{
					ModelId:                    aws.String("some.text-model"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityText},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
				{
					ModelId:                    aws.String("some.image-model"),
					ResponseStreamingSupported: &streamingTrue,
					OutputModalities:          []types.ModelModality{types.ModelModalityImage},
					ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
				},
			},
			wantCount:    1,
			wantModelIDs: []string{"some.text-model"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discovery := NewBedrockDiscovery(tt.config, slog.Default())

			mockClient := &mockBedrockClient{
				models: tt.mockModels,
				err:    tt.mockErr,
			}
			discovery.SetClientFactory(func(region string) BedrockClient {
				return mockClient
			})

			models, err := discovery.Discover(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(models) != tt.wantCount {
				t.Errorf("got %d models, want %d", len(models), tt.wantCount)
			}

			if tt.wantModelIDs != nil {
				gotIDs := make(map[string]bool)
				for _, m := range models {
					gotIDs[m.ID] = true
				}
				for _, wantID := range tt.wantModelIDs {
					if !gotIDs[wantID] {
						t.Errorf("missing expected model ID: %s", wantID)
					}
				}
			}
		})
	}
}

func TestBedrockDiscovery_Caching(t *testing.T) {
	activeStatus := types.FoundationModelLifecycleStatusActive
	streamingTrue := true

	callCount := 0
	mockClient := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:                    aws.String("test.model"),
				ResponseStreamingSupported: &streamingTrue,
				OutputModalities:          []types.ModelModality{types.ModelModalityText},
				ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
			},
		},
	}

	discovery := NewBedrockDiscovery(BedrockDiscoveryConfig{
		Enabled:         true,
		Region:          "us-east-1",
		RefreshInterval: 1 * time.Hour,
	}, slog.Default())

	discovery.SetClientFactory(func(region string) BedrockClient {
		callCount++
		return mockClient
	})

	// First call should fetch
	_, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("first discover failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call should use cache
	_, err = discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("second discover failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call (cached), got %d", callCount)
	}

	// Clear cache
	discovery.ClearCache()

	// Should fetch again
	_, err = discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("third discover failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls after cache clear, got %d", callCount)
	}
}

func TestBedrockDiscovery_RegisterWithCatalog(t *testing.T) {
	activeStatus := types.FoundationModelLifecycleStatusActive
	streamingTrue := true

	mockClient := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:                    aws.String("anthropic.claude-3-sonnet"),
				ModelName:                  aws.String("Claude 3 Sonnet"),
				ProviderName:               aws.String("Anthropic"),
				ResponseStreamingSupported: &streamingTrue,
				OutputModalities:          []types.ModelModality{types.ModelModalityText},
				InputModalities:           []types.ModelModality{types.ModelModalityText, types.ModelModalityImage},
				ModelLifecycle:            &types.FoundationModelLifecycle{Status: activeStatus},
			},
		},
	}

	discovery := NewBedrockDiscovery(BedrockDiscoveryConfig{
		Enabled: true,
		Region:  "us-east-1",
	}, slog.Default())

	discovery.SetClientFactory(func(region string) BedrockClient {
		return mockClient
	})

	catalog := NewCatalog()
	err := discovery.RegisterWithCatalog(context.Background(), catalog)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	model, found := catalog.Get("anthropic.claude-3-sonnet")
	if !found {
		t.Fatal("model not found in catalog")
	}

	if model.Provider != ProviderBedrock {
		t.Errorf("expected provider %s, got %s", ProviderBedrock, model.Provider)
	}

	if !model.HasCapability(CapVision) {
		t.Error("expected model to have vision capability")
	}

	if !model.HasCapability(CapStreaming) {
		t.Error("expected model to have streaming capability")
	}
}

func TestInferTier(t *testing.T) {
	tests := []struct {
		id   string
		name string
		want Tier
	}{
		{"anthropic.claude-opus", "Claude Opus", TierFlagship},
		{"anthropic.claude-haiku", "Claude Haiku", TierFast},
		{"amazon.titan-lite", "Titan Lite", TierFast},
		{"anthropic.claude-sonnet", "Claude Sonnet", TierStandard},
		{"model.with-instant", "Instant Model", TierMini},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := inferTier(tt.id, tt.name)
			if got != tt.want {
				t.Errorf("inferTier(%q, %q) = %v, want %v", tt.id, tt.name, got, tt.want)
			}
		})
	}
}

func TestNormalizeProviderFilter(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		want   []string
		length int
	}{
		{
			name:   "empty filter",
			input:  nil,
			want:   nil,
			length: 0,
		},
		{
			name:   "normalizes case",
			input:  []string{"Anthropic", "AMAZON"},
			length: 2,
		},
		{
			name:   "removes duplicates",
			input:  []string{"anthropic", "Anthropic", "ANTHROPIC"},
			length: 1,
		},
		{
			name:   "trims whitespace",
			input:  []string{" anthropic ", "  amazon  "},
			length: 2,
		},
		{
			name:   "removes empty entries",
			input:  []string{"anthropic", "", "  ", "amazon"},
			length: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeProviderFilter(tt.input)
			if len(got) != tt.length {
				t.Errorf("normalizeProviderFilter() length = %d, want %d", len(got), tt.length)
			}
		})
	}
}

func TestHasTextModality(t *testing.T) {
	tests := []struct {
		name       string
		modalities []types.ModelModality
		want       bool
	}{
		{
			name:       "has text",
			modalities: []types.ModelModality{types.ModelModalityText},
			want:       true,
		},
		{
			name:       "has text and image",
			modalities: []types.ModelModality{types.ModelModalityText, types.ModelModalityImage},
			want:       true,
		},
		{
			name:       "only image",
			modalities: []types.ModelModality{types.ModelModalityImage},
			want:       false,
		},
		{
			name:       "empty",
			modalities: nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasTextModality(tt.modalities)
			if got != tt.want {
				t.Errorf("hasTextModality() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractProviderName(t *testing.T) {
	tests := []struct {
		name    string
		summary types.FoundationModelSummary
		want    string
	}{
		{
			name: "from provider name field",
			summary: types.FoundationModelSummary{
				ModelId:      aws.String("anthropic.claude-3"),
				ProviderName: aws.String("Anthropic"),
			},
			want: "anthropic",
		},
		{
			name: "from model ID",
			summary: types.FoundationModelSummary{
				ModelId: aws.String("amazon.titan-text"),
			},
			want: "amazon",
		},
		{
			name: "empty provider",
			summary: types.FoundationModelSummary{
				ModelId: aws.String("unknown"),
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractProviderName(tt.summary)
			if got != tt.want {
				t.Errorf("extractProviderName() = %q, want %q", got, tt.want)
			}
		})
	}
}
