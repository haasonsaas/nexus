package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	edgepb "github.com/haasonsaas/nexus/pkg/proto/edge"
)

func TestNewClient(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		client := NewClient(ClientConfig{
			GatewayAddr: "localhost:50051",
			EdgeID:      "test-edge",
			EdgeName:    "Test Edge",
		}, nil)

		if client.config.HeartbeatInterval != 30*time.Second {
			t.Errorf("expected default heartbeat interval 30s, got %v", client.config.HeartbeatInterval)
		}
		if client.config.ReconnectDelay != 5*time.Second {
			t.Errorf("expected default reconnect delay 5s, got %v", client.config.ReconnectDelay)
		}
		if client.config.MaxConcurrentExecutions != 10 {
			t.Errorf("expected default max concurrent 10, got %d", client.config.MaxConcurrentExecutions)
		}
	})

	t.Run("custom config values", func(t *testing.T) {
		client := NewClient(ClientConfig{
			GatewayAddr:             "localhost:9999",
			EdgeID:                  "custom-edge",
			EdgeName:                "Custom Edge",
			HeartbeatInterval:       60 * time.Second,
			ReconnectDelay:          10 * time.Second,
			MaxConcurrentExecutions: 20,
		}, nil)

		if client.config.HeartbeatInterval != 60*time.Second {
			t.Errorf("expected custom heartbeat interval 60s, got %v", client.config.HeartbeatInterval)
		}
		if client.config.ReconnectDelay != 10*time.Second {
			t.Errorf("expected custom reconnect delay 10s, got %v", client.config.ReconnectDelay)
		}
		if client.config.MaxConcurrentExecutions != 20 {
			t.Errorf("expected custom max concurrent 20, got %d", client.config.MaxConcurrentExecutions)
		}
	})

	t.Run("shared secret auth", func(t *testing.T) {
		client := NewClient(ClientConfig{
			GatewayAddr:  "localhost:50051",
			EdgeID:       "secret-edge",
			AuthMethod:   edgepb.AuthMethod_AUTH_METHOD_SHARED_SECRET,
			SharedSecret: "my-secret",
		}, nil)

		if client.config.AuthMethod != edgepb.AuthMethod_AUTH_METHOD_SHARED_SECRET {
			t.Errorf("expected shared secret auth, got %v", client.config.AuthMethod)
		}
		if client.config.SharedSecret != "my-secret" {
			t.Errorf("expected shared secret 'my-secret', got %s", client.config.SharedSecret)
		}
	})

	t.Run("TOFU auth", func(t *testing.T) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		client := NewClient(ClientConfig{
			GatewayAddr: "localhost:50051",
			EdgeID:      "tofu-edge",
			AuthMethod:  edgepb.AuthMethod_AUTH_METHOD_TOFU,
			PrivateKey:  priv,
		}, nil)

		if client.config.AuthMethod != edgepb.AuthMethod_AUTH_METHOD_TOFU {
			t.Errorf("expected TOFU auth, got %v", client.config.AuthMethod)
		}
		if client.config.PrivateKey == nil {
			t.Error("expected private key to be set")
		}
	})
}

func TestRegisterTool(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr: "localhost:50051",
		EdgeID:      "test-edge",
	}, nil)

	tool := &Tool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Category:    edgepb.ToolCategory_TOOL_CATEGORY_CUSTOM,
		RiskLevel:   edgepb.RiskLevel_RISK_LEVEL_LOW,
	}

	called := false
	handler := func(ctx context.Context, req *ToolExecutionRequest) (*ClientToolResult, error) {
		called = true
		return &ClientToolResult{Success: true}, nil
	}

	client.RegisterTool(tool, handler)

	// Verify tool was registered
	client.mu.RLock()
	registeredTool, ok := client.tools["test_tool"]
	client.mu.RUnlock()

	if !ok {
		t.Fatal("tool was not registered")
	}
	if registeredTool.Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got %s", registeredTool.Name)
	}
	if registeredTool.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got %s", registeredTool.Description)
	}

	// Verify handler was registered
	client.mu.RLock()
	h, ok := client.handlers["test_tool"]
	client.mu.RUnlock()

	if !ok {
		t.Fatal("handler was not registered")
	}

	// Call handler
	_, err := h(context.Background(), &ToolExecutionRequest{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestRegisterMultipleTools(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr: "localhost:50051",
		EdgeID:      "test-edge",
	}, nil)

	tools := []*Tool{
		{Name: "tool1", Description: "First tool"},
		{Name: "tool2", Description: "Second tool"},
		{Name: "tool3", Description: "Third tool"},
	}

	for _, tool := range tools {
		client.RegisterTool(tool, func(ctx context.Context, req *ToolExecutionRequest) (*ClientToolResult, error) {
			return &ClientToolResult{Success: true}, nil
		})
	}

	client.mu.RLock()
	count := len(client.tools)
	client.mu.RUnlock()

	if count != 3 {
		t.Errorf("expected 3 tools registered, got %d", count)
	}
}

func TestIsConnected(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr: "localhost:50051",
		EdgeID:      "test-edge",
	}, nil)

	// Initially not connected
	if client.IsConnected() {
		t.Error("expected not connected initially")
	}

	// Manually set connected
	client.mu.Lock()
	client.connected = true
	client.mu.Unlock()

	if !client.IsConnected() {
		t.Error("expected connected after setting flag")
	}

	// Manually set disconnected
	client.mu.Lock()
	client.connected = false
	client.mu.Unlock()

	if client.IsConnected() {
		t.Error("expected not connected after clearing flag")
	}
}

