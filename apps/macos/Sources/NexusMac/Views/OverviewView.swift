import SwiftUI

struct OverviewView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("System Status")
                    .font(.title2)
                Spacer()

                // Real-time connection indicator
                ConnectionStatusBadge(isConnected: model.isWebSocketConnected)

                Button("Refresh") {
                    Task { await model.refreshStatus() }
                }
            }

            // Real-time server info from WebSocket
            if model.isWebSocketConnected {
                HStack(spacing: 12) {
                    Label("Live", systemImage: "bolt.fill")
                        .font(.caption)
                        .foregroundColor(.green)
                    Text("Server uptime: \(formatUptime(model.serverUptimeMs))")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .background(Color.green.opacity(0.1))
                .cornerRadius(6)
            }

            if let status = model.status {
                HStack(spacing: 24) {
                    StatCard(title: "Uptime", value: status.uptimeString)
                    StatCard(title: "Sessions", value: "\(status.sessionCount)")
                    StatCard(title: "Go", value: status.goVersion)
                    StatCard(title: "Memory", value: String(format: "%.0f MB", status.memAllocMb))
                    StatCard(title: "DB", value: status.databaseStatus)
                }
                .padding(.bottom, 8)

                // Active tool calls from WebSocket
                if !model.activeToolCalls.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Active Tool Calls")
                            .font(.headline)

                        ForEach(model.activeToolCalls.indices, id: \.self) { index in
                            let toolCall = model.activeToolCalls[index]
                            HStack {
                                Image(systemName: "gear.badge.checkmark")
                                    .foregroundColor(.orange)
                                Text(toolCall.toolName ?? "Unknown Tool")
                                    .font(.subheadline)
                                Spacer()
                                ProgressView()
                                    .scaleEffect(0.7)
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(Color.orange.opacity(0.1))
                            .cornerRadius(6)
                        }
                    }
                    .padding(.bottom, 8)
                }

                // Recent session events from WebSocket
                if !model.recentSessionEvents.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text("Recent Activity")
                            .font(.headline)

                        ForEach(model.recentSessionEvents.suffix(5).indices, id: \.self) { index in
                            let event = model.recentSessionEvents[index]
                            HStack {
                                Image(systemName: "bubble.left.fill")
                                    .foregroundColor(.blue)
                                Text(event.eventType)
                                    .font(.subheadline)
                                Spacer()
                                Text(event.sessionId.prefix(8) + "...")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                        }
                    }
                    .padding(.bottom, 8)
                }

                Text("Channels")
                    .font(.headline)

                Table(status.channels) {
                    TableColumn("Name") { Text($0.name) }
                    TableColumn("Status") { channel in
                        HStack(spacing: 6) {
                            Circle()
                                .fill(channel.status == "connected" ? Color.green : Color.red)
                                .frame(width: 8, height: 8)
                            Text(channel.status)
                        }
                    }
                    TableColumn("Enabled") { Text($0.enabled ? "Yes" : "No") }
                    TableColumn("Healthy") { channel in
                        HStack(spacing: 6) {
                            Circle()
                                .fill((channel.healthy ?? false) ? Color.green : Color.orange)
                                .frame(width: 8, height: 8)
                            Text((channel.healthy ?? false) ? "Yes" : "No")
                        }
                    }
                }
            } else {
                Text("No status loaded. Configure base URL and API key in Settings.")
                    .foregroundColor(.secondary)
            }

            if let error = model.lastError {
                Text(error)
                    .foregroundColor(.red)
            }

            if let wsError = model.webSocketError {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                    Text("WebSocket: \(wsError)")
                }
                .foregroundColor(.orange)
                .font(.caption)
            }

            Spacer()
        }
        .padding()
    }

    private func formatUptime(_ ms: Int64) -> String {
        let seconds = ms / 1000
        let minutes = seconds / 60
        let hours = minutes / 60
        let days = hours / 24

        if days > 0 {
            return "\(days)d \(hours % 24)h"
        } else if hours > 0 {
            return "\(hours)h \(minutes % 60)m"
        } else if minutes > 0 {
            return "\(minutes)m \(seconds % 60)s"
        } else {
            return "\(seconds)s"
        }
    }
}

// MARK: - Connection Status Badge

struct ConnectionStatusBadge: View {
    let isConnected: Bool

    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(isConnected ? Color.green : Color.red)
                .frame(width: 8, height: 8)
            Text(isConnected ? "Connected" : "Disconnected")
                .font(.caption)
                .foregroundColor(isConnected ? .green : .red)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .background(isConnected ? Color.green.opacity(0.1) : Color.red.opacity(0.1))
        .cornerRadius(12)
    }
}

struct StatCard: View {
    let title: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundColor(.secondary)
            Text(value)
                .font(.headline)
        }
        .padding(10)
        .background(Color(NSColor.windowBackgroundColor))
        .cornerRadius(8)
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.gray.opacity(0.2))
        )
    }
}
