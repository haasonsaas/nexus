package nodes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNodeNotFound indicates the node doesn't exist.
	ErrNodeNotFound = errors.New("node not found")

	// ErrPairingTokenNotFound indicates the pairing token doesn't exist.
	ErrPairingTokenNotFound = errors.New("pairing token not found")

	// ErrPairingTokenExpired indicates the token has expired.
	ErrPairingTokenExpired = errors.New("pairing token expired")

	// ErrPairingTokenUsed indicates the token was already used.
	ErrPairingTokenUsed = errors.New("pairing token already used")

	// ErrNodeRevoked indicates the node's access was revoked.
	ErrNodeRevoked = errors.New("node access revoked")

	// ErrPermissionDenied indicates the action is not allowed.
	ErrPermissionDenied = errors.New("permission denied")
)

// Store provides persistence for nodes.
type Store interface {
	// SaveNode creates or updates a node.
	SaveNode(ctx context.Context, node *Node) error

	// GetNode retrieves a node by ID.
	GetNode(ctx context.Context, id NodeID) (*Node, error)

	// ListNodes returns all nodes for an owner.
	ListNodes(ctx context.Context, ownerID string) ([]*Node, error)

	// DeleteNode removes a node.
	DeleteNode(ctx context.Context, id NodeID) error

	// SavePairingToken stores a pairing token.
	SavePairingToken(ctx context.Context, token *PairingToken) error

	// GetPairingToken retrieves a pairing token.
	GetPairingToken(ctx context.Context, token string) (*PairingToken, error)

	// DeletePairingToken removes a pairing token.
	DeletePairingToken(ctx context.Context, token string) error

	// SavePermissions stores node permissions.
	SavePermissions(ctx context.Context, perms *NodePermissions) error

	// GetPermissions retrieves node permissions.
	GetPermissions(ctx context.Context, nodeID NodeID) (*NodePermissions, error)

	// AppendAuditLog adds an audit log entry.
	AppendAuditLog(ctx context.Context, entry *AuditLogEntry) error

	// GetAuditLogs retrieves audit logs for a node.
	GetAuditLogs(ctx context.Context, nodeID NodeID, limit int) ([]*AuditLogEntry, error)
}

// RegistryConfig configures the node registry.
type RegistryConfig struct {
	// PairingTokenTTL is how long pairing tokens are valid.
	PairingTokenTTL time.Duration

	// DefaultOwnerID for single-user mode.
	DefaultOwnerID string
}

// DefaultRegistryConfig returns sensible defaults.
func DefaultRegistryConfig() RegistryConfig {
	return RegistryConfig{
		PairingTokenTTL: 24 * time.Hour,
		DefaultOwnerID:  "local-owner",
	}
}

// Registry manages nodes, pairing, and permissions.
type Registry struct {
	mu     sync.RWMutex
	store  Store
	config RegistryConfig
	logger *slog.Logger

	// In-memory cache of node statuses (edge connections are ephemeral)
	onlineNodes map[NodeID]time.Time
}

// NewRegistry creates a new node registry.
func NewRegistry(store Store, config RegistryConfig, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		store:       store,
		config:      config,
		logger:      logger.With("component", "nodes.registry"),
		onlineNodes: make(map[NodeID]time.Time),
	}
}

// CreatePairingToken generates a new pairing token for a device.
func (r *Registry) CreatePairingToken(ctx context.Context, name, deviceType, ownerID string) (*PairingToken, error) {
	if ownerID == "" {
		ownerID = r.config.DefaultOwnerID
	}

	// Generate secure token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	tokenStr := base64.URLEncoding.EncodeToString(tokenBytes)

	// Generate node ID
	nodeID := NodeID(uuid.New().String())

	token := &PairingToken{
		Token:      tokenStr,
		NodeID:     nodeID,
		Name:       name,
		DeviceType: deviceType,
		OwnerID:    ownerID,
		ExpiresAt:  time.Now().Add(r.config.PairingTokenTTL),
		CreatedAt:  time.Now(),
	}

	if err := r.store.SavePairingToken(ctx, token); err != nil {
		return nil, fmt.Errorf("save pairing token: %w", err)
	}

	r.logger.Info("created pairing token",
		"node_id", nodeID,
		"name", name,
		"device_type", deviceType,
		"expires_at", token.ExpiresAt,
	)

	return token, nil
}

