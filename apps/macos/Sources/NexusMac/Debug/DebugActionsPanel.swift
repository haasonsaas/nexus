import AppKit
import Foundation
import OSLog

#if DEBUG

// MARK: - DebugActionCategory

/// Categories for organizing debug actions.
enum DebugActionCategory: String, CaseIterable, Identifiable, Sendable {
    case gateway = "Gateway"
    case session = "Session"
    case agent = "Agent"
    case storage = "Storage"
    case ui = "UI"
    case network = "Network"

    var id: String { rawValue }

    var icon: String {
        switch self {
        case .gateway: return "server.rack"
        case .session: return "person.2.wave.2"
        case .agent: return "cpu"
        case .storage: return "externaldrive"
        case .ui: return "rectangle.on.rectangle"
        case .network: return "network"
        }
    }

    var description: String {
        switch self {
        case .gateway: return "Gateway connection and health management"
        case .session: return "Session and conversation management"
        case .agent: return "Agent orchestration and tool execution"
        case .storage: return "Local storage and configuration"
        case .ui: return "User interface debugging"
        case .network: return "Network and WebSocket debugging"
        }
    }
}

// MARK: - DebugAction

/// Represents a single debug action that can be executed.
struct DebugAction: Identifiable, Sendable {
    let id: String
    let name: String
    let description: String
    let category: DebugActionCategory
    let isDestructive: Bool
    let keyboardShortcut: KeyboardShortcut?

    /// Handler that executes the action and returns a result message.
    private let _handler: @MainActor @Sendable () async throws -> String

    init(
        id: String,
        name: String,
        description: String,
        category: DebugActionCategory,
        isDestructive: Bool = false,
        keyboardShortcut: KeyboardShortcut? = nil,
        handler: @MainActor @Sendable @escaping () async throws -> String
    ) {
        self.id = id
        self.name = name
        self.description = description
        self.category = category
        self.isDestructive = isDestructive
        self.keyboardShortcut = keyboardShortcut
        self._handler = handler
    }

    @MainActor
    func execute() async throws -> String {
        try await _handler()
    }

    struct KeyboardShortcut: Sendable {
        let key: String
        let modifiers: [String]

        var displayString: String {
            var parts: [String] = []
            if modifiers.contains("command") { parts.append("\u{2318}") }
            if modifiers.contains("shift") { parts.append("\u{21E7}") }
            if modifiers.contains("option") { parts.append("\u{2325}") }
            if modifiers.contains("control") { parts.append("\u{2303}") }
            parts.append(key.uppercased())
            return parts.joined()
        }
    }
}

// MARK: - DebugActionResult

/// Result of executing a debug action.
struct DebugActionResult: Identifiable, Sendable {
    let id: UUID
    let actionId: String
    let actionName: String
    let success: Bool
    let message: String
    let timestamp: Date

    init(actionId: String, actionName: String, success: Bool, message: String) {
        self.id = UUID()
        self.actionId = actionId
        self.actionName = actionName
        self.success = success
        self.message = message
        self.timestamp = Date()
    }
}

// MARK: - DebugActionsManager

