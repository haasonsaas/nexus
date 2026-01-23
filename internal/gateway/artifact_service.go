package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/internal/artifacts"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// artifactService implements the ArtifactService gRPC service.
type artifactService struct {
	proto.UnimplementedArtifactServiceServer
	repo artifacts.Repository
}

func newArtifactService(repo artifacts.Repository) *artifactService {
	return &artifactService{repo: repo}
}

// GetArtifact retrieves artifact metadata and generates a download URL.
func (s *artifactService) GetArtifact(ctx context.Context, req *proto.GetArtifactRequest) (*proto.GetArtifactResponse, error) {
	if req.ArtifactId == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}

	artifact, data, err := s.repo.GetArtifact(ctx, req.ArtifactId)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if strings.Contains(err.Error(), "expired") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "failed to get artifact: %v", err)
	}
	defer data.Close()

	// Generate download URL based on reference type
	downloadURL := s.generateDownloadURL(artifact)

	return &proto.GetArtifactResponse{
		Artifact:    artifact,
		DownloadUrl: downloadURL,
	}, nil
}

// ListArtifacts lists artifacts matching the filter criteria.
func (s *artifactService) ListArtifacts(ctx context.Context, req *proto.ListArtifactsRequest) (*proto.ListArtifactsResponse, error) {
	filter := artifacts.Filter{
		SessionID: req.SessionId,
		EdgeID:    req.EdgeId,
		Type:      req.Type,
		Limit:     int(req.Limit),
	}

	if req.CreatedAfter != nil {
		filter.CreatedAfter = req.CreatedAfter.AsTime()
	}
	if req.CreatedBefore != nil {
		filter.CreatedBefore = req.CreatedBefore.AsTime()
	}

	if filter.Limit == 0 {
		filter.Limit = 100 // Default limit
	}

	list, err := s.repo.ListArtifacts(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list artifacts: %v", err)
	}

	return &proto.ListArtifactsResponse{
		Artifacts: list,
	}, nil
}

// DeleteArtifact deletes an artifact.
func (s *artifactService) DeleteArtifact(ctx context.Context, req *proto.DeleteArtifactRequest) (*proto.DeleteArtifactResponse, error) {
	if req.ArtifactId == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}

	if err := s.repo.DeleteArtifact(ctx, req.ArtifactId); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete artifact: %v", err)
	}

	return &proto.DeleteArtifactResponse{
		Success: true,
	}, nil
}

// GetArtifactDownloadURL generates a presigned URL for artifact download.
func (s *artifactService) GetArtifactDownloadURL(ctx context.Context, req *proto.GetArtifactDownloadURLRequest) (*proto.GetArtifactDownloadURLResponse, error) {
	if req.ArtifactId == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}

	artifact, data, err := s.repo.GetArtifact(ctx, req.ArtifactId)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "failed to get artifact: %v", err)
	}
	defer data.Close()

	expiresIn := time.Duration(req.ExpiresInSeconds) * time.Second
	if expiresIn == 0 {
		expiresIn = 1 * time.Hour // Default 1 hour
	}

	downloadURL := s.generateDownloadURL(artifact)
	expiresAt := time.Now().Add(expiresIn)

	return &proto.GetArtifactDownloadURLResponse{
		Url:       downloadURL,
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

// generateDownloadURL creates a download URL based on the artifact reference type.
func (s *artifactService) generateDownloadURL(artifact *proto.Artifact) string {
	if artifact == nil {
		return ""
	}

	ref := artifact.Reference

	// For inline data, return a data URL
	if strings.HasPrefix(ref, "inline://") {
		if len(artifact.Data) > 0 {
			return fmt.Sprintf("data:%s;base64,%s",
				artifact.MimeType,
				base64.StdEncoding.EncodeToString(artifact.Data))
		}
		return ""
	}

	// For file:// references, strip the prefix for local serving
	if strings.HasPrefix(ref, "file://") {
		// In production, this would be served through an HTTP endpoint
		return "/api/v1/artifacts/" + artifact.Id + "/download"
	}

	// For S3/external references, the reference is already a URL
	if strings.HasPrefix(ref, "s3://") || strings.HasPrefix(ref, "https://") {
		return ref
	}

	// For redacted artifacts, no download URL
	if strings.HasPrefix(ref, "redacted://") {
		return ""
	}

	// Default: use API endpoint
	return "/api/v1/artifacts/" + artifact.Id + "/download"
}

// ServeArtifactData reads and returns artifact data for HTTP serving.
func (s *artifactService) ServeArtifactData(ctx context.Context, artifactID string) ([]byte, string, error) {
	artifact, data, err := s.repo.GetArtifact(ctx, artifactID)
	if err != nil {
		return nil, "", err
	}
	defer data.Close()

	// Read all data
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, data); err != nil {
		return nil, "", fmt.Errorf("read artifact data: %w", err)
	}

	return buf.Bytes(), artifact.MimeType, nil
}
