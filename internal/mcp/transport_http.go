package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// HTTPTransport implements the MCP HTTP/SSE transport.
type HTTPTransport struct {
	config *ServerConfig
	logger *slog.Logger
	client *http.Client

	events    chan *JSONRPCNotification
	requests  chan *JSONRPCRequest
	connected atomic.Bool
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// NewHTTPTransport creates a new HTTP transport.
func NewHTTPTransport(cfg *ServerConfig) *HTTPTransport {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTTPTransport{
		config: cfg,
		logger: slog.Default().With("mcp_server", cfg.ID, "transport", "http"),
		client: &http.Client{
			Timeout: timeout,
		},
		events:   make(chan *JSONRPCNotification, 100),
		requests: make(chan *JSONRPCRequest, 100),
		stopChan: make(chan struct{}),
	}
}

// Connect establishes the HTTP connection.
func (t *HTTPTransport) Connect(ctx context.Context) error {
	if t.config.URL == "" {
		return fmt.Errorf("URL is required for HTTP transport")
	}

	// Test connection with a simple request
	// We don't call initialize here - that's done by the client
	t.connected.Store(true)
	t.logger.Info("HTTP transport ready", "url", t.config.URL)

	// Start SSE listener for notifications
	t.wg.Add(1)
	go t.sseLoop(ctx)

	return nil
}

// Close closes the HTTP connection.
func (t *HTTPTransport) Close() error {
	t.connected.Store(false)
	close(t.stopChan)
	t.wg.Wait()
	return nil
}

// Call sends a request and waits for a response.
func (t *HTTPTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !t.connected.Load() {
		return nil, fmt.Errorf("not connected")
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = paramsJSON
	}

	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.config.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.config.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Notify sends a notification (no response expected).
func (t *HTTPTransport) Notify(ctx context.Context, method string, params any) error {
	if !t.connected.Load() {
		return fmt.Errorf("not connected")
	}

	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		notif.Params = paramsJSON
	}

	body, _ := json.Marshal(notif)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.config.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	resp.Body.Close()

	return nil
}

// Events returns the notification channel.
func (t *HTTPTransport) Events() <-chan *JSONRPCNotification {
	return t.events
}

// Requests returns the request channel.
func (t *HTTPTransport) Requests() <-chan *JSONRPCRequest {
	return t.requests
}

// Respond sends a response to a server request.
func (t *HTTPTransport) Respond(ctx context.Context, id any, result any, rpcErr *JSONRPCError) error {
	if !t.connected.Load() {
		return fmt.Errorf("not connected")
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcErr,
	}
	if rpcErr == nil && result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("marshal result: %w", err)
		}
		resp.Result = data
	}
	body, _ := json.Marshal(resp)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.config.Headers {
		httpReq.Header.Set(k, v)
	}

	respHTTP, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	respHTTP.Body.Close()
	return nil
}

// Connected returns whether the transport is connected.
func (t *HTTPTransport) Connected() bool {
	return t.connected.Load()
}

// sseLoop listens for Server-Sent Events.
func (t *HTTPTransport) sseLoop(ctx context.Context) {
	defer t.wg.Done()

	// Build SSE endpoint URL
	sseURL := strings.TrimSuffix(t.config.URL, "/") + "/sse"

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopChan:
			return
		default:
		}

		t.connectSSE(ctx, sseURL)

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-t.stopChan:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// connectSSE establishes an SSE connection.
func (t *HTTPTransport) connectSSE(ctx context.Context, sseURL string) {
	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		t.logger.Debug("failed to create SSE request", "error", err)
		return
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		t.logger.Debug("SSE connection failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.logger.Debug("SSE returned non-200", "status", resp.StatusCode)
		return
	}

	t.logger.Debug("SSE connected", "url", sseURL)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case <-t.stopChan:
			return
		default:
		}

		line := scanner.Text()

		// Parse SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var envelope struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      any             `json:"id"`
				Method  string          `json:"method"`
				Params  json.RawMessage `json:"params,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &envelope); err != nil {
				continue
			}
			if envelope.Method == "" {
				continue
			}
			if envelope.ID != nil {
				req := &JSONRPCRequest{
					JSONRPC: envelope.JSONRPC,
					ID:      envelope.ID,
					Method:  envelope.Method,
					Params:  envelope.Params,
				}
				select {
				case t.requests <- req:
				default:
					t.logger.Warn("request channel full, dropping")
				}
				continue
			}
			notif := &JSONRPCNotification{
				JSONRPC: envelope.JSONRPC,
				Method:  envelope.Method,
				Params:  envelope.Params,
			}
			select {
			case t.events <- notif:
			default:
				t.logger.Warn("notification channel full, dropping")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.logger.Debug("SSE scanner error", "error", err)
	}
}
