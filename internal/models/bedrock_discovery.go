// Package models provides a catalog of LLM models and their capabilities.
//
// bedrock_discovery.go implements automatic discovery of available AWS Bedrock
// foundation models using the AWS SDK.
package models

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrock/types"
)

const (
	// DefaultBedrockRefreshInterval is how often to refresh the model list.
	DefaultBedrockRefreshInterval = 1 * time.Hour
	// DefaultBedrockContextWindow is the default context window for discovered models.
	DefaultBedrockContextWindow = 32000
	// DefaultBedrockMaxTokens is the default max tokens for discovered models.
	DefaultBedrockMaxTokens = 4096
)

// BedrockDiscoveryConfig configures Bedrock model discovery.
type BedrockDiscoveryConfig struct {
	// Enabled controls whether discovery is active.
	Enabled bool `yaml:"enabled"`

	// Region is the AWS region to query for models.
	Region string `yaml:"region"`

	// RefreshInterval is how often to refresh the model list.
	// Default: 1 hour. Set to 0 to disable caching.
	RefreshInterval time.Duration `yaml:"refresh_interval"`

	// ProviderFilter limits discovery to specific model providers.
	// Example: ["anthropic", "amazon", "meta"]
	// Empty means all providers.
	ProviderFilter []string `yaml:"provider_filter"`

	// DefaultContextWindow is used when the model doesn't report context size.
	DefaultContextWindow int `yaml:"default_context_window"`

	// DefaultMaxTokens is used when the model doesn't report max output.
	DefaultMaxTokens int `yaml:"default_max_tokens"`
}

// BedrockDiscovery manages automatic discovery of Bedrock models.
type BedrockDiscovery struct {
	config BedrockDiscoveryConfig
	logger *slog.Logger

	mu        sync.RWMutex
	cache     []*Model
	expiresAt time.Time
	inFlight  bool

	// For testing
	clientFactory func(region string) BedrockClient
}

// BedrockClient is the interface for AWS Bedrock operations.
type BedrockClient interface {
	ListFoundationModels(ctx context.Context, params *bedrock.ListFoundationModelsInput, optFns ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error)
}

// NewBedrockDiscovery creates a new Bedrock discovery instance.
func NewBedrockDiscovery(cfg BedrockDiscoveryConfig, logger *slog.Logger) *BedrockDiscovery {
	if logger == nil {
		logger = slog.Default()
	}

	// Apply defaults
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = DefaultBedrockRefreshInterval
	}
	if cfg.DefaultContextWindow <= 0 {
		cfg.DefaultContextWindow = DefaultBedrockContextWindow
	}
	if cfg.DefaultMaxTokens <= 0 {
		cfg.DefaultMaxTokens = DefaultBedrockMaxTokens
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	return &BedrockDiscovery{
		config: cfg,
		logger: logger,
	}
}

// Discover retrieves the list of available Bedrock models.
// Results are cached according to RefreshInterval.
func (d *BedrockDiscovery) Discover(ctx context.Context) ([]*Model, error) {
	if !d.config.Enabled {
		return nil, nil
	}

	d.mu.RLock()
	if d.cache != nil && time.Now().Before(d.expiresAt) {
		models := d.cache
		d.mu.RUnlock()
		return models, nil
	}
	d.mu.RUnlock()

	d.mu.Lock()
	// Double-check after acquiring write lock
	if d.cache != nil && time.Now().Before(d.expiresAt) {
		models := d.cache
		d.mu.Unlock()
		return models, nil
	}

	// Prevent concurrent discovery
	if d.inFlight {
		d.mu.Unlock()
		// Wait a bit and return cached data if available
		time.Sleep(100 * time.Millisecond)
		d.mu.RLock()
		models := d.cache
		d.mu.RUnlock()
		return models, nil
	}

	d.inFlight = true
	d.mu.Unlock()

	// Perform discovery
	models, err := d.fetchModels(ctx)

	d.mu.Lock()
	d.inFlight = false
	if err == nil {
		d.cache = models
		d.expiresAt = time.Now().Add(d.config.RefreshInterval)
	}
	d.mu.Unlock()

	if err != nil {
		d.logger.Warn("bedrock discovery failed", "error", err)
		// Return cached data if available
		d.mu.RLock()
		cached := d.cache
		d.mu.RUnlock()
		if cached != nil {
			return cached, nil
		}
		return nil, err
	}

	return models, nil
}

// RegisterWithCatalog discovers Bedrock models and registers them with the catalog.
func (d *BedrockDiscovery) RegisterWithCatalog(ctx context.Context, catalog *Catalog) error {
	models, err := d.Discover(ctx)
	if err != nil {
		return err
	}

	for _, model := range models {
		catalog.Register(model)
	}

	d.logger.Info("registered bedrock models", "count", len(models))
	return nil
}

// fetchModels calls the AWS API to list foundation models.
func (d *BedrockDiscovery) fetchModels(ctx context.Context) ([]*Model, error) {
	client, err := d.createClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create bedrock client: %w", err)
	}

	output, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list foundation models: %w", err)
	}

	var models []*Model
	providerFilter := normalizeProviderFilter(d.config.ProviderFilter)

	for _, summary := range output.ModelSummaries {
		if !d.shouldInclude(summary, providerFilter) {
			continue
		}

		model := d.toModel(summary)
		if model != nil {
			models = append(models, model)
		}
	}

	d.logger.Debug("discovered bedrock models",
		"total", len(output.ModelSummaries),
		"included", len(models))

	return models, nil
}