// CompletePairing uses a pairing token to create a node.
// Returns the created node and its assigned edge ID.
func (r *Registry) CompletePairing(ctx context.Context, tokenStr string, capabilities []Capability, channelTypes []string, metadata map[string]string) (*Node, error) {
	token, err := r.store.GetPairingToken(ctx, tokenStr)
	if err != nil {
		if errors.Is(err, ErrPairingTokenNotFound) {
			return nil, ErrPairingTokenNotFound
		}
		return nil, fmt.Errorf("get pairing token: %w", err)
	}

	if token.IsExpired() {
		return nil, ErrPairingTokenExpired
	}

	if token.IsUsed() {
		return nil, ErrPairingTokenUsed
	}

	// Mark token as used
	now := time.Now()
	token.UsedAt = &now
	if err := r.store.SavePairingToken(ctx, token); err != nil {
		return nil, fmt.Errorf("mark token used: %w", err)
	}

	// Create the node
	node := &Node{
		ID:           token.NodeID,
		Name:         token.Name,
		DeviceType:   token.DeviceType,
		OwnerID:      token.OwnerID,
		Status:       StatusOnline,
		Capabilities: capabilities,
		ChannelTypes: channelTypes,
		EdgeID:       string(token.NodeID), // Use node ID as edge ID
		LastSeenAt:   &now,
		Metadata:     metadata,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := r.store.SaveNode(ctx, node); err != nil {
		return nil, fmt.Errorf("save node: %w", err)
	}

	// Set default permissions (owner-only, require approval for sensitive ops)
	perms := &NodePermissions{
		NodeID:      token.NodeID,
		Permissions: make(map[Capability]*Permission),
	}
	for _, cap := range capabilities {
		perms.Permissions[cap] = &Permission{
			Capability:         cap,
			Allowed:            true,
			RequireApproval:    isSensitiveCapability(cap),
			AllowedByOwnerOnly: true, // Single-user mode: owner only
		}
	}
	if err := r.store.SavePermissions(ctx, perms); err != nil {
		r.logger.Warn("failed to save default permissions", "error", err)
	}

	// Audit log
	r.logAudit(ctx, token.NodeID, "paired", token.OwnerID, map[string]any{
		"name":         token.Name,
		"device_type":  token.DeviceType,
		"capabilities": capabilities,
	})

	r.logger.Info("node paired successfully",
		"node_id", token.NodeID,
		"name", token.Name,
		"capabilities", capabilities,
	)

	// Mark as online
	r.mu.Lock()
	r.onlineNodes[token.NodeID] = now
	r.mu.Unlock()

	return node, nil
}

// GetNode retrieves a node by ID.
func (r *Registry) GetNode(ctx context.Context, id NodeID) (*Node, error) {
	node, err := r.store.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update status from in-memory cache
	r.mu.RLock()
	if lastSeen, ok := r.onlineNodes[id]; ok {
		node.Status = StatusOnline
		node.LastSeenAt = &lastSeen
	} else if node.Status == StatusOnline {
		// Was online but edge disconnected
		node.Status = StatusOffline
	}
	r.mu.RUnlock()

	return node, nil
}

// ListNodes returns all nodes for an owner.
func (r *Registry) ListNodes(ctx context.Context, ownerID string) ([]*Node, error) {
	if ownerID == "" {
		ownerID = r.config.DefaultOwnerID
	}

	nodes, err := r.store.ListNodes(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	// Update statuses from in-memory cache
	r.mu.RLock()
	for _, node := range nodes {
		if lastSeen, ok := r.onlineNodes[node.ID]; ok {
			node.Status = StatusOnline
			node.LastSeenAt = &lastSeen
		} else if node.Status == StatusOnline {
			node.Status = StatusOffline
		}
	}
	r.mu.RUnlock()

	return nodes, nil
}

// UpdateNode updates a node's information.
func (r *Registry) UpdateNode(ctx context.Context, id NodeID, name string, metadata map[string]string) error {
	node, err := r.store.GetNode(ctx, id)
	if err != nil {
		return err
	}

	node.Name = name
	node.Metadata = metadata
	node.UpdatedAt = time.Now()

	return r.store.SaveNode(ctx, node)
}

// RevokeNode revokes a node's access.
func (r *Registry) RevokeNode(ctx context.Context, id NodeID, userID string) error {
	node, err := r.store.GetNode(ctx, id)
	if err != nil {
		return err
	}

	node.Status = StatusRevoked
	node.UpdatedAt = time.Now()

	if err := r.store.SaveNode(ctx, node); err != nil {
		return err
	}

	r.logAudit(ctx, id, "revoked", userID, nil)

	r.mu.Lock()
	delete(r.onlineNodes, id)
	r.mu.Unlock()

	r.logger.Info("node access revoked", "node_id", id)
	return nil
}

// DeleteNode permanently deletes a node.
func (r *Registry) DeleteNode(ctx context.Context, id NodeID, userID string) error {
	r.logAudit(ctx, id, "deleted", userID, nil)

	r.mu.Lock()
	delete(r.onlineNodes, id)
	r.mu.Unlock()

	return r.store.DeleteNode(ctx, id)
}

// NodeConnected marks a node as online.
func (r *Registry) NodeConnected(ctx context.Context, id NodeID) error {
	node, err := r.store.GetNode(ctx, id)
	if err != nil {
		return err
	}

	if node.Status == StatusRevoked {
		return ErrNodeRevoked
	}

	now := time.Now()
	node.Status = StatusOnline
	node.LastSeenAt = &now
	node.UpdatedAt = now

	if err := r.store.SaveNode(ctx, node); err != nil {
		return err
	}

	r.mu.Lock()
	r.onlineNodes[id] = now
	r.mu.Unlock()

	r.logAudit(ctx, id, "connected", node.OwnerID, nil)

	r.logger.Debug("node connected", "node_id", id)
	return nil
}

// NodeDisconnected marks a node as offline.
func (r *Registry) NodeDisconnected(ctx context.Context, id NodeID) error {
	r.mu.Lock()
	delete(r.onlineNodes, id)
	r.mu.Unlock()

	node, err := r.store.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNodeNotFound) {
			return nil // Already deleted
		}
		return err
	}

	now := time.Now()
	node.Status = StatusOffline
	node.LastSeenAt = &now
	node.UpdatedAt = now

	if err := r.store.SaveNode(ctx, node); err != nil {
		return err
	}

	r.logAudit(ctx, id, "disconnected", node.OwnerID, nil)

	r.logger.Debug("node disconnected", "node_id", id)
	return nil
}

