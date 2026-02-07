package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/internal/status"
	"github.com/haasonsaas/nexus/internal/usage"
)

const usageBaselineTokens int64 = 1_000_000

type usageWindowResponse struct {
	Label       string  `json:"label"`
	UsedPercent float64 `json:"usedPercent"`
	ResetAt     *int64  `json:"resetAt,omitempty"`
}

type usageProviderResponse struct {
	Provider    string                `json:"provider"`
	DisplayName string                `json:"displayName"`
	Windows     []usageWindowResponse `json:"windows"`
	Plan        string                `json:"plan,omitempty"`
	Error       string                `json:"error,omitempty"`
}

type usageSummaryResponse struct {
	UpdatedAt int64                   `json:"updatedAt"`
	Providers []usageProviderResponse `json:"providers"`
}

type costUsageEntry struct {
	Date     time.Time `json:"date"`
	Cost     float64   `json:"cost"`
	Provider string    `json:"provider,omitempty"`
}

type costUsageResponse struct {
	Entries []costUsageEntry `json:"entries"`
}

// apiUsage handles GET /api/usage.
func (h *Handler) apiUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config == nil || h.config.UsageCache == nil {
		h.jsonError(w, "Usage data unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	providerIDs, providerConfigs := usageProviderIDs(h.config.GatewayConfig)
	usageByProvider := make(map[string]*usage.ProviderUsage)
	if len(providerIDs) == 0 {
		for _, entry := range h.config.UsageCache.GetAll(ctx) {
			if entry == nil {
				continue
			}
			providerID := strings.ToLower(strings.TrimSpace(entry.Provider))
			if providerID == "" {
				continue
			}
			if _, ok := usageByProvider[providerID]; ok {
				continue
			}
			providerIDs = append(providerIDs, providerID)
			usageByProvider[providerID] = entry
		}
	}

	for _, providerID := range providerIDs {
		if _, ok := usageByProvider[providerID]; ok {
			continue
		}
		entry, err := h.config.UsageCache.Get(ctx, providerID)
		if err != nil {
			entry = &usage.ProviderUsage{
				Provider:  providerID,
				FetchedAt: time.Now().UnixMilli(),
				Error:     err.Error(),
			}
		} else if entry == nil {
			entry = &usage.ProviderUsage{
				Provider:  providerID,
				FetchedAt: time.Now().UnixMilli(),
				Error:     "no usage data",
			}
		}
		usageByProvider[providerID] = entry
	}

	sort.Strings(providerIDs)
	response := usageSummaryResponse{
		UpdatedAt: time.Now().UnixMilli(),
		Providers: make([]usageProviderResponse, 0, len(providerIDs)),
	}
	for _, providerID := range providerIDs {
		entry := usageByProvider[providerID]
		providerCfg, ok := providerConfigs[providerID]
		response.Providers = append(response.Providers, buildUsageProvider(providerID, providerCfg, ok, entry))
	}

	h.jsonResponse(w, response)
}

// apiUsageCosts handles GET /api/usage/costs.
func (h *Handler) apiUsageCosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config == nil || h.config.EventStore == nil {
		h.jsonError(w, "Usage data unavailable", http.StatusServiceUnavailable)
		return
	}

	days := 7
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			days = parsed
		}
	}
	if days > 90 {
		days = 90
	}

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -days+1)
	events, err := h.config.EventStore.GetByType(observability.EventTypeLLMResponse, 0)
	if err != nil {
		h.jsonError(w, "Failed to load usage events", http.StatusInternalServerError)
		return
	}

	dayTotals := make(map[string]float64)
	dayDates := make(map[string]time.Time)
	for _, event := range events {
		if event.Timestamp.Before(start) {
			continue
		}
		provider := eventDataString(event.Data, "provider")
		model := eventDataString(event.Data, "model")
		if provider == "" || model == "" {
			continue
		}
		inputTokens := eventDataInt(event.Data, "input_tokens")
		outputTokens := eventDataInt(event.Data, "output_tokens")
		cost := status.EstimateUsageCost(inputTokens, outputTokens, status.ResolveModelCostConfig(provider, model, h.config.GatewayConfig))
		day := time.Date(event.Timestamp.Year(), event.Timestamp.Month(), event.Timestamp.Day(), 0, 0, 0, 0, event.Timestamp.Location())
		key := day.Format("2006-01-02")
		dayTotals[key] += cost
		dayDates[key] = day
	}

	entries := make([]costUsageEntry, 0, len(dayTotals))
	for key, cost := range dayTotals {
		entries = append(entries, costUsageEntry{
			Date: dayDates[key],
			Cost: cost,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Date.Before(entries[j].Date)
	})

	h.jsonResponse(w, costUsageResponse{Entries: entries})
}

