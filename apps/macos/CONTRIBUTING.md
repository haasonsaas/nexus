# Contributing to Nexus macOS Client

Thank you for contributing to the Nexus macOS client! This guide covers coding standards, patterns, and best practices for the codebase.

## Table of Contents

- [Development Setup](#development-setup)
- [Code Standards](#code-standards)
- [SwiftUI Best Practices](#swiftui-best-practices)
- [State Management](#state-management)
- [Error Handling](#error-handling)
- [Async/Await Patterns](#asyncawait-patterns)
- [Memory Management](#memory-management)
- [Testing](#testing)
- [Pull Request Guidelines](#pull-request-guidelines)

## Development Setup

### Prerequisites

- macOS 14.0+ (Sonoma)
- Xcode 15.0+
- Swift 5.9+

### Building

```bash
cd apps/macos
swift build
```

### Running

```bash
swift run NexusMac
```

### Testing

```bash
swift test
```

## Code Standards

### File Organization

Follow the existing feature-based directory structure:

```
Sources/NexusMac/
├── FeatureName/
│   ├── FeatureService.swift      # Service singleton
│   ├── FeatureStore.swift        # Observable state (if needed)
│   └── FeatureView.swift         # SwiftUI view (if applicable)
```

### File Template

```swift
import Foundation
import OSLog

/// Brief description of the service's purpose.
@MainActor
@Observable
final class FeatureService {
    static let shared = FeatureService()
    private init() {}

    private let logger = Logger(subsystem: "com.nexus.mac", category: "feature")

    // MARK: - State

    private(set) var state: State = .idle

    enum State {
        case idle
        case loading
        case ready
        case error(String)
    }

    // MARK: - Public API

    func doSomething() async {
        state = .loading
        defer { state = .ready }

        logger.info("doing something")
        // Implementation
    }

    // MARK: - Private Methods

    private func helper() {
        // Implementation
    }
}
```

### Naming Conventions

| Category | Convention | Example |
|----------|------------|---------|
| Services | `*Service`, `*Manager` | `ToolExecutionService` |
| State stores | `*Store` | `AppStateStore` |
| Bridges | `*Bridge` | `SessionBridge` |
| Controllers | `*Controller` | `OnboardingController` |
| Coordinators | `*Coordinator` | `ConnectionModeCoordinator` |
| Views | `*View` | `AgentWorkspaceView` |
| Runtimes | `*Runtime` | `VoiceWakeRuntime` |

### MARK Comments

Use standard sections in this order:

```swift
// MARK: - Properties
// MARK: - Initialization
// MARK: - Public API
// MARK: - Private Methods
// MARK: - Persistence
```

## SwiftUI Best Practices

### View Size

Keep views under 200 lines. Extract logical sections into subviews:

```swift
// Good
struct FeatureView: View {
    var body: some View {
        VStack {
            headerSection
            contentSection
            footerSection
        }
    }

    private var headerSection: some View { ... }
    private var contentSection: some View { ... }
    private var footerSection: some View { ... }
}

// Avoid: Single view with 500+ lines
```

### State Location

- `@State` - Local view state only
- `@EnvironmentObject` - Shared app model (via `.environmentObject()`)
- `@Bindable` - For `@Observable` objects passed as parameters
- Avoid `@StateObject` unless creating a new model instance

```swift
// Good
struct FeatureView: View {
    @EnvironmentObject var model: AppModel
    @State private var isRefreshing = false

    var body: some View { ... }
}

// Avoid: Creating service instances in views
struct FeatureView: View {
    @StateObject private var service = FeatureService()  // Don't do this
}
```

### Use StateViews Components

Always use the standard state views from `Components/StateViews.swift`:

```swift
// Good
if isLoading {
    LoadingStateView(message: "Loading items...", showSkeleton: true)
} else if items.isEmpty {
    EmptyStateView(
        icon: "tray",
        title: "No Items",
        description: "Items will appear here.",
        actionTitle: "Refresh"
    ) { refresh() }
}

// Avoid: Custom one-off loading views
if isLoading {
    Text("Loading...")  // Don't do this
}
```

### Animations

Use spring animations for state transitions:

```swift
.animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
.animation(.easeInOut(duration: 0.2), value: isRefreshing)
```

### onChange Modifier

Use the new two-parameter closure (Swift 5.9+):

```swift
// Good
.onChange(of: selectedItem) { oldValue, newValue in
    handleChange(from: oldValue, to: newValue)
}

// Deprecated
.onChange(of: selectedItem) { newValue in  // Avoid
    handleChange(newValue)
}
```

## State Management

### Use @Observable (Swift 5.9+)

Prefer `@Observable` over `ObservableObject`:

```swift
// Good
@MainActor
@Observable
final class FeatureStore {
    var items: [Item] = []
    var isLoading = false
}

// Legacy (avoid for new code)
class FeatureStore: ObservableObject {
    @Published var items: [Item] = []
}
```

### Singleton Pattern

Follow this template for service singletons:

```swift
@MainActor
@Observable
final class FeatureService {
    static let shared = FeatureService()
    private init() {}  // Always include private init

    private let logger = Logger(subsystem: "com.nexus.mac", category: "feature")

    // State and methods...
}
```

### ServiceContainer Registration

Register new services in `ServiceContainer`:

```swift
// Services/ServiceContainer.swift
var featureService: FeatureService { FeatureService.shared }
```

## Error Handling

### Define Custom Errors

Create typed errors conforming to `LocalizedError`:

```swift
enum FeatureError: LocalizedError {
    case notFound(String)
    case invalidState
    case networkFailure(underlying: Error)

    var errorDescription: String? {
        switch self {
        case .notFound(let item):
            return "Item not found: \(item)"
        case .invalidState:
            return "Feature is in an invalid state"
        case .networkFailure(let error):
            return "Network error: \(error.localizedDescription)"
        }
    }
}
```

### Log Errors

Always log errors with context:

```swift
do {
    try await performAction()
} catch {
    logger.error("action failed: \(error.localizedDescription)")
    state = .error(error.localizedDescription)
}
```

### Never Silently Swallow Errors

```swift
// Bad
} catch {
    // Silent failure
}

// Good
} catch {
    logger.warning("non-critical operation failed: \(error.localizedDescription)")
}
```

### Display Errors to Users

Use `ErrorBanner` for inline errors:

```swift
if let error = model.lastError {
    ErrorBanner(message: error, severity: .error) {
        model.lastError = nil
    }
}
```

## Async/Await Patterns

### Task Lifecycle Management

Store task references for long-running operations:

```swift
final class FeatureService {
    private var refreshTask: Task<Void, Never>?

    func startRefreshing() {
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.refresh()
                try? await Task.sleep(for: .seconds(30))
            }
        }
    }

    func stopRefreshing() {
        refreshTask?.cancel()
        refreshTask = nil
    }
}
```

### Check for Cancellation

In long loops, check `Task.isCancelled`:

```swift
func processItems(_ items: [Item]) async {
    for item in items {
        guard !Task.isCancelled else { return }
        await process(item)
    }
}
```

### Use Actors for Concurrent State

For thread-safe state accessed from multiple contexts:

```swift
actor DataCache {
    private var cache: [String: Data] = [:]

    func get(_ key: String) -> Data? {
        cache[key]
    }

    func set(_ key: String, data: Data) {
        cache[key] = data
    }
}
```

### Avoid Blocking Main Thread

Never use synchronous blocking calls on MainActor:

```swift
// Bad
func loadData() {
    let result = process.waitUntilExit()  // Blocks main thread
}

// Good
func loadData() async {
    await withCheckedContinuation { continuation in
        process.terminationHandler = { _ in
            continuation.resume()
        }
    }
}
```

## Memory Management

### Always Use [weak self] in Closures

```swift
// Good
Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] _ in
    Task { @MainActor in self?.update() }
}

// Bad - potential retain cycle
Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { _ in
    Task { @MainActor in self.update() }
}
```

### Guard Early After Weak Self

```swift
someAsyncOperation { [weak self] result in
    guard let self else { return }  // Guard immediately
    self.handleResult(result)
}
```

### Clean Up in deinit (When Applicable)

Note: Singleton classes never call deinit. For non-singleton objects:

```swift
class FeatureObserver {
    private var observation: NSKeyValueObservation?

    deinit {
        observation?.invalidate()
    }
}
```

## Testing

### Unit Tests

Place tests in `Tests/NexusMacTests/`:

```swift
import XCTest
@testable import NexusMac

final class FeatureServiceTests: XCTestCase {
    func testInitialState() async {
        let service = FeatureService.shared
        XCTAssertEqual(service.state, .idle)
    }
}
```

### Test Async Code

```swift
func testAsyncOperation() async throws {
    let result = try await service.fetchData()
    XCTAssertFalse(result.isEmpty)
}
```

## Pull Request Guidelines

### Before Submitting

1. **Build passes**: `swift build`
2. **Tests pass**: `swift test`
3. **No warnings**: Address all compiler warnings
4. **Format code**: Consistent indentation and spacing

### PR Description

Include:
- Summary of changes
- Related issue (if applicable)
- Testing performed
- Screenshots (for UI changes)

### Commit Messages

Use conventional commits:

```
feat: add agent workspace view
fix: resolve connection timeout issue
refactor: extract card component
docs: update architecture documentation
```

### Code Review

Expect feedback on:
- Pattern consistency
- Error handling
- Memory management
- UI/UX design

## Questions?

- Check [ARCHITECTURE.md](ARCHITECTURE.md) for architectural guidance
- Review existing code for patterns
- Open an issue for discussion
