# macOS Edge Client

This document describes the macOS client for Nexus. The client is a local
"edge" daemon (`nexus-edge`) that connects to the Nexus core over gRPC and
executes local tools (camera, screen capture, shell, browser relay, and
edge-only channels).

The macOS client includes:
- `nexus-edge`: headless daemon that registers tools and channels.
- LaunchAgent support for auto-start on login.
- Rich SwiftUI menu bar companion app (`apps/macos`).

## Quick setup (macOS)

1) Create a pairing token on the core:

```bash
nexus nodes pair --name "My MacBook" --type mac
```

2) Initialize edge config on the Mac:

```bash
nexus-edge init --core-url localhost:9090 --edge-id my-mac --pair-token <TOKEN>
```

3) Install as a LaunchAgent:

```bash
nexus-edge install --start
```

4) Verify connection from the core:

```bash
nexus edge list
```

## Config file

Default location: `~/.nexus-edge/config.yaml`

Example:

```yaml
core_url: localhost:9090
edge_id: my-mac
name: "My MacBook"
log_level: info
channel_types:
  - imessage
pairing_token: abc123
node_policy:
  shell:
    allowlist:
      - /usr/bin/uptime
      - /opt/homebrew/bin/rg
```

Notes:
- `pairing_token` is accepted as an alias for `auth_token` during initial pairing.
- `node_policy.shell.allowlist` is optional; if set, only matching commands are
  allowed for `nodes.shell_run`. Patterns use filepath-style globs.

## LaunchAgent paths

- Service plist: `~/Library/LaunchAgents/com.haasonsaas.nexus-edge.plist`
- Logs: `~/Library/Logs/nexus-edge.log` and `~/Library/Logs/nexus-edge.err.log`

## Rich macOS UI (menu bar companion)

The companion app lives in `apps/macos` and provides a native SwiftUI interface
for:
- Edge service control (install/start/stop).
- Gateway status and channel health.
- Nodes and tool invocation.
- Artifacts browsing (downloaded via API key).
- Config editing and log viewing.

Run it locally:

```bash
cd apps/macos
swift run NexusMac
```

The app expects:
- Gateway HTTP API at `http://localhost:8080` by default.
- An API key set in Settings (stored in Keychain).
