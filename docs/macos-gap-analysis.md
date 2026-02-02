# Nexus macOS Client - Gap Analysis vs Clawdbot

This document provides a comprehensive comparison between the Nexus macOS client and the Clawdbot macOS client, identifying feature gaps and implementation priorities.

## Executive Summary

| Metric | Nexus | Clawdbot | Gap |
|--------|-------|----------|-----|
| **Swift Files** | 214 | 252 | -38 (~85% coverage) |
| **Lines of Code** | ~68,000 | ~46,846 | +~21,000 (Nexus larger) |
| **Feature Directories** | 64 | 80+ | -16+ |
| **Singleton Services** | ~109 | 80+ | +~29 (Nexus higher) |

## Status Update (2026-01-26)

Most items previously listed as gaps are now implemented in the Nexus macOS client. The sections below have been updated to reflect current coverage and highlight any remaining gaps.

**Remaining gaps / partials:**
- Anthropic OAuth flow is still API-key only in Nexus.

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
- Exec approvals store/models + allowlist management (`ExecApprovalsStore`, `ExecApprovalModels`)
- IPC socket server and prompt presenter (`ExecApprovalsService`, `ExecApprovalsSocketServer`, `ExecApprovalsPromptPresenter`)
- Approval UI + settings (`ExecApprovalAlert`, `ExecApprovalsListView`, `ExecApprovalsSettingsView`)
- Node command dispatch integrates approval checks (`NodeCommandDispatcher`)

**Files to Reference:**
- `clawdbot/ExecApprovals.swift` (790 LOC) - Core approval models and store
- `clawdbot/ExecApprovalsSocket.swift` (832 LOC) - Socket server and prompt UI
- `clawdbot/ExecApprovalsGatewayPrompter.swift` - Gateway integration

**Status:** Implemented in Nexus (see file references above)

---

### 2. Cost/Usage Tracking with Charts (Priority: High)

**Clawdbot Has:**
- `CostUsageHistoryMenuView` with Swift Charts
- Daily cost breakdown with bar charts
- Today vs. last N days comparison
- Missing cost entries indicator
- Real-time cost summaries in menu bar

**Nexus Has:**
- `CostUsageMenuView` (compact + expanded) with embedded charting
- `CostUsageChartView` and `CostUsageStore` backed by Swift Charts
- `/api/usage/costs` integration via `NexusAPI.fetchCostUsage`

**Files to Reference:**
- `clawdbot/CostUsageMenuView.swift` (100 LOC)

**Status:** Implemented in Nexus (see file references above)

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
- Full WebKit-based canvas system (`CanvasWindowController`)
- Custom scheme handler (`CanvasSchemeHandler`) and session artifact directory support
- Live reload via `CanvasFileWatcher`
- A2UI action bridge via `CanvasA2UIHandler` and `CanvasManager`

**Files to Reference:**
- `clawdbot/CanvasWindowController.swift` (362 LOC)
- `clawdbot/CanvasSchemeHandler.swift`
- `clawdbot/CanvasFileWatcher.swift`
- `clawdbot/CanvasA2UIActionMessageHandler.swift`

**Status:** Implemented in Nexus (see file references above)

---

### 4. Presence Reporter (Priority: Medium)

**Clawdbot Has:**
- Periodic presence beacons to gateway
- Instance identity tracking
- IP address and platform detection
- Last user input tracking (`CGEventSource`)
- Immediate presence on connect

**Nexus Has:**
- `PresenceReporter` with periodic beacons and instance identity integration
- App lifecycle wiring in `NexusApp` and `ApplicationCoordinator`

**Files to Reference:**
- `clawdbot/PresenceReporter.swift` (159 LOC)

**Status:** Implemented in Nexus (see file references above)

---

### 5. Config File Watcher (Priority: Medium)

**Clawdbot Has:**
- FSEvents-based file watching (`ConfigFileWatcher`)
- External config file sync
- Debounced change notifications
- Reload on config changes

**Nexus Has:**
- `ConfigFileWatcher` with FSEvents + debounced change notifications

**Files to Reference:**
- `clawdbot/ConfigFileWatcher.swift` (119 LOC)

**Status:** Implemented in Nexus (see file references above)

---

### 6. Node Service Manager (Priority: Medium)

**Clawdbot Has:**
- Remote node service control (`NodeServiceManager`)
- Start/stop commands with JSON output
- Error parsing and hint merging
- Service status checking

**Nexus Has:**
- `NodeServiceManager` with start/stop/status operations
- `NodeServiceView` UI and error handling