func usageProviderIDs(cfg *config.Config) ([]string, map[string]config.LLMProviderConfig) {
	configs := make(map[string]config.LLMProviderConfig)
	if cfg == nil {
		return nil, configs
	}
	providers := make([]string, 0, len(cfg.LLM.Providers))
	for id, providerCfg := range cfg.LLM.Providers {
		providerID := strings.ToLower(strings.TrimSpace(id))
		if providerID == "" {
			continue
		}
		if _, ok := configs[providerID]; ok {
			continue
		}
		providers = append(providers, providerID)
		configs[providerID] = providerCfg
	}
	return providers, configs
}

func buildUsageProvider(providerID string, providerCfg config.LLMProviderConfig, hasConfig bool, entry *usage.ProviderUsage) usageProviderResponse {
	errMsg := ""
	if entry != nil && entry.Error != "" {
		errMsg = entry.Error
	}
	if hasConfig && strings.TrimSpace(providerCfg.APIKey) == "" {
		if errMsg == "" || errMsg == "provider not configured" {
			errMsg = "no API key configured"
		}
	}
	label := "Current period"
	if entry != nil {
		if period := strings.TrimSpace(entry.Period); period != "" {
			label = period
		}
	}
	usedPercent := usagePercent(entry, errMsg)
	return usageProviderResponse{
		Provider:    providerID,
		DisplayName: providerDisplayName(providerID),
		Windows: []usageWindowResponse{{
			Label:       label,
			UsedPercent: usedPercent,
		}},
		Plan:  "",
		Error: errMsg,
	}
}

func usagePercent(entry *usage.ProviderUsage, errMsg string) float64 {
	if errMsg != "" || entry == nil || entry.TotalTokens <= 0 || usageBaselineTokens <= 0 {
		return 0
	}
	percent := float64(entry.TotalTokens) / float64(usageBaselineTokens) * 100
	if percent < 0 {
		return 0
	}
	return math.Min(100, percent)
}

func providerDisplayName(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "google":
		return "Google"
	case "gemini":
		return "Gemini"
	case "bedrock":
		return "AWS Bedrock"
	case "azure", "azure-openai":
		return "Azure OpenAI"
	case "cohere":
		return "Cohere"
	case "mistral":
		return "Mistral"
	case "groq":
		return "Groq"
	case "ollama":
		return "Ollama"
	case "venice":
		return "Venice"
	case "deepseek":
		return "DeepSeek"
	case "perplexity":
		return "Perplexity"
	case "xai", "x-ai":
		return "xAI"
	case "openrouter":
		return "OpenRouter"
	case "together":
		return "Together"
	case "huggingface", "hf":
		return "Hugging Face"
	case "fireworks":
		return "Fireworks"
	case "replicate":
		return "Replicate"
	case "ai21":
		return "AI21"
	case "claude":
		return "Claude"
	case "amazon":
		return "Amazon"
	}
	if provider == "" {
		return "Unknown"
	}
	parts := strings.FieldsFunc(provider, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func eventDataString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return ""
}

func eventDataInt(data map[string]interface{}, key string) int {
	if data == nil {
		return 0
	}
	value, ok := data[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}
