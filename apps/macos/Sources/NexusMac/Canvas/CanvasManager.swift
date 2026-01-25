import AppKit
import Foundation
import OSLog
import WebKit

/// Singleton manager for canvas windows across sessions.
/// Provides centralized control over agent-generated artifact rendering.
@MainActor
@Observable
final class CanvasManager {
    static let shared = CanvasManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas.manager")

    /// Active canvas windows keyed by session ID
    private(set) var canvasBySession: [String: CanvasWindowController] = [:]

    /// Debug mode for additional logging
    private(set) var isDebugEnabled = false

    /// Base directory for session content
    private var baseDirectory: URL

    /// Callback for canvas actions from JavaScript
    var onCanvasAction: ((CanvasAction) -> Void)?

    // MARK: - Initialization

    private init() {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        baseDirectory = appSupport.appendingPathComponent("Nexus/sessions")

        // Ensure base directory exists
        try? FileManager.default.createDirectory(at: baseDirectory, withIntermediateDirectories: true)
    }

    // MARK: - Types

    /// Represents an action received from canvas JavaScript
    struct CanvasAction {
        let sessionId: String
        let action: String
        let payload: [String: Any]?
    }

    // MARK: - Session Tracking

    /// All active session IDs with open canvases
    var activeSessions: [String] {
        Array(canvasBySession.keys)
    }

    /// Number of active canvases
    var activeCount: Int {
        canvasBySession.count
    }

    /// Check if a session has an active canvas
    func hasCanvas(for sessionId: String) -> Bool {
        canvasBySession[sessionId] != nil
    }

    // MARK: - Canvas Lifecycle

    /// Create or get an existing canvas for a session
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - title: Optional window title
    ///   - size: Optional window size
    ///   - presentation: Window or panel presentation mode
    /// - Returns: The canvas window controller
    @discardableResult
    func create(
        sessionId: String,
        title: String? = nil,
        size: CGSize? = nil,
        presentation: CanvasPresentation = .window
    ) -> CanvasWindowController {
        // Return existing canvas if present
        if let existing = canvasBySession[sessionId] {
            logger.debug("returning existing canvas sessionId=\(sessionId)")
            return existing
        }

        let controller = CanvasWindowController(
            sessionId: sessionId,
            title: title ?? "Canvas - \(sessionId)",
            size: size ?? CGSize(width: 800, height: 600),
            baseDirectory: baseDirectory,
            presentation: presentation
        )

        controller.onAction = { [weak self] action, payload in
            self?.handleAction(sessionId: sessionId, action: action, payload: payload)
        }

        controller.onClose = { [weak self] in
            self?.canvasBySession.removeValue(forKey: sessionId)
            self?.logger.info("canvas removed from tracking sessionId=\(sessionId)")
        }

        canvasBySession[sessionId] = controller
        logger.info("canvas created sessionId=\(sessionId)")

        return controller
    }

    /// Get an existing canvas for a session
    /// - Parameter sessionId: The session identifier
    /// - Returns: The canvas controller if it exists
    func get(sessionId: String) -> CanvasWindowController? {
        canvasBySession[sessionId]
    }

    /// Get or create a canvas for a session
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - title: Optional window title (only used if creating)
    /// - Returns: The canvas window controller
    func getOrCreate(sessionId: String, title: String? = nil) -> CanvasWindowController {
        if let existing = canvasBySession[sessionId] {
            return existing
        }
        return create(sessionId: sessionId, title: title)
    }

    /// Close and remove a canvas for a session
    /// - Parameter sessionId: The session identifier
    func close(sessionId: String) {
        guard let controller = canvasBySession[sessionId] else {
            logger.debug("no canvas to close sessionId=\(sessionId)")
            return
        }

        controller.close()
        canvasBySession.removeValue(forKey: sessionId)
        logger.info("canvas closed sessionId=\(sessionId)")
    }

    /// Close all canvas windows
    func closeAll() {
        for (sessionId, controller) in canvasBySession {
            controller.close()
            logger.debug("closing canvas sessionId=\(sessionId)")
        }
        canvasBySession.removeAll()
        logger.info("all canvases closed count=\(self.canvasBySession.count)")
    }

    // MARK: - Canvas Operations

    /// Open a canvas with HTML content
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - html: The HTML content to display
    ///   - title: Optional window title
    ///   - size: Optional window size
    func open(sessionId: String, html: String, title: String? = nil, size: CGSize? = nil) {
        let controller = create(sessionId: sessionId, title: title, size: size)
        controller.loadHTML(html)
        controller.showCanvas()

        logger.info("canvas opened with HTML sessionId=\(sessionId)")
    }

    /// Open a canvas with a URL
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - url: The URL to load
    ///   - title: Optional window title
    ///   - size: Optional window size
    func openURL(sessionId: String, url: URL, title: String? = nil, size: CGSize? = nil) {
        let controller = create(sessionId: sessionId, title: title, size: size)
        controller.loadURL(url)
        controller.showCanvas()

        logger.info("canvas opened with URL sessionId=\(sessionId) url=\(url.absoluteString)")
    }

