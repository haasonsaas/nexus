import AppKit
import OSLog
import SwiftUI

// MARK: - Approval Prompt View

/// SwiftUI view for approval prompts
struct ExecApprovalPromptView: View {
    let request: ExecApprovalSocket.ApprovalRequest
    let onDecision: (ApprovalSocketMessage.ApprovalDecision) -> Void

    @State private var showDetails = false
    @State private var countdown: Int = 60
    @State private var countdownTimer: Timer?

    var body: some View {
        VStack(spacing: 16) {
            headerSection
            commandSection
            detailsSection
            Divider()
            buttonSection
        }
        .padding()
        .frame(width: 420)
        .onAppear {
            startCountdown()
        }
        .onDisappear {
            countdownTimer?.invalidate()
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        HStack(spacing: 12) {
            Image(systemName: "terminal.fill")
                .font(.system(size: 28))
                .foregroundStyle(.orange)

            VStack(alignment: .leading, spacing: 2) {
                Text("Command Approval")
                    .font(.headline)

                Text("An agent wants to run a command")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Countdown badge
            Text("\(countdown)s")
                .font(.caption.monospacedDigit())
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(
                    Capsule()
                        .fill(countdown <= 10 ? Color.red.opacity(0.2) : Color.secondary.opacity(0.15))
                )
                .foregroundStyle(countdown <= 10 ? .red : .secondary)
        }
    }

    // MARK: - Command Section

    private var commandSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Command")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)

            ScrollView(.horizontal, showsIndicators: false) {
                Text(request.command)
                    .font(.system(.body, design: .monospaced))
                    .textSelection(.enabled)
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(NSColor.textBackgroundColor))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .strokeBorder(Color.secondary.opacity(0.2), lineWidth: 1)
            )
        }
    }

    // MARK: - Details Section

    @ViewBuilder
    private var detailsSection: some View {
        if let workingDir = request.workingDirectory {
            DisclosureGroup(isExpanded: $showDetails) {
                VStack(alignment: .leading, spacing: 8) {
                    detailRow(title: "Working Directory", value: workingDir)
                    detailRow(title: "Request ID", value: String(request.id.prefix(8)))
                    detailRow(title: "Requested", value: formatTime(request.requestedAt))
                }
                .padding(.top, 8)
            } label: {
                HStack {
                    Image(systemName: "info.circle")
                        .foregroundStyle(.secondary)
                    Text("Details")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func detailRow(title: String, value: String) -> some View {
        HStack(alignment: .top) {
            Text(title + ":")
                .font(.caption)
                .foregroundStyle(.tertiary)
                .frame(width: 120, alignment: .leading)

            Text(value)
                .font(.caption)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)

            Spacer()
        }
    }

    private func formatTime(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.timeStyle = .medium
        return formatter.string(from: date)
    }

    // MARK: - Buttons

    private var buttonSection: some View {
        HStack(spacing: 12) {
            Button(role: .destructive) {
                onDecision(.deny)
            } label: {
                Text("Deny")
                    .frame(minWidth: 60)
            }
            .buttonStyle(.bordered)
            .keyboardShortcut(.escape, modifiers: [])

            Spacer()

            Button {
                onDecision(.allow)
            } label: {
                Text("Allow Once")
                    .frame(minWidth: 80)
            }
            .buttonStyle(.bordered)
            .keyboardShortcut("a", modifiers: [.command])

            Button {
                onDecision(.allowAlways)
            } label: {
                Text("Always Allow")
                    .frame(minWidth: 90)
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.return, modifiers: [])
        }
    }

    // MARK: - Countdown

    private func startCountdown() {
        countdown = max(1, Int(60 - Date().timeIntervalSince(request.requestedAt)))
        countdownTimer = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { timer in
            if countdown > 0 {
                countdown -= 1
            } else {
                timer.invalidate()
                onDecision(.deny)
            }
        }
    }
}

// MARK: - Approval Prompter

/// Presents approval prompts as floating windows
@MainActor
final class ExecApprovalPrompter {
    static let shared = ExecApprovalPrompter()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "approval-prompter")
    private var windows: [String: NSWindow] = [:]
    private var windowDelegates: [String: WindowCloseDelegate] = [:]

    private init() {}

    // MARK: - Show Prompt

    func showPrompt(for request: ExecApprovalSocket.ApprovalRequest) {
        // Check if window already exists for this request
        guard windows[request.id] == nil else {
            logger.debug("Window already exists for request: \(request.id.prefix(8), privacy: .public)")
            return
        }

        logger.info("Showing prompt for: \(request.command.prefix(50), privacy: .public)")

        let view = ExecApprovalPromptView(request: request) { [weak self] decision in
            ExecApprovalSocket.shared.respondToRequest(request.id, decision: decision)
            self?.dismissPrompt(requestId: request.id)
        }

        let hostingView = NSHostingView(rootView: view)
        hostingView.translatesAutoresizingMaskIntoConstraints = false

        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 420, height: 280),
            styleMask: [.titled, .closable, .nonactivatingPanel, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )

        panel.title = "Command Approval"
        panel.titlebarAppearsTransparent = true
        panel.titleVisibility = .hidden
        panel.contentView = hostingView
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.isMovableByWindowBackground = true
        panel.backgroundColor = NSColor.windowBackgroundColor

        // Position window
        positionWindow(panel)

        // Handle window close
        let delegate = WindowCloseDelegate(requestId: request.id) { [weak self] requestId in
            ExecApprovalSocket.shared.respondToRequest(requestId, decision: .deny)
            self?.windows.removeValue(forKey: requestId)
            self?.windowDelegates.removeValue(forKey: requestId)
        }
        panel.delegate = delegate

        panel.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)

        windows[request.id] = panel
        windowDelegates[request.id] = delegate

        // Play notification sound
        NSSound.beep()
    }

    // MARK: - Position

    private func positionWindow(_ window: NSWindow) {
        guard let screen = NSScreen.main else {
            window.center()
            return
        }

        let screenFrame = screen.visibleFrame
        let windowFrame = window.frame

        // Position in top-right corner with padding
        let x = screenFrame.maxX - windowFrame.width - 20
        let y = screenFrame.maxY - windowFrame.height - 20

        window.setFrameOrigin(NSPoint(x: x, y: y))
    }

    // MARK: - Dismiss

    func dismissPrompt(requestId: String) {
        guard let window = windows.removeValue(forKey: requestId) else { return }
        windowDelegates.removeValue(forKey: requestId)
        window.close()
        logger.debug("Dismissed prompt for: \(requestId.prefix(8), privacy: .public)")
    }

    func dismissAll() {
        for (requestId, window) in windows {
            window.close()
            // Auto-deny dismissed requests
            ExecApprovalSocket.shared.respondToRequest(requestId, decision: .deny)
        }
        windows.removeAll()
        windowDelegates.removeAll()
        logger.info("Dismissed all prompts")
    }

    // MARK: - Query

    var activePromptCount: Int {
        windows.count
    }

    func hasPrompt(for requestId: String) -> Bool {
        windows[requestId] != nil
    }
}

