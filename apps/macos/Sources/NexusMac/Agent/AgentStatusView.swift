import OSLog
import SwiftUI

// MARK: - Agent Status View

/// Shows agent lifecycle status with active agents and controls
struct AgentStatusView: View {
    @State private var lifecycle = AgentLifecycleManager.shared
    @State private var showHistory = false
    @State private var selectedAgentId: String?

    private let logger = Logger(subsystem: "com.nexus.mac", category: "agent-status-view")

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            headerView

            if showHistory {
                historyView
            } else {
                activeAgentsView
            }
        }
        .animation(.easeInOut(duration: 0.2), value: lifecycle.activeAgents.count)
        .animation(.easeInOut(duration: 0.2), value: showHistory)
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text("Agents")
                    .font(.headline)

                if lifecycle.isAnyAgentRunning {
                    Text("\(lifecycle.activeCount) active")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            // History toggle
            Picker("View", selection: $showHistory) {
                Text("Active").tag(false)
                Text("History").tag(true)
            }
            .pickerStyle(.segmented)
            .frame(width: 140)

            // Cancel all button
            if lifecycle.isAnyAgentRunning {
                Button {
                    lifecycle.cancelAll()
                } label: {
                    Label("Cancel All", systemImage: "xmark.circle")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .tint(.red)
            }
        }
    }

    // MARK: - Active Agents

    private var activeAgentsView: some View {
        if lifecycle.activeAgents.isEmpty {
            return AnyView(
                EmptyStateView(
                    icon: "bolt.horizontal.circle",
                    title: "No Active Agents",
                    description: "Agents will appear here when they are running."
                )
                .frame(maxHeight: 200)
            )
        }

        return AnyView(
            ScrollView {
                LazyVStack(spacing: 8) {
                    ForEach(lifecycle.activeAgents, id: \.id) { agent in
                        AgentStatusInstanceRow(
                            agent: agent,
                            isSelected: selectedAgentId == agent.id,
                            onSelect: { selectedAgentId = agent.id }
                        )
                    }
                }
                .padding(.vertical, 4)
            }
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.textBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .stroke(Color.gray.opacity(0.2), lineWidth: 1)
            )
        )
    }

    // MARK: - History View

    private var historyView: some View {
        Group {
            if lifecycle.completedAgents.isEmpty {
                EmptyStateView(
                    icon: "clock.arrow.circlepath",
                    title: "No History",
                    description: "Completed agents will appear here."
                )
                .frame(maxHeight: 200)
            } else {
                VStack(spacing: 8) {
                    HStack {
                        Text("\(lifecycle.completedAgents.count) completed")
                            .font(.caption)
                            .foregroundStyle(.secondary)

                        Spacer()

                        Button("Clear") {
                            lifecycle.clearHistory()
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.mini)
                    }

                    ScrollView {
                        LazyVStack(spacing: 6) {
                            ForEach(lifecycle.completedAgents, id: \.id) { agent in
                                CompletedAgentRow(agent: agent)
                            }
                        }
                        .padding(.vertical, 4)
                    }
                    .background(
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(Color(NSColor.textBackgroundColor))
                    )
                    .overlay(
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .stroke(Color.gray.opacity(0.2), lineWidth: 1)
                    )
                }
            }
        }
    }
}

// MARK: - Agent Instance Row

private struct AgentStatusInstanceRow: View {
    let agent: AgentInstance
    let isSelected: Bool
    let onSelect: () -> Void

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            // Header row
            HStack(spacing: 8) {
                stateIndicator

                VStack(alignment: .leading, spacing: 2) {
                    Text(agent.currentTask ?? "Running...")
                        .font(.subheadline)
                        .lineLimit(1)

                    HStack(spacing: 8) {
                        Text(agent.state.displayName)
                            .font(.caption2)
                            .foregroundStyle(stateColor)

                        Text(formatDuration(agent.elapsedDuration))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }

                Spacer()

                // Actions
                actionButtons
            }

            // Progress bar
            if agent.progress > 0 && agent.progress < 1 {
                ProgressView(value: agent.progress)
                    .progressViewStyle(.linear)
                    .tint(stateColor)
            }

            // Metrics row
            metricsRow
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(isHovered || isSelected ? Color.secondary.opacity(0.1) : Color.clear)
        )
        .contentShape(Rectangle())
        .onTapGesture(perform: onSelect)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    // MARK: - State Indicator

    @ViewBuilder
    private var stateIndicator: some View {
        switch agent.state {
        case .executing, .spawning:
            ProgressView()
                .controlSize(.small)
                .frame(width: 20, height: 20)

        case .paused:
            Image(systemName: "pause.circle.fill")
                .font(.system(size: 18))
                .foregroundStyle(.orange)

        case .waitingForTool:
            Image(systemName: "hammer.circle.fill")
                .font(.system(size: 18))
                .foregroundStyle(.blue)
                .symbolEffect(.pulse)

        case .waitingForApproval:
            Image(systemName: "hand.raised.circle.fill")
                .font(.system(size: 18))
                .foregroundStyle(.yellow)
                .symbolEffect(.pulse)

        default:
            Image(systemName: "circle.fill")
                .font(.system(size: 8))
                .foregroundStyle(.secondary)
                .frame(width: 20, height: 20)
        }
    }

    private var stateColor: Color {
        switch agent.state {
        case .executing, .spawning: return .blue
        case .paused: return .orange
        case .waitingForTool: return .blue
        case .waitingForApproval: return .yellow
        case .completed: return .green
        case .failed: return .red
        case .cancelled: return .secondary
        default: return .secondary
        }
    }

