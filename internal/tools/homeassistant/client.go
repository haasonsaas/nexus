package homeassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout          = 10 * time.Second
	defaultMaxResponseBytes = int64(1 << 20) // 1MB
)

// Config configures the Home Assistant client.
type Config struct {
	BaseURL          string
	Token            string
	Timeout          time.Duration
	MaxResponseBytes int64
	HTTPClient       *http.Client
}

// Client wraps Home Assistant's REST API.
type Client struct {
	baseURL  string
	token    string
	client   *http.Client
	maxBytes int64
}

// NewClient creates a Home Assistant REST API client.
func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("homeassistant: base_url is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("homeassistant: invalid base_url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("homeassistant: base_url scheme must be http or https")
	}

	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("homeassistant: token is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}

	return &Client{
		baseURL:  baseURL,
		token:    token,
		client:   client,
		maxBytes: maxBytes,
	}, nil
}

// ListStates returns all entity states (GET /api/states).
func (c *Client) ListStates(ctx context.Context) (json.RawMessage, error) {
	return c.doJSON(ctx, http.MethodGet, c.baseURL+"/api/states", nil)
}

// GetState returns a single entity state (GET /api/states/{entity_id}).
func (c *Client) GetState(ctx context.Context, entityID string) (json.RawMessage, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, fmt.Errorf("homeassistant: entity_id is required")
	}
	return c.doJSON(ctx, http.MethodGet, c.baseURL+"/api/states/"+url.PathEscape(entityID), nil)
}

// CallService calls a Home Assistant service (POST /api/services/{domain}/{service}).
func (c *Client) CallService(ctx context.Context, domain, service string, data map[string]any) (json.RawMessage, error) {
	domain = strings.TrimSpace(domain)
	service = strings.TrimSpace(service)
	if domain == "" || service == "" {
		return nil, fmt.Errorf("homeassistant: domain and service are required")
	}
	var body io.Reader
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("homeassistant: encode service_data: %w", err)
		}
		body = bytes.NewReader(encoded)
	} else {
		body = bytes.NewReader([]byte(`{}`))
	}
	return c.doJSON(ctx, http.MethodPost, c.baseURL+"/api/services/"+url.PathEscape(domain)+"/"+url.PathEscape(service), body)
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, body io.Reader) (json.RawMessage, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("homeassistant: client not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("homeassistant: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("homeassistant: request failed: %w", err)
	}
	defer resp.Body.Close()

	limit := c.maxBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("homeassistant: read response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("homeassistant: response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("homeassistant: %s", msg)
	}

	return json.RawMessage(data), nil
}
