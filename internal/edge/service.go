package edge

import (
	"context"
	"encoding/base64"
	"strconv"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

const defaultPageSize = 100

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

// ListEdges returns all connected edges with pagination support.
func (s *Service) ListEdges(ctx context.Context, req *pb.ListEdgesRequest) (*pb.ListEdgesResponse, error) {
	allEdges := s.manager.ListEdges()
	totalCount := int32(len(allEdges))

	// Determine page size
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	// Parse page token (contains offset index)
	offset := 0
	if req.PageToken != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.PageToken)
		if err == nil {
			offset, _ = strconv.Atoi(string(decoded))
		}
	}

	// Apply pagination
	var edges []*pb.EdgeStatus
	var nextPageToken string

	if offset < len(allEdges) {
		end := offset + pageSize
		if end > len(allEdges) {
			end = len(allEdges)
		}
		edges = allEdges[offset:end]

		// Generate next page token if there are more results
		if end < len(allEdges) {
			nextPageToken = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
		}
	}

	return &pb.ListEdgesResponse{
		Edges:         edges,
		TotalCount:    totalCount,
		NextPageToken: nextPageToken,
	}, nil
}