**Files to Reference:**
- `clawdbot/NodeServiceManager.swift` (151 LOC)

**Status:** Implemented in Nexus (see file references above)

---

### 7. Gateway Connectivity Coordinator (Priority: Medium)

**Clawdbot Has:**
- Unified `GatewayConnectivityCoordinator`
- Endpoint state subscription
- Mode and URL resolution
- Host label formatting
- Automatic endpoint refresh on changes

**Nexus Has:**
- `GatewayConnectivityCoordinator` with endpoint state + mode resolution
- `ConnectionModeCoordinator` for mode switching

**Files to Reference:**
- `clawdbot/GatewayConnectivityCoordinator.swift` (64 LOC)

**Status:** Implemented in Nexus (see file references above)

---

### 8. Instance Identity System (Priority: Medium)

**Clawdbot Has:**
- `InstanceIdentity` with stable instance ID
- Display name derivation
- Model identifier detection
- Used across presence, canvas, approvals

**Nexus Has:**
- `InstanceIdentity` for stable instance ID + display name + hardware metadata
- `InstanceIdentityView` UI and usage in `InstancesStore`

**Status:** Implemented in Nexus (see file references above)

---

### 9. Device Pairing Approval Prompter (Priority: Low)

**Clawdbot Has:**
- `DevicePairingApprovalPrompter`
- `NodePairingApprovalPrompter`
- Interactive pairing dialogs
- QR code authentication flow

**Nexus Has:**
- `DevicePairingApprovalPrompter` and `NodePairingApprovalPrompter`
- Interactive pairing dialogs for device/node workflows

**Status:** Implemented in Nexus (see file references above)

---

### 10. Launchd Integration (Priority: Low)

**Clawdbot Has:**
- `LaunchdManager` for service lifecycle
- Attach-only override mode
- `Launchctl` wrapper utilities
- LaunchAgent installation

**Nexus Has:**
- `LaunchdManager` lifecycle management for LaunchAgents
- `Launchctl` wrapper utilities + settings UI

**Files to Reference:**
- `clawdbot/LaunchdManager.swift`
- `clawdbot/Launchctl.swift`

**Status:** Implemented in Nexus (see file references above)

---

## Additional Coverage Updates

### UI/UX Features

| Feature | Clawdbot | Nexus |
|---------|----------|-------|
| Cron Job Editor | Full editor with validation | Full CRUD editor with validation (`CronJobEditorView`, `CronSchedulerView`) |
| Channel Config Forms | Schema-driven dynamic forms | Schema-driven dynamic forms (`ChannelConfigFormView`) |
| Agent Events Window | Dedicated event viewer | `AgentEventsView` + `AgentEventStore` |
| Context Menu Cards | Rich context cards | Context card framework with live pending approvals + cost summaries wired |
| Dock Icon Manager | Dynamic icon states | Dynamic icon states (`DockIconManager`) |
| Menu Sessions Injector | Pre-warmed session previews | `MenuSessionsInjector` for session previews |

### Infrastructure

| Feature | Clawdbot | Nexus |
|---------|----------|-------|
| AnthropicOAuth | Full OAuth flow | API key only (OAuth not yet implemented) |
| CLI Installer | Guided CLI install | `CLIInstaller` + UI (direct/Homebrew/Go) |
| Debug Actions | Developer tools | `DebugActions` panel + registry |
| Diagnostics File Log | File-based logging | `DiagnosticsFileLogger` + export view |
| Audio Input Observer | Device change monitoring | `AudioInputObserver` device monitoring |

### Agent/Session

| Feature | Clawdbot | Nexus |
|---------|----------|-------|
| Agent Event Store | Persistent event history | `AgentEventStore` + `AgentEventsView` |
| Instances Store | Multi-instance tracking | `InstancesStore` multi-instance tracking |
| Heartbeat Store | Connection health | `HeartbeatStore` with health + timeout tracking |

---

## Implementation Roadmap (Historical)

Most items below are now implemented in Nexus. This roadmap is retained for historical context; remaining gaps are listed in the status update above.

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

1. **Add Anthropic OAuth** - Only remaining infra gap in this comparison.
2. **Finish context card data wiring** - Complete (pending approvals + cost summaries now wired).

---

## Tracking

This document focuses on feature gaps and implementation phases rather than code size targets. For up-to-date codebase stats,
see `apps/macos/README.md`.

---

*Updated: 2026-01-26*
*Nexus Version: Current HEAD*
*Clawdbot Version: Latest Main*
