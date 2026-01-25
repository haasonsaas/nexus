# Nexus macOS Client Architecture

This document provides a comprehensive architectural overview of the Nexus macOS client for developers and contributors.

## Table of Contents

- [Overview](#overview)
- [Layered Architecture](#layered-architecture)
- [State Management](#state-management)
- [Service Layer](#service-layer)
- [Gateway Communication](#gateway-communication)
- [UI Patterns](#ui-patterns)
- [Data Flow](#data-flow)
- [Design Patterns](#design-patterns)
- [Naming Conventions](#naming-conventions)

## Overview

The Nexus macOS client is a SwiftUI-based menu bar application with ~29,000 lines of Swift code across 128 files. It follows a layered architecture with clear separation between UI, state, services, and infrastructure.

### Architecture Principles

1. **Feature-Based Organization** - Code organized by domain, not layer
2. **Observable State** - Reactive UI with `@Observable` macro
3. **Service Singletons** - Centralized services via `ServiceContainer`
4. **Actor Isolation** - Thread-safe concurrent operations
5. **Protocol Abstraction** - Extensible through interfaces

## Layered Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            UI Layer (SwiftUI Views)                          │
│  • Views/       - Main application views (24 files)                         │
│  • Components/  - Reusable UI components (StateViews, StatusBadge)          │
│  • Settings/    - Settings tab views                                         │
│  • Menu/        - Menu bar components                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                         State Layer (@Observable)                            │
│  • AppStateStore   - Central app configuration and preferences              │
│  • AppModel        - API data and WebSocket state                           │
│  • Feature Stores  - UsageStore, HealthStore, HeartbeatStore, etc.         │
├─────────────────────────────────────────────────────────────────────────────┤
│                        Service Layer (Singletons)                            │
│  • ServiceContainer     - Central service locator                           │
│  • AgentOrchestrator    - AI agent lifecycle management                     │
│  • SessionBridge        - Session type transitions                          │
│  • ToolExecutionService - Computer use and MCP tools                        │
│  • MCPServerRegistry    - MCP server management                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                     Communication Layer (Gateway)                            │
│  • GatewayConnection (actor)    - WebSocket transport                       │
│  • ControlChannel               - High-level API, error recovery            │
│  • ConnectionModeCoordinator    - Local/remote mode management              │
│  • GatewayDiscovery             - Bonjour/Tailscale auto-discovery          │
├─────────────────────────────────────────────────────────────────────────────┤
│                       Infrastructure Layer                                   │
│  • KeychainStore       - Secure credential storage                          │
│  • PermissionManager   - System permission handling                         │
│  • ConfigStore         - File-based configuration                           │
│  • Logger (OSLog)      - Unified logging                                    │
└─────────────────────────────────────────────────────────────────────────────┘
```

## State Management

### State Patterns in Use

| Pattern | Usage | Location |
|---------|-------|----------|
| `@Observable` | Primary reactive state | 43 files |
| `ObservableObject` | Legacy (AppModel) | 5 files |
| Actor | Concurrent state | 3 files |
| `@State` | Local view state | All views |
| `@EnvironmentObject` | Dependency injection | Views |

### AppStateStore (Central Configuration)

```swift
// Location: State/AppStateStore.swift
@MainActor
@Observable
final class AppStateStore {
    static let shared = AppStateStore()

    // Connection
    var connectionMode: ConnectionMode = .unconfigured
    var isPaused: Bool = false

    // Gateway
    var gatewayPort: Int = 8080
    var gatewayAutostart: Bool = true

    // Features
    var voiceWakeEnabled: Bool = false
    var nodeModeEnabled: Bool = false

    // Persisted via UserDefaults with @AppStorage-like behavior
}
```

### AppModel (API State)

```swift
// Location: AppModel.swift
@MainActor
final class AppModel: ObservableObject {
    @Published var status: GatewayStatus?
    @Published var nodes: [NodeSummary] = []
    @Published var sessions: [SessionSummary] = []
    @Published var providers: [ProviderStatus] = []
    // ... API data and refresh methods
}
```

### Feature Stores

Each feature may have its own store:
- `UsageStore` - API usage tracking
- `HealthStore` - Gateway health status
- `HeartbeatStore` - Connection heartbeat
- `SkillsStore` - Skill eligibility
- `CronJobsStore` - Scheduled jobs
- `ChannelsStore` - Messaging channels

## Service Layer

### ServiceContainer

Central service locator providing access to all singleton services:

```swift
// Location: Services/ServiceContainer.swift
@MainActor
final class ServiceContainer {
    static let shared = ServiceContainer()

    var coordinator: ApplicationCoordinator { ApplicationCoordinator.shared }
    var gateway: GatewayProcessManager { GatewayProcessManager.shared }
    var controlChannel: ControlChannel { ControlChannel.shared }
    var orchestrator: AgentOrchestrator { AgentOrchestrator.shared }
    var sessionBridge: SessionBridge { SessionBridge.shared }
    var toolService: ToolExecutionService { ToolExecutionService.shared }
    var voiceWake: VoiceWakeRuntime { VoiceWakeRuntime.shared }
    // ... 30+ services
}
```

### Key Services

#### ApplicationCoordinator
Manages application lifecycle and initialization:
```swift
enum InitializationPhase {
    case pending, config, permissions, services, gateway, ui, complete, failed
}

func initialize() async {
    phase = .config
    await loadConfiguration()

    phase = .permissions
    await checkPermissions()

    phase = .services
    await initializeServices()

    phase = .gateway
    await connectGateway()

    phase = .ui
    await initializeUI()

    phase = .complete
}
```

#### AgentOrchestrator
Manages AI agent lifecycle:
```swift
struct AgentInstance: Identifiable {
    let id: String
    let type: AgentType  // computerUse, coder, researcher, assistant, custom
    var status: AgentStatus  // idle, thinking, executing, waiting, completed, error
    var currentTask: String?
}

func spawn(type: AgentType, task: String?) -> AgentInstance
func terminate(agentId: String)
func processRequest(_ request: AgentRequest) async throws -> AgentResponse
```

#### SessionBridge
Enables session type transitions:
```swift
enum SessionType { case chat, voice, agent, computerUse, mcp }

func createSession(type: SessionType) -> Session
func bridge(from sourceId: String, to targetType: SessionType) -> Session?
func getLineage(sessionId: String) -> [Session]  // Parent chain
```

#### ToolExecutionService
Unified tool execution interface:
```swift
enum ComputerUseAction {
    case screenshot(ScreenshotOptions)
    case click(x: Int, y: Int, button: String?, count: Int?)
    case type(text: String, delay: TimeInterval?)
    case key(name: String, modifiers: [String]?)
    case scroll(deltaX: Int?, deltaY: Int)
    case drag(fromX: Int, fromY: Int, toX: Int, toY: Int, duration: TimeInterval?)
    // ... 15 total actions
}

func executeComputerUse(_ action: ComputerUseAction) async throws -> ToolResult
func executeMCPTool(serverId: String, toolName: String, arguments: [String: Any]) async throws -> ToolResult
```

## Gateway Communication

### Architecture

```
┌───────────────────────────────────────────────────────────┐
│                     ControlChannel                         │
│  (High-level API, State Management, Error Recovery)        │
│  • refreshEndpoint()  • request()  • broadcast()           │
├───────────────────────────────────────────────────────────┤
│                   GatewayConnection                        │
│  (Actor, WebSocket Management, Request/Response)           │
│  • connect()  • send()  • receive()  • healthOK()          │
├───────────────────────────────────────────────────────────┤
│                   WebSocket Transport                      │
│  (URLSessionWebSocketTask, Frame Encoding/Decoding)        │
└───────────────────────────────────────────────────────────┘
```

### Connection States

```swift
// ControlChannel.ConnectionState
enum ConnectionState: Equatable {
    case disconnected
    case connecting
    case connected
    case degraded(String)
}

// ConnectionModeCoordinator.ConnectionState
enum ConnectionState: Equatable {
    case disconnected
    case connecting
    case connected
    case reconnecting
    case error(String)
}
```

### Event Flow

```
Gateway WebSocket
        │
        ▼
GatewayConnection (actor)
    │ parseFrame() → broadcast(.event(...))
    ▼
ControlChannel (subscriber)
    │ handle(push:)
    ▼
NotificationCenter.post()
    │
    ├─► AgentOrchestrator (observes .controlAgentEvent)
    ├─► HeartbeatStore (observes .controlHeartbeat)
    └─► CanvasManager (handles canvas.open/update/close)
```

### Auto-Discovery

```swift
// GatewayDiscovery.swift
@MainActor
@Observable
final class GatewayDiscovery {
    private(set) var discoveredGateways: [DiscoveredGateway] = []

    func startScan()   // Bonjour + Tailscale
    func stopScan()
    func addManualGateway(host: String, port: Int, name: String?)
}

struct DiscoveredGateway: Identifiable {
    let id: String
    let name: String
    let host: String
    let port: Int
    let source: DiscoverySource  // bonjour, tailscale, manual
    var isReachable: Bool
    var latencyMs: Int?
}
```

## UI Patterns

### StateViews Component Library

```swift
// Location: Components/StateViews.swift

// Loading state with optional skeleton shimmer
struct LoadingStateView: View {
    let message: String
    var showSkeleton: Bool = false
}

// Empty state with icon, title, description, optional action
struct EmptyStateView: View {
    let icon: String
    let title: String
    let description: String
    var actionTitle: String?
    var action: (() -> Void)?
}

// Error state with severity levels
struct ErrorStateView: View {
    enum ErrorSeverity { case warning, error, network }
    let message: String
    let severity: ErrorSeverity
}

// Status badge with multiple variants
struct StatusBadge: View {
    enum Status { case online, offline, connecting, warning, error }
    enum Variant { case minimal, badge, animated, detailed }
}

// Skeleton loading placeholder
struct SkeletonView: View {
    var width: CGFloat?
    var height: CGFloat
    var cornerRadius: CGFloat = 4
}
```

### View Structure Pattern

```swift
struct FeatureView: View {
    @EnvironmentObject var model: AppModel
    @State private var isRefreshing = false
    @State private var selectedItem: Item?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            headerView

            // Error banner (if applicable)
            if let error = model.lastError {
                ErrorBanner(message: error, severity: .error) {
                    model.lastError = nil
                }
            }

            // Content with state handling
            Group {
                if isRefreshing && model.items.isEmpty {
                    LoadingStateView(message: "Loading...", showSkeleton: true)
                } else if model.items.isEmpty {
                    EmptyStateView(
                        icon: "tray",
                        title: "No Items",
                        description: "Items will appear here.",
                        actionTitle: "Refresh"
                    ) { refresh() }
                } else {
                    itemsList
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isRefreshing)
        }
        .padding()
    }

    // MARK: - Subviews
    private var headerView: some View { ... }
    private var itemsList: some View { ... }

    // MARK: - Actions
    private func refresh() { ... }
}
```

### Card Component Pattern

```swift
struct ItemCard: View {
    let item: Item
    let isSelected: Bool
    let onSelect: () -> Void

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Card content
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(isSelected ? .blue.opacity(0.5) :
                       (isHovered ? Color.gray.opacity(0.3) : Color.gray.opacity(0.15)),
                       lineWidth: isSelected ? 2 : 1)
        )
        .scaleEffect(isHovered ? 1.01 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
        .onHover { hovering in isHovered = hovering }
        .onTapGesture { onSelect() }
    }
}
```

## Data Flow

### Initialization Flow

```
App Launch
    │
    ▼
ApplicationCoordinator.initialize()
    │
    ├─1─► loadConfiguration()
    │     → ConfigStore.load()
    │     → ModelRouter.loadConfiguration()
    │     → PromptLibrary.loadPrompts()
    │
    ├─2─► checkPermissions()
    │     → AccessibilityBridge.checkPermission()
    │     → NotificationBridge.requestPermission()
    │
    ├─3─► initializeServices()
    │     → ClipboardHistory.startTracking()
    │     → SystemIntegration.startMonitoring()
    │     → GatewayDiscovery.startScan()
    │     → AgentOrchestrator.registerHandlers()
    │     → TailscaleService.startMonitoring()
    │
    ├─4─► connectGateway()
    │     → ConnectionModeCoordinator.apply()
    │     → ControlChannel.refreshEndpoint()
    │
    └─5─► initializeUI()
          → AppIntegration.detectActiveApp()
          → UpdateChecker.checkIfNeeded()
          → OnboardingController.show()
```

### Tool Execution Flow

```
User Action / Agent Request
        │
        ▼
ToolExecutionService.executeComputerUse(action)
        │
        ├─► MouseController      (click, drag, scroll)
        ├─► KeyboardController   (type, key press)
        ├─► ScreenCaptureService (screenshot, window list)
        └─► MCPServerRegistry    (MCP tool calls)
        │
        ▼
ToolResult { toolName, success, output, error }
```

## Design Patterns

### Singleton Pattern

Used for all services. Standard implementation:

```swift
@MainActor
@Observable
final class ServiceName {
    static let shared = ServiceName()
    private init() {}  // Enforce singleton

    private let logger = Logger(subsystem: "com.nexus.mac", category: "service")

    // Observable state
    private(set) var state: State = .initial

    // MARK: - Public API
    func doSomething() async { ... }
}
```

### Coordinator Pattern

Used for lifecycle and navigation management:

```swift
@MainActor
@Observable
final class FeatureCoordinator {
    static let shared = FeatureCoordinator()

    enum State { case idle, active, transitioning }
    private(set) var state: State = .idle

    func activate() async { ... }
    func deactivate() async { ... }
}
```

### Command Pattern

Used for tool actions:

```swift
enum ToolAction {
    case screenshot(options: ScreenshotOptions)
    case click(x: Int, y: Int)
    case type(text: String)

    var name: String { ... }
    func execute() async throws -> Result { ... }
}
```

### State Machine Pattern

Used for connection and lifecycle states:

```swift
enum ConnectionState {
    case disconnected
    case connecting
    case connected
    case reconnecting
    case error(String)

    var canConnect: Bool { self == .disconnected || self == .error("") }
    var isActive: Bool { self == .connected }
}
```

## Naming Conventions

### Type Naming

| Category | Suffix | Examples |
|----------|--------|----------|
| Services | `*Service`, `*Manager` | `ToolExecutionService`, `QuickActionManager` |
| State | `*Store` | `AppStateStore`, `UsageStore`, `HealthStore` |
| Bridges | `*Bridge` | `SessionBridge`, `AccessibilityBridge` |
| Controllers | `*Controller`, `*Coordinator` | `OnboardingController`, `ConnectionModeCoordinator` |
| Views | `*View` | `OverviewView`, `AgentWorkspaceView` |
| Runtimes | `*Runtime` | `VoiceWakeRuntime`, `TalkModeRuntime` |
| Errors | `*Error` | `ToolError`, `OrchestratorError` |

### File Organization

Files match their primary type name:
- `AppStateStore.swift` → `class AppStateStore`
- `GatewayProcessManager.swift` → `class GatewayProcessManager`

### MARK Comments

Standard sections in order:
1. Properties / State
2. Initialization
3. Public API / Service API
4. Private Methods / Internals
5. Persistence (if applicable)
6. Nested Types / Extensions

```swift
final class Service {
    // MARK: - Properties

    // MARK: - Initialization

    // MARK: - Public API

    // MARK: - Private Methods

    // MARK: - Persistence
}

// MARK: - Extensions
extension Service: SomeProtocol { }
```

### Method Naming

| Pattern | Prefix | Examples |
|---------|--------|----------|
| Async fetchers | `fetch*`, `load*` | `fetchStatus()`, `loadConfiguration()` |
| State changers | `set*`, `apply*` | `setEnabled()`, `applyDockIconSetting()` |
| Lifecycle | `start*`, `stop*`, `initialize*` | `startTracking()`, `stopMonitoring()` |
| Event handlers | `handle*` | `handleWebSocketEvent()`, `handleURL()` |
| Refresh | `refresh*` | `refreshNodes()`, `refreshStatus()` |

## Known Issues & Technical Debt

### Areas for Improvement

1. **Migrate AppModel to @Observable** - Currently uses legacy `ObservableObject`
2. **Decompose AppModel** - 630 lines, handles too many responsibilities
3. **Standardize private init()** - Not all singletons enforce private initialization
4. **Extract duplicate AnyCodable** - Defined in multiple files
5. **Add protocol abstractions** - Services could be more testable with protocols

### Low Priority

- Move root-level service files to appropriate directories
- Consolidate HotkeyManager and HotkeyService
- Add accessibility identifiers for UI testing

## Architecture Maturity

**Score: 7/10**

**Strengths:**
- Modern Swift patterns (@Observable, actors, async/await)
- Clear feature-based organization
- Comprehensive functionality
- Good error recovery in Gateway layer
- Well-designed reusable UI components
- Very low technical debt (1 TODO)

**Areas Requiring Attention:**
- Testability limited by singleton pattern
- State management could be more consistent
- AppModel is a "god object"

The architecture successfully handles complex requirements including real-time WebSocket communication, voice activation, computer use tools, and multi-agent orchestration. Gradual refactoring toward dependency injection would improve long-term maintainability.
