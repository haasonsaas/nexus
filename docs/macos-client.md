# macOS Client

This document describes the Nexus macOS client ecosystem, including the edge daemon and the rich SwiftUI companion app.

## Components

### nexus-edge Daemon

A headless daemon that:
- Connects to the Nexus core over gRPC
- Registers local tools (camera, screen capture, shell, browser relay)
- Provides edge-only channels (iMessage)
- Runs as a LaunchAgent for auto-start

### NexusMac Companion App

A native SwiftUI menu bar application (`apps/macos`) providing:
- Gateway status monitoring with live WebSocket updates
- Agent workspace for monitoring AI agents
- Computer use control (mouse, keyboard, screenshot)
- Voice wake word detection ("Hey Nexus")
- Conversational talk mode
- Session management and bridging
- Node and tool management
- Provider health and QR authentication
- Skills eligibility tracking
- Cron job monitoring
- Artifacts browsing
- Log viewing with filtering
- Comprehensive settings

For detailed architecture and contribution guidelines, see:
- [apps/macos/README.md](../apps/macos/README.md) - Quick start and features
- [apps/macos/ARCHITECTURE.md](../apps/macos/ARCHITECTURE.md) - Architecture deep dive
- [apps/macos/CONTRIBUTING.md](../apps/macos/CONTRIBUTING.md) - Coding guidelines

## Quick Setup

### 1. Create Pairing Token

On the core server:

```bash
nexus nodes pair --name "My MacBook" --type mac
```

### 2. Initialize Edge Config

On the Mac:

```bash
nexus-edge init --core-url localhost:9090 --edge-id my-mac --pair-token <TOKEN>
```

### 3. Install LaunchAgent

```bash
nexus-edge install --start
```

### 4. Verify Connection

```bash
nexus edge list
```

## Config File

Default location: `~/.nexus-edge/config.yaml`

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
  computer_use:
    allowlist:
      - screenshot
      - mouse_move
      - left_click
      - type
```

### Policy Options

- `pairing_token` - Alias for `auth_token` during initial pairing
- `node_policy.shell.allowlist` - Restrict shell commands (filepath-style globs)
- `node_policy.computer_use` - Restrict UI automation actions

## LaunchAgent

### Paths

- Service plist: `~/Library/LaunchAgents/com.haasonsaas.nexus-edge.plist`
- Stdout log: `~/Library/Logs/nexus-edge.log`
- Stderr log: `~/Library/Logs/nexus-edge.err.log`

### Management

```bash
# Install and start
nexus-edge install --start

# Check status
launchctl list | grep nexus

# View logs
tail -f ~/Library/Logs/nexus-edge.log
```

## Companion App

### Build & Run

```bash
cd apps/macos
swift build -c release
swift run NexusMac
```

### Requirements

- macOS 14.0+ (Sonoma)
- Gateway HTTP API (default: `http://localhost:8080`)
- API key configured in Settings (stored in Keychain)

### Features

| Feature | Description |
|---------|-------------|
| **Menu Bar** | Quick status, sessions, pause/resume |
| **Overview** | Gateway status, uptime, channels, live updates |
| **Agents** | Active AI agents with status and tasks |
| **Nodes** | Edge nodes with tool invocation |
| **Computer Use** | Screenshot, click, type, scroll, drag |
| **Voice Wake** | "Hey Nexus" wake word detection |
| **Talk Mode** | Hands-free conversation |
| **Sessions** | Browse history and messages |
| **Providers** | Health status and QR authentication |
| **Skills** | Eligibility and environment setup |
| **Cron** | Scheduled jobs with timing |
| **Artifacts** | Generated files with preview |
| **Logs** | Edge logs with search and filter |
| **Settings** | Comprehensive configuration |

### Permissions Required

The app may request:
- **Accessibility** - For UI automation (mouse, keyboard)
- **Screen Recording** - For screenshots
- **Microphone** - For voice wake and talk mode
- **Speech Recognition** - For voice commands

### Connection Modes

1. **Local** - Connect to gateway on localhost
2. **Remote** - Connect via SSH tunnel or direct URL
3. **Auto-Discovery** - Find gateways via Bonjour/Tailscale

## Architecture

The companion app follows a layered architecture:

```
UI Layer (SwiftUI)
    ↓
State Layer (@Observable stores)
    ↓
Service Layer (Singletons via ServiceContainer)
    ↓
Gateway Layer (WebSocket + HTTP)
    ↓
Infrastructure (Keychain, Permissions, Logging)
```

Key components:
- **ApplicationCoordinator** - Lifecycle management
- **ConnectionModeCoordinator** - Gateway connection
- **AgentOrchestrator** - AI agent management
- **SessionBridge** - Session type transitions
- **ToolExecutionService** - Computer use and MCP tools

### Codebase Stats

- **128 Swift files**
- **29,000+ lines of code**
- **55 feature directories**
- **64 singleton services**

## Troubleshooting

### Edge Not Connecting

1. Verify core URL: `nexus-edge config show`
2. Check network: `curl http://localhost:9090/health`
3. View logs: `tail -f ~/Library/Logs/nexus-edge.log`

### Companion App Issues

1. **No connection**: Check gateway URL in Settings
2. **Missing API key**: Configure in Settings > Gateway
3. **Permission denied**: Grant in System Settings > Privacy & Security

### Reset Configuration

```bash
# Edge config
rm -rf ~/.nexus-edge

# App preferences
defaults delete com.haasonsaas.NexusMac
```

## Development

See the companion app documentation:
- [apps/macos/ARCHITECTURE.md](../apps/macos/ARCHITECTURE.md) - Detailed architecture
- [apps/macos/CONTRIBUTING.md](../apps/macos/CONTRIBUTING.md) - Contribution guidelines
