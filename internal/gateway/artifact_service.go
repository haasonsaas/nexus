package gateway

import (
	"context"
	"errors"
	"io"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/haasonsaas/nexus/internal/artifacts"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

func (g *grpcService) ListArtifacts(ctx context.Context, req *proto.ListArtifactsRequest) (*proto.ListArtifactsResponse, error) {
	if g.server == nil || g.server.artifactRepo == nil {
		return nil, status.Error(codes.FailedPrecondition, "artifact storage is not configured")
	}
	filter := artifacts.Filter{}
	if req != nil {
		filter.SessionID = req.SessionId
		filter.EdgeID = req.EdgeId
		filter.Type = req.Type
		filter.Limit = int(req.Limit)
		if req.CreatedAfter != nil {
			filter.CreatedAfter = req.CreatedAfter.AsTime()
		}
		if req.CreatedBefore != nil {
			filter.CreatedBefore = req.CreatedBefore.AsTime()
		}
	}

	results, err := g.server.artifactRepo.ListArtifacts(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list artifacts: %v", err)
	}
	return &proto.ListArtifactsResponse{Artifacts: results}, nil
}

func (g *grpcService) GetArtifact(ctx context.Context, req *proto.GetArtifactRequest) (*proto.GetArtifactResponse, error) {
	if g.server == nil || g.server.artifactRepo == nil {
		return nil, status.Error(codes.FailedPrecondition, "artifact storage is not configured")
	}
	if req == nil || strings.TrimSpace(req.ArtifactId) == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}

	artifact, reader, err := g.server.artifactRepo.GetArtifact(ctx, req.ArtifactId)
	if err != nil {
		if isArtifactNotFound(err) {
			return nil, status.Error(codes.NotFound, "artifact not found")
		}
		return nil, status.Errorf(codes.Internal, "get artifact: %v", err)
	}
	defer reader.Close()

	resp := &proto.GetArtifactResponse{Artifact: artifact}
	if req.IncludeData {
		maxBytes := artifacts.MaxInlineDataBytes
		if artifact != nil && artifact.Size > 0 && artifact.Size > maxBytes {
			return nil, status.Errorf(codes.ResourceExhausted, "artifact data too large to include (max %d bytes)", maxBytes)
		}

		data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "read artifact data: %v", err)
		}
		if int64(len(data)) > maxBytes {
			return nil, status.Errorf(codes.ResourceExhausted, "artifact data too large to include (max %d bytes)", maxBytes)
		}
		resp.Data = data
	}
	return resp, nil
}

func (g *grpcService) DeleteArtifact(ctx context.Context, req *proto.DeleteArtifactRequest) (*proto.DeleteArtifactResponse, error) {
	if g.server == nil || g.server.artifactRepo == nil {
		return nil, status.Error(codes.FailedPrecondition, "artifact storage is not configured")
	}
	if req == nil || strings.TrimSpace(req.ArtifactId) == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}
	if err := g.server.artifactRepo.DeleteArtifact(ctx, req.ArtifactId); err != nil {
		if isArtifactNotFound(err) {
			return &proto.DeleteArtifactResponse{Deleted: false}, nil
		}
		return nil, status.Errorf(codes.Internal, "delete artifact: %v", err)
	}
	return &proto.DeleteArtifactResponse{Deleted: true}, nil
}

func isArtifactNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
