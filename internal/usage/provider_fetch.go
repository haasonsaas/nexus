// Package usage provides provider usage fetching from external APIs.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ProviderUsage contains usage data fetched from a provider.
type ProviderUsage struct {
	Provider     string           `json:"provider"`
	Period       string           `json:"period,omitempty"`
	TotalTokens  int64            `json:"total_tokens,omitempty"`
	InputTokens  int64            `json:"input_tokens,omitempty"`
	OutputTokens int64            `json:"output_tokens,omitempty"`
	TotalCostUSD float64          `json:"total_cost_usd,omitempty"`
	Breakdown    []UsageBreakdown `json:"breakdown,omitempty"`
	FetchedAt    int64            `json:"fetched_at"`
	Error        string           `json:"error,omitempty"`
}

// UsageBreakdown contains per-model usage breakdown.
type UsageBreakdown struct {
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	TotalTokens  int64   `json:"total_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Requests     int64   `json:"requests,omitempty"`
}

// ProviderUsageFetcher fetches usage from a provider API.
type ProviderUsageFetcher interface {
	Fetch(ctx context.Context) (*ProviderUsage, error)
	Provider() string
}

// AnthropicUsageFetcher fetches usage from Anthropic API.
type AnthropicUsageFetcher struct {
	APIKey     string
	HTTPClient *http.Client
}

// Provider returns the provider name.
func (f *AnthropicUsageFetcher) Provider() string {
	return "anthropic"
}

// Fetch retrieves usage data from Anthropic.
func (f *AnthropicUsageFetcher) Fetch(ctx context.Context) (*ProviderUsage, error) {
	usage := &ProviderUsage{
		Provider:  "anthropic",
		FetchedAt: time.Now().UnixMilli(),
	}

	if f.APIKey == "" {
		usage.Error = "no API key configured"
		return usage, nil
	}

	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	now := time.Now().UTC()
	startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	endDate := now
	usage.Period = fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	breakdown := make(map[string]*UsageBreakdown)
	if err := f.fetchUsageReport(ctx, client, startDate, endDate, breakdown); err != nil {
		usage.Error = err.Error()
		return usage, nil
	}

	totalCostUSD, err := f.fetchCostReport(ctx, client, startDate, endDate, breakdown)
	if err == nil {
		usage.TotalCostUSD = totalCostUSD
	}

	for _, b := range breakdown {
		b.TotalTokens = b.InputTokens + b.OutputTokens
		usage.InputTokens += b.InputTokens
		usage.OutputTokens += b.OutputTokens
		usage.Breakdown = append(usage.Breakdown, *b)
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	return usage, nil
}

const anthropicAdminVersion = "2023-06-01"
const anthropicUsageEndpoint = "https://api.anthropic.com/v1/organizations/usage_report/messages"
const anthropicCostEndpoint = "https://api.anthropic.com/v1/organizations/cost_report"

func (f *AnthropicUsageFetcher) fetchUsageReport(ctx context.Context, client *http.Client, startDate, endDate time.Time, breakdown map[string]*UsageBreakdown) error {
	page := ""
	for {
		endpoint, err := buildAnthropicUsageURL(startDate, endDate, page)
		if err != nil {
			return err
		}
		resp, err := f.doRequest(ctx, client, endpoint)
		if err != nil {
			return err
		}

		var payload anthropicUsageReportResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return fmt.Errorf("decode usage report: %w", err)
		}
		resp.Body.Close()

		for _, bucket := range payload.Data {
			for _, item := range bucket.Results {
				model := strings.TrimSpace(item.Model)
				if model == "" {
					model = "unknown"
				}
				b, ok := breakdown[model]
				if !ok {
					b = &UsageBreakdown{Model: model}
					breakdown[model] = b
				}

				inputTokens := item.UncachedInputTokens + item.CacheReadInputTokens
				if item.CacheCreation != nil {
					inputTokens += item.CacheCreation.Ephemeral1hInputTokens
					inputTokens += item.CacheCreation.Ephemeral5mInputTokens
				}
				b.InputTokens += inputTokens
				b.OutputTokens += item.OutputTokens
			}
		}

		if !payload.HasMore || payload.NextPage == "" {
			break
		}
		page = payload.NextPage
	}
	return nil
}

func (f *AnthropicUsageFetcher) fetchCostReport(ctx context.Context, client *http.Client, startDate, endDate time.Time, breakdown map[string]*UsageBreakdown) (float64, error) {
	totalCostUSD := 0.0
	page := ""
	for {
		endpoint, err := buildAnthropicCostURL(startDate, endDate, page)
		if err != nil {
			return totalCostUSD, err
		}
		resp, err := f.doRequest(ctx, client, endpoint)
		if err != nil {
			return totalCostUSD, err
		}

		var payload anthropicCostReportResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return totalCostUSD, fmt.Errorf("decode cost report: %w", err)
		}
		resp.Body.Close()

		for _, bucket := range payload.Data {
			for _, item := range bucket.Results {
				if strings.TrimSpace(item.Amount) == "" {
					continue
				}
				amountCents, err := strconv.ParseFloat(item.Amount, 64)
				if err != nil {
					return totalCostUSD, fmt.Errorf("parse cost amount: %w", err)
				}
				costUSD := amountCents / 100.0
				totalCostUSD += costUSD

				model := strings.TrimSpace(item.Model)
				if model == "" {
					continue
				}
				b, ok := breakdown[model]
				if !ok {
					b = &UsageBreakdown{Model: model}
					breakdown[model] = b
				}
				b.CostUSD += costUSD
			}
		}

		if !payload.HasMore || payload.NextPage == "" {
			break
		}
		page = payload.NextPage
	}
	return totalCostUSD, nil
}

func (f *AnthropicUsageFetcher) doRequest(ctx context.Context, client *http.Client, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", f.APIKey)
	req.Header.Set("anthropic-version", anthropicAdminVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch usage: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("API error %d (read body failed: %v)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func buildAnthropicUsageURL(startDate, endDate time.Time, page string) (string, error) {
	query := url.Values{}
	query.Set("starting_at", startDate.Format(time.RFC3339))
	query.Set("ending_at", endDate.Format(time.RFC3339))
	query.Set("bucket_width", "1d")
	query.Set("limit", "31")
	query.Add("group_by[]", "model")
	if page != "" {
		query.Set("page", page)
	}
	return anthropicUsageEndpoint + "?" + query.Encode(), nil
}

func buildAnthropicCostURL(startDate, endDate time.Time, page string) (string, error) {
	query := url.Values{}
	query.Set("starting_at", startDate.Format(time.RFC3339))
	query.Set("ending_at", endDate.Format(time.RFC3339))
	query.Set("bucket_width", "1d")
	query.Set("limit", "31")
	query.Add("group_by[]", "description")
	if page != "" {
		query.Set("page", page)
	}
	return anthropicCostEndpoint + "?" + query.Encode(), nil
}

type anthropicUsageReportResponse struct {
	Data     []anthropicUsageBucket `json:"data"`
	HasMore  bool                   `json:"has_more"`
	NextPage string                 `json:"next_page"`
}

type anthropicUsageBucket struct {
	StartingAt string                 `json:"starting_at"`
	EndingAt   string                 `json:"ending_at"`
	Results    []anthropicUsageResult `json:"results"`
}

type anthropicUsageResult struct {
	UncachedInputTokens  int64                   `json:"uncached_input_tokens"`
	CacheCreation        *anthropicCacheCreation `json:"cache_creation"`
	CacheReadInputTokens int64                   `json:"cache_read_input_tokens"`
	OutputTokens         int64                   `json:"output_tokens"`
	Model                string                  `json:"model"`
}

type anthropicCacheCreation struct {
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
}

type anthropicCostReportResponse struct {
	Data     []anthropicCostBucket `json:"data"`
	HasMore  bool                  `json:"has_more"`
	NextPage string                `json:"next_page"`
}

type anthropicCostBucket struct {
	StartingAt string                `json:"starting_at"`
	EndingAt   string                `json:"ending_at"`
	Results    []anthropicCostResult `json:"results"`
}

type anthropicCostResult struct {
	Currency    string `json:"currency"`
	Amount      string `json:"amount"`
	CostType    string `json:"cost_type"`
	Description string `json:"description"`
	Model       string `json:"model"`
}

// OpenAIUsageFetcher fetches usage from OpenAI API.
type OpenAIUsageFetcher struct {
	APIKey       string
	Organization string
	HTTPClient   *http.Client
}

// Provider returns the provider name.
func (f *OpenAIUsageFetcher) Provider() string {
	return "openai"
}

// Fetch retrieves usage data from OpenAI.
func (f *OpenAIUsageFetcher) Fetch(ctx context.Context) (*ProviderUsage, error) {
	usage := &ProviderUsage{
		Provider:  "openai",
		FetchedAt: time.Now().UnixMilli(),
	}

	if f.APIKey == "" {
		usage.Error = "no API key configured"
		return usage, nil
	}

	client := f.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	// Get current billing period dates
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	endDate := now

	url := fmt.Sprintf("https://api.openai.com/v1/usage?start_date=%s&end_date=%s",
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		usage.Error = fmt.Sprintf("create request: %v", err)
		return usage, nil
	}

	req.Header.Set("Authorization", "Bearer "+f.APIKey)
	if f.Organization != "" {
		req.Header.Set("OpenAI-Organization", f.Organization)
	}

	resp, err := client.Do(req)
	if err != nil {
		usage.Error = fmt.Sprintf("fetch usage: %v", err)
		return usage, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			usage.Error = fmt.Sprintf("API error %d (read body failed: %v)", resp.StatusCode, err)
			return usage, nil
		}
		usage.Error = fmt.Sprintf("API error %d: %s", resp.StatusCode, string(body))
		return usage, nil
	}

	var result openAIUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		usage.Error = fmt.Sprintf("decode response: %v", err)
		return usage, nil
	}

	// Aggregate usage
	breakdown := make(map[string]*UsageBreakdown)
	for _, item := range result.Data {
		model := item.SnapshotID
		if model == "" {
			model = "unknown"
		}

		b, ok := breakdown[model]
		if !ok {
			b = &UsageBreakdown{Model: model}
			breakdown[model] = b
		}

		b.InputTokens += item.NContextTokensTotal
		b.OutputTokens += item.NGeneratedTokensTotal
		b.Requests += item.NRequests
	}

	for _, b := range breakdown {
		b.TotalTokens = b.InputTokens + b.OutputTokens
		usage.InputTokens += b.InputTokens
		usage.OutputTokens += b.OutputTokens
		usage.Breakdown = append(usage.Breakdown, *b)
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	usage.Period = fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	return usage, nil
}

type openAIUsageResponse struct {
	Object string           `json:"object"`
	Data   []openAIUsageDay `json:"data"`
}

type openAIUsageDay struct {
	SnapshotID            string `json:"snapshot_id"`
	NContextTokensTotal   int64  `json:"n_context_tokens_total"`
	NGeneratedTokensTotal int64  `json:"n_generated_tokens_total"`
	NRequests             int64  `json:"n_requests"`
}

// GeminiUsageFetcher fetches usage from Google Gemini API.
type GeminiUsageFetcher struct {
	APIKey     string
	HTTPClient *http.Client
}

// Provider returns the provider name.
func (f *GeminiUsageFetcher) Provider() string {
	return "gemini"
}

// Fetch retrieves usage data from Gemini.
func (f *GeminiUsageFetcher) Fetch(ctx context.Context) (*ProviderUsage, error) {
	usage := &ProviderUsage{
		Provider:  "gemini",
		FetchedAt: time.Now().UnixMilli(),
	}

	if f.APIKey == "" {
		usage.Error = "no API key configured"
		return usage, nil
	}

	// Gemini doesn't have a public usage API endpoint yet
	usage.Error = "Gemini usage API not yet available"
	return usage, nil
}

// UsageFetcherRegistry manages provider usage fetchers.
type UsageFetcherRegistry struct {
	fetchers map[string]ProviderUsageFetcher
}

// NewUsageFetcherRegistry creates a new registry.
func NewUsageFetcherRegistry() *UsageFetcherRegistry {
	return &UsageFetcherRegistry{
		fetchers: make(map[string]ProviderUsageFetcher),
	}
}

// Register adds a fetcher for a provider.
func (r *UsageFetcherRegistry) Register(fetcher ProviderUsageFetcher) {
	r.fetchers[fetcher.Provider()] = fetcher
}

// Fetch retrieves usage from a specific provider.
func (r *UsageFetcherRegistry) Fetch(ctx context.Context, provider string) (*ProviderUsage, error) {
	fetcher, ok := r.fetchers[provider]
	if !ok {
		return &ProviderUsage{
			Provider:  provider,
			FetchedAt: time.Now().UnixMilli(),
			Error:     "provider not configured",
		}, nil
	}
	return fetcher.Fetch(ctx)
}

// FetchAll retrieves usage from all configured providers.
func (r *UsageFetcherRegistry) FetchAll(ctx context.Context) []*ProviderUsage {
	results := make([]*ProviderUsage, 0, len(r.fetchers))
	for _, fetcher := range r.fetchers {
		usage, err := fetcher.Fetch(ctx)
		if err != nil {
			usage = &ProviderUsage{
				Provider:  fetcher.Provider(),
				FetchedAt: time.Now().UnixMilli(),
				Error:     err.Error(),
			}
		}
		if usage == nil {
			usage = &ProviderUsage{
				Provider:  fetcher.Provider(),
				FetchedAt: time.Now().UnixMilli(),
				Error:     "no usage data",
			}
		}
		results = append(results, usage)
	}
	return results
}

// Providers returns all registered provider names.
func (r *UsageFetcherRegistry) Providers() []string {
	names := make([]string, 0, len(r.fetchers))
	for name := range r.fetchers {
		names = append(names, name)
	}
	return names
}

// UsageCache caches provider usage data.
type UsageCache struct {
	cache    map[string]*cachedUsage
	ttl      time.Duration
	registry *UsageFetcherRegistry
}

type cachedUsage struct {
	usage     *ProviderUsage
	fetchedAt time.Time
}

// NewUsageCache creates a new usage cache.
func NewUsageCache(registry *UsageFetcherRegistry, ttl time.Duration) *UsageCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &UsageCache{
		cache:    make(map[string]*cachedUsage),
		ttl:      ttl,
		registry: registry,
	}
}

// Get retrieves cached usage or fetches fresh data.
func (c *UsageCache) Get(ctx context.Context, provider string) (*ProviderUsage, error) {
	if cached, ok := c.cache[provider]; ok {
		if time.Since(cached.fetchedAt) < c.ttl {
			return cached.usage, nil
		}
	}

	usage, err := c.registry.Fetch(ctx, provider)
	if err != nil {
		return nil, err
	}

	c.cache[provider] = &cachedUsage{
		usage:     usage,
		fetchedAt: time.Now(),
	}

	return usage, nil
}

// GetAll retrieves all provider usage with caching.
func (c *UsageCache) GetAll(ctx context.Context) []*ProviderUsage {
	results := make([]*ProviderUsage, 0)
	for _, provider := range c.registry.Providers() {
		usage, err := c.Get(ctx, provider)
		if err != nil {
			results = append(results, &ProviderUsage{
				Provider:  provider,
				FetchedAt: time.Now().UnixMilli(),
				Error:     err.Error(),
			})
			continue
		}
		if usage != nil {
			results = append(results, usage)
		}
	}
	return results
}

// Invalidate clears the cache for a provider.
func (c *UsageCache) Invalidate(provider string) {
	delete(c.cache, provider)
}

// InvalidateAll clears all cached data.
func (c *UsageCache) InvalidateAll() {
	c.cache = make(map[string]*cachedUsage)
}

// FormatProviderUsage formats provider usage for display.
func FormatProviderUsage(usage *ProviderUsage) string {
	if usage == nil {
		return "No usage data"
	}

	if usage.Error != "" {
		return fmt.Sprintf("%s: %s", usage.Provider, usage.Error)
	}

	result := fmt.Sprintf("%s Usage", usage.Provider)
	if usage.Period != "" {
		result += fmt.Sprintf(" (%s)", usage.Period)
	}
	result += "\n"

	result += fmt.Sprintf("  Total: %s tokens\n", FormatTokenCount(usage.TotalTokens))
	if usage.InputTokens > 0 {
		result += fmt.Sprintf("  Input: %s tokens\n", FormatTokenCount(usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		result += fmt.Sprintf("  Output: %s tokens\n", FormatTokenCount(usage.OutputTokens))
	}
	if usage.TotalCostUSD > 0 {
		result += fmt.Sprintf("  Cost: %s\n", FormatUSD(usage.TotalCostUSD))
	}

	if len(usage.Breakdown) > 0 {
		result += "  By model:\n"
		for _, b := range usage.Breakdown {
			result += fmt.Sprintf("    %s: %s tokens", b.Model, FormatTokenCount(b.TotalTokens))
			if b.CostUSD > 0 {
				result += fmt.Sprintf(" (%s)", FormatUSD(b.CostUSD))
			}
			result += "\n"
		}
	}

	return result
}
