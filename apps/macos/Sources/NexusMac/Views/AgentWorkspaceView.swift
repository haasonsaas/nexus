import SwiftUI

/// Workspace view showing active agents and their status.
/// Provides visibility into agent orchestration and activity.
struct AgentWorkspaceView: View {
    @State private var orchestrator = AgentOrchestrator.shared
    @State private var selectedAgent: AgentOrchestrator.AgentInstance?
    @State private var isExpanded = true

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            headerView

            // Content
            if orchestrator.activeAgents.isEmpty {
                EmptyStateView(
                    icon: "cpu",
                    title: "No Active Agents",
                    description: "Agents will appear here when they're running tasks."
                )
            } else {
                agentsList
            }

            Spacer(minLength: 0)
        }
        .padding()
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Agent Workspace")
                    .font(.title2)

                HStack(spacing: 8) {
                    if orchestrator.isProcessing {
                        HStack(spacing: 4) {
                            ProgressView()
                                .controlSize(.mini)
                            Text("Processing")
                                .font(.caption)
                                .foregroundStyle(.blue)
                        }
                    } else {
                        Text("\(orchestrator.activeAgents.count) active")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }

            Spacer()

            // Status badge
            StatusBadge(
                status: orchestrator.isProcessing ? .connecting : (orchestrator.activeAgents.isEmpty ? .offline : .online),
                variant: .animated
            )
        }
    }

    // MARK: - Agents List

    private var agentsList: some View {
        ScrollView {
            LazyVStack(spacing: 10) {
                ForEach(orchestrator.activeAgents) { agent in
                    AgentCard(
                        agent: agent,
                        isSelected: selectedAgent?.id == agent.id,
                        onSelect: {
                            withAnimation(.spring(response: 0.3)) {
                                selectedAgent = agent
                            }
                        },
                        onTerminate: {
                            withAnimation(.spring(response: 0.3)) {
                                orchestrator.terminate(agentId: agent.id)
                                if selectedAgent?.id == agent.id {
                                    selectedAgent = nil
                                }
                            }
                        }
                    )
                }
            }
        }
    }
}

// MARK: - Agent Card

struct AgentCard: View {
    let agent: AgentOrchestrator.AgentInstance
    let isSelected: Bool
    let onSelect: () -> Void
    let onTerminate: () -> Void

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Header
            HStack(spacing: 10) {
                // Type icon
                ZStack {
                    Circle()
                        .fill(agentColor.opacity(0.2))
                        .frame(width: 36, height: 36)

                    Image(systemName: agentIcon)
                        .font(.system(size: 16))
                        .foregroundStyle(agentColor)
                        .symbolEffect(.pulse, isActive: agent.status == .executing || agent.status == .thinking)
                }

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(agentTypeName)
                            .font(.subheadline.weight(.medium))

                        AgentStatusBadge(status: agent.status)
                    }

                    Text(agent.id.prefix(12) + "...")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .textSelection(.enabled)
                }

                Spacer()

                // Terminate button
                Button {
                    onTerminate()
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .opacity(isHovered ? 1 : 0)
            }

            // Current task
            if let task = agent.currentTask, !task.isEmpty {
                HStack(spacing: 6) {
                    Image(systemName: "arrow.right.circle.fill")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    Text(task)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
                .background(
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(Color(NSColor.controlBackgroundColor))
                )
            }

            // Timing info
            HStack(spacing: 12) {
                Label("Started \(agent.startedAt, style: .relative)", systemImage: "clock")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                Label("Active \(agent.lastActivityAt, style: .relative)", systemImage: "bolt")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(isSelected ? agentColor.opacity(0.5) : (isHovered ? Color.gray.opacity(0.3) : Color.gray.opacity(0.15)), lineWidth: isSelected ? 2 : 1)
        )
        .scaleEffect(isHovered ? 1.01 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
        .onTapGesture {
            onSelect()
        }
    }

    private var agentTypeName: String {
        switch agent.type {
        case .computerUse: return "Computer Use"
        case .coder: return "Coder"
        case .researcher: return "Researcher"
        case .assistant: return "Assistant"
        case .custom: return "Custom"
        }
    }

    private var agentIcon: String {
        switch agent.type {
        case .computerUse: return "desktopcomputer"
        case .coder: return "chevron.left.forwardslash.chevron.right"
        case .researcher: return "magnifyingglass"
        case .assistant: return "bubble.left.and.bubble.right"
        case .custom: return "cpu"
        }
    }

    private var agentColor: Color {
        switch agent.type {
        case .computerUse: return .purple
        case .coder: return .blue
        case .researcher: return .green
        case .assistant: return .orange
        case .custom: return .gray
        }
    }
}

// MARK: - Agent Status Badge

struct AgentStatusBadge: View {
    let status: AgentOrchestrator.AgentInstance.AgentStatus

    var body: some View {
        HStack(spacing: 4) {
            if status == .executing || status == .thinking {
                ProgressView()
                    .controlSize(.mini)
            } else {
                Circle()
                    .fill(statusColor)
                    .frame(width: 6, height: 6)
            }

            Text(statusText)
                .font(.caption2.weight(.medium))
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(
            Capsule()
                .fill(statusColor.opacity(0.15))
        )
        .foregroundStyle(statusColor)
    }

    private var statusText: String {
        switch status {
        case .idle: return "Idle"
        case .thinking: return "Thinking"
        case .executing: return "Executing"
        case .waiting: return "Waiting"
        case .completed: return "Completed"
        case .error: return "Error"
        }
    }

    private var statusColor: Color {
        switch status {
        case .idle: return .secondary
        case .thinking: return .blue
        case .executing: return .orange
        case .waiting: return .yellow
        case .completed: return .green
        case .error: return .red
        }
    }
}

// MARK: - Menu Session Row (Bridge)

struct MenuSessionRow: View {
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
            Circle()
                .fill(.green)
                .frame(width: 6, height: 6)
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
    AgentWorkspaceView()
        .frame(width: 400, height: 500)
}
