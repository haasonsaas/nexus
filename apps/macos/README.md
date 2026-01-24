# Nexus macOS Companion (menu bar + rich UI)

This is a macOS menu bar companion for Nexus. It provides a rich SwiftUI
interface for managing the local edge daemon and inspecting the gateway
(via the HTTP API).

## Features

- Menu bar quick actions (start/stop edge, refresh, open app).
- LaunchAgent management for `nexus-edge` (install/update/start/stop).
- Overview dashboard (gateway status, channels, uptime).
- Nodes + tools explorer (view edge nodes, invoke tools).
- Sessions viewer (browse session history and messages).
- Providers dashboard (health + QR codes).
- Skills dashboard (eligibility + refresh).
- Cron dashboard (schedules + next/last runs).
- Artifacts browser (open downloaded artifacts locally).
- Config editor (edit `~/.nexus-edge/config.yaml`).
- Log viewer (tail `~/Library/Logs/nexus-edge.log`).

## Build

```bash
cd apps/macos
swift build -c release
```

## Run

```bash
cd apps/macos
swift run NexusMac
```

## Requirements

- `nexus-edge` installed on PATH (or set `NEXUS_EDGE_BIN=/path/to/nexus-edge`).
- Gateway HTTP API reachable (default `http://localhost:8080`).
- API key in Settings (stored in Keychain) for API access.
