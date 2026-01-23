// Package nodes provides the node management subsystem for Nexus.
//
// Nodes are devices (Mac, iPhone, servers) that can execute privileged actions
// like camera captures, screen recordings, location queries, and command execution.
//
// # Architecture
//
// Nodes are persistent entities that represent devices. When a node connects via
// the edge daemon, it is matched to its Node record using the pairing token.
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                      Nodes Registry                              │
//	│  ┌─────────────────────────────────────────────────────────────┐│
//	│  │    Pairing     ->    Node Record    ->    Edge Connection   ││
//	│  │  (one-time)         (persistent)          (ephemeral)       ││
//	│  └─────────────────────────────────────────────────────────────┘│
//	└─────────────────────────────────────────────────────────────────┘
//
// # Pairing Flow
//
//  1. Owner creates a pairing token via API
//  2. Token is shared with the device (QR code, copy-paste)
//  3. Edge daemon uses token during registration
//  4. Core matches token to pending pairing, creates Node
//  5. Subsequent connections use the assigned node_id
//
// # Capabilities
//
// Each node declares what it can do:
//   - camera: Take photos
//   - screen: Screen capture/recording
//   - location: GPS coordinates
//   - filesystem: File access
//   - shell: Command execution
//   - browser: Browser relay
//   - channels: Message channel hosting
package nodes

import (
	"time"
)

// NodeID uniquely identifies a node.
type NodeID string

// NodeStatus represents the current state of a node.
type NodeStatus string

const (
	// StatusPending means the node has a pairing token but hasn't connected yet.
	StatusPending NodeStatus = "pending"

	// StatusOnline means the node is currently connected.
	StatusOnline NodeStatus = "online"

	// StatusOffline means the node was previously connected but is now disconnected.
	StatusOffline NodeStatus = "offline"

	// StatusRevoked means the node's access has been revoked.
	StatusRevoked NodeStatus = "revoked"
)

// Capability represents something a node can do.
type Capability string

const (
	CapCamera     Capability = "camera"
	CapScreen     Capability = "screen"
	CapLocation   Capability = "location"
	CapFilesystem Capability = "filesystem"
	CapShell      Capability = "shell"
	CapBrowser    Capability = "browser"
	CapChannels   Capability = "channels"
)

// Node represents a registered device/agent.
type Node struct {
	// ID is the unique node identifier.
	ID NodeID `json:"id"`

	// Name is the human-readable name.
	Name string `json:"name"`

	// DeviceType indicates what kind of device this is.
	DeviceType string `json:"device_type"` // "mac", "iphone", "linux", "windows"

	// OwnerID is the user who owns this node (for multi-user support).
	OwnerID string `json:"owner_id"`

	// Status is the current node status.
	Status NodeStatus `json:"status"`

	// Capabilities declared by this node.
	Capabilities []Capability `json:"capabilities"`

	// ChannelTypes hosted by this node.
	ChannelTypes []string `json:"channel_types,omitempty"`

	// EdgeID is the ID used for edge daemon connections.
	EdgeID string `json:"edge_id,omitempty"`

	// LastSeenAt is when the node was last online.
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`

	// Metadata is additional node information.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when the node was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the node was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// PairingToken is a one-time token for pairing a new node.
type PairingToken struct {
	// Token is the secret token string.
	Token string `json:"token"`

	// NodeID is the ID that will be assigned to the paired node.
	NodeID NodeID `json:"node_id"`

	// Name is the name to assign to the node.
	Name string `json:"name"`

	// DeviceType is the expected device type.
	DeviceType string `json:"device_type"`

	// OwnerID is the user creating this pairing.
	OwnerID string `json:"owner_id"`

	// ExpiresAt is when this token expires.
	ExpiresAt time.Time `json:"expires_at"`

	// CreatedAt is when the token was created.
	CreatedAt time.Time `json:"created_at"`

	// UsedAt is when the token was used (nil if not used).
	UsedAt *time.Time `json:"used_at,omitempty"`
}

// IsExpired returns true if the token has expired.
func (t *PairingToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// IsUsed returns true if the token has been used.
func (t *PairingToken) IsUsed() bool {
	return t.UsedAt != nil
}

// Permission represents what actions are allowed on a node.
type Permission struct {
	// Capability is the capability this permission applies to.
	Capability Capability `json:"capability"`

	// Allowed indicates if this capability is allowed.
	Allowed bool `json:"allowed"`

	// RequireApproval indicates if each use requires explicit approval.
	RequireApproval bool `json:"require_approval"`

	// AllowedByOwnerOnly indicates only the owner can use this capability.
	AllowedByOwnerOnly bool `json:"allowed_by_owner_only"`
}

// NodePermissions is the permission set for a node.
type NodePermissions struct {
	// NodeID is the node these permissions apply to.
	NodeID NodeID `json:"node_id"`

	// Permissions maps capability to permission settings.
	Permissions map[Capability]*Permission `json:"permissions"`
}

// IsAllowed checks if a capability is allowed for the given user.
func (np *NodePermissions) IsAllowed(cap Capability, userID, ownerID string) bool {
	perm, ok := np.Permissions[cap]
	if !ok {
		return false
	}
	if !perm.Allowed {
		return false
	}
	if perm.AllowedByOwnerOnly && userID != ownerID {
		return false
	}
	return true
}

// RequiresApproval checks if a capability requires per-use approval.
func (np *NodePermissions) RequiresApproval(cap Capability) bool {
	perm, ok := np.Permissions[cap]
	if !ok {
		return true // Unknown capabilities require approval
	}
	return perm.RequireApproval
}

// AuditLogEntry records an action taken on or by a node.
type AuditLogEntry struct {
	// ID is the unique log entry ID.
	ID string `json:"id"`

	// NodeID is the node involved.
	NodeID NodeID `json:"node_id"`

	// Action is what was done.
	Action string `json:"action"` // "paired", "connected", "disconnected", "camera_snap", etc.

	// UserID is who initiated the action.
	UserID string `json:"user_id"`

	// Details contains action-specific information.
	Details map[string]any `json:"details,omitempty"`

	// Timestamp is when this happened.
	Timestamp time.Time `json:"timestamp"`
}
