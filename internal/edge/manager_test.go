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

func TestManagerSetChannelHandler(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	// Initially no handler
	if manager.channelHandler != nil {
		t.Error("expected nil channel handler initially")
	}

	// Set a handler
	var handlerCalled bool
	manager.SetChannelHandler(func(ctx context.Context, msg *pb.EdgeChannelInbound) error {
		handlerCalled = true
		return nil
	})

	if manager.channelHandler == nil {
		t.Error("expected channel handler to be set")
	}

	// Call it to verify it's the right handler
	_ = manager.channelHandler(context.Background(), &pb.EdgeChannelInbound{})
	if !handlerCalled {
		t.Error("expected handler to be called")
	}
}

func TestManagerGetEdgesWithChannel(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	// Add some mock edges with channel types
	manager.mu.Lock()
	manager.edges["edge1"] = &EdgeConnection{
		ID:           "edge1",
		ChannelTypes: []string{"imessage", "signal"},
	}
	manager.edges["edge2"] = &EdgeConnection{
		ID:           "edge2",
		ChannelTypes: []string{"whatsapp"},
	}
	manager.edges["edge3"] = &EdgeConnection{
		ID:           "edge3",
		ChannelTypes: []string{"imessage"},
	}
	manager.mu.Unlock()

	// Query for imessage
	edges := manager.GetEdgesWithChannel("imessage")
	if len(edges) != 2 {
		t.Errorf("expected 2 edges with imessage, got %d", len(edges))
	}

	// Query for whatsapp
	edges = manager.GetEdgesWithChannel("whatsapp")
	if len(edges) != 1 {
		t.Errorf("expected 1 edge with whatsapp, got %d", len(edges))
	}

	// Query for nonexistent
	edges = manager.GetEdgesWithChannel("telegram")
	if len(edges) != 0 {
		t.Errorf("expected 0 edges with telegram, got %d", len(edges))
	}
}

func TestManagerSelectEdgeLeastBusy(t *testing.T) {
	manager := NewManager(DefaultManagerConfig(), &mockAuthenticator{}, nil)

	manager.mu.Lock()
	manager.edges["edge-fast"] = &EdgeConnection{
		ID:    "edge-fast",
		Tools: map[string]*EdgeTool{"browser.snapshot": {Name: "browser.snapshot"}},
		Metrics: &pb.EdgeMetrics{
			ActiveToolCount: 1,
		},
	}
	manager.edges["edge-busy"] = &EdgeConnection{
		ID:    "edge-busy",
		Tools: map[string]*EdgeTool{"browser.snapshot": {Name: "browser.snapshot"}},
		Metrics: &pb.EdgeMetrics{
			ActiveToolCount: 7,
		},
	}
	manager.mu.Unlock()

	edge, err := manager.SelectEdge(SelectionCriteria{ToolName: "browser.snapshot"})
	if err != nil {
		t.Fatalf("SelectEdge: %v", err)
	}
	if edge.ID != "edge-fast" {
		t.Fatalf("expected edge-fast, got %s", edge.ID)
	}
}

func TestManagerSelectEdgeMetadata(t *testing.T) {
	manager := NewManager(DefaultManagerConfig(), &mockAuthenticator{}, nil)

	manager.mu.Lock()
	manager.edges["edge-us"] = &EdgeConnection{
		ID:       "edge-us",
		Tools:    map[string]*EdgeTool{"browser.snapshot": {Name: "browser.snapshot"}},
		Metadata: map[string]string{"region": "us"},
	}
	manager.edges["edge-eu"] = &EdgeConnection{
		ID:       "edge-eu",
		Tools:    map[string]*EdgeTool{"browser.snapshot": {Name: "browser.snapshot"}},
		Metadata: map[string]string{"region": "eu"},
	}
	manager.mu.Unlock()

	edge, err := manager.SelectEdge(SelectionCriteria{
		ToolName: "browser.snapshot",
		Metadata: map[string]string{"region": "eu"},
	})
	if err != nil {
		t.Fatalf("SelectEdge: %v", err)
	}
	if edge.ID != "edge-eu" {
		t.Fatalf("expected edge-eu, got %s", edge.ID)
	}
}

func TestManagerSelectEdgeRoundRobin(t *testing.T) {
	manager := NewManager(DefaultManagerConfig(), &mockAuthenticator{}, nil)

	manager.mu.Lock()
	manager.edges["edge-1"] = &EdgeConnection{ID: "edge-1"}
	manager.edges["edge-2"] = &EdgeConnection{ID: "edge-2"}
	manager.mu.Unlock()

	first, err := manager.SelectEdge(SelectionCriteria{Strategy: StrategyRoundRobin})
	if err != nil {
		t.Fatalf("SelectEdge: %v", err)
	}
	second, err := manager.SelectEdge(SelectionCriteria{Strategy: StrategyRoundRobin})
	if err != nil {
		t.Fatalf("SelectEdge: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("expected round robin to alternate, got %s twice", first.ID)
	}
}

func TestManagerSendChannelMessageNoEdge(t *testing.T) {
	config := DefaultManagerConfig()
	auth := &mockAuthenticator{}
	manager := NewManager(config, auth, nil)

	ctx := context.Background()
	msg := &pb.CoreChannelOutbound{
		MessageId:   "msg-1",
		SessionId:   "session-1",
		ChannelType: pb.ChannelType_CHANNEL_TYPE_IMESSAGE,
		ChannelId:   "+1234567890",
		Content:     "Hello",
	}

	_, err := manager.SendChannelMessage(ctx, "nonexistent", msg)
	if err == nil {
		t.Error("expected error for nonexistent edge")
	}
}

func TestPendingChannelMessage(t *testing.T) {
	pending := &PendingChannelMessage{
		MessageID:  "msg-1",
		SessionID:  "session-1",
		EdgeID:     "edge-1",
		SentAt:     time.Now(),
		ResultChan: make(chan *pb.EdgeChannelAck, 1),
	}

	if pending.MessageID != "msg-1" {
		t.Errorf("expected MessageID msg-1, got %s", pending.MessageID)
	}
	if pending.SessionID != "session-1" {
		t.Errorf("expected SessionID session-1, got %s", pending.SessionID)
	}
}