// MARK: - Window Close Delegate

private final class WindowCloseDelegate: NSObject, NSWindowDelegate {
    let requestId: String
    let onClose: (String) -> Void

    init(requestId: String, onClose: @escaping (String) -> Void) {
        self.requestId = requestId
        self.onClose = onClose
    }

    func windowWillClose(_ notification: Notification) {
        onClose(requestId)
    }
}

// MARK: - Mini Prompt View (for menu bar)

struct ExecApprovalMiniView: View {
    let requests: [ExecApprovalSocket.ApprovalRequest]

    var body: some View {
        if requests.isEmpty {
            EmptyState()
        } else {
            VStack(spacing: 0) {
                ForEach(requests) { request in
                    MiniRequestRow(request: request)
                    if request.id != requests.last?.id {
                        Divider()
                    }
                }
            }
        }
    }

    private struct EmptyState: View {
        var body: some View {
            VStack(spacing: 8) {
                Image(systemName: "checkmark.shield")
                    .font(.title2)
                    .foregroundStyle(.green)
                Text("No pending approvals")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding()
            .frame(maxWidth: .infinity)
        }
    }

    private struct MiniRequestRow: View {
        let request: ExecApprovalSocket.ApprovalRequest

        var body: some View {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Image(systemName: "terminal")
                        .foregroundStyle(.orange)

                    Text(commandPreview)
                        .font(.subheadline.monospaced())
                        .lineLimit(1)
                        .truncationMode(.middle)

                    Spacer()

                    Text(timeAgo)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                HStack(spacing: 8) {
                    Button("Deny") {
                        ExecApprovalSocket.shared.respondToRequest(request.id, decision: .deny)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)

                    Button("Allow") {
                        ExecApprovalSocket.shared.respondToRequest(request.id, decision: .allow)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)

                    Button("Always") {
                        ExecApprovalSocket.shared.respondToRequest(request.id, decision: .allowAlways)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
        }

        private var commandPreview: String {
            let cmd = request.command
            if cmd.count > 40 {
                return String(cmd.prefix(37)) + "..."
            }
            return cmd
        }

        private var timeAgo: String {
            let interval = Date().timeIntervalSince(request.requestedAt)
            if interval < 60 {
                return "\(Int(interval))s"
            }
            return "\(Int(interval / 60))m"
        }
    }
}

// MARK: - Previews

#Preview("Approval Prompt") {
    ExecApprovalPromptView(
        request: ExecApprovalSocket.ApprovalRequest(
            id: "test-123",
            command: "npm install express body-parser cors helmet",
            workingDirectory: "/Users/developer/projects/my-app",
            connectionId: UUID(),
            requestedAt: Date()
        ),
        onDecision: { decision in
            print("Decision: \(decision)")
        }
    )
}

#Preview("Mini View - With Requests") {
    ExecApprovalMiniView(requests: [
        ExecApprovalSocket.ApprovalRequest(
            id: "1",
            command: "git push origin main",
            workingDirectory: "/Users/dev/project",
            connectionId: UUID(),
            requestedAt: Date().addingTimeInterval(-30)
        ),
        ExecApprovalSocket.ApprovalRequest(
            id: "2",
            command: "npm run build",
            workingDirectory: "/Users/dev/webapp",
            connectionId: UUID(),
            requestedAt: Date().addingTimeInterval(-120)
        ),
    ])
    .frame(width: 300)
}

#Preview("Mini View - Empty") {
    ExecApprovalMiniView(requests: [])
        .frame(width: 300)
}
