package nodes

import (
	"context"
	"testing"
	"time"
)

func TestCreatePairingToken(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	token, err := registry.CreatePairingToken(ctx, "My MacBook", "mac", "owner1")
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	if token.Token == "" {
		t.Error("expected non-empty token")
	}
	if token.NodeID == "" {
		t.Error("expected non-empty node ID")
	}
	if token.Name != "My MacBook" {
		t.Errorf("expected name 'My MacBook', got %s", token.Name)
	}
	if token.DeviceType != "mac" {
		t.Errorf("expected device type 'mac', got %s", token.DeviceType)
	}
	if token.OwnerID != "owner1" {
		t.Errorf("expected owner 'owner1', got %s", token.OwnerID)
	}
	if token.IsExpired() {
		t.Error("token should not be expired")
	}
	if token.IsUsed() {
		t.Error("token should not be used")
	}
}

func TestCompletePairing(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create pairing token
	token, err := registry.CreatePairingToken(ctx, "My MacBook", "mac", "owner1")
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	// Complete pairing
	caps := []Capability{CapCamera, CapScreen, CapShell}
	channels := []string{"imessage"}
	metadata := map[string]string{"os_version": "14.0"}

	node, err := registry.CompletePairing(ctx, token.Token, caps, channels, metadata)
	if err != nil {
		t.Fatalf("CompletePairing: %v", err)
	}

	if node.ID != token.NodeID {
		t.Errorf("expected node ID %s, got %s", token.NodeID, node.ID)
	}
	if node.Name != "My MacBook" {
		t.Errorf("expected name 'My MacBook', got %s", node.Name)
	}
	if node.Status != StatusOnline {
		t.Errorf("expected status online, got %s", node.Status)
	}
	if len(node.Capabilities) != 3 {
		t.Errorf("expected 3 capabilities, got %d", len(node.Capabilities))
	}
	if len(node.ChannelTypes) != 1 {
		t.Errorf("expected 1 channel type, got %d", len(node.ChannelTypes))
	}
	if node.Metadata["os_version"] != "14.0" {
		t.Errorf("expected os_version '14.0', got %s", node.Metadata["os_version"])
	}

	// Verify token is now used
	usedToken, _ := store.GetPairingToken(ctx, token.Token)
	if !usedToken.IsUsed() {
		t.Error("token should be marked as used")
	}

	// Try to use token again
	_, err = registry.CompletePairing(ctx, token.Token, caps, nil, nil)
	if err != ErrPairingTokenUsed {
		t.Errorf("expected ErrPairingTokenUsed, got %v", err)
	}
}

func TestCompletePairingExpired(t *testing.T) {
	store := NewMemoryStore()
	config := RegistryConfig{
		PairingTokenTTL: -1 * time.Hour, // Already expired
		DefaultOwnerID:  "owner",
	}
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	token, err := registry.CreatePairingToken(ctx, "Test", "mac", "")
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	_, err = registry.CompletePairing(ctx, token.Token, nil, nil, nil)
	if err != ErrPairingTokenExpired {
		t.Errorf("expected ErrPairingTokenExpired, got %v", err)
	}
}

func TestGetNode(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair a node
	token, _ := registry.CreatePairingToken(ctx, "Test Node", "mac", "owner1")
	node, _ := registry.CompletePairing(ctx, token.Token, []Capability{CapCamera}, nil, nil)

	// Retrieve it
	retrieved, err := registry.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}

	if retrieved.ID != node.ID {
		t.Errorf("expected ID %s, got %s", node.ID, retrieved.ID)
	}
	if retrieved.Name != "Test Node" {
		t.Errorf("expected name 'Test Node', got %s", retrieved.Name)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	_, err := registry.GetNode(ctx, "nonexistent")
	if err != ErrNodeNotFound {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestListNodes(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create nodes for two owners
	token1, _ := registry.CreatePairingToken(ctx, "Node 1", "mac", "owner1")
	registry.CompletePairing(ctx, token1.Token, nil, nil, nil)

	token2, _ := registry.CreatePairingToken(ctx, "Node 2", "mac", "owner1")
	registry.CompletePairing(ctx, token2.Token, nil, nil, nil)

	token3, _ := registry.CreatePairingToken(ctx, "Node 3", "mac", "owner2")
	registry.CompletePairing(ctx, token3.Token, nil, nil, nil)

	// List owner1's nodes
	nodes, err := registry.ListNodes(ctx, "owner1")
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes for owner1, got %d", len(nodes))
	}

	// List owner2's nodes
	nodes, err = registry.ListNodes(ctx, "owner2")
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 node for owner2, got %d", len(nodes))
	}
}

