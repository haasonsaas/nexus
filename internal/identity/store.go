// Package identity provides cross-channel identity linking functionality.
//
// Identity linking allows mapping platform-specific user IDs (e.g., telegram:123456)
// to canonical identities for unified session management across channels.
package identity

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Identity represents a canonical user identity that can span multiple channels.
type Identity struct {
	// CanonicalID is the unique identifier for this identity.
	CanonicalID string

	// DisplayName is a human-readable name.
	DisplayName string

	// Email (optional).
	Email string

	// LinkedPeers are platform-specific peer IDs linked to this identity.
	// Format: "channel:peer_id" (e.g., "telegram:123456", "discord:789").
	LinkedPeers []string

	// Metadata holds arbitrary key-value data.
	Metadata map[string]string

	// CreatedAt is when the identity was created.
	CreatedAt time.Time

	// UpdatedAt is when the identity was last modified.
	UpdatedAt time.Time
}

// Store defines the interface for identity persistence.
type Store interface {
	// Create creates a new identity.
	Create(ctx context.Context, identity *Identity) error

	// Get retrieves an identity by canonical ID.
	Get(ctx context.Context, canonicalID string) (*Identity, error)

	// Update updates an existing identity.
	Update(ctx context.Context, identity *Identity) error

	// Delete removes an identity.
	Delete(ctx context.Context, canonicalID string) error

	// List returns all identities with pagination.
	List(ctx context.Context, limit, offset int) ([]*Identity, int, error)

	// LinkPeer adds a peer link to an identity.
	LinkPeer(ctx context.Context, canonicalID, channel, peerID string) error

	// UnlinkPeer removes a peer link from an identity.
	UnlinkPeer(ctx context.Context, canonicalID, channel, peerID string) error

	// ResolveByPeer finds the identity linked to a channel/peer combination.
	ResolveByPeer(ctx context.Context, channel, peerID string) (*Identity, error)

	// GetLinkedPeers returns all peers linked to an identity.
	GetLinkedPeers(ctx context.Context, canonicalID string) ([]string, error)
}

// MemoryStore is an in-memory implementation of the identity store.
type MemoryStore struct {
	mu sync.RWMutex

	// identities maps canonical_id -> Identity
	identities map[string]*Identity

	// peerIndex maps channel:peer_id -> canonical_id for fast lookup
	peerIndex map[string]string
}

// NewMemoryStore creates a new in-memory identity store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		identities: make(map[string]*Identity),
		peerIndex:  make(map[string]string),
	}
}

// Create creates a new identity.
func (s *MemoryStore) Create(ctx context.Context, identity *Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.identities[identity.CanonicalID]; exists {
		return fmt.Errorf("identity already exists: %s", identity.CanonicalID)
	}

	now := time.Now()
	identity.CreatedAt = now
	identity.UpdatedAt = now

	// Clone to avoid external modifications
	clone := *identity
	clone.LinkedPeers = make([]string, len(identity.LinkedPeers))
	copy(clone.LinkedPeers, identity.LinkedPeers)
	if identity.Metadata != nil {
		clone.Metadata = make(map[string]string)
		for k, v := range identity.Metadata {
			clone.Metadata[k] = v
		}
	}

	s.identities[identity.CanonicalID] = &clone

	// Index all linked peers
	for _, peer := range clone.LinkedPeers {
		s.peerIndex[peer] = identity.CanonicalID
	}

	return nil
}

// Get retrieves an identity by canonical ID.
func (s *MemoryStore) Get(ctx context.Context, canonicalID string) (*Identity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	identity, exists := s.identities[canonicalID]
	if !exists {
		return nil, nil
	}

	// Return a clone
	clone := *identity
	clone.LinkedPeers = make([]string, len(identity.LinkedPeers))
	copy(clone.LinkedPeers, identity.LinkedPeers)
	if identity.Metadata != nil {
		clone.Metadata = make(map[string]string)
		for k, v := range identity.Metadata {
			clone.Metadata[k] = v
		}
	}

	return &clone, nil
}

// Update updates an existing identity.
func (s *MemoryStore) Update(ctx context.Context, identity *Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.identities[identity.CanonicalID]
	if !exists {
		return fmt.Errorf("identity not found: %s", identity.CanonicalID)
	}

	// Remove old peer indexes
	for _, peer := range existing.LinkedPeers {
		delete(s.peerIndex, peer)
	}

	identity.UpdatedAt = time.Now()
	identity.CreatedAt = existing.CreatedAt

	// Clone to avoid external modifications
	clone := *identity
	clone.LinkedPeers = make([]string, len(identity.LinkedPeers))
	copy(clone.LinkedPeers, identity.LinkedPeers)
	if identity.Metadata != nil {
		clone.Metadata = make(map[string]string)
		for k, v := range identity.Metadata {
			clone.Metadata[k] = v
		}
	}

	s.identities[identity.CanonicalID] = &clone

	// Index new linked peers
	for _, peer := range clone.LinkedPeers {
		s.peerIndex[peer] = identity.CanonicalID
	}

	return nil
}

