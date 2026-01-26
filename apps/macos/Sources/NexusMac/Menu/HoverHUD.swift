import AppKit
import OSLog
import SwiftUI

/// Quick-access floating HUD that appears when hovering over the menu bar icon.
/// Provides instant access to chat without clicking.
@MainActor
@Observable
final class HoverHUDController {
    static let shared = HoverHUDController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "hoverhud")

    // MARK: - State

    private(set) var isVisible = false
    private(set) var isSuppressed = false

    private var window: NSPanel?
    private var showTask: Task<Void, Never>?
    private var hideTask: Task<Void, Never>?

    private let showDelay: TimeInterval = 0.4
    private let hideDelay: TimeInterval = 0.3

    private init() {}

    // MARK: - Public API

    /// Called when mouse enters the status item
    func statusItemHoverChanged(inside: Bool, anchorProvider: @escaping () -> NSRect?) {
        if inside {
            scheduleShow(anchorProvider: anchorProvider)
        } else {
            scheduleHide()
        }
    }

    /// Suppress the HUD (e.g., when menu is open)
    func setSuppressed(_ suppressed: Bool) {
        isSuppressed = suppressed
        if suppressed {
            hide(reason: "suppressed")
        }
    }

    /// Dismiss the HUD
    func dismiss(reason: String) {
        hide(reason: reason)
    }

    // MARK: - Show/Hide

    private func scheduleShow(anchorProvider: @escaping () -> NSRect?) {
        hideTask?.cancel()
        hideTask = nil

        guard !isSuppressed, !isVisible else { return }

        showTask?.cancel()
        showTask = Task {
            try? await Task.sleep(for: .seconds(showDelay))

            guard !Task.isCancelled, !isSuppressed else { return }

            show(anchorProvider: anchorProvider)
        }
    }

    private func scheduleHide() {
        showTask?.cancel()
        showTask = nil

        guard isVisible else { return }

        hideTask?.cancel()
        hideTask = Task {
            try? await Task.sleep(for: .seconds(hideDelay))

            guard !Task.isCancelled else { return }

            hide(reason: "hover-exit")
        }
    }

    private func show(anchorProvider: @escaping () -> NSRect?) {
        guard !isVisible, !isSuppressed else { return }

        createWindowIfNeeded()

        guard let window, let anchor = anchorProvider() else { return }

        positionWindow(below: anchor)

        window.alphaValue = 0
        window.orderFront(nil)

        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.2
            context.timingFunction = CAMediaTimingFunction(name: .easeOut)
            window.animator().alphaValue = 1
        }

        isVisible = true
        logger.debug("hover HUD shown")
    }

    private func hide(reason: String) {
        showTask?.cancel()
        showTask = nil
        hideTask?.cancel()
        hideTask = nil

        guard isVisible, let window else { return }

        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.15
            context.timingFunction = CAMediaTimingFunction(name: .easeIn)
            window.animator().alphaValue = 0
        } completionHandler: { [weak self] in
            window.orderOut(nil)
            self?.isVisible = false
        }

        logger.debug("hover HUD hidden: \(reason)")
    }

    // MARK: - Window

    private func createWindowIfNeeded() {
        guard window == nil else { return }

        let content = HoverHUDView(controller: self)
        let hosting = NSHostingController(rootView: content)

        let panel = NSPanel(contentViewController: hosting)
        panel.styleMask = [.borderless, .nonactivatingPanel]
        panel.level = .popUpMenu
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

        self.window = panel

        logger.debug("hover HUD window created")
    }

    private func positionWindow(below anchor: NSRect) {
        guard let window else { return }

        let margin: CGFloat = 4
        let width: CGFloat = 280
        let height: CGFloat = 200

        // Position centered below the status item
        let x = anchor.midX - width / 2
        let y = anchor.minY - height - margin

        window.setFrame(NSRect(x: x, y: y, width: width, height: height), display: true)
    }

    /// Called when mouse enters the HUD window
    func hudMouseEntered() {
        hideTask?.cancel()
        hideTask = nil
    }

    /// Called when mouse exits the HUD window
    func hudMouseExited() {
        scheduleHide()
    }
}

// MARK: - Hover HUD View

struct HoverHUDView: View {
    let controller: HoverHUDController

    @State private var inputText = ""
    @FocusState private var isInputFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            // Quick input field
            HStack(spacing: 10) {
                Image(systemName: "sparkle")
                    .font(.system(size: 14))
                    .foregroundStyle(.secondary)

                TextField("Ask Nexus...", text: $inputText)
                    .textFieldStyle(.plain)
                    .font(.system(size: 14))
                    .focused($isInputFocused)
                    .onSubmit {
                        sendMessage()
                    }

                if !inputText.isEmpty {
                    Button {
                        sendMessage()
                    } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 20))
                            .foregroundStyle(Color.accentColor)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.textBackgroundColor))
            )
            .padding(12)

            Divider()
                .padding(.horizontal, 12)

            // Quick actions
            VStack(spacing: 2) {
                HoverHUDActionButton(
                    title: "New Chat",
                    icon: "message",
                    action: {
                        controller.dismiss(reason: "action")
                        let session = SessionBridge.shared.createSession(type: .chat)
                        WebChatManager.shared.openChat(for: session.id)
                    }
                )

                HoverHUDActionButton(
                    title: "Voice Input",
                    icon: "mic",
                    action: {
                        controller.dismiss(reason: "action")
                        VoiceWakeOverlayController.shared.show(source: .pushToTalk)
                    }
                )

                HoverHUDActionButton(
                    title: "Settings",
                    icon: "gear",
                    action: {
                        controller.dismiss(reason: "action")
                        NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
                    }
                )
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 8)
        }
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(.ultraThinMaterial)
                .shadow(color: .black.opacity(0.2), radius: 20, x: 0, y: 10)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(Color.white.opacity(0.1), lineWidth: 1)
        )
        .onHover { inside in
            if inside {
                controller.hudMouseEntered()
            } else {
                controller.hudMouseExited()
            }
        }
        .onAppear {
            // Auto-focus the input
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
                isInputFocused = true
            }
        }
    }

    private func sendMessage() {
        guard !inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }

        let message = inputText
        inputText = ""

        controller.dismiss(reason: "send")

        // Create session and send message
        Task {
            let session = SessionBridge.shared.createSession(type: .chat)
            WebChatManager.shared.openChat(for: session.id, withMessage: message)
        }
    }
}

struct HoverHUDActionButton: View {
    let title: String
    let icon: String
    let action: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: icon)
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
                    .frame(width: 20)

                Text(title)
                    .font(.system(size: 13))

                Spacer()
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isHovered ? Color.accentColor.opacity(0.15) : Color.clear)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.1)) {
                isHovered = hovering
            }
        }
    }
}

#Preview {
    HoverHUDView(controller: HoverHUDController.shared)
        .frame(width: 280)
        .padding()
        .background(Color.gray.opacity(0.2))
}
