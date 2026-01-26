import SwiftUI

/// Content view for the menu bar dropdown.
/// Provides quick access to chat, settings, and status.
struct MenuContentView: View {
    @Bindable var appState: AppStateStore
    @State private var coordinator = ConnectionModeCoordinator.shared
    @State private var permissions = PermissionManager.shared

    var onOpenChat: () -> Void
    var onOpenSettings: () -> Void
    var onTogglePause: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            // Status header
            statusHeader

            Divider()

            // Quick actions
            quickActions

            Divider()

            // Recent sessions (if any)
            recentSessions

            Divider()

            // Bottom actions
            bottomActions
        }
        .frame(width: 280)
    }

    @ViewBuilder
    private var statusHeader: some View {
        HStack(spacing: 12) {
            // Status icon
            ZStack {
                Circle()
                    .fill(statusColor.opacity(0.2))
                    .frame(width: 36, height: 36)

                Image(systemName: statusIcon)
                    .font(.system(size: 16))
                    .foregroundStyle(statusColor)
                    .symbolEffect(.pulse, isActive: coordinator.isConnecting)
            }

            VStack(alignment: .leading, spacing: 2) {
                Text("Nexus")
                    .font(.headline)

                HStack(spacing: 6) {
                    StatusBadge(status: badgeStatus, variant: .animated)

                    Text(coordinator.statusDescription)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer()

            // Pause button
            Button {
                withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) {
                    onTogglePause()
                }
            } label: {
                Image(systemName: appState.isPaused ? "play.fill" : "pause.fill")
                    .font(.system(size: 12))
                    .foregroundStyle(appState.isPaused ? .green : .secondary)
                    .frame(width: 26, height: 26)
                    .background(
                        Circle()
                            .fill(Color(NSColor.controlBackgroundColor))
                    )
            }
            .buttonStyle(.plain)
            .help(appState.isPaused ? "Resume" : "Pause")
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }

    @ViewBuilder
    private var quickActions: some View {
        VStack(spacing: 0) {
            MenuActionRow(
                title: "New Chat",
                icon: "message",
                shortcut: "N",
                action: onOpenChat
            )

            MenuActionRow(
                title: "Voice Input",
                icon: "mic",
                shortcut: nil,
                isDisabled: !appState.voiceWakeEnabled || !permissions.voiceWakePermissionsGranted
            ) {
                VoiceWakeOverlayController.shared.show(source: .pushToTalk)
            }

            MenuActionRow(
                title: "Screenshot",
                icon: "camera.viewfinder",
                shortcut: nil
            ) {
                Task {
                    _ = try? await ScreenCaptureService.shared.capture()
                }
            }
        }
    }

    @ViewBuilder
    private var recentSessions: some View {
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
            .padding(.vertical, 6)

            let sessions = Array(SessionBridge.shared.activeSessions.prefix(4))

            if sessions.isEmpty {
                HStack(spacing: 8) {
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
                    MenuSessionRow(session: session)
                }
            }
        }
    }

    @ViewBuilder
    private var bottomActions: some View {
        VStack(spacing: 0) {
            MenuActionRow(
                title: "Settings...",
                icon: "gear",
                shortcut: ",",
                action: onOpenSettings
            )

            Divider()
                .padding(.vertical, 4)

            MenuActionRow(
                title: "Quit Nexus",
                icon: "power",
                shortcut: "Q",
                isDestructive: true
            ) {
                NSApp.terminate(nil)
            }
        }
    }

    // MARK: - Computed Properties

    private var statusIcon: String {
        if appState.isPaused {
            return "pause.circle.fill"
        }
        switch coordinator.state {
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
        switch coordinator.state {
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

    private var badgeStatus: StatusBadge.Status {
        if appState.isPaused {
            return .offline
        }
        switch coordinator.state {
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
}

// MARK: - Menu Action Row

struct MenuActionRow: View {
    let title: String
    let icon: String
    var shortcut: String? = nil
    var isDisabled: Bool = false
    var isDestructive: Bool = false
    let action: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: icon)
                    .font(.system(size: 12))
                    .foregroundStyle(foregroundColor)
                    .frame(width: 18)

                Text(title)
                    .font(.system(size: 13))
                    .foregroundStyle(foregroundColor)

                Spacer()

                if let shortcut {
                    Text("\u{2318}\(shortcut)")
                        .font(.system(size: 11))
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
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
            withAnimation(.easeOut(duration: 0.12)) {
                isHovered = hovering
            }
        }
    }

    private var foregroundColor: Color {
        if isDisabled { return .secondary }
        if isDestructive { return .red }
        return .primary
    }
}

// MARK: - Menu Session Row (Reused)

struct MenuSessionRowView: View {
    let session: SessionBridge.Session

    @State private var isHovered = false

    var body: some View {
        Button {
            WebChatManager.shared.openChat(for: session.id)
        } label: {
            HStack(spacing: 10) {
                Image(systemName: sessionIcon)
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .frame(width: 18)

                VStack(alignment: .leading, spacing: 2) {
                    Text(session.metadata.title ?? "Session")
                        .font(.system(size: 12))
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
            withAnimation(.easeOut(duration: 0.12)) {
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

/// Custom button style for menu items
struct MenuButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(configuration.isPressed ? Color.accentColor.opacity(0.2) : Color.clear)
            .contentShape(Rectangle())
    }
}
