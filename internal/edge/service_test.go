package edge

import (
	"context"
	"encoding/base64"
	"strconv"
	"testing"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

func TestService_ListEdges_EmptyList(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	svc := NewService(manager)

	resp, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{})
	if err != nil {
		t.Fatalf("ListEdges error: %v", err)
	}

	if len(resp.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(resp.Edges))
	}
	if resp.TotalCount != 0 {
		t.Errorf("expected total count 0, got %d", resp.TotalCount)
	}
	if resp.NextPageToken != "" {
		t.Errorf("expected empty next page token, got %q", resp.NextPageToken)
	}
}

func TestService_ListEdges_DefaultPageSize(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	// Add test edges directly to manager
	for i := 0; i < 10; i++ {
		manager.mu.Lock()
		manager.edges["edge-"+strconv.Itoa(i)] = &EdgeConnection{
			ID:     "edge-" + strconv.Itoa(i),
			Name:   "Test Edge " + strconv.Itoa(i),
			Status: pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED,
		}
		manager.mu.Unlock()
	}

	svc := NewService(manager)

	resp, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{})
	if err != nil {
		t.Fatalf("ListEdges error: %v", err)
	}

	if len(resp.Edges) != 10 {
		t.Errorf("expected 10 edges, got %d", len(resp.Edges))
	}
	if resp.TotalCount != 10 {
		t.Errorf("expected total count 10, got %d", resp.TotalCount)
	}
	if resp.NextPageToken != "" {
		t.Errorf("expected empty next page token when all results fit, got %q", resp.NextPageToken)
	}
}

func TestService_ListEdges_Pagination(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	// Add 25 test edges
	for i := 0; i < 25; i++ {
		manager.mu.Lock()
		manager.edges["edge-"+strconv.Itoa(i)] = &EdgeConnection{
			ID:     "edge-" + strconv.Itoa(i),
			Name:   "Test Edge " + strconv.Itoa(i),
			Status: pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED,
		}
		manager.mu.Unlock()
	}

	svc := NewService(manager)

	// First page - request 10 items
	resp1, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListEdges page 1 error: %v", err)
	}

	if len(resp1.Edges) != 10 {
		t.Errorf("page 1: expected 10 edges, got %d", len(resp1.Edges))
	}
	if resp1.TotalCount != 25 {
		t.Errorf("page 1: expected total count 25, got %d", resp1.TotalCount)
	}
	if resp1.NextPageToken == "" {
		t.Error("page 1: expected non-empty next page token")
	}

	// Second page
	resp2, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{
		PageSize:  10,
		PageToken: resp1.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListEdges page 2 error: %v", err)
	}

	if len(resp2.Edges) != 10 {
		t.Errorf("page 2: expected 10 edges, got %d", len(resp2.Edges))
	}
	if resp2.NextPageToken == "" {
		t.Error("page 2: expected non-empty next page token")
	}

	// Third page (last, partial)
	resp3, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{
		PageSize:  10,
		PageToken: resp2.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListEdges page 3 error: %v", err)
	}

	if len(resp3.Edges) != 5 {
		t.Errorf("page 3: expected 5 edges, got %d", len(resp3.Edges))
	}
	if resp3.NextPageToken != "" {
		t.Errorf("page 3: expected empty next page token, got %q", resp3.NextPageToken)
	}
}

func TestService_ListEdges_InvalidPageToken(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	for i := 0; i < 5; i++ {
		manager.mu.Lock()
		manager.edges["edge-"+strconv.Itoa(i)] = &EdgeConnection{
			ID:     "edge-" + strconv.Itoa(i),
			Status: pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED,
		}
		manager.mu.Unlock()
	}

	svc := NewService(manager)

	// Invalid page token should be treated as start of list
	resp, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{
		PageToken: "invalid-token",
	})
	if err != nil {
		t.Fatalf("ListEdges error: %v", err)
	}

	// Should return all edges since invalid token is treated as offset 0
	if len(resp.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(resp.Edges))
	}
}

func TestService_ListEdges_OffsetBeyondLength(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	for i := 0; i < 5; i++ {
		manager.mu.Lock()
		manager.edges["edge-"+strconv.Itoa(i)] = &EdgeConnection{
			ID:     "edge-" + strconv.Itoa(i),
			Status: pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED,
		}
		manager.mu.Unlock()
	}

	svc := NewService(manager)

	// Create page token with offset beyond list length
	token := base64.StdEncoding.EncodeToString([]byte("1000"))

	resp, err := svc.ListEdges(context.Background(), &pb.ListEdgesRequest{
		PageToken: token,
	})
	if err != nil {
		t.Fatalf("ListEdges error: %v", err)
	}

	if len(resp.Edges) != 0 {
		t.Errorf("expected 0 edges when offset beyond list, got %d", len(resp.Edges))
	}
	if resp.TotalCount != 5 {
		t.Errorf("expected total count 5, got %d", resp.TotalCount)
	}
}

func TestService_GetEdgeStatus_NotFound(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	svc := NewService(manager)

	resp, err := svc.GetEdgeStatus(context.Background(), &pb.GetEdgeStatusRequest{
		EdgeId: "nonexistent",
	})
	if err != nil {
		t.Fatalf("GetEdgeStatus error: %v", err)
	}

	if resp.Status.EdgeId != "nonexistent" {
		t.Errorf("expected edge ID 'nonexistent', got %q", resp.Status.EdgeId)
	}
	if resp.Status.ConnectionStatus != pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_DISCONNECTED {
		t.Errorf("expected disconnected status, got %v", resp.Status.ConnectionStatus)
	}
}

func TestService_GetEdgeStatus_Found(t *testing.T) {
	manager := NewManager(ManagerConfig{}, nil, nil)
	manager.mu.Lock()
	manager.edges["edge-1"] = &EdgeConnection{
		ID:     "edge-1",
		Name:   "Test Edge",
		Status: pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED,
	}
	manager.mu.Unlock()

	svc := NewService(manager)

	resp, err := svc.GetEdgeStatus(context.Background(), &pb.GetEdgeStatusRequest{
		EdgeId: "edge-1",
	})
	if err != nil {
		t.Fatalf("GetEdgeStatus error: %v", err)
	}

	if resp.Status.EdgeId != "edge-1" {
		t.Errorf("expected edge ID 'edge-1', got %q", resp.Status.EdgeId)
	}
	if resp.Status.ConnectionStatus != pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED {
		t.Errorf("expected connected status, got %v", resp.Status.ConnectionStatus)
	}
}
