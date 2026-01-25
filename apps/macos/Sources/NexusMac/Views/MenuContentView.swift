import SwiftUI

/// Content view for the menu bar dropdown.
/// Provides quick access to chat, settings, and status.
struct MenuContentView: View {
    @Bindable var appState: AppStateStore

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
    }

    @ViewBuilder
    private var statusHeader: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text("Nexus")
                    .font(.headline)

                HStack(spacing: 4) {
                    Circle()
                        .fill(connectionColor)
                        .frame(width: 6, height: 6)

                    Text(connectionStatus)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            Button {
                onTogglePause()
            } label: {
                Image(systemName: appState.isPaused ? "play.fill" : "pause.fill")
            }
            .buttonStyle(.borderless)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private var quickActions: some View {
        VStack(spacing: 0) {
            Button {
                onOpenChat()
            } label: {
                Label("New Chat", systemImage: "message")
            }
            .buttonStyle(MenuButtonStyle())

            Button {
                // Voice input
            } label: {
                Label("Voice Input", systemImage: "mic")
            }
            .buttonStyle(MenuButtonStyle())
            .disabled(!appState.voiceWakeEnabled)

            Button {
                // Screenshot
                Task {
                    _ = try? await ScreenCaptureService.shared.capture()
                }
            } label: {
                Label("Screenshot", systemImage: "camera")
            }
            .buttonStyle(MenuButtonStyle())
        }
    }

    @ViewBuilder
    private var recentSessions: some View {
        let sessions = SessionBridge.shared.activeSessions.prefix(3)

        if sessions.isEmpty {
            Text("No active sessions")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.vertical, 8)
        } else {
            ForEach(Array(sessions)) { session in
                Button {
                    WebChatManager.shared.openChat(for: session.id)
                } label: {
                    HStack {
                        Image(systemName: sessionIcon(for: session.type))
                        Text(session.metadata.title ?? "Session")
                            .lineLimit(1)
                        Spacer()
                        Text(session.lastActiveAt, style: .relative)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
                .buttonStyle(MenuButtonStyle())
            }
        }
    }

    @ViewBuilder
    private var bottomActions: some View {
        VStack(spacing: 0) {
            Button {
                onOpenSettings()
            } label: {
                Label("Settings...", systemImage: "gear")
            }
            .buttonStyle(MenuButtonStyle())
            .keyboardShortcut(",", modifiers: .command)

            Divider()

            Button {
                NSApp.terminate(nil)
            } label: {
                Label("Quit Nexus", systemImage: "power")
            }
            .buttonStyle(MenuButtonStyle())
            .keyboardShortcut("q", modifiers: .command)
        }
    }

    private var connectionStatus: String {
        if appState.isPaused {
            return "Paused"
        }

        switch appState.connectionMode {
        case .local:
            return "Local Gateway"
        case .remote:
            return "Remote: \(appState.remoteHost ?? "Unknown")"
        case .unconfigured:
            return "Not Configured"
        }
    }

    private var connectionColor: Color {
        if appState.isPaused {
            return .secondary
        }

        switch appState.connectionMode {
        case .local, .remote:
            return .green
        case .unconfigured:
            return .orange
        }
    }

    private func sessionIcon(for type: SessionBridge.Session.SessionType) -> String {
        switch type {
        case .chat: return "message"
        case .voice: return "mic"
        case .agent: return "cpu"
        case .computerUse: return "desktopcomputer"
        case .mcp: return "puzzlepiece"
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
