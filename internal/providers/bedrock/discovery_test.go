package bedrock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrock/types"
)

// mockBedrockClient implements BedrockClientAPI for testing.
type mockBedrockClient struct {
	models    []types.FoundationModelSummary
	err       error
	callCount atomic.Int32
	delay     time.Duration
}

func (m *mockBedrockClient) ListFoundationModels(ctx context.Context, params *bedrock.ListFoundationModelsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error) {
	m.callCount.Add(1)
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &bedrock.ListFoundationModelsOutput{
		ModelSummaries: m.models,
	}, nil
}

// setupMockClient installs a mock client factory and returns cleanup function.
func setupMockClient(mock *mockBedrockClient) func() {
	originalFactory := clientFactory
	clientFactory = func(cfg aws.Config) BedrockClientAPI {
		return mock
	}
	ClearCache()
	return func() {
		clientFactory = originalFactory
		ClearCache()
	}
}

func TestDiscoverModels_Basic(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:                    aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:                  aws.String("Claude 3 Sonnet"),
				ProviderName:               aws.String("Anthropic"),
				InputModalities:            []types.ModelModality{types.ModelModalityText, types.ModelModalityImage},
				OutputModalities:           []types.ModelModality{types.ModelModalityText},
				ResponseStreamingSupported: aws.Bool(true),
				ModelLifecycle:             &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			{
				ModelId:                    aws.String("meta.llama3-70b-instruct-v1:0"),
				ModelName:                  aws.String("Llama 3 70B"),
				ProviderName:               aws.String("Meta"),
				InputModalities:            []types.ModelModality{types.ModelModalityText},
				OutputModalities:           []types.ModelModality{types.ModelModalityText},
				ResponseStreamingSupported: aws.Bool(true),
				ModelLifecycle:             &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
		},
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx := context.Background()
	models, err := DiscoverModels(ctx, nil)
	if err != nil {
		t.Fatalf("DiscoverModels failed: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ID != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("expected claude model ID, got %s", models[0].ID)
	}
	if models[0].Provider != "Anthropic" {
		t.Errorf("expected Anthropic provider, got %s", models[0].Provider)
	}
	if !models[0].StreamingSupported {
		t.Error("expected streaming to be supported")
	}
	if len(models[0].Input) != 2 {
		t.Errorf("expected 2 input modalities, got %d", len(models[0].Input))
	}
}

func TestDiscoverModels_ProviderFilter(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:        aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:      aws.String("Claude 3 Sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			{
				ModelId:        aws.String("meta.llama3-70b-instruct-v1:0"),
				ModelName:      aws.String("Llama 3 70B"),
				ProviderName:   aws.String("Meta"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			{
				ModelId:        aws.String("amazon.titan-text-express-v1"),
				ModelName:      aws.String("Titan Text Express"),
				ProviderName:   aws.String("Amazon"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
		},
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx := context.Background()
	cfg := &DiscoveryConfig{
		ProviderFilter: []string{"anthropic", "meta"},
	}

	models, err := DiscoverModels(ctx, cfg)
	if err != nil {
		t.Fatalf("DiscoverModels failed: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("expected 2 models with filter, got %d", len(models))
	}

	for _, m := range models {
		if m.Provider != "Anthropic" && m.Provider != "Meta" {
			t.Errorf("unexpected provider: %s", m.Provider)
		}
	}
}

func TestDiscoverModels_Caching(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:        aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:      aws.String("Claude 3 Sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
		},
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx := context.Background()
	cfg := &DiscoveryConfig{
		RefreshInterval: time.Hour,
	}

	// First call
	_, err := DiscoverModels(ctx, cfg)
	if err != nil {
		t.Fatalf("First DiscoverModels failed: %v", err)
	}

	// Second call should use cache
	_, err = DiscoverModels(ctx, cfg)
	if err != nil {
		t.Fatalf("Second DiscoverModels failed: %v", err)
	}

	// Should only have called API once
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 API call (cached), got %d", mock.callCount.Load())
	}
}

func TestDiscoverModels_RequestDeduplication(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:        aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:      aws.String("Claude 3 Sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
		},
		delay: 100 * time.Millisecond,
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx := context.Background()
	cfg := &DiscoveryConfig{}

	// Launch multiple concurrent requests
	var wg sync.WaitGroup
	results := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := DiscoverModels(ctx, cfg)
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	// Check all succeeded
	for err := range results {
		if err != nil {
			t.Errorf("DiscoverModels failed: %v", err)
		}
	}

	// Should have only made one API call due to deduplication
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 API call (deduplicated), got %d", mock.callCount.Load())
	}
}

func TestDiscoverModels_LifecycleFilter(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:        aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:      aws.String("Claude 3 Sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			{
				ModelId:        aws.String("anthropic.claude-v1"),
				ModelName:      aws.String("Claude V1 (Legacy)"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusLegacy},
			},
		},
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx := context.Background()
	models, err := DiscoverModels(ctx, nil)
	if err != nil {
		t.Fatalf("DiscoverModels failed: %v", err)
	}

	// Should only return ACTIVE models
	if len(models) != 1 {
		t.Errorf("expected 1 active model, got %d", len(models))
	}
	if models[0].ID != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("expected active claude model, got %s", models[0].ID)
	}
}

func TestDiscoverModels_ContextCancellation(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:      aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:    aws.String("Claude 3 Sonnet"),
				ProviderName: aws.String("Anthropic"),
			},
		},
		delay: time.Second,
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := DiscoverModels(ctx, nil)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestShouldIncludeModel(t *testing.T) {
	tests := []struct {
		name     string
		summary  *types.FoundationModelSummary
		filter   []string
		expected bool
	}{
		{
			name:     "nil summary",
			summary:  nil,
			filter:   nil,
			expected: false,
		},
		{
			name: "active model no filter",
			summary: &types.FoundationModelSummary{
				ModelId:        aws.String("anthropic.claude-3-sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			filter:   nil,
			expected: true,
		},
		{
			name: "legacy model",
			summary: &types.FoundationModelSummary{
				ModelId:        aws.String("anthropic.claude-v1"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusLegacy},
			},
			filter:   nil,
			expected: false,
		},
		{
			name: "provider in filter",
			summary: &types.FoundationModelSummary{
				ModelId:        aws.String("anthropic.claude-3-sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			filter:   []string{"anthropic"},
			expected: true,
		},
		{
			name: "provider not in filter",
			summary: &types.FoundationModelSummary{
				ModelId:        aws.String("amazon.titan-text-express-v1"),
				ProviderName:   aws.String("Amazon"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			filter:   []string{"anthropic", "meta"},
			expected: false,
		},
		{
			name: "model ID prefix match",
			summary: &types.FoundationModelSummary{
				ModelId:        aws.String("meta.llama3-70b"),
				ProviderName:   aws.String("Meta"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
			filter:   []string{"meta"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldIncludeModel(tt.summary, tt.filter)
			if result != tt.expected {
				t.Errorf("shouldIncludeModel() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestToModelDefinition(t *testing.T) {
	defaults := DiscoveryConfig{
		DefaultContextWindow: 4096,
		DefaultMaxTokens:     4096,
	}

	tests := []struct {
		name            string
		summary         *types.FoundationModelSummary
		expectedContext int
		expectedMax     int
		expectedReason  bool
	}{
		{
			name: "claude 3 sonnet",
			summary: &types.FoundationModelSummary{
				ModelId:          aws.String("anthropic.claude-3-sonnet-20240229-v1:0"),
				ModelName:        aws.String("Claude 3 Sonnet"),
				ProviderName:     aws.String("Anthropic"),
				InputModalities:  []types.ModelModality{types.ModelModalityText, types.ModelModalityImage},
				OutputModalities: []types.ModelModality{types.ModelModalityText},
			},
			expectedContext: 200000,
			expectedMax:     4096,
			expectedReason:  false,
		},
		{
			name: "claude 3.5 sonnet (reasoning)",
			summary: &types.FoundationModelSummary{
				ModelId:      aws.String("anthropic.claude-3-5-sonnet-20241022-v2:0"),
				ModelName:    aws.String("Claude 3.5 Sonnet v2"),
				ProviderName: aws.String("Anthropic"),
			},
			expectedContext: 200000,
			expectedMax:     8192,
			expectedReason:  true,
		},
		{
			name: "llama 3",
			summary: &types.FoundationModelSummary{
				ModelId:      aws.String("meta.llama3-70b-instruct-v1:0"),
				ModelName:    aws.String("Llama 3 70B"),
				ProviderName: aws.String("Meta"),
			},
			expectedContext: 8192,
			expectedMax:     2048,
			expectedReason:  false,
		},
		{
			name: "mistral",
			summary: &types.FoundationModelSummary{
				ModelId:      aws.String("mistral.mixtral-8x7b-instruct-v0:1"),
				ModelName:    aws.String("Mixtral 8x7B"),
				ProviderName: aws.String("Mistral"),
			},
			expectedContext: 32768,
			expectedMax:     4096,
			expectedReason:  false,
		},
		{
			name: "cohere command-r",
			summary: &types.FoundationModelSummary{
				ModelId:      aws.String("cohere.command-r-plus-v1:0"),
				ModelName:    aws.String("Command R+"),
				ProviderName: aws.String("Cohere"),
			},
			expectedContext: 128000,
			expectedMax:     4096,
			expectedReason:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toModelDefinition(tt.summary, defaults)

			if result.ContextWindow != tt.expectedContext {
				t.Errorf("ContextWindow = %d, expected %d", result.ContextWindow, tt.expectedContext)
			}
			if result.MaxTokens != tt.expectedMax {
				t.Errorf("MaxTokens = %d, expected %d", result.MaxTokens, tt.expectedMax)
			}
			if result.Reasoning != tt.expectedReason {
				t.Errorf("Reasoning = %v, expected %v", result.Reasoning, tt.expectedReason)
			}
		})
	}
}

func TestIsReasoningModel(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", true},
		{"anthropic.claude-sonnet-4-20250514-v1:0", true},
		{"anthropic.claude-opus-4-20250514-v1:0", true},
		{"anthropic.claude-3-sonnet-20240229-v1:0", false},
		{"anthropic.claude-3-haiku-20240307-v1:0", false},
		{"meta.llama3-70b-instruct-v1:0", false},
		{"openai.o1-preview", true},
		{"openai.o3-mini", true},
		{"deepseek.deepseek-r1", true},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := isReasoningModel(tt.modelID)
			if result != tt.expected {
				t.Errorf("isReasoningModel(%s) = %v, expected %v", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestGetModelContextWindow(t *testing.T) {
	tests := []struct {
		modelID  string
		expected int
	}{
		{"anthropic.claude-3-sonnet-20240229-v1:0", 200000},
		{"anthropic.claude-v2:1", 200000},
		{"meta.llama3-70b-instruct-v1:0", 8192},
		{"meta.llama3-405b-instruct-v1:0", 128000},
		{"mistral.mixtral-8x7b-instruct-v0:1", 32768},
		{"cohere.command-r-plus-v1:0", 128000},
		{"amazon.titan-text-express-v1", 8192},
		{"unknown.model-v1", 4096}, // default
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := getModelContextWindow(tt.modelID, 4096)
			if result != tt.expected {
				t.Errorf("getModelContextWindow(%s) = %d, expected %d", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestClearCache(t *testing.T) {
	mock := &mockBedrockClient{
		models: []types.FoundationModelSummary{
			{
				ModelId:        aws.String("anthropic.claude-3-sonnet"),
				ModelName:      aws.String("Claude 3 Sonnet"),
				ProviderName:   aws.String("Anthropic"),
				ModelLifecycle: &types.FoundationModelLifecycle{Status: types.FoundationModelLifecycleStatusActive},
			},
		},
	}
	cleanup := setupMockClient(mock)
	defer cleanup()

	ctx := context.Background()

	// First call populates cache
	_, _ = DiscoverModels(ctx, nil)
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", mock.callCount.Load())
	}

	// Second call uses cache
	_, _ = DiscoverModels(ctx, nil)
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 call (cached), got %d", mock.callCount.Load())
	}

	// Clear cache
	ClearCache()

	// Third call should fetch again
	_, _ = DiscoverModels(ctx, nil)
	if mock.callCount.Load() != 2 {
		t.Errorf("expected 2 calls after cache clear, got %d", mock.callCount.Load())
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &DiscoveryConfig{}
	applyDefaults(cfg)

	if cfg.Region != "us-east-1" {
		t.Errorf("expected default region us-east-1, got %s", cfg.Region)
	}
	if cfg.RefreshInterval != time.Hour {
		t.Errorf("expected default refresh interval 1h, got %v", cfg.RefreshInterval)
	}
	if cfg.DefaultContextWindow != 4096 {
		t.Errorf("expected default context window 4096, got %d", cfg.DefaultContextWindow)
	}
	if cfg.DefaultMaxTokens != 4096 {
		t.Errorf("expected default max tokens 4096, got %d", cfg.DefaultMaxTokens)
	}

	// Custom values should not be overwritten
	cfg2 := &DiscoveryConfig{
		Region:               "us-west-2",
		RefreshInterval:      30 * time.Minute,
		DefaultContextWindow: 8192,
		DefaultMaxTokens:     2048,
	}
	applyDefaults(cfg2)

	if cfg2.Region != "us-west-2" {
		t.Errorf("custom region was overwritten: %s", cfg2.Region)
	}
	if cfg2.RefreshInterval != 30*time.Minute {
		t.Errorf("custom refresh interval was overwritten: %v", cfg2.RefreshInterval)
	}
}

func TestFilterByProvider(t *testing.T) {
	models := []ModelDefinition{
		{ID: "anthropic.claude-3-sonnet", Provider: "Anthropic"},
		{ID: "meta.llama3-70b", Provider: "Meta"},
		{ID: "amazon.titan-text-express-v1", Provider: "Amazon"},
		{ID: "cohere.command-r-plus", Provider: "Cohere"},
	}

	// No filter returns all
	result := filterByProvider(models, nil)
	if len(result) != 4 {
		t.Errorf("expected 4 models with no filter, got %d", len(result))
	}

	// Single provider filter
	result = filterByProvider(models, []string{"anthropic"})
	if len(result) != 1 {
		t.Errorf("expected 1 model with anthropic filter, got %d", len(result))
	}
	if result[0].Provider != "Anthropic" {
		t.Errorf("expected Anthropic, got %s", result[0].Provider)
	}

	// Multiple provider filter
	result = filterByProvider(models, []string{"anthropic", "meta"})
	if len(result) != 2 {
		t.Errorf("expected 2 models with anthropic+meta filter, got %d", len(result))
	}

	// Case insensitive
	result = filterByProvider(models, []string{"ANTHROPIC"})
	if len(result) != 1 {
		t.Errorf("expected case-insensitive filter to work, got %d models", len(result))
	}
}
