package infra

import (
	"context"
	"sync"
	"time"
)

// UsageWindow represents a rate limit window.
type UsageWindow struct {
	// Name identifies the window (e.g., "daily", "monthly").
	Name string

	// Used is the number of units consumed.
	Used int64

	// Limit is the maximum allowed units.
	Limit int64

	// ResetsAt is when the window resets.
	ResetsAt time.Time
}

// UsagePercent returns the usage as a percentage (0-100).
func (w UsageWindow) UsagePercent() float64 {
	if w.Limit <= 0 {
		return 0
	}
	pct := float64(w.Used) / float64(w.Limit) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// Remaining returns the remaining units.
func (w UsageWindow) Remaining() int64 {
	r := w.Limit - w.Used
	if r < 0 {
		return 0
	}
	return r
}

// ProviderUsage contains usage data for a single provider.
type ProviderUsage struct {
	// Provider is the provider identifier.
	Provider string

	// DisplayName is the human-readable name.
	DisplayName string

	// Windows contains rate limit windows.
	Windows []UsageWindow

	// RequestCount is the total requests made.
	RequestCount int64

	// TokensUsed is the total tokens consumed.
	TokensUsed int64

	// LastRequestAt is when the last request was made.
	LastRequestAt time.Time

	// Error contains any error fetching usage.
	Error string
}

// IsNearLimit returns true if any window is above the threshold percentage.
func (p ProviderUsage) IsNearLimit(threshold float64) bool {
	for _, w := range p.Windows {
		if w.UsagePercent() >= threshold {
			return true
		}
	}
	return false
}

// UsageSummary aggregates usage across all providers.
type UsageSummary struct {
	UpdatedAt time.Time
	Providers []ProviderUsage
}

// Provider returns usage for a specific provider.
func (s *UsageSummary) Provider(id string) (*ProviderUsage, bool) {
	for i := range s.Providers {
		if s.Providers[i].Provider == id {
			return &s.Providers[i], true
		}
	}
	return nil, false
}

// ProvidersNearLimit returns providers above the threshold.
func (s *UsageSummary) ProvidersNearLimit(threshold float64) []ProviderUsage {
	var result []ProviderUsage
	for _, p := range s.Providers {
		if p.IsNearLimit(threshold) {
			result = append(result, p)
		}
	}
	return result
}

// UsageTracker tracks API usage across providers.
type UsageTracker struct {
	mu        sync.RWMutex
	providers map[string]*providerTracker
}

type providerTracker struct {
	displayName   string
	windows       map[string]*windowTracker
	requestCount  int64
	tokensUsed    int64
	lastRequestAt time.Time
}

type windowTracker struct {
	used     int64
	limit    int64
	resetsAt time.Time
}

// NewUsageTracker creates a new usage tracker.
func NewUsageTracker() *UsageTracker {
	return &UsageTracker{
		providers: make(map[string]*providerTracker),
	}
}

// RegisterProvider registers a provider with a display name.
func (t *UsageTracker) RegisterProvider(id, displayName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.providers[id]; !exists {
		t.providers[id] = &providerTracker{
			displayName: displayName,
			windows:     make(map[string]*windowTracker),
		}
	}
}

// RecordRequest records an API request.
func (t *UsageTracker) RecordRequest(provider string, tokens int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := t.getOrCreateProvider(provider)
	p.requestCount++
	p.tokensUsed += tokens
	p.lastRequestAt = time.Now()
}

// UpdateWindow updates a rate limit window for a provider.
func (t *UsageTracker) UpdateWindow(provider, windowName string, used, limit int64, resetsAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := t.getOrCreateProvider(provider)
	p.windows[windowName] = &windowTracker{
		used:     used,
		limit:    limit,
		resetsAt: resetsAt,
	}
}

// IncrementWindow increments usage in a rate limit window.
func (t *UsageTracker) IncrementWindow(provider, windowName string, amount int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := t.getOrCreateProvider(provider)
	w, ok := p.windows[windowName]
	if !ok {
		w = &windowTracker{}
		p.windows[windowName] = w
	}
	w.used += amount
}

// SetWindowLimit sets the limit for a rate limit window.
func (t *UsageTracker) SetWindowLimit(provider, windowName string, limit int64, resetsAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p := t.getOrCreateProvider(provider)
	w, ok := p.windows[windowName]
	if !ok {
		w = &windowTracker{}
		p.windows[windowName] = w
	}
	w.limit = limit
	w.resetsAt = resetsAt
}

func (t *UsageTracker) getOrCreateProvider(id string) *providerTracker {
	p, ok := t.providers[id]
	if !ok {
		p = &providerTracker{
			displayName: id,
			windows:     make(map[string]*windowTracker),
		}
		t.providers[id] = p
	}
	return p
}

// Summary returns the current usage summary.
func (t *UsageTracker) Summary() *UsageSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	summary := &UsageSummary{
		UpdatedAt: time.Now(),
		Providers: make([]ProviderUsage, 0, len(t.providers)),
	}

	for id, p := range t.providers {
		usage := ProviderUsage{
			Provider:      id,
			DisplayName:   p.displayName,
			RequestCount:  p.requestCount,
			TokensUsed:    p.tokensUsed,
			LastRequestAt: p.lastRequestAt,
			Windows:       make([]UsageWindow, 0, len(p.windows)),
		}

		for name, w := range p.windows {
			usage.Windows = append(usage.Windows, UsageWindow{
				Name:     name,
				Used:     w.used,
				Limit:    w.limit,
				ResetsAt: w.resetsAt,
			})
		}

		summary.Providers = append(summary.Providers, usage)
	}

	return summary
}

