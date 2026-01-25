// Package bedrock provides AWS Bedrock model discovery and management utilities.
package bedrock

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrock/types"
)

// ModelDefinition represents a discovered Bedrock model with its capabilities.
type ModelDefinition struct {
	ID                 string   // Model identifier (e.g., "anthropic.claude-3-sonnet-20240229-v1:0")
	Name               string   // Human-readable model name
	Provider           string   // Provider name (e.g., "Anthropic", "Meta")
	Reasoning          bool     // Whether the model supports reasoning/thinking
	Input              []string // Supported input modalities: "text", "image"
	Output             []string // Supported output modalities: "text", "image"
	ContextWindow      int      // Maximum context window size
	MaxTokens          int      // Maximum output tokens
	StreamingSupported bool     // Whether streaming is supported
	LifecycleStatus    string   // Model lifecycle status (e.g., "ACTIVE", "LEGACY")
}

// DiscoveryConfig holds configuration for model discovery.
type DiscoveryConfig struct {
	// Region is the AWS region to query (default: us-east-1)
	Region string

	// RefreshInterval is how long to cache discovered models (default: 1 hour)
	RefreshInterval time.Duration

	// ProviderFilter limits discovery to specific providers (e.g., ["anthropic", "meta"])
	// Empty slice means all providers
	ProviderFilter []string

	// DefaultContextWindow is used when model doesn't specify context size
	DefaultContextWindow int

	// DefaultMaxTokens is used when model doesn't specify max output tokens
	DefaultMaxTokens int

	// AccessKeyID for explicit AWS credentials (optional)
	AccessKeyID string

	// SecretAccessKey for explicit AWS credentials (optional)
	SecretAccessKey string

	// SessionToken for temporary credentials (optional)
	SessionToken string
}

// discoveryCache holds cached model discovery results with thread-safe access.
type discoveryCache struct {
	mu        sync.RWMutex
	models    []ModelDefinition
	expiresAt time.Time
	inFlight  chan struct{} // Used for request deduplication
}

// globalCache is the package-level cache for discovered models.
var globalCache = &discoveryCache{}

// BedrockClientAPI defines the interface for Bedrock client operations.
// This allows for mocking in tests.
type BedrockClientAPI interface {
	ListFoundationModels(ctx context.Context, params *bedrock.ListFoundationModelsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error)
}

// clientFactory allows overriding client creation for testing.
var clientFactory func(cfg aws.Config) BedrockClientAPI

func init() {
	clientFactory = func(cfg aws.Config) BedrockClientAPI {
		return bedrock.NewFromConfig(cfg)
	}
}

