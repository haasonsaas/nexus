import AppKit
import Foundation
import OSLog
import WebKit

/// Presentation mode for canvas windows
enum CanvasPresentation {
    case window
    case panel
}

/// A borderless panel that can accept key focus
final class CanvasPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }
}

/// Target specification for loading canvas content
struct CanvasTarget {
    let sessionId: String
    let path: String

    /// Create a target for a session's index
    static func index(session: String) -> CanvasTarget {
        CanvasTarget(sessionId: session, path: "index.html")
    }

    /// Create a target for a specific file
    static func file(session: String, path: String) -> CanvasTarget {
        CanvasTarget(sessionId: session, path: path)
    }
}

/// Controller for individual canvas windows with WebKit rendering.
/// Supports custom URL scheme, file watching, and live reload.
@MainActor
final class CanvasWindowController: NSObject {
    private let sessionId: String
    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas.window")
    private let baseDirectory: URL
    private let presentation: CanvasPresentation

    private var window: NSWindow?
    private var webView: WKWebView?
    private var schemeHandler: CanvasSchemeHandler?
    private var fileWatcher: CanvasFileWatcher?

    private(set) var currentTarget: CanvasTarget?
    private(set) var isVisible: Bool = false

    var onAction: ((String, [String: Any]?) -> Void)?
    var onClose: (() -> Void)?
    var onVisibilityChanged: ((Bool) -> Void)?

    /// Enable live reload when files change
    var liveReloadEnabled: Bool = true {
        didSet {
            updateFileWatcher()
        }
    }

    // MARK: - Initialization

    init(
        sessionId: String,
        title: String = "Canvas",
        size: CGSize = CGSize(width: 800, height: 600),
        baseDirectory: URL? = nil,
        presentation: CanvasPresentation = .window
    ) {
        self.sessionId = sessionId
        self.presentation = presentation

        // Default base directory is Application Support/Nexus/sessions
        if let baseDirectory {
            self.baseDirectory = baseDirectory
        } else {
            let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
            self.baseDirectory = appSupport.appendingPathComponent("Nexus/sessions")
        }

        super.init()
        setupWindow(title: title, size: size)
        setupFileWatcher()
    }

    // MARK: - Window Setup

    private func setupWindow(title: String, size: CGSize) {
        // Create scheme handler
        schemeHandler = CanvasSchemeHandler(baseDirectory: baseDirectory)

        // Configure WebView
        let config = WKWebViewConfiguration()
        config.preferences.isElementFullscreenEnabled = true

        // Register custom URL scheme handler
        if let handler = schemeHandler {
            config.setURLSchemeHandler(handler, forURLScheme: CanvasScheme.scheme)
        }

        // Setup message handler for JavaScript bridge
        let userContentController = WKUserContentController()
        userContentController.add(self, name: "nexusCanvas")
        config.userContentController = userContentController

        // Inject the JavaScript bridge
        let bridgeScript = WKUserScript(
            source: javaScriptBridge,
            injectionTime: .atDocumentStart,
            forMainFrameOnly: false
        )
        userContentController.addUserScript(bridgeScript)

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.translatesAutoresizingMaskIntoConstraints = false
        webView.navigationDelegate = self
        self.webView = webView

        // Create window based on presentation mode
        let window: NSWindow
        switch presentation {
        case .window:
            window = NSWindow(
                contentRect: NSRect(origin: .zero, size: size),
                styleMask: [.titled, .closable, .miniaturizable, .resizable],
                backing: .buffered,
                defer: false
            )
            window.minSize = NSSize(width: 400, height: 300)

        case .panel:
            let panel = CanvasPanel(
                contentRect: NSRect(origin: .zero, size: size),
                styleMask: [.titled, .closable, .resizable, .nonactivatingPanel],
                backing: .buffered,
                defer: false
            )
            panel.isFloatingPanel = true
            panel.level = .floating
            panel.hidesOnDeactivate = false
            panel.minSize = NSSize(width: 400, height: 300)
            window = panel
        }

        window.title = title
        window.contentView = webView
        window.delegate = self
        window.isReleasedWhenClosed = false

        self.window = window
    }

    private func setupFileWatcher() {
        let watcher = CanvasFileWatcher()
        watcher.onFilesChanged = { [weak self] in
            guard let self, self.liveReloadEnabled else { return }
            self.logger.debug("files changed, triggering reload")
            self.reload()
        }
        fileWatcher = watcher
    }

    private func updateFileWatcher() {
        guard let target = currentTarget else {
            fileWatcher?.stop()
            return
        }

        if liveReloadEnabled {
            let sessionDir = baseDirectory
                .appendingPathComponent(target.sessionId)
                .appendingPathComponent("canvas")
            fileWatcher?.watch(path: sessionDir.path)
        } else {
            fileWatcher?.stop()
        }
    }

    // MARK: - Public Methods

    /// Show the canvas window
    func showCanvas() {
        guard let window else { return }

        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)

        isVisible = true
        onVisibilityChanged?(true)

