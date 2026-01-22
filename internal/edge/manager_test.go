package edge

import (
	"context"
	"testing"
	"time"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// mockAuthenticator accepts all connections.
type mockAuthenticator struct{}

func (m *mockAuthenticator) Authenticate(ctx context.Context, reg *pb.EdgeRegister) (string, error) {
	return reg.EdgeId, nil
}

func TestNewManager(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}

	if manager.config.HeartbeatInterval != 30*time.Second {
		t.Errorf("expected heartbeat interval 30s, got %v", manager.config.HeartbeatInterval)
	}

	if len(manager.edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(manager.edges))
	}
}

func TestDefaultManagerConfig(t *testing.T) {
	config := DefaultManagerConfig()

	if config.HeartbeatInterval != 30*time.Second {
		t.Errorf("expected HeartbeatInterval 30s, got %v", config.HeartbeatInterval)
	}

	if config.HeartbeatTimeout != 90*time.Second {
		t.Errorf("expected HeartbeatTimeout 90s, got %v", config.HeartbeatTimeout)
	}

	if config.DefaultToolTimeout != 60*time.Second {
		t.Errorf("expected DefaultToolTimeout 60s, got %v", config.DefaultToolTimeout)
	}

	if config.MaxConcurrentTools != 10 {
		t.Errorf("expected MaxConcurrentTools 10, got %d", config.MaxConcurrentTools)
	}

	if config.EventBufferSize != 1000 {
		t.Errorf("expected EventBufferSize 1000, got %d", config.EventBufferSize)
	}
}

func TestManagerListEdgesEmpty(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	edges := manager.ListEdges()
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestManagerGetToolsEmpty(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	tools := manager.GetTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestManagerMetrics(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	metrics := manager.Metrics()
	if metrics.ConnectedEdges != 0 {
		t.Errorf("expected 0 connected edges, got %d", metrics.ConnectedEdges)
	}
	if metrics.TotalToolCalls != 0 {
		t.Errorf("expected 0 total tool calls, got %d", metrics.TotalToolCalls)
	}
}

func TestManagerExecuteToolNoEdge(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	ctx := context.Background()
	_, err := manager.ExecuteTool(ctx, "nonexistent", "test_tool", "{}", ExecuteOptions{})
	if err == nil {
		t.Error("expected error for nonexistent edge")
	}
}

func TestManagerCancelToolNotFound(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	err := manager.CancelTool("nonexistent", "test")
	if err == nil {
		t.Error("expected error for nonexistent execution")
	}
}

func TestManagerGetEdgeNotFound(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	status, ok := manager.GetEdge("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent edge")
	}
	if status != nil {
		t.Error("expected nil status for nonexistent edge")
	}
}

func TestManagerClose(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	err := manager.Close()
	if err != nil {
		t.Errorf("unexpected error from Close: %v", err)
	}
}
