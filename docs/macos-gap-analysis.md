# Nexus macOS Client - Gap Analysis vs Clawdbot

This document provides a comprehensive comparison between the Nexus macOS client and the Clawdbot macOS client, identifying feature gaps and implementation priorities.

## Executive Summary

| Metric | Nexus | Clawdbot | Gap |
|--------|-------|----------|-----|
| **Swift Files** | 214 | 252 | -38 (~85% coverage) |
| **Lines of Code** | ~68,000 | ~46,846 | +~21,000 (Nexus larger) |
| **Feature Directories** | 64 | 80+ | -16+ |
| **Singleton Services** | ~109 | 80+ | +~29 (Nexus higher) |

## Critical Feature Gaps

### 1. Exec Approvals System (Priority: Critical)

**Clawdbot Has:**
- Full command approval workflow with allowlist management
- Unix domain socket server for IPC (`ExecApprovalsSocketServer`)
- HMAC-authenticated exec requests with TTL validation
- Per-agent security policies (deny, allowlist, always-allow)
- "Ask on miss" mode for allowlist prompts
- Skill binaries auto-allow via `SkillBinsCache`
- NSAlert-based approval UI with command context display
- Exec event logging and allowlist entry recording

**Nexus Has:**
- Basic tool execution via `ToolExecutionService`
- No user approval prompts for shell commands
- No allowlist management

**Files to Reference:**
- `clawdbot/ExecApprovals.swift` (790 LOC) - Core approval models and store
- `clawdbot/ExecApprovalsSocket.swift` (832 LOC) - Socket server and prompt UI
- `clawdbot/ExecApprovalsGatewayPrompter.swift` - Gateway integration

**Implementation Estimate:** High complexity

---

### 2. Cost/Usage Tracking with Charts (Priority: High)

**Clawdbot Has:**
- `CostUsageHistoryMenuView` with Swift Charts
- Daily cost breakdown with bar charts
- Today vs. last N days comparison
- Missing cost entries indicator
- Real-time cost summaries in menu bar

**Nexus Has:**
- Basic `UsageMenuView` with progress bars
- No historical tracking or charts
- No cost breakdowns in the macOS UI (gateway exposes `/api/usage/costs`)

**Files to Reference:**
- `clawdbot/CostUsageMenuView.swift` (100 LOC)

**Implementation Estimate:** Medium complexity

---

### 3. Canvas/Artifact WebKit System (Priority: High)

**Clawdbot Has:**
- Full WebKit-based canvas system (`CanvasWindowController`)
- Custom URL scheme handler (`CanvasSchemeHandler`)
- A2UI action bridge for agent feedback loops
- File watcher for live reload (`CanvasFileWatcher`)
- Session-based artifact directories
- JavaScript evaluation and snapshot capture
- Panel/Window presentation modes
- Debug status overlay

**Nexus Has:**
- Basic `ArtifactsView` with grid display
- File type filtering and search
- No WebKit rendering or live preview
- No agent feedback loops

**Files to Reference:**
- `clawdbot/CanvasWindowController.swift` (362 LOC)
- `clawdbot/CanvasSchemeHandler.swift`
- `clawdbot/CanvasFileWatcher.swift`
- `clawdbot/CanvasA2UIActionMessageHandler.swift`

**Implementation Estimate:** High complexity

---

### 4. Presence Reporter (Priority: Medium)

**Clawdbot Has:**
- Periodic presence beacons to gateway
- Instance identity tracking
- IP address and platform detection
- Last user input tracking (`CGEventSource`)
- Immediate presence on connect

**Nexus Has:**
- Gateway connection status
- No presence reporting
- No user activity tracking

**Files to Reference:**
- `clawdbot/PresenceReporter.swift` (159 LOC)

**Implementation Estimate:** Low complexity

---

### 5. Config File Watcher (Priority: Medium)

**Clawdbot Has:**
- FSEvents-based file watching (`ConfigFileWatcher`)
- External config file sync
- Debounced change notifications
- Reload on config changes

**Nexus Has:**
- In-memory config store
- No external file watching
- Manual refresh required

**Files to Reference:**
- `clawdbot/ConfigFileWatcher.swift` (119 LOC)

**Implementation Estimate:** Low complexity

---

### 6. Node Service Manager (Priority: Medium)

**Clawdbot Has:**
- Remote node service control (`NodeServiceManager`)
- Start/stop commands with JSON output
- Error parsing and hint merging
- Service status checking

**Nexus Has:**
- Basic node listing
- No remote service control

**Files to Reference:**
- `clawdbot/NodeServiceManager.swift` (151 LOC)

**Implementation Estimate:** Medium complexity

---

### 7. Gateway Connectivity Coordinator (Priority: Medium)

**Clawdbot Has:**
- Unified `GatewayConnectivityCoordinator`
- Endpoint state subscription
- Mode and URL resolution
- Host label formatting
- Automatic endpoint refresh on changes

**Nexus Has:**
- `ConnectionModeCoordinator` for mode switching
- Less unified endpoint management

**Files to Reference:**
- `clawdbot/GatewayConnectivityCoordinator.swift` (64 LOC)

**Implementation Estimate:** Low complexity

---

### 8. Instance Identity System (Priority: Medium)

