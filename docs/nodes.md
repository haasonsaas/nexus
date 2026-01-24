# Nodes Subsystem

Nodes are devices (Mac, iPhone, servers) that can execute privileged actions like camera captures, screen recordings, location queries, and command execution.

## Concepts

### Node vs Edge

- **Node**: A persistent record representing a registered device. Stored in database.
- **Edge**: An ephemeral gRPC connection from a device. Lives only while connected.

When an edge daemon connects, it is matched to a Node record. The Node persists across connections.

### Capabilities

Nodes declare what they can do:

| Capability | Description | Sensitive |
|-----------|-------------|-----------|
| `camera` | Take photos | Yes |
| `screen` | Screen capture/recording | Yes |
| `location` | GPS coordinates | Yes |
| `filesystem` | File access | Yes |
| `shell` | Command execution | Yes |
| `browser` | Browser relay | No |
| `computer_use` | UI automation (mouse/keyboard/screen) | Yes |
| `channels` | Message channel hosting | No |

## Pairing Flow

```
Owner                Edge Daemon             Core
  │                      │                    │
  │  1. Create Token     │                    │
  ├──────────────────────┼───────────────────>│
  │                      │   Token Created    │
  │<─────────────────────┼────────────────────┤
  │                      │                    │
  │  2. Share Token      │                    │
  ├─────────────────────>│                    │
  │                      │                    │
  │                      │  3. Register       │
  │                      ├───────────────────>│
  │                      │     with Token     │
  │                      │                    │
  │                      │  4. Node Created   │
  │                      │<───────────────────┤
  │                      │                    │
  │                      │  5. Connected      │
  │                      │<──────────────────>│
  │                      │                    │
```

### Step 1: Create Pairing Token

```bash
nexus nodes pair --name "My MacBook" --type mac
```

Output:
```
Pairing token created (expires in 24 hours):
  Token: abc123...
  QR: [QR code displayed]
```

### Step 2: Share Token

The token can be shared via:
- QR code (for mobile devices)
- Copy-paste (for desktop)
- Secure transfer (email, messaging)

### Step 3: Edge Daemon Registration

The edge daemon uses the token during its first connection:

```bash
nexus-edge --pair-token=abc123...
```

Or via config file:
```yaml
# ~/.nexus-edge/config.yaml
pairing_token: abc123...
core_url: grpc://nexus.example.com:50051
```

On macOS, you can initialize and install the edge daemon as a LaunchAgent:

```bash
nexus-edge init --core-url grpc://nexus.example.com:50051 --pair-token abc123...
nexus-edge install --start
```

### Step 4: Node Created

Upon successful pairing:
- The pairing token is marked as used (one-time use)
- A Node record is created with the declared capabilities
- Default permissions are applied (owner-only, sensitive ops require approval)
- The node is marked as online

### Step 5: Connection Established

The edge daemon:
- Maintains a persistent gRPC stream
- Sends periodic heartbeats
- Receives tool execution requests
- Forwards channel messages

## Security Model

### Single-User Mode (Self-Hosted)

In single-user mode, permissions are straightforward:

```
┌────────────────────────────────────────────────────────────────┐
│                       Local Owner                               │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                    Full Control                           │  │
│  │  • Create/revoke nodes                                    │  │
│  │  • Execute any capability                                 │  │
│  │  • Approve sensitive actions                              │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

Configuration:
```yaml
# Core config
nodes:
  default_owner_id: local-owner
  require_approval:
    camera: true
    screen: true
    location: true
    shell: true
    filesystem: true
```

### Multi-User Mode (Future)

Multi-user support will add:
- User-specific node ownership
- Delegated permissions
- Team/org hierarchies
- Approval workflows

## Permissions

### Default Permissions

When a node is paired, it receives default permissions:

| Capability | Allowed | Owner Only | Requires Approval |
|-----------|---------|------------|-------------------|
| camera | Yes | Yes | Yes |
| screen | Yes | Yes | Yes |
| location | Yes | Yes | Yes |
| filesystem | Yes | Yes | Yes |
| shell | Yes | Yes | Yes |
| browser | Yes | Yes | No |
| computer_use | Yes | Yes | Yes |
| channels | Yes | Yes | No |

## Computer Use Tool

Nexus exposes a `computer` tool that proxies to `nodes.computer_use` on a
connected edge. It supports UI actions such as mouse movement, clicks, typing,
scrolling, and screenshots. The edge reports display and permission metadata
(`display_width_px`, `display_height_px`, `display_scale`, `display_number`,
and `perm_*` keys) so agents can reason about available UI capabilities.

### Custom Permissions

Permissions can be updated:

```bash
nexus nodes permissions set <node-id> \
  --capability=camera \
  --require-approval=false
```

## Audit Logs

All node actions are logged:

```bash
nexus nodes audit <node-id>
```

Example output:
```
TIME                    ACTION        USER      DETAILS
2026-01-22 10:00:00    paired        owner1    name="MacBook", caps=[camera,screen]
2026-01-22 10:00:05    connected     owner1
2026-01-22 10:15:00    camera_snap   owner1    approved=true
2026-01-22 10:20:00    disconnected  owner1
2026-01-22 11:00:00    connected     owner1
```

## API Reference

### CreatePairingToken

```protobuf
rpc CreatePairingToken(CreatePairingTokenRequest) returns (CreatePairingTokenResponse);

message CreatePairingTokenRequest {
  string name = 1;
  string device_type = 2;  // "mac", "iphone", "linux", "windows"
}

message CreatePairingTokenResponse {
  PairingToken token = 1;
}
```

### ListNodes

```protobuf
rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);

message ListNodesRequest {
  string owner_id = 1;
  int32 page_size = 2;
  string page_token = 3;
}

message ListNodesResponse {
  repeated Node nodes = 1;
  string next_page_token = 2;
  int32 total_count = 3;
}
```

### GetNode

```protobuf
rpc GetNode(GetNodeRequest) returns (GetNodeResponse);

message GetNodeRequest {
  string node_id = 1;
}
```

### RequestAction

```protobuf
rpc RequestAction(RequestActionRequest) returns (RequestActionResponse);

message RequestActionRequest {
  string node_id = 1;
  NodeCapability capability = 2;
  map<string, string> parameters = 3;
}

message RequestActionResponse {
  bool success = 1;
  string error = 2;
  bytes data = 3;
  map<string, string> metadata = 4;
}
```

## CLI Reference

```bash
# List all nodes
nexus nodes list

# Create pairing token
nexus nodes pair --name "My Device" --type mac

# Get node details
nexus nodes get <node-id>

# Revoke node access
nexus nodes revoke <node-id>

# Delete node permanently
nexus nodes delete <node-id>

# Request action
nexus nodes action <node-id> camera

# View audit logs
nexus nodes audit <node-id>

# Update permissions
nexus nodes permissions set <node-id> --capability=camera --require-approval=false
```

## Troubleshooting

### Node Not Connecting

1. Check the pairing token hasn't expired (24h default)
2. Verify the token hasn't been used already (one-time use)
3. Check network connectivity to core
4. Verify core URL in edge daemon config

### Actions Timing Out

1. Check node is online: `nexus nodes get <node-id>`
2. Verify edge daemon is running on the device
3. Check edge daemon logs for errors
4. Verify capability is declared by the node

### Permission Denied

1. Verify you are the node owner
2. Check capability is in node's capabilities list
3. Review node permissions: `nexus nodes permissions get <node-id>`
4. For sensitive actions, check if approval is required
