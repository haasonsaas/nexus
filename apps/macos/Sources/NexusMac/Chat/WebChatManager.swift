import AppKit
import Foundation

/// A borderless panel that can still accept key focus (needed for typing).
final class WebChatPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }
}

enum WebChatPresentation {
    case window
    case panel(anchorProvider: () -> NSRect?)

    var isPanel: Bool {
        if case .panel = self { return true }
        return false
    }
}

@MainActor
final class WebChatManager {
    static let shared = WebChatManager()

    private var windowController: WebChatWindowController?
    private var windowSessionKey: String?
    private var panelController: WebChatWindowController?
    private var panelSessionKey: String?
    private var cachedPreferredSessionKey: String?

    var onPanelVisibilityChanged: ((Bool) -> Void)?

    var activeSessionKey: String? {
        panelSessionKey ?? windowSessionKey
    }

    func openChat(for sessionId: String, withMessage message: String? = nil) {
        show(sessionKey: sessionId)
        Task { @MainActor in
            try? await ChatSessionManager.shared.switchSession(sessionId)
            if let message, !message.isEmpty {
                try? await ChatSessionManager.shared.send(content: message)
            }
        }
    }

    func show(sessionKey: String) {
        closePanel()
        if let controller = windowController {
            if windowSessionKey == sessionKey {
                controller.show()
                return
            }

            controller.close()
            windowController = nil
            windowSessionKey = nil
        }
        let controller = WebChatWindowController(sessionKey: sessionKey, presentation: .window)
        controller.onVisibilityChanged = { [weak self] visible in
            self?.onPanelVisibilityChanged?(visible)
        }
        windowController = controller
        windowSessionKey = sessionKey
        controller.show()
    }

    func togglePanel(sessionKey: String, anchorProvider: @escaping () -> NSRect?) {
        if let controller = panelController {
            if panelSessionKey != sessionKey {
                controller.close()
                panelController = nil
                panelSessionKey = nil
            } else {
                if controller.isVisible {
                    controller.close()
                } else {
                    controller.presentAnchored(anchorProvider: anchorProvider)
                }
                return
            }
        }

        let controller = WebChatWindowController(
            sessionKey: sessionKey,
            presentation: .panel(anchorProvider: anchorProvider))
        controller.onClosed = { [weak self] in
            self?.panelHidden()
        }
        controller.onVisibilityChanged = { [weak self] visible in
            self?.onPanelVisibilityChanged?(visible)
        }
        panelController = controller
        panelSessionKey = sessionKey
        controller.presentAnchored(anchorProvider: anchorProvider)
    }

    func closePanel() {
        panelController?.close()
    }

    func preferredSessionKey() async -> String {
        if let cachedPreferredSessionKey { return cachedPreferredSessionKey }
        let key = WorkActivityStore.shared.mainSessionKey
        cachedPreferredSessionKey = key
        return key
    }

    func resetTunnels() {
        windowController?.close()
        windowController = nil
        windowSessionKey = nil
        panelController?.close()
        panelController = nil
        panelSessionKey = nil
        cachedPreferredSessionKey = nil
    }

    func close() {
        windowController?.close()
        windowController = nil
        windowSessionKey = nil
        panelController?.close()
        panelController = nil
        panelSessionKey = nil
        cachedPreferredSessionKey = nil
    }

    private func panelHidden() {
        onPanelVisibilityChanged?(false)
        // Keep panel controller cached so reopening doesn't re-bootstrap.
    }
}
