package managers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type ollamaDiscoveryResult struct {
	BaseURL      string
	DefaultModel string
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func discoverOllama(locations []string, logger *slog.Logger) (*ollamaDiscoveryResult, error) {
	if len(locations) == 0 {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	client := &http.Client{Timeout: 2 * time.Second}

	for _, loc := range locations {
		baseURL := strings.TrimSpace(loc)
		if baseURL == "" {
			continue
		}
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "http://" + baseURL
		}
		baseURL = strings.TrimRight(baseURL, "/")

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		if resp.Body != nil {
			defer resp.Body.Close()
		}
		if resp.StatusCode != http.StatusOK {
			cancel()
			continue
		}

		var payload ollamaTagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			cancel()
			logger.Warn("ollama discovery decode failed", "url", baseURL, "error", err)
			return &ollamaDiscoveryResult{BaseURL: baseURL}, nil
		}
		cancel()

		defaultModel := ""
		if len(payload.Models) > 0 {
			defaultModel = strings.TrimSpace(payload.Models[0].Name)
		}
		logger.Info("ollama discovery succeeded", "url", baseURL)
		return &ollamaDiscoveryResult{BaseURL: baseURL, DefaultModel: defaultModel}, nil
	}

	return nil, nil
}
