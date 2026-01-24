// Package usage provides token usage tracking, cost estimation, and formatting.
package usage

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Usage represents token usage for a single request.
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

// Total returns the total token count.
func (u *Usage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// Add adds another usage record to this one.
func (u *Usage) Add(other *Usage) {
	if other == nil {
		return
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
}

// Cost represents pricing for a model (per million tokens).
type Cost struct {
	Input      float64 `json:"input" yaml:"input"`
	Output     float64 `json:"output" yaml:"output"`
	CacheRead  float64 `json:"cache_read" yaml:"cache_read"`
	CacheWrite float64 `json:"cache_write" yaml:"cache_write"`
}

// Estimate calculates the estimated cost for the given usage.
func (c *Cost) Estimate(usage *Usage) float64 {
	if usage == nil {
		return 0
	}
	total := float64(usage.InputTokens)*c.Input +
		float64(usage.OutputTokens)*c.Output +
		float64(usage.CacheReadTokens)*c.CacheRead +
		float64(usage.CacheWriteTokens)*c.CacheWrite
	return total / 1_000_000
}

// Record represents a usage record for tracking.
type Record struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	UserID    string    `json:"user_id,omitempty"`
	ChannelID string    `json:"channel_id,omitempty"`
	Usage     Usage     `json:"usage"`
	Cost      float64   `json:"cost,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Tracker tracks usage across multiple requests.
type Tracker struct {
	mu       sync.RWMutex
	records  []Record
	totals   map[string]*Usage // keyed by "provider:model"
	byUser   map[string]*Usage
	maxAge   time.Duration
	maxCount int
}

// TrackerConfig configures the usage tracker.
type TrackerConfig struct {
	MaxAge   time.Duration
	MaxCount int
}

// DefaultTrackerConfig returns default tracker configuration.
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		MaxAge:   24 * time.Hour,
		MaxCount: 10000,
	}
}

// NewTracker creates a new usage tracker.
func NewTracker(config TrackerConfig) *Tracker {
	if config.MaxAge <= 0 {
		config.MaxAge = 24 * time.Hour
	}
	if config.MaxCount <= 0 {
		config.MaxCount = 10000
	}

	return &Tracker{
		records:  make([]Record, 0),
		totals:   make(map[string]*Usage),
		byUser:   make(map[string]*Usage),
		maxAge:   config.MaxAge,
		maxCount: config.MaxCount,
	}
}

// Record adds a usage record.
func (t *Tracker) Record(r Record) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}

	t.records = append(t.records, r)

	// Update totals by model
	key := r.Provider + ":" + r.Model
	if t.totals[key] == nil {
		t.totals[key] = &Usage{}
	}
	t.totals[key].Add(&r.Usage)

	// Update totals by user
	if r.UserID != "" {
		if t.byUser[r.UserID] == nil {
			t.byUser[r.UserID] = &Usage{}
		}
		t.byUser[r.UserID].Add(&r.Usage)
	}

	// Prune old records
	t.pruneOld()
}

// pruneOld removes records older than maxAge and beyond maxCount.
func (t *Tracker) pruneOld() {
	cutoff := time.Now().Add(-t.maxAge)

	// Find first record that's not expired
	startIdx := 0
	for i, r := range t.records {
		if r.Timestamp.After(cutoff) {
			startIdx = i
			break
		}
		startIdx = i + 1
	}

	if startIdx > 0 {
		t.records = t.records[startIdx:]
	}

	// Also trim if over count limit
	if len(t.records) > t.maxCount {
		t.records = t.records[len(t.records)-t.maxCount:]
	}
}

// GetTotals returns usage totals for a provider:model key.
func (t *Tracker) GetTotals(provider, model string) *Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := provider + ":" + model
	if usage := t.totals[key]; usage != nil {
		u := *usage
		return &u
	}
	return nil
}

// GetUserTotals returns usage totals for a user.
func (t *Tracker) GetUserTotals(userID string) *Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if usage := t.byUser[userID]; usage != nil {
		u := *usage
		return &u
	}
	return nil
}

// GetRecentRecords returns recent usage records.
func (t *Tracker) GetRecentRecords(limit int) []Record {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.records) {
		limit = len(t.records)
	}

	// Return most recent
	start := len(t.records) - limit
	result := make([]Record, limit)
	copy(result, t.records[start:])
	return result
}

// GetSummary returns a summary of all usage.
func (t *Tracker) GetSummary() map[string]*Usage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]*Usage)
	for k, v := range t.totals {
		u := *v
		result[k] = &u
	}
	return result
}

// FormatTokenCount formats a token count for display.
func FormatTokenCount(count int64) string {
	if count <= 0 {
		return "0"
	}
	if count >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(count)/1_000_000)
	}
	if count >= 10_000 {
		return fmt.Sprintf("%dk", count/1_000)
	}
	if count >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(count)/1_000)
	}
	return fmt.Sprintf("%d", count)
}

// FormatUSD formats a dollar amount for display.
func FormatUSD(amount float64) string {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return ""
	}
	if amount >= 1 {
		return fmt.Sprintf("$%.2f", amount)
	}
	if amount >= 0.01 {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("$%.4f", amount)
}

// FormatUsage formats usage for display.
func FormatUsage(usage *Usage) string {
	if usage == nil {
		return "0 tokens"
	}
	total := usage.Total()
	return FormatTokenCount(total) + " tokens"
}

// FormatUsageDetailed formats usage with breakdown.
func FormatUsageDetailed(usage *Usage) string {
	if usage == nil {
		return "No usage"
	}
	parts := []string{}
	if usage.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in: %s", FormatTokenCount(usage.InputTokens)))
	}
	if usage.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("out: %s", FormatTokenCount(usage.OutputTokens)))
	}
	if usage.CacheReadTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache-r: %s", FormatTokenCount(usage.CacheReadTokens)))
	}
	if usage.CacheWriteTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache-w: %s", FormatTokenCount(usage.CacheWriteTokens)))
	}
	if len(parts) == 0 {
		return "0 tokens"
	}
	return fmt.Sprintf("%s (%s)", FormatTokenCount(usage.Total()), joinParts(parts))
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
