// Package gateway provides the main Nexus gateway server.
//
// identity_service.go implements the IdentityService gRPC handlers for cross-channel identity linking.
package gateway

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/internal/identity"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// identityService implements the proto.IdentityServiceServer interface.
type identityService struct {
	proto.UnimplementedIdentityServiceServer
	store identity.Store
}

// newIdentityService creates a new identity service handler.
func newIdentityService(store identity.Store) *identityService {
	return &identityService{store: store}
}

// CreateIdentity creates a new canonical identity.
func (s *identityService) CreateIdentity(ctx context.Context, req *proto.CreateIdentityRequest) (*proto.CreateIdentityResponse, error) {
	if req.CanonicalId == "" {
		return nil, fmt.Errorf("canonical_id is required")
	}

	id := &identity.Identity{
		CanonicalID: req.CanonicalId,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		LinkedPeers: req.LinkedPeers,
	}

	if len(req.Metadata) > 0 {
		id.Metadata = make(map[string]string)
		for k, v := range req.Metadata {
			id.Metadata[k] = v
		}
	}

	if err := s.store.Create(ctx, id); err != nil {
		return nil, err
	}

	return &proto.CreateIdentityResponse{
		Identity: identityToProto(id),
	}, nil
}

// GetIdentity retrieves an identity by canonical ID.
func (s *identityService) GetIdentity(ctx context.Context, req *proto.GetIdentityRequest) (*proto.GetIdentityResponse, error) {
	id, err := s.store.Get(ctx, req.CanonicalId)
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, fmt.Errorf("identity not found: %s", req.CanonicalId)
	}

	return &proto.GetIdentityResponse{
		Identity: identityToProto(id),
	}, nil
}

// ListIdentities lists all identities.
func (s *identityService) ListIdentities(ctx context.Context, req *proto.ListIdentitiesRequest) (*proto.ListIdentitiesResponse, error) {
	limit := int(req.PageSize)
	if limit <= 0 {
		limit = 50
	}

	// Parse page token as offset (simple implementation)
	offset := 0

	identities, total, err := s.store.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}

	protoIdentities := make([]*proto.Identity, 0, len(identities))
	for _, id := range identities {
		protoIdentities = append(protoIdentities, identityToProto(id))
	}

	return &proto.ListIdentitiesResponse{
		Identities: protoIdentities,
		TotalCount: int32(total),
	}, nil
}

// DeleteIdentity deletes an identity and all its links.
func (s *identityService) DeleteIdentity(ctx context.Context, req *proto.DeleteIdentityRequest) (*proto.DeleteIdentityResponse, error) {
	if err := s.store.Delete(ctx, req.CanonicalId); err != nil {
		return nil, err
	}

	return &proto.DeleteIdentityResponse{Success: true}, nil
}

// LinkPeer links a platform-specific peer ID to a canonical identity.
func (s *identityService) LinkPeer(ctx context.Context, req *proto.LinkPeerRequest) (*proto.LinkPeerResponse, error) {
	if req.CanonicalId == "" {
		return nil, fmt.Errorf("canonical_id is required")
	}
	if req.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	if req.PeerId == "" {
		return nil, fmt.Errorf("peer_id is required")
	}

	if err := s.store.LinkPeer(ctx, req.CanonicalId, req.Channel, req.PeerId); err != nil {
		return nil, err
	}

	id, err := s.store.Get(ctx, req.CanonicalId)
	if err != nil {
		return nil, err
	}

	return &proto.LinkPeerResponse{
		Identity: identityToProto(id),
	}, nil
}

// UnlinkPeer removes a peer link from an identity.
func (s *identityService) UnlinkPeer(ctx context.Context, req *proto.UnlinkPeerRequest) (*proto.UnlinkPeerResponse, error) {
	if req.CanonicalId == "" {
		return nil, fmt.Errorf("canonical_id is required")
	}
	if req.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	if req.PeerId == "" {
		return nil, fmt.Errorf("peer_id is required")
	}

	if err := s.store.UnlinkPeer(ctx, req.CanonicalId, req.Channel, req.PeerId); err != nil {
		return nil, err
	}

	id, err := s.store.Get(ctx, req.CanonicalId)
	if err != nil {
		return nil, err
	}

	return &proto.UnlinkPeerResponse{
		Identity: identityToProto(id),
	}, nil
}

// ResolveIdentity resolves a platform peer ID to its canonical identity.
func (s *identityService) ResolveIdentity(ctx context.Context, req *proto.ResolveIdentityRequest) (*proto.ResolveIdentityResponse, error) {
	if req.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	if req.PeerId == "" {
		return nil, fmt.Errorf("peer_id is required")
	}

	id, err := s.store.ResolveByPeer(ctx, req.Channel, req.PeerId)
	if err != nil {
		return nil, err
	}

	if id == nil {
		// Not linked, return the platform ID
		return &proto.ResolveIdentityResponse{
			Found:      false,
			ResolvedId: req.Channel + ":" + req.PeerId,
		}, nil
	}

	return &proto.ResolveIdentityResponse{
		Found:      true,
		Identity:   identityToProto(id),
		ResolvedId: id.CanonicalID,
	}, nil
}

// GetLinkedPeers returns all peer IDs linked to an identity.
func (s *identityService) GetLinkedPeers(ctx context.Context, req *proto.GetLinkedPeersRequest) (*proto.GetLinkedPeersResponse, error) {
	peers, err := s.store.GetLinkedPeers(ctx, req.CanonicalId)
	if err != nil {
		return nil, err
	}

	return &proto.GetLinkedPeersResponse{
		LinkedPeers: peers,
	}, nil
}

// identityToProto converts an identity.Identity to a proto.Identity.
func identityToProto(id *identity.Identity) *proto.Identity {
	p := &proto.Identity{
		CanonicalId: id.CanonicalID,
		DisplayName: id.DisplayName,
		Email:       id.Email,
		LinkedPeers: id.LinkedPeers,
		CreatedAt:   timestamppb.New(id.CreatedAt),
		UpdatedAt:   timestamppb.New(id.UpdatedAt),
	}

	if len(id.Metadata) > 0 {
		p.Metadata = make(map[string]string)
		for k, v := range id.Metadata {
			p.Metadata[k] = v
		}
	}

	return p
}