    /// Open a canvas with session content (using custom URL scheme)
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - path: The path within the session's canvas directory
    ///   - title: Optional window title
    ///   - size: Optional window size
    func openSessionContent(
        sessionId: String,
        path: String = "index.html",
        title: String? = nil,
        size: CGSize? = nil
    ) {
        let controller = create(sessionId: sessionId, title: title, size: size)
        let target = CanvasTarget(sessionId: sessionId, path: path)
        controller.load(target: target)
        controller.showCanvas()

        logger.info("canvas opened with session content sessionId=\(sessionId) path=\(path)")
    }

    /// Show a canvas window (if it exists)
    /// - Parameter sessionId: The session identifier
    func show(sessionId: String) {
        guard let controller = canvasBySession[sessionId] else {
            logger.debug("no canvas to show sessionId=\(sessionId)")
            return
        }
        controller.showCanvas()
    }

    /// Hide a canvas window (if it exists)
    /// - Parameter sessionId: The session identifier
    func hide(sessionId: String) {
        guard let controller = canvasBySession[sessionId] else {
            logger.debug("no canvas to hide sessionId=\(sessionId)")
            return
        }
        controller.hideCanvas()
    }

    /// Bring a canvas to front
    /// - Parameter sessionId: The session identifier
    func bringToFront(sessionId: String) {
        guard let controller = canvasBySession[sessionId] else {
            logger.debug("no canvas to bring to front sessionId=\(sessionId)")
            return
        }
        controller.bringToFront()
    }

    /// Reload a canvas
    /// - Parameter sessionId: The session identifier
    func reload(sessionId: String) {
        guard let controller = canvasBySession[sessionId] else {
            logger.debug("no canvas to reload sessionId=\(sessionId)")
            return
        }
        controller.reload()
    }

    // MARK: - JavaScript Evaluation

    /// Send a message to a canvas
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - message: The message name
    ///   - data: Optional message data
    func sendMessage(sessionId: String, message: String, data: [String: Any]? = nil) async {
        guard let controller = canvasBySession[sessionId] else {
            logger.debug("no canvas to send message sessionId=\(sessionId)")
            return
        }
        await controller.sendMessage(message, data: data)
    }

    /// Execute JavaScript in a canvas
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - script: The JavaScript to execute
    /// - Returns: The result of the evaluation
    func executeJS(sessionId: String, script: String) async throws -> Any? {
        guard let controller = canvasBySession[sessionId] else {
            throw CanvasError.canvasNotFound(sessionId)
        }
        return try await controller.eval(javaScript: script)
    }

    /// Take a snapshot of a canvas
    /// - Parameter sessionId: The session identifier
    /// - Returns: An NSImage of the canvas content
    func snapshot(sessionId: String) async throws -> NSImage? {
        guard let controller = canvasBySession[sessionId] else {
            throw CanvasError.canvasNotFound(sessionId)
        }
        return try await controller.snapshot()
    }

    // MARK: - Configuration

    /// Set the base directory for session content
    /// - Parameter directory: The new base directory
    func setBaseDirectory(_ directory: URL) {
        baseDirectory = directory
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        logger.info("base directory set to \(directory.path)")
    }

    /// Get the session directory for a given session
    /// - Parameter sessionId: The session identifier
    /// - Returns: The URL to the session's canvas directory
    func sessionDirectory(for sessionId: String) -> URL {
        baseDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent("canvas")
    }

    /// Enable or disable debug mode
    /// - Parameter enabled: Whether debug mode should be enabled
    func setDebugEnabled(_ enabled: Bool) {
        isDebugEnabled = enabled
        logger.info("debug mode \(enabled ? "enabled" : "disabled")")
    }

    /// Set live reload for a specific session's canvas
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - enabled: Whether live reload should be enabled
    func setLiveReload(sessionId: String, enabled: Bool) {
        guard let controller = canvasBySession[sessionId] else { return }
        controller.liveReloadEnabled = enabled
    }

    // MARK: - Private

    private func handleAction(sessionId: String, action: String, payload: [String: Any]?) {
        logger.debug("canvas action sessionId=\(sessionId) action=\(action)")

        let canvasAction = CanvasAction(
            sessionId: sessionId,
            action: action,
            payload: payload
        )

        onCanvasAction?(canvasAction)

        // Forward to gateway if connected
        Task {
            do {
                _ = try await ControlChannel.shared.request(
                    method: "canvas.action",
                    params: [
                        "sessionId": sessionId,
                        "action": action,
                        "payload": payload ?? [:]
                    ] as [String: AnyHashable]
                )
            } catch {
                logger.warning("failed to forward canvas action: \(error.localizedDescription)")
            }
        }
    }
}

// MARK: - Convenience Extensions

extension CanvasManager {
    /// Open a canvas for the default/main session
    func openDefault(html: String, title: String = "Canvas") {
        open(sessionId: "default", html: html, title: title)
    }

    /// Check if any canvases are visible
    var hasVisibleCanvases: Bool {
        canvasBySession.values.contains { $0.isVisible }
    }

    /// Get all visible canvases
    var visibleSessions: [String] {
        canvasBySession.filter { $0.value.isVisible }.map { $0.key }
    }
}
