import AppKit
import Foundation
import OSLog

// MARK: - Canvas A2UI Handler

/// Singleton handler for Agent-to-UI (A2UI) actions targeting canvas windows.
/// Processes incoming A2UI actions from the gateway, routes them to appropriate canvases,
/// and manages action queuing for offline scenarios.
@MainActor
@Observable
final class CanvasA2UIHandler {
    static let shared = CanvasA2UIHandler()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas-a2ui")

    // MARK: - State

    /// Registered canvas controllers keyed by session ID
    private var canvasControllers: [String: CanvasWindowController] = [:]

    /// Queued actions for sessions that aren't yet registered
    private var pendingActions: [String: [QueuedAction]] = [:]

    /// Action history for debugging (limited size)
    private(set) var actionHistory: [A2UIActionHistoryEntry] = []

    /// Maximum number of history entries to keep
    private let maxHistoryEntries = 100

    /// Maximum number of queued actions per session
    private let maxQueuedActionsPerSession = 50

    /// Gateway event subscription task
    private var subscriptionTask: Task<Void, Never>?

    // MARK: - Types

    /// Queued action with metadata
    private struct QueuedAction {
        let action: A2UIAction
        let queuedAt: Date
        let actionId: String
    }

    /// History entry for debugging
    struct A2UIActionHistoryEntry: Identifiable {
        let id: String
        let sessionId: String
        let action: A2UIAction
        let result: A2UIActionResult?
        let timestamp: Date

        var success: Bool { result?.success ?? false }
    }

    // MARK: - Initialization

    private init() {
        startGatewaySubscription()
    }

    deinit {
        Task { @MainActor [weak self] in
            self?.subscriptionTask?.cancel()
        }
    }

    // MARK: - Canvas Registration

    /// Register a canvas controller for a session
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - controller: The canvas window controller
    func registerCanvas(sessionId: String, controller: CanvasWindowController) {
        canvasControllers[sessionId] = controller
        logger.info("canvas registered sessionId=\(sessionId)")

        // Process any pending actions for this session
        processPendingActions(for: sessionId)
    }

    /// Unregister a canvas controller for a session
    /// - Parameter sessionId: The session identifier
    func unregisterCanvas(sessionId: String) {
        canvasControllers.removeValue(forKey: sessionId)
        pendingActions.removeValue(forKey: sessionId)
        logger.info("canvas unregistered sessionId=\(sessionId)")
    }

    /// Check if a canvas is registered for a session
    /// - Parameter sessionId: The session identifier
    /// - Returns: Whether a canvas is registered
    func hasCanvas(sessionId: String) -> Bool {
        canvasControllers[sessionId] != nil
    }

    // MARK: - Action Handling

    /// Handle a single A2UI action for a session
    /// - Parameters:
    ///   - action: The action to execute
    ///   - sessionId: The target session identifier
    /// - Returns: The action result
    @discardableResult
    func handle(action: A2UIAction, sessionId: String) async -> A2UIActionResult {
        let actionId = UUID().uuidString

        logger.debug("handling action=\(action.actionType) sessionId=\(sessionId) actionId=\(actionId)")

        // Validate the action
        if let validationError = action.validate() {
            let result = A2UIActionResult.failure(action: action, error: validationError.localizedDescription)
            recordHistory(sessionId: sessionId, action: action, result: result, actionId: actionId)
            await emitResult(result, sessionId: sessionId, actionId: actionId)
            return result
        }

        // Check if canvas is available
        guard let controller = canvasControllers[sessionId] else {
            // Queue action for later processing
            queueAction(action, sessionId: sessionId, actionId: actionId)
            let result = A2UIActionResult(
                success: false,
                error: "Canvas not available, action queued",
                action: action
            )
            recordHistory(sessionId: sessionId, action: action, result: result, actionId: actionId)
            return result
        }

        // Execute the action
        let result = await executeAction(action, on: controller)
        recordHistory(sessionId: sessionId, action: action, result: result, actionId: actionId)
        await emitResult(result, sessionId: sessionId, actionId: actionId)

        return result
    }

