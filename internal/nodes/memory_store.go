package nodes

import (
	"context"
	"sync"
)

// MemoryStore is an in-memory implementation of Store for testing and simple deployments.
type MemoryStore struct {
	mu            sync.RWMutex
	nodes         map[NodeID]*Node
	pairingTokens map[string]*PairingToken
	permissions   map[NodeID]*NodePermissions
	auditLogs     map[NodeID][]*AuditLogEntry
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:         make(map[NodeID]*Node),
		pairingTokens: make(map[string]*PairingToken),
		permissions:   make(map[NodeID]*NodePermissions),
		auditLogs:     make(map[NodeID][]*AuditLogEntry),
	}
}

// SaveNode creates or updates a node.
func (s *MemoryStore) SaveNode(ctx context.Context, node *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Make a copy to avoid external mutation
	nodeCopy := *node
	s.nodes[node.ID] = &nodeCopy
	return nil
}

// GetNode retrieves a node by ID.
func (s *MemoryStore) GetNode(ctx context.Context, id NodeID) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, ok := s.nodes[id]
	if !ok {
		return nil, ErrNodeNotFound
	}

	// Return a copy
	nodeCopy := *node
	return &nodeCopy, nil
}

// ListNodes returns all nodes for an owner.
func (s *MemoryStore) ListNodes(ctx context.Context, ownerID string) ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Node
	for _, node := range s.nodes {
		if node.OwnerID == ownerID {
			nodeCopy := *node
			result = append(result, &nodeCopy)
		}
	}
	return result, nil
}

// DeleteNode removes a node.
func (s *MemoryStore) DeleteNode(ctx context.Context, id NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.nodes, id)
	delete(s.permissions, id)
	delete(s.auditLogs, id)
	return nil
}

// SavePairingToken stores a pairing token.
func (s *MemoryStore) SavePairingToken(ctx context.Context, token *PairingToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokenCopy := *token
	s.pairingTokens[token.Token] = &tokenCopy
	return nil
}

// GetPairingToken retrieves a pairing token.
func (s *MemoryStore) GetPairingToken(ctx context.Context, token string) (*PairingToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pt, ok := s.pairingTokens[token]
	if !ok {
		return nil, ErrPairingTokenNotFound
	}

	tokenCopy := *pt
	return &tokenCopy, nil
}

// DeletePairingToken removes a pairing token.
func (s *MemoryStore) DeletePairingToken(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pairingTokens, token)
	return nil
}

// SavePermissions stores node permissions.
func (s *MemoryStore) SavePermissions(ctx context.Context, perms *NodePermissions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Deep copy permissions
	permsCopy := &NodePermissions{
		NodeID:      perms.NodeID,
		Permissions: make(map[Capability]*Permission),
	}
	for cap, perm := range perms.Permissions {
		permCopy := *perm
		permsCopy.Permissions[cap] = &permCopy
	}
	s.permissions[perms.NodeID] = permsCopy
	return nil
}

// GetPermissions retrieves node permissions.
func (s *MemoryStore) GetPermissions(ctx context.Context, nodeID NodeID) (*NodePermissions, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	perms, ok := s.permissions[nodeID]
	if !ok {
		return nil, ErrNodeNotFound
	}

	// Deep copy
	permsCopy := &NodePermissions{
		NodeID:      perms.NodeID,
		Permissions: make(map[Capability]*Permission),
	}
	for cap, perm := range perms.Permissions {
		permCopy := *perm
		permsCopy.Permissions[cap] = &permCopy
	}
	return permsCopy, nil
}

// AppendAuditLog adds an audit log entry.
func (s *MemoryStore) AppendAuditLog(ctx context.Context, entry *AuditLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryCopy := *entry
	s.auditLogs[entry.NodeID] = append(s.auditLogs[entry.NodeID], &entryCopy)
	return nil
}

// GetAuditLogs retrieves audit logs for a node.
func (s *MemoryStore) GetAuditLogs(ctx context.Context, nodeID NodeID, limit int) ([]*AuditLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs, ok := s.auditLogs[nodeID]
	if !ok {
		return []*AuditLogEntry{}, nil
	}

	// Return most recent first
	start := 0
	if limit > 0 && len(logs) > limit {
		start = len(logs) - limit
	}

	result := make([]*AuditLogEntry, 0, limit)
	for i := len(logs) - 1; i >= start; i-- {
		entryCopy := *logs[i]
		result = append(result, &entryCopy)
	}
	return result, nil
}

// Verify MemoryStore implements Store
var _ Store = (*MemoryStore)(nil)
