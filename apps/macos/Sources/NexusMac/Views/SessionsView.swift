import SwiftUI

struct SessionsView: View {
    @EnvironmentObject var model: AppModel
    @State private var selectedSession: SessionSummary?
    @State private var messagePage: Int = 1
    @State private var isLoadingSessions = false
    @State private var isLoadingMessages = false

    var body: some View {
        HSplitView {
            sessionsListPane
            messagesPane
        }
    }

    // MARK: - Sessions List Pane

    private var sessionsListPane: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Header
            HStack {
                Text("Sessions")
                    .font(.title2)
                Spacer()

                StatusBadge(
                    status: model.isWebSocketConnected ? .online : .offline,
                    variant: .animated
                )

                Button {
                    refreshSessions()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .disabled(isLoadingSessions)
            }

            // Live activity indicator
            if model.isWebSocketConnected && !model.recentSessionEvents.isEmpty {
                HStack(spacing: 6) {
                    Image(systemName: "bolt.fill")
                        .foregroundStyle(.green)
                        .symbolEffect(.pulse, isActive: true)
                    Text("Live updates enabled")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(Color.green.opacity(0.1))
                )
            }

            // Content
            Group {
                if isLoadingSessions {
                    LoadingStateView(message: "Loading sessions...", showSkeleton: true)
                } else if model.sessions.isEmpty {
                    EmptyStateView(
                        icon: "bubble.left.and.bubble.right",
                        title: "No Sessions",
                        description: "Start a new conversation to see it here.",
                        actionTitle: "New Chat"
                    ) {
                        let session = SessionBridge.shared.createSession(type: .chat)
                        WebChatManager.shared.openChat(for: session.id)
                    }
                } else {
                    sessionsList
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isLoadingSessions)
            .animation(.easeInOut(duration: 0.2), value: model.sessions.isEmpty)
        }
        .frame(minWidth: 280)
        .padding()
    }

    private var sessionsList: some View {
        List(model.sessions, selection: $selectedSession) { session in
            HStack(spacing: 10) {
                // Activity indicator
                if hasRecentActivity(sessionId: session.id) {
                    Circle()
                        .fill(Color.blue)
                        .frame(width: 8, height: 8)
                        .transition(.scale.combined(with: .opacity))
                } else {
                    Circle()
                        .fill(Color.clear)
                        .frame(width: 8, height: 8)
                }

                VStack(alignment: .leading, spacing: 4) {
                    Text(session.title.isEmpty ? session.id : session.title)
                        .font(.headline)
                        .lineLimit(1)
                    HStack {
                        Text("\(session.channel) \u{2022} \(session.agentId)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                        Spacer()
                        Text(session.updatedAt, style: .relative)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            .padding(.vertical, 2)
        }
        .onChange(of: selectedSession) { _, newSession in
            guard let session = newSession else { return }
            messagePage = 1
            loadMessages(sessionID: session.id)
        }
    }

    // MARK: - Messages Pane

    private var messagesPane: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let session = selectedSession {
                // Header
                VStack(alignment: .leading, spacing: 4) {
                    Text(session.title.isEmpty ? session.id : session.title)
                        .font(.title2)
                    Text("Channel: \(session.channel) \u{2022} Agent: \(session.agentId)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                // Content
                Group {
                    if isLoadingMessages && model.sessionMessages.isEmpty {
                        LoadingStateView(message: "Loading messages...")
                    } else if model.sessionMessages.isEmpty {
                        EmptyStateView(
                            icon: "bubble.left",
                            title: "No Messages",
                            description: "This session doesn't have any messages yet."
                        )
                    } else {
                        messagesList
                    }
                }
                .animation(.easeInOut(duration: 0.2), value: isLoadingMessages)

                // Load more button
                if model.sessionHasMore && !isLoadingMessages {
                    Button("Load More") {
                        messagePage += 1
                        loadMessages(sessionID: session.id, append: true)
                    }
                    .buttonStyle(.borderless)
                }
            } else {
                EmptyStateView(
                    icon: "arrow.left.circle",
                    title: "Select a Session",
                    description: "Choose a session from the list to view its messages."
                )
            }

            // Error banner
            if let error = model.lastError {
                ErrorBanner(message: error, severity: .error) {
                    model.lastError = nil
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }

            Spacer()
        }
        .padding()
        .animation(.easeInOut(duration: 0.2), value: model.lastError)
    }

    private var messagesList: some View {
        List(model.sessionMessages) { message in
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text(message.role.capitalized)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(message.role == "assistant" ? .blue : .secondary)
                    Spacer()
                    Text(message.createdAt, style: .time)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Text(message.content)
                    .font(.body)
                    .textSelection(.enabled)
            }
            .padding(.vertical, 4)
        }
    }

    // MARK: - Actions

    private func refreshSessions() {
        isLoadingSessions = true
        Task {
            await model.refreshSessions()
            isLoadingSessions = false
        }
    }

    private func loadMessages(sessionID: String, append: Bool = false) {
        isLoadingMessages = true
        Task {
            await model.loadSessionMessages(sessionID: sessionID, page: messagePage)
            isLoadingMessages = false
        }
    }

    // MARK: - Helper Functions

    private func hasRecentActivity(sessionId: String) -> Bool {
        return model.recentSessionEvents.contains { $0.sessionId == sessionId }
    }

    private func formatRelativeDate(_ date: Date) -> String {
        let now = Date()
        let interval = now.timeIntervalSince(date)

        if interval < 60 {
            return "Just now"
        } else if interval < 3600 {
            let minutes = Int(interval / 60)
            return "\(minutes)m ago"
        } else if interval < 86400 {
            let hours = Int(interval / 3600)
            return "\(hours)h ago"
        } else {
            let days = Int(interval / 86400)
            return "\(days)d ago"
        }
    }
}