    /// Handle a batch of A2UI actions for a session
    /// - Parameters:
    ///   - actions: The actions to execute
    ///   - sessionId: The target session identifier
    /// - Returns: Array of action results
    func handleBatch(actions: [A2UIAction], sessionId: String) async -> [A2UIActionResult] {
        let batchId = UUID().uuidString
        logger.info("handling batch count=\(actions.count) sessionId=\(sessionId) batchId=\(batchId)")

        var results: [A2UIActionResult] = []

        for action in actions {
            let result = await handle(action: action, sessionId: sessionId)
            results.append(result)
        }

        return results
    }

    // MARK: - Action Execution

    private func executeAction(_ action: A2UIAction, on controller: CanvasWindowController) async -> A2UIActionResult {
        do {
            switch action {
            case .navigate(let url):
                controller.loadURL(url)
                return .success(action: action)

            case .reload:
                controller.reload()
                return .success(action: action)

            case .close:
                controller.close()
                return .success(action: action)

            case .resize(let width, let height):
                resizeWindow(controller: controller, width: width, height: height)
                return .success(action: action)

            case .move(let x, let y):
                moveWindow(controller: controller, x: x, y: y)
                return .success(action: action)

            case .focus:
                controller.bringToFront()
                return .success(action: action)

            case .blur:
                sendToBack(controller: controller)
                return .success(action: action)

            case .fullscreen(let enabled):
                toggleFullscreen(controller: controller, enabled: enabled)
                return .success(action: action)

            case .screenshot:
                let image = try await controller.snapshot()
                let data = imageToBase64(image)
                return .success(action: action, data: data.map { ["screenshot": AnyCodable($0)] })

            case .executeJS(let script):
                let result = try await controller.eval(javaScript: script)
                let resultData: [String: AnyCodable]? = result.map { ["result": AnyCodable($0)] }
                return .success(action: action, data: resultData)

            case .postMessage(let message, let data):
                await controller.sendMessage(message, data: data)
                return .success(action: action)

            case .setTitle(let title):
                setWindowTitle(controller: controller, title: title)
                return .success(action: action)

            case .showDevTools:
                showDeveloperTools(controller: controller)
                return .success(action: action)

            case .print:
                printCanvas(controller: controller)
                return .success(action: action)
            }
        } catch {
            logger.error("action failed action=\(action.actionType) error=\(error.localizedDescription)")
            return .failure(action: action, error: error.localizedDescription)
        }
    }

    // MARK: - Window Operations

    private func resizeWindow(controller: CanvasWindowController, width: Int, height: Int) {
        guard let window = getWindow(from: controller) else { return }
        var frame = window.frame
        let newSize = NSSize(width: CGFloat(width), height: CGFloat(height))
        frame.size = newSize
        window.setFrame(frame, display: true, animate: true)
        logger.debug("resized window width=\(width) height=\(height)")
    }

    private func moveWindow(controller: CanvasWindowController, x: Int, y: Int) {
        guard let window = getWindow(from: controller) else { return }
        let origin = NSPoint(x: CGFloat(x), y: CGFloat(y))
        window.setFrameOrigin(origin)
        logger.debug("moved window x=\(x) y=\(y)")
    }

    private func sendToBack(controller: CanvasWindowController) {
        guard let window = getWindow(from: controller) else { return }
        window.orderBack(nil)
        logger.debug("sent window to back")
    }

    private func toggleFullscreen(controller: CanvasWindowController, enabled: Bool) {
        guard let window = getWindow(from: controller) else { return }
        let isFullscreen = window.styleMask.contains(.fullScreen)
        if enabled != isFullscreen {
            window.toggleFullScreen(nil)
        }
        logger.debug("fullscreen enabled=\(enabled)")
    }

