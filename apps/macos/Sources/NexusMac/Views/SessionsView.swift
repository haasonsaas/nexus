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
                    Button("Refresh") {
                        Task { await model.refreshSessions() }
                    }
                }

                List(model.sessions, selection: $selectedSession) { session in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(session.title.isEmpty ? session.id : session.title)
                            .font(.headline)
                        Text("\(session.channel) • \(session.agentId)")
                            .font(.caption)
                            .foregroundColor(.secondary)
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
}