    // MARK: - Actions

    @ViewBuilder
    private var actionButtons: some View {
        HStack(spacing: 4) {
            if agent.state == .executing || agent.state == .waitingForTool {
                Button {
                    AgentLifecycleManager.shared.pause(agent.id)
                } label: {
                    Image(systemName: "pause.fill")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .help("Pause agent")
            }

            if agent.state == .paused {
                Button {
                    AgentLifecycleManager.shared.resume(agent.id)
                } label: {
                    Image(systemName: "play.fill")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.green)
                .help("Resume agent")
            }

            Button {
                AgentLifecycleManager.shared.cancel(agent.id)
            } label: {
                Image(systemName: "xmark")
                    .font(.caption.weight(.semibold))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
            .help("Cancel agent")
        }
    }

    // MARK: - Metrics

    private var metricsRow: some View {
        HStack(spacing: 12) {
            // Tool calls
            Label("\(agent.metrics.toolCallCount)", systemImage: "wrench.and.screwdriver")
                .font(.caption2)
                .foregroundStyle(.tertiary)

            // Tokens
            if agent.metrics.totalTokens > 0 {
                Label(formatTokens(agent.metrics.totalTokens), systemImage: "text.bubble")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            Spacer()

            // Session ID (truncated)
            Text(truncatedId(agent.sessionId))
                .font(.caption2)
                .foregroundStyle(.quaternary)
                .help("Session: \(agent.sessionId)")
        }
    }

    // MARK: - Helpers

    private func formatDuration(_ interval: TimeInterval) -> String {
        let minutes = Int(interval) / 60
        let seconds = Int(interval) % 60
        return String(format: "%d:%02d", minutes, seconds)
    }

    private func formatTokens(_ count: Int) -> String {
        if count >= 1000 {
            return String(format: "%.1fk", Double(count) / 1000)
        }
        return "\(count)"
    }

    private func truncatedId(_ id: String) -> String {
        if id.count > 8 {
            return String(id.prefix(8)) + "..."
        }
        return id
    }
}

// MARK: - Completed Agent Row

struct CompletedAgentRow: View {
    let agent: AgentInstance

    var body: some View {
        HStack(spacing: 10) {
            // State icon
            stateIcon

            // Info
            VStack(alignment: .leading, spacing: 2) {
                Text(agent.currentTask ?? "Completed task")
                    .font(.caption)
                    .lineLimit(1)

                HStack(spacing: 8) {
                    Text(agent.state.displayName)
                        .font(.caption2)
                        .foregroundStyle(stateColor)

                    if let completedAt = agent.completedAt {
                        Text(completedAt, style: .relative)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
            }

            Spacer()

            // Metrics summary
            VStack(alignment: .trailing, spacing: 2) {
                Text("\(agent.metrics.toolCallCount) tools")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                Text(formatDuration(agent.metrics.totalDuration))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private var stateIcon: some View {
        switch agent.state {
        case .completed:
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.green)

        case .failed:
            Image(systemName: "xmark.circle.fill")
                .foregroundStyle(.red)

        case .cancelled:
            Image(systemName: "minus.circle.fill")
                .foregroundStyle(.secondary)

        default:
            Image(systemName: "circle.fill")
                .foregroundStyle(.secondary)
        }
    }

    private var stateColor: Color {
        switch agent.state {
        case .completed: return .green
        case .failed: return .red
        case .cancelled: return .secondary
        default: return .secondary
        }
    }

    private func formatDuration(_ interval: TimeInterval) -> String {
        let minutes = Int(interval) / 60
        let seconds = Int(interval) % 60
        return String(format: "%d:%02d", minutes, seconds)
    }
}

// MARK: - Compact Agent Status (for menu bar)

/// Compact agent status indicator for menu bar or status areas
struct CompactAgentStatus: View {
    @State private var lifecycle = AgentLifecycleManager.shared

    var body: some View {
        if lifecycle.isAnyAgentRunning {
            HStack(spacing: 6) {
                ProgressView()
                    .controlSize(.small)

                Text("\(lifecycle.activeCount)")
                    .font(.caption.monospacedDigit())

                if lifecycle.activeCount == 1,
                   let agent = lifecycle.activeAgents.first {
                    Text(agent.state.displayName)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                Capsule()
                    .fill(Color.blue.opacity(0.1))
            )
        }
    }
}

// MARK: - Agent Status Badge

/// Badge showing agent count with animation
private struct AgentStatusBadgeView: View {
    @State private var lifecycle = AgentLifecycleManager.shared

    var body: some View {
        if lifecycle.activeCount > 0 {
            ZStack {
                Circle()
                    .fill(.blue)
                    .frame(width: 18, height: 18)

                Text("\(lifecycle.activeCount)")
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(.white)
            }
            .transition(.scale.combined(with: .opacity))
        }
    }
}

// MARK: - Previews

#if DEBUG
#Preview("Agent Status View") {
    AgentStatusView()
        .frame(width: 400, height: 400)
        .padding()
}

#Preview("Agent Instance Row") {
    let agent = AgentLifecycleManager._testCreateAgent()
    AgentStatusInstanceRow(agent: agent, isSelected: false, onSelect: {})
        .frame(width: 350)
        .padding()
}

#Preview("Compact Status") {
    CompactAgentStatus()
        .padding()
}
#endif