**Clawdbot Has:**
- `InstanceIdentity` with stable instance ID
- Display name derivation
- Model identifier detection
- Used across presence, canvas, approvals

**Nexus Has:**
- Basic device identification
- No unified identity system

**Implementation Estimate:** Low complexity

---

### 9. Device Pairing Approval Prompter (Priority: Low)

**Clawdbot Has:**
- `DevicePairingApprovalPrompter`
- `NodePairingApprovalPrompter`
- Interactive pairing dialogs
- QR code authentication flow

**Nexus Has:**
- Basic provider health view
- QR code display (needs verification)

**Implementation Estimate:** Medium complexity

---

### 10. Launchd Integration (Priority: Low)

**Clawdbot Has:**
- `LaunchdManager` for service lifecycle
- Attach-only override mode
- `Launchctl` wrapper utilities
- LaunchAgent installation

**Nexus Has:**
- LaunchAgent paths documented
- Basic install command
- No attach-only mode

**Files to Reference:**
- `clawdbot/LaunchdManager.swift`
- `clawdbot/Launchctl.swift`

**Implementation Estimate:** Medium complexity

---

## Additional Gaps

### UI/UX Features

| Feature | Clawdbot | Nexus |
|---------|----------|-------|
| Cron Job Editor | Full editor with validation | View only |
| Channel Config Forms | Schema-driven dynamic forms | Static config |
| Agent Events Window | Dedicated event viewer | Inline in workspace |
| Context Menu Cards | Rich context cards | Basic menus |
| Dock Icon Manager | Dynamic icon states | Static icon |
| Menu Sessions Injector | Pre-warmed session previews | Basic session list |

### Infrastructure

| Feature | Clawdbot | Nexus |
|---------|----------|-------|
| AnthropicOAuth | Full OAuth flow | API key only |
| CLI Installer | Guided CLI install | Manual setup |
| Debug Actions | Developer tools | Limited debug |
| Diagnostics File Log | File-based logging | OSLog only |
| Audio Input Observer | Device change monitoring | Static config |

### Agent/Session

| Feature | Clawdbot | Nexus |
|---------|----------|-------|
| Agent Event Store | Persistent event history | In-memory |
| Instances Store | Multi-instance tracking | Single instance |
| Heartbeat Store | Connection health | Basic status |

---

## Implementation Roadmap

### Phase 1: Security & Approvals (Critical)

1. **Exec Approvals Store** - Core models and persistence
2. **Exec Approvals Socket** - Unix socket IPC server
3. **Exec Approvals UI** - NSAlert-based approval prompts
4. **Allowlist Management** - Settings view for allowlist editing

### Phase 2: Monitoring & Analytics (High)

1. **Cost Usage Charts** - Swift Charts integration
2. **Presence Reporter** - Periodic presence beacons
3. **Instance Identity** - Stable device identification

### Phase 3: Canvas System (High)

1. **Canvas Scheme Handler** - Custom URL scheme
2. **Canvas Window Controller** - WebKit container
3. **Canvas File Watcher** - Live reload support
4. **A2UI Action Bridge** - Agent feedback loop

### Phase 4: Infrastructure (Medium)

1. **Config File Watcher** - FSEvents-based watching
2. **Node Service Manager** - Remote node control
3. **Gateway Connectivity** - Unified endpoint management
4. **Launchd Integration** - Service lifecycle management

### Phase 5: Polish (Low)

1. **Cron Job Editor** - Full editing capabilities
2. **Channel Config Forms** - Schema-driven forms
3. **Device Pairing** - Interactive pairing dialogs
4. **Diagnostics Logging** - File-based log export

---

## Architecture Comparison

### Service Layer

**Clawdbot Pattern:**
```swift
@MainActor
@Observable
final class FeatureService {
    static let shared = FeatureService()
    private init() {}

    private let logger = Logger(subsystem: "com.clawdbot", category: "feature")

    // Async subscription to state changes
    func subscribe() -> AsyncStream<State> { ... }
}
```

**Nexus Pattern:**
```swift
@MainActor
@Observable
final class FeatureService {
    static let shared = FeatureService()
    private init() {}

    private let logger = Logger(subsystem: "com.nexus.mac", category: "feature")

    // State published via @Observable
    private(set) var state: State = .idle
}
```

Both follow similar patterns. Clawdbot makes heavier use of `AsyncStream` for state subscriptions.

### IPC

**Clawdbot:** Uses Unix domain sockets with HMAC authentication for secure local IPC (exec approvals).

**Nexus:** Relies primarily on HTTP/WebSocket to gateway.

### File System

**Clawdbot:** Extensive use of FSEvents for file watching, custom URL schemes for canvas.

**Nexus:** Standard file operations, no custom URL schemes.

---

## Recommendations

1. **Prioritize Exec Approvals** - Security-critical feature for production deployment
2. **Add Cost Tracking** - Important for user visibility and cost management
3. **Implement Canvas System** - Enables rich agent-generated content display
4. **Add Presence Reporting** - Improves observability and debugging
5. **Unify Identity System** - Foundation for many other features

---

## Tracking

This document focuses on feature gaps and implementation phases rather than code size targets. For up-to-date codebase stats,
see `apps/macos/README.md`.

---

*Generated: 2026-01-26*
*Nexus Version: Current HEAD*
*Clawdbot Version: Latest Main*