// NodeHeartbeat updates the last seen time for a node.
func (r *Registry) NodeHeartbeat(ctx context.Context, id NodeID) {
	r.mu.Lock()
	r.onlineNodes[id] = time.Now()
	r.mu.Unlock()
}

// CheckPermission verifies if an action is allowed.
func (r *Registry) CheckPermission(ctx context.Context, nodeID NodeID, cap Capability, userID string) error {
	node, err := r.store.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	if node.Status == StatusRevoked {
		return ErrNodeRevoked
	}

	perms, err := r.store.GetPermissions(ctx, nodeID)
	if err != nil {
		if errors.Is(err, ErrNodeNotFound) {
			// No explicit permissions = deny
			return ErrPermissionDenied
		}
		return err
	}

	if !perms.IsAllowed(cap, userID, node.OwnerID) {
		return ErrPermissionDenied
	}

	return nil
}

// RequiresApproval checks if an action needs per-use approval.
func (r *Registry) RequiresApproval(ctx context.Context, nodeID NodeID, cap Capability) (bool, error) {
	perms, err := r.store.GetPermissions(ctx, nodeID)
	if err != nil {
		if errors.Is(err, ErrNodeNotFound) {
			return true, nil // Unknown = require approval
		}
		return true, err
	}

	return perms.RequiresApproval(cap), nil
}

// UpdatePermissions modifies a node's permission settings.
func (r *Registry) UpdatePermissions(ctx context.Context, nodeID NodeID, perms *NodePermissions, userID string) error {
	perms.NodeID = nodeID
	if err := r.store.SavePermissions(ctx, perms); err != nil {
		return err
	}

	r.logAudit(ctx, nodeID, "permissions_updated", userID, map[string]any{
		"permissions": perms.Permissions,
	})

	return nil
}

// GetAuditLogs retrieves audit logs for a node.
func (r *Registry) GetAuditLogs(ctx context.Context, nodeID NodeID, limit int) ([]*AuditLogEntry, error) {
	return r.store.GetAuditLogs(ctx, nodeID, limit)
}

// logAudit records an audit log entry.
func (r *Registry) logAudit(ctx context.Context, nodeID NodeID, action, userID string, details map[string]any) {
	entry := &AuditLogEntry{
		ID:        uuid.New().String(),
		NodeID:    nodeID,
		Action:    action,
		UserID:    userID,
		Details:   details,
		Timestamp: time.Now(),
	}

	if err := r.store.AppendAuditLog(ctx, entry); err != nil {
		r.logger.Warn("failed to write audit log",
			"node_id", nodeID,
			"action", action,
			"error", err,
		)
	}
}

// isSensitiveCapability returns true for capabilities that should require approval.
func isSensitiveCapability(cap Capability) bool {
	switch cap {
	case CapCamera, CapScreen, CapLocation, CapFilesystem, CapShell:
		return true
	default:
		return false
	}
}

// GetOnlineNodes returns the IDs of currently online nodes.
func (r *Registry) GetOnlineNodes() []NodeID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]NodeID, 0, len(r.onlineNodes))
	for id := range r.onlineNodes {
		result = append(result, id)
	}
	return result
}
