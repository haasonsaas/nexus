import AppKit
import Foundation

/// Manager that connects HotkeyService to AppModel and handles action execution
@MainActor
final class HotkeyManager: ObservableObject {
    static let shared = HotkeyManager()

    /// Reference to the app model for executing actions
    weak var appModel: AppModel?

    /// Reference to the hotkey service
    private let hotkeyService = HotkeyService.shared

    /// The currently selected node for computer use operations
    @Published var selectedNodeEdgeId: String?

    /// Last executed action result
    @Published private(set) var lastActionResult: HotkeyActionResult?

    /// Whether an action is currently executing
    @Published private(set) var isExecuting: Bool = false

    /// Status message for UI feedback
    @Published private(set) var statusMessage: String?

    private init() {
        setupHotkeyHandler()
    }

    // MARK: - Setup

    /// Configure the hotkey handler to execute actions
    private func setupHotkeyHandler() {
        hotkeyService.onHotkeyTriggered = { [weak self] action in
            Task { @MainActor in
                await self?.executeAction(action)
            }
        }
    }

    /// Set the app model reference
    func configure(with model: AppModel) {
        self.appModel = model
    }

    // MARK: - Action Execution

    /// Execute a hotkey action
    func executeAction(_ action: HotkeyAction) async {
        guard let model = appModel else {
            updateStatus("No app model configured", isError: true)
            return
        }

        guard let nodeEdgeId = selectedNodeEdgeId else {
            updateStatus("No node selected for hotkey action", isError: true)
            NotificationService.shared.sendNotification(
                title: "Hotkey Action Failed",
                body: "Please select a node in Computer Use first",
                category: .error
            )
            return
        }

        // Check if the node is still available
        guard model.nodes.contains(where: { $0.edgeId == nodeEdgeId && $0.tools.contains("nodes.computer_use") }) else {
            updateStatus("Selected node is no longer available", isError: true)
            return
        }

        isExecuting = true
        defer { isExecuting = false }

        let startTime = Date()

        do {
            let result = try await performAction(action, edgeId: nodeEdgeId, model: model)
            let duration = Date().timeIntervalSince(startTime)

            lastActionResult = HotkeyActionResult(
                action: action,
                success: !result.isError,
                message: result.content,
                duration: duration,
                timestamp: Date()
            )

            updateStatus("\(action.displayName) completed", isError: false)

            // Notify success
            NotificationService.shared.sendNotification(
                title: "Hotkey: \(action.displayName)",
                body: result.isError ? "Action failed" : "Action completed",
                category: .toolComplete
            )
        } catch {
            lastActionResult = HotkeyActionResult(
                action: action,
                success: false,
                message: error.localizedDescription,
                duration: Date().timeIntervalSince(startTime),
                timestamp: Date()
            )

            updateStatus("Action failed: \(error.localizedDescription)", isError: true)
        }
    }

    /// Perform the actual action via the model
    private func performAction(_ action: HotkeyAction, edgeId: String, model: AppModel) async throws -> ToolInvocationResult {
        var payload: [String: Any] = [:]

        switch action {
        case .screenshot:
            payload["action"] = "screenshot"

        case .click:
            // Get current cursor position first, then click
            payload["action"] = "left_click"
            // Note: click at current position doesn't require coordinates
            // The remote tool will use the current mouse position

        case .cursorPosition:
            payload["action"] = "cursor_position"

        case .typeClipboard:
            payload["action"] = "type"
            // Get clipboard contents
            let clipboard = NSPasteboard.general
            if let text = clipboard.string(forType: .string) {
                payload["text"] = text
            } else {
                throw HotkeyError.noClipboardContent
            }
        }

        guard let result = await model.invokeTool(
            edgeID: edgeId,
            toolName: "nodes.computer_use",
            payload: payload,
            approved: true
        ) else {
            throw HotkeyError.toolInvocationFailed(model.lastError ?? "Unknown error")
        }

        if result.isError {
            throw HotkeyError.toolInvocationFailed(result.content)
        }

        return result
    }

    /// Update status message
    private func updateStatus(_ message: String, isError: Bool) {
        statusMessage = message
        if isError {
            print("HotkeyManager: \(message)")
        }

        // Clear status after delay
        Task {
            try? await Task.sleep(nanoseconds: 3_000_000_000) // 3 seconds
            if statusMessage == message {
                statusMessage = nil
            }
        }
    }

    // MARK: - Convenience Accessors

    /// Get all bindings from the service
    var bindings: [HotkeyBinding] {
        hotkeyService.bindings
    }

    /// Whether global hotkeys are enabled
    var globalHotkeysEnabled: Bool {
        get { hotkeyService.globalHotkeysEnabled }
        set { hotkeyService.globalHotkeysEnabled = newValue }
    }

    /// Last triggered action
    var lastTriggeredAction: HotkeyAction? {
        hotkeyService.lastTriggeredAction
    }

    /// Check accessibility permission
    func checkAccessibilityPermission() -> Bool {
        hotkeyService.checkAccessibilityPermission()
    }

    /// Is accessibility granted
    func isAccessibilityGranted() -> Bool {
        hotkeyService.isAccessibilityGranted()
    }

    /// Update a binding
    func updateBinding(_ binding: HotkeyBinding) {
        hotkeyService.updateBinding(binding)
    }

    /// Set enabled state for an action
    func setEnabled(_ enabled: Bool, for action: HotkeyAction) {
        hotkeyService.setEnabled(enabled, for: action)
    }

    /// Get binding for action
    func binding(for action: HotkeyAction) -> HotkeyBinding? {
        hotkeyService.binding(for: action)
    }

    /// Reset to defaults
    func resetToDefaults() {
        hotkeyService.resetToDefaults()
    }
}

// MARK: - Supporting Types

/// Result of a hotkey action execution
struct HotkeyActionResult: Identifiable {
    let id = UUID()
    let action: HotkeyAction
    let success: Bool
    let message: String
    let duration: TimeInterval
    let timestamp: Date

    var durationString: String {
        if duration < 1 {
            return String(format: "%.0fms", duration * 1000)
        }
        return String(format: "%.1fs", duration)
    }
}

/// Errors that can occur during hotkey action execution
enum HotkeyError: LocalizedError {
    case noNodeSelected
    case nodeNotAvailable
    case noClipboardContent
    case toolInvocationFailed(String)

    var errorDescription: String? {
        switch self {
        case .noNodeSelected:
            return "No node selected for computer use"
        case .nodeNotAvailable:
            return "Selected node is no longer available"
        case .noClipboardContent:
            return "No text content in clipboard"
        case .toolInvocationFailed(let message):
            return "Tool invocation failed: \(message)"
        }
    }
}