    private func setWindowTitle(controller: CanvasWindowController, title: String) {
        guard let window = getWindow(from: controller) else { return }
        window.title = title
        logger.debug("set window title=\(title)")
    }

    private func showDeveloperTools(controller: CanvasWindowController) {
        // WebKit developer tools are typically shown via preferences
        // This requires WKWebViewConfiguration.preferences.setValue(true, forKey: "developerExtrasEnabled")
        logger.info("developer tools requested - enable via WebView configuration")
    }

    private func printCanvas(controller: CanvasWindowController) {
        guard let window = getWindow(from: controller) else { return }
        // Trigger print dialog on the window's content view
        if let printView = window.contentView {
            let printInfo = NSPrintInfo.shared
            let printOperation = NSPrintOperation(view: printView, printInfo: printInfo)
            printOperation.showsPrintPanel = true
            printOperation.showsProgressPanel = true
            printOperation.run()
        }
        logger.debug("print dialog shown")
    }

    /// Get the NSWindow from a CanvasWindowController using KVC
    private func getWindow(from controller: CanvasWindowController) -> NSWindow? {
        let mirror = Mirror(reflecting: controller)
        for child in mirror.children {
            if child.label == "window", let window = child.value as? NSWindow {
                return window
            }
        }
        return nil
    }

    private func imageToBase64(_ image: NSImage?) -> String? {
        guard let image,
              let tiffData = image.tiffRepresentation,
              let bitmapRep = NSBitmapImageRep(data: tiffData),
              let pngData = bitmapRep.representation(using: .png, properties: [:]) else {
            return nil
        }
        return pngData.base64EncodedString()
    }

    // MARK: - Action Queuing

    private func queueAction(_ action: A2UIAction, sessionId: String, actionId: String) {
        var queue = pendingActions[sessionId] ?? []

        // Limit queue size
        if queue.count >= maxQueuedActionsPerSession {
            queue.removeFirst()
            logger.warning("queue full, dropped oldest action sessionId=\(sessionId)")
        }

        queue.append(QueuedAction(action: action, queuedAt: Date(), actionId: actionId))
        pendingActions[sessionId] = queue

        logger.debug("action queued sessionId=\(sessionId) queueSize=\(queue.count)")
    }

    private func processPendingActions(for sessionId: String) {
        guard let queue = pendingActions.removeValue(forKey: sessionId), !queue.isEmpty else { return }

        logger.info("processing pending actions sessionId=\(sessionId) count=\(queue.count)")

        Task {
            for queued in queue {
                // Skip actions older than 5 minutes
                if Date().timeIntervalSince(queued.queuedAt) > 300 {
                    logger.debug("skipping stale action actionId=\(queued.actionId)")
                    continue
                }

                await handle(action: queued.action, sessionId: sessionId)
            }
        }
    }

    // MARK: - History

    private func recordHistory(sessionId: String, action: A2UIAction, result: A2UIActionResult?, actionId: String) {
        let entry = A2UIActionHistoryEntry(
            id: actionId,
            sessionId: sessionId,
            action: action,
            result: result,
            timestamp: Date()
        )

        actionHistory.append(entry)

        // Trim history if needed
        if actionHistory.count > maxHistoryEntries {
            actionHistory.removeFirst(actionHistory.count - maxHistoryEntries)
        }
    }

    /// Clear action history
    func clearHistory() {
        actionHistory.removeAll()
        logger.debug("action history cleared")
    }

    /// Get history entries for a specific session
    func history(for sessionId: String) -> [A2UIActionHistoryEntry] {
        actionHistory.filter { $0.sessionId == sessionId }
    }

    // MARK: - Gateway Integration

    private func startGatewaySubscription() {
        subscriptionTask = Task { [weak self] in
            let stream = await GatewayConnection.shared.subscribe()

            for await push in stream {
                if Task.isCancelled { return }

                await MainActor.run {
                    self?.handleGatewayPush(push)
                }
            }
        }

        logger.info("gateway subscription started")
    }