        logger.debug("canvas shown sessionId=\(self.sessionId)")
    }

    /// Hide the canvas window
    func hideCanvas() {
        window?.orderOut(nil)
        isVisible = false
        onVisibilityChanged?(false)

        logger.debug("canvas hidden sessionId=\(self.sessionId)")
    }

    /// Close the canvas window
    func close() {
        fileWatcher?.stop()
        window?.close()
        window = nil
        webView = nil
        isVisible = false
        onClose?()

        logger.debug("canvas closed sessionId=\(self.sessionId)")
    }

    /// Bring the canvas window to front
    func bringToFront() {
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    /// Load a canvas target (session file or path)
    func load(target: CanvasTarget) {
        currentTarget = target

        guard let url = CanvasScheme.makeURL(session: target.sessionId, path: target.path) else {
            logger.error("failed to create URL for target session=\(target.sessionId) path=\(target.path)")
            return
        }

        webView?.load(URLRequest(url: url))
        updateFileWatcher()

        logger.info("loading target session=\(target.sessionId) path=\(target.path)")
    }

    /// Reload the current content
    func reload() {
        webView?.reload()
        logger.debug("reloading content")
    }

    /// Load raw HTML content
    func loadHTML(_ html: String) {
        webView?.loadHTMLString(html, baseURL: nil)
        fileWatcher?.stop()

        logger.debug("loaded HTML content")
    }

    /// Load a URL directly
    func loadURL(_ url: URL) {
        webView?.load(URLRequest(url: url))
        fileWatcher?.stop()

        logger.debug("loaded URL=\(url.absoluteString)")
    }

    /// Evaluate JavaScript in the canvas
    /// - Parameter javaScript: The JavaScript code to execute
    /// - Returns: The result of the evaluation
    func eval(javaScript: String) async throws -> Any? {
        guard let webView else {
            throw CanvasError.webViewNotReady
        }

        return try await webView.evaluateJavaScript(javaScript)
    }

    /// Send a message to the canvas JavaScript
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

    /// Take a snapshot of the current canvas content
    /// - Returns: An NSImage of the canvas content
    func snapshot() async throws -> NSImage? {
        guard let webView else {
            throw CanvasError.webViewNotReady
        }

        return try await withCheckedThrowingContinuation { continuation in
            let config = WKSnapshotConfiguration()

            webView.takeSnapshot(with: config) { image, error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume(returning: image)
                }
            }
        }
    }

    /// Get the session directory path for this canvas
    func sessionDirectory() -> URL {
        baseDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent("canvas")
    }

    // MARK: - JavaScript Bridge

    private var javaScriptBridge: String {
        """
        window.nexus = {
            send: function(action, payload) {
                window.webkit.messageHandlers.nexusCanvas.postMessage({
                    action: action,
                    payload: payload || {}
                });
            },
            close: function() {
                window.nexus.send('close');
            },
            reload: function() {
                window.nexus.send('reload');
            },
            ready: function() {
                window.nexus.send('ready');
            },
            log: function(message) {
                window.nexus.send('log', { message: message });
            }
        };

        // Auto-notify when DOM is ready
        if (document.readyState === 'complete') {
            window.nexus.ready();
        } else {
            window.addEventListener('load', function() {
                window.nexus.ready();
            });
        }
        """
    }
}

// MARK: - NSWindowDelegate

extension CanvasWindowController: NSWindowDelegate {
    func windowWillClose(_ notification: Notification) {
        isVisible = false
        onVisibilityChanged?(false)
        onClose?()
    }

    func windowDidBecomeKey(_ notification: Notification) {
        isVisible = true
        onVisibilityChanged?(true)
    }

    func windowDidResignKey(_ notification: Notification) {
        // Window is still visible, just not key
    }
}

// MARK: - WKScriptMessageHandler

extension CanvasWindowController: WKScriptMessageHandler {
    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.name == "nexusCanvas",
              let body = message.body as? [String: Any],
              let action = body["action"] as? String else {
            return
        }

        let payload = body["payload"] as? [String: Any]

        logger.debug("received action=\(action)")

        // Handle built-in actions
        switch action {
        case "close":
            close()
        case "reload":
            reload()
        case "ready":
            logger.debug("canvas ready")
        case "log":
            if let msg = payload?["message"] as? String {
                logger.debug("canvas log: \(msg)")
            }
        default:
            // Forward to external handler
            onAction?(action, payload)
        }
    }
}

// MARK: - WKNavigationDelegate

extension CanvasWindowController: WKNavigationDelegate {
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        logger.debug("navigation finished")
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        logger.error("navigation failed: \(error.localizedDescription)")
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        logger.error("provisional navigation failed: \(error.localizedDescription)")

        // Show error page
        let errorHTML = CanvasSchemeHandler.errorHTML(
            title: "Load Failed",
            message: error.localizedDescription
        )
        webView.loadHTMLString(errorHTML, baseURL: nil)
    }

    func webView(
        _ webView: WKWebView,
        decidePolicyFor navigationAction: WKNavigationAction,
        decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
    ) {
        guard let url = navigationAction.request.url else {
            decisionHandler(.allow)
            return
        }

        // Allow canvas scheme URLs
        if url.scheme == CanvasScheme.scheme {
            decisionHandler(.allow)
            return
        }

        // Allow about: URLs (for blank pages, etc.)
        if url.scheme == "about" {
            decisionHandler(.allow)
            return
        }

        // Allow data: URLs
        if url.scheme == "data" {
            decisionHandler(.allow)
            return
        }

        // For external URLs, open in default browser
        if url.scheme == "http" || url.scheme == "https" {
            if navigationAction.navigationType == .linkActivated {
                NSWorkspace.shared.open(url)
                decisionHandler(.cancel)
                return
            }
        }

        decisionHandler(.allow)
    }
}

// MARK: - Canvas Errors

enum CanvasError: LocalizedError {
    case canvasNotFound(String)
    case loadFailed(String)
    case webViewNotReady
    case snapshotFailed

    var errorDescription: String? {
        switch self {
        case .canvasNotFound(let id):
            return "Canvas not found: \(id)"
        case .loadFailed(let reason):
            return "Canvas load failed: \(reason)"
        case .webViewNotReady:
            return "WebView is not ready"
        case .snapshotFailed:
            return "Failed to take snapshot"
        }
    }
}
