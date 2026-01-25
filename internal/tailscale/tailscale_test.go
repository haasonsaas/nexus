package tailscale

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient()

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, client.Timeout)
	}
}

func TestNewClientWithPath(t *testing.T) {
	path := "/custom/path/tailscale"
	client := NewClientWithPath(path)

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.Path != path {
		t.Errorf("expected path %s, got %s", path, client.Path)
	}

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, client.Timeout)
	}
}

func TestClient_IsAvailable_NoBinary(t *testing.T) {
	client := &Client{
		Path:    "",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	if client.IsAvailable(ctx) {
		t.Error("expected IsAvailable to return false with empty path")
	}
}

func TestClient_IsAvailable_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/binary",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	if client.IsAvailable(ctx) {
		t.Error("expected IsAvailable to return false with invalid binary")
	}
}

func TestParsePossiblyNoisyJSON_CleanJSON(t *testing.T) {
	input := []byte(`{"key": "value"}`)
	output, err := parsePossiblyNoisyJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"key": "value"}`
	if string(output) != expected {
		t.Errorf("expected %s, got %s", expected, string(output))
	}
}

func TestParsePossiblyNoisyJSON_NoisyPrefix(t *testing.T) {
	input := []byte(`some noise before {"key": "value"}`)
	output, err := parsePossiblyNoisyJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"key": "value"}`
	if string(output) != expected {
		t.Errorf("expected %s, got %s", expected, string(output))
	}
}

func TestParsePossiblyNoisyJSON_NoisySuffix(t *testing.T) {
	input := []byte(`{"key": "value"} some noise after`)
	output, err := parsePossiblyNoisyJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"key": "value"}`
	if string(output) != expected {
		t.Errorf("expected %s, got %s", expected, string(output))
	}
}

func TestParsePossiblyNoisyJSON_Array(t *testing.T) {
	input := []byte(`prefix [1, 2, 3] suffix`)
	output, err := parsePossiblyNoisyJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `[1, 2, 3]`
	if string(output) != expected {
		t.Errorf("expected %s, got %s", expected, string(output))
	}
}

func TestParsePossiblyNoisyJSON_NestedJSON(t *testing.T) {
	input := []byte(`{"outer": {"inner": "value"}}`)
	output, err := parsePossiblyNoisyJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"outer": {"inner": "value"}}`
	if string(output) != expected {
		t.Errorf("expected %s, got %s", expected, string(output))
	}
}