// DiscoverModels fetches available Bedrock models with caching and request deduplication.
//
// The function maintains a cache of discovered models that is refreshed according to
// the RefreshInterval in the config. Concurrent calls during a refresh will wait for
// the in-flight request to complete rather than making duplicate API calls.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - cfg: Discovery configuration (nil uses defaults)
//
// Returns:
//   - []ModelDefinition: List of available models matching the filter criteria
//   - error: Any error encountered during discovery
func DiscoverModels(ctx context.Context, cfg *DiscoveryConfig) ([]ModelDefinition, error) {
	if cfg == nil {
		cfg = &DiscoveryConfig{}
	}
	applyDefaults(cfg)

	// Check cache first
	globalCache.mu.RLock()
	if time.Now().Before(globalCache.expiresAt) && len(globalCache.models) > 0 {
		models := filterByProvider(globalCache.models, cfg.ProviderFilter)
		globalCache.mu.RUnlock()
		return models, nil
	}
	globalCache.mu.RUnlock()

	// Need to refresh - acquire write lock
	globalCache.mu.Lock()

	// Double-check after acquiring write lock
	if time.Now().Before(globalCache.expiresAt) && len(globalCache.models) > 0 {
		models := filterByProvider(globalCache.models, cfg.ProviderFilter)
		globalCache.mu.Unlock()
		return models, nil
	}

	// Check if there's an in-flight request
	if globalCache.inFlight != nil {
		inFlight := globalCache.inFlight
		globalCache.mu.Unlock()

		// Wait for in-flight request to complete
		select {
		case <-inFlight:
			// Request completed, read from cache
			globalCache.mu.RLock()
			models := filterByProvider(globalCache.models, cfg.ProviderFilter)
			globalCache.mu.RUnlock()
			return models, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Start new request
	globalCache.inFlight = make(chan struct{})
	globalCache.mu.Unlock()

	// Fetch models from AWS
	models, err := fetchModels(ctx, cfg)

	// Update cache
	globalCache.mu.Lock()
	if err == nil {
		globalCache.models = models
		globalCache.expiresAt = time.Now().Add(cfg.RefreshInterval)
	}
	close(globalCache.inFlight)
	globalCache.inFlight = nil
	globalCache.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return filterByProvider(models, cfg.ProviderFilter), nil
}

// ClearCache clears the discovery cache, forcing a refresh on next call.
func ClearCache() {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()
	globalCache.models = nil
	globalCache.expiresAt = time.Time{}
}

// fetchModels retrieves models from the AWS Bedrock API.
func fetchModels(ctx context.Context, cfg *DiscoveryConfig) ([]ModelDefinition, error) {
	// Load AWS config
	var awsCfg aws.Config
	var err error

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				cfg.SessionToken,
			)),
		)
	} else {
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
		)
	}
	if err != nil {
		return nil, err
	}

	client := clientFactory(awsCfg)

	// List foundation models
	output, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, err
	}

	models := make([]ModelDefinition, 0, len(output.ModelSummaries))
	for _, summary := range output.ModelSummaries {
		if !shouldIncludeModel(&summary, cfg.ProviderFilter) {
			continue
		}
		models = append(models, toModelDefinition(&summary, *cfg))
	}

	return models, nil
}

// shouldIncludeModel determines if a model should be included based on filter criteria.
func shouldIncludeModel(summary *types.FoundationModelSummary, filter []string) bool {
	if summary == nil {
		return false
	}

	// Check lifecycle status - only include ACTIVE models
	if summary.ModelLifecycle != nil {
		status := string(summary.ModelLifecycle.Status)
		if status != "ACTIVE" && status != "" {
			return false
		}
	}

	// Check response streaming support - we prefer models that support streaming
	// Note: We still include non-streaming models, this is just informational

	// Check provider filter
	if len(filter) == 0 {
		return true
	}

	providerName := strings.ToLower(aws.ToString(summary.ProviderName))
	for _, f := range filter {
		if strings.ToLower(f) == providerName {
			return true
		}
		// Also check if model ID starts with provider prefix
		modelID := strings.ToLower(aws.ToString(summary.ModelId))
		if strings.HasPrefix(modelID, strings.ToLower(f)+".") {
			return true
		}
	}

	return false
}

// toModelDefinition converts an AWS FoundationModelSummary to our ModelDefinition.
func toModelDefinition(summary *types.FoundationModelSummary, defaults DiscoveryConfig) ModelDefinition {
	def := ModelDefinition{
		ID:                 aws.ToString(summary.ModelId),
		Name:               aws.ToString(summary.ModelName),
		Provider:           aws.ToString(summary.ProviderName),
		ContextWindow:      defaults.DefaultContextWindow,
		MaxTokens:          defaults.DefaultMaxTokens,
		StreamingSupported: aws.ToBool(summary.ResponseStreamingSupported),
	}

	// Convert input modalities
	for _, m := range summary.InputModalities {
		def.Input = append(def.Input, strings.ToLower(string(m)))
	}

	// Convert output modalities
	for _, m := range summary.OutputModalities {
		def.Output = append(def.Output, strings.ToLower(string(m)))
	}

	// Set lifecycle status
	if summary.ModelLifecycle != nil {
		def.LifecycleStatus = string(summary.ModelLifecycle.Status)
	}

	// Detect reasoning capability based on model ID patterns
	modelID := strings.ToLower(def.ID)
	def.Reasoning = isReasoningModel(modelID)

	// Set context window based on known model families
	def.ContextWindow = getModelContextWindow(modelID, defaults.DefaultContextWindow)

	// Set max tokens based on known model families
	def.MaxTokens = getModelMaxTokens(modelID, defaults.DefaultMaxTokens)

	return def
}