// ProviderUsageSnapshot returns usage for a specific provider.
func (t *UsageTracker) ProviderUsageSnapshot(provider string) (*ProviderUsage, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	p, ok := t.providers[provider]
	if !ok {
		return nil, false
	}

	usage := &ProviderUsage{
		Provider:      provider,
		DisplayName:   p.displayName,
		RequestCount:  p.requestCount,
		TokensUsed:    p.tokensUsed,
		LastRequestAt: p.lastRequestAt,
		Windows:       make([]UsageWindow, 0, len(p.windows)),
	}

	for name, w := range p.windows {
		usage.Windows = append(usage.Windows, UsageWindow{
			Name:     name,
			Used:     w.used,
			Limit:    w.limit,
			ResetsAt: w.resetsAt,
		})
	}

	return usage, true
}

// Reset clears all usage data.
func (t *UsageTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.providers = make(map[string]*providerTracker)
}

// ResetProvider clears usage data for a specific provider.
func (t *UsageTracker) ResetProvider(provider string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.providers, provider)
}

// UsageFetcher fetches usage data from a provider's API.
type UsageFetcher interface {
	FetchUsage(ctx context.Context) (*ProviderUsage, error)
}

// UsageAggregator combines multiple usage sources.
type UsageAggregator struct {
	mu       sync.RWMutex
	fetchers map[string]UsageFetcher
	timeout  time.Duration
	cache    *UsageSummary
	cacheTTL time.Duration
	cacheAt  time.Time
}

// NewUsageAggregator creates a new usage aggregator.
func NewUsageAggregator(timeout, cacheTTL time.Duration) *UsageAggregator {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &UsageAggregator{
		fetchers: make(map[string]UsageFetcher),
		timeout:  timeout,
		cacheTTL: cacheTTL,
	}
}

// RegisterFetcher registers a usage fetcher for a provider.
func (a *UsageAggregator) RegisterFetcher(provider string, fetcher UsageFetcher) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fetchers[provider] = fetcher
}

// FetchAll fetches usage from all registered providers.
func (a *UsageAggregator) FetchAll(ctx context.Context) (*UsageSummary, error) {
	a.mu.Lock()
	// Check cache
	if a.cache != nil && a.cacheTTL > 0 && time.Since(a.cacheAt) < a.cacheTTL {
		cached := a.cache
		a.mu.Unlock()
		return cached, nil
	}
	fetchers := make(map[string]UsageFetcher, len(a.fetchers))
	for k, v := range a.fetchers {
		fetchers[k] = v
	}
	a.mu.Unlock()

	summary := &UsageSummary{
		UpdatedAt: time.Now(),
		Providers: make([]ProviderUsage, 0, len(fetchers)),
	}

	var wg sync.WaitGroup
	var resultMu sync.Mutex
	results := make([]ProviderUsage, 0, len(fetchers))

	for provider, fetcher := range fetchers {
		wg.Add(1)
		go func(p string, f UsageFetcher) {
			defer wg.Done()

			fetchCtx, cancel := context.WithTimeout(ctx, a.timeout)
			defer cancel()

			usage, err := f.FetchUsage(fetchCtx)

			resultMu.Lock()
			if err != nil {
				results = append(results, ProviderUsage{
					Provider: p,
					Error:    err.Error(),
				})
			} else if usage != nil {
				results = append(results, *usage)
			}
			resultMu.Unlock()
		}(provider, fetcher)
	}

	wg.Wait()
	summary.Providers = results

	// Update cache
	if a.cacheTTL > 0 {
		a.mu.Lock()
		a.cache = summary
		a.cacheAt = time.Now()
		a.mu.Unlock()
	}

	return summary, nil
}

// IgnoredErrors contains errors that should be filtered from results.
var IgnoredErrors = map[string]bool{
	"No credentials": true,
	"No token":       true,
	"No API key":     true,
	"Not logged in":  true,
	"No auth":        true,
}

// FilterIgnoredErrors removes providers with ignored errors.
func FilterIgnoredErrors(summary *UsageSummary) *UsageSummary {
	filtered := &UsageSummary{
		UpdatedAt: summary.UpdatedAt,
		Providers: make([]ProviderUsage, 0, len(summary.Providers)),
	}

	for _, p := range summary.Providers {
		if p.Error != "" && IgnoredErrors[p.Error] {
			continue
		}
		filtered.Providers = append(filtered.Providers, p)
	}

	return filtered
}

// DefaultUsageTracker is the global usage tracker.
var DefaultUsageTracker = NewUsageTracker()

// RecordAPIRequest records an API request to the default tracker.
func RecordAPIRequest(provider string, tokens int64) {
	DefaultUsageTracker.RecordRequest(provider, tokens)
}

// UpdateRateLimitWindow updates a rate limit window in the default tracker.
func UpdateRateLimitWindow(provider, windowName string, used, limit int64, resetsAt time.Time) {
	DefaultUsageTracker.UpdateWindow(provider, windowName, used, limit, resetsAt)
}

// GetUsageSummary returns the usage summary from the default tracker.
func GetUsageSummary() *UsageSummary {
	return DefaultUsageTracker.Summary()
}