// createClient creates an AWS Bedrock client.
func (d *BedrockDiscovery) createClient(ctx context.Context) (BedrockClient, error) {
	if d.clientFactory != nil {
		return d.clientFactory(d.config.Region), nil
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(d.config.Region))
	if err != nil {
		return nil, err
	}

	return bedrock.NewFromConfig(cfg), nil
}

// shouldInclude checks if a model should be included based on filters.
func (d *BedrockDiscovery) shouldInclude(summary types.FoundationModelSummary, providerFilter []string) bool {
	// Must have a model ID
	if summary.ModelId == nil || *summary.ModelId == "" {
		return false
	}

	// Must support streaming
	if summary.ResponseStreamingSupported == nil || !*summary.ResponseStreamingSupported {
		return false
	}

	// Must output text
	if !hasTextModality(summary.OutputModalities) {
		return false
	}

	// Must be active
	if summary.ModelLifecycle == nil || summary.ModelLifecycle.Status != types.FoundationModelLifecycleStatusActive {
		return false
	}

	// Check provider filter
	if len(providerFilter) > 0 {
		providerName := extractProviderName(summary)
		if providerName == "" {
			return false
		}
		found := false
		for _, p := range providerFilter {
			if strings.EqualFold(p, providerName) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// toModel converts an AWS model summary to our Model type.
func (d *BedrockDiscovery) toModel(summary types.FoundationModelSummary) *Model {
	if summary.ModelId == nil {
		return nil
	}

	id := *summary.ModelId
	name := id
	if summary.ModelName != nil && *summary.ModelName != "" {
		name = *summary.ModelName
	}

	model := &Model{
		ID:              id,
		Name:            name,
		Provider:        ProviderBedrock,
		Tier:            inferTier(id, name),
		ContextWindow:   d.config.DefaultContextWindow,
		MaxOutputTokens: d.config.DefaultMaxTokens,
		Capabilities:    inferCapabilities(summary),
	}

	// Add provider-specific aliases
	providerName := extractProviderName(summary)
	if providerName != "" {
		model.Description = fmt.Sprintf("%s model via AWS Bedrock", providerName)
	}

	return model
}

// extractProviderName gets the provider name from the model summary.
func extractProviderName(summary types.FoundationModelSummary) string {
	if summary.ProviderName != nil && *summary.ProviderName != "" {
		return strings.ToLower(*summary.ProviderName)
	}
	// Extract from model ID (format: provider.model-name)
	if summary.ModelId != nil {
		parts := strings.SplitN(*summary.ModelId, ".", 2)
		if len(parts) > 0 {
			return strings.ToLower(parts[0])
		}
	}
	return ""
}

// hasTextModality checks if the modalities include text.
func hasTextModality(modalities []types.ModelModality) bool {
	for _, m := range modalities {
		if m == types.ModelModalityText {
			return true
		}
	}
	return false
}

// inferTier determines the model tier based on ID and name.
func inferTier(id, name string) Tier {
	lower := strings.ToLower(id + " " + name)

	if strings.Contains(lower, "opus") || strings.Contains(lower, "large") {
		return TierFlagship
	}
	if strings.Contains(lower, "haiku") || strings.Contains(lower, "mini") || strings.Contains(lower, "lite") {
		return TierFast
	}
	if strings.Contains(lower, "instant") || strings.Contains(lower, "nano") {
		return TierMini
	}
	return TierStandard
}

// inferCapabilities determines capabilities based on the model summary.
func inferCapabilities(summary types.FoundationModelSummary) []Capability {
	caps := []Capability{CapStreaming}

	// Check input modalities
	for _, m := range summary.InputModalities {
		if m == types.ModelModalityImage {
			caps = append(caps, CapVision)
		}
	}

	// Check customization types for fine-tuning support
	for _, c := range summary.CustomizationsSupported {
		if c == types.ModelCustomizationFineTuning {
			caps = append(caps, CapFineTunable)
		}
	}

	// Check inference types
	for _, inf := range summary.InferenceTypesSupported {
		if inf == types.InferenceTypeOnDemand {
			// Most on-demand models support tools
			caps = append(caps, CapTools)
		}
	}

	// Infer reasoning support from model name
	if summary.ModelId != nil {
		lower := strings.ToLower(*summary.ModelId)
		if strings.Contains(lower, "reason") || strings.Contains(lower, "think") {
			caps = append(caps, CapReasoning)
		}
	}

	return caps
}

// normalizeProviderFilter cleans and normalizes the provider filter list.
func normalizeProviderFilter(filter []string) []string {
	if len(filter) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []string

	for _, p := range filter {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}

	return result
}

// ClearCache clears the discovery cache, forcing a refresh on next call.
func (d *BedrockDiscovery) ClearCache() {
	d.mu.Lock()
	d.cache = nil
	d.expiresAt = time.Time{}
	d.mu.Unlock()
}

// SetClientFactory sets a custom client factory (for testing).
func (d *BedrockDiscovery) SetClientFactory(factory func(region string) BedrockClient) {
	d.clientFactory = factory
}