/// Manages debug actions for developer tools and debugging utilities.
/// Only available in DEBUG builds.
@MainActor
@Observable
final class DebugActionsManager {
    static let shared = DebugActionsManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "debug")

    private(set) var actions: [DebugAction] = []
    private(set) var results: [DebugActionResult] = []
    private(set) var isExecuting = false
    private(set) var currentActionId: String?

    private init() {
        registerDefaultActions()
    }

    // MARK: - Action Registration

    /// Register a new debug action.
    func register(_ action: DebugAction) {
        if !actions.contains(where: { $0.id == action.id }) {
            actions.append(action)
            logger.debug("debug action registered: \(action.id)")
        }
    }

    /// Unregister a debug action by ID.
    func unregister(actionId: String) {
        actions.removeAll { $0.id == actionId }
    }

    // MARK: - Action Retrieval

    /// Get all actions for a specific category.
    func actions(for category: DebugActionCategory) -> [DebugAction] {
        actions.filter { $0.category == category }
    }

    /// Search actions by name or description.
    func search(_ query: String) -> [DebugAction] {
        guard !query.isEmpty else { return actions }
        let lowercased = query.lowercased()
        return actions.filter {
            $0.name.lowercased().contains(lowercased) ||
            $0.description.lowercased().contains(lowercased)
        }
    }

    /// Get action by ID.
    func action(withId id: String) -> DebugAction? {
        actions.first { $0.id == id }
    }

    // MARK: - Action Execution

    /// Execute a debug action by ID.
    @discardableResult
    func run(actionId: String) async -> DebugActionResult {
        guard let action = action(withId: actionId) else {
            let result = DebugActionResult(
                actionId: actionId,
                actionName: "Unknown",
                success: false,
                message: "Action not found: \(actionId)"
            )
            appendResult(result)
            return result
        }

        return await run(action: action)
    }

    /// Execute a debug action.
    @discardableResult
    func run(action: DebugAction) async -> DebugActionResult {
        isExecuting = true
        currentActionId = action.id
        defer {
            isExecuting = false
            currentActionId = nil
        }

        logger.info("executing debug action: \(action.id)")

        let result: DebugActionResult
        do {
            let message = try await action.execute()
            result = DebugActionResult(
                actionId: action.id,
                actionName: action.name,
                success: true,
                message: message
            )
            logger.info("debug action completed: \(action.id) - \(message)")
        } catch {
            result = DebugActionResult(
                actionId: action.id,
                actionName: action.name,
                success: false,
                message: "Error: \(error.localizedDescription)"
            )
            logger.error("debug action failed: \(action.id) - \(error.localizedDescription)")
        }

        appendResult(result)
        return result
    }

    /// Clear execution results.
    func clearResults() {
        results.removeAll()
    }

    private func appendResult(_ result: DebugActionResult) {
        results.insert(result, at: 0)
        // Keep only last 100 results
        if results.count > 100 {
            results = Array(results.prefix(100))
        }
    }

    // MARK: - Default Actions

    private func registerDefaultActions() {
        // Gateway Actions
        registerGatewayActions()

        // Session Actions
        registerSessionActions()

        // Agent Actions
        registerAgentActions()

        // Storage Actions
        registerStorageActions()

        // UI Actions
        registerUIActions()

        // Network Actions
        registerNetworkActions()
    }

    private func registerGatewayActions() {
        register(DebugAction(
            id: "gateway.force_reconnect",
            name: "Force Reconnect",
            description: "Force disconnect and reconnect to the gateway",
            category: .gateway,
            keyboardShortcut: .init(key: "R", modifiers: ["command", "shift"])
        ) {
            await GatewayConnection.shared.shutdown()
            try? await Task.sleep(nanoseconds: 500_000_000)
            try await GatewayConnection.shared.refresh()
            return "Gateway reconnection initiated"
        })

        register(DebugAction(
            id: "gateway.clear_endpoints_cache",
            name: "Reset Gateway Settings",
            description: "Reset gateway connection settings to local defaults",
            category: .gateway
        ) {
            let appState = AppStateStore.shared
            appState.connectionMode = .local
            appState.remoteHost = nil
            appState.remoteIdentityFile = nil
            appState.gatewayUseTLS = false
            appState.gatewayPort = 8080

            await ConnectionModeCoordinator.shared.apply(mode: .local, paused: appState.isPaused)
            return "Gateway settings reset to local"
        })

        register(DebugAction(
            id: "gateway.reset_health_store",
            name: "Reset Health Store",
            description: "Reset health monitoring state",
            category: .gateway
        ) {
            HealthStore.shared.stop()
            HealthStore.shared.markUnknown()
            HealthStore.shared.start()
            return "Health store reset and restarted"
        })

        register(DebugAction(
            id: "gateway.toggle_verbose_logging",
            name: "Toggle Verbose Logging",
            description: "Enable or disable verbose gateway logging",
            category: .gateway,
            keyboardShortcut: .init(key: "V", modifiers: ["command", "shift"])
        ) {
            let newState = await DebugActions.toggleVerboseLoggingMain()
            return "Verbose logging: \(newState ? "enabled" : "disabled")"
        })

        register(DebugAction(
            id: "gateway.restart",
            name: "Restart Gateway Process",
            description: "Stop and restart the gateway process",
            category: .gateway,
            isDestructive: true
        ) {
            DebugActions.restartGateway()
            return "Gateway restart initiated"
        })

        register(DebugAction(
            id: "gateway.refresh_health",
            name: "Refresh Health Now",
            description: "Trigger immediate health check",
            category: .gateway
        ) {
            await HealthStore.shared.refresh()
            let state = HealthStore.shared.state
            switch state {
            case .ok:
                return "Health check: OK"
            case .degraded(let msg):
                return "Health check: Degraded - \(msg)"
            case .linkingNeeded:
                return "Health check: Linking needed"
            case .unknown:
                return "Health check: Unknown"
            }
        })
    }

    private func registerSessionActions() {
        register(DebugAction(
            id: "session.clear_all",
            name: "Clear All Sessions",
            description: "End and remove all active sessions",
            category: .session,
            isDestructive: true
        ) {
            let count = SessionBridge.shared.activeSessions.count
            for session in SessionBridge.shared.activeSessions {
                SessionBridge.shared.endSession(id: session.id, status: .completed)
            }
            return "Cleared \(count) sessions"
        })

        register(DebugAction(
            id: "session.reset_conversation_memory",
            name: "Reset Conversation Memory",
            description: "Clear all conversation history and learned facts",
            category: .session,
            isDestructive: true
        ) {
            let memory = ConversationMemory.shared
            let convCount = memory.conversations.count
            let factCount = memory.facts.count
            // Note: ConversationMemory doesn't expose a clear method, so we delete individually
            for conv in memory.conversations {
                memory.deleteConversation(id: conv.id)
            }
            for fact in memory.facts {
                memory.forget(factId: fact.id)
            }
            return "Cleared \(convCount) conversations and \(factCount) facts"
        })

        register(DebugAction(
            id: "session.dump_active",
            name: "Dump Active Sessions",
            description: "Log details of all active sessions",
            category: .session
        ) {
            let sessions = SessionBridge.shared.activeSessions
            if sessions.isEmpty {
                return "No active sessions"
            }

            var output = "Active sessions (\(sessions.count)):\n"
            for session in sessions {
                output += "  - \(session.id): \(session.type.rawValue) [\(session.status.rawValue)]\n"
                if let title = session.metadata.title {
                    output += "    Title: \(title)\n"
                }
            }
            return output
        })

        register(DebugAction(
            id: "session.dump_memory_stats",
            name: "Dump Memory Stats",
            description: "Show conversation memory statistics",
            category: .session
        ) {
            let memory = ConversationMemory.shared
            var output = "Memory Statistics:\n"
            output += "  Conversations: \(memory.conversations.count)\n"
            output += "  Facts: \(memory.facts.count)\n"
            output += "  Preferences: \(memory.preferences.count)\n"

            // Breakdown by category
            output += "\nFacts by category:\n"
            for category in ConversationMemory.MemoryFact.Category.allCases {
                let count = memory.recall(category: category).count
                if count > 0 {
                    output += "  - \(category.rawValue): \(count)\n"
                }
            }

            return output
        })
    }

    private func registerAgentActions() {
        register(DebugAction(
            id: "agent.stop_all",
            name: "Stop All Agents",
            description: "Terminate all running agent instances",
            category: .agent,
            isDestructive: true,
            keyboardShortcut: .init(key: ".", modifiers: ["command"])
        ) {
            let orchestrator = AgentOrchestrator.shared
            let count = orchestrator.activeAgents.count
            for agent in orchestrator.activeAgents {
                orchestrator.terminate(agentId: agent.id)
            }
            return "Terminated \(count) agents"
        })

        register(DebugAction(
            id: "agent.clear_tool_cache",
            name: "Clear Tool Cache",
            description: "Clear cached tool execution results",
            category: .agent
        ) {
            // ToolExecutionService doesn't have a cache to clear, but we can reset last result
            // This is a placeholder for future caching implementation
            return "Tool cache cleared (no cache currently implemented)"
        })

        register(DebugAction(
            id: "agent.reset_orchestrator_state",
            name: "Reset Orchestrator State",
            description: "Reset the agent orchestrator to initial state",
            category: .agent,
            isDestructive: true
        ) {
            let orchestrator = AgentOrchestrator.shared
            let count = orchestrator.activeAgents.count
            for agent in orchestrator.activeAgents {
                orchestrator.terminate(agentId: agent.id)
            }
            return "Orchestrator reset, terminated \(count) agents"
        })

        register(DebugAction(
            id: "agent.dump_active",
            name: "Dump Active Agents",
            description: "Log details of all active agents",
            category: .agent
        ) {
            let agents = AgentOrchestrator.shared.activeAgents
            if agents.isEmpty {
                return "No active agents"
            }

            var output = "Active agents (\(agents.count)):\n"
            for agent in agents {
                output += "  - \(agent.id): \(agent.type.rawValue) [\(agent.status.rawValue)]\n"
                if let task = agent.currentTask {
                    output += "    Task: \(task)\n"
                }
            }
            return output
        })

        register(DebugAction(
            id: "agent.dump_tool_result",
            name: "Dump Last Tool Result",
            description: "Show the last tool execution result",
            category: .agent
        ) {
            guard let result = ToolExecutionService.shared.lastResult else {
                return "No tool execution result available"
            }

            var output = "Last tool result:\n"
            output += "  Tool: \(result.toolName)\n"
            output += "  Success: \(result.success)\n"
            output += "  Time: \(result.timestamp)\n"
            if let error = result.error {
                output += "  Error: \(error.localizedDescription)\n"
            }
            if let outputValue = result.output {
                output += "  Output: \(String(describing: outputValue).prefix(500))\n"
            }
            return output
        })
    }

    private func registerStorageActions() {
        register(DebugAction(
            id: "storage.clear_user_defaults",
            name: "Clear UserDefaults",
            description: "Remove all UserDefaults entries for this app",
            category: .storage,
            isDestructive: true
        ) {
            let defaults = UserDefaults.standard
            guard let bundleId = Bundle.main.bundleIdentifier else {
                return "Cannot determine bundle identifier"
            }
            defaults.removePersistentDomain(forName: bundleId)
            defaults.synchronize()
            return "UserDefaults cleared for \(bundleId)"
        })

        register(DebugAction(
            id: "storage.reset_config",
            name: "Reset Config",
            description: "Reset configuration to defaults",
            category: .storage,
            isDestructive: true
        ) {
            // Delete local config file
            let configPath = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".nexus/config.json")
            if FileManager.default.fileExists(atPath: configPath.path) {
                try? FileManager.default.removeItem(at: configPath)
            }
            return "Config file deleted, will use defaults on restart"
        })

        register(DebugAction(
            id: "storage.export_app_data",
            name: "Export App Data",
            description: "Export all app data to a file",
            category: .storage
        ) {
            let panel = NSSavePanel()
            panel.nameFieldStringValue = "nexus-debug-export-\(ISO8601DateFormatter().string(from: Date())).json"
            panel.allowedContentTypes = [.json]

            let response = await panel.begin()
            guard response == .OK, let url = panel.url else {
                return "Export cancelled"
            }

            var exportData: [String: Any] = [:]

            // Export UserDefaults (non-sensitive keys only)
            if let bundleId = Bundle.main.bundleIdentifier,
               let defaults = UserDefaults.standard.persistentDomain(forName: bundleId) {
                let filtered = defaults.filter { key, _ in
                    !key.lowercased().contains("key") &&
                    !key.lowercased().contains("secret") &&
                    !key.lowercased().contains("token") &&
                    !key.lowercased().contains("password")
                }
                exportData["userDefaults"] = filtered
            }

            // Export session info (IDs only)
            exportData["sessions"] = SessionBridge.shared.activeSessions.map { [
                "id": $0.id,
                "type": $0.type.rawValue,
                "status": $0.status.rawValue
            ] }

            // Export agent info
            exportData["agents"] = AgentOrchestrator.shared.activeAgents.map { [
                "id": $0.id,
                "type": $0.type.rawValue,
                "status": $0.status.rawValue
            ] }

            // Export health state
            switch HealthStore.shared.state {
            case .ok:
                exportData["healthState"] = "ok"
            case .degraded(let msg):
                exportData["healthState"] = "degraded: \(msg)"
            case .linkingNeeded:
                exportData["healthState"] = "linkingNeeded"
            case .unknown:
                exportData["healthState"] = "unknown"
            }

            let jsonData = try JSONSerialization.data(withJSONObject: exportData, options: [.prettyPrinted, .sortedKeys])
            try jsonData.write(to: url)

            return "Exported app data to \(url.lastPathComponent)"
        })

        register(DebugAction(
            id: "storage.import_app_data",
            name: "Import App Data",
            description: "Import app data from a file",
            category: .storage,
            isDestructive: true
        ) {
            let panel = NSOpenPanel()
            panel.allowedContentTypes = [.json]
            panel.allowsMultipleSelection = false

            let response = await panel.begin()
            guard response == .OK, let url = panel.urls.first else {
                return "Import cancelled"
            }

            let data = try Data(contentsOf: url)
            guard let importData = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                return "Invalid import file format"
            }

            // Import UserDefaults
            if let defaults = importData["userDefaults"] as? [String: Any] {
                for (key, value) in defaults {
                    UserDefaults.standard.set(value, forKey: key)
                }
                UserDefaults.standard.synchronize()
            }

            return "Imported app data from \(url.lastPathComponent)"
        })

        register(DebugAction(
            id: "storage.open_config_folder",
            name: "Open Config Folder",
            description: "Open ~/.nexus in Finder",
            category: .storage
        ) {
            DebugActions.openConfigFolder()
            return "Opened config folder in Finder"
        })

        register(DebugAction(
            id: "storage.open_logs",
            name: "Open Log File",
            description: "Open gateway log file in Finder",
            category: .storage
        ) {
            DebugActions.openLog()
            return "Opened log file in Finder"
        })
    }

    private func registerUIActions() {
        register(DebugAction(
            id: "ui.reset_window_positions",
            name: "Reset Window Positions",
            description: "Reset all window positions to defaults",
            category: .ui
        ) {
            // Remove window frame preferences
            let defaults = UserDefaults.standard
            let keys = defaults.dictionaryRepresentation().keys.filter {
                $0.hasPrefix("NSWindow Frame") || $0.contains("WindowPosition")
            }
            for key in keys {
                defaults.removeObject(forKey: key)
            }
            defaults.synchronize()
            return "Reset \(keys.count) window position preferences"
        })

        register(DebugAction(
            id: "ui.force_theme_refresh",
            name: "Force Theme Refresh",
            description: "Force UI to refresh with current theme",
            category: .ui
        ) {
            // Force appearance update
            for window in NSApp.windows {
                window.appearance = nil // Reset to system
                window.invalidateShadow()
                window.displayIfNeeded()
            }
            return "Refreshed \(NSApp.windows.count) windows"
        })

        register(DebugAction(
            id: "ui.dump_view_hierarchy",
            name: "Dump View Hierarchy",
            description: "Log the view hierarchy of all windows",
            category: .ui
        ) {
            var output = "Window hierarchy:\n"
            for (index, window) in NSApp.windows.enumerated() {
                output += "\nWindow \(index): \(window.title.isEmpty ? "(untitled)" : window.title)\n"
                output += "  Frame: \(window.frame)\n"
                output += "  Level: \(window.level.rawValue)\n"
                output += "  isVisible: \(window.isVisible)\n"
                output += "  isKey: \(window.isKeyWindow)\n"
                if let contentView = window.contentView {
                    output += "  ContentView: \(type(of: contentView))\n"
                    output += "  Subviews: \(contentView.subviews.count)\n"
                }
            }
            return output
        })

        register(DebugAction(
            id: "ui.list_windows",
            name: "List All Windows",
            description: "List all application windows",
            category: .ui
        ) {
            let windows = NSApp.windows
            if windows.isEmpty {
                return "No windows"
            }

            var output = "Application windows (\(windows.count)):\n"
            for window in windows {
                let visibility = window.isVisible ? "visible" : "hidden"
                let key = window.isKeyWindow ? " [key]" : ""
                output += "  - \(window.title.isEmpty ? "(untitled)" : window.title): \(visibility)\(key)\n"
            }
            return output
        })

        register(DebugAction(
            id: "ui.restart_onboarding",
            name: "Restart Onboarding",
            description: "Reset onboarding state to show it again",
            category: .ui
        ) {
            DebugActions.restartOnboarding()
            return "Onboarding will show on next launch"
        })
    }

    private func registerNetworkActions() {
        register(DebugAction(
            id: "network.clear_url_cache",
            name: "Clear URL Cache",
            description: "Clear the shared URL cache",
            category: .network
        ) {
            URLCache.shared.removeAllCachedResponses()
            let diskUsage = URLCache.shared.currentDiskUsage
            let memUsage = URLCache.shared.currentMemoryUsage
            return "URL cache cleared. Disk: \(diskUsage), Memory: \(memUsage)"
        })

        register(DebugAction(
            id: "network.reset_websocket_state",
            name: "Reset WebSocket State",
            description: "Disconnect and reset WebSocket connection",
            category: .network
        ) {
            await GatewayConnection.shared.shutdown()
            return "WebSocket connection reset"
        })

        register(DebugAction(
            id: "network.log_network_stats",
            name: "Log Network Stats",
            description: "Log current network statistics",
            category: .network
        ) {
            var output = "Network Statistics:\n"

            // URL Cache stats
            let cache = URLCache.shared
            output += "\nURL Cache:\n"
            output += "  Disk usage: \(cache.currentDiskUsage) / \(cache.diskCapacity)\n"
            output += "  Memory usage: \(cache.currentMemoryUsage) / \(cache.memoryCapacity)\n"

            // Gateway connection state
            output += "\nGateway Connection:\n"
            let gwState = await GatewayConnection.shared.getStateDescription()
            output += "  State: \(gwState)\n"

            // Gateway settings snapshot
            let appState = AppStateStore.shared
            output += "\nGateway Settings:\n"
            output += "  Mode: \(appState.connectionMode.rawValue)\n"
            if appState.connectionMode == .remote {
                output += "  Host: \(appState.remoteHost ?? "not set")\n"
            } else {
                output += "  Host: 127.0.0.1\n"
            }
            output += "  Port: \(appState.gatewayPort)\n"
            output += "  TLS: \(appState.gatewayUseTLS)\n"

            return output
        })

        register(DebugAction(
            id: "network.send_test_notification",
            name: "Send Test Notification",
            description: "Send a test system notification",
            category: .network
        ) {
            await DebugActions.sendTestNotification()
            return "Test notification sent"
        })

        register(DebugAction(
            id: "network.dump_websocket_events",
            name: "Dump Recent Events",
            description: "Show recent WebSocket events",
            category: .network
        ) {
            // We don't have direct access to WebSocketService events from here
            // but we can show what we have
            var output = "Recent Gateway Activity:\n"

            let health = HealthStore.shared
            output += "\nHealth State: "
            switch health.state {
            case .ok:
                output += "OK"
            case .degraded(let msg):
                output += "Degraded - \(msg)"
            case .linkingNeeded:
                output += "Linking Needed"
            case .unknown:
                output += "Unknown"
            }

            if let lastSuccess = health.lastSuccess {
                output += "\nLast Success: \(lastSuccess)"
            }

            return output
        })
    }
}

// MARK: - GatewayConnection State Extension

extension GatewayConnection {
    /// Get current connection state as a string for debugging.
    func getStateDescription() async -> String {
        // Check if we can perform a health check
        do {
            let ok = try await healthOK(timeoutMs: 3000)
            return ok ? "connected (healthy)" : "connected (unhealthy)"
        } catch {
            return "disconnected"
        }
    }
}

#endif