func TestParsePingLatency_Milliseconds(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{
			input:    "pong from hostname (100.64.0.1) via DERP(nyc) in 42ms",
			expected: 42 * time.Millisecond,
		},
		{
			input:    "pong from hostname (100.64.0.1) via direct in 1.5ms",
			expected: 1500 * time.Microsecond,
		},
		{
			input:    "pong from host (100.64.0.1) in 100ms",
			expected: 100 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parsePingLatency(tc.input)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestParsePingLatency_Seconds(t *testing.T) {
	input := "pong from hostname (100.64.0.1) in 1.5s"
	expected := 1500 * time.Millisecond

	result := parsePingLatency(input)
	if result != expected {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestParsePingLatency_NoMatch(t *testing.T) {
	input := "no latency info here"
	result := parsePingLatency(input)
	if result != 0 {
		t.Errorf("expected 0, got %v", result)
	}
}

func TestFindTailscalePath_LooksInCommonLocations(t *testing.T) {
	// This test just verifies the function runs without panicking
	// The actual result depends on the system configuration
	path := FindTailscalePath()
	// Path may be empty if tailscale is not installed
	_ = path
}

func TestTailscaleStatus_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"BackendState": "Running",
		"Self": {
			"ID": "n123",
			"PublicKey": "pubkey123",
			"HostName": "myhost",
			"DNSName": "myhost.tailnet.ts.net.",
			"OS": "linux",
			"TailscaleIPs": ["100.64.0.1"],
			"Online": true
		},
		"Peer": {
			"n456": {
				"ID": "n456",
				"PublicKey": "peerkey",
				"HostName": "peerhost",
				"DNSName": "peerhost.tailnet.ts.net.",
				"OS": "darwin",
				"TailscaleIPs": ["100.64.0.2"],
				"Online": true,
				"LastSeen": "2024-01-01T00:00:00Z"
			}
		},
		"CurrentTailnet": {
			"Name": "tailnet.ts.net",
			"MagicDNSSuffix": "tailnet.ts.net",
			"MagicDNSEnabled": true
		}
	}`

	parsed, err := parsePossiblyNoisyJSON([]byte(jsonData))
	if err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	var status TailscaleStatus
	if err := json.Unmarshal(parsed, &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if status.BackendState != "Running" {
		t.Errorf("expected BackendState 'Running', got %s", status.BackendState)
	}

	if status.Self == nil {
		t.Fatal("expected Self to be non-nil")
	}

	if status.Self.HostName != "myhost" {
		t.Errorf("expected HostName 'myhost', got %s", status.Self.HostName)
	}

	if len(status.Self.TailscaleIPs) != 1 || status.Self.TailscaleIPs[0] != "100.64.0.1" {
		t.Errorf("unexpected TailscaleIPs: %v", status.Self.TailscaleIPs)
	}

	if len(status.Peer) != 1 {
		t.Errorf("expected 1 peer, got %d", len(status.Peer))
	}

	peer, ok := status.Peer["n456"]
	if !ok {
		t.Fatal("expected peer n456")
	}

	if peer.HostName != "peerhost" {
		t.Errorf("expected peer HostName 'peerhost', got %s", peer.HostName)
	}

	if status.CurrentTailnet == nil {
		t.Fatal("expected CurrentTailnet to be non-nil")
	}

	if status.CurrentTailnet.Name != "tailnet.ts.net" {
		t.Errorf("expected tailnet name 'tailnet.ts.net', got %s", status.CurrentTailnet.Name)
	}
}

func TestServeConfig_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"TCP": {
			"443": {"HTTPS": true}
		},
		"Web": {
			"myhost.tailnet.ts.net": {
				"Handlers": {
					"/": {"Proxy": "http://localhost:8080"}
				}
			}
		}
	}`

	var config ServeConfig
	if err := json.Unmarshal([]byte(jsonData), &config); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if len(config.TCP) != 1 {
		t.Errorf("expected 1 TCP entry, got %d", len(config.TCP))
	}

	if len(config.Web) != 1 {
		t.Errorf("expected 1 Web entry, got %d", len(config.Web))
	}

	web, ok := config.Web["myhost.tailnet.ts.net"]
	if !ok {
		t.Fatal("expected web config for myhost.tailnet.ts.net")
	}

	if web.Handlers == nil {
		t.Fatal("expected Handlers to be non-nil")
	}

	handler, ok := web.Handlers["/"]
	if !ok {
		t.Fatal("expected handler for /")
	}

	if handler.Proxy != "http://localhost:8080" {
		t.Errorf("expected proxy 'http://localhost:8080', got %s", handler.Proxy)
	}
}

func TestPeer_Fields(t *testing.T) {
	peer := Peer{
		ID:           "n123",
		PublicKey:    "pubkey",
		HostName:     "host",
		DNSName:      "host.tailnet.ts.net.",
		OS:           "linux",
		TailscaleIPs: []string{"100.64.0.1", "fd7a::1"},
		Online:       true,
		LastSeen:     "2024-01-01T00:00:00Z",
	}

	if peer.ID != "n123" {
		t.Errorf("unexpected ID: %s", peer.ID)
	}
	if !peer.Online {
		t.Error("expected Online to be true")
	}
	if len(peer.TailscaleIPs) != 2 {
		t.Errorf("expected 2 IPs, got %d", len(peer.TailscaleIPs))
	}
}

func TestFunnelStatus_Fields(t *testing.T) {
	status := FunnelStatus{
		AllowFunnel: true,
		Services: map[string]string{
			"443": "https",
		},
	}

	if !status.AllowFunnel {
		t.Error("expected AllowFunnel to be true")
	}
	if len(status.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(status.Services))
	}
}

func TestTailnetInfo_Fields(t *testing.T) {
	info := TailnetInfo{
		Name:            "example.ts.net",
		MagicDNSSuffix:  "example.ts.net",
		MagicDNSEnabled: true,
	}

	if info.Name != "example.ts.net" {
		t.Errorf("unexpected Name: %s", info.Name)
	}
	if !info.MagicDNSEnabled {
		t.Error("expected MagicDNSEnabled to be true")
	}
}

func TestClient_runCommand_EmptyPath(t *testing.T) {
	client := &Client{
		Path:    "",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.runCommand(ctx, "status")
	if err == nil {
		t.Error("expected error with empty path")
	}
}

func TestClient_Status_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.Status(ctx)
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_IsConnected_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	if client.IsConnected(ctx) {
		t.Error("expected IsConnected to return false with invalid binary")
	}
}

func TestClient_GetSelfDNSName_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.GetSelfDNSName(ctx)
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_GetSelfIP_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.GetSelfIP(ctx)
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestDefaultTimeout(t *testing.T) {
	if DefaultTimeout != 10*time.Second {
		t.Errorf("expected DefaultTimeout to be 10s, got %v", DefaultTimeout)
	}
}

func TestClient_FindPeerByHostname_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.FindPeerByHostname(ctx, "somehost")
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_FindPeerByIP_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.FindPeerByIP(ctx, "100.64.0.1")
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_Ping_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.Ping(ctx, "somehost")
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_ListPeers_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.ListPeers(ctx)
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_GetTailnet_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.GetTailnet(ctx)
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestTCPServe_Fields(t *testing.T) {
	serve := TCPServe{
		HTTPS: true,
	}

	if !serve.HTTPS {
		t.Error("expected HTTPS to be true")
	}
}

func TestWebServe_Fields(t *testing.T) {
	serve := WebServe{
		Handlers: map[string]*WebHandler{
			"/": {Proxy: "http://localhost:8080"},
		},
	}

	if serve.Handlers == nil {
		t.Fatal("expected Handlers to be non-nil")
	}

	handler, ok := serve.Handlers["/"]
	if !ok {
		t.Fatal("expected handler for /")
	}

	if handler.Proxy != "http://localhost:8080" {
		t.Errorf("expected proxy 'http://localhost:8080', got %s", handler.Proxy)
	}
}

func TestWebHandler_Fields(t *testing.T) {
	handler := WebHandler{
		Proxy: "http://localhost:8080",
		Path:  "/api",
	}

	if handler.Proxy != "http://localhost:8080" {
		t.Errorf("expected proxy 'http://localhost:8080', got %s", handler.Proxy)
	}

	if handler.Path != "/api" {
		t.Errorf("expected path '/api', got %s", handler.Path)
	}
}

func TestTailscaleSelf_Fields(t *testing.T) {
	self := TailscaleSelf{
		ID:           "n123",
		PublicKey:    "pubkey",
		HostName:     "myhost",
		DNSName:      "myhost.tailnet.ts.net.",
		OS:           "linux",
		TailscaleIPs: []string{"100.64.0.1"},
		Online:       true,
	}

	if self.ID != "n123" {
		t.Errorf("unexpected ID: %s", self.ID)
	}

	if self.HostName != "myhost" {
		t.Errorf("unexpected HostName: %s", self.HostName)
	}

	if !self.Online {
		t.Error("expected Online to be true")
	}
}

func TestClient_GetPublicURL(t *testing.T) {
	// This test would need a mock, but we can at least test error handling
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	_, err := client.GetPublicURL(ctx, 443)
	if err == nil {
		t.Error("expected error with invalid binary")
	}
}

func TestClient_IsFunnelEnabled_InvalidBinary(t *testing.T) {
	client := &Client{
		Path:    "/nonexistent/tailscale",
		Timeout: DefaultTimeout,
	}

	ctx := context.Background()
	enabled, err := client.IsFunnelEnabled(ctx)
	// Should not error but return false
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("expected funnel to not be enabled with invalid binary")
	}
}
