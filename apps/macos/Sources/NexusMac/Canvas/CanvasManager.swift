import AppKit
import Foundation
import OSLog
import WebKit

/// Manages interactive HTML/JavaScript canvas windows.
/// Provides WebKit-based rendering for agent interfaces.
@MainActor
@Observable
final class CanvasManager {
    static let shared = CanvasManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas")

    private(set) var activeCanvases: [String: CanvasWindowController] = [:]
    private(set) var isDebugEnabled = false

    var onCanvasAction: ((CanvasAction) -> Void)?

    struct CanvasAction {
        let canvasId: String
        let action: String
        let payload: [String: Any]?
    }

    // MARK: - Canvas Lifecycle

    /// Open a canvas window with HTML content
    func open(canvasId: String, html: String, title: String? = nil, size: CGSize? = nil) {
        if let existing = activeCanvases[canvasId] {
            existing.bringToFront()
            return
        }

        let controller = CanvasWindowController(
            canvasId: canvasId,
            title: title ?? "Canvas",
            size: size ?? CGSize(width: 800, height: 600)
        )

        controller.onAction = { [weak self] action, payload in
            self?.handleAction(canvasId: canvasId, action: action, payload: payload)
        }

        controller.onClose = { [weak self] in
            self?.activeCanvases.removeValue(forKey: canvasId)
        }

        controller.loadHTML(html)
        controller.show()

        activeCanvases[canvasId] = controller
        logger.info("canvas opened id=\(canvasId)")
    }

    /// Open a canvas window with a URL
    func openURL(canvasId: String, url: URL, title: String? = nil, size: CGSize? = nil) {
        if let existing = activeCanvases[canvasId] {
            existing.bringToFront()
            return
        }

        let controller = CanvasWindowController(
            canvasId: canvasId,
            title: title ?? "Canvas",
            size: size ?? CGSize(width: 800, height: 600)
        )

        controller.onAction = { [weak self] action, payload in
            self?.handleAction(canvasId: canvasId, action: action, payload: payload)
        }

        controller.onClose = { [weak self] in
            self?.activeCanvases.removeValue(forKey: canvasId)
        }

        controller.loadURL(url)
        controller.show()

        activeCanvases[canvasId] = controller
        logger.info("canvas opened from URL id=\(canvasId) url=\(url.absoluteString)")
    }

    /// Close a canvas window
    func close(canvasId: String) {
        guard let controller = activeCanvases[canvasId] else { return }
        controller.close()
        activeCanvases.removeValue(forKey: canvasId)
        logger.info("canvas closed id=\(canvasId)")
    }

    /// Close all canvas windows
    func closeAll() {
        for (_, controller) in activeCanvases {
            controller.close()
        }
        activeCanvases.removeAll()
    }

    /// Send message to canvas JavaScript
    func sendMessage(canvasId: String, message: String, data: [String: Any]? = nil) async {
        guard let controller = activeCanvases[canvasId] else { return }
        await controller.sendMessage(message, data: data)
    }

    /// Execute JavaScript in canvas
    func executeJS(canvasId: String, script: String) async throws -> Any? {
        guard let controller = activeCanvases[canvasId] else {
            throw CanvasError.canvasNotFound(canvasId)
        }
        return try await controller.executeJS(script)
    }

    // MARK: - Private

    private func handleAction(canvasId: String, action: String, payload: [String: Any]?) {
        logger.debug("canvas action id=\(canvasId) action=\(action)")

        let canvasAction = CanvasAction(
            canvasId: canvasId,
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
                        "canvasId": canvasId,
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

/// Controller for individual canvas windows
@MainActor
final class CanvasWindowController: NSObject {
    private let canvasId: String
    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas.window")

    private var window: NSWindow?
    private var webView: WKWebView?

    var onAction: ((String, [String: Any]?) -> Void)?
    var onClose: (() -> Void)?

    init(canvasId: String, title: String, size: CGSize) {
        self.canvasId = canvasId
        super.init()
        setupWindow(title: title, size: size)
    }

    private func setupWindow(title: String, size: CGSize) {
        // Configure WebView
        let config = WKWebViewConfiguration()
        config.preferences.isElementFullscreenEnabled = true

        let userContentController = WKUserContentController()
        userContentController.add(self, name: "nexusCanvas")
        config.userContentController = userContentController

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.translatesAutoresizingMaskIntoConstraints = false
        self.webView = webView

        // Create window
        let window = NSWindow(
            contentRect: NSRect(origin: .zero, size: size),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = title
        window.contentView = webView
        window.minSize = NSSize(width: 400, height: 300)
        window.delegate = self
        window.isReleasedWhenClosed = false

        self.window = window
    }

    func show() {
        window?.center()
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func bringToFront() {
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func close() {
        window?.close()
        window = nil
        webView = nil
    }

    func loadHTML(_ html: String) {
        let wrappedHTML = wrapWithBridge(html)
        webView?.loadHTMLString(wrappedHTML, baseURL: nil)
    }

    func loadURL(_ url: URL) {
        webView?.load(URLRequest(url: url))
    }

    func sendMessage(_ message: String, data: [String: Any]? = nil) async {
        var dataJSON = "{}"
        if let data, let jsonData = try? JSONSerialization.data(withJSONObject: data),
           let json = String(data: jsonData, encoding: .utf8) {
            dataJSON = json
        }

        let script = """
        if (window.onNexusMessage) {
            window.onNexusMessage('\(message)', \(dataJSON));
        }
        """

        _ = try? await webView?.evaluateJavaScript(script)
    }

    func executeJS(_ script: String) async throws -> Any? {
        try await webView?.evaluateJavaScript(script)
    }

    private func wrapWithBridge(_ html: String) -> String {
        let bridge = """
        <script>
        window.nexus = {
            send: function(action, payload) {
                window.webkit.messageHandlers.nexusCanvas.postMessage({
                    action: action,
                    payload: payload || {}
                });
            },
            close: function() {
                window.nexus.send('close');
            }
        };
        </script>
        """

        if html.contains("<head>") {
            return html.replacingOccurrences(of: "<head>", with: "<head>\(bridge)")
        } else if html.contains("<html>") {
            return html.replacingOccurrences(of: "<html>", with: "<html><head>\(bridge)</head>")
        } else {
            return "\(bridge)\(html)"
        }
    }
}

extension CanvasWindowController: NSWindowDelegate {
    func windowWillClose(_ notification: Notification) {
        onClose?()
    }
}

extension CanvasWindowController: WKScriptMessageHandler {
    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.name == "nexusCanvas",
              let body = message.body as? [String: Any],
              let action = body["action"] as? String else {
            return
        }

        let payload = body["payload"] as? [String: Any]
        onAction?(action, payload)
    }
}

enum CanvasError: LocalizedError {
    case canvasNotFound(String)
    case loadFailed(String)

    var errorDescription: String? {
        switch self {
        case .canvasNotFound(let id):
            return "Canvas not found: \(id)"
        case .loadFailed(let reason):
            return "Canvas load failed: \(reason)"
        }
    }
}