func TestSetMetadata(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr: "localhost:50051",
		EdgeID:      "test-edge-123",
	}, nil)

	client.mu.Lock()
	client.sessionToken = "session-abc-456"
	client.mu.Unlock()

	ctx := client.SetMetadata(context.Background())

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in context")
	}

	edgeID := md.Get("x-edge-id")
	if len(edgeID) == 0 || edgeID[0] != "test-edge-123" {
		t.Errorf("expected edge ID 'test-edge-123', got %v", edgeID)
	}

	sessionToken := md.Get("x-session-token")
	if len(sessionToken) == 0 || sessionToken[0] != "session-abc-456" {
		t.Errorf("expected session token 'session-abc-456', got %v", sessionToken)
	}
}

func TestToolConversion(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr: "localhost:50051",
		EdgeID:      "test-edge",
	}, nil)

	tool := &Tool{
		Name:              "complex_tool",
		Description:       "A complex tool",
		InputSchema:       json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`),
		Category:          edgepb.ToolCategory_TOOL_CATEGORY_SYSTEM,
		RequiresApproval:  true,
		RiskLevel:         edgepb.RiskLevel_RISK_LEVEL_HIGH,
		SupportsStreaming: true,
		Metadata:          map[string]string{"version": "1.0"},
	}

	client.RegisterTool(tool, nil)

	client.mu.RLock()
	registeredTool := client.tools["complex_tool"]
	client.mu.RUnlock()

	if registeredTool.Category != edgepb.ToolCategory_TOOL_CATEGORY_SYSTEM {
		t.Errorf("expected category SYSTEM, got %v", registeredTool.Category)
	}
	if !registeredTool.RequiresApproval {
		t.Error("expected RequiresApproval to be true")
	}
	if registeredTool.RiskLevel != edgepb.RiskLevel_RISK_LEVEL_HIGH {
		t.Errorf("expected risk level HIGH, got %v", registeredTool.RiskLevel)
	}
	if !registeredTool.SupportsStreaming {
		t.Error("expected SupportsStreaming to be true")
	}
	if registeredTool.Metadata["version"] != "1.0" {
		t.Errorf("expected metadata version '1.0', got %s", registeredTool.Metadata["version"])
	}
}

func TestToolExecutionRequest(t *testing.T) {
	req := &ToolExecutionRequest{
		RequestID: "req-123",
		ToolName:  "test_tool",
		Input:     json.RawMessage(`{"key": "value"}`),
		SessionID: "session-1",
		UserID:    "user-1",
		AgentID:   "agent-1",
		MessageID: "msg-1",
		Metadata:  map[string]string{"source": "test"},
		Timeout:   30 * time.Second,
	}

	if req.RequestID != "req-123" {
		t.Errorf("expected request ID 'req-123', got %s", req.RequestID)
	}
	if req.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", req.Timeout)
	}
	if req.Metadata["source"] != "test" {
		t.Errorf("expected metadata source 'test', got %s", req.Metadata["source"])
	}
}

func TestClientToolResult(t *testing.T) {
	result := &ClientToolResult{
		Success:      true,
		Output:       map[string]string{"result": "ok"},
		ErrorMessage: "",
		DurationMS:   100,
	}

	if !result.Success {
		t.Error("expected success to be true")
	}
	if result.DurationMS != 100 {
		t.Errorf("expected duration 100ms, got %d", result.DurationMS)
	}

	// Test with error
	errorResult := &ClientToolResult{
		Success:      false,
		ErrorMessage: "something went wrong",
		DurationMS:   50,
	}

	if errorResult.Success {
		t.Error("expected success to be false")
	}
	if errorResult.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %s", errorResult.ErrorMessage)
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		result := min(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("min(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestToolOverwrite(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr: "localhost:50051",
		EdgeID:      "test-edge",
	}, nil)

	// Register initial tool
	client.RegisterTool(&Tool{
		Name:        "my_tool",
		Description: "Original description",
	}, func(ctx context.Context, req *ToolExecutionRequest) (*ClientToolResult, error) {
		return &ClientToolResult{Output: "v1"}, nil
	})

	// Overwrite with new tool
	client.RegisterTool(&Tool{
		Name:        "my_tool",
		Description: "Updated description",
	}, func(ctx context.Context, req *ToolExecutionRequest) (*ClientToolResult, error) {
		return &ClientToolResult{Output: "v2"}, nil
	})

	client.mu.RLock()
	tool := client.tools["my_tool"]
	handler := client.handlers["my_tool"]
	client.mu.RUnlock()

	if tool.Description != "Updated description" {
		t.Errorf("expected 'Updated description', got %s", tool.Description)
	}

	result, _ := handler(context.Background(), &ToolExecutionRequest{})
	if result.Output != "v2" {
		t.Errorf("expected output 'v2', got %v", result.Output)
	}
}

func TestClientChannels(t *testing.T) {
	client := NewClient(ClientConfig{
		GatewayAddr:             "localhost:50051",
		EdgeID:                  "test-edge",
		MaxConcurrentExecutions: 5,
	}, nil)

	// Verify channels are created
	if client.done == nil {
		t.Error("expected done channel to be created")
	}
	if client.requests == nil {
		t.Error("expected requests channel to be created")
	}

	// Verify request channel buffer size
	if cap(client.requests) != 5 {
		t.Errorf("expected request channel capacity 5, got %d", cap(client.requests))
	}
}