// isReasoningModel determines if a model supports extended reasoning/thinking.
func isReasoningModel(modelID string) bool {
	reasoningPatterns := []string{
		"claude-3-5",
		"claude-sonnet-4",
		"claude-opus-4",
		"o1",
		"o3",
		"deepseek-r1",
	}

	for _, pattern := range reasoningPatterns {
		if strings.Contains(modelID, pattern) {
			return true
		}
	}
	return false
}

// getModelContextWindow returns the context window size for known model families.
func getModelContextWindow(modelID string, defaultSize int) int {
	// Claude models
	if strings.Contains(modelID, "claude-3") || strings.Contains(modelID, "claude-sonnet-4") || strings.Contains(modelID, "claude-opus-4") {
		return 200000
	}
	if strings.Contains(modelID, "claude-v2") || strings.Contains(modelID, "claude-2") {
		return 200000
	}
	if strings.Contains(modelID, "claude-instant") {
		return 100000
	}

	// Llama models
	if strings.Contains(modelID, "llama3") {
		if strings.Contains(modelID, "405b") {
			return 128000
		}
		return 8192
	}
	if strings.Contains(modelID, "llama2") {
		return 4096
	}

	// Mistral models
	if strings.Contains(modelID, "mistral") || strings.Contains(modelID, "mixtral") {
		return 32768
	}

	// Cohere models
	if strings.Contains(modelID, "command-r") {
		return 128000
	}
	if strings.Contains(modelID, "command") {
		return 4096
	}

	// Amazon Titan
	if strings.Contains(modelID, "titan-text-express") {
		return 8192
	}
	if strings.Contains(modelID, "titan-text-lite") {
		return 4096
	}
	if strings.Contains(modelID, "titan") {
		return 8192
	}

	// AI21 Jamba
	if strings.Contains(modelID, "jamba") {
		return 256000
	}

	return defaultSize
}

// getModelMaxTokens returns the max output tokens for known model families.
func getModelMaxTokens(modelID string, defaultSize int) int {
	// Claude models - support up to 8192 output tokens (some newer versions support more)
	if strings.Contains(modelID, "claude-3-5") || strings.Contains(modelID, "claude-sonnet-4") || strings.Contains(modelID, "claude-opus-4") {
		return 8192
	}
	if strings.Contains(modelID, "claude-3") {
		return 4096
	}
	if strings.Contains(modelID, "claude") {
		return 4096
	}

	// Llama models
	if strings.Contains(modelID, "llama") {
		return 2048
	}

	// Mistral models
	if strings.Contains(modelID, "mistral") || strings.Contains(modelID, "mixtral") {
		return 4096
	}

	// Cohere
	if strings.Contains(modelID, "command") {
		return 4096
	}

	// Amazon Titan
	if strings.Contains(modelID, "titan") {
		return 8192
	}

	return defaultSize
}

// filterByProvider filters models by provider names.
func filterByProvider(models []ModelDefinition, filter []string) []ModelDefinition {
	if len(filter) == 0 {
		return models
	}

	result := make([]ModelDefinition, 0, len(models))
	for _, m := range models {
		providerLower := strings.ToLower(m.Provider)
		idLower := strings.ToLower(m.ID)
		for _, f := range filter {
			fLower := strings.ToLower(f)
			if providerLower == fLower || strings.HasPrefix(idLower, fLower+".") {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

// applyDefaults sets default values for unset config fields.
func applyDefaults(cfg *DiscoveryConfig) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = time.Hour
	}
	if cfg.DefaultContextWindow == 0 {
		cfg.DefaultContextWindow = 4096
	}
	if cfg.DefaultMaxTokens == 0 {
		cfg.DefaultMaxTokens = 4096
	}
}