// Delete removes an identity.
func (s *MemoryStore) Delete(ctx context.Context, canonicalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	identity, exists := s.identities[canonicalID]
	if !exists {
		return nil
	}

	// Remove peer indexes
	for _, peer := range identity.LinkedPeers {
		delete(s.peerIndex, peer)
	}

	delete(s.identities, canonicalID)
	return nil
}

// List returns all identities with pagination.
func (s *MemoryStore) List(ctx context.Context, limit, offset int) ([]*Identity, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.identities)

	// Collect all identities
	all := make([]*Identity, 0, total)
	for _, identity := range s.identities {
		clone := *identity
		clone.LinkedPeers = make([]string, len(identity.LinkedPeers))
		copy(clone.LinkedPeers, identity.LinkedPeers)
		if identity.Metadata != nil {
			clone.Metadata = make(map[string]string)
			for k, v := range identity.Metadata {
				clone.Metadata[k] = v
			}
		}
		all = append(all, &clone)
	}

	// Apply pagination
	if offset >= len(all) {
		return []*Identity{}, total, nil
	}

	end := offset + limit
	if end > len(all) {
		end = len(all)
	}

	return all[offset:end], total, nil
}

// LinkPeer adds a peer link to an identity.
func (s *MemoryStore) LinkPeer(ctx context.Context, canonicalID, channel, peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	identity, exists := s.identities[canonicalID]
	if !exists {
		return fmt.Errorf("identity not found: %s", canonicalID)
	}

	platformID := channel + ":" + peerID

	// Check if already linked to another identity
	if existing, ok := s.peerIndex[platformID]; ok && existing != canonicalID {
		return fmt.Errorf("peer %s already linked to identity %s", platformID, existing)
	}

	// Check if already linked to this identity
	for _, p := range identity.LinkedPeers {
		if p == platformID {
			return nil // Already linked
		}
	}

	identity.LinkedPeers = append(identity.LinkedPeers, platformID)
	identity.UpdatedAt = time.Now()
	s.peerIndex[platformID] = canonicalID

	return nil
}

// UnlinkPeer removes a peer link from an identity.
func (s *MemoryStore) UnlinkPeer(ctx context.Context, canonicalID, channel, peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	identity, exists := s.identities[canonicalID]
	if !exists {
		return fmt.Errorf("identity not found: %s", canonicalID)
	}

	platformID := channel + ":" + peerID

	// Remove from linked peers
	newPeers := make([]string, 0, len(identity.LinkedPeers))
	for _, p := range identity.LinkedPeers {
		if p != platformID {
			newPeers = append(newPeers, p)
		}
	}
	identity.LinkedPeers = newPeers
	identity.UpdatedAt = time.Now()

	// Remove from index
	delete(s.peerIndex, platformID)

	return nil
}

// ResolveByPeer finds the identity linked to a channel/peer combination.
func (s *MemoryStore) ResolveByPeer(ctx context.Context, channel, peerID string) (*Identity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	platformID := channel + ":" + peerID

	canonicalID, exists := s.peerIndex[platformID]
	if !exists {
		return nil, nil
	}

	identity, exists := s.identities[canonicalID]
	if !exists {
		return nil, nil
	}

	// Return a clone
	clone := *identity
	clone.LinkedPeers = make([]string, len(identity.LinkedPeers))
	copy(clone.LinkedPeers, identity.LinkedPeers)
	if identity.Metadata != nil {
		clone.Metadata = make(map[string]string)
		for k, v := range identity.Metadata {
			clone.Metadata[k] = v
		}
	}

	return &clone, nil
}

// GetLinkedPeers returns all peers linked to an identity.
func (s *MemoryStore) GetLinkedPeers(ctx context.Context, canonicalID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	identity, exists := s.identities[canonicalID]
	if !exists {
		return nil, fmt.Errorf("identity not found: %s", canonicalID)
	}

	peers := make([]string, len(identity.LinkedPeers))
	copy(peers, identity.LinkedPeers)
	return peers, nil
}

// ExportToConfig exports the current identity links as a config map.
// This is useful for generating config.SessionScopeConfig.IdentityLinks.
func (s *MemoryStore) ExportToConfig() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config := make(map[string][]string)
	for canonicalID, identity := range s.identities {
		if len(identity.LinkedPeers) > 0 {
			config[canonicalID] = make([]string, len(identity.LinkedPeers))
			copy(config[canonicalID], identity.LinkedPeers)
		}
	}
	return config
}

// ImportFromConfig imports identity links from a config map.
// This is useful for loading config.SessionScopeConfig.IdentityLinks.
func (s *MemoryStore) ImportFromConfig(ctx context.Context, config map[string][]string) error {
	for canonicalID, peers := range config {
		identity := &Identity{
			CanonicalID: canonicalID,
			LinkedPeers: peers,
		}
		if err := s.Create(ctx, identity); err != nil {
			// If identity exists, update it
			existing, getErr := s.Get(ctx, canonicalID)
			if getErr == nil && existing != nil {
				existing.LinkedPeers = peers
				if updateErr := s.Update(ctx, existing); updateErr != nil {
					return updateErr
				}
			}
		}
	}
	return nil
}
