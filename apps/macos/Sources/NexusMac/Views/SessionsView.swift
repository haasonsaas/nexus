import SwiftUI

struct SessionsView: View {
    @EnvironmentObject var model: AppModel
    @State private var selectedSession: SessionSummary?
    @State private var messagePage: Int = 1

    var body: some View {
        HSplitView {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Sessions")
                        .font(.title2)
                    Spacer()

                    // Real-time connection indicator
                    ConnectionStatusBadge(isConnected: model.isWebSocketConnected)

                    Button("Refresh") {
                        Task { await model.refreshSessions() }
                    }
                }

                // Real-time activity indicator
                if model.isWebSocketConnected && !model.recentSessionEvents.isEmpty {
                    HStack {
                        Image(systemName: "bolt.fill")
                            .foregroundColor(.green)
                        Text("Live updates enabled")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Color.green.opacity(0.1))
                    .cornerRadius(6)
                }

                List(model.sessions, selection: $selectedSession) { session in
                    HStack {
                        // Activity indicator for sessions with recent events
                        if hasRecentActivity(sessionId: session.id) {
                            Circle()
                                .fill(Color.blue)
                                .frame(width: 8, height: 8)
                        }

                        VStack(alignment: .leading, spacing: 4) {
                            Text(session.title.isEmpty ? session.id : session.title)
                                .font(.headline)
                            HStack {
                                Text("\(session.channel) • \(session.agentId)")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                Spacer()
                                Text(formatRelativeDate(session.updatedAt))
                                    .font(.caption2)
                                    .foregroundColor(.secondary)
                            }
                        }
                    }
                }
                .onChange(of: selectedSession) { newSession in
                    guard let session = newSession else { return }
                    messagePage = 1
                    Task { await model.loadSessionMessages(sessionID: session.id, page: messagePage) }
                }
            }
            .frame(minWidth: 280)
            .padding()

            VStack(alignment: .leading, spacing: 12) {
                if let session = selectedSession {
                    Text(session.title.isEmpty ? session.id : session.title)
                        .font(.title2)
                    Text("Channel: \(session.channel) • Agent: \(session.agentId)")
                        .font(.caption)
                        .foregroundColor(.secondary)

                    List(model.sessionMessages) { message in
                        VStack(alignment: .leading, spacing: 6) {
                            HStack {
                                Text(message.role.capitalized)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                Spacer()
                                Text(message.createdAt.formatted())
                                    .font(.caption2)
                                    .foregroundColor(.secondary)
                            }
                            Text(message.content)
                                .font(.body)
                                .textSelection(.enabled)
                        }
                        .padding(.vertical, 4)
                    }

                    if model.sessionHasMore {
                        Button("Load More") {
                            messagePage += 1
                            Task { await model.loadSessionMessages(sessionID: session.id, page: messagePage) }
                        }
                    }
                } else {
                    Text("Select a session to view messages")
                        .foregroundColor(.secondary)
                }

                if let error = model.lastError {
                    Text(error)
                        .foregroundColor(.red)
                }

                Spacer()
            }
            .padding()
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
