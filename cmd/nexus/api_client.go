package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/infra"
)

type systemStatus struct {
	Uptime         time.Duration       `json:"uptime"`
	UptimeString   string              `json:"uptime_string"`
	GoVersion      string              `json:"go_version"`
	NumGoroutines  int                 `json:"num_goroutines"`
	MemAllocMB     float64             `json:"mem_alloc_mb"`
	MemSysMB       float64             `json:"mem_sys_mb"`
	NumCPU         int                 `json:"num_cpu"`
	SessionCount   int                 `json:"session_count"`
	DatabaseStatus string              `json:"database_status"`
	Channels       []channelStatus     `json:"channels"`
	HealthChecks   *infra.HealthReport `json:"health_checks,omitempty"`
}

type channelStatus struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	Enabled         bool   `json:"enabled"`
	Connected       bool   `json:"connected,omitempty"`
	Error           string `json:"error,omitempty"`
	LastPing        int64  `json:"last_ping,omitempty"`
	Healthy         bool   `json:"healthy,omitempty"`
	HealthMessage   string `json:"health_message,omitempty"`
	HealthLatencyMs int64  `json:"health_latency_ms,omitempty"`
	HealthDegraded  bool   `json:"health_degraded,omitempty"`
}

type providerStatus struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Connected      bool   `json:"connected"`
	Error          string `json:"error,omitempty"`
	LastPing       int64  `json:"last_ping,omitempty"`
	Healthy        bool   `json:"healthy,omitempty"`
	HealthMessage  string `json:"health_message,omitempty"`
	HealthLatency  int64  `json:"health_latency_ms,omitempty"`
	HealthDegraded bool   `json:"health_degraded,omitempty"`
	QRAvailable    bool   `json:"qr_available,omitempty"`
	QRUpdatedAt    string `json:"qr_updated_at,omitempty"`
}

type apiClient struct {
	baseURL    string
	token      string
	apiKey     string
	httpClient *http.Client
}

func newAPIClient(baseURL, token, apiKey string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *apiClient) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("request %s failed: %s (read body: %w)", path, resp.Status, readErr)
		}
		if len(body) > 0 {
			return fmt.Errorf("request %s failed: %s (%s)", path, resp.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("request %s failed: %s", path, resp.Status)
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *apiClient) postJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("request %s failed: %s (read body: %w)", path, resp.Status, readErr)
		}
		if len(bodyBytes) > 0 {
			return fmt.Errorf("request %s failed: %s (%s)", path, resp.Status, strings.TrimSpace(string(bodyBytes)))
		}
		return fmt.Errorf("request %s failed: %s", path, resp.Status)
	}

	if out == nil {
		return nil
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func resolveHTTPBaseURL(configPath, serverAddr string) (string, error) {
	addr := strings.TrimSpace(serverAddr)
	if addr == "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return "", fmt.Errorf("load config: %w", err)
		}
		host := cfg.Server.Host
		if strings.TrimSpace(host) == "" {
			host = "localhost"
		}
		port := cfg.Server.HTTPPort
		if port == 0 {
			port = 8080
		}
		addr = fmt.Sprintf("%s:%d", host, port)
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/"), nil
	}
	return "http://" + strings.TrimRight(addr, "/"), nil
}
