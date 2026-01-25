import SwiftUI

struct OverviewView: View {
    @EnvironmentObject var model: AppModel
    @State private var isRefreshing = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header
            headerSection

            // Error banner
            if let error = model.lastError {
                ErrorBanner(message: error, severity: .error) {
                    model.lastError = nil
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }

            if let wsError = model.webSocketError {
                ErrorBanner(message: "WebSocket: \(wsError)", severity: .warning)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }

            // Main content
            if isRefreshing && model.status == nil {
                LoadingStateView(message: "Loading system status...", showSkeleton: true)
            } else if let status = model.status {
                statusContent(status)
            } else {
                EmptyStateView(
                    icon: "server.rack",
                    title: "Not Connected",
                    description: "Configure gateway connection in Settings to view system status.",
                    actionTitle: "Open Settings"
                ) {
                    NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
                }
            }

            Spacer()
        }
        .padding()
        .animation(.easeInOut(duration: 0.2), value: model.lastError)
        .animation(.easeInOut(duration: 0.2), value: model.webSocketError)
    }

    // MARK: - Header Section

    private var headerSection: some View {
        HStack {
            Text("System Status")
                .font(.title2)
            Spacer()

            StatusBadge(
                status: model.isWebSocketConnected ? .online : .offline,
                variant: .animated
            )

            Button {
                refreshStatus()
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .disabled(isRefreshing)
        }
    }

    // MARK: - Status Content

    @ViewBuilder
    private func statusContent(_ status: GatewayStatus) -> some View {
        // Live indicator
        if model.isWebSocketConnected {
            HStack(spacing: 8) {
                Image(systemName: "bolt.fill")
                    .foregroundStyle(.green)
                    .symbolEffect(.pulse, isActive: true)
                Text("Live")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.green)
                Text("\u{2022}")
                    .foregroundStyle(.tertiary)
                Text("Server uptime: \(formatUptime(model.serverUptimeMs))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Color.green.opacity(0.1))
            )
        }

        // Stats cards
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 100))], spacing: 12) {
            ImprovedStatCard(title: "Uptime", value: status.uptimeString, icon: "clock")
            ImprovedStatCard(title: "Sessions", value: "\(status.sessionCount)", icon: "bubble.left.and.bubble.right")
            ImprovedStatCard(title: "Go", value: status.goVersion, icon: "chevron.left.forwardslash.chevron.right")
            ImprovedStatCard(title: "Memory", value: String(format: "%.0f MB", status.memAllocMb), icon: "memorychip")
            ImprovedStatCard(title: "Database", value: status.databaseStatus, icon: "cylinder")
        }
        .padding(.bottom, 8)

        // Active tool calls
        if !model.activeToolCalls.isEmpty {
            activeToolCallsSection
        }

        // Recent activity
        if !model.recentSessionEvents.isEmpty {
            recentActivitySection
        }

        // Channels
        channelsSection(status.channels)
    }

    // MARK: - Active Tool Calls

    private var activeToolCallsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Active Tool Calls")
                .font(.headline)

            ForEach(model.activeToolCalls.indices, id: \.self) { index in
                let toolCall = model.activeToolCalls[index]
                HStack(spacing: 10) {
                    Image(systemName: "gearshape.2.fill")
                        .foregroundStyle(.orange)
                        .symbolEffect(.rotate, isActive: true)
                    Text(toolCall.toolName ?? "Unknown Tool")
                        .font(.subheadline)
                    Spacer()
                    ProgressView()
                        .controlSize(.small)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .fill(Color.orange.opacity(0.1))
                )
            }
        }
        .padding(.bottom, 8)
    }

    // MARK: - Recent Activity

    private var recentActivitySection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Recent Activity")
                .font(.headline)

            ForEach(Array(model.recentSessionEvents.suffix(5).enumerated()), id: \.offset) { _, event in
                HStack(spacing: 10) {
                    Image(systemName: "bubble.left.fill")
                        .foregroundStyle(.blue)
                    Text(event.eventType)
                        .font(.subheadline)
                    Spacer()
                    Text(String(event.sessionId.prefix(8)) + "...")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
            }
        }
        .padding(.bottom, 8)
    }

    // MARK: - Channels Section

    @ViewBuilder
    private func channelsSection(_ channels: [ChannelStatus]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Channels")
                .font(.headline)

            if channels.isEmpty {
                HStack {
                    Image(systemName: "antenna.radiowaves.left.and.right.slash")
                        .foregroundStyle(.tertiary)
                    Text("No channels configured")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 8)
            } else {
                Table(channels) {
                    TableColumn("Name") { Text($0.name) }
                    TableColumn("Status") { channel in
                        HStack(spacing: 6) {
                            StatusBadge(
                                status: channel.status == "connected" ? .online : .error,
                                variant: .minimal
                            )
                            Text(channel.status)
                        }
                    }
                    TableColumn("Enabled") { Text($0.enabled ? "Yes" : "No") }
                    TableColumn("Healthy") { channel in
                        HStack(spacing: 6) {
                            StatusBadge(
                                status: (channel.healthy ?? false) ? .online : .warning,
                                variant: .minimal
                            )
                            Text((channel.healthy ?? false) ? "Yes" : "No")
                        }
                    }
                }
            }
        }
    }

    // MARK: - Actions

    private func refreshStatus() {
        isRefreshing = true
        Task {
            await model.refreshStatus()
            isRefreshing = false
        }
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

struct ImprovedStatCard: View {
    let title: String
    let value: String
    var icon: String? = nil

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                if let icon {
                    Image(systemName: icon)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                Text(title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Text(value)
                .font(.system(.headline, design: .rounded))
                .fontWeight(.semibold)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(isHovered ? Color.accentColor.opacity(0.3) : Color.gray.opacity(0.15), lineWidth: 1)
        )
        .scaleEffect(isHovered ? 1.02 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.7), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
    }
}
