import AppKit
import Foundation
import OSLog

/// Manages quick actions accessible from menu bar and hotkeys.
/// Provides fast access to common AI agent operations.
@MainActor
@Observable
final class QuickActionManager {
    static let shared = QuickActionManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "quickaction")

    private(set) var actions: [QuickAction] = []
    private(set) var recentlyUsed: [String] = []

    struct QuickAction: Identifiable, Codable {
        let id: String
        var name: String
        var description: String?
        var icon: String?
        var hotkey: String?
        var category: Category
        var isBuiltIn: Bool
        var command: ActionCommand

        enum Category: String, Codable, CaseIterable {
            case ai = "AI"
            case screenshot = "Screenshot"
            case clipboard = "Clipboard"
            case window = "Window"
            case system = "System"
            case custom = "Custom"
        }

        enum ActionCommand: Codable {
            case screenshot
            case screenshotRegion
            case screenshotWindow
            case copyContext
            case pasteHistory
            case newChat
            case voiceInput
            case computerUse
            case runWorkflow(String)
            case mcpTool(serverId: String, toolName: String)
            case bash(String)
            case applescript(String)
            case url(String)
        }
    }

    init() {
        registerBuiltInActions()
    }

    // MARK: - Action Management

    /// Register a custom action
    func register(_ action: QuickAction) {
        if let index = actions.firstIndex(where: { $0.id == action.id }) {
            actions[index] = action
        } else {
            actions.append(action)
        }
        persistActions()
        logger.info("action registered id=\(action.id) name=\(action.name)")
    }

    /// Remove an action
    func remove(actionId: String) {
        actions.removeAll { $0.id == actionId }
        persistActions()
        logger.info("action removed id=\(actionId)")
    }

    /// Get actions by category
    func actions(in category: QuickAction.Category) -> [QuickAction] {
        actions.filter { $0.category == category }
    }

    /// Get action by hotkey
    func action(forHotkey hotkey: String) -> QuickAction? {
        actions.first { $0.hotkey == hotkey }
    }

    // MARK: - Execution

    /// Execute an action
    func execute(actionId: String) async throws {
        guard let action = actions.first(where: { $0.id == actionId }) else {
            throw QuickActionError.actionNotFound(actionId)
        }

        await execute(action)
    }

    /// Execute an action directly
    func execute(_ action: QuickAction) async {
        logger.info("executing action id=\(action.id) name=\(action.name)")

        // Track usage
        updateRecentlyUsed(action.id)

        do {
            switch action.command {
            case .screenshot:
                let result = try await ScreenCaptureService.shared.capture()
                NSPasteboard.general.clearContents()
                NSPasteboard.general.writeObjects([result.image])

            case .screenshotRegion:
                // TODO: Implement region selection UI
                break

            case .screenshotWindow:
                let windows = ScreenCaptureService.shared.listWindows()
                if let frontWindow = windows.first {
                    let result = try await ScreenCaptureService.shared.captureWindow(windowID: frontWindow.windowID)
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.writeObjects([result.image])
                }

            case .copyContext:
                let context = await ContextManager.shared.gatherContext()
                let markdown = ContextManager.shared.exportMarkdown()
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(markdown, forType: .string)

            case .pasteHistory:
                // TODO: Show clipboard history UI
                break

            case .newChat:
                let session = SessionBridge.shared.createSession(type: .chat)
                WebChatManager.shared.openChat(for: session.id)

            case .voiceInput:
                // TODO: Trigger voice input
                break

            case .computerUse:
                // Create computer use agent session
                let session = SessionBridge.shared.createSession(type: .computerUse)
                _ = AgentOrchestrator.shared.spawn(type: .computerUse, task: "Computer Use")

            case .runWorkflow(let workflowId):
                _ = await WorkflowEngine.shared.execute(workflowId: workflowId)

            case .mcpTool(let serverId, let toolName):
                _ = try await ToolExecutionService.shared.executeMCPTool(
                    serverId: serverId,
                    toolName: toolName,
                    arguments: [:]
                )

            case .bash(let command):
                let process = Process()
                process.executableURL = URL(fileURLWithPath: "/bin/bash")
                process.arguments = ["-c", command]
                try process.run()
                process.waitUntilExit()

            case .applescript(let script):
                var error: NSDictionary?
                if let appleScript = NSAppleScript(source: script) {
                    appleScript.executeAndReturnError(&error)
                    if let error {
                        throw QuickActionError.scriptFailed(error.description)
                    }
                }

            case .url(let urlString):
                if let url = URL(string: urlString) {
                    NSWorkspace.shared.open(url)
                }
            }

            logger.debug("action completed id=\(action.id)")
        } catch {
            logger.error("action failed id=\(action.id) error=\(error.localizedDescription)")
        }
    }

    // MARK: - Private

    private func registerBuiltInActions() {
        let builtIn: [QuickAction] = [
            QuickAction(
                id: "builtin_screenshot",
                name: "Screenshot",
                description: "Capture screen and copy to clipboard",
                icon: "camera",
                hotkey: "cmd+shift+3",
                category: .screenshot,
                isBuiltIn: true,
                command: .screenshot
            ),
            QuickAction(
                id: "builtin_screenshot_window",
                name: "Screenshot Window",
                description: "Capture frontmost window",
                icon: "macwindow",
                hotkey: "cmd+shift+4",
                category: .screenshot,
                isBuiltIn: true,
                command: .screenshotWindow
            ),
            QuickAction(
                id: "builtin_copy_context",
                name: "Copy Context",
                description: "Copy current context to clipboard",
                icon: "doc.on.clipboard",
                hotkey: "cmd+shift+c",
                category: .ai,
                isBuiltIn: true,
                command: .copyContext
            ),
            QuickAction(
                id: "builtin_new_chat",
                name: "New Chat",
                description: "Open a new AI chat",
                icon: "message",
                hotkey: "cmd+shift+n",
                category: .ai,
                isBuiltIn: true,
                command: .newChat
            ),
            QuickAction(
                id: "builtin_computer_use",
                name: "Computer Use",
                description: "Start computer use agent",
                icon: "desktopcomputer",
                hotkey: nil,
                category: .ai,
                isBuiltIn: true,
                command: .computerUse
            )
        ]

        for action in builtIn {
            if !actions.contains(where: { $0.id == action.id }) {
                actions.append(action)
            }
        }
    }

    private func updateRecentlyUsed(_ actionId: String) {
        recentlyUsed.removeAll { $0 == actionId }
        recentlyUsed.insert(actionId, at: 0)
        if recentlyUsed.count > 10 {
            recentlyUsed.removeLast()
        }
    }

    private func persistActions() {
        let url = actionsFileURL()
        let customActions = actions.filter { !$0.isBuiltIn }
        do {
            let data = try JSONEncoder().encode(customActions)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist actions: \(error.localizedDescription)")
        }
    }

    func loadActions() {
        let url = actionsFileURL()
        guard FileManager.default.fileExists(atPath: url.path),
              let data = try? Data(contentsOf: url),
              let loaded = try? JSONDecoder().decode([QuickAction].self, from: data) else {
            return
        }
        for action in loaded {
            if !actions.contains(where: { $0.id == action.id }) {
                actions.append(action)
            }
        }
        logger.debug("loaded \(loaded.count) custom actions")
    }

    private func actionsFileURL() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus")
        try? FileManager.default.createDirectory(at: nexusDir, withIntermediateDirectories: true)
        return nexusDir.appendingPathComponent("quickactions.json")
    }
}

enum QuickActionError: LocalizedError {
    case actionNotFound(String)
    case scriptFailed(String)
    case executionFailed(String)

    var errorDescription: String? {
        switch self {
        case .actionNotFound(let id):
            return "Action not found: \(id)"
        case .scriptFailed(let message):
            return "Script failed: \(message)"
        case .executionFailed(let reason):
            return "Execution failed: \(reason)"
        }
    }
}
