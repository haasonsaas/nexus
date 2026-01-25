import AppKit
import Foundation
import OSLog
import WebKit

/// Controls a web-based chat interface window or panel.
@MainActor
final class WebChatWindowController {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "webchat")
    private let sessionKey: String
    private let presentation: WebChatPresentation

    private var window: NSWindow?
    private var webView: WKWebView?

    var onClosed: (() -> Void)?
    var onVisibilityChanged: ((Bool) -> Void)?

    var isVisible: Bool {
        window?.isVisible ?? false
    }

    init(sessionKey: String, presentation: WebChatPresentation) {
        self.sessionKey = sessionKey
        self.presentation = presentation
    }

    func show() {
        ensureWindow()
        guard let window else { return }

        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        onVisibilityChanged?(true)
    }

    func presentAnchored(anchorProvider: @escaping () -> NSRect?) {
        ensureWindow()
        guard let window else { return }

        if let anchor = anchorProvider() {
            // Position below the anchor (menu bar item)
            let windowSize = window.frame.size
            let origin = CGPoint(
                x: anchor.midX - windowSize.width / 2,
                y: anchor.minY - windowSize.height - 4
            )
            window.setFrameOrigin(origin)
        } else {
            window.center()
        }

        window.makeKeyAndOrderFront(nil)
        onVisibilityChanged?(true)
    }

    func close() {
        window?.close()
        window = nil
        webView = nil
        onClosed?()
        onVisibilityChanged?(false)
    }

    // MARK: - Private

    private func ensureWindow() {
        guard window == nil else { return }

        let config = WKWebViewConfiguration()
        config.preferences.isElementFullscreenEnabled = true

        let webView = WKWebView(frame: .zero, configuration: config)
        webView.translatesAutoresizingMaskIntoConstraints = false
        self.webView = webView

        let window: NSWindow
        let size = NSSize(width: 420, height: 600)

        switch presentation {
        case .window:
            window = NSWindow(
                contentRect: NSRect(origin: .zero, size: size),
                styleMask: [.titled, .closable, .miniaturizable, .resizable],
                backing: .buffered,
                defer: false
            )
            window.title = "Nexus Chat"
            window.minSize = NSSize(width: 320, height: 400)

        case .panel:
            let panel = WebChatPanel(
                contentRect: NSRect(origin: .zero, size: size),
                styleMask: [.titled, .closable, .resizable, .nonactivatingPanel],
                backing: .buffered,
                defer: false
            )
            panel.isFloatingPanel = true
            panel.level = .floating
            panel.hidesOnDeactivate = false
            panel.title = "Nexus Chat"
            panel.minSize = NSSize(width: 320, height: 400)
            window = panel
        }

        window.contentView = webView
        window.isReleasedWhenClosed = false

        // Load chat URL
        loadChatURL()

        self.window = window
        logger.debug("webchat window created sessionKey=\(self.sessionKey)")
    }

    private func loadChatURL() {
        guard let webView else { return }

        // Build chat URL from gateway endpoint
        Task {
            do {
                let baseURL = try await resolveGatewayURL()
                let chatURL = baseURL.appendingPathComponent("chat")
                    .appendingPathComponent(sessionKey)
                webView.load(URLRequest(url: chatURL))
                logger.debug("webchat loading url=\(chatURL.absoluteString)")
            } catch {
                logger.error("webchat failed to resolve gateway URL: \(error.localizedDescription)")
                // Load error page
                webView.loadHTMLString(errorHTML(error), baseURL: nil)
            }
        }
    }

    private func resolveGatewayURL() async throws -> URL {
        let port = GatewayEnvironment.gatewayPort()
        guard let url = URL(string: "http://localhost:\(port)") else {
            throw URLError(.badURL)
        }
        return url
    }

    private func errorHTML(_ error: Error) -> String {
        """
        <!DOCTYPE html>
        <html>
        <head>
            <style>
                body {
                    font-family: -apple-system, BlinkMacSystemFont, sans-serif;
                    display: flex;
                    align-items: center;
                    justify-content: center;
                    height: 100vh;
                    margin: 0;
                    background: #f5f5f5;
                    color: #333;
                }
                .error {
                    text-align: center;
                    padding: 40px;
                }
                h1 { font-size: 24px; margin-bottom: 16px; }
                p { color: #666; }
            </style>
        </head>
        <body>
            <div class="error">
                <h1>Unable to Connect</h1>
                <p>\(error.localizedDescription)</p>
            </div>
        </body>
        </html>
        """
    }
}
