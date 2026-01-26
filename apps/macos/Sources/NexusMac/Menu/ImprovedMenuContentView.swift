import SwiftUI

/// Improved menu content view with better states and visual polish.
struct ImprovedMenuContentView: View {
    @Bindable var appState: AppStateStore
    @State private var connectionCoordinator = ConnectionModeCoordinator.shared
    @State private var permissions = PermissionManager.shared

    var onOpenChat: () -> Void
    var onOpenSettings: () -> Void
    var onTogglePause: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            // Status header with connection info
            statusHeader
                .padding(.horizontal, 12)
                .padding(.vertical, 10)

            Divider()

            // Quick actions
            quickActionsSection

            Divider()

            // Active sessions
            sessionsSection

            Divider()

            // Bottom actions
            bottomActionsSection
        }
        .frame(width: 320)
    }

    // MARK: - Status Header

    private var statusHeader: some View {
        HStack(spacing: 12) {
            // Status icon with animation
            ZStack {
                Circle()
                    .fill(statusColor.opacity(0.2))
                    .frame(width: 40, height: 40)

                Image(systemName: statusIcon)
                    .font(.system(size: 18))
                    .foregroundStyle(statusColor)
                    .symbolEffect(.pulse, isActive: connectionCoordinator.isConnecting)
            }

            VStack(alignment: .leading, spacing: 2) {
                Text("Nexus")
                    .font(.headline)

                HStack(spacing: 6) {
                    StatusBadge(status: connectionStatus, variant: .animated)

                    Text(connectionCoordinator.statusDescription)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer()

            // Pause/Resume button
            Button {
                withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) {
                    onTogglePause()
                }
            } label: {
                Image(systemName: appState.isPaused ? "play.fill" : "pause.fill")
                    .font(.system(size: 14))
                    .foregroundStyle(appState.isPaused ? .green : .secondary)
                    .frame(width: 28, height: 28)
                    .background(
                        Circle()
                            .fill(Color(NSColor.controlBackgroundColor))
                    )
            }
            .buttonStyle(.plain)
            .help(appState.isPaused ? "Resume" : "Pause")
        }
    }

    private var statusIcon: String {
        if appState.isPaused {
            return "pause.circle.fill"
        }
        switch connectionCoordinator.state {
        case .connected:
            return "checkmark.circle.fill"
        case .connecting, .reconnecting:
            return "arrow.triangle.2.circlepath"
        case .error:
            return "exclamationmark.triangle.fill"
        case .disconnected:
            return "circle.slash"
        }
    }

    private var statusColor: Color {
        if appState.isPaused {
            return .secondary
        }
        switch connectionCoordinator.state {
        case .connected:
            return .green
        case .connecting, .reconnecting:
            return .blue
        case .error:
            return .red
        case .disconnected:
            return .secondary
        }
    }

    private var connectionStatus: StatusBadge.Status {
        if appState.isPaused {
            return .offline
        }
        switch connectionCoordinator.state {
        case .connected:
            return .online
        case .connecting, .reconnecting:
            return .connecting
        case .error:
            return .error
        case .disconnected:
            return .offline
        }
    }

    // MARK: - Quick Actions

    private var quickActionsSection: some View {
        VStack(spacing: 0) {
            MenuActionButton(
                title: "New Chat",
                icon: "message",
                shortcut: "N",
                action: onOpenChat
            )

            MenuActionButton(
                title: "Voice Input",
                icon: "mic",
                shortcut: nil,
                isDisabled: !appState.voiceWakeEnabled || !permissions.voiceWakePermissionsGranted,
                action: {
                    VoiceWakeOverlayController.shared.show(source: .pushToTalk)
                }
            )

            MenuActionButton(
                title: "Screenshot",
                icon: "camera.viewfinder",
                shortcut: nil,
                action: {
                    Task {
                        _ = try? await ScreenCaptureService.shared.capture()
                    }
                }
            )
        }
    }

    // MARK: - Sessions

    private var sessionsSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Section header
            HStack {
                Text("Sessions")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                Spacer()
                Text("\(SessionBridge.shared.activeSessions.count)")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 8)

            let sessions = Array(SessionBridge.shared.activeSessions.prefix(5))

            if sessions.isEmpty {
                HStack {
                    Image(systemName: "tray")
                        .foregroundStyle(.tertiary)
                    Text("No active sessions")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
            } else {
                ForEach(sessions) { session in
                    ImprovedMenuSessionRow(session: session)
                }
            }
        }
    }

    // MARK: - Bottom Actions

    private var bottomActionsSection: some View {
        VStack(spacing: 0) {
            MenuActionButton(
                title: "Settings...",
                icon: "gear",
                shortcut: ",",
                action: onOpenSettings
            )

            Divider()
                .padding(.vertical, 4)

            MenuActionButton(
                title: "Quit Nexus",
                icon: "power",
                shortcut: "Q",
                isDestructive: true,
                action: {
                    NSApp.terminate(nil)
                }
            )
        }
    }
}

// MARK: - Menu Action Button

struct MenuActionButton: View {
    let title: String
    let icon: String
    var shortcut: String?
    var isDisabled: Bool = false
    var isDestructive: Bool = false
    let action: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: icon)
                    .font(.system(size: 13))
                    .foregroundStyle(foregroundColor)
                    .frame(width: 20)

                Text(title)
                    .font(.system(size: 13))
                    .foregroundStyle(foregroundColor)

                Spacer()

                if let shortcut {
                    Text("⌘\(shortcut)")
                        .font(.system(size: 11))
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isHovered ? Color.accentColor.opacity(0.15) : Color.clear)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(isDisabled)
        .opacity(isDisabled ? 0.5 : 1)
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var foregroundColor: Color {
        if isDisabled {
            return .secondary
        }
        if isDestructive {
            return .red
        }
        return .primary
    }
}

// MARK: - Menu Session Row

struct ImprovedMenuSessionRow: View {
    let session: SessionBridge.Session

    @State private var isHovered = false

    var body: some View {
        Button {
            WebChatManager.shared.openChat(for: session.id)
        } label: {
            HStack(spacing: 10) {
                Image(systemName: sessionIcon)
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .frame(width: 20)

                VStack(alignment: .leading, spacing: 2) {
                    Text(session.metadata.title ?? "Session")
                        .font(.system(size: 13))
                        .lineLimit(1)

                    Text(session.lastActiveAt, style: .relative)
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)
                }

                Spacer()

                sessionStatusIndicator
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isHovered ? Color.accentColor.opacity(0.15) : Color.clear)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var sessionIcon: String {
        switch session.type {
        case .chat: return "message"
        case .voice: return "mic"
        case .agent: return "cpu"
        case .computerUse: return "desktopcomputer"
        case .mcp: return "puzzlepiece"
        }
    }

    @ViewBuilder
    private var sessionStatusIndicator: some View {
        switch session.status {
        case .active:
            ProgressView()
                .controlSize(.mini)
        case .paused:
            Circle()
                .fill(.orange)
                .frame(width: 6, height: 6)
        case .error:
            Circle()
                .fill(.red)
                .frame(width: 6, height: 6)
        case .completed:
            EmptyView()
        }
    }
}

#Preview {
    ImprovedMenuContentView(
        appState: AppStateStore.shared,
        onOpenChat: {},
        onOpenSettings: {},
        onTogglePause: {}
    )
}