    private func handleGatewayPush(_ push: GatewayPush) {
        switch push {
        case .event(let frame) where frame.event == "canvas.a2ui":
            handleA2UIEvent(frame.payload)

        case .event(let frame) where frame.event == "canvas.a2ui.batch":
            handleA2UIBatchEvent(frame.payload)

        default:
            break
        }
    }

    private func handleA2UIEvent(_ payload: Data?) {
        guard let payload,
              let json = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let sessionId = json["sessionId"] as? String,
              let actionJSON = json["action"] as? [String: Any],
              let action = A2UIAction.fromJSON(actionJSON) else {
            logger.warning("invalid a2ui event payload")
            return
        }

        logger.debug("received a2ui event sessionId=\(sessionId) action=\(action.actionType)")

        Task {
            await handle(action: action, sessionId: sessionId)
        }
    }

    private func handleA2UIBatchEvent(_ payload: Data?) {
        guard let payload,
              let json = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let batch = A2UIActionBatch.fromJSON(json) else {
            logger.warning("invalid a2ui batch event payload")
            return
        }

        logger.debug("received a2ui batch sessionId=\(batch.sessionId) count=\(batch.actions.count)")

        Task {
            await handleBatch(actions: batch.actions, sessionId: batch.sessionId)
        }
    }

    private func emitResult(_ result: A2UIActionResult, sessionId: String, actionId: String) async {
        do {
            var params: [String: AnyHashable] = [
                "sessionId": sessionId,
                "actionId": actionId,
                "success": result.success,
                "timestamp": result.timestamp.timeIntervalSince1970,
                "actionType": result.action.actionType
            ]

            if let error = result.error {
                params["error"] = error
            }

            // Note: result.data is not included as it may contain non-hashable values
            // For screenshot results, the data is typically large and should be handled separately

            _ = try await ControlChannel.shared.request(
                method: "canvas.a2ui.result",
                params: params
            )

            logger.debug("result emitted actionId=\(actionId) success=\(result.success)")
        } catch {
            logger.warning("failed to emit result: \(error.localizedDescription)")
        }
    }

    // MARK: - Debug

    /// Get debug information about current state
    var debugInfo: [String: Any] {
        [
            "registeredCanvases": Array(canvasControllers.keys),
            "pendingActionCounts": pendingActions.mapValues(\.count),
            "historyCount": actionHistory.count,
            "subscriptionActive": subscriptionTask != nil && !subscriptionTask!.isCancelled
        ]
    }
}

// MARK: - Convenience Extensions

extension CanvasA2UIHandler {
    /// Execute a navigate action
    func navigate(sessionId: String, to url: URL) async -> A2UIActionResult {
        await handle(action: .navigate(url: url), sessionId: sessionId)
    }

    /// Execute a reload action
    func reload(sessionId: String) async -> A2UIActionResult {
        await handle(action: .reload, sessionId: sessionId)
    }

    /// Execute a close action
    func close(sessionId: String) async -> A2UIActionResult {
        await handle(action: .close, sessionId: sessionId)
    }

    /// Execute a focus action
    func focus(sessionId: String) async -> A2UIActionResult {
        await handle(action: .focus, sessionId: sessionId)
    }

    /// Execute JavaScript on a canvas
    func executeJS(sessionId: String, script: String) async -> A2UIActionResult {
        await handle(action: .executeJS(script: script), sessionId: sessionId)
    }

    /// Post a message to a canvas
    func postMessage(sessionId: String, message: String, data: [String: Any]? = nil) async -> A2UIActionResult {
        await handle(action: .postMessage(message: message, data: data), sessionId: sessionId)
    }

    /// Take a screenshot of a canvas
    func screenshot(sessionId: String) async -> A2UIActionResult {
        await handle(action: .screenshot, sessionId: sessionId)
    }
}
