package edge

import (
	"context"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// Service implements the EdgeService gRPC interface.
type Service struct {
	pb.UnimplementedEdgeServiceServer
	manager *Manager
}

// NewService creates a new EdgeService.
func NewService(manager *Manager) *Service {
	return &Service{manager: manager}
}

// Connect handles a bidirectional stream from an edge daemon.
func (s *Service) Connect(stream pb.EdgeService_ConnectServer) error {
	return s.manager.HandleConnect(stream)
}

// GetEdgeStatus returns the status of a connected edge.
func (s *Service) GetEdgeStatus(ctx context.Context, req *pb.GetEdgeStatusRequest) (*pb.GetEdgeStatusResponse, error) {
	status, ok := s.manager.GetEdge(req.EdgeId)
	if !ok {
		return &pb.GetEdgeStatusResponse{
			Status: &pb.EdgeStatus{
				EdgeId:           req.EdgeId,
				ConnectionStatus: pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_DISCONNECTED,
			},
		}, nil
	}
	return &pb.GetEdgeStatusResponse{Status: status}, nil
}

// ListEdges returns all connected edges.
func (s *Service) ListEdges(ctx context.Context, req *pb.ListEdgesRequest) (*pb.ListEdgesResponse, error) {
	edges := s.manager.ListEdges()

	// TODO: implement pagination
	return &pb.ListEdgesResponse{
		Edges:      edges,
		TotalCount: int32(len(edges)),
	}, nil
}
