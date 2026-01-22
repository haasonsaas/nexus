package mcp

import (
	"testing"
	"time"
)

func TestNewTransportStdio(t *testing.T) {
	cfg := &ServerConfig{
		ID:        "test",
		Transport: TransportStdio,
		Command:   "echo",
	}

	transport := NewTransport(cfg)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}

	_, ok := transport.(*StdioTransport)
	if !ok {
		t.Error("expected StdioTransport")
	}
}

func TestNewTransportHTTP(t *testing.T) {
	cfg := &ServerConfig{
		ID:        "test",
		Transport: TransportHTTP,
		URL:       "https://example.com/mcp",
	}

	transport := NewTransport(cfg)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}

	_, ok := transport.(*HTTPTransport)
	if !ok {
		t.Error("expected HTTPTransport")
	}
}

func TestNewTransportDefault(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
		// No transport type specified, should default to stdio
	}

	transport := NewTransport(cfg)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}

	_, ok := transport.(*StdioTransport)
	if !ok {
		t.Error("expected StdioTransport as default")
	}
}

func TestNewStdioTransport(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test-stdio",
		Command: "mcp-server",
		Args:    []string{"--config", "test.yaml"},
		Env:     map[string]string{"DEBUG": "true"},
		WorkDir: "/tmp",
		Timeout: 30 * time.Second,
	}

	transport := NewStdioTransport(cfg)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}

	if transport.config != cfg {
		t.Error("expected config to be set")
	}
	if transport.pending == nil {
		t.Error("expected pending map to be initialized")
	}
	if transport.events == nil {
		t.Error("expected events channel to be initialized")
	}
	if transport.requests == nil {
		t.Error("expected requests channel to be initialized")
	}
}

func TestStdioTransportConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
	}

	transport := NewStdioTransport(cfg)

	if transport.Connected() {
		t.Error("expected Connected() to return false before Connect()")
	}
}

func TestStdioTransportEvents(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
	}

	transport := NewStdioTransport(cfg)

	events := transport.Events()
	if events == nil {
		t.Error("expected non-nil events channel")
	}
}

func TestStdioTransportRequests(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
	}

	transport := NewStdioTransport(cfg)

	requests := transport.Requests()
	if requests == nil {
		t.Error("expected non-nil requests channel")
	}
}

func TestNewHTTPTransport(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test-http",
		URL:     "https://mcp.example.com/api",
		Headers: map[string]string{"Authorization": "Bearer token"},
		Timeout: 60 * time.Second,
	}

	transport := NewHTTPTransport(cfg)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}

	if transport.config != cfg {
		t.Error("expected config to be set")
	}
	if transport.events == nil {
		t.Error("expected events channel to be initialized")
	}
	if transport.requests == nil {
		t.Error("expected requests channel to be initialized")
	}
}

func TestHTTPTransportDefaultTimeout(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
		// No timeout specified
	}

	transport := NewHTTPTransport(cfg)

	if transport.client.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", transport.client.Timeout)
	}
}

func TestHTTPTransportCustomTimeout(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		URL:     "https://mcp.example.com",
		Timeout: 60 * time.Second,
	}

	transport := NewHTTPTransport(cfg)

	if transport.client.Timeout != 60*time.Second {
		t.Errorf("expected timeout 60s, got %v", transport.client.Timeout)
	}
}

func TestHTTPTransportConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
	}

	transport := NewHTTPTransport(cfg)

	if transport.Connected() {
		t.Error("expected Connected() to return false before Connect()")
	}
}

func TestHTTPTransportEvents(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
	}

	transport := NewHTTPTransport(cfg)

	events := transport.Events()
	if events == nil {
		t.Error("expected non-nil events channel")
	}
}

func TestHTTPTransportRequests(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
	}

	transport := NewHTTPTransport(cfg)

	requests := transport.Requests()
	if requests == nil {
		t.Error("expected non-nil requests channel")
	}
}

func TestStdioTransportConnectNoCommand(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "", // No command
	}

	transport := NewStdioTransport(cfg)

	err := transport.Connect(nil)
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestHTTPTransportConnectNoURL(t *testing.T) {
	cfg := &ServerConfig{
		ID:        "test",
		Transport: TransportHTTP,
		URL:       "", // No URL
	}

	transport := NewHTTPTransport(cfg)

	err := transport.Connect(nil)
	if err == nil {
		t.Error("expected error for missing URL")
	}
}

func TestStdioTransportCallNotConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
	}

	transport := NewStdioTransport(cfg)

	_, err := transport.Call(nil, "test", nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestHTTPTransportCallNotConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
	}

	transport := NewHTTPTransport(cfg)

	_, err := transport.Call(nil, "test", nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestStdioTransportNotifyNotConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
	}

	transport := NewStdioTransport(cfg)

	err := transport.Notify(nil, "test", nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestHTTPTransportNotifyNotConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
	}

	transport := NewHTTPTransport(cfg)

	err := transport.Notify(nil, "test", nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestStdioTransportRespondNotConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:      "test",
		Command: "echo",
	}

	transport := NewStdioTransport(cfg)

	err := transport.Respond(nil, 1, nil, nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestHTTPTransportRespondNotConnected(t *testing.T) {
	cfg := &ServerConfig{
		ID:  "test",
		URL: "https://mcp.example.com",
	}

	transport := NewHTTPTransport(cfg)

	err := transport.Respond(nil, 1, nil, nil)
	if err == nil {
		t.Error("expected error when not connected")
	}
}
