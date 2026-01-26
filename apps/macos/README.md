# Nexus macOS Client

A native macOS companion app for Nexus providing a rich SwiftUI interface for managing the edge daemon, monitoring gateway status, and controlling AI agents.

## Features

### Core Functionality

- **Menu Bar Integration** - Quick access to status, sessions, and controls
- **Gateway Management** - Local and remote gateway connection with auto-discovery
- **Agent Workspace** - Monitor active AI agents and their status
- **Computer Use** - Full UI automation (mouse, keyboard, screenshots)
- **Voice Wake** - "Hey Nexus" wake word detection with push-to-talk
- **Talk Mode** - Hands-free conversational interface
- **Session Bridge** - Seamless transitions between chat, voice, and agent modes

### Dashboard Views

| View | Description |
|------|-------------|
| **Overview** | Gateway status, uptime, channels, live WebSocket updates |
| **Agents** | Active agent instances, status, current tasks |
| **Nodes** | Connected edge nodes with tool invocation |
| **Sessions** | Browse conversation history and messages |
| **Providers** | LLM provider health, connections, QR authentication |
| **Skills** | Skill eligibility, requirements, environment setup |
| **Cron** | Scheduled jobs with next/last run times |
| **Artifacts** | Generated files with preview and quick open |
| **Logs** | Edge service logs with filtering and search |

### UI Components

- **StateViews** - Consistent loading, empty, and error states
- **StatusBadge** - Animated connection indicators
- **HoverHUD** - Quick-access floating panel
- **Onboarding** - Guided first-run experience
- **Notification Center** - System notifications for events

## Quick Start

### Prerequisites

- macOS 14.0+ (Sonoma)
- Xcode 15.0+ (for building)
- `nexus-edge` binary on PATH (or set `NEXUS_EDGE_BIN`)
- Gateway HTTP API reachable (default `http://localhost:8080`)

### Build & Run

```bash
cd apps/macos
swift build -c release
swift run NexusMac
```

### Configuration

On first launch, the app will guide you through:
1. Gateway connection (local or remote)
2. API key setup (stored in Keychain)
3. Permission grants (accessibility, microphone, screen recording)

Settings are persisted in UserDefaults and Keychain.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     UI Layer (SwiftUI)                          │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐               │
│  │   Views/    │ │ Components/ │ │  Settings/  │               │
│  │  (24 files) │ │  (reusable) │ │   (tabs)    │               │
│  └─────────────┘ └─────────────┘ └─────────────┘               │
├─────────────────────────────────────────────────────────────────┤
│                   State Layer (@Observable)                      │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐               │
│  │AppStateStore│ │ AppModel    │ │ *Stores     │               │
│  │  (config)   │ │(API state)  │ │(per-feature)│               │
│  └─────────────┘ └─────────────┘ └─────────────┘               │
├─────────────────────────────────────────────────────────────────┤
│                   Service Layer (Singletons)                     │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐               │
│  │ControlChannel│ │ToolService │ │ AgentOrch.  │               │
│  │ SessionBridge│ │ MCPRegistry│ │ VoiceWake   │               │
│  └─────────────┘ └─────────────┘ └─────────────┘               │
├─────────────────────────────────────────────────────────────────┤
│                   Gateway Layer (WebSocket + HTTP)               │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  GatewayConnection (actor) ─► ControlChannel (events)    │   │
│  │  GatewayDiscovery (Bonjour) ─► ConnectionModeCoordinator │   │
│  └─────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                   Infrastructure Layer                           │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐               │
│  │KeychainStore│ │ Permissions │ │  Logging    │               │
│  │ ConfigStore │ │ Accessibility│ │  (OSLog)   │               │
│  └─────────────┘ └─────────────┘ └─────────────┘               │
└─────────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
Sources/NexusMac/
├── Core/                    # ApplicationCoordinator, lifecycle
├── State/                   # AppStateStore (central config)
├── Services/                # ServiceContainer (DI)
├── Gateway/                 # Connection, discovery, tunneling
│   ├── ControlChannel.swift
│   ├── GatewayConnection.swift
│   ├── GatewayDiscovery.swift
│   └── ConnectionModeCoordinator.swift
├── Agent/                   # AgentOrchestrator
├── Session/                 # SessionBridge
├── Tools/                   # ToolExecutionService
├── ComputerUse/             # Mouse, keyboard, screen capture
├── VoiceWake/               # Wake word detection
├── TalkMode/                # Conversational mode
├── MCP/                     # MCP server management
├── Views/                   # Main UI views (24 files)
├── Components/              # Reusable UI (StateViews, etc.)
├── Settings/                # Settings tab views
├── Menu/                    # Menu bar components
├── Onboarding/              # First-run experience
└── Notifications/           # System notifications
```

### Key Patterns

**State Management:**
- `@Observable` macro for reactive state (Swift 5.9+)
- Singleton pattern for services (`static let shared`)
- `@EnvironmentObject` for view dependency injection

**Service Communication:**
- Actor-based `GatewayConnection` for thread-safe WebSocket
- `ControlChannel` for high-level API with error recovery
- `NotificationCenter` for event broadcasting

**UI Components:**
- Reusable `StateViews` library (Loading, Empty, Error)
- Consistent card-based layouts with hover effects
- Spring animations for state transitions

## Codebase Stats

- **214 Swift files**
- **68,000+ lines of code**
- **64 feature directories**
- **~109 singleton services**
- Low technical debt

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for coding guidelines and patterns.

### Testing

```bash
swift test
```

### Linting

```bash
swiftlint
```

## Requirements

- macOS 14.0+ (Sonoma)
- Swift 5.9+
- Gateway HTTP API at configurable URL
- API key stored in Keychain

## Related Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) - Detailed architecture documentation
- [CONTRIBUTING.md](CONTRIBUTING.md) - Contribution guidelines
- [docs/macos-client.md](../../docs/macos-client.md) - Edge daemon setup

## License

MIT License - see [LICENSE](../../LICENSE) for details.