func TestNodeConnectionStatus(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair a node
	token, _ := registry.CreatePairingToken(ctx, "Test", "mac", "owner1")
	node, _ := registry.CompletePairing(ctx, token.Token, nil, nil, nil)

	// Should be online after pairing
	retrieved, _ := registry.GetNode(ctx, node.ID)
	if retrieved.Status != StatusOnline {
		t.Errorf("expected online after pairing, got %s", retrieved.Status)
	}

	// Disconnect
	if err := registry.NodeDisconnected(ctx, node.ID); err != nil {
		t.Fatalf("NodeDisconnected: %v", err)
	}

	retrieved, _ = registry.GetNode(ctx, node.ID)
	if retrieved.Status != StatusOffline {
		t.Errorf("expected offline after disconnect, got %s", retrieved.Status)
	}

	// Reconnect
	if err := registry.NodeConnected(ctx, node.ID); err != nil {
		t.Fatalf("NodeConnected: %v", err)
	}

	retrieved, _ = registry.GetNode(ctx, node.ID)
	if retrieved.Status != StatusOnline {
		t.Errorf("expected online after reconnect, got %s", retrieved.Status)
	}
}

func TestRevokeNode(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair a node
	token, _ := registry.CreatePairingToken(ctx, "Test", "mac", "owner1")
	node, _ := registry.CompletePairing(ctx, token.Token, nil, nil, nil)

	// Revoke it
	if err := registry.RevokeNode(ctx, node.ID, "owner1"); err != nil {
		t.Fatalf("RevokeNode: %v", err)
	}

	retrieved, _ := registry.GetNode(ctx, node.ID)
	if retrieved.Status != StatusRevoked {
		t.Errorf("expected revoked status, got %s", retrieved.Status)
	}

	// Try to connect a revoked node
	err := registry.NodeConnected(ctx, node.ID)
	if err != ErrNodeRevoked {
		t.Errorf("expected ErrNodeRevoked, got %v", err)
	}
}

func TestPermissions(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair a node with camera capability
	token, _ := registry.CreatePairingToken(ctx, "Test", "mac", "owner1")
	node, _ := registry.CompletePairing(ctx, token.Token, []Capability{CapCamera}, nil, nil)

	// Owner should have permission
	err := registry.CheckPermission(ctx, node.ID, CapCamera, "owner1")
	if err != nil {
		t.Errorf("owner should have camera permission: %v", err)
	}

	// Non-owner should not have permission (owner-only by default)
	err = registry.CheckPermission(ctx, node.ID, CapCamera, "other-user")
	if err != ErrPermissionDenied {
		t.Errorf("non-owner should not have permission: %v", err)
	}

	// Unconfigured capability should be denied
	err = registry.CheckPermission(ctx, node.ID, CapShell, "owner1")
	if err != ErrPermissionDenied {
		t.Errorf("unconfigured capability should be denied: %v", err)
	}

	// Camera should require approval (sensitive)
	requiresApproval, _ := registry.RequiresApproval(ctx, node.ID, CapCamera)
	if !requiresApproval {
		t.Error("camera should require approval")
	}
}

func TestAuditLogs(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair a node
	token, _ := registry.CreatePairingToken(ctx, "Test", "mac", "owner1")
	node, _ := registry.CompletePairing(ctx, token.Token, nil, nil, nil)

	// Trigger some actions
	registry.NodeDisconnected(ctx, node.ID)
	registry.NodeConnected(ctx, node.ID)

	// Get audit logs
	logs, err := registry.GetAuditLogs(ctx, node.ID, 10)
	if err != nil {
		t.Fatalf("GetAuditLogs: %v", err)
	}

	if len(logs) < 3 {
		t.Errorf("expected at least 3 audit logs (paired, disconnected, connected), got %d", len(logs))
	}

	// Most recent should be 'connected'
	if logs[0].Action != "connected" {
		t.Errorf("expected most recent action 'connected', got %s", logs[0].Action)
	}
}

func TestGetOnlineNodes(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair two nodes
	token1, _ := registry.CreatePairingToken(ctx, "Node 1", "mac", "owner1")
	node1, _ := registry.CompletePairing(ctx, token1.Token, nil, nil, nil)

	token2, _ := registry.CreatePairingToken(ctx, "Node 2", "mac", "owner1")
	registry.CompletePairing(ctx, token2.Token, nil, nil, nil)

	// Both should be online
	online := registry.GetOnlineNodes()
	if len(online) != 2 {
		t.Errorf("expected 2 online nodes, got %d", len(online))
	}

	// Disconnect one
	registry.NodeDisconnected(ctx, node1.ID)

	online = registry.GetOnlineNodes()
	if len(online) != 1 {
		t.Errorf("expected 1 online node, got %d", len(online))
	}
}

func TestNodeHeartbeat(t *testing.T) {
	store := NewMemoryStore()
	config := DefaultRegistryConfig()
	registry := NewRegistry(store, config, nil)

	ctx := context.Background()

	// Create and pair a node
	token, _ := registry.CreatePairingToken(ctx, "Test", "mac", "owner1")
	node, _ := registry.CompletePairing(ctx, token.Token, nil, nil, nil)

	// Record initial time
	initialNode, _ := registry.GetNode(ctx, node.ID)
	initialLastSeen := initialNode.LastSeenAt

	// Wait a tiny bit and send heartbeat
	time.Sleep(10 * time.Millisecond)
	registry.NodeHeartbeat(ctx, node.ID)

	// Get updated node
	updatedNode, _ := registry.GetNode(ctx, node.ID)
	if !updatedNode.LastSeenAt.After(*initialLastSeen) {
		t.Error("LastSeenAt should be updated after heartbeat")
	}
}
