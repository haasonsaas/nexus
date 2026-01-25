import AppKit
import OSLog
import SwiftUI

/// Controller for the voice wake overlay panel.
/// Shows live transcription near the menu bar during voice input.
@MainActor
@Observable
final class VoiceWakeOverlayController {
    static let shared = VoiceWakeOverlayController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voicewake.overlay")

    /// Window level just below popup menus
    static let preferredWindowLevel = NSWindow.Level(rawValue: NSWindow.Level.popUpMenu.rawValue - 4)

    // MARK: - Model

    struct Model {
        var text: String = ""
        var isFinal: Bool = false
        var isVisible: Bool = false
        var forwardEnabled: Bool = false
        var isSending: Bool = false
        var isOverflowing: Bool = false
        var isEditing: Bool = false
        var level: Double = 0
    }

    enum Source: String {
        case wakeWord
        case pushToTalk
    }

    private(set) var model = Model()
    var isVisible: Bool { model.isVisible }

    // MARK: - Window State

    private var window: NSPanel?
    private var hostingView: NSHostingView<VoiceWakeOverlayView>?
    private var autoSendTask: Task<Void, Never>?
    private var activeToken: UUID?
    private var activeSource: Source?

    // MARK: - Layout Constants

    private let width: CGFloat = 360
    private let padding: CGFloat = 10
    private let maxHeight: CGFloat = 400
    private let minHeight: CGFloat = 48

    private init() {}

    // MARK: - Public API

    /// Show the overlay with the given source
    func show(source: Source) {
        guard !model.isVisible else { return }

        let token = UUID()
        activeToken = token
        activeSource = source

        model = Model(isVisible: true)

        createWindowIfNeeded()
        positionWindow()
        window?.orderFront(nil)

        logger.debug("overlay shown for \(source.rawValue)")
    }

    /// Hide the overlay
    func hide() {
        guard model.isVisible else { return }

        model.isVisible = false
        activeToken = nil
        activeSource = nil
        autoSendTask?.cancel()
        autoSendTask = nil

        window?.orderOut(nil)

        logger.debug("overlay hidden")
    }

    /// Update the transcript text
    func updateText(_ text: String, isFinal: Bool = false) {
        model.text = text
        model.isFinal = isFinal
        model.forwardEnabled = !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty

        updateWindowFrame()

        // Auto-send after final transcript
        if isFinal && model.forwardEnabled {
            scheduleAutoSend()
        }
    }

    /// Update the audio level (0-1)
    func updateLevel(_ level: Double) {
        model.level = max(0, min(1, level))
    }

    /// User requested to send the command
    func requestSend() {
        guard model.forwardEnabled, !model.isSending else { return }

        autoSendTask?.cancel()
        model.isSending = true

        let command = model.text.trimmingCharacters(in: .whitespacesAndNewlines)

        Task {
            await sendCommand(command)
            model.isSending = false
            hide()
        }
    }

    /// User began editing the transcript
    func userBeganEditing() {
        autoSendTask?.cancel()
        model.isEditing = true
    }

    /// User ended editing
    func endEditing() {
        model.isEditing = false
    }

    /// Cancel editing and dismiss
    func cancelEditingAndDismiss() {
        model.isEditing = false
        hide()
    }

    // MARK: - Private Methods

    private func createWindowIfNeeded() {
        guard window == nil else { return }

        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: width, height: minHeight),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )

        panel.level = Self.preferredWindowLevel
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.isMovableByWindowBackground = true
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

        let hosting = NSHostingView(rootView: VoiceWakeOverlayView(controller: self))
        hosting.frame = panel.contentView?.bounds ?? .zero
        hosting.autoresizingMask = [.width, .height]
        panel.contentView?.addSubview(hosting)

        self.window = panel
        self.hostingView = hosting

        logger.debug("overlay window created")
    }

    private func positionWindow() {
        guard let window, let screen = NSScreen.main else { return }

        // Position below menu bar, centered horizontally
        let menuBarHeight: CGFloat = 24
        let margin: CGFloat = 8

        let x = (screen.frame.width - width) / 2
        let y = screen.frame.maxY - menuBarHeight - window.frame.height - margin

        window.setFrameOrigin(NSPoint(x: x, y: y))
    }

    private func updateWindowFrame(animate: Bool = false) {
        guard let window else { return }

        // Calculate required height based on text
        let textHeight = calculateTextHeight()
        let newHeight = min(maxHeight, max(minHeight, textHeight + padding * 2))

        let frame = NSRect(
            x: window.frame.minX,
            y: window.frame.maxY - newHeight,
            width: width,
            height: newHeight
        )

        if animate {
            NSAnimationContext.runAnimationGroup { context in
                context.duration = 0.15
                context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
                window.animator().setFrame(frame, display: true)
            }
        } else {
            window.setFrame(frame, display: true)
        }

        model.isOverflowing = textHeight > maxHeight - padding * 2
    }

    private func calculateTextHeight() -> CGFloat {
        let font = NSFont.systemFont(ofSize: 14)
        let constraintWidth = width - padding * 2 - 50 // Account for button

        let attributedString = NSAttributedString(
            string: model.text,
            attributes: [.font: font]
        )

        let textContainer = NSTextContainer(
            containerSize: NSSize(width: constraintWidth, height: .greatestFiniteMagnitude)
        )
        textContainer.lineFragmentPadding = 0

        let layoutManager = NSLayoutManager()
        layoutManager.addTextContainer(textContainer)

        let textStorage = NSTextStorage(attributedString: attributedString)
        textStorage.addLayoutManager(layoutManager)

        layoutManager.ensureLayout(for: textContainer)

        return layoutManager.usedRect(for: textContainer).height
    }

    private func scheduleAutoSend() {
        autoSendTask?.cancel()
        autoSendTask = Task {
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            requestSend()
        }
    }

    private func sendCommand(_ command: String) async {
        logger.info("sending voice command: \(command)")

        // Create a new chat session with the voice command
        let session = SessionBridge.shared.createSession(type: .voice)

        // Send to the gateway
        do {
            try await ControlChannel.shared.send(
                method: "voice.command",
                params: [
                    "sessionId": session.id,
                    "command": command,
                    "source": activeSource?.rawValue ?? "unknown",
                ]
            )
        } catch {
            logger.error("failed to send voice command: \(error.localizedDescription)")
        }
    }
}
